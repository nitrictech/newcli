// Copyright Nitric Pty Ltd.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package start

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"

	"github.com/nitrictech/cli/pkg/output"
	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/run"
	"github.com/nitrictech/cli/pkg/tasklet"
)

// decorateStatus - return a stylized string for printing statuses
func decorateStatus(status run.ServiceStatus, text string) string {
	if status == run.Stopped {
		return pterm.Red(fmt.Sprintf("🔴 %s", text))
	} else if status == run.Started {
		return pterm.Green(fmt.Sprintf("🟢 %s", text))
	}
	return pterm.Yellow(fmt.Sprintf("🟡 %s", text))
}

// serviceStatusTable - return a stylized table of all local service statuses
func serviceStatusTable(status run.LocalServicesStatus) (string, int) {
	statuses := []string{
		decorateStatus(run.Started, "API Gateway"),
		decorateStatus(status.StorageStatus, "Storage"),
		decorateStatus(run.Started, "Queues"),
		decorateStatus(run.Started, "Messages"),
		decorateStatus(run.Started, "Collections"),
		decorateStatus(run.Started, "Secrets"),
	}

	tableData := pterm.TableData{}
	cols := 3
	for i, j := 0, cols; i < len(statuses); i, j = i+cols, j+cols {
		if j > len(statuses) {
			j = len(statuses)
		}

		tableData = append(tableData, statuses[i:j])
	}
	str, _ := pterm.DefaultTable.WithData(tableData).Srender()

	return str, len(tableData)
}

var startCmd = &cobra.Command{
	Use:         "start",
	Short:       "Run nitric services locally for development and testing",
	Long:        `Run nitric services locally for development and testing`,
	Example:     `nitric start`,
	Annotations: map[string]string{"commonCommand": "yes"},
	Run: func(cmd *cobra.Command, args []string) {
		term := make(chan os.Signal, 1)
		signal.Notify(term, syscall.SIGTERM, syscall.SIGINT)

		// Divert default log output to pterm debug
		log.SetOutput(output.NewPtermWriter(pterm.Debug))
		log.SetFlags(0)

		var servicesStatus = run.LocalServicesStatus{}
		statusChannel := make(chan run.LocalServicesStatus)

		stackState := run.NewStackState()

		area, _ := pterm.DefaultArea.Start()
		lck := sync.Mutex{}
		printStatus := func(we *run.WorkerEvent) {
			lck.Lock()
			defer lck.Unlock()
			// area.Clear()

			if we != nil {
				stackState.UpdateFromWorkerEvent(*we)
			}

			tables := []string{}

			table, rows := serviceStatusTable(servicesStatus)
			if rows > 0 {
				tables = append(tables, table)
			}

			table, rows = stackState.ApiTable(9001)
			if rows > 0 {
				tables = append(tables, table)
			}

			table, rows = stackState.TopicTable(9001)
			if rows > 0 {
				tables = append(tables, table)
			}

			table, rows = stackState.SchedulesTable(9001)
			if rows > 0 {
				tables = append(tables, table)
			}
			area.Update(strings.Join(tables, "\n\n"))
		}

		localServices := run.NewLocalServices(&project.Project{
			Name: "local",
		}, statusChannel)

		if localServices.Running() {
			pterm.Error.Println("Only one instance of Nitric can be run locally at a time, please check that you have ended all other instances and try again")
			os.Exit(2)
		}

		memErr := make(chan error)
		pool := run.NewRunProcessPool()

		startLocalServices := tasklet.Runner{
			StartMsg: "Local Services Initializing",
			Runner: func(progress output.Progress) error {
				go func(errChannel chan error) {
					errChannel <- localServices.Start(pool, statusChannel)
				}(memErr)

				for {
					select {
					case err := <-memErr:
						// catch any early errors from Start()
						if err != nil {
							return err
						}
					default:
					}
					if localServices.Running() {
						break
					}
					progress.Busyf("Local Services Initializing")
					time.Sleep(time.Second)
				}
				return nil
			},
			StopMsg: "Local Services Initialized",
		}
		tasklet.MustRun(startLocalServices, tasklet.Opts{
			Signal: term,
		})

		pterm.DefaultBasicText.Println("Running, use ctrl-C to stop")
		// Once the running message has printed, start printing local service status updates
		go func() {
			for {
				servicesStatus = <-statusChannel
				printStatus(nil)
			}
		}()
		// React to worker pool state and update services table
		pool.Listen(printStatus)

		select {
		case membraneError := <-memErr:
			fmt.Println(errors.WithMessage(membraneError, "membrane error, exiting"))
		case <-term:
			fmt.Println("Shutting down services - exiting")
		}

		_ = area.Stop()
		// Stop the membrane
		cobra.CheckErr(localServices.Stop())
	},
	Args: cobra.ExactArgs(0),
}

func RootCommand() *cobra.Command {
	return startCmd
}
