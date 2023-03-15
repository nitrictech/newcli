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

package remote

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/AlecAivazis/survey/v2"
	"gopkg.in/yaml.v2"
)

// TODO: Move this into remote provider logic
// A provider should be able to produce it's own template for a valid stack specification as stack specifications may change over time
type azureProvider struct {
	*nitricDeployment
}

var azureSupportedRegions = []string{
	"canadacentral",
	"eastasia",
	"eastus",
	"eastus2",
	"germanywestcentral",
	"japaneast",
	"northeurope",
	"uksouth",
	"westeurope",
	"westus",
}

// FIXME: Prompting to create a new stack state and memory and persisting to a file should be two separte functions
func (a *azureProvider) AskAndSave() error {
	answers := struct {
		Region     string
		Org        string
		AdminEmail string
	}{}
	qs := []*survey.Question{
		{
			Name: "region",
			Prompt: &survey.Select{
				Message: "select the region",
				Options: azureSupportedRegions,
			},
		},
		{
			Name: "org",
			Prompt: &survey.Input{
				Message: "Provide the organisation to associate with the API",
			},
		},
		{
			Name: "adminEmail",
			Prompt: &survey.Input{
				Message: "Provide the adminEmail to associate with the API",
			},
		},
	}

	err := survey.Ask(qs, &answers)
	if err != nil {
		return err
	}

	a.sfc.Props["region"] = answers.Region
	a.sfc.Props["adminemail"] = answers.AdminEmail
	a.sfc.Props["org"] = answers.Org

	b, err := yaml.Marshal(a.sfc)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(a.cfc.ProjectDir(), fmt.Sprintf("nitric-%s.yaml", a.sfc.Name)), b, 0o644)
}
