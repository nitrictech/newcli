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
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nitrictech/cli/pkg/paths"
	"github.com/nitrictech/cli/pkg/schemas"
	"github.com/nitrictech/cli/pkg/update"
	"github.com/nitrictech/cli/pkg/view/tui"
)

const usageTemplate = `Nitric - The fastest way to build serverless apps

To start with nitric, run the 'nitric new' command:

    $ nitric new

This will guide you through project creation, including selecting from available templates.

For further details visit our docs https://nitric.io/docs`

var CI bool

func usageString() string {
	return usageTemplate
}

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "nitric",
	Short: "CLI for Nitric applications",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// if output.VerboseLevel > 1 {
		// 	pterm.EnableDebugMessages()
		// }

		// if output.VerboseLevel == 0 {
		// 	pterm.Info.Debugger = true
		// }

		// Ensure the Nitric Home Directory Exists
		if _, err := os.Stat(paths.NitricHomeDir()); os.IsNotExist(err) {
			err := os.MkdirAll(paths.NitricHomeDir(), 0o700) // Create the Nitric Home Directory if it's missing
			if err != nil {
				tui.CheckErr(fmt.Errorf("Failed to create nitric home directory. %w", err))
			}
		}

		update.FetchLatestCLIVersion()
		update.FetchLatestProviderVersion()
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		update.PrintOutdatedCLIWarning()
		// an unstyled \n is always needed at the end of the view to ensure the last line renders
		fmt.Println()

		// Check/install schemas
		err := schemas.Install()
		if err != nil {
			tui.CheckErr(fmt.Errorf("Failed to create nitric schema. %w", err))
		}
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	defer func() {
		if err := recover(); err != nil {
			tui.Error.Printfln(
				"An unexpected error occurred:\n %s\n If you'd like to raise an issue in github https://github.com/nitrictech/cli/issues please include the above stack trace in the description",
				string(debug.Stack()),
			)
		}
	}()

	tui.CheckErr(rootCmd.Execute())
}

func init() {
	// rootCmd.PersistentFlags().IntVarP(&output.VerboseLevel, "verbose", "v", 1, "set the verbosity of output (larger is more verbose)")
	rootCmd.PersistentFlags().BoolVar(&CI, "ci", false, "CI mode, disable output styling and auto-confirm all operations")
	// rootCmd.PersistentFlags().VarP(output.OutputTypeFlag, "output", "o", "output format")

	// err := rootCmd.RegisterFlagCompletionFunc("output", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// 	return output.OutputTypeFlag.Allowed, cobra.ShellCompDirectiveDefault
	// })
	// tui.CheckErr(err)

	rootCmd.Long = usageString()
}

func addAlias(from, to string, commonCommand bool) {
	cmd, _, err := rootCmd.Find(strings.Split(from, " "))
	tui.CheckErr(err)

	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}

	cmd.Annotations["alias:to"] = to
	alias := &cobra.Command{
		Annotations: map[string]string{"alias:from": from},
		Use:         to,
		Short:       cmd.Short,
		Long:        cmd.Long,
		Example:     cmd.Example,
		Run: func(cmd *cobra.Command, args []string) {
			newArgs := []string{os.Args[0]}
			newArgs = append(newArgs, strings.Split(from, " ")...)
			newArgs = append(newArgs, args...)
			os.Args = newArgs
			tui.CheckErr(rootCmd.Execute())
		},
		DisableFlagParsing: true, // the real command will parse the flags
	}

	if commonCommand {
		alias.Annotations["commonCommand"] = "yes"
	}

	rootCmd.AddCommand(alias)
}

func AllCommandsUsage() []string {
	cmdH := []string{
		"",
		"Documentation for all available commands:",
		"",
	}
	cmdH = append(cmdH, cmdUsage([]string{}, rootCmd, false)...)

	return append(cmdH, "")
}

// cmdUsage returns the command usage for commonOnly commands or all.
// if all commands, then the aliases are group with the full command.
func cmdUsage(prefix []string, c *cobra.Command, commonOnly bool) []string {
	cmdH := []string{}
	use := strings.Join(append(prefix, c.Use), " ")

	add := true
	if _, ok := c.Annotations["commonCommand"]; commonOnly && !ok {
		add = false
	}

	if _, ok := c.Annotations["alias:from"]; !commonOnly && ok {
		add = false
	}

	if !c.HasParent() {
		add = false
	}

	if add {
		cmdH = append(cmdH, fmt.Sprintf("- %s : %s", use, c.Short))

		if _, ok := c.Annotations["alias:to"]; ok {
			use = "nitric " + c.Annotations["alias:to"]
			cmdH = append(cmdH, fmt.Sprintf("  (alias: %s)", use))
		}
	}

	for _, sc := range c.Commands() {
		cmdH = append(cmdH, cmdUsage(append(prefix, c.Use), sc, commonOnly)...)
	}

	return cmdH
}

// isNonInteractive returns true if the CLI is running in a non-interactive environment
func isNonInteractive() bool {
	return CI || !tui.IsTerminal()
}
