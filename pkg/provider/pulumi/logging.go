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

package pulumi

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pterm/pterm"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/events"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optdestroy"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optup"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"

	"github.com/nitrictech/cli/pkg/output"
)

func updateLoggingOpts(log output.Progress) []optup.Option {
	upChannel := make(chan events.EngineEvent)
	opts := []optup.Option{
		optup.EventStreams(upChannel),
	}
	go collectEvents(log, upChannel, "Deploying.. ")

	// if output.VerboseLevel >= 2 {
	// 	piper, pipew := io.Pipe()
	// 	go output.StdoutToPtermDebug(piper, log, "Deploying.. ")

	// 	opts = append(opts, optup.ProgressStreams(pipew))
	// }
	// if output.VerboseLevel > 2 {
	// 	var loglevel uint = uint(output.VerboseLevel)
	// 	opts = append(opts, optup.DebugLogging(debug.LoggingOptions{
	// 		LogLevel:      &loglevel,
	// 		LogToStdErr:   true,
	// 		FlowToPlugins: true,
	// 	}))
	// }
	return opts
}

func destroyLoggingOpts(log output.Progress) []optdestroy.Option {
	upChannel := make(chan events.EngineEvent)
	opts := []optdestroy.Option{
		optdestroy.EventStreams(upChannel),
	}
	go collectEvents(log, upChannel, "Deleting.. ")

	// if output.VerboseLevel >= 2 {
	// 	piper, pipew := io.Pipe()
	// 	go output.StdoutToPtermDebug(piper, log, "Deleting.. ")

	// 	opts = append(opts, optdestroy.ProgressStreams(pipew))
	// }
	// if output.VerboseLevel > 2 {
	// 	var loglevel uint = uint(output.VerboseLevel)
	// 	opts = append(opts, optdestroy.DebugLogging(debug.LoggingOptions{
	// 		LogLevel:      &loglevel,
	// 		LogToStdErr:   true,
	// 		FlowToPlugins: true,
	// 	}))
	// }
	return opts
}

func stepEventToString(eType string, evt *apitype.StepEventMetadata) string {
	urnSplit := strings.Split(evt.URN, "::")
	name := urnSplit[len(urnSplit)-1]

	typeSplit := strings.Split(evt.Type, ":")
	rType := typeSplit[len(typeSplit)-1]

	return fmt.Sprintf("%s/%s", rType, name)
}

type ResourceStatus = string

const (
	ResourceStatus_Pending = "pending"
	ResourceStatus_Created = "created"
	ResourceStatus_Deleted = "deleted"
	ResourceStatus_Same    = "unchanged"
	ResourceStatus_Failed  = "failed"
)

type ResourceState struct {
	name       string
	lastUpdate string
	status     ResourceStatus
}

type DeploymentState = map[string]*ResourceState

func deploymentStateToTable(state DeploymentState) string {
	tableData := pterm.TableData{{"Resource", "Update", "Status"}}

	keys := make([]string, 0, len(state))
	for k := range state {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, k := range keys {

		status := state[k]

		var statusStr string
		switch status.status {
		case ResourceStatus_Pending:
			statusStr = pterm.Yellow(status.status)
		case ResourceStatus_Created:
			statusStr = pterm.Green(status.status)
		case ResourceStatus_Deleted:
			statusStr = pterm.LightRed(status.status)
		case ResourceStatus_Failed:
			statusStr = pterm.Red(status.status)
		case ResourceStatus_Same:
			statusStr = pterm.LightYellow(status.status)
		}

		tableData = append(tableData, []string{status.name, status.lastUpdate, statusStr})
	}

	result, _ := pterm.DefaultTable.WithHasHeader(true).WithData(tableData).Srender()
	return result
}

func collectEvents(log output.Progress, eventChannel <-chan events.EngineEvent, prefix string) {
	state := make(DeploymentState)

	for {
		var event events.EngineEvent
		var ok bool

		event, ok = <-eventChannel
		if !ok {
			return
		}

		if event.ResourcePreEvent != nil {
			urnParts := strings.Split(event.ResourcePreEvent.Metadata.URN, "::")

			name := urnParts[len(urnParts)-1]

			// typ := event.ResourcePreEvent.Metadata.Type

			state[event.ResourcePreEvent.Metadata.URN] = &ResourceState{
				name:       name,
				lastUpdate: "",
				status:     ResourceStatus_Pending,
			}
		}

		if event.DiagnosticEvent != nil {
			if state[event.DiagnosticEvent.URN] != nil {
				state[event.DiagnosticEvent.URN].lastUpdate = strings.TrimSpace(event.DiagnosticEvent.Message)
			}
		}

		if event.ResOutputsEvent != nil {
			// lc := stepEventToString("ResOutputsEvent", &event.ResOutputsEvent.Metadata)
			res, ok := state[event.ResOutputsEvent.Metadata.URN]

			if ok {
				switch event.ResOutputsEvent.Metadata.Op {
				case apitype.OpCreate:
					res.status = ResourceStatus_Created
				case apitype.OpSame:
					res.status = ResourceStatus_Same
				case apitype.OpDelete:
					res.status = ResourceStatus_Deleted
				default:
					res.status = ResourceStatus_Created
				}
			}

		}
		if event.ResOpFailedEvent != nil {
			lc := stepEventToString("ResOpFailedEvent", &event.ResOpFailedEvent.Metadata)
			log.Failf("%s\n", lc)

			state[event.ResOpFailedEvent.Metadata.URN] = &ResourceState{
				lastUpdate: "failed to deployed resource",
				status:     ResourceStatus_Failed,
			}
		}

		log.Busyf(deploymentStateToTable(state))
	}
}
