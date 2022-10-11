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

package project

import (
	"fmt"
)

var _ Compute = &Container{}

func (c *Container) String() string {
	return fmt.Sprintf("%s(%s)", c.Name, c.Dockerfile)
}

func (c *Container) Unit() *ComputeUnit {
	return &c.ComputeUnit
}

// ImageTagName returns the default image tag for a source image built from this function
// provider the provider name (e.g. aws), used to uniquely identify builds for specific providers
func (c *Container) ImageTagName(s *Project, provider string) string {
	providerString := ""
	if provider != "" {
		providerString = "-" + provider
	}

	return fmt.Sprintf("%s-%s%s", s.Name, c.Name, providerString)
}

func (c *Container) Workers() int {
	// Default to expecting a minimum of 1 worker for containers
	return 1
}
