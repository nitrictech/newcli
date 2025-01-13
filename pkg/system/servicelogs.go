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

package system

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/nitrictech/cli/pkg/paths"
)

type LogEntry struct {
	Timestamp string       `json:"time"`
	Level     logrus.Level `json:"level"`
	Message   string       `json:"msg"`
	Origin    string       `json:"origin"`
}

// ServiceLogger struct to encapsulate the logger and file path
type ServiceLogger struct {
	Logger      *logrus.Logger
	LogFilePath string
}

var (
	serviceLogsInstance *ServiceLogger
	serviceLogsOnce     sync.Once
	logFilePath         string
)

// InitializeLogger sets the file path for the singleton instance
// This must be called before the singleton is accessed.
func InitializeServiceLogger(stackFilePath string) {
	logFilePath = paths.NitricServiceLogFile(stackFilePath)
}

// GetServiceLogger retrieves the singleton instance of the ServiceLogger
func GetServiceLogger() *ServiceLogger {
	serviceLogsOnce.Do(func() {
		if logFilePath == "" {
			panic("InitializeLogger must be called before accessing the logger")
		}

		logger := logrus.New()
		logger.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: "2006-01-02T15:04:05.000Z07:00", // Format with milliseconds
		})

		serviceLogsInstance = &ServiceLogger{
			Logger:      logger,
			LogFilePath: logFilePath,
		}
	})

	return serviceLogsInstance
}

// WriteLog writes a log entry with the specified level and message
func (s *ServiceLogger) WriteLog(level logrus.Level, message, origin string) {
	// Do not write empty log messages
	if message == "" {
		return
	}

	// Open the log file when writing a log entry
	file, err := os.OpenFile(s.LogFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Printf("Error writing log for origin '%s': %v\n", origin, err)
	}
	defer file.Close() // Ensure the file is closed after writing

	// Set the output of the logger to the file
	s.Logger.SetOutput(file)

	s.Logger.WithFields(logrus.Fields{
		"origin": origin,
	}).Log(level, message)
}

// ReadLogs reads the log file from the service's log file path and returns a slice of LogEntry objects
func ReadLogs() ([]LogEntry, error) {
	// Open the log file for reading
	file, err := os.Open(GetServiceLogger().LogFilePath)
	if err != nil {
		return nil, fmt.Errorf("could not open log file: %w", err)
	}
	defer file.Close() // Ensure the file is closed when the function finishes

	var logs []LogEntry

	// Read the file line by line
	decoder := json.NewDecoder(file)

	for {
		var log LogEntry
		if err := decoder.Decode(&log); err != nil {
			if err.Error() == "EOF" {
				break // End of file reached
			}

			return nil, fmt.Errorf("error decoding log entry: %w", err)
		}

		logs = append(logs, log)
	}

	return logs, nil
}

// PurgeLogs truncates the log file to remove all log entries
func PurgeLogs() error {
	file, err := os.OpenFile(GetServiceLogger().LogFilePath, os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("could not purge log file: %w", err)
	}
	defer file.Close()

	return nil
}
