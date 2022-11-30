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

package run

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/pkg/errors"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"

	"github.com/nitrictech/cli/pkg/build"
	"github.com/nitrictech/cli/pkg/containerengine"
	"github.com/nitrictech/cli/pkg/output"
	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/run"
	"github.com/nitrictech/cli/pkg/tasklet"
	"github.com/nitrictech/cli/pkg/utils"
)

var envFile string

var runCmd = &cobra.Command{
	Use:         "run",
	Short:       "Run your project locally for development and testing",
	Long:        `Run your project locally for development and testing`,
	Example:     `nitric run`,
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

		envFiles := utils.FilesExisting(".env", ".env.development", envFile)
		envMap := map[string]string{}
		if len(envFiles) > 0 {
			envMap, err = godotenv.Read(envFiles...)
			cobra.CheckErr(err)
		}

		ls := run.NewLocalServices(proj, nil)
		if ls.Running() {
			pterm.Error.Println("Only one instance of Nitric can be run locally at a time, please check that you have ended all other instances and try again")
			os.Exit(2)
		}

		ce, err := containerengine.Discover()
		cobra.CheckErr(err)

		logger := ce.Logger(proj.Dir)
		cobra.CheckErr(logger.Start())

		createBaseImage := tasklet.Runner{
			StartMsg: "Building Images",
			Runner: func(_ output.Progress) error {
				return build.BuildBaseImages(proj)
			},
			StopMsg: "Images Built",
		}
		tasklet.MustRun(createBaseImage, tasklet.Opts{Signal: term})

		memerr := make(chan error)
		pool := run.NewRunProcessPool()

		startLocalServices := tasklet.Runner{
			StartMsg: "Starting local services",
			Runner: func(progress output.Progress) error {
				go func(errch chan error) {
					errch <- ls.Start(pool, nil)
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
					progress.Busyf("Waiting for local services...")
					time.Sleep(time.Second)
				}
				return nil
			},
			StopMsg: "Local services running",
		}
		tasklet.MustRun(startLocalServices, tasklet.Opts{
			Signal: term,
		})

		var functions []*run.Function

		startFunctions := tasklet.Runner{
			StartMsg: "Starting functions",
			Runner: func(_ output.Progress) error {
				functions, err = run.FunctionsFromHandlers(proj)
				if err != nil {
					return err
				}
				for _, f := range functions {
					err = f.Start(envMap)
					if err != nil {
						return err
					}
				}
				return nil
			},
			StopMsg: "Functions running",
		}
		tasklet.MustRun(startFunctions, tasklet.Opts{Signal: term})

		pterm.DefaultBasicText.Println("Application running, use ctrl-C to stop")

		stackState := run.StateFromPool(pool)

		area, _ := pterm.DefaultArea.Start()
		area.Update(stackState.Tables(9001))

		lck := sync.Mutex{}
		// React to worker pool state and update services table
		pool.Listen(func(we *run.WorkerEvent) {
			lck.Lock()
			defer lck.Unlock()
			// area.Clear()

			stackState.UpdateFromWorkerEvent(*we)
			area.Update(stackState.Tables(9001))
		})

		select {
		case membraneError := <-memerr:
			fmt.Println(errors.WithMessage(membraneError, "membrane error, exiting"))
		case <-term:
			fmt.Println("Shutting down services - exiting")
		}

		for _, f := range functions {
			if err = f.Stop(); err != nil {
				fmt.Println(f.Name(), " stop error ", err)
			}
		}

		_ = area.Stop()
		_ = logger.Stop()
		// Stop the membrane
		cobra.CheckErr(ls.Stop())
	},
	Args: cobra.ExactArgs(0),
}

func RootCommand() *cobra.Command {
	runCmd.Flags().StringVarP(&envFile, "env-file", "e", "", "--env-file config/.my-env")
	return runCmd
}
