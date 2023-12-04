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
	"errors"
	"io"
	"path/filepath"
	"runtime"
	"strings"
)

type Runtime interface {
	ContainerName() string
	BuildIgnore(additional ...string) []string
	// WriteDockerfile - Write the dockerfile for this runtime to the provided writer
	WriteDockerfile(w io.Writer) error
	BuildArgs() map[string]string
}

type RuntimeExt string

const (
	RuntimeTypescript RuntimeExt = "ts"
	RuntimeJavascript RuntimeExt = "js"
	RuntimePython     RuntimeExt = "py"
	RuntimeGolang     RuntimeExt = "go"
	RuntimeCsharp     RuntimeExt = "cs"
	RuntimeJvm        RuntimeExt = "jar"

	RuntimeUnknown RuntimeExt = ""
)

var commonIgnore = []string{".nitric/", "!.nitric/*.yaml", ".git/", ".idea/", ".vscode/", ".github/", "*.dockerfile", "*.dockerignore"}

func NewCustomRuntime(handler string, dockerfile string, args map[string]string) (Runtime, error) {
	return &custom{
		handler:    handler,
		dockerfile: dockerfile,
		args:       args,
	}, nil
}

// normalizeFileName - Normalizes a file name to a usable container name
func normalizeFileName(handler string) string {
	baseName := strings.Replace(filepath.Base(handler), filepath.Ext(handler), "", 1)
	return strings.ToLower(strings.Replace(baseName, ".", "-", -1))
}

func NewRunTimeFromHandler(handler string) (Runtime, error) {
	rt := RuntimeExt(strings.Replace(filepath.Ext(handler), ".", "", -1))

	if strings.Contains(runtime.GOARCH, "arm") && rt == RuntimeCsharp {
		return nil, errors.New("the .NET runtime is not supported in containers using ARM based architecture. We recommend using nitric start for local development, and a CI/CD pipeline for deployments. For more info on the issue: https://devblogs.microsoft.com/dotnet/announcing-net-6/#docker-on-arm64")
	}

	switch rt {
	case RuntimeGolang:
		return &golang{rte: rt, handler: handler}, nil
	case RuntimeJavascript:
		return &javascript{rte: rt, handler: handler}, nil
	case RuntimePython:
		return &python{rte: rt, handler: handler}, nil
	case RuntimeTypescript:
		return &typescript{rte: rt, handler: handler}, nil
	case RuntimeCsharp:
		return &csharp{rte: rt, handler: handler}, nil
	case RuntimeJvm:
		return &jvm{rte: rt, handler: handler}, nil
	default:
		return nil, errors.New("runtime '" + string(rt) + "' not supported")
	}
}
