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
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/nitrictech/cli/pkg/collector"
	"github.com/nitrictech/cli/pkg/env"
	"github.com/nitrictech/cli/pkg/pflagx"
	"github.com/nitrictech/cli/pkg/preview"
	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/project/stack"
	"github.com/nitrictech/cli/pkg/provider"
	"github.com/nitrictech/cli/pkg/provider/pulumi"
	"github.com/nitrictech/cli/pkg/view/tui"
	"github.com/nitrictech/cli/pkg/view/tui/commands/build"
	stack_down "github.com/nitrictech/cli/pkg/view/tui/commands/stack/down"
	stack_new "github.com/nitrictech/cli/pkg/view/tui/commands/stack/new"
	stack_select "github.com/nitrictech/cli/pkg/view/tui/commands/stack/select"
	stack_up "github.com/nitrictech/cli/pkg/view/tui/commands/stack/up"
	"github.com/nitrictech/cli/pkg/view/tui/components/list"
	"github.com/nitrictech/cli/pkg/view/tui/components/view"
	"github.com/nitrictech/cli/pkg/view/tui/teax"
	deploymentspb "github.com/nitrictech/nitric/core/pkg/proto/deployments/v1"
)

var (
	stackFlag     string // stack flag value
	confirmDown   bool
	forceStack    bool
	forceNewStack bool
	envFile       string
)

var stackCmd = &cobra.Command{
	Use:   "stack",
	Short: "Manage stacks (the deployed app containing multiple resources e.g. services, buckets and topics)",
	Long: `Manage stacks (the deployed app containing multiple resources e.g. services, buckets and topics).

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
		// currPass := os.Getenv("PULUMI_CONFIG_PASSPHRASE")
		// currPassFile := os.Getenv("PULUMI_CONFIG_PASSPHRASE_FILE")
		// if currPass == "" && currPassFile == "" {
		// 	p, err := preferences.GetLocalPassPhraseFile()
		// 	// In non-CI environments we can generate the file to save a step.
		// 	// in CI environments this file would typically be lost, so it shouldn't auto-generate
		// 	if err != nil && !output.CI {
		// 		p, err = preferences.GenerateLocalPassPhraseFile()
		// 	}
		// 	if err != nil {
		// 		err = fmt.Errorf("unable to determine configured passphrase. See https://nitric.io/docs/guides/github-actions#configuring-environment-variables")
		// 	}
		// 	utils.CheckErr(err)

		// 	// Set the default
		// 	os.Setenv("PULUMI_CONFIG_PASSPHRASE_FILE", p)
		// }
	},
}

var newStackCmd = &cobra.Command{
	Use:   "new [stackName] [providerName]",
	Short: "Create a new Nitric stack",
	Long:  `Creates a new Nitric stack.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !tui.IsTerminal() {
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
		_, err := teax.NewProgram(stack_new.New(afero.NewOsFs(), stack_new.Args{
			StackName:    stackName,
			ProviderName: providerName,
			Force:        forceNewStack,
		})).Run()

		return err
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
		fs := afero.NewOsFs()

		stackFiles, err := stack.GetAllStackFiles(fs)
		tui.CheckErr(err)

		if len(stackFiles) == 0 {
			tui.CheckErr(fmt.Errorf("no stacks found in project, to create a new one run `nitric stack new`"))
		}

		// Step 0. Get the stack file, or prompt if more than 1.
		stackSelection := stackFlag

		if isNonInteractive() {
			if len(stackFiles) > 1 && stackSelection == "" {
				tui.CheckErr(fmt.Errorf("multiple stacks found in project, please specify one with -s"))
			}
		}

		if stackSelection == "" {
			if len(stackFiles) > 1 {
				stackList := make([]list.ListItem, len(stackFiles))

				for i, stackFile := range stackFiles {
					stackName, err := stack.GetStackNameFromFileName(stackFile)
					tui.CheckErr(err)
					stackConfig, err := stack.ConfigFromName[map[string]any](fs, stackName)
					tui.CheckErr(err)
					stackList[i] = stack_select.StackListItem{
						Name:     stackConfig.Name,
						Provider: stackConfig.Provider,
					}
				}

				promptModel := stack_select.New(stack_select.Args{
					Prompt:    "Which stack would you like to update?",
					StackList: stackList,
				})

				selection, err := teax.NewProgram(promptModel).Run()
				tui.CheckErr(err)
				stackSelection = selection.(stack_select.Model).Choice()
				if stackSelection == "" {
					return
				}
			} else {
				stackSelection, err = stack.GetStackNameFromFileName(stackFiles[0])
				tui.CheckErr(err)
			}
		}

		stackConfig, err := stack.ConfigFromName[map[string]any](fs, stackSelection)
		tui.CheckErr(err)

		if !isNonInteractive() {
			_ = pulumi.EnsurePulumiPassphrase(fs)
		}

		proj, err := project.FromFile(fs, "")
		tui.CheckErr(err)

		// Step 0a. Locate/Download provider where applicable.
		prov, err := provider.NewProvider(stackConfig.Provider, proj, fs)
		tui.CheckErr(err)

		err = prov.Install()
		tui.CheckErr(err)

		// Build the Project's Services (Containers)
		buildUpdates, err := proj.BuildServices(fs)
		tui.CheckErr(err)

		if isNonInteractive() {
			fmt.Println("building project services")
			for _, service := range proj.GetServices() {
				fmt.Printf("service matched '%s', auto-naming this service '%s'\n", service.GetFilePath(), service.Name)
			}

			// non-interactive environment
			for update := range buildUpdates {
				for _, line := range strings.Split(strings.TrimSuffix(update.Message, "\n"), "\n") {
					fmt.Printf("%s [%s]: %s\n", update.ServiceName, update.Status, line)
				}
			}
		} else {
			prog := teax.NewProgram(build.NewModel(buildUpdates, "Building Services"))
			// blocks but quits once the above updates channel is closed by the build process
			buildModel, err := prog.Run()
			tui.CheckErr(err)
			if buildModel.(build.Model).Err != nil {
				tui.CheckErr(fmt.Errorf("error building services"))
			}
		}

		// Step 2. Start the collectors and containers (respectively in pairs)
		// Step 3. Merge requirements from collectors into a specification
		serviceRequirements, err := proj.CollectServicesRequirements()
		tui.CheckErr(err)

		batchRequirements, err := proj.CollectBatchRequirements()
		tui.CheckErr(err)

		additionalEnvFiles := []string{}

		if envFile != "" {
			additionalEnvFiles = append(additionalEnvFiles, envFile)
		}

		envVariables, err := env.ReadLocalEnv(additionalEnvFiles...)
		if err != nil && os.IsNotExist(err) {
			if !os.IsNotExist(err) {
				tui.CheckErr(err)
			}
			// If it doesn't exist set blank
			envVariables = map[string]string{}
		}

		// Allow Beta providers to be run if 'beta-providers' is enabled in preview flags
		if slices.Contains(proj.Preview, preview.Feature_BetaProviders) {
			envVariables["NITRIC_BETA_PROVIDERS"] = "true"
		}

		migrationImageContexts, err := collector.GetMigrationImageBuildContexts(serviceRequirements, fs)
		tui.CheckErr(err)
		// Build images from contexts and provide updates on the builds

		if len(migrationImageContexts) > 0 {
			migrationBuildUpdates, err := project.BuildMigrationImages(fs, migrationImageContexts)
			tui.CheckErr(err)

			if isNonInteractive() {
				fmt.Println("building project migration images")
				// non-interactive environment
				for update := range migrationBuildUpdates {
					for _, line := range strings.Split(strings.TrimSuffix(update.Message, "\n"), "\n") {
						fmt.Printf("%s [%s]: %s\n", update.ServiceName, update.Status, line)
					}
				}
			} else {
				prog := teax.NewProgram(build.NewModel(migrationBuildUpdates, "Building Database Migrations"))
				// blocks but quits once the above updates channel is closed by the build process
				buildModel, err := prog.Run()
				tui.CheckErr(err)
				if buildModel.(build.Model).Err != nil {
					tui.CheckErr(fmt.Errorf("error building services"))
				}
			}
		}

		spec, err := collector.ServiceRequirementsToSpec(proj.Name, envVariables, serviceRequirements, batchRequirements)
		tui.CheckErr(err)

		providerStdout := make(chan string)

		// Step 4. Start the deployment provider server
		providerAddress, err := prov.Start(&provider.StartOptions{
			Env:    envVariables,
			StdOut: providerStdout,
			StdErr: providerStdout,
		})
		tui.CheckErr(err)
		defer func() {
			err := prov.Stop()
			tui.CheckErr(err)
		}()

		// Step 5a. Send specification to provider for deployment
		deploymentClient := provider.NewDeploymentClient(providerAddress, true)

		attributes := map[string]interface{}{}

		attributes["stack"] = stackConfig.Name
		attributes["project"] = proj.Name

		for k, v := range stackConfig.Config {
			attributes[k] = v
		}

		attributesStruct, err := structpb.NewStruct(attributes)
		tui.CheckErr(err)

		eventChan, errorChan := deploymentClient.Up(&deploymentspb.DeploymentUpRequest{
			Spec:        spec,
			Attributes:  attributesStruct,
			Interactive: true,
		})

		// Step 5b. Communicate with server to share progress of ...
		if isNonInteractive() {
			fmt.Printf("Deploying %s stack with provider %s\n", stackConfig.Name, stackConfig.Provider)
			go func() {
				for update := range errorChan {
					fmt.Printf("Error: %s\n", update)
				}
			}()

			go func() {
				for outMessage := range providerStdout {
					fmt.Printf("%s: %s\n", stackConfig.Provider, outMessage)
				}
			}()

			// non-interactive environment
			for update := range eventChan {
				switch content := update.Content.(type) {
				case *deploymentspb.DeploymentUpEvent_Message:
					fmt.Printf("%s\n", content.Message)
				case *deploymentspb.DeploymentUpEvent_Update:
					updateResType := ""
					updateResName := ""
					if content.Update.Id != nil {
						updateResType = content.Update.Id.Type.String()
						updateResName = content.Update.Id.Name
					}

					if updateResType == "" {
						updateResType = "Stack"
					}
					if updateResName == "" {
						updateResName = stackConfig.Name
					}
					if content.Update.SubResource != "" {
						updateResName = fmt.Sprintf("%s:%s", updateResName, content.Update.SubResource)
					}

					fmt.Printf("%s:%s [%s]:%s %s\n", updateResType, updateResName, content.Update.Action, content.Update.Status, content.Update.Message)
				case *deploymentspb.DeploymentUpEvent_Result:
					fmt.Printf("\nResult: %s\n", content.Result.GetText())
				}
			}
		} else {
			// interactive environment
			// Step 5c. Start the stack up view
			stackUp := stack_up.New(stackConfig.Provider, stackConfig.Name, eventChan, providerStdout, errorChan)
			_, err = teax.NewProgram(stackUp).Run()
			tui.CheckErr(err)
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
nitric stack down -s aws -y`,
	Run: func(cmd *cobra.Command, args []string) {
		fs := afero.NewOsFs()

		stackFiles, err := stack.GetAllStackFiles(fs)
		tui.CheckErr(err)

		if len(stackFiles) == 0 {
			tui.CheckErr(fmt.Errorf("no stacks found in project root, to create a new one run `nitric stack new`"))
		}

		// Step 0. Get the stack file, or prompt if more than 1.
		stackSelection := stackFlag

		if isNonInteractive() {
			if len(stackFiles) > 1 && stackSelection == "" {
				tui.CheckErr(fmt.Errorf("multiple stacks found in project, please specify one with -s"))
			}
		}

		if stackSelection == "" {
			if len(stackFiles) > 1 {
				stackList := make([]list.ListItem, len(stackFiles))

				for i, stackFile := range stackFiles {
					stackName, err := stack.GetStackNameFromFileName(stackFile)
					tui.CheckErr(err)
					stackConfig, err := stack.ConfigFromName[map[string]any](fs, stackName)
					tui.CheckErr(err)
					stackList[i] = stack_select.StackListItem{
						Name:     stackConfig.Name,
						Provider: stackConfig.Provider,
					}
				}

				promptModel := stack_select.New(stack_select.Args{
					Prompt:    "Which stack would you like to delete?",
					StackList: stackList,
				})

				selection, err := teax.NewProgram(promptModel).Run()
				tui.CheckErr(err)
				stackSelection = selection.(stack_select.Model).Choice()
				if stackSelection == "" {
					return
				}
			} else {
				stackSelection, err = stack.GetStackNameFromFileName(stackFiles[0])
				tui.CheckErr(err)
			}
		}

		stackConfig, err := stack.ConfigFromName[map[string]any](fs, stackSelection)
		tui.CheckErr(err)

		if !isNonInteractive() {
			_ = pulumi.EnsurePulumiPassphrase(fs)
		}

		proj, err := project.FromFile(fs, "")
		tui.CheckErr(err)

		// Step 0a. Locate/Download provider where applicable.
		prov, err := provider.NewProvider(stackConfig.Provider, proj, fs)
		tui.CheckErr(err)

		err = prov.Install()
		tui.CheckErr(err)

		providerStdout := make(chan string)

		additionalEnvFiles := []string{}
		if envFile != "" {
			additionalEnvFiles = append(additionalEnvFiles, envFile)
		}

		envVariables, err := env.ReadLocalEnv(additionalEnvFiles...)
		if err != nil && os.IsNotExist(err) {
			if !os.IsNotExist(err) {
				tui.CheckErr(err)
			}
			// If it doesn't exist set blank
			envVariables = map[string]string{}
		}

		// Allow Beta providers to be run if 'beta-providers' is enabled in preview flags
		if slices.Contains(proj.Preview, preview.Feature_BetaProviders) {
			envVariables["NITRIC_BETA_PROVIDERS"] = "true"
		}

		// Step 4. Start the deployment provider server
		providerAddress, err := prov.Start(&provider.StartOptions{
			Env:    envVariables,
			StdOut: providerStdout,
			StdErr: providerStdout,
		})
		tui.CheckErr(err)

		defer func() {
			err = prov.Stop()
			tui.CheckErr(err)
		}()

		// Step 5a. Send specification to provider for deployment
		deploymentClient := provider.NewDeploymentClient(providerAddress, true)

		attributes := map[string]interface{}{}

		attributes["stack"] = stackConfig.Name
		attributes["project"] = proj.Name

		for k, v := range stackConfig.Config {
			attributes[k] = v
		}

		attributesStruct, err := structpb.NewStruct(attributes)
		tui.CheckErr(err)

		eventChannel, errorChan := deploymentClient.Down(&deploymentspb.DeploymentDownRequest{
			Attributes:  attributesStruct,
			Interactive: true,
		})

		if isNonInteractive() {
			fmt.Printf("Deploying %s stack with provider %s\n", stackConfig.Name, stackConfig.Provider)
			go func() {
				for update := range errorChan {
					fmt.Printf("Error: %s\n", update)
				}
			}()

			go func() {
				for outMessage := range providerStdout {
					fmt.Printf("%s: %s\n", stackConfig.Provider, outMessage)
				}
			}()

			// non-interactive environment
			for update := range eventChannel {
				switch content := update.Content.(type) {
				case *deploymentspb.DeploymentDownEvent_Message:
					fmt.Printf("%s\n", content.Message)
				case *deploymentspb.DeploymentDownEvent_Update:
					updateResType := ""
					updateResName := ""
					if content.Update.Id != nil {
						updateResType = content.Update.Id.Type.String()
						updateResName = content.Update.Id.Name
					}

					if updateResType == "" {
						updateResType = "Stack"
					}
					if updateResName == "" {
						updateResName = stackConfig.Name
					}
					if content.Update.SubResource != "" {
						updateResName = fmt.Sprintf("%s:%s", updateResName, content.Update.SubResource)
					}

					fmt.Printf("%s:%s [%s]:%s %s\n", updateResType, updateResName, content.Update.Action, content.Update.Status, content.Update.Message)
				case *deploymentspb.DeploymentDownEvent_Result:
					fmt.Println("\nStack down complete")
				}
			}
		} else {
			stackDown := stack_down.New(stackConfig.Provider, stackConfig.Name, eventChannel, providerStdout, errorChan)

			_, err = teax.NewProgram(stackDown).Run()
			tui.CheckErr(err)
		}
	},
	Args: cobra.ExactArgs(0),
}

var stackListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all stacks in the project",
	Long:  `List all stacks in the project`,
	Run: func(cmd *cobra.Command, args []string) {
		fs := afero.NewOsFs()

		stackFiles, err := stack.GetAllStackFiles(fs)
		tui.CheckErr(err)

		if len(stackFiles) == 0 {
			// no stack files found
			// print error with suggestion for user to run stack new
			tui.CheckErr(fmt.Errorf("no stacks found in project root, to create a new one run `nitric stack new`"))
		}

		nameLength := 4 // start with the width of the column heading "name".
		for _, stackFile := range stackFiles {
			stackName, err := stack.GetStackNameFromFileName(stackFile)
			tui.CheckErr(err)

			if len(stackName) > nameLength {
				nameLength = len(stackName)
			}
		}

		nameStyle := lipgloss.NewStyle().Bold(true).Foreground(tui.Colors.Blue).Width(nameLength + 1).PaddingRight(1).BorderRight(true).BorderStyle(lipgloss.NormalBorder()).BorderForeground(tui.Colors.Gray)
		providerStyle := lipgloss.NewStyle().Foreground(tui.Colors.Purple).PaddingLeft(1)

		v := view.New()
		v.Break()
		v.Add("name").WithStyle(nameStyle)
		v.Addln("provider").WithStyle(providerStyle)
		v.Break()
		for _, stackFile := range stackFiles {
			stackName, err := stack.GetStackNameFromFileName(stackFile)
			tui.CheckErr(err)

			stackConfig, err := stack.ConfigFromName[map[string]any](fs, stackName)
			tui.CheckErr(err)

			v.Add(stackConfig.Name).WithStyle(nameStyle)
			v.Addln(stackConfig.Provider).WithStyle(providerStyle)
		}
		fmt.Println(v.Render())
	},
}

func AddOptions(cmd *cobra.Command, providerOnly bool) error {
	fs := afero.NewOsFs()

	stacks, err := stack.GetAllStackNames(fs)
	if err != nil {
		return fmt.Errorf("failed to get stacks available for this project. %w", err)
	}

	cmd.Flags().VarP(pflagx.NewStringEnumVar(&stackFlag, stacks, ""), "stack", "s", "specify a stack file, -s your_stack")

	return cmd.RegisterFlagCompletionFunc("stack", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return stacks, cobra.ShellCompDirectiveDefault
	})
}

func init() {
	// New Stack
	stackCmd.AddCommand(newStackCmd)
	newStackCmd.Flags().BoolVarP(&forceNewStack, "force", "f", false, "force stack creation.")

	// Update Stack (Up)
	stackCmd.AddCommand(tui.AddDependencyCheck(stackUpdateCmd, tui.Pulumi, tui.Docker, tui.DockerBuildx))
	stackUpdateCmd.Flags().StringVarP(&envFile, "env-file", "e", "", "--env-file config/.my-env")
	stackUpdateCmd.Flags().BoolVarP(&forceStack, "force", "f", false, "force override previous deployment")
	tui.CheckErr(AddOptions(stackUpdateCmd, false))

	// Delete Stack (Down)
	stackCmd.AddCommand(tui.AddDependencyCheck(stackDeleteCmd, tui.Pulumi))
	stackDeleteCmd.Flags().BoolVarP(&confirmDown, "yes", "y", false, "confirm the destruction of the stack")
	tui.CheckErr(AddOptions(stackDeleteCmd, false))

	// List Stacks
	stackCmd.AddCommand(stackListCmd)

	// Add Stack Commands
	rootCmd.AddCommand(stackCmd)

	addAlias("stack update", "up", true)
	addAlias("stack down", "down", true)
}
