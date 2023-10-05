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
	"fmt"
	"io"
	"sync"

	"github.com/pkg/errors"
	"github.com/pterm/pterm"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/nitrictech/cli/pkg/output"
	"github.com/nitrictech/cli/pkg/provider/types"
	deploy "github.com/nitrictech/nitric/core/pkg/api/nitric/deploy/v1"
)

type remoteDeployment struct {
	cfc            types.ConfigFromCode
	sfc            *StackConfig
	address        string
	interactive    bool
	subscriberLock sync.RWMutex
	subscribers    []chan types.Event
}

var _ types.Provider = &remoteDeployment{}

func (p *remoteDeployment) AskAndSave() error {
	return errors.New("not supported on remote deployment servers")
}

func (p *remoteDeployment) SupportedRegions() []string {
	return []string{}
}

func (p *remoteDeployment) List() (interface{}, error) {
	return nil, errors.New("not supported for remote deployments")
}

func (p *remoteDeployment) Subscribe(sub chan types.Event) {
	p.subscriberLock.Lock()
	defer p.subscriberLock.Unlock()

	p.subscribers = append(p.subscribers, sub)
}

func (p *remoteDeployment) notifySubscribers(evt types.Event) {
	p.subscriberLock.RLock()
	defer p.subscriberLock.RUnlock()
	for _, sub := range p.subscribers {
		sub <- evt
	}
}

func (a *remoteDeployment) Preview(log output.Progress) (string, error) {
	return "", errors.New("not supported for remote deployments")
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

	for {
		evt, err := op.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return res, nil
			}

			return res, err
		}

		switch t := evt.Content.(type) {
		case *deploy.DeployUpEvent_Message:
			p.notifySubscribers(types.Event{
				Progress: &types.ProgressMessage{
					Content: t.Message.Message,
				},
			})
		case *deploy.DeployUpEvent_Result:
			p.notifySubscribers(types.Event{
				Result: &types.ResultMessage{
					Content: t.Result.Result.GetStringResult(),
					Success: t.Result.Success,
				},
			})

			if !t.Result.Success {
				return res, errors.New("failed to deploy")
			}

			// Print the deployment output
			pterm.Success.Print(t.Result.Result.GetStringResult())

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

	for {
		evt, err := op.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return res, nil
			}

			return res, err
		}

		switch t := evt.Content.(type) {
		case *deploy.DeployDownEvent_Message:
			fmt.Print(t.Message.Message)
		case *deploy.DeployDownEvent_Result: // TODO - handle errors
			return res, nil
		}
	}
}
