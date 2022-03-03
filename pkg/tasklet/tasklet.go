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

package tasklet

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/nitrictech/cli/pkg/output"
	"github.com/pterm/pterm"
)

type TaskletFn func(output.Progress) error

type Runner struct {
	Runner     TaskletFn
	TaskletCtx TaskletContext
	StartMsg   string
	StopMsg    string
}

type Opts struct {
	Signal        chan os.Signal
	Timeout       time.Duration
	SuccessPrefix string
}

func MustRun(runner Runner, opts Opts) {
	if Run(runner, opts) != nil {
		os.Exit(1)
	}
}

func Run(runner Runner, opts Opts) error {

	if runner.TaskletCtx == nil {
		// Default to a spinner context
		runner.TaskletCtx = NewSpinnerContext(runner.StartMsg)
	}

	err := runner.TaskletCtx.Start()
	if err != nil {
		return err
	}
	defer func() {
		_ = runner.TaskletCtx.Stop()
	}()

	if ctx, ok := runner.TaskletCtx.(*taskletSpinnerContext); ok && opts.SuccessPrefix != "" {
		ctx.spinner.SuccessPrinter = &pterm.PrefixPrinter{
			MessageStyle: &pterm.ThemeDefault.SuccessMessageStyle,
			Prefix: pterm.Prefix{
				Style: &pterm.ThemeDefault.SuccessPrefixStyle,
				Text:  opts.SuccessPrefix,
			},
		}
	}

	start := time.Now()
	done := make(chan bool, 1)
	doErr := make(chan error, 1)

	if opts.Timeout == 0 {
		opts.Timeout = time.Hour // our infinite
	}
	timer := time.NewTimer(opts.Timeout)

	go func() {
		err = runner.Runner(runner.TaskletCtx)
		if err != nil {
			doErr <- err
		}
		done <- true
	}()
	select {
	case err = <-doErr:
	case <-timer.C:
		err = errors.New("tasklet timedout after " + opts.Timeout.String())
	case <-done:
	case <-opts.Signal:
		fmt.Println("Shutting down services - exiting")
	}

	elapsed := time.Since(start)
	if elapsed < time.Second {
		time.Sleep(time.Second - elapsed)
	}

	if ctx, ok := runner.TaskletCtx.(*taskletSpinnerContext); err != nil && ok {
		ctx.spinner.Fail(err)
		return err
	}

	if ctx, ok := runner.TaskletCtx.(*taskletSpinnerContext); err != nil && ok {
		ctx.spinner.SuccessPrinter.Printf("%s (%s)", runner.StopMsg, elapsed.Round(time.Second).String())
	}

	return nil
}
