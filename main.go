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

package main

import (
	"fmt"

	"github.com/nitrictech/cli/pkg/cmd"
	"github.com/nitrictech/cli/pkg/ghissue"
	"github.com/pterm/pterm"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			// We've recovered from a panic, let's provide the stack but a cleaner error message
			link := ghissue.BugLink(fmt.Errorf("an unhandled panic occurred"))

			pterm.Error.Printfln("Looks like you've hit a bug in the nitric CLI, you can raise an issue for the using the following link: %s", link)
		}
	}()

	cmd.Execute()
}
