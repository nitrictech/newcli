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
	"context"
	_ "embed"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/golangci/golangci-lint/pkg/sliceutil"
	multierror "github.com/missionMeteora/toolkit/errors"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-azure-native/sdk/go/azure/authorization"
	"github.com/pulumi/pulumi-azure-native/sdk/go/azure/eventgrid"
	"github.com/pulumi/pulumi-azure-native/sdk/go/azure/keyvault"
	"github.com/pulumi/pulumi-azure-native/sdk/go/azure/resources"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/provider/pulumi/common"
	"github.com/nitrictech/cli/pkg/stack"
	"github.com/nitrictech/cli/pkg/utils"
)

type azureProvider struct {
	proj       *project.Project
	sc         *stack.Config
	envMap     map[string]string
	tmpDir     string
	org        string
	adminEmail string
}

var (
	//go:embed pulumi-azure-version.txt
	azurePluginVersion string
	//go:embed pulumi-azuread-version.txt
	azureADPluginVersion string
	//go:embed pulumi-azure-native-version.txt
	azureNativePluginVersion string
)

func New(s *project.Project, t *stack.Config, envMap map[string]string) common.PulumiProvider {
	return &azureProvider{
		proj:   s,
		sc:     t,
		envMap: envMap,
	}
}

func (a *azureProvider) Plugins() []common.Plugin {
	return []common.Plugin{
		{
			Name:    "azure-native",
			Version: strings.TrimSpace(azureNativePluginVersion),
		},
		{
			Name:    "azure",
			Version: strings.TrimSpace(azurePluginVersion),
		},
		{
			Name:    "azuread",
			Version: strings.TrimSpace(azureADPluginVersion),
		},
	}
}

func (a *azureProvider) Ask() (*stack.Config, error) {
	answers := struct {
		Region     string
		Org        string
		AdminEmail string
	}{}
	qs := []*survey.Question{
		{
			Name: "region",
			Prompt: &survey.Select{
				Message: "select the region",
				Options: a.SupportedRegions(),
			},
		},
		{
			Name: "org",
			Prompt: &survey.Input{
				Message: "Provide the organisation to associate with the API",
			},
		},
		{
			Name: "adminEmail",
			Prompt: &survey.Input{
				Message: "Provide the adminEmail to associate with the API",
			},
		},
	}
	sc := &stack.Config{
		Name:     a.sc.Name,
		Provider: a.sc.Provider,
		Extra:    map[string]interface{}{},
	}

	err := survey.Ask(qs, &answers)
	if err != nil {
		return nil, err
	}

	sc.Region = answers.Region
	sc.Extra["adminemail"] = answers.AdminEmail
	sc.Extra["org"] = answers.Org

	return sc, nil
}

func (a *azureProvider) SupportedRegions() []string {
	return []string{
		"eastus2",
	}
}

func (a *azureProvider) Validate() error {
	errList := &multierror.ErrorList{}

	if a.sc.Region == "" {
		errList.Push(fmt.Errorf("target %s requires \"region\"", a.sc.Provider))
	} else if !sliceutil.Contains(a.SupportedRegions(), a.sc.Region) {
		errList.Push(utils.NewNotSupportedErr(fmt.Sprintf("region %s not supported on provider %s", a.sc.Region, a.sc.Provider)))
	}

	if _, ok := a.sc.Extra["org"]; !ok {
		errList.Push(fmt.Errorf("target %s requires \"org\"", a.sc.Provider))
	} else {
		a.org = a.sc.Extra["org"].(string)
	}

	if _, ok := a.sc.Extra["adminemail"]; !ok {
		errList.Push(fmt.Errorf("target %s requires \"adminemail\"", a.sc.Provider))
	} else {
		a.adminEmail = a.sc.Extra["adminemail"].(string)
	}

	return errList.Err()
}

func (a *azureProvider) Configure(ctx context.Context, autoStack *auto.Stack) error {
	if a.sc.Region != "" {
		err := autoStack.SetConfig(ctx, "azure:location", auto.ConfigValue{Value: a.sc.Region})
		if err != nil {
			return err
		}
		err = autoStack.SetConfig(ctx, "azure-native:location", auto.ConfigValue{Value: a.sc.Region})
		if err != nil {
			return err
		}
		return nil
	}
	region, err := autoStack.GetConfig(ctx, "azure-native:location")
	if err != nil {
		return err
	}
	a.sc.Region = region.Value
	return nil
}

func (a *azureProvider) TryPullImages() error {
	return nil
}

func (a *azureProvider) Deploy(ctx *pulumi.Context) error {
	var err error
	a.tmpDir, err = ioutil.TempDir("", ctx.Stack()+"-*")
	if err != nil {
		return err
	}

	clientConfig, err := authorization.GetClientConfig(ctx)
	if err != nil {
		return err
	}

	rg, err := resources.NewResourceGroup(ctx, resourceName(ctx, "", ResourceGroupRT), &resources.ResourceGroupArgs{
		Location: pulumi.String(a.sc.Region),
		Tags:     common.Tags(ctx, ctx.Stack()),
	})

	if err != nil {
		return errors.WithMessage(err, "resource group create")
	}

	contAppsArgs := &ContainerAppsArgs{
		ResourceGroupName: rg.Name,
		Location:          rg.Location,
		SubscriptionID:    pulumi.String(clientConfig.SubscriptionId),
		Topics:            map[string]*eventgrid.Topic{},
		EnvMap:            a.envMap,
	}

	// Create a stack level keyvault if secrets are enabled
	// At the moment secrets have no config level setting
	kvName := resourceName(ctx, "", KeyVaultRT)
	kv, err := keyvault.NewVault(ctx, kvName, &keyvault.VaultArgs{
		Location:          rg.Location,
		ResourceGroupName: rg.Name,
		Properties: &keyvault.VaultPropertiesArgs{
			EnableSoftDelete:        pulumi.Bool(false),
			EnableRbacAuthorization: pulumi.Bool(true),
			Sku: &keyvault.SkuArgs{
				Family: pulumi.String("A"),
				Name:   keyvault.SkuNameStandard,
			},
			TenantId: pulumi.String(clientConfig.TenantId),
		},
		Tags: common.Tags(ctx, kvName),
	})

	if err != nil {
		return err
	}
	contAppsArgs.KVaultName = kv.Name

	if len(a.proj.Buckets) > 0 || len(a.proj.Queues) > 0 {
		sr, err := a.newStorageResources(ctx, "storage", &StorageArgs{ResourceGroupName: rg.Name})
		if err != nil {
			return errors.WithMessage(err, "storage create")
		}
		contAppsArgs.StorageAccountBlobEndpoint = sr.Account.PrimaryEndpoints.Blob()
		contAppsArgs.StorageAccountQueueEndpoint = sr.Account.PrimaryEndpoints.Queue()
	}

	for k := range a.proj.Topics {
		contAppsArgs.Topics[k], err = eventgrid.NewTopic(ctx, resourceName(ctx, k, EventGridRT), &eventgrid.TopicArgs{
			ResourceGroupName: rg.Name,
			Location:          rg.Location,
			Tags:              common.Tags(ctx, k),
		})
		if err != nil {
			return errors.WithMessage(err, "eventgrid topic "+k)
		}
	}

	if len(a.proj.Collections) > 0 {
		mc, err := a.newMongoCollections(ctx, "mongodb", &MongoCollectionsArgs{
			ResourceGroup: rg,
		})
		if err != nil {
			return errors.WithMessage(err, "mongodb collections")
		}
		contAppsArgs.MongoDatabaseName = mc.MongoDB.Name
		contAppsArgs.MongoDatabaseConnectionString = mc.ConnectionString
	}

	var apps *ContainerApps
	if len(a.proj.Functions) > 0 || len(a.proj.Containers) > 0 {
		apps, err = a.newContainerApps(ctx, "containerApps", contAppsArgs)
		if err != nil {
			return errors.WithMessage(err, "containerApps")
		}
	}

	_, err = newSubscriptions(ctx, "subscriptions", &SubscriptionsArgs{
		ResourceGroupName: rg.Name,
		Apps:              apps.Apps,
	})
	if err != nil {
		return errors.WithMessage(err, "subscriptions")
	}

	// TODO: Add schedule support
	// NOTE: Currently CRONTAB support is required, we either need to revisit the design of
	// our scheduled expressions or implement a workaround or request a feature.
	schedules := make(map[string]*Schedule)
	for name, schedule := range a.proj.Schedules {
		schedules[name], err = newSchedule(ctx, name, &ScheduleArgs{
			Schedule:          schedule,
			Functions:         apps,
			ResourceGroupName: rg.Name,
		})

		if err != nil {
			return errors.WithMessage(err, "schedule "+name)
		}
	}

	for k, v := range a.proj.ApiDocs {
		_, err = newAzureApiManagement(ctx, k, &AzureApiManagementArgs{
			ResourceGroupName: rg.Name,
			OrgName:           pulumi.String(a.org),
			AdminEmail:        pulumi.String(a.adminEmail),
			OpenAPISpec:       v,
			Apps:              apps.Apps,
		})
		if err != nil {
			return errors.WithMessage(err, "gateway "+k)
		}
	}

	return nil
}

func (a *azureProvider) CleanUp() {
	if a.tmpDir != "" {
		os.Remove(a.tmpDir)
	}
}
