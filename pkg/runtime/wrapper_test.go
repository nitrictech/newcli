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
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_telemetryConfig(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		want     string
		wantErr  bool
	}{
		{
			name:     "aws",
			provider: "aws",
			want: `
receivers:
  otlp:
    protocols:
      grpc:

processors:

extensions:

service:
  extensions:

  pipelines:
    traces:
      receivers: [otlp]
      exporters: [awsxray]
    metrics:
      receivers: [otlp]
      exporters: [awsemf]

exporters:
  awsxray: 
  awsemf: 
`,
		},
		{
			name:     "gcp",
			provider: "gcp",
			want: `
receivers:
  otlp:
    protocols:
      grpc:

processors:

extensions:

service:
  extensions:

  pipelines:
    traces:
      receivers: [otlp]
      exporters: [googlecloud]
    metrics:
      receivers: [otlp]
      exporters: [googlecloud]

exporters:
  googlecloud: {"retry_on_failure": {"enabled": false}}
  
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := telemetryConfig(tt.provider)
			if (err != nil) != tt.wantErr {
				t.Errorf("telemetryConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("telemetryConfig() %v", cmp.Diff(tt.want, got))
			}
		})
	}
}
