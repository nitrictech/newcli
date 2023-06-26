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
	"syscall"
	"time"

	"github.com/bep/debounce"
	"github.com/pkg/errors"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"

	"github.com/nitrictech/cli/pkg/dashboard"
	"github.com/nitrictech/cli/pkg/history"
	"github.com/nitrictech/cli/pkg/output"
	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/run"
	"github.com/nitrictech/cli/pkg/tasklet"
)

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

		config, err := project.ConfigFromProjectPath("")
		cobra.CheckErr(err)

		proj, err := project.FromConfig(config)
		cobra.CheckErr(err)

		dash, err := dashboard.New(proj, map[string]string{})
		if err != nil {
			cobra.CheckErr(err)
		}

		ls := run.NewLocalServices(&project.Project{
			Name: "local",
			History: &history.History{
				ProjectDir: proj.Dir,
			},
		}, true, dash)
		if ls.Running() {
			pterm.Error.Println("Only one instance of Nitric can be run locally at a time, please check that you have ended all other instances and try again")
			os.Exit(2)
		}

		memerr := make(chan error)
		pool := run.NewRunProcessPool()

		startLocalServices := tasklet.Runner{
			StartMsg: "Starting Local Services",
			Runner: func(progress output.Progress) error {
				go func(errch chan error) {
					errch <- ls.Start(pool)
				}(memerr)

				for {
					select {
					case err := <-memerr:
						// catch any early errors from Start()
						if err != nil {
							return err
						}
					default:
					}
					if ls.Running() {
						break
					}
					progress.Busyf("Waiting for Local Services to be ready")
					time.Sleep(time.Second)
				}
				return nil
			},
			StopMsg: "Started Local Services!",
		}
		tasklet.MustRun(startLocalServices, tasklet.Opts{
			Signal: term,
		})

		pterm.DefaultBasicText.Println("Local running, use ctrl-C to stop")

		stackState := run.NewStackState(proj)

		area, _ := pterm.DefaultArea.Start()
		// Create a debouncer for the refresh and remove locking
		debounced := debounce.New(500 * time.Millisecond)

		// React to worker pool state and update services table
		pool.Listen(func(we run.WorkerEvent) {
			debounced(func() {
				err := ls.Refresh()
				if err != nil {
					cobra.CheckErr(err)
				}

				stackState.Update(pool, ls)

				area.Update(stackState.Tables(9001, *ls.GetDashPort()))

				for _, warning := range stackState.Warnings() {
					pterm.Warning.Println(warning)
				}
			})
		})

		select {
		case membraneError := <-memerr:
			fmt.Println(errors.WithMessage(membraneError, "membrane error, exiting"))
		case <-term:
			fmt.Println("Shutting down services - exiting")
		}

		_ = area.Stop()
		// Stop the membrane
		cobra.CheckErr(ls.Stop())
	},
	Args: cobra.ExactArgs(0),
}

func RootCommand() *cobra.Command {
	return startCmd
}
