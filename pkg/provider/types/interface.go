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

package types

import (
	"github.com/nitrictech/cli/pkg/output"
	deploy "github.com/nitrictech/nitric/core/pkg/api/nitric/deploy/v1"
)

const (
	Aws   = "aws"
	Azure = "azure"
	Gcp   = "gcp"
)

var Providers = []string{Aws, Azure, Gcp}

type ResourceState struct {
	OpType   string
	Errored  bool
	Messages []string
}

type Summary struct {
	Resources map[string]*ResourceState
}

type Deployment struct {
	Summary      *Summary
	ApiEndpoints map[string]string `json:"apiEndpoints,omitempty"`
}

type ProviderOpts struct {
	Force bool
}

type Provider interface {
	Up(log output.Progress) (*Deployment, error)
	Down(log output.Progress) (*Summary, error)
	List() (interface{}, error)
	AskAndSave() error
	SupportedRegions() []string
	// Status()
}

type ConfigFromCode interface {
	ProjectDir() string
	ProjectName() string
	ToUpRequest() (*deploy.DeployUpRequest, error)
}
