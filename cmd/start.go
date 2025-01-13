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

package cmd

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"github.com/nitrictech/cli/pkg/cloud"
	"github.com/nitrictech/cli/pkg/cloud/gateway"
	"github.com/nitrictech/cli/pkg/dashboard"
	"github.com/nitrictech/cli/pkg/env"
	"github.com/nitrictech/cli/pkg/paths"
	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/system"
	"github.com/nitrictech/cli/pkg/view/tui"
	"github.com/nitrictech/cli/pkg/view/tui/commands/local"
	"github.com/nitrictech/cli/pkg/view/tui/commands/services"
	"github.com/nitrictech/cli/pkg/view/tui/fragments"
	"github.com/nitrictech/cli/pkg/view/tui/teax"
)

var (
	startNoBrowser bool
	enableHttps    bool
)

// generateSelfSignedCert generates a self-signed X.509 certificate and returns the PEM-encoded certificate and private key
func generateSelfSignedCert() ([]byte, []byte, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	notBefore := time.Now()
	notAfter := notBefore.Add(365 * 24 * time.Hour)

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Acme Co"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	privBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes})

	return certPEM, keyPEM, nil
}

func createTlsCredentialsIfNotPresent(fs afero.Fs, projectDir string) {
	certPath := paths.NitricTlsCertFile(projectDir)
	keyPath := paths.NitricTlsKeyFile(projectDir)

	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		certPEM, keyPEM, err := generateSelfSignedCert()
		tui.CheckErr(err)

		// Make sure the credentials directory exists
		err = fs.MkdirAll(paths.NitricTlsCredentialsPath(projectDir), 0o700)
		tui.CheckErr(err)

		err = afero.WriteFile(fs, certPath, certPEM, 0o600)
		tui.CheckErr(err)

		err = afero.WriteFile(fs, keyPath, keyPEM, 0o600)
		tui.CheckErr(err)
	}
}

var startCmd = &cobra.Command{
	Use:         "start",
	Short:       "Run nitric services locally for development and testing",
	Long:        `Run nitric services locally for development and testing`,
	Example:     `nitric start`,
	Annotations: map[string]string{"commonCommand": "yes"},
	RunE: func(cmd *cobra.Command, args []string) error {
		// Divert default log output to pterm debug
		// log.SetOutput(output.NewPtermWriter(pterm.Debug))
		// log.SetFlags(0)
		fs := afero.NewOsFs()

		proj, err := project.FromFile(fs, "")
		tui.CheckErr(err)

		fmt.Print(fragments.NitricTag())
		fmt.Println(" start")
		fmt.Println()

		additionalEnvFiles := []string{}

		if envFile != "" {
			additionalEnvFiles = append(additionalEnvFiles, envFile)
		}

		localEnv, err := env.ReadLocalEnv(additionalEnvFiles...)
		if err != nil && !os.IsNotExist(err) {
			tui.CheckErr(err)
		}

		var tlsCredentials *gateway.TLSCredentials
		if enableHttps {
			createTlsCredentialsIfNotPresent(fs, proj.Directory)
			tlsCredentials = &gateway.TLSCredentials{
				CertFile: paths.NitricTlsCertFile(proj.Directory),
				KeyFile:  paths.NitricTlsKeyFile(proj.Directory),
			}
		}

		logFilePath, err := paths.NewNitricLogFile(proj.Directory)
		tui.CheckErr(err)

		logWriter, err := fs.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		tui.CheckErr(err)
		defer logWriter.Close()

		// Initialize the system service log logger
		system.InitializeServiceLogger(proj.Directory)

		teaOptions := []tea.ProgramOption{}
		if isNonInteractive() {
			teaOptions = append(teaOptions, tea.WithoutRenderer(), tea.WithInput(nil))
		}

		runView := teax.NewProgram(local.NewLocalCloudStartModel(isNonInteractive()), teaOptions...)

		var localCloud *cloud.LocalCloud
		go func() {
			// Start the local cloud service analogues
			localCloud, err = cloud.New(proj.Name, cloud.LocalCloudOptions{
				TLSCredentials:  tlsCredentials,
				LogWriter:       logWriter,
				LocalConfig:     proj.LocalConfig,
				MigrationRunner: project.BuildAndRunMigrations,
				LocalCloudMode:  cloud.LocalCloudModeStart,
			})
			tui.CheckErr(err)
			runView.Send(local.LocalCloudStartStatusMsg{Status: local.Done})
		}()

		_, err = runView.Run()
		tui.CheckErr(err)

		// Start dashboard
		dash, err := dashboard.New(startNoBrowser, localCloud, proj)
		tui.CheckErr(err)

		err = dash.Start()
		tui.CheckErr(err)

		bold := lipgloss.NewStyle().Bold(true).Foreground(tui.Colors.Purple)
		numServices := fmt.Sprintf("%d", len(proj.GetServices()))

		fmt.Print("found ")
		fmt.Print(bold.Render(numServices))
		fmt.Print(" services in project\n")

		// Run the app code (project services)
		stopChan := make(chan bool)
		updatesChan := make(chan project.ServiceRunUpdate)

		// panic recovery for local cloud
		// gracefully stop the local cloud in the case of a panic
		defer func() {
			if r := recover(); r != nil {
				localCloud.Stop()
			}
		}()

		go func() {
			err := proj.RunServicesWithCommand(localCloud, stopChan, updatesChan, localEnv)
			if err != nil {
				localCloud.Stop()
				tui.CheckErr(err)
			}
		}()
		// FIXME: Duplicate code
		go func() {
			err := proj.RunBatchesWithCommand(localCloud, stopChan, updatesChan, localEnv)
			if err != nil {
				localCloud.Stop()

				tui.CheckErr(err)
			}
		}()

		// FIXME: This is a hack to get labelled logs into the TUI
		// We should refactor the system logs to be more generic
		systemChan := make(chan project.ServiceRunUpdate)
		system.SubscribeToLogs(func(msg string) {
			systemChan <- project.ServiceRunUpdate{
				ServiceName: "nitric",
				Label:       "nitric",
				Status:      project.ServiceRunStatus_Running,
				Message:     msg,
			}
		})

		allUpdates := lo.FanIn(10, updatesChan, systemChan)

		// non-interactive environment
		if isNonInteractive() {
			go func() {
				sigChan := make(chan os.Signal, 1)
				signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

				// Wait for a signal
				<-sigChan

				fmt.Println("Stopping local cloud")

				localCloud.Stop()

				// Send stop signal to stopChan
				close(stopChan)
			}()

			logger := system.GetServiceLogger()

			for {
				select {
				case update := <-allUpdates:
					fmt.Printf("%s [%s]: %s", update.ServiceName, update.Status, update.Message)
					// Write log to file
					level := logrus.InfoLevel

					if update.Status == project.ServiceRunStatus_Error {
						level = logrus.ErrorLevel
					}

					logger.WriteLog(level, update.Message, update.Label)
				case <-stopChan:
					fmt.Println("Shutting down services - exiting")
					return nil
				}
			}
		} else {
			// interactive environment
			runView := teax.NewProgram(services.NewModel(stopChan, allUpdates, localCloud, dash.GetDashboardUrl()))

			_, err = runView.Run()
			tui.CheckErr(err)

			localCloud.Stop()
		}

		return nil
	},
	Args: cobra.ExactArgs(0),
}

func init() {
	startCmd.Flags().StringVarP(&envFile, "env-file", "e", "", "--env-file config/.my-env")
	startCmd.Flags().BoolVar(&enableHttps, "https-preview", false, "enable https support for local APIs (preview feature)")
	startCmd.PersistentFlags().BoolVar(
		&startNoBrowser,
		"no-browser",
		false,
		"disable browser opening for local dashboard, note: in CI mode the browser opening feature is disabled",
	)

	rootCmd.AddCommand(startCmd)
}
