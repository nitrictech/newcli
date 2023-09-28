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

package stack_new

import (
	"github.com/AlecAivazis/survey/v2"
	"github.com/spf13/afero"

	"github.com/nitrictech/cli/pkg/codeconfig"
	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/provider"
	"github.com/nitrictech/cli/pkg/provider/types"
	"github.com/nitrictech/cli/pkg/utils"
)

func Run(fs afero.Fs) error {
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

	pc, err := project.ConfigFromProjectPath(fs, "")
	if err != nil {
		return err
	}

	cc, err := codeconfig.New(project.New(pc.BaseConfig), map[string]string{})
	utils.CheckErr(err)

	prov, err := provider.NewProvider(cc, name, pName, map[string]string{}, &types.ProviderOpts{})
	if err != nil {
		return err
	}

	return prov.AskAndSave()
}
