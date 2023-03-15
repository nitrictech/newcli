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

package run

import (
	"fmt"
	"strings"

	"github.com/pterm/pterm"

	"github.com/nitrictech/nitric/core/pkg/worker"
	"github.com/nitrictech/nitric/core/pkg/worker/pool"
)

type RunStackState struct {
	apis      map[string]string
	subs      map[string]string
	schedules map[string]string
}

func (r *RunStackState) Update(pool pool.WorkerPool, ls LocalServices) {
	// reset state maps
	r.apis = make(map[string]string)
	r.subs = make(map[string]string)
	r.schedules = make(map[string]string)

	for name, address := range ls.Apis() {
		r.apis[name] = address
	}

	// TODO: We can probably move this directly into local service state
	for _, wrkr := range pool.GetWorkers(nil) {
		switch w := wrkr.(type) {
		case *worker.SubscriptionWorker:
			r.subs[w.Topic()] = fmt.Sprintf("http://%s/topic/%s", ls.TriggerAddress(), w.Topic())
		case *worker.ScheduleWorker:
			topicKey := strings.ToLower(strings.ReplaceAll(w.Key(), " ", "-"))
			r.subs[w.Key()] = fmt.Sprintf("http://%s/topic/%s", ls.TriggerAddress(), topicKey)
		}
	}
}

func (r *RunStackState) Tables(port int) string {
	tables := []string{}

	table, rows := r.ApiTable(9001)
	if rows > 0 {
		tables = append(tables, table)
	}

	table, rows = r.TopicTable(9001)
	if rows > 0 {
		tables = append(tables, table)
	}

	table, rows = r.SchedulesTable(9001)
	if rows > 0 {
		tables = append(tables, table)
	}

	return strings.Join(tables, "\n\n")
}

func (r *RunStackState) ApiTable(port int) (string, int) {
	tableData := pterm.TableData{{"Api", "Endpoint"}}

	for name, address := range r.apis {
		tableData = append(tableData, []string{
			name, fmt.Sprintf("http://%s", address),
		})
	}

	str, _ := pterm.DefaultTable.WithHasHeader().WithData(tableData).Srender()

	return str, len(r.apis)
}

func (r *RunStackState) TopicTable(port int) (string, int) {
	tableData := pterm.TableData{{"Topic", "Endpoint"}}

	for k, address := range r.subs {
		tableData = append(tableData, []string{
			k, address,
		})
	}

	str, _ := pterm.DefaultTable.WithHasHeader().WithData(tableData).Srender()

	return str, len(r.subs)
}

func (r *RunStackState) SchedulesTable(port int) (string, int) {
	tableData := pterm.TableData{{"Schedule", "Endpoint"}}

	for k, address := range r.schedules {
		tableData = append(tableData, []string{
			k, address,
		})
	}

	str, _ := pterm.DefaultTable.WithHasHeader().WithData(tableData).Srender()

	return str, len(r.schedules)
}

func NewStackState() *RunStackState {
	return &RunStackState{
		apis:      map[string]string{},
		subs:      map[string]string{},
		schedules: map[string]string{},
	}
}
