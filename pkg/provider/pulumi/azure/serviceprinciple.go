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

package azure

import (
	"github.com/pulumi/pulumi-azuread/sdk/v5/go/azuread"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type ServicePrincipleArgs struct {
	SubscriptionID    pulumi.StringInput
	ResourceGroupName pulumi.StringInput
}

type ServicePrinciple struct {
	pulumi.ResourceState

	Name               string
	ClientID           pulumi.StringOutput
	TenantID           pulumi.StringOutput
	ServicePrincipalId pulumi.StringOutput
	ClientSecret       pulumi.StringOutput
}

func newServicePrinciple(ctx *pulumi.Context, name string, args *ServicePrincipleArgs, opts ...pulumi.ResourceOption) (*ServicePrinciple, error) {
	res := &ServicePrinciple{Name: name}
	err := ctx.RegisterComponentResource("nitric:principal:AzureAD", name, res, opts...)
	if err != nil {
		return nil, err
	}

	// create an application per service principal
	app, err := azuread.NewApplication(ctx, resourceName(ctx, name, ADApplicationRT), &azuread.ApplicationArgs{
		DisplayName: pulumi.String(name + "App"),
		//Tags:        common.Tags(ctx, name+"App"),
	}, pulumi.Parent(res))
	if err != nil {
		return nil, err
	}
	res.ClientID = app.ApplicationId

	sp, err := azuread.NewServicePrincipal(ctx, resourceName(ctx, name, ADServicePrincipalRT), &azuread.ServicePrincipalArgs{
		ApplicationId: app.ApplicationId,
	}, pulumi.Parent(res))
	if err != nil {
		return nil, err
	}
	res.TenantID = sp.ApplicationTenantId
	res.ServicePrincipalId = pulumi.StringOutput(sp.ID())

	if err != nil {
		return nil, err
	}

	spPwd, err := azuread.NewServicePrincipalPassword(ctx, resourceName(ctx, name, ADServicePrincipalPasswordRT), &azuread.ServicePrincipalPasswordArgs{
		ServicePrincipalId: sp.ID().ToStringOutput(),
	}, pulumi.Parent(res))
	if err != nil {
		return nil, err
	}
	res.ClientSecret = spPwd.Value

	return res, ctx.RegisterResourceOutputs(res, pulumi.Map{
		"name":               pulumi.StringPtr(res.Name),
		"tenantID":           res.TenantID,
		"clientID":           res.ClientID,
		"clientSecret":       res.ClientSecret,
		"servicePrincipalId": res.ServicePrincipalId,
	})
}
