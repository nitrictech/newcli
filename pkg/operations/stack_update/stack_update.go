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
	"os"

	"github.com/pterm/pterm"

	"github.com/nitrictech/cli/pkg/build"
	"github.com/nitrictech/cli/pkg/codeconfig"
	"github.com/nitrictech/cli/pkg/env"
	"github.com/nitrictech/cli/pkg/output"
	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/provider"
	"github.com/nitrictech/cli/pkg/provider/types"
	"github.com/nitrictech/cli/pkg/stack"
	"github.com/nitrictech/cli/pkg/tasklet"
	"github.com/nitrictech/cli/pkg/utils"
)

type Args struct {
	EnvFile     string
	Stack       *stack.Config
	Force       bool
	Interactive bool
}

func Run(args Args) {
	config, err := project.ConfigFromProjectPath("")
	utils.CheckErr(err)

	proj, err := project.FromConfig(config)
	utils.CheckErr(err)

	log.SetOutput(output.NewPtermWriter(pterm.Debug))
	log.SetFlags(0)

	envMap, err := env.LoadFromFiles(".env", ".env.production", args.EnvFile)
	utils.CheckErr(err)

	// build base images on updates
	buildBaseImagesTask := tasklet.Runner{
		StartMsg: "Building Images",
		Runner: func(_ output.Progress) error {
			return build.BaseImages(proj, nil)
		},
		StopMsg: "Images Built",
	}
	tasklet.MustRun(buildBaseImagesTask, tasklet.Opts{})

	configFromCode, err := codeconfig.New(proj, envMap)
	utils.CheckErr(err)

	gatherConfigFromCodeTask := tasklet.Runner{
		StartMsg: "Gathering configuration from code..",
		Runner: func(_ output.Progress) error {
			return configFromCode.Collect()
		},
		StopMsg: "Configuration gathered",
	}
	tasklet.MustRun(gatherConfigFromCodeTask, tasklet.Opts{})

	p, err := provider.FromStackFile(configFromCode, args.Stack.Name, args.Stack.Provider, envMap, &types.ProviderOpts{Force: args.Force, Interactive: args.Interactive})
	utils.CheckErr(err)

	d, err := p.Up()
	if err != nil {
		pterm.Error.Println(err)
		os.Exit(1)
	}

	// Print callable APIs if any were deployed
	if len(d.ApiEndpoints) > 0 {
		rows := [][]string{{"API", "Endpoint"}}
		for k, v := range d.ApiEndpoints {
			rows = append(rows, []string{k, v})
		}

		_ = pterm.DefaultTable.WithBoxed().WithData(rows).Render()
	}
}
