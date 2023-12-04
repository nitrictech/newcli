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

package runtime

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type custom struct {
	handler    string
	dockerfile string
	args       map[string]string
}

var _ Runtime = &custom{}

func (t *custom) ContainerName() string {
	return strings.Replace(filepath.Base(t.handler), filepath.Ext(t.handler), "", 1)
}

func (t *custom) BuildIgnore(additional ...string) []string {
	ignoreContents := []string{}
	// make an ignore file from one if there is one available
	dockerfile, err := os.ReadFile(fmt.Sprintf("%s.dockerignore", t.dockerfile))
	if err == nil {
		ignoreContents = strings.Split(string(dockerfile), "\n")
	}

	ignoreContents = append(ignoreContents, commonIgnore...)

	return append(additional, ignoreContents...)
}

func (t *custom) BuildArgs() map[string]string {
	args := map[string]string{
		"HANDLER": filepath.ToSlash(t.handler),
	}

	for k, v := range t.args {
		args[k] = v
	}

	return args
}

func (t *custom) WriteDockerfile(w io.Writer) error {
	dockerfile, err := os.ReadFile(t.dockerfile)
	if err != nil {
		return err
	}

	_, err = w.Write(dockerfile)

	return err
}
