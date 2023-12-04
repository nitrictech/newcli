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

package utils

import (
	"os"
	"path/filepath"
	"strings"
)

func FindFilesInDir(dir string, name string) ([]string, error) {
	files := []string{}
	err := filepath.Walk(dir, func(path string, f os.FileInfo, err error) error {
		if f != nil && f.Name() == name {
			// remove the provided dir (so it's like we have changed dir here)
			files = append(files, strings.Replace(path, dir, "", 1))
		}

		return nil
	})

	return files, err
}

func GlobInDir(dir, pattern string) ([]string, error) {
	if !strings.HasPrefix(pattern, dir) {
		pattern = filepath.Join(dir, pattern)
	}

	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	final := []string{}
	for _, f := range files {
		final = append(final, strings.TrimPrefix(strings.TrimPrefix(f, dir), "/"))
	}

	return final, nil
}
