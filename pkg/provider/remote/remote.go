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
	"context"
	"io"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/nitrictech/cli/pkg/output"
	"github.com/nitrictech/cli/pkg/provider/types"
	deploy "github.com/nitrictech/nitric/core/pkg/api/nitric/deploy/v1"
)

type remoteDeployment struct {
	cfc         types.ConfigFromCode
	sfc         *StackConfig
	address     string
	interactive bool
}

var _ types.Provider = &remoteDeployment{}

func (p *remoteDeployment) AskAndSave() error {
	return errors.New("not supported on remote deployment servers")
}

func (p *remoteDeployment) ToFile() error {
	return errors.New("not supported on remote deployment servers")
}

func (p *remoteDeployment) SetStackConfigProp(key string, value any) {
	p.sfc.Props[key] = value
}

func (p *remoteDeployment) SupportedRegions() []types.RegionItem {
	return []types.RegionItem{}
}

func (p *remoteDeployment) List() (interface{}, error) {
	return nil, errors.New("not supported for remote deployments")
}

func (p *remoteDeployment) Preview() (*types.Summary, error) {
	conn, err := p.dialConnection()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	req, err := p.cfc.ToPreviewRequest()
	if err != nil {
		return nil, err
	}

	req.Interactive = p.interactive

	attributes := map[string]any{}

	attributes["project"] = p.cfc.ProjectName()
	attributes["stack"] = p.sfc.Name

	for k, v := range p.sfc.Props {
		attributes[k] = v
	}

	req.Attributes, err = structpb.NewStruct(attributes)
	if err != nil {
		return nil, err
	}

	client := deploy.NewDeployServiceClient(conn)

	op, err := client.Preview(context.Background(), req)
	if err != nil {
		return nil, err
	}

	res := &types.Summary{}

	model, err := NewOutputModel()
	if err != nil {
		return nil, err
	}

	program := tea.NewProgram(model)

	go func() {
		_, err = program.Run()
		if err != nil {
			return
		}
	}()

	for {
		evt, err := op.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return res, nil
			}

			return res, nil
		}

		program.Send(evt.Content)

		eventResult, ok := evt.Content.(*deploy.DeployPreviewEvent_Result)
		if ok {
			if !eventResult.Result.Success {
				return res, errors.New("deployment failed")
			}

			return res, nil
		}
	}
}

func (p *remoteDeployment) dialConnection() (*grpc.ClientConn, error) {
	if p.address == "" {
		p.address = "127.0.0.1:50051"
	}

	conn, err := grpc.Dial(p.address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return nil, err
	}

	return conn, nil
}

func (p *remoteDeployment) Up() (*types.Deployment, error) {
	conn, err := p.dialConnection()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	req, err := p.cfc.ToUpRequest()
	if err != nil {
		return nil, err
	}

	req.Interactive = p.interactive

	attributes := map[string]any{}

	attributes["project"] = p.cfc.ProjectName()
	attributes["stack"] = p.sfc.Name

	for k, v := range p.sfc.Props {
		attributes[k] = v
	}

	req.Attributes, err = structpb.NewStruct(attributes)
	if err != nil {
		return nil, err
	}

	client := deploy.NewDeployServiceClient(conn)

	op, err := client.Up(context.Background(), req)
	if err != nil {
		return nil, err
	}

	res := &types.Deployment{
		Summary:      &types.Summary{},
		ApiEndpoints: map[string]string{},
	}

	model, err := NewOutputModel()
	if err != nil {
		return nil, err
	}

	program := tea.NewProgram(model)

	go func() {
		_, err = program.Run()
		if err != nil {
			return
		}
	}()

	for {
		evt, err := op.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return res, nil
			}

			return res, nil
		}

		program.Send(evt.Content)

		eventResult, ok := evt.Content.(*deploy.DeployUpEvent_Result)
		if ok {
			if !eventResult.Result.Success {
				return res, errors.New("deployment failed")
			}

			return res, nil
		}
	}
}

func (p *remoteDeployment) Down() (*types.Summary, error) {
	conn, err := p.dialConnection()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	attributes := map[string]any{}

	attributes["project"] = p.cfc.ProjectName()
	attributes["stack"] = p.sfc.Name

	for k, v := range p.sfc.Props {
		attributes[k] = v
	}

	reqAttributes, err := structpb.NewStruct(attributes)
	if err != nil {
		return nil, err
	}

	req := &deploy.DeployDownRequest{
		Attributes:  reqAttributes,
		Interactive: p.interactive,
	}

	client := deploy.NewDeployServiceClient(conn)

	op, err := client.Down(context.Background(), req)
	if err != nil {
		return nil, err
	}

	res := &types.Summary{}

	model, err := NewOutputModel()
	if err != nil {
		return nil, err
	}

	program := tea.NewProgram(model)

	go func() {
		_, err = program.Run()
		if err != nil {
			return
		}
	}()

	for {
		evt, err := op.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return res, nil
			}

			return res, nil
		}

		program.Send(evt.Content)

		_, ok := evt.Content.(*deploy.DeployDownEvent_Result)
		if ok {
			return res, nil
		}
	}
}
