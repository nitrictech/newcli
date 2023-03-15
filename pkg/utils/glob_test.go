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
	"path/filepath"
	"reflect"
	"testing"
)

func TestFindFilesInDir(t *testing.T) {
	tests := []struct {
		dir     string
		name    string
		want    []string
		wantErr bool
	}{
		{
			dir:  "../",
			name: "glob.go",
			want: []string{"utils/glob.go"},
		},
		{
			dir:  "../../",
			name: "generator.go",
			want: []string{"pkg/provider/generator.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.dir+":"+tt.name, func(t *testing.T) {
			got, err := FindFilesInDir(tt.dir, tt.name)
			if (err != nil) != tt.wantErr {
				t.Errorf("FindFilesInDir() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FindFilesInDir() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGlobInDir(t *testing.T) {
	tests := []struct {
		dir     string
		pattern string
		want    []string
		wantErr bool
	}{
		{
			dir:     ".",
			pattern: "glob.*",
			want:    []string{"glob.go"},
		},
		{
			dir:     "../../",
			pattern: "*/*/generator.go",
			want:    []string{"pkg/provider/generator.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.dir+":"+tt.pattern, func(t *testing.T) {
			absDir, err := filepath.Abs(tt.dir)
			if err != nil {
				t.Error(err)
			}

			got, err := GlobInDir(absDir, tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("GlobInDir() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GlobInDir() = %v, want %v", got, tt.want)
			}
		})
	}
}
