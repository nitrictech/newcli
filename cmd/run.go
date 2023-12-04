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
	"log"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"

	"github.com/nitrictech/cli/pkg/build"
	"github.com/nitrictech/cli/pkg/command"
	"github.com/nitrictech/cli/pkg/operations/local_run"
	"github.com/nitrictech/cli/pkg/output"
	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/utils"
)

var runNoBrowser bool

var runCmd = &cobra.Command{
	Use:         "run",
	Short:       "Run your project locally for development and testing",
	Long:        `Run your project locally for development and testing`,
	Example:     `nitric run`,
	Annotations: map[string]string{"commonCommand": "yes"},
	RunE: func(cmd *cobra.Command, args []string) error {
		// Divert default log output to pterm debug
		log.SetOutput(output.NewPtermWriter(pterm.Debug))
		log.SetFlags(0)

		if !utils.IsTerminal() && !output.CI {
			fmt.Println("")
			pterm.Warning.Println("non-interactive environment detected, switching to non-interactive mode")
			output.CI = true
		}

		if output.CI {
			return local_run.RunNonInteractive(runNoBrowser)
		}

		config, err := project.ConfigFromProjectPath("")
		cobra.CheckErr(err)

		proj, err := project.FromConfig(config)
		cobra.CheckErr(err)

		// if _, err := tea.NewProgram(local_run.New(context.TODO(), local_run.ModelArgs{
		// 	NoBrowser: runNoBrowser,
		// }), tea.WithAltScreen()).Run(); err != nil {
		// 	return err
		// }
		if _, err := tea.NewProgram(build.New(build.Args{
			Project: proj,
		}), tea.WithoutCatchPanics()).Run(); err != nil {
			return err
		}

		return nil
	},
	Args: cobra.ExactArgs(0),
}

func init() {
	runCmd.Flags().StringVarP(&envFile, "env-file", "e", "", "--env-file config/.my-env")
	runCmd.PersistentFlags().BoolVar(
		&runNoBrowser,
		"no-browser",
		false,
		"disable browser opening for local dashboard, note: in CI mode the browser opening feature is disabled",
	)
	rootCmd.AddCommand(command.AddDependencyCheck(runCmd, command.Docker))
}
