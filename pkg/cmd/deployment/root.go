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

package deployment

import (
	"log"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"

	"github.com/nitrictech/cli/pkg/build"
	"github.com/nitrictech/cli/pkg/codeconfig"
	"github.com/nitrictech/cli/pkg/output"
	"github.com/nitrictech/cli/pkg/provider"
	"github.com/nitrictech/cli/pkg/provider/types"
	"github.com/nitrictech/cli/pkg/stack"
	"github.com/nitrictech/cli/pkg/target"
	"github.com/nitrictech/cli/pkg/tasklet"
)

var deploymentName string

var deploymentCmd = &cobra.Command{
	Use:   "deployment",
	Short: "Work with a deployment",
	Long: `Stack deployment command set.

The deployment commands generally need 3 things:
1. a target (either explicitly with "-t <targetname> or defined in the config)
2. a deployment name (either explicitly with -n <deployment name> or use the default name of "dep")
3. a stack definition, this automatically collected from the code in functions.
   A glob to the functions can be a supplied by:
  - Configuration - there are default globs for each supported language in the .nitiric-config.yaml
  - Aruments to the deployment actions.
	`,
	Example: `nitric deployment apply
nitric deployment delete
nitric deployment list
`,
}

var deploymentApplyCmd = &cobra.Command{
	Use:   "apply [handlerGlob]",
	Short: "Create or Update a new application deployment",
	Long:  `Applies a Nitric application deployment.`,
	Example: `# Configured default handlerGlob (stack in the current directory).
nitric deployment apply -t aws

# use an explicit handlerGlob (stack in the current directory)
nitric deployment apply -t aws "functions/*/*.go"

# use an explicit handlerGlob and explicit stack directory
nitric deployment apply -s ../projectX -t aws "functions/*/*.go"

# use a custom deployment name
nitric deployment apply -n prod -t aws`,
	Run: func(cmd *cobra.Command, args []string) {
		t, err := target.FromOptions()
		cobra.CheckErr(err)
		s, err := stack.FromOptions(args)
		cobra.CheckErr(err)

		log.SetOutput(output.NewPtermWriter(pterm.Debug))

		codeAsConfig := tasklet.Runner{
			StartMsg: "Gathering configuration from code..",
			Runner: func(_ output.Progress) error {
				s, err = codeconfig.Populate(s)
				return err
			},
			StopMsg: "Configuration gathered",
		}
		tasklet.MustRun(codeAsConfig, tasklet.Opts{})

		p, err := provider.NewProvider(s, t)
		cobra.CheckErr(err)

		buildImages := tasklet.Runner{
			StartMsg: "Building Images",
			Runner: func(_ output.Progress) error {
				return build.Create(s, t)
			},
			StopMsg: "Images built",
		}
		tasklet.MustRun(buildImages, tasklet.Opts{})

		tCtx, _ := tasklet.NewAreaContext()

		d := &types.Deployment{}
		deploy := tasklet.Runner{
			StartMsg:   "Deploying..",
			TaskletCtx: tCtx,
			Runner: func(progress output.Progress) error {
				d, err = p.Apply(progress, deploymentName)
				return err
			},
			StopMsg: "Stack",
		}
		tasklet.MustRun(deploy, tasklet.Opts{SuccessPrefix: "Deployed"})

		rows := [][]string{{"API", "Endpoint"}}
		for k, v := range d.ApiEndpoints {
			rows = append(rows, []string{k, v})
		}
		_ = pterm.DefaultTable.WithBoxed().WithData(rows).Render()
	},
	Args: cobra.MinimumNArgs(0),
}

var deploymentDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete an application deployment",
	Long:  `Delete a Nitric application deployment.`,
	Example: `nitric deployment delete
nitric deployment delete -s ../project/ -t prod
nitric deployment delete -n prod-aws -s ../project/ -t prod
`,
	Run: func(cmd *cobra.Command, args []string) {
		t, err := target.FromOptions()
		cobra.CheckErr(err)

		s, err := stack.FromOptionsMinimal()
		cobra.CheckErr(err)

		p, err := provider.NewProvider(s, t)
		cobra.CheckErr(err)

		tCtx, _ := tasklet.NewAreaContext()

		deploy := tasklet.Runner{
			StartMsg: "Deleting..",
			Runner: func(progress output.Progress) error {
				return p.Delete(progress, deploymentName)
			},
			TaskletCtx: tCtx,
			StopMsg:    "Deployment",
		}
		tasklet.MustRun(deploy, tasklet.Opts{
			SuccessPrefix: "Deleted",
		})
	},
	Args: cobra.ExactArgs(0),
}

var deploymentListCmd = &cobra.Command{
	Use:   "list",
	Short: "list deployments for a stack",
	Long:  `Lists Nitric application deployments for a stack.`,
	Example: `nitric list
nitric deployment list -s ../project/ -t prod
`,
	Run: func(cmd *cobra.Command, args []string) {
		t, err := target.FromOptions()
		cobra.CheckErr(err)

		s, err := stack.FromOptionsMinimal()
		cobra.CheckErr(err)

		p, err := provider.NewProvider(s, t)
		cobra.CheckErr(err)

		deps, err := p.List()
		cobra.CheckErr(err)

		output.Print(deps)
	},
	Args: cobra.ExactArgs(0),
}

func RootCommand() *cobra.Command {
	deploymentCmd.AddCommand(deploymentApplyCmd)
	deploymentApplyCmd.Flags().StringVarP(&deploymentName, "name", "n", "dep", "the name of the deployment")
	cobra.CheckErr(target.AddOptions(deploymentApplyCmd, false))
	stack.AddOptions(deploymentApplyCmd)

	deploymentCmd.AddCommand(deploymentDeleteCmd)
	deploymentDeleteCmd.Flags().StringVarP(&deploymentName, "name", "n", "dep", "the name of the deployment")
	cobra.CheckErr(target.AddOptions(deploymentDeleteCmd, false))
	stack.AddOptions(deploymentDeleteCmd)

	deploymentCmd.AddCommand(deploymentListCmd)
	stack.AddOptions(deploymentListCmd)
	cobra.CheckErr(target.AddOptions(deploymentListCmd, false))
	return deploymentCmd
}
