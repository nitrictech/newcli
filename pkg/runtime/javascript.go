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

type javascript struct {
	rte     RuntimeExt
	handler string
}

//go:embed javascript.dockerfile
var javascriptDockerfile string

var (
	_                    Runtime = &javascript{}
	javascriptIgnoreList         = append(commonIgnore, "node_modules/")
)

func (t *javascript) ContainerName() string {
	return normalizeFileName(t.handler)
}

func (t *javascript) BuildIgnore(additional ...string) []string {
	return append(additional, javascriptIgnoreList...)
}

func (t *javascript) WriteDockerfile(w io.Writer) error {
	_, err := w.Write([]byte(javascriptDockerfile))
	return err
}

func (t *javascript) BuildArgs() map[string]string {
	return map[string]string{
		"HANDLER": filepath.ToSlash(t.handler),
	}
}
