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
	"os"
	"path/filepath"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/aws/aws-sdk-go/service/lambda/lambdaiface"
	multierror "github.com/missionMeteora/toolkit/errors"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/dynamodb"
	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/ecr"
	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/resourcegroups"
	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/s3"
	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/secretsmanager"
	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/sqs"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"golang.org/x/exp/slices"
	"gopkg.in/yaml.v2"

	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/provider/pulumi/common"
	"github.com/nitrictech/cli/pkg/provider/types"
	"github.com/nitrictech/cli/pkg/utils"
	v1 "github.com/nitrictech/nitric/pkg/api/nitric/v1"
)

type awsFunctionConfig struct {
	Memory    *int  `yaml:"memory,omitempty"`
	Timeout   *int  `yaml:"timeout,omitempty"`
	Telemetry *bool `yaml:"telemetry,omitempty"`
}

type awsStackConfig struct {
	Name     string                       `yaml:"name,omitempty"`
	Provider string                       `yaml:"provider,omitempty"`
	Region   string                       `yaml:"region,omitempty"`
	Config   map[string]awsFunctionConfig `yaml:"config,omitempty"`
}

type awsProvider struct {
	proj         *project.Project
	sc           *awsStackConfig
	lambdaClient lambdaiface.LambdaAPI
	envMap       map[string]string
	tmpDir       string

	// created resources (mostly here for testing)
	rg          *resourcegroups.Group
	topics      map[string]*Topic
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

func New(p *project.Project, name string, envMap map[string]string) (common.PulumiProvider, error) {
	// default provider config
	asc := &awsStackConfig{
		Name:     name,
		Provider: types.Aws,
		Config:   map[string]awsFunctionConfig{},
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

	return &awsProvider{
		proj:        p,
		sc:          asc,
		envMap:      envMap,
		topics:      map[string]*Topic{},
		buckets:     map[string]*s3.Bucket{},
		queues:      map[string]*sqs.Queue{},
		collections: map[string]*dynamodb.Table{},
		secrets:     map[string]*secretsmanager.Secret{},
		images:      map[string]*common.Image{},
		funcs:       map[string]*Lambda{},
		schedules:   map[string]*Schedule{},
	}, nil
}

func (a *awsProvider) AskAndSave() error {
	err := survey.AskOne(&survey.Select{
		Message: "select the region",
		Options: a.SupportedRegions(),
	}, &a.sc.Region)
	if err != nil {
		return err
	}

	b, err := yaml.Marshal(a.sc)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(a.proj.Dir, fmt.Sprintf("nitric-%s.yaml", a.sc.Name)), b, 0o644)
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
	errList := &multierror.ErrorList{}

	_, err := session.NewSession()
	if err != nil {
		errList.Push(fmt.Errorf("unable to validate AWS credentials - see https://nitric.io/docs/reference/aws for config info"))
	}

	if a.sc.Region == "" {
		errList.Push(fmt.Errorf("target %s requires \"region\"", a.sc.Provider))
	} else if !slices.Contains(a.SupportedRegions(), a.sc.Region) {
		errList.Push(utils.NewNotSupportedErr(fmt.Sprintf("region %s not supported on provider %s", a.sc.Region, a.sc.Provider)))
	}

	for fn, fc := range a.sc.Config {
		if fc.Memory != nil && *fc.Memory < 128 {
			errList.Push(fmt.Errorf("function config %s requires \"memory\" to be greater than 128 Mi", fn))
		}

		if fc.Timeout != nil && *fc.Timeout < 15 {
			errList.Push(fmt.Errorf("function config %s requires \"timeout\" to be greater than 15 seconds", fn))
		}
	}

	return errList.Err()
}

func (a *awsProvider) Configure(ctx context.Context, autoStack *auto.Stack) error {
	dc, dok := a.sc.Config["default"]

	for fn, f := range a.proj.Functions {
		f.ComputeUnit.Memory = 512
		f.ComputeUnit.Timeout = 15
		f.ComputeUnit.Telemetry = false

		if dok {
			if dc.Memory != nil {
				f.ComputeUnit.Memory = *dc.Memory
			}

			if dc.Timeout != nil {
				f.ComputeUnit.Timeout = *dc.Timeout
			}

			if dc.Telemetry != nil {
				f.ComputeUnit.Telemetry = *dc.Telemetry
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

			if fc.Telemetry != nil {
				f.ComputeUnit.Telemetry = *fc.Telemetry
			}
		}

		a.proj.Functions[fn] = f
	}

	if a.sc.Region != "" && autoStack != nil {
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

func (a *awsProvider) Deploy(ctx *pulumi.Context) error {
	var err error

	a.tmpDir, err = os.MkdirTemp("", ctx.Stack()+"-*")
	if err != nil {
		return err
	}

	if a.lambdaClient == nil {
		sess := session.Must(session.NewSessionWithOptions(session.Options{
			SharedConfigState: session.SharedConfigEnable,
		}))
		a.lambdaClient = lambda.New(sess, &aws.Config{Region: aws.String(a.sc.Region)})
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

	for k, v := range a.proj.Topics {
		a.topics[k], err = newTopic(ctx, k, &TopicArgs{
			Topic: v,
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

	for k := range a.proj.Secrets {
		a.secrets[k], err = secretsmanager.NewSecret(ctx, k, &secretsmanager.SecretArgs{
			Tags: common.Tags(ctx, k),
		})
		if err != nil {
			return errors.WithMessage(err, "secretsmanager secret"+k)
		}
	}

	for k, s := range a.proj.Schedules {
		if len(a.topics) > 0 && s.Target.Type == "topic" && s.Target.Name != "" {
			topic, ok := a.topics[s.Target.Name]
			if !ok {
				return fmt.Errorf("schedule %s does not have a topic %s", k, s.Target.Name)
			}

			a.schedules[k], err = a.newSchedule(ctx, k, ScheduleArgs{
				Expression: s.Expression,
				TopicArn:   topic.Sns.Arn,
				TopicName:  topic.Sns.Name,
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

	for _, c := range a.proj.Computes() {
		localImageName := c.ImageTagName(a.proj, "")

		repo, err := ecr.NewRepository(ctx, localImageName, &ecr.RepositoryArgs{
			ForceDelete: pulumi.BoolPtr(true),
			Tags:        common.Tags(ctx, localImageName),
		})
		if err != nil {
			return err
		}

		image, ok := a.images[c.Unit().Name]
		if !ok {
			image, err = common.NewImage(ctx, c.Unit().Name, &common.ImageArgs{
				ProjectDir:    a.proj.Dir,
				Provider:      a.sc.Provider,
				Compute:       c,
				SourceImage:   fmt.Sprintf("%s-%s", a.proj.Name, c.Unit().Name),
				RepositoryUrl: repo.RepositoryUrl,
				Server:        pulumi.String(authToken.ProxyEndpoint),
				Username:      pulumi.String(authToken.UserName),
				Password:      pulumi.String(authToken.Password),
				TempDir:       a.tmpDir,
			}, pulumi.DependsOn([]pulumi.Resource{repo}))
			if err != nil {
				return errors.WithMessage(err, "function image tag "+c.Unit().Name)
			}

			a.images[c.Unit().Name] = image
		}

		a.funcs[c.Unit().Name], err = newLambda(ctx, c.Unit().Name, &LambdaArgs{
			Client:      a.lambdaClient,
			Topics:      a.topics,
			DockerImage: image,
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
