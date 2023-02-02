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

package provider

import (
	"github.com/nitrictech/cli/pkg/provider/pulumi"
	"github.com/nitrictech/cli/pkg/provider/remote"
	"github.com/nitrictech/cli/pkg/provider/types"
)

func NewProvider(cfc types.ConfigFromCode, name, provider string, envMap map[string]string, opts *types.ProviderOpts) (types.Provider, error) {
	switch provider {
	case types.Aws, types.Azure, types.Gcp:
		return pulumi.New(cfc, name, provider, envMap, opts)
	default:
		return remote.New(cfc, name, provider, envMap, opts)
	}
}
