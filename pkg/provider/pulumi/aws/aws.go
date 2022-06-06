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

package aws

import (
	"context"
	"crypto/md5"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/dynamodb"
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/ecr"
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/resourcegroups"
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/s3"
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/secretsmanager"
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/sns"
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/sqs"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/provider/pulumi/common"
	"github.com/nitrictech/cli/pkg/stack"
	"github.com/nitrictech/cli/pkg/utils"
	v1 "github.com/nitrictech/nitric/pkg/api/nitric/v1"
)

type awsProvider struct {
	proj   *project.Project
	sc     *stack.Config
	envMap map[string]string
	tmpDir string

	// created resources (mostly here for testing)
	rg          *resourcegroups.Group
	topics      map[string]*sns.Topic
	buckets     map[string]*s3.Bucket
	queues      map[string]*sqs.Queue
	collections map[string]*dynamodb.Table
	secrets     map[string]*secretsmanager.Secret
	images      map[string]*common.Image
	funcs       map[string]*Lambda
	schedules   map[string]*Schedule
}

//go:embed pulumi-aws-version.txt
var awsPluginVersion string

func New(s *project.Project, t *stack.Config, envMap map[string]string) common.PulumiProvider {
	return &awsProvider{
		proj:        s,
		sc:          t,
		envMap:      envMap,
		topics:      map[string]*sns.Topic{},
		buckets:     map[string]*s3.Bucket{},
		queues:      map[string]*sqs.Queue{},
		collections: map[string]*dynamodb.Table{},
		secrets:     map[string]*secretsmanager.Secret{},
		images:      map[string]*common.Image{},
		funcs:       map[string]*Lambda{},
		schedules:   map[string]*Schedule{},
	}
}

func (a *awsProvider) Ask() (*stack.Config, error) {
	sc := &stack.Config{Name: a.sc.Name, Provider: a.sc.Provider}
	err := survey.AskOne(&survey.Select{
		Message: "select the region",
		Options: a.SupportedRegions(),
	}, &sc.Region)
	return sc, err
}

func (a *awsProvider) Plugins() []common.Plugin {
	return []common.Plugin{
		{
			Name:    "aws",
			Version: strings.TrimSpace(awsPluginVersion),
		},
	}
}

func (a *awsProvider) SupportedRegions() []string {
	return []string{
		"us-east-1",
		"us-west-1",
		"us-west-2",
		"eu-west-1",
		"eu-central-1",
		"ap-southeast-1",
		"ap-northeast-1",
		"ap-southeast-2",
		"ap-northeast-2",
		"sa-east-1",
		"cn-north-1",
		"ap-south-1",
	}
}

func (a *awsProvider) Validate() error {
	found := false
	for _, r := range a.SupportedRegions() {
		if r == a.sc.Region {
			found = true
			break
		}
	}
	if !found {
		return utils.NewNotSupportedErr(fmt.Sprintf("region %s not supported on provider %s", a.sc.Region, a.sc.Provider))
	}
	return nil
}

func (a *awsProvider) Configure(ctx context.Context, autoStack *auto.Stack) error {
	if a.sc.Region != "" {
		return autoStack.SetConfig(ctx, "aws:region", auto.ConfigValue{Value: a.sc.Region})
	}

	return nil
}

func md5Hash(b []byte) string {
	hasher := md5.New()
	hasher.Write(b)
	return hex.EncodeToString(hasher.Sum(nil))
}

func policyResourceName(policy *v1.PolicyResource) (string, error) {
	policyDoc, err := json.Marshal(policy)
	if err != nil {
		return "", err
	}

	return md5Hash(policyDoc), nil
}

func (a *awsProvider) TryPullImages() error {
	return nil
}

func (a *awsProvider) Deploy(ctx *pulumi.Context) error {
	var err error
	a.tmpDir, err = ioutil.TempDir("", ctx.Stack()+"-*")
	if err != nil {
		return err
	}

	rgQueryJSON, err := json.Marshal(map[string]interface{}{
		"ResourceTypeFilters": []string{"AWS::AllSupported"},
		"TagFilters": []interface{}{
			map[string]interface{}{
				"Key":    "x-nitric-stack",
				"Values": []string{ctx.Stack()},
			},
		},
	})
	if err != nil {
		return errors.WithMessage(err, "resource group json marshal")
	}

	a.rg, err = resourcegroups.NewGroup(ctx, ctx.Stack(), &resourcegroups.GroupArgs{
		ResourceQuery: &resourcegroups.GroupResourceQueryArgs{
			Query: pulumi.String(rgQueryJSON),
		},
	})
	if err != nil {
		return errors.WithMessage(err, "resource group create")
	}

	for k := range a.proj.Topics {
		a.topics[k], err = sns.NewTopic(ctx, k, &sns.TopicArgs{
			// FIXME: Autonaming of topics disabled until improvements to
			// nitric topic name discovery is made for SNS topics.
			Name: pulumi.StringPtr(k),
			Tags: common.Tags(ctx, k),
		})
		if err != nil {
			return errors.WithMessage(err, "sns topic "+k)
		}
	}

	for k := range a.proj.Buckets {
		a.buckets[k], err = s3.NewBucket(ctx, k, &s3.BucketArgs{
			Tags: common.Tags(ctx, k),
		})
		if err != nil {
			return errors.WithMessage(err, "s3 bucket "+k)
		}
	}

	for k := range a.proj.Queues {
		a.queues[k], err = sqs.NewQueue(ctx, k, &sqs.QueueArgs{
			Tags: common.Tags(ctx, k),
		})
		if err != nil {
			return errors.WithMessage(err, "sqs queue "+k)
		}
	}

	for k := range a.proj.Collections {
		a.collections[k], err = dynamodb.NewTable(ctx, k, &dynamodb.TableArgs{
			Attributes: dynamodb.TableAttributeArray{
				&dynamodb.TableAttributeArgs{
					Name: pulumi.String("_pk"),
					Type: pulumi.String("S"),
				},
				&dynamodb.TableAttributeArgs{
					Name: pulumi.String("_sk"),
					Type: pulumi.String("S"),
				},
			},
			HashKey:     pulumi.String("_pk"),
			RangeKey:    pulumi.String("_sk"),
			BillingMode: pulumi.String("PAY_PER_REQUEST"),
			Tags:        common.Tags(ctx, k),
		})
		if err != nil {
			return errors.WithMessage(err, "dynamodb table "+k)
		}
	}

	secrets := map[string]*secretsmanager.Secret{}
	for k := range a.proj.Secrets {
		secrets[k], err = secretsmanager.NewSecret(ctx, k, &secretsmanager.SecretArgs{
			Name: pulumi.StringPtr(k),
			Tags: common.Tags(ctx, k),
		})
		if err != nil {
			return errors.WithMessage(err, "secretsmanager secret"+k)
		}
	}

	for k, s := range a.proj.Schedules {
		a.schedules[k], err = a.newSchedule(ctx, k, ScheduleArgs{
			Expression: s.Expression,
			Functions:  a.funcs,
			Topics:     a.topics,
			Schedule:   s,
		})
		if err != nil {
			return errors.WithMessage(err, "schedule "+k)
		}
	}

	authToken, err := ecr.GetAuthorizationToken(ctx, &ecr.GetAuthorizationTokenArgs{})
	if err != nil {
		return err
	}

	principalMap := make(map[v1.ResourceType]map[string]*iam.Role)
	principalMap[v1.ResourceType_Function] = make(map[string]*iam.Role)

	for _, c := range a.proj.Computes() {
		localImageName := c.ImageTagName(a.proj, "")

		repo, err := ecr.NewRepository(ctx, localImageName, &ecr.RepositoryArgs{
			Tags: common.Tags(ctx, localImageName),
		})
		if err != nil {
			return err
		}

		image, ok := a.images[c.Unit().Name]
		if !ok {
			image, err = common.NewImage(ctx, c.Unit().Name, &common.ImageArgs{
				LocalImageName:  localImageName,
				SourceImageName: c.ImageTagName(a.proj, a.sc.Provider),
				RepositoryUrl:   repo.RepositoryUrl,
				Server:          pulumi.String(authToken.ProxyEndpoint),
				Username:        pulumi.String(authToken.UserName),
				Password:        pulumi.String(authToken.Password),
				TempDir:         a.tmpDir})

			if err != nil {
				return errors.WithMessage(err, "function image tag "+c.Unit().Name)
			}
			a.images[c.Unit().Name] = image
		}

		a.funcs[c.Unit().Name], err = newLambda(ctx, c.Unit().Name, &LambdaArgs{
			Topics:      a.topics,
			DockerImage: image.DockerImage,
			Compute:     c,
			StackName:   ctx.Stack(),
			EnvMap:      a.envMap,
		})
		if err != nil {
			return errors.WithMessage(err, "lambda container "+c.Unit().Name)
		}

		principalMap[v1.ResourceType_Function][c.Unit().Name] = a.funcs[c.Unit().Name].Role
	}

	for k, v := range a.proj.ApiDocs {
		_, err = newApiGateway(ctx, k, &ApiGatewayArgs{
			OpenAPISpec:        v,
			LambdaFunctions:    a.funcs,
			SecurityDefintions: a.proj.SecurityDefinitions[k],
		})
		if err != nil {
			return errors.WithMessage(err, "gateway "+k)
		}
	}

	for _, p := range a.proj.Policies {
		if len(p.Actions) == 0 {
			// note Topic receiving does not require an action.
			_ = ctx.Log.Debug("policy has no actions "+fmt.Sprint(p), &pulumi.LogArgs{Ephemeral: true})
			continue
		}
		policyName, err := policyResourceName(p)
		if err != nil {
			return err
		}

		if _, err := newPolicy(ctx, policyName, &PolicyArgs{
			Policy: p,
			Resources: &StackResources{
				Topics:      a.topics,
				Queues:      a.queues,
				Buckets:     a.buckets,
				Collections: a.collections,
				Secrets:     a.secrets,
			},
			Principals: principalMap,
		}); err != nil {
			return err
		}
	}

	return nil
}

func (a *awsProvider) CleanUp() {
	if a.tmpDir != "" {
		os.Remove(a.tmpDir)
	}
}
