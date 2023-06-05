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
	"os"
	"path/filepath"
	"reflect"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"

	"github.com/nitrictech/cli/pkg/preview"
	"github.com/nitrictech/cli/pkg/utils"
)

// Config shared by all compute types
type BaseComputeConfig struct {
	Type string `yaml:"type"`
}

type HandlerConfig struct {
	BaseComputeConfig
	Match string
}

// TODO: Determine best way to use generic mixed type constraint when deserializing
// type Handler interface {
// 	string | HandlerConfig
// }

type BaseConfig struct {
	Name            string            `yaml:"name"`
	Dir             string            `yaml:"-"`
	Handlers        []any             `yaml:"handlers"`
	PreviewFeatures []preview.Feature `yaml:"preview-features"`
}

type Config struct {
	BaseConfig       `yaml:",inline"`
	ConcreteHandlers []*HandlerConfig `yaml:"-"`
}

func (p *Config) ToFile() error {
	if p.Dir == "" || p.Name == "" {
		return errors.New("fields Dir and Name must be provided")
	}

	b, err := yaml.Marshal(p)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(p.Dir, "nitric.yaml"), b, 0o644)
}

// configFromBaseConfig - Unwraps Generic configs (e.g. Handler) and populates missing defaults (e.g. Type)
func configFromBaseConfig(base BaseConfig) (*Config, error) {
	newConfig := &Config{
		BaseConfig:       base,
		ConcreteHandlers: make([]*HandlerConfig, 0),
	}

	if newConfig.BaseConfig.PreviewFeatures == nil {
		newConfig.BaseConfig.PreviewFeatures = make([]string, 0)
	}

	for _, h := range base.Handlers {
		if str, isString := h.(string); isString {
			// if its a basic string populate with default handler config
			newConfig.ConcreteHandlers = append(newConfig.ConcreteHandlers, &HandlerConfig{
				BaseComputeConfig: BaseComputeConfig{
					Type: "default",
				},
				Match: str,
			})
		} else if m, isMap := h.(map[any]any); isMap {
			// otherwise extract its map configuration
			// TODO: Check and validate the map properties
			newConfig.ConcreteHandlers = append(newConfig.ConcreteHandlers, &HandlerConfig{
				BaseComputeConfig: BaseComputeConfig{
					Type: m["type"].(string),
				},
				Match: m["match"].(string),
			})
		} else {
			return nil, fmt.Errorf("invalid handler config provided: %+v %s", h, reflect.TypeOf(h))
		}
	}

	return newConfig, nil
}

// ConfigFromProjectPath - loads the config nitric.yaml file from the project path, defaults to the current working directory
func ConfigFromProjectPath(projPath string) (*Config, error) {
	if projPath == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}

		projPath = wd
	}

	absDir, err := filepath.Abs(projPath)
	if err != nil {
		return nil, err
	}

	p := BaseConfig{
		Dir: absDir,
	}

	yamlFile, err := os.ReadFile(filepath.Join(projPath, "nitric.yaml"))
	if err != nil {
		return nil, errors.WithMessage(err, "No nitric project found (unable to find nitric.yaml). If you haven't created a project yet, run `nitric new` to get started")
	}

	err = yaml.Unmarshal(yamlFile, &p)
	if err != nil {
		return nil, err
	}

	p.Name = utils.FormatProjectName(p.Name)

	return configFromBaseConfig(p)
}
