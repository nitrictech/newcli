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

	"github.com/samber/lo"

	"github.com/nitrictech/cli/pkg/history"
	"github.com/nitrictech/cli/pkg/preview"
	"github.com/nitrictech/cli/pkg/runtime"
)

type Function struct {
	// Parent Project backreference
	Project *Project
	// The functions unique name
	Name string `yaml:"-"`
	// The location of the function handler
	Handler string `yaml:"handler"`
	// The functions type
	Config *HandlerConfig `yaml:"-"`
}

func (f *Function) GetRuntime() (runtime.Runtime, error) {
	if f.Config.Docker != nil {
		if !f.Project.IsPreviewFeatureEnabled(preview.Feature_Dockerfile) {
			return nil, fmt.Errorf("custom dockerfiles are currently in preview and must be enabled using preview-features in your nitric.yaml file")
		}
		// Using a custom runtime
		return runtime.NewCustomRuntime(f.Handler, f.Config.Docker.File, f.Config.Docker.Args)
	} else {
		// Using an OOTB runtime
		return runtime.NewRunTimeFromHandler(f.Handler)
	}
}

type Project struct {
	Dir             string               `yaml:"-"`
	Name            string               `yaml:"name"`
	Functions       map[string]*Function `yaml:"functions,omitempty"`
	PreviewFeatures []preview.Feature    `yaml:"-"`
	History         *history.History     `yaml:"-"`
}

func (p *Project) IsPreviewFeatureEnabled(feat preview.Feature) bool {
	return lo.Contains(p.PreviewFeatures, feat)
}

func New(config BaseConfig) *Project {
	return &Project{
		Name:            config.Name,
		Dir:             config.Dir,
		Functions:       map[string]*Function{},
		PreviewFeatures: config.PreviewFeatures,
		History:         history.NewHistory(config.Dir),
	}
}
