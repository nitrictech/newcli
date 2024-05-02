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

package stack

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"
)

type StackConfig[T any] struct {
	Name     string `yaml:-`
	Provider string `yaml:"provider"`
	Config   T      `yaml:",inline"`
}

//go:embed aws.config.yaml
var awsConfigTemplate string

//go:embed azure.config.yaml
var azureConfigTemplate string

//go:embed gcp.config.yaml
var gcpConfigTemplate string

//go:embed custom.config.yaml
var customConfigTemplate string

var fileNameRegex = regexp.MustCompile(`(?i)^nitric\.(\S+)\.ya?ml$`)

func IsValidFileName(stackName string) bool {
	return fileNameRegex.MatchString(stackName)
}

func NewStackFile(fs afero.Fs, providerName string, stackName string, dir string, customProviderName string) (string, error) {
	if dir == "" {
		dir = "./"
	}

	var template string = ""

	switch providerName {
	case "aws":
		template = awsConfigTemplate
	case "gcp":
		template = gcpConfigTemplate
	case "azure":
		template = azureConfigTemplate
	case "custom":
		template = fmt.Sprintf(customConfigTemplate, customProviderName)
	}

	fileName := StackFileName(stackName)

	if !IsValidFileName(fileName) {
		return "", fmt.Errorf("requested stack name '%s' is invalid", stackName)
	}

	stackFilePath := filepath.Join(dir, fileName)
	relativePath, _ := filepath.Rel(".", stackFilePath)

	return fmt.Sprintf(".%s%s", string(os.PathSeparator), relativePath), afero.WriteFile(fs, stackFilePath, []byte(template), os.ModePerm)
}

// StackFileName returns the stack file name for a given stack name
func StackFileName(stackName string) string {
	return fmt.Sprintf("nitric.%s.yaml", stackName)
}

// ConfigFromName returns a stack configuration from a given stack name
func ConfigFromName[T any](fs afero.Fs, stackName string) (*StackConfig[T], error) {
	stackFile := StackFileName(stackName)
	if !IsValidFileName(stackFile) {
		return nil, fmt.Errorf("stack name '%s' is invalid", stackName)
	}

	return configFromFile[T](fs, filepath.Join("./", stackFile))
}

// GetAllStackFiles returns a list of all stack files in the current directory
func GetAllStackFiles(fs afero.Fs) ([]string, error) {
	return afero.Glob(fs, "./nitric.*.yaml")
}

// GetAllStackNames returns a list of all stack names in the current directory
func GetAllStackNames(fs afero.Fs) ([]string, error) {
	stackFiles, err := GetAllStackFiles(fs)
	if err != nil {
		return nil, err
	}

	stackNames := make([]string, len(stackFiles))

	for i, stackFile := range stackFiles {
		stackName, err := GetStackNameFromFileName(stackFile)
		if err != nil {
			return nil, err
		}

		stackNames[i] = stackName
	}

	return stackNames, nil
}

// GetAllStacks returns a map of all stack configurations in the current directory, keyed by stack name
func GetAllStacks[T any](fs afero.Fs) (map[string]*StackConfig[T], error) {
	stackFiles, err := GetAllStackFiles(fs)
	if err != nil {
		return nil, err
	}

	stacks := make(map[string]*StackConfig[T], len(stackFiles))

	for _, stackFile := range stackFiles {
		stackConfig, err := configFromFile[T](fs, stackFile)
		if err != nil {
			return nil, err
		}

		stacks[stackConfig.Name] = stackConfig
	}

	return stacks, nil
}

// GetStackNameFromFileName returns the stack name from a given stack file name
// e.g. nitric.aws.yaml -> aws
func GetStackNameFromFileName(fileName string) (string, error) {
	matches := fileNameRegex.FindStringSubmatch(fileName)
	if len(matches) > 1 {
		return matches[1], nil
	}

	return "", fmt.Errorf("file '%s' isn't a valid stack file name, name doesn't match required pattern %s", fileName, fileNameRegex.String())
}

// ConfigFromFile returns a stack configuration from a given stack file
func configFromFile[T any](fs afero.Fs, filePath string) (*StackConfig[T], error) {
	stackFileContents, err := afero.ReadFile(fs, filePath)
	if err != nil {
		return nil, err
	}

	stackConfig := &StackConfig[T]{}

	if err := yaml.Unmarshal(stackFileContents, stackConfig); err != nil {
		return nil, err
	}

	stackConfig.Name, err = GetStackNameFromFileName(filePath)
	if err != nil {
		return nil, err
	}

	return stackConfig, nil
}
