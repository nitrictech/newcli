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

package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/system"
)

func (d *Dashboard) createServiceLogsHandler(project *project.Project) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "*")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method == "GET" {
			logs, err := system.ReadLogs()
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			// Send logs as JSON response
			w.Header().Set("Content-Type", "application/json")

			if err := json.NewEncoder(w).Encode(logs); err != nil {
				http.Error(w, "Failed to encode logs: "+err.Error(), http.StatusInternalServerError)
			}

			return
		}

		if r.Method == "DELETE" {
			err := system.PurgeLogs()
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			w.WriteHeader(http.StatusOK)
		}
	}
}
