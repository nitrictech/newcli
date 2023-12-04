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

package build

import (
	"fmt"
	"os"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/nitrictech/cli/mocks/mock_containerengine"
	"github.com/nitrictech/cli/pkg/containerengine"
	"github.com/nitrictech/cli/pkg/project"
)

func TestBuildBaseImages(t *testing.T) {
	ctrl := gomock.NewController(t)
	me := mock_containerengine.NewMockContainerEngine(ctrl)

	dir, err := os.MkdirTemp("", "test-nitric-build")
	if err != nil {
		t.Error(err)
	}

	defer os.RemoveAll(dir)

	s := project.New(project.BaseConfig{Name: "", Dir: dir, PreviewFeatures: []string{"dockerfile"}})
	s.Functions = map[string]*project.Function{"foo": {Project: s, Handler: "functions/list.ts", Name: "foo", Config: &project.HandlerConfig{
		Type:  "default",
		Match: "functions/list.ts",
	}}}

	me.EXPECT().Build(gomock.Any(), dir, "-foo", gomock.Any(), []string{
		".nitric/", "!.nitric/*.yaml", ".git/", ".idea/", ".vscode/", ".github/", "*.dockerfile", "*.dockerignore", "node_modules/",
	}, gomock.Any())

	containerengine.DiscoveredEngine = me

	if err := BaseImages(s, nil); err != nil {
		t.Errorf("CreateBaseDev() error = %v", err)
	}
}

func TestBuildBaseImagesThrowsError(t *testing.T) {
	ctrl := gomock.NewController(t)
	me := mock_containerengine.NewMockContainerEngine(ctrl)

	dir, err := os.MkdirTemp("", "test-nitric-build")
	if err != nil {
		t.Error(err)
	}

	defer os.RemoveAll(dir)

	s := project.New(project.BaseConfig{Name: "", Dir: dir, PreviewFeatures: []string{"dockerfile"}})
	s.Functions = map[string]*project.Function{"foo": {Project: s, Handler: "functions/list.ts", Name: "foo", Config: &project.HandlerConfig{
		Type:  "default",
		Match: "functions/list.ts",
	}}}

	me.EXPECT().Build(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("an error occurred building the functions"))

	containerengine.DiscoveredEngine = me

	err = BaseImages(s, nil)

	gomock.Not(gomock.Nil().Matches(err))
	gomock.Eq(err.Error()).Matches("an error occurred building the functions")
}
