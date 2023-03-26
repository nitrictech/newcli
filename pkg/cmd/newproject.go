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
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"

	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/templates"
)

var (
	force         bool
	nameRegex     = regexp.MustCompile(`^([a-zA-Z0-9-])*$`)
	projectNameQu = survey.Question{
		Name:     "projectName",
		Prompt:   &survey.Input{Message: "What is the name of the project?"},
		Validate: validateName,
	}
	templateNameQu = survey.Question{
		Name: "templateName",
	}
)

var newProjectCmd = &cobra.Command{
	Use:   "new [projectName] [templateName]",
	Short: "Create a new project",
	Long:  `Creates a new Nitric project from a template.`,
	Example: `# For an interactive command that will ask the required questions
nitric new

# For a non-interactive command use the arguments.
nitric new hello-world "official/TypeScript - Starter" `,
	Run: func(cmd *cobra.Command, args []string) {
		answers := struct {
			ProjectName  string
			TemplateName string
			FeedbackName string
			Handlers     string
		}{}

		downloadr := templates.NewDownloader()
		dirs, err := downloadr.Names()
		cobra.CheckErr(err)

		templateNameQu.Prompt = &survey.Select{
			Message: "Choose a template:",
			Options: dirs,
		}
		templateNameQu.Validate = func(ans interface{}) error {
			if len(args) < 2 {
				return nil
			}

			a, ok := ans.(string)
			if !ok {
				return errors.New("wrong type, need a string")
			}

			if downloadr.Get(a) == nil {
				return fmt.Errorf("%s not in %v", a, dirs)
			}

			return nil
		}

		qs := []*survey.Question{}

		if len(args) > 0 {
			if err := projectNameQu.Validate(args[0]); err != nil {
				pterm.Error.PrintOnError(err)
				qs = append(qs, &projectNameQu)
			} else {
				answers.ProjectName = args[0]
			}
		} else {
			qs = append(qs, &projectNameQu)
		}

		if len(args) > 1 {
			if err := templateNameQu.Validate(args[1]); err != nil {
				pterm.Error.PrintOnError(err)
				qs = append(qs, &templateNameQu)
			} else {
				answers.TemplateName = args[1]
			}
		} else {
			qs = append(qs, &templateNameQu)
			args = []string{} // reassign args to ensure validation works correctly.
		}

		if len(qs) > 0 {
			err = survey.Ask(qs, &answers)
			cobra.CheckErr(err)
		}

		cd, err := filepath.Abs(".")
		cobra.CheckErr(err)

		projDir := path.Join(cd, answers.ProjectName)

		err = downloadr.DownloadDirectoryContents(answers.TemplateName, projDir, force)
		cobra.CheckErr(err)

		var p *project.Config
		// Check if the downloaded template has a default nitric.yaml file
		if _, err := os.Stat(filepath.Join(projDir, "nitric.yaml")); errors.Is(err, os.ErrNotExist) {
			// Old template detected, without nitric.yaml file - prompt for glob pattern for backwards compatibility
			globQ := []*survey.Question{
				{
					Name: "handlers",
					Prompt: &survey.Input{
						Message: "Glob for the function handlers?",
						Default: "functions/*.ts",
						Suggest: func(toComplete string) []string {
							return []string{
								"functions/*.ts",
								"functions/*.js",
								"functions/*/*.go",
							}
						},
					},
				},
			}

			globA := struct {
				Handlers string
			}{}

			err = survey.Ask(globQ, &globA)
			cobra.CheckErr(err)

			p = &project.Config{
				BaseConfig: &project.BaseConfig{
					Dir:      path.Join(cd, answers.ProjectName),
					Name:     answers.ProjectName,
					Handlers: []any{globA.Handlers},
				},
			}
		} else {
			// Load and update the project name in the template's nitric.yaml
			p, err = project.ConfigFromProjectPath(projDir)
			cobra.CheckErr(err)
			p.Name = answers.ProjectName
		}

		err = p.ToFile()
		cobra.CheckErr(err)
	},
	Args: cobra.MaximumNArgs(2),
}

func validateName(val interface{}) error {
	name, ok := val.(string)
	if !ok {
		return errors.New("project name must be a string")
	}

	if name == "" {
		return errors.New("project name can not be empty")
	}

	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") || !nameRegex.MatchString(name) {
		return errors.New("invalid project name, only letters, numbers and dashes are supported")
	}

	return nil
}
