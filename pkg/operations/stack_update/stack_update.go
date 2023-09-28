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

package stack_update

import (
	"log"

	"github.com/joho/godotenv"
	"github.com/pterm/pterm"
	"github.com/spf13/afero"

	"github.com/nitrictech/cli/pkg/build"
	"github.com/nitrictech/cli/pkg/codeconfig"
	"github.com/nitrictech/cli/pkg/output"
	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/provider"
	"github.com/nitrictech/cli/pkg/provider/types"
	"github.com/nitrictech/cli/pkg/stack"
	"github.com/nitrictech/cli/pkg/tasklet"
	"github.com/nitrictech/cli/pkg/utils"
)

func Run(fs afero.Fs, envFile string, s *stack.Config, force bool) {
	config, err := project.ConfigFromProjectPath(fs, "")
	utils.CheckErr(err)

	proj, err := project.FromConfig(config)
	utils.CheckErr(err)

	log.SetOutput(output.NewPtermWriter(pterm.Debug))
	log.SetFlags(0)

	envFiles := utils.FilesExisting(".env", ".env.production", envFile)

	envMap := map[string]string{}

	if len(envFiles) > 0 {
		envMap, err = godotenv.Read(envFiles...)
		utils.CheckErr(err)
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
	utils.CheckErr(err)

	codeAsConfig := tasklet.Runner{
		StartMsg: "Gathering configuration from code..",
		Runner: func(_ output.Progress) error {
			return cc.Collect()
		},
		StopMsg: "Configuration gathered",
	}
	tasklet.MustRun(codeAsConfig, tasklet.Opts{})

	p, err := provider.ProviderFromFile(cc, s.Name, s.Provider, envMap, &types.ProviderOpts{Force: force})
	utils.CheckErr(err)

	d := &types.Deployment{}
	deploy := tasklet.Runner{
		StartMsg: "Deploying..",
		Runner: func(progress output.Progress) error {
			d, err = p.Up(progress)

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
}
