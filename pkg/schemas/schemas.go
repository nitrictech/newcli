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

package schemas

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"os"
	"path/filepath"

	"github.com/nitrictech/cli/pkg/paths"
	"github.com/nitrictech/cli/pkg/version"
)

//go:embed nitric-yaml-schema.json
var nitricYamlSchemaTemplate string

// NewProvider - Returns a new provider instance based on the given providerId string
// The providerId string is in the form of <org-name>/<provider-name>@<version>
func Install() error {
	currentVersion := version.Version
	dir := paths.NitricSchemasDir()
	filePath := filepath.Join(dir, "nitric-yaml-schema.json")
	versionFilePath := filepath.Join(dir, "version.lock")

	// Ensure the Nitric Schemas Directory Exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err := os.MkdirAll(dir, 0o700)
		if err != nil {
			return fmt.Errorf("failed to create nitric schemas directory. %w", err)
		}
	}

	// Read the existing version from the version file, if it exists
	storedVersion, err := os.ReadFile(versionFilePath)
	if err == nil {
		// Remove trailing newline for comparison
		storedVersion = bytes.TrimSpace(storedVersion)
	}

	// Check if the stored version matches the current version
	if string(storedVersion) == currentVersion {
		// Versions are the same, no need to update
		return nil
	}

	// Prepare the template with the current version
	tmpl, err := template.New("schema").Parse(nitricYamlSchemaTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse nitric schema template: %w", err)
	}

	var content bytes.Buffer

	err = tmpl.Execute(&content, struct {
		Version string
	}{
		Version: currentVersion,
	})
	if err != nil {
		return fmt.Errorf("failed to execute template for nitric schema file: %w", err)
	}

	// Write the new content to the schema file
	err = os.WriteFile(filePath, content.Bytes(), 0o644)
	if err != nil {
		return fmt.Errorf("failed to write nitric schema file: %w", err)
	}

	// Write the new version lock
	err = os.WriteFile(versionFilePath, []byte(currentVersion+"\n"), 0o644)
	if err != nil {
		return fmt.Errorf("failed to write nitric schema version lock file: %w", err)
	}

	return nil
}
