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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/google/uuid"

	"github.com/nitrictech/cli/pkg/containerengine"
)

const wrapperDockerFile = `
ARG BASE_IMAGE

FROM ${BASE_IMAGE}

ARG MEMBRANE_URI
ARG MEMBRANE_VERSION

ENV MEMBRANE_VERSION ${MEMBRANE_VERSION}

ADD ${MEMBRANE_URI} /bin/membrane

RUN chmod +x-rw /bin/membrane


CMD [%s]
ENTRYPOINT ["/bin/membrane"]
`

const wrapperDockerFileWithOTel = `
ARG BASE_IMAGE

FROM ${BASE_IMAGE}

ARG MEMBRANE_URI
ARG MEMBRANE_VERSION

ENV MEMBRANE_VERSION ${MEMBRANE_VERSION}

RUN apk add --no-cache wget && \
    wget -q ${MEMBRANE_URI} -O /bin/membrane && \
    chmod +x-rw /bin/membrane

ARG OTELCOL_CONTRIB_URI

ADD ${OTELCOL_CONTRIB_URI} /usr/bin/
RUN tar -xzf /usr/bin/otelcol*.tar.gz &&\
    rm /usr/bin/otelcol*.tar.gz &&\
	mv /otelcol-contrib /usr/bin/

ARG OTELCOL_CONFIG
COPY ${OTELCOL_CONFIG} /etc/otelcol/config.yaml
RUN chmod -R a+r /etc/otelcol

CMD [%s]
ENTRYPOINT ["/bin/membrane"]
`

const otelTemplate = `
receivers:
  otlp:
    protocols:
      grpc:

processors:

service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [{{.TraceName}}]
    metrics:
      receivers: [otlp]
      exporters: [{{.MetricName}}]

exporters:
  {{ .TraceName }}: {{ .TraceExporterConfig }}
  {{ if ne .MetricName .TraceName }}
  {{ .MetricName }}: {{ .MetricExporterConfig }}
  {{ end }}
`

// CmdFromImage - Takes the existing Entrypoint and CMD from and image and makes it a new CMD to be wrapped by a new entrypoint
func cmdFromImage(ce containerengine.ContainerEngine, imageName string) ([]string, error) {
	ii, err := ce.Inspect(imageName)
	if err != nil {
		return nil, err
	}

	// Get the new cmd
	cmds := append(ii.Config.Entrypoint, ii.Config.Cmd...)

	execCmds := make([]string, 0)
	for _, cmd := range cmds {
		execCmds = append(execCmds, fmt.Sprintf("\"%s\"", cmd))
	}

	return execCmds, nil
}

type WrappedBuildInput struct {
	Args       map[string]string
	Dockerfile string
}

type otelConfig struct {
	MetricName           string
	TraceName            string
	MetricExporterConfig string
	TraceExporterConfig  string
}

func telemetryConfig(provider string) (string, error) {
	t := template.Must(template.New("otelconfig").Parse(otelTemplate))
	config := &strings.Builder{}

	switch provider {
	case "aws":
		err := t.Execute(config, &otelConfig{
			TraceName:  "awsxray",
			MetricName: "awsemf",
		})
		if err != nil {
			return "", err
		}

	case "gcp":
		err := t.Execute(config, &otelConfig{
			TraceName:           "googlecloud",
			MetricName:          "googlecloud",
			TraceExporterConfig: `{"retry_on_failure": {"enabled": false}}`,
		})
		if err != nil {
			return "", err
		}

	default:
		return "", errors.New("telemetry not supported on this cloud yet")
	}

	return config.String(), nil
}

func yamlConfigFile(dir, name string) (*os.File, error) {
	// create a more stable file name for the hashing
	err := os.MkdirAll(filepath.Join(dir, ".nitric"), os.ModePerm)
	if err != nil {
		return nil, err
	}

	return os.Create(filepath.Join(dir, ".nitric", name+".yaml"))
}

type WrapperBuildArgsConfig struct {
	ProjectDir           string
	ImageName            string
	Provider             string
	MembraneVersion      string
	OtelCollectorVersion string
	Telemetry            bool
}

func WrapperBuildArgs(config *WrapperBuildArgsConfig) (*WrappedBuildInput, error) {
	ce, err := containerengine.Discover()
	if err != nil {
		return nil, err
	}

	cmd, err := cmdFromImage(ce, config.ImageName)
	if err != nil {
		return nil, err
	}

	membraneName := "membrane-" + config.Provider
	fetchFrom := fmt.Sprintf("https://github.com/nitrictech/nitric/releases/download/%s/%s", config.MembraneVersion, membraneName)
	membraneVersion := config.MembraneVersion

	if os.Getenv("TEST_MEMBRANE_URI") != "" {
		membraneVersion = uuid.NewString() // to get the development membrane re-inserted.
		fetchFrom = fmt.Sprintf("%s?foo=%s", os.Getenv("TEST_MEMBRANE_URI"), membraneVersion)
	}

	if config.Telemetry {
		tf, err := yamlConfigFile(config.ProjectDir, "otel-config")
		if err != nil {
			return nil, err
		}

		defer tf.Close()

		cfg, err := telemetryConfig(config.Provider)
		if err != nil {
			return nil, err
		}

		_, err = tf.WriteString(cfg)
		if err != nil {
			return nil, err
		}

		relConfig, err := filepath.Rel(config.ProjectDir, tf.Name())
		if err != nil {
			return nil, err
		}

		otelCollectorVer := fmt.Sprintf(
			"https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/%s/otelcol-contrib_%s_linux_amd64.tar.gz",
			config.OtelCollectorVersion, strings.TrimSpace(strings.TrimLeft(config.OtelCollectorVersion, "v")))

		return &WrappedBuildInput{
			Dockerfile: fmt.Sprintf(wrapperDockerFileWithOTel, strings.Join(cmd, ",")),
			Args: map[string]string{
				"MEMBRANE_URI":        fetchFrom,
				"MEMBRANE_VERSION":    membraneVersion,
				"BASE_IMAGE":          config.ImageName,
				"OTELCOL_CONFIG":      relConfig,
				"OTELCOL_CONTRIB_URI": otelCollectorVer,
			},
		}, nil
	}

	return &WrappedBuildInput{
		Dockerfile: fmt.Sprintf(wrapperDockerFile, strings.Join(cmd, ",")),
		Args: map[string]string{
			"MEMBRANE_URI":     fetchFrom,
			"MEMBRANE_VERSION": membraneVersion,
			"BASE_IMAGE":       config.ImageName,
		},
	}, nil
}
