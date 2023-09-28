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

	"github.com/spf13/afero"

	"path/filepath"
	"reflect"

	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"

	"github.com/nitrictech/cli/pkg/preview"
	"github.com/nitrictech/cli/pkg/utils"
)

type DockerConfig struct {
	File string
	Args map[string]string
}

type HandlerConfig struct {
	Type   string        `yaml:"type" mapstructure:"type"`
	Match  string        `yaml:"match" mapstructure:"match"`
	Docker *DockerConfig `yaml:"docker,omitempty" mapstructure:"docker,omitempty"`
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
	fs               *afero.Fs
}

func (p *Config) ToFile() error {
	if p.Dir == "" || p.Name == "" {
		return errors.New("fields Dir and Name must be provided")
	}

	b, err := yaml.Marshal(p)
	if err != nil {
		return err
	}

	return afero.WriteFile(*p.fs, filepath.Join(p.Dir, "nitric.yaml"), b, 0o644)
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
				Type:  "default",
				Match: str,
			})
		} else if m, isMap := h.(map[any]any); isMap {
			actualConfig := &HandlerConfig{}

			err := mapstructure.Decode(m, actualConfig)
			if err != nil {
				return nil, err
			}

			// otherwise extract its map configuration
			// TODO: Check and validate the map properties
			newConfig.ConcreteHandlers = append(newConfig.ConcreteHandlers, actualConfig)
		} else {
			return nil, fmt.Errorf("invalid handler config provided: %+v %s", h, reflect.TypeOf(h))
		}
	}

	return newConfig, nil
}

// ConfigFromProjectPath - loads the config nitric.yaml file from the project path, defaults to the current working directory
func ConfigFromProjectPath(fs afero.Fs, projPath string) (*Config, error) {
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

	yamlFile, err := afero.ReadFile(fs, filepath.Join(projPath, "nitric.yaml"))
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
