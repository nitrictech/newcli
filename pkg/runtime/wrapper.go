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

	"github.com/nitrictech/cli/pkg/containerengine"
)

var (
	//go:embed wrapper.dockerfile
	wrapperDockerFile string
	//go:embed wrapper-telemetry.dockerfile
	wrapperTelemetryDockerFile string
	//go:embed otel-collector.yaml
	otelCollectorTemplate string
)

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
	Extensions           []string
}

func telemetryConfig(provider string) (string, error) {
	t := template.Must(template.New("otelconfig").Parse(otelCollectorTemplate))
	config := &strings.Builder{}

	switch provider {
	case "aws":
		err := t.Execute(config, &otelConfig{
			TraceName:  "awsxray",
			MetricName: "awsemf",
			Extensions: []string{},
		})
		if err != nil {
			return "", err
		}

	case "gcp":
		err := t.Execute(config, &otelConfig{
			TraceName:           "googlecloud",
			MetricName:          "googlecloud",
			TraceExporterConfig: `{"retry_on_failure": {"enabled": false}}`,
			Extensions:          []string{},
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
	Telemetry            int
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

	fetchFrom := fmt.Sprintf("https://github.com/nitrictech/nitric/releases/download/%s/membrane-%s", config.MembraneVersion, config.Provider)

	if config.Telemetry > 0 {
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
			Dockerfile: fmt.Sprintf(wrapperTelemetryDockerFile, strings.Join(cmd, ",")),
			Args: map[string]string{
				"MEMBRANE_URI":                fetchFrom,
				"MEMBRANE_VERSION":            config.MembraneVersion,
				"BASE_IMAGE":                  config.ImageName,
				"OTELCOL_CONFIG":              relConfig,
				"OTELCOL_CONTRIB_URI":         otelCollectorVer,
				"NITRIC_TRACE_SAMPLE_PERCENT": fmt.Sprint(config.Telemetry),
			},
		}, nil
	}

	return &WrappedBuildInput{
		Dockerfile: fmt.Sprintf(wrapperDockerFile, strings.Join(cmd, ",")),
		Args: map[string]string{
			"MEMBRANE_URI":     fetchFrom,
			"MEMBRANE_VERSION": config.MembraneVersion,
			"BASE_IMAGE":       config.ImageName,
		},
	}, nil
}
