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

package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/samber/lo"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"github.com/nitrictech/cli/pkg/cloud"
	"github.com/nitrictech/cli/pkg/cloud/gateway"
	"github.com/nitrictech/cli/pkg/dashboard"
	docker "github.com/nitrictech/cli/pkg/docker"
	"github.com/nitrictech/cli/pkg/env"
	"github.com/nitrictech/cli/pkg/paths"
	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/system"
	"github.com/nitrictech/cli/pkg/view/tui"
	"github.com/nitrictech/cli/pkg/view/tui/commands/build"
	"github.com/nitrictech/cli/pkg/view/tui/commands/local"
	"github.com/nitrictech/cli/pkg/view/tui/commands/services"
	"github.com/nitrictech/cli/pkg/view/tui/teax"
)

var runNoBrowser bool

var runCmd = &cobra.Command{
	Use:         "run",
	Short:       "Run your project locally for development and testing",
	Long:        `Run your project locally for development and testing`,
	Example:     `nitric run`,
	Annotations: map[string]string{"commonCommand": "yes"},
	RunE: func(cmd *cobra.Command, args []string) error {
		err := docker.VerifyDockerIsAvailable()
		tui.CheckErr(err)

		fs := afero.NewOsFs()

		proj, err := project.FromFile(fs, "")
		tui.CheckErr(err)

		additionalEnvFiles := []string{}

		if envFile != "" {
			additionalEnvFiles = append(additionalEnvFiles, envFile)
		}

		loadEnv, err := env.ReadLocalEnv(additionalEnvFiles...)
		if err != nil && !os.IsNotExist(err) {
			tui.CheckErr(err)
		}

		var tlsCredentials *gateway.TLSCredentials
		if enableHttps {
			createTlsCredentialsIfNotPresent(fs, proj.Directory)
			tlsCredentials = &gateway.TLSCredentials{
				CertFile: paths.NitricTlsCertFile(proj.Directory),
				KeyFile:  paths.NitricTlsKeyFile(proj.Directory),
			}
		}

		logFilePath, err := paths.NewNitricLogFile(proj.Directory)
		tui.CheckErr(err)

		logWriter, err := fs.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		tui.CheckErr(err)
		defer logWriter.Close()

		teaOptions := []tea.ProgramOption{}
		if isNonInteractive() {
			teaOptions = append(teaOptions, tea.WithoutRenderer(), tea.WithInput(nil))
		}

		runView := teax.NewProgram(local.NewLocalCloudStartModel(isNonInteractive()), teaOptions...)

		var localCloud *cloud.LocalCloud
		go func() {
			// Start the local cloud service analogues
			localCloud, err = cloud.New(proj.Name, cloud.LocalCloudOptions{
				TLSCredentials: tlsCredentials,
				LogWriter:      logWriter,
				LocalConfig:    proj.LocalConfig,
			}, project.BuildAndRunMigrations)
			tui.CheckErr(err)
			runView.Send(local.LocalCloudStartStatusMsg{Status: local.Done})
		}()

		_, err = runView.Run()
		tui.CheckErr(err)

		// Start dashboard
		dash, err := dashboard.New(startNoBrowser, localCloud, proj)
		tui.CheckErr(err)

		err = dash.Start()
		tui.CheckErr(err)

		updates, err := proj.BuildServices(fs)
		tui.CheckErr(err)

		prog := teax.NewProgram(build.NewModel(updates, "Building Services"))
		// blocks but quits once the above updates channel is closed by the build process
		_, err = prog.Run()
		tui.CheckErr(err)

		// Run the app code (project services)
		stopChan := make(chan bool)
		updatesChan := make(chan project.ServiceRunUpdate)

		// panic recovery for local cloud
		// gracefully stop the local cloud in the case of a panic
		defer func() {
			if r := recover(); r != nil {
				localCloud.Stop()
			}
		}()

		go func() {
			err := proj.RunServices(localCloud, stopChan, updatesChan, loadEnv)
			if err != nil {
				localCloud.Stop()

				tui.CheckErr(err)
			}
		}()

		// FIXME: This is a hack to get labelled logs into the TUI
		// We should refactor the system logs to be more generic
		systemChan := make(chan project.ServiceRunUpdate)
		system.SubscribeToLogs(func(msg string) {
			systemChan <- project.ServiceRunUpdate{
				ServiceName: "nitric",
				Label:       "nitric",
				Status:      project.ServiceRunStatus_Running,
				Message:     msg,
			}
		})

		allUpdates := lo.FanIn(10, updatesChan, systemChan)

		// non-interactive environment
		if isNonInteractive() {
			go func() {
				sigChan := make(chan os.Signal, 1)
				signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

				// Wait for a signal
				<-sigChan

				fmt.Println("Stopping local cloud")

				localCloud.Stop()

				// Send stop signal to stopChan
				close(stopChan)
			}()

			for {
				select {
				case update := <-allUpdates:
					fmt.Printf("%s [%s]: %s", update.ServiceName, update.Status, update.Message)
				case <-stopChan:
					fmt.Println("Shutting down services - exiting")
					return nil
				}
			}
		} else {
			runView := teax.NewProgram(services.NewModel(stopChan, allUpdates, localCloud, dash.GetDashboardUrl()))

			_, _ = runView.Run()

			localCloud.Stop()
		}

		return nil
	},
	Args: cobra.ExactArgs(0),
}

func init() {
	runCmd.Flags().StringVarP(&envFile, "env-file", "e", "", "--env-file config/.my-env")
	runCmd.Flags().BoolVar(&enableHttps, "https-preview", false, "enable https support for local APIs (preview feature)")
	runCmd.PersistentFlags().BoolVar(
		&runNoBrowser,
		"no-browser",
		false,
		"disable browser opening for local dashboard, note: in CI mode the browser opening feature is disabled",
	)
	rootCmd.AddCommand(tui.AddDependencyCheck(runCmd, tui.Docker, tui.DockerBuildx))
}
