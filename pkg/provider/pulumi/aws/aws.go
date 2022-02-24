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
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

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

	"github.com/nitrictech/cli/pkg/provider/pulumi/common"
	"github.com/nitrictech/cli/pkg/stack"
	"github.com/nitrictech/cli/pkg/target"
	"github.com/nitrictech/cli/pkg/utils"
	v1 "github.com/nitrictech/nitric/pkg/api/nitric/v1"
)

type awsProvider struct {
	s      *stack.Stack
	t      *target.Target
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

func New(s *stack.Stack, t *target.Target) common.PulumiProvider {
	return &awsProvider{
		s:           s,
		t:           t,
		topics:      map[string]*sns.Topic{},
		buckets:     map[string]*s3.Bucket{},
		queues:      map[string]*sqs.Queue{},
		collections: map[string]*dynamodb.Table{},
		secrets:     map[string]*secretsmanager.Secret{},
		images:      map[string]*common.Image{},
		funcs:       map[string]*Lambda{},
	}
}

func (a *awsProvider) Plugins() []common.Plugin {
	return []common.Plugin{
		{
			Name:    "aws",
			Version: "v4.37.1",
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
		if r == a.t.Region {
			found = true
			break
		}
	}
	if !found {
		return utils.NewNotSupportedErr(fmt.Sprintf("region %s not supported on provider %s", a.t.Region, a.t.Provider))
	}
	return nil
}

func (a *awsProvider) Configure(ctx context.Context, autoStack *auto.Stack) error {
	if a.t.Region != "" {
		return autoStack.SetConfig(ctx, "aws:region", auto.ConfigValue{Value: a.t.Region})
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

	for k := range a.s.Topics {
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

	for k := range a.s.Buckets {
		a.buckets[k], err = s3.NewBucket(ctx, k, &s3.BucketArgs{
			Tags: common.Tags(ctx, k),
		})
		if err != nil {
			return errors.WithMessage(err, "s3 bucket "+k)
		}
	}

	for k := range a.s.Queues {
		a.queues[k], err = sqs.NewQueue(ctx, k, &sqs.QueueArgs{
			Tags: common.Tags(ctx, k),
		})
		if err != nil {
			return errors.WithMessage(err, "sqs queue "+k)
		}
	}

	for k := range a.s.Collections {
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
	for k := range a.s.Secrets {
		secrets[k], err = secretsmanager.NewSecret(ctx, k, &secretsmanager.SecretArgs{
			Name: pulumi.StringPtr(k),
			Tags: common.Tags(ctx, k),
		})
	}

	for k, s := range a.s.Schedules {
		if len(a.topics) > 0 && s.Target.Type == "topic" && s.Target.Name != "" {
			a.schedules[k], err = a.newSchedule(ctx, k, ScheduleArgs{
				Expression: s.Expression,
				TopicArn:   a.topics[s.Target.Name].Arn,
				TopicName:  a.topics[s.Target.Name].Name,
			})
			if err != nil {
				return errors.WithMessage(err, "schedule "+k)
			}
		}
	}

	authToken, err := ecr.GetAuthorizationToken(ctx, &ecr.GetAuthorizationTokenArgs{})
	if err != nil {
		return err
	}

	principalMap := make(map[v1.ResourceType]map[string]*iam.Role)
	principalMap[v1.ResourceType_Function] = make(map[string]*iam.Role)

	computes := []stack.Compute{}
	for _, c := range a.s.Functions {
		copy := c
		computes = append(computes, &copy)
	}
	for _, c := range a.s.Containers {
		copy := c
		computes = append(computes, &copy)
	}
	for _, c := range computes {
		localImageName := c.ImageTagName(a.s, "")

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
				SourceImageName: c.ImageTagName(a.s, a.t.Provider),
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
		})
		if err != nil {
			return errors.WithMessage(err, "lambda container "+c.Unit().Name)
		}

		principalMap[v1.ResourceType_Function][c.Unit().Name] = a.funcs[c.Unit().Name].Role
	}

	for k, v := range a.s.ApiDocs {
		_, err = newApiGateway(ctx, k, &ApiGatewayArgs{
			OpenAPISpec:     v,
			LambdaFunctions: a.funcs})
		if err != nil {
			return errors.WithMessage(err, "gateway "+k)
		}
	}

	for _, p := range a.s.Policies {
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
