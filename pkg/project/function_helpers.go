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
	_ "embed"
	"fmt"
	"path/filepath"
)

//go:embed membraneversion.txt
var DefaultMembraneVersion string

//go:embed otel-collector-version.txt
var DefaultOTELCollectorVersion string

var _ Compute = &Function{}

func (f *Function) String() string {
	return fmt.Sprintf("%s(%s) telemetry:%v", f.Name, f.Handler, f.Telemetry)
}

func (f *Function) Unit() *ComputeUnit {
	return &f.ComputeUnit
}

func (f *Function) RelativeHandlerPath(s *Project) (string, error) {
	relativeHandlerPath := f.Handler

	if filepath.IsAbs(f.Handler) {
		var err error

		relativeHandlerPath, err = filepath.Rel(s.Dir, f.Handler)
		if err != nil {
			return "", err
		}
	}

	return relativeHandlerPath, nil
}

// ImageTagName returns the default image tag for a source image built from this function
// provider the provider name (e.g. aws), used to uniquely identify builds for specific providers
func (f *Function) ImageTagName(s *Project, provider string) string {
	providerString := ""
	if provider != "" {
		providerString = "-" + provider
	}

	return fmt.Sprintf("%s-%s%s", s.Name, f.Name, providerString)
}

func (c *Function) Workers() int {
	return c.WorkerCount
}
