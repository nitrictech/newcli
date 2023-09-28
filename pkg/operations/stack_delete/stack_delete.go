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

package stack_delete

import (
	"log"
	"os"

	"github.com/pterm/pterm"

	"github.com/nitrictech/cli/pkg/codeconfig"
	"github.com/nitrictech/cli/pkg/output"
	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/provider"
	"github.com/nitrictech/cli/pkg/provider/types"
	"github.com/nitrictech/cli/pkg/stack"
	"github.com/nitrictech/cli/pkg/utils"
)

type Args struct {
	Interactive bool
}

func Run(args Args) {
	s, err := stack.ConfigFromOptions()
	utils.CheckErr(err)

	log.SetOutput(output.NewPtermWriter(pterm.Debug))
	log.SetFlags(0)

	config, err := project.ConfigFromProjectPath("")
	utils.CheckErr(err)

	proj, err := project.FromConfig(config)
	utils.CheckErr(err)

	cc, err := codeconfig.New(proj, map[string]string{})
	utils.CheckErr(err)

	p, err := provider.ProviderFromFile(cc, s.Name, s.Provider, map[string]string{}, &types.ProviderOpts{Force: true, Interactive: args.Interactive})
	utils.CheckErr(err)

	_, err = p.Down()

	if err != nil {
		os.Exit(1)
	}
}
