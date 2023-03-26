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

package project

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/joho/godotenv"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"

	"github.com/nitrictech/cli/pkg/build"
	"github.com/nitrictech/cli/pkg/codeconfig"
	"github.com/nitrictech/cli/pkg/command"
	"github.com/nitrictech/cli/pkg/output"
	"github.com/nitrictech/cli/pkg/preferences"
	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/provider"
	"github.com/nitrictech/cli/pkg/provider/types"
	"github.com/nitrictech/cli/pkg/stack"
	"github.com/nitrictech/cli/pkg/tasklet"
	"github.com/nitrictech/cli/pkg/utils"
)

var (
	confirmDown bool
	force       bool
	envFile     string
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
			cobra.CheckErr(err)

			// Set the default
			os.Setenv("PULUMI_CONFIG_PASSPHRASE_FILE", p)
		}
	},
}

var newStackCmd = &cobra.Command{
	Use:   "new",
	Short: "Create a new Nitric stack",
	Long:  `Creates a new Nitric stack.`,
	Run: func(cmd *cobra.Command, args []string) {
		err := newStack(cmd, args)
		cobra.CheckErr(err)
	},
	Args:        cobra.MaximumNArgs(2),
	Annotations: map[string]string{"commonCommand": "yes"},
}

func writeDigest(projectName string, stackName string, out output.Progress, summary *types.Summary) {
	out.Busyf("Writing deployment results")

	stacksDir, err := utils.NitricStacksDir()
	if err != nil {
		out.Failf("Error getting Nitric stack directory: %w", err)
		return
	}

	digestFile := path.Join(stacksDir, fmt.Sprintf("%s-%s.results.json", projectName, stackName))
	// TODO: Also look at writing to a unique build identifier for buils status history
	b, err := json.Marshal(summary)
	if err != nil {
		out.Failf("Error serializing deployment results: %w", err)
		return
	}

	err = os.WriteFile(digestFile, b, os.ModePerm)

	if err != nil {
		out.Failf("Error writing deployment results: %w", err)
	}

	out.Successf("build results written to: %s", digestFile)
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
			cobra.CheckErr(err)
			if confirm != "Yes" {
				pterm.Info.Println("You can run `nitric stack new` to create a new stack.")
				os.Exit(0)
			}
			err = newStack(cmd, args)
			cobra.CheckErr(err)

			s, err = stack.ConfigFromOptions()
			cobra.CheckErr(err)
		}

		config, err := project.ConfigFromProjectPath("")
		cobra.CheckErr(err)

		proj, err := project.FromConfig(config)
		cobra.CheckErr(err)

		log.SetOutput(output.NewPtermWriter(pterm.Debug))
		log.SetFlags(0)

		envFiles := utils.FilesExisting(".env", ".env.production", envFile)
		envMap := map[string]string{}
		if len(envFiles) > 0 {
			envMap, err = godotenv.Read(envFiles...)
			cobra.CheckErr(err)
		}

		// build base images on updates
		createBaseImage := tasklet.Runner{
			StartMsg: "Building Images",
			Runner: func(_ output.Progress) error {
				return build.BuildBaseImages(proj)
			},
			StopMsg: "Images Built",
		}
		tasklet.MustRun(createBaseImage, tasklet.Opts{})

		cc, err := codeconfig.New(proj, envMap)
		cobra.CheckErr(err)

		codeAsConfig := tasklet.Runner{
			StartMsg: "Gathering configuration from code..",
			Runner: func(_ output.Progress) error {
				return cc.Collect()
			},
			StopMsg: "Configuration gathered",
		}
		tasklet.MustRun(codeAsConfig, tasklet.Opts{})

		p, err := provider.ProviderFromFile(cc, s.Name, s.Provider, envMap, &types.ProviderOpts{Force: force})
		cobra.CheckErr(err)

		d := &types.Deployment{}
		deploy := tasklet.Runner{
			StartMsg: "Deploying..",
			Runner: func(progress output.Progress) error {
				d, err = p.Up(progress)
				// Write the digest regardless of deployment errors if available
				if d != nil {
					writeDigest(cc.ProjectName(), s.Name, progress, d.Summary)
				}

				return err
			},
			StopMsg: "Stack",
		}
		tasklet.MustRun(deploy, tasklet.Opts{SuccessPrefix: "Deployed"})

		// Print callable APIs if any were deployed
		if len(d.ApiEndpoints) > 0 {
			rows := [][]string{{"API", "Endpoint"}}
			for k, v := range d.ApiEndpoints {
				rows = append(rows, []string{k, v})
			}
			_ = pterm.DefaultTable.WithBoxed().WithData(rows).Render()
		}
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
nitric stack down -e aws -y`,
	Run: func(cmd *cobra.Command, args []string) {
		if !confirmDown {
			confirm := ""
			err := survey.AskOne(&survey.Select{
				Message: "Warning - This operation will destroy your stack, all deployed resources will be removed. Are you sure you want to proceed?",
				Default: "No",
				Options: []string{"Yes", "No"},
			}, &confirm)
			cobra.CheckErr(err)
			if confirm != "Yes" {
				pterm.Info.Println("Cancelling command")
				os.Exit(0)
			}
		}

		s, err := stack.ConfigFromOptions()
		cobra.CheckErr(err)

		log.SetOutput(output.NewPtermWriter(pterm.Debug))
		log.SetFlags(0)

		config, err := project.ConfigFromProjectPath("")
		cobra.CheckErr(err)

		proj, err := project.FromConfig(config)
		cobra.CheckErr(err)

		cc, err := codeconfig.New(proj, map[string]string{})
		cobra.CheckErr(err)

		p, err := provider.ProviderFromFile(cc, s.Name, s.Provider, map[string]string{}, &types.ProviderOpts{Force: true})
		cobra.CheckErr(err)

		deploy := tasklet.Runner{
			StartMsg: "Deleting..",
			Runner: func(progress output.Progress) error {
				sum, err := p.Down(progress)
				if sum != nil {
					writeDigest(proj.Name, s.Name, progress, sum)
				}
				return err
			},
			StopMsg: "Stack",
		}
		tasklet.MustRun(deploy, tasklet.Opts{
			SuccessPrefix: "Deleted",
		})
	},
	Args: cobra.ExactArgs(0),
}

var stackListCmd = &cobra.Command{
	Use:   "list [-s stack]",
	Short: "List all project stacks and their status",
	Long:  `List all project stacks and their status`,
	Example: `nitric stack list

nitric stack list -s aws
`,
	Run: func(cmd *cobra.Command, args []string) {
		s, err := stack.ConfigFromOptions()
		cobra.CheckErr(err)

		config, err := project.ConfigFromProjectPath("")
		cobra.CheckErr(err)

		proj, err := project.FromConfig(config)
		cobra.CheckErr(err)

		cc, err := codeconfig.New(proj, map[string]string{})
		cobra.CheckErr(err)

		p, err := provider.ProviderFromFile(cc, s.Name, s.Provider, map[string]string{}, &types.ProviderOpts{})
		cobra.CheckErr(err)

		deps, err := p.List()
		cobra.CheckErr(err)

		output.Print(deps)
	},
	Args:    cobra.ExactArgs(0),
	Aliases: []string{"ls"},
}

func RootCommand() *cobra.Command {
	stackCmd.AddCommand(newStackCmd)

	stackCmd.AddCommand(command.AddDependencyCheck(stackUpdateCmd, command.Pulumi, command.Docker))
	stackUpdateCmd.Flags().StringVarP(&envFile, "env-file", "e", "", "--env-file config/.my-env")
	stackUpdateCmd.Flags().BoolVarP(&force, "force", "f", false, "force override previous deployment")
	cobra.CheckErr(stack.AddOptions(stackUpdateCmd, false))

	stackCmd.AddCommand(command.AddDependencyCheck(stackDeleteCmd, command.Pulumi))
	stackDeleteCmd.Flags().BoolVarP(&confirmDown, "yes", "y", false, "confirm the destruction of the stack")
	cobra.CheckErr(stack.AddOptions(stackDeleteCmd, false))

	stackCmd.AddCommand(stackListCmd)
	cobra.CheckErr(stack.AddOptions(stackListCmd, false))

	return stackCmd
}

func newStack(cmd *cobra.Command, args []string) error {
	name := ""

	err := survey.AskOne(&survey.Input{
		Message: "What do you want to call your new stack?",
	}, &name)
	if err != nil {
		return err
	}

	pName := ""

	err = survey.AskOne(&survey.Select{
		Message: "Which Cloud do you wish to deploy to?",
		Default: types.Aws,
		Options: types.Providers,
	}, &pName)
	if err != nil {
		return err
	}

	pc, err := project.ConfigFromProjectPath("")
	if err != nil {
		return err
	}

	cc, err := codeconfig.New(project.New(pc.BaseConfig), map[string]string{})
	cobra.CheckErr(err)

	prov, err := provider.NewProvider(cc, name, pName, map[string]string{}, &types.ProviderOpts{})
	if err != nil {
		return err
	}

	return prov.AskAndSave()
}
