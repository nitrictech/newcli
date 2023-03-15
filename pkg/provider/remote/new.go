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
	"path"
	"regexp"
	"strings"

	"github.com/nitrictech/cli/pkg/provider/types"
)

const providerRegex = `[a-z]+\/[a-z]+@(latest|(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*)))?`

// Official provider string the in the format of <org_name>/<provider_name>@<version>
type provider struct {
	org     string
	name    string
	version string
}

func getProviderParts(fullProvider string) (*provider, error) {
	match, err := regexp.MatchString(providerRegex, fullProvider)
	if err != nil {
		return nil, err
	}

	if !match {
		return nil, fmt.Errorf("invalid provider format %s, valid example is nitric/aws@1.2.3", fullProvider)
	}

	providerVersionParts := strings.Split(fullProvider, "@")

	prov := providerVersionParts[0]
	version := providerVersionParts[1]

	orgproviderParts := strings.Split(prov, "/")
	org := orgproviderParts[0]
	prov = orgproviderParts[1]

	return &provider{
		org:     org,
		name:    prov,
		version: version,
	}, nil
}

// Default providers and
// Use providers with a convention of <org-name>/<provider-name>
var defaultNitricOrg = "nitric"

func FromFile(cfc types.ConfigFromCode, name, stack string, envMap map[string]string, opts *types.ProviderOpts) (types.Provider, error) {
	// Read the stack file
	sc, err := StackConfigFromFile(path.Join(cfc.ProjectDir(), fmt.Sprintf("nitric-%s.yaml", name)))
	if err != nil {
		return nil, err
	}

	// Get the providers name and determine the type of deployment
	prov, err := getProviderParts(sc.Provider)
	if err != nil {
		return nil, err
	}

	if prov.org == defaultNitricOrg {
		// use the default nitric provider
		return NewNitricDeployment(cfc, sc, prov, envMap, opts)
	}

	// Otherwise assume provider already exists in the provider directory
	return NewBinaryRemoteDeployment(cfc, sc, prov, envMap, opts)
}

func New(cfc types.ConfigFromCode, name, provider string, envMap map[string]string, opts *types.ProviderOpts) (types.Provider, error) {
	sc := &StackConfig{
		Name:     name,
		Provider: provider,
		Props:    map[string]any{},
	}

	// Get the providers name and determine the type of deployment
	prov, err := getProviderParts(sc.Provider)
	if err != nil {
		return nil, err
	}

	if prov.org == defaultNitricOrg {
		// use the default nitric provider
		return NewNitricDeployment(cfc, sc, prov, envMap, opts)
	}

	// Otherwise assume provider already exists in the provider directory
	return NewBinaryRemoteDeployment(cfc, sc, prov, envMap, opts)
}
