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
	"os"
	"path/filepath"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	multierror "github.com/missionMeteora/toolkit/errors"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-azure-native/sdk/go/azure/authorization"
	"github.com/pulumi/pulumi-azure-native/sdk/go/azure/eventgrid"
	"github.com/pulumi/pulumi-azure-native/sdk/go/azure/keyvault"
	"github.com/pulumi/pulumi-azure-native/sdk/go/azure/managedidentity"
	"github.com/pulumi/pulumi-azure-native/sdk/go/azure/resources"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"golang.org/x/exp/slices"
	"gopkg.in/yaml.v2"

	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/provider/pulumi/common"
	"github.com/nitrictech/cli/pkg/provider/types"
	"github.com/nitrictech/cli/pkg/utils"
)

type azureFunctionConfig struct {
	Memory    *int  `yaml:"memory,omitempty"`
	Timeout   *int  `yaml:"timeout,omitempty"`
	Telemetry *bool `yaml:"telemetry,omitempty"`
}

type azureStackConfig struct {
	Name     string `yaml:"name,omitempty"`
	Provider string `yaml:"provider,omitempty"`
	Region   string `yaml:"region,omitempty"`

	AdminEmail string                         `yaml:"adminemail,omitempty"`
	Org        string                         `yaml:"org,omitempty"`
	Config     map[string]azureFunctionConfig `yaml:"config,omitempty"`
}

type azureProvider struct {
	proj   *project.Project
	sc     *azureStackConfig
	envMap map[string]string
	tmpDir string
}

var (
	//go:embed pulumi-azure-version.txt
	azurePluginVersion string
	//go:embed pulumi-azuread-version.txt
	azureADPluginVersion string
	//go:embed pulumi-azure-native-version.txt
	azureNativePluginVersion string
)

func New(p *project.Project, name string, envMap map[string]string) (common.PulumiProvider, error) {
	// default provider config
	asc := &azureStackConfig{
		Name:     name,
		Provider: types.Azure,
		Config:   map[string]azureFunctionConfig{},
	}

	// Hydrate from file if already exists
	b, err := os.ReadFile(filepath.Join(p.Dir, "nitric-"+name+".yaml"))
	if err == nil {
		err = yaml.Unmarshal(b, asc)
		if err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	return &azureProvider{
		proj:   p,
		sc:     asc,
		envMap: envMap,
	}, nil
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

func (a *azureProvider) AskAndSave() error {
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

	err := survey.Ask(qs, &answers)
	if err != nil {
		return err
	}

	a.sc.Region = answers.Region
	a.sc.AdminEmail = answers.AdminEmail
	a.sc.Org = answers.Org

	b, err := yaml.Marshal(a.sc)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(a.proj.Dir, fmt.Sprintf("nitric-%s.yaml", a.sc.Name)), b, 0o644)
}

func (a *azureProvider) SupportedRegions() []string {
	return []string{
		"canadacentral",
		"eastasia",
		"eastus",
		"eastus2",
		"germanywestcentral",
		"japaneast",
		"northeurope",
		"uksouth",
		"westeurope",
		"westus",
	}
}

func (a *azureProvider) Validate() error {
	errList := &multierror.ErrorList{}

	if a.sc.Region == "" {
		errList.Push(fmt.Errorf("target %s requires \"region\"", a.sc.Provider))
	} else if !slices.Contains(a.SupportedRegions(), a.sc.Region) {
		errList.Push(utils.NewNotSupportedErr(fmt.Sprintf("region %s not supported on provider %s", a.sc.Region, a.sc.Provider)))
	}

	if a.sc.Org == "" {
		errList.Push(fmt.Errorf("target %s requires \"org\"", a.sc.Provider))
	}

	if a.sc.AdminEmail == "" {
		errList.Push(fmt.Errorf("target %s requires \"adminemail\"", a.sc.Provider))
	}

	for fn, fc := range a.sc.Config {
		if fc.Memory != nil && *fc.Memory < 128 {
			errList.Push(fmt.Errorf("function config %s requires \"memory\" to be greater than 128 Mi", fn))
		}

		if fc.Timeout != nil && *fc.Timeout < 15 {
			errList.Push(fmt.Errorf("function config %s requires \"timeout\" to be greater than 15 seconds", fn))
		}

		if fc.Telemetry != nil {
			errList.Push(fmt.Errorf("function config %s telemetry is not supported on azure yet", fn))
		}
	}

	return errList.Err()
}

func (a *azureProvider) Configure(ctx context.Context, autoStack *auto.Stack) error {
	dc, dok := a.sc.Config["default"]

	for fn, f := range a.proj.Functions {
		f.ComputeUnit.Memory = 512
		f.ComputeUnit.Timeout = 15

		if dok {
			if dc.Memory != nil {
				f.ComputeUnit.Memory = *dc.Memory
			}

			if dc.Timeout != nil {
				f.ComputeUnit.Timeout = *dc.Timeout
			}
		}

		fc, ok := a.sc.Config[f.Handler]
		if ok {
			if fc.Memory != nil {
				f.ComputeUnit.Memory = *fc.Memory
			}

			if fc.Timeout != nil {
				f.ComputeUnit.Timeout = *fc.Timeout
			}
		}

		a.proj.Functions[fn] = f
	}

	err := autoStack.SetConfig(ctx, "azure:location", auto.ConfigValue{Value: a.sc.Region})
	if err != nil {
		return err
	}

	return autoStack.SetConfig(ctx, "azure-native:location", auto.ConfigValue{Value: a.sc.Region})
}

func (a *azureProvider) Deploy(ctx *pulumi.Context) error {
	var err error

	a.tmpDir, err = os.MkdirTemp("", ctx.Stack()+"-*")
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

	managedUser, err := managedidentity.NewUserAssignedIdentity(ctx, "managed-identity", &managedidentity.UserAssignedIdentityArgs{
		Location:          pulumi.String(a.sc.Region),
		ResourceGroupName: rg.Name,
		ResourceName:      pulumi.String("managed-identity"),
	})
	if err != nil {
		return err
	}

	contAppsArgs.ManagedIdentityID = managedUser.ClientId

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
	if len(a.proj.Schedules) > 0 {
		_ = ctx.Log.Warn("Schedules are not currently supported for Azure deployments", &pulumi.LogArgs{})
	}

	for k, v := range a.proj.ApiDocs {
		_, err = newAzureApiManagement(ctx, k, &AzureApiManagementArgs{
			ResourceGroupName:   rg.Name,
			OrgName:             pulumi.String(a.sc.Org),
			AdminEmail:          pulumi.String(a.sc.AdminEmail),
			OpenAPISpec:         v,
			Apps:                apps.Apps,
			SecurityDefinitions: a.proj.SecurityDefinitions[k],
			ManagedIdentity:     managedUser,
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
