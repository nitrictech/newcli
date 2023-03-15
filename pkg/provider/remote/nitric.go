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

package remote

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/hashicorp/go-getter"

	"github.com/nitrictech/cli/pkg/provider/types"
)

type nitricDeployment struct {
	*binaryRemoteDeployment
}

// Gets a default provider string and translates it into a file name that can be retrieved from
// our github releases
func providerFileName(prov *provider) string {
	// Get the OS name
	os := runtime.GOOS
	platform := runtime.GOARCH

	// tarballs are the default archive type
	archive := "tar.gz"
	if os == "windows" {
		// We use zips for windows
		archive = "zip"
	}

	if platform == "amd64" {
		platform = "x86_64"
	}

	// Return the archive uri in the form of
	// {PROVIDER}_{OS}_{PLATFORM}.{ARCHIVE}
	// e.g. gcp_linux_x86_64.tar.gz
	return strings.ToLower(fmt.Sprintf("%s_%s_%s.%s", prov.name, os, platform, archive))
}

func defaultDownloadUri(prov *provider) string {
	fileName := providerFileName(prov)

	if prov.version == "latest" {
		return fmt.Sprintf("https://github.com/nitrictech/nitric/releases/latest/download/%s", fileName)
	}

	return fmt.Sprintf("https://github.com/nitrictech/nitric/releases/download/v%s/%s", prov.version, fileName)
}

func checkProvider(prov *provider) (string, error) {
	// Check to see if the provider already exists
	provFile := providerFilePath(prov)

	// Check if the provider we're after actually exists already
	_, err := os.Stat(provFile)
	if err != nil {
		if err := getter.GetFile(provFile, defaultDownloadUri(prov)); err != nil {
			return "", fmt.Errorf("error downloading file %s (%w)", defaultDownloadUri(prov), err)
		}
	}

	return provFile, nil
}

func NewNitricDeployment(cfc types.ConfigFromCode, sc *StackConfig, prov *provider, envMap map[string]string, opts *types.ProviderOpts) (types.Provider, error) {
	// Check and validate that the nitric provider exists before creating a new binary provider
	// This will also attempt to automatically retrieve the provider if it doesn't already exist
	_, err := checkProvider(prov)
	if err != nil {
		return nil, err
	}

	baseBinaryDeployment, err := NewBinaryRemoteDeployment(cfc, sc, prov, envMap, opts)
	if err != nil {
		return nil, err
	}

	baseNitricDeployment := &nitricDeployment{binaryRemoteDeployment: baseBinaryDeployment}

	switch prov.name {
	case "aws":
		return &awsProvider{
			nitricDeployment: baseNitricDeployment,
		}, nil
	case "gcp":
		return &gcpProvider{
			nitricDeployment: baseNitricDeployment,
		}, nil
	case "azure":
		return &azureProvider{
			nitricDeployment: baseNitricDeployment,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported nitric provider %s", prov.name)
	}
}
