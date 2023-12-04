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
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/docker/distribution/reference"
	"github.com/samber/lo"
	"golang.org/x/sync/errgroup"

	goruntime "runtime"

	"github.com/nitrictech/cli/pkg/containerengine"
	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/sync"
)

func newBlankDynamicDockerfile(dir, name string) (*os.File, error) {
	// create a more stable file name for the hashing
	return os.Create(filepath.Join(dir, fmt.Sprintf("%s.nitric.dynamic.dockerfile", name)))
}

func GenerateContainerImageTag(projectName string, functionName string) string {
	return fmt.Sprintf("%s-%s", projectName, functionName)
}

func buildExecUnitContainerImage(buildContext string, fun *project.Function, ignoredFiles []string, logs io.Writer) error {
	containerEngine, err := containerengine.Discover()
	if err != nil {
		return err
	}

	funcRuntime, err := fun.GetRuntime()
	if err != nil {
		return err
	}

	dockerfile, err := newBlankDynamicDockerfile(buildContext, fun.Name)
	if err != nil {
		return err
	}

	defer func() {
		dockerfile.Close()
		os.Remove(dockerfile.Name())
	}()

	if err := funcRuntime.WriteDockerfile(dockerfile); err != nil {
		return err
	}

	ignoreList := funcRuntime.BuildIgnore(ignoredFiles...)

	if err := containerEngine.Build(filepath.Base(dockerfile.Name()), buildContext, GenerateContainerImageTag(fun.Project.Name, fun.Name), funcRuntime.BuildArgs(), ignoreList, logs); err != nil {
		return err
	}

	return nil
}

func isValidFunctionName(name string) bool {
	_, err := reference.Parse(name)
	return err == nil
}

type BuildResult struct {
	results map[string]error
}

// BaseImages - Builds images for all execution units in the project, without embedding the nitric runtime.
//
//	allows containers to be connected to an external nitric server, such as when gathering configuration from code.
func BaseImages(functions []*project.Function, logger *Multiplexer) (*BuildResult, error) {
	errs, _ := errgroup.WithContext(context.Background())

	// set concurrent build limit here
	maxConcurrency := lo.Min([]int{goruntime.GOMAXPROCS(0), goruntime.NumCPU()})

	maxConcurrencyEnv := os.Getenv("MAX_BUILD_CONCURRENCY")
	if maxConcurrencyEnv != "" {
		newVal, err := strconv.Atoi(maxConcurrencyEnv)
		if err != nil {
			return nil, fmt.Errorf("invalid value for MAX_BUILD_CONCURRENCY must be int got %s", maxConcurrencyEnv)
		}

		maxConcurrency = newVal
	}

	for _, fun := range functions {
		if !isValidFunctionName(fun.Name) {
			return nil, fmt.Errorf("invalid handler name \"%s\". Names can only include alphanumeric characters, underscores, periods and hyphens", fun.Handler)
		}
	}

	fmt.Printf("running %d builds concurrently\n", maxConcurrency)

	workerPool := sync.NewWorkerPool[error](maxConcurrency)

	// errs.SetLimit(maxConcurrency)

	for _, fun := range functions {
		// Ignore all other execution unit entrypoint files.
		// Entrypoint files should never import other entrypoints since this could cause inadvertent application of resource permissions.
		// This ensures code breaks at build time if that restriction is violated.
		otherExecUnits := lo.Filter(lo.Values(functions), func(other *project.Function, index int) bool {
			return other.Name != fun.Name
		})

		ignoreEntrypoints := lo.Map(otherExecUnits, func(other *project.Function, index int) string {
			return other.Handler
		})

		var logout = io.Discard
		if logger != nil {
			// Add a writer to the log multiplexer
			logout = logger.CreateWriter(fun.Name)
		}

		workerPool.Go(fun.Name, func() error {
			return buildExecUnitContainerImage(fun.Project.Dir, fun, ignoreEntrypoints, logout)
		})
	}

	buildResults := workerPool.Wait()

	// errorResults := lo.Filter(buildResults, func(result sync.JobResult[error], index int) bool {
	// 	return result.Result != nil
	// })

	errorMap := map[string]error{}
	for _, result := range buildResults {

	}

	if len(errorResults) > 0 {
		errorResult := "errors occurred building images:\n"
		for _, result := range errorResults {
			errorResult += fmt.Sprintf()
		}

		return nil, err
	}

	return nil
}
