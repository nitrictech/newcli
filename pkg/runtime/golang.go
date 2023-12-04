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
	"io"
	"path/filepath"
)

type golang struct {
	rte     RuntimeExt
	handler string
}

var _ Runtime = &golang{}

//go:embed golang.dockerfile
var golangDockerfile string

func (t *golang) BuildIgnore(additional ...string) []string {
	return append(additional, commonIgnore...)
}

func (t *golang) WriteDockerfile(w io.Writer) error {
	_, err := w.Write([]byte(golangDockerfile))
	return err
}

func (t *golang) BuildArgs() map[string]string {
	return map[string]string{
		"HANDLER": filepath.ToSlash(filepath.Dir(t.handler)),
	}
}

func (t *golang) ContainerName() string {
	// get the abs dir in case user provides "."
	absH, err := filepath.Abs(t.handler)
	if err != nil {
		return ""
	}

	return filepath.Base(filepath.Dir(absH))
}
