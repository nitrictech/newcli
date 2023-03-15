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
type gcpProvider struct {
	*nitricDeployment
}

var gcpSupportedRegions = []string{
	"us-west2",
	"us-west3",
	"us-west4",
	"us-central1",
	"us-east1",
	"us-east4",
	"europe-west1",
	"europe-west2",
	"asia-east1",
	"australia-southeast1",
}

// FIXME: Prompting to create a new stack state and memory and persisting to a file should be two separte functions
func (g *gcpProvider) AskAndSave() error {
	answers := struct {
		Region  string
		Project string
	}{}

	qs := []*survey.Question{
		{
			Name: "region",
			Prompt: &survey.Select{
				Message: "select the region",
				Options: gcpSupportedRegions,
			},
		},
		{
			Name: "project",
			Prompt: &survey.Input{
				Message: "Provide the gcp project to use",
			},
		},
	}

	err := survey.Ask(qs, &answers)
	if err != nil {
		return err
	}

	g.sfc.Props["region"] = answers.Region
	g.sfc.Props["gcp-project-id"] = answers.Project

	b, err := yaml.Marshal(g.sfc)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(g.cfc.ProjectDir(), fmt.Sprintf("nitric-%s.yaml", g.sfc.Name)), b, 0o644)
}
