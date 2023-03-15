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
type awsProvider struct {
	*nitricDeployment
}

var awsSupportedRegions = []string{
	"us-east-1",
	"us-west-1",
	"us-west-2",
	"eu-west-1",
	"eu-central-1",
	"ap-southeast-1",
	"ap-northeast-1",
	"ap-southeast-2",
	"ap-northeast-2",
	"sa-east-1",
	"cn-north-1",
	"ap-south-1",
}

// FIXME: Prompting to create a new stack state and memory and persisting to a file should be two separte functions
func (g *awsProvider) AskAndSave() error {
	answers := struct {
		Region  string
		Project string
	}{}

	qs := []*survey.Question{
		{
			Name: "region",
			Prompt: &survey.Select{
				Message: "select the region",
				Options: awsSupportedRegions,
			},
		},
	}

	err := survey.Ask(qs, &answers)
	if err != nil {
		return err
	}

	g.sfc.Props["region"] = answers.Region

	b, err := yaml.Marshal(g.sfc)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(g.cfc.ProjectDir(), fmt.Sprintf("nitric-%s.yaml", g.sfc.Name)), b, 0o644)
}
