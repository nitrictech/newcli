package project

import "github.com/nitrictech/cli/pkg/project/runtime"

type Batch struct {
	Name string

	// filepath relative to the project root directory
	filepath     string
	buildContext runtime.RuntimeBuildContext

	runCmd string
}
