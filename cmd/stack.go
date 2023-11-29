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
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"

	"github.com/nitrictech/cli/pkg/command"
	"github.com/nitrictech/cli/pkg/operations/stack_delete"
	"github.com/nitrictech/cli/pkg/operations/stack_new"
	"github.com/nitrictech/cli/pkg/operations/stack_preview"
	"github.com/nitrictech/cli/pkg/operations/stack_update"
	"github.com/nitrictech/cli/pkg/output"
	"github.com/nitrictech/cli/pkg/preferences"
	"github.com/nitrictech/cli/pkg/stack"
	"github.com/nitrictech/cli/pkg/utils"
)

var (
	confirmDown   bool
	forceStack    bool
	forceNewStack bool
	envFile       string
)

var stackCmd = &cobra.Command{
	Use:   "stack",
	Short: "Manage stacks (the deployed app containing multiple resources e.g. collection, bucket, topic)",
	Long: `Manage stacks (the deployed app containing multiple resources e.g. collection, bucket, topic).

A stack is a named update target, and a single project may have many of them.`,
	Example: `nitric stack up
nitric stack down
nitric stack list
`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if cmd.Root().PersistentPreRun != nil {
			cmd.Root().PersistentPreRun(cmd, args)
		}

		// Respect existing pulumi configuration if one already exists
		currPass := os.Getenv("PULUMI_CONFIG_PASSPHRASE")
		currPassFile := os.Getenv("PULUMI_CONFIG_PASSPHRASE_FILE")
		if currPass == "" && currPassFile == "" {
			p, err := preferences.GetLocalPassPhraseFile()
			// In non-CI environments we can generate the file to save a step.
			// in CI environments this file would typically be lost, so it shouldn't auto-generate
			if err != nil && !output.CI {
				p, err = preferences.GenerateLocalPassPhraseFile()
			}
			if err != nil {
				err = fmt.Errorf("unable to determine configured passphrase. See https://nitric.io/docs/guides/github-actions#configuring-environment-variables")
			}
			utils.CheckErr(err)

			// Set the default
			os.Setenv("PULUMI_CONFIG_PASSPHRASE_FILE", p)
		}
	},
}

var newStackCmd = &cobra.Command{
	Use:   "new [stackName] [providerName]",
	Short: "Create a new Nitric stack",
	Long:  `Creates a new Nitric stack.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !utils.IsTerminal() {
			return fmt.Errorf("the stack new command does not support non-interactive environments")
		}

		stackName := ""
		if len(args) >= 1 {
			stackName = args[0]
		}

		providerName := ""
		if len(args) >= 2 {
			providerName = args[1]
		}

		return stack_new.Run(stack_new.Args{
			StackName:    stackName,
			ProviderName: providerName,
			Force:        forceNewStack,
		})
	},
	Args:        cobra.MaximumNArgs(2),
	Annotations: map[string]string{"commonCommand": "yes"},
}

var stackUpdateCmd = &cobra.Command{
	Use:     "update [-s stack]",
	Short:   "Create or update a deployed stack",
	Long:    `Create or update a deployed stack`,
	Example: `nitric stack update -s aws`,
	Run: func(cmd *cobra.Command, args []string) {
		s, err := stack.ConfigFromOptions()

		if err != nil && strings.Contains(err.Error(), "No nitric stacks found") {
			confirm := ""
			err = survey.AskOne(&survey.Select{
				Message: "A stack is required to deploy your project, create one now?",
				Default: "Yes",
				Options: []string{"Yes", "No"},
			}, &confirm)
			utils.CheckErr(err)

			if confirm != "Yes" {
				pterm.Info.Println("You can run `nitric stack new` to create a new stack.")
				os.Exit(0)
			}

			err = stack_new.Run(stack_new.Args{})
			utils.CheckErr(err)

			_, err = stack.ConfigFromOptions()
			utils.CheckErr(err)
		}

		if !utils.IsTerminal() && !output.CI {
			fmt.Println("")
			pterm.Warning.Println("non-interactive environment detected, switching to non-interactive mode.")
			output.CI = true
		}

		stack_update.Run(stack_update.Args{
			EnvFile:     envFile,
			Stack:       s,
			Force:       forceStack,
			Interactive: !output.CI,
		})
	},
	Args:    cobra.MinimumNArgs(0),
	Aliases: []string{"up"},
}

var stackDeleteCmd = &cobra.Command{
	Use:   "down [-s stack]",
	Short: "Undeploy a previously deployed stack, deleting resources",
	Long:  `Undeploy a previously deployed stack, deleting resources`,
	Example: `nitric stack down -s aws

# To not be prompted, use -y
nitric stack down -s aws -y`,
	Run: func(cmd *cobra.Command, args []string) {
		// Check a stack exists
		s, err := stack.ConfigFromOptions()

		if err != nil && strings.Contains(err.Error(), "No nitric stacks found") {
			pterm.Info.Println("No stack was found. Have you previously deployed this project using Nitric?")
			os.Exit(0)
		}

		if !confirmDown && !output.CI {
			confirm := ""
			err := survey.AskOne(&survey.Select{
				Message: "Warning - This operation will destroy your stack and all resources, it cannot be undone. Continue?",
				Default: "No",
				Options: []string{"Yes", "No"},
			}, &confirm)
			utils.CheckErr(err)

			if confirm != "Yes" {
				pterm.Info.Println("Cancelling command")
				os.Exit(0)
			}
		}

		if !utils.IsTerminal() && !output.CI {
			fmt.Println("")
			pterm.Warning.Println("non-interactive environment detected, switching to non-interactive mode.")
			output.CI = true
		}

		stack_delete.Run(stack_delete.Args{
			Stack:       s,
			Interactive: !output.CI,
		})
	},
	Args: cobra.ExactArgs(0),
}

var stackPreviewCommand = &cobra.Command{
	Use:     "preview [-s stack]",
	Short:   "Preview the deployment of a stack",
	Long:    `Preview the deployment of a stack`,
	Example: `nitric stack preview -s aws`,
	Run: func(cmd *cobra.Command, args []string) {
		s, err := stack.ConfigFromOptions()

		if err != nil && strings.Contains(err.Error(), "No nitric stacks found") {
			confirm := ""
			err = survey.AskOne(&survey.Select{
				Message: "A stack is required to deploy your project, create one now?",
				Default: "Yes",
				Options: []string{"Yes", "No"},
			}, &confirm)
			utils.CheckErr(err)

			if confirm != "Yes" {
				pterm.Info.Println("You can run `nitric stack new` to create a new stack.")
				os.Exit(0)
			}

			err = stack_new.Run(stack_new.Args{})
			utils.CheckErr(err)

			_, err = stack.ConfigFromOptions()
			utils.CheckErr(err)
		}

		stack_preview.Run(stack_preview.Args{
			EnvFile:     envFile,
			Stack:       s,
			Force:       forceStack,
			Interactive: !output.CI,
		})
	},
	Args:    cobra.ExactArgs(0),
	Aliases: []string{"preview"},
}

func init() {
	stackCmd.AddCommand(newStackCmd)
	newStackCmd.Flags().BoolVarP(&forceNewStack, "force", "f", false, "force stack creation.")

	stackCmd.AddCommand(command.AddDependencyCheck(stackUpdateCmd, command.Pulumi, command.Docker))
	stackUpdateCmd.Flags().StringVarP(&envFile, "env-file", "e", "", "--env-file config/.my-env")
	stackUpdateCmd.Flags().BoolVarP(&forceStack, "force", "f", false, "force override previous deployment")
	utils.CheckErr(stack.AddOptions(stackUpdateCmd, false))

	stackCmd.AddCommand(command.AddDependencyCheck(stackPreviewCommand, command.Pulumi, command.Docker))
	stackPreviewCommand.Flags().StringVarP(&envFile, "env-file", "e", "", "--env-file config/.my-env")
	stackPreviewCommand.Flags().BoolVarP(&forceStack, "force", "f", false, "force override previous deployment")
	utils.CheckErr(stack.AddOptions(stackPreviewCommand, false))

	stackCmd.AddCommand(command.AddDependencyCheck(stackDeleteCmd, command.Pulumi))
	stackDeleteCmd.Flags().BoolVarP(&confirmDown, "yes", "y", false, "confirm the destruction of the stack")
	utils.CheckErr(stack.AddOptions(stackDeleteCmd, false))

	rootCmd.AddCommand(stackCmd)

	addAlias("stack update", "up", true)
	addAlias("stack down", "down", true)
	addAlias("stack preview", "preview", true)
}
