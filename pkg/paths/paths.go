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

package paths

import (
	"errors"
	"fmt"
	"go/build"
	"log"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// slashSplitter - used to split strings, with the same output regardless of leading or trailing slashes
// e.g - strings.FieldsFunc("/one/two/three/", f) == strings.FieldsFunc("/one/two/three", f) == strings.FieldsFunc("one/two/three", f) == ["one" "two" "three"]
func slashSplitter(c rune) bool {
	return c == '/'
}

// SplitPath - splits a path into its component parts, ignoring leading or trailing slashes.
// e.g - SplitPath("/one/two/three/") == SplitPath("/one/two/three") == SplitPath("one/two/three") == ["one" "two" "three"]
func SplitPath(p string) []string {
	return strings.FieldsFunc(p, slashSplitter)
}

// NitricHomeDir gets the nitric home directory
func NitricHomeDir() string {
	nitricHomeEnv := os.Getenv("NITRIC_HOME")
	if nitricHomeEnv != "" {
		return nitricHomeEnv
	}

	dirname, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}

	return filepath.Join(dirname, ".nitric")
}

// NitricProviderDir returns the directory to place provider deployment binaries.
func NitricProviderDir() string {
	return filepath.Join(NitricHomeDir(), "providers")
}

// NitricTemplatesDir returns the directory to place template related data.
func NitricTemplatesDir() string {
	return filepath.Join(NitricHomeDir(), "store")
}

func NitricStacksDir() (string, error) {
	homeDir := NitricHomeDir()
	stacksDir := path.Join(homeDir, "stacks")

	// ensure .nitric exists
	err := os.MkdirAll(stacksDir, os.ModePerm)
	if err != nil {
		return "", err
	}

	return stacksDir, nil
}

// NitricConfigDir returns the directory to find configuration.
func NitricConfigDir() string {
	if runtime.GOOS == "linux" {
		dirname, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}

		return filepath.Join(dirname, ".config", "nitric")
	}

	return NitricHomeDir()
}

func NitricLocalPassphrasePath() string {
	return filepath.Join(NitricHomeDir(), ".local-stack-pass")
}

// NitricTmpDir returns the directory to find temporary files for a project.
func NitricTmpDir(stackPath string) string {
	return filepath.Join(stackPath, ".nitric")
}

// NewNitricLogFile returns a path to a unique log file that does not exist.
func NewNitricLogFile(stackPath string) (string, error) {
	logDir := NitricTmpDir(stackPath)

	// ensure .nitric exists
	err := os.MkdirAll(logDir, os.ModePerm)
	if err != nil {
		return "", err
	}

	tf, err := os.CreateTemp(logDir, "run-*.log")
	if err != nil {
		return "", err
	}

	tf.Close()

	return tf.Name(), nil
}

func NitricServiceLogFile(stackPath string) string {
	return filepath.Join(NitricTmpDir(stackPath), "./services.log")
}

func NitricTlsCredentialsPath(stackPath string) string {
	return filepath.Join(NitricTmpDir(stackPath), "./tls")
}

func NitricTlsCertFile(stackPath string) string {
	return filepath.Join(NitricTlsCredentialsPath(stackPath), "./cert.pem")
}

func NitricTlsKeyFile(stackPath string) string {
	return filepath.Join(NitricTlsCredentialsPath(stackPath), "./key.pem")
}

// NitricHistoryFile returns a path to a request history file, making one if it doesn't exist
func NitricHistoryFile(stackPath string, historyType string) (string, error) {
	logDir := NitricTmpDir(stackPath)

	fileName := filepath.Clean(fmt.Sprintf("%s/history-%s.json", logDir, historyType))

	// ensure .nitric exists
	err := os.MkdirAll(logDir, os.ModePerm)
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(fileName); errors.Is(err, os.ErrNotExist) {
		tf, err := os.Create(fileName)
		if err != nil {
			return "", err
		}

		_, err = tf.Write([]byte("[]"))
		if err != nil {
			return "", err
		}

		tf.Close()
	}

	return fileName, nil
}

func GoPath() (string, error) {
	goPath := os.Getenv("GOPATH")
	if goPath == "" {
		goPath = build.Default.GOPATH
	}

	return goPath, nil
}
