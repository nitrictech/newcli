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

package gcp

import (
	"context"
	"crypto/md5"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/getkin/kin-openapi/openapi2conv"
	multierror "github.com/missionMeteora/toolkit/errors"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/cloudscheduler"
	"github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/cloudtasks"
	"github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/organizations"
	"github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/projects"
	"github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/pubsub"
	"github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/secretmanager"
	"github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/serviceaccount"
	"github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/storage"
	"github.com/pulumi/pulumi-random/sdk/v4/go/random"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"golang.org/x/exp/slices"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"gopkg.in/yaml.v2"

	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/provider/pulumi/common"
	"github.com/nitrictech/cli/pkg/provider/types"
	"github.com/nitrictech/cli/pkg/utils"
	v1 "github.com/nitrictech/nitric/pkg/api/nitric/v1"
)

type gcpFunctionConfig struct {
	Memory    *int  `yaml:"memory,omitempty"`
	Timeout   *int  `yaml:"timeout,omitempty"`
	Telemetry *bool `yaml:"telemetry,omitempty"`
}

type gcpStackConfig struct {
	Name     string                       `yaml:"name,omitempty"`
	Provider string                       `yaml:"provider,omitempty"`
	Region   string                       `yaml:"region,omitempty"`
	Project  string                       `yaml:"project,omitempty"`
	Config   map[string]gcpFunctionConfig `yaml:"config,omitempty"`
}

type gcpProvider struct {
	sc         *gcpStackConfig
	proj       *project.Project
	envMap     map[string]string
	tmpDir     string
	gcpProject string

	token         *oauth2.Token
	projectNumber string
	projectId     string

	buckets            map[string]*storage.Bucket
	topics             map[string]*pubsub.Topic
	queueTopics        map[string]*pubsub.Topic
	queueSubscriptions map[string]*pubsub.Subscription
	images             map[string]*common.Image
	secrets            map[string]*secretmanager.Secret
	cloudRunners       map[string]*CloudRunner
}

//go:embed pulumi-gcp-version.txt
var gcpPluginVersion string

//go:embed pulumi-random-version.txt
var randomPluginVersion string

func New(p *project.Project, name string, envMap map[string]string) (common.PulumiProvider, error) {
	gsc := &gcpStackConfig{
		Name:     name,
		Provider: types.Gcp,
		Config:   map[string]gcpFunctionConfig{},
	}

	// Hydrate from file if already exists
	b, err := os.ReadFile(filepath.Join(p.Dir, "nitric-"+name+".yaml"))
	if err == nil {
		err = yaml.Unmarshal(b, gsc)
		if err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	return &gcpProvider{
		proj:               p,
		sc:                 gsc,
		envMap:             envMap,
		buckets:            map[string]*storage.Bucket{},
		topics:             map[string]*pubsub.Topic{},
		queueTopics:        map[string]*pubsub.Topic{},
		queueSubscriptions: map[string]*pubsub.Subscription{},
		images:             map[string]*common.Image{},
		cloudRunners:       map[string]*CloudRunner{},
		secrets:            map[string]*secretmanager.Secret{},
	}, nil
}

func (g *gcpProvider) Plugins() []common.Plugin {
	return []common.Plugin{
		{
			Name:    "gcp",
			Version: strings.TrimSpace(gcpPluginVersion),
		},
		{
			Name:    "random",
			Version: strings.TrimSpace(randomPluginVersion),
		},
	}
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

func (g *gcpProvider) SupportedRegions() []string {
	return []string{
		"us-west2",
		"us-west3",
		"us-west4",
		"us-central1",
		"us-east1",
		"us-east4",
		"europe-west1",
		"europe-west2",
		"asia-east1",
		"australia-southeast1",
	}
}

func (g *gcpProvider) AskAndSave() error {
	answers := struct {
		Region  string
		Project string
	}{}

	qs := []*survey.Question{
		{
			Name: "region",
			Prompt: &survey.Select{
				Message: "select the region",
				Options: g.SupportedRegions(),
			},
		},
		{
			Name: "project",
			Prompt: &survey.Input{
				Message: "Provide the gcp project to use",
			},
		},
	}

	err := survey.Ask(qs, &answers)
	if err != nil {
		return err
	}

	g.sc.Region = answers.Region
	g.sc.Project = answers.Project

	b, err := yaml.Marshal(g.sc)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(g.proj.Dir, fmt.Sprintf("nitric-%s.yaml", g.sc.Name)), b, 0o644)
}

func (g *gcpProvider) Validate() error {
	errList := &multierror.ErrorList{}

	if g.sc.Region == "" {
		errList.Push(fmt.Errorf("target %s requires \"region\"", g.sc.Provider))
	} else if !slices.Contains(g.SupportedRegions(), g.sc.Region) {
		errList.Push(utils.NewNotSupportedErr(fmt.Sprintf("region %s not supported on provider %s", g.sc.Region, g.sc.Provider)))
	}

	if g.sc.Project == "" {
		errList.Push(fmt.Errorf("target %s requires GCP \"project\"", g.sc.Provider))
	} else {
		g.gcpProject = g.sc.Project
	}

	for fn, fc := range g.sc.Config {
		if fc.Memory != nil && *fc.Memory < 128 {
			errList.Push(fmt.Errorf("function config %s requires \"memory\" to be greater than 128 Mi", fn))
		}

		if fc.Timeout != nil && *fc.Timeout < 15 {
			errList.Push(fmt.Errorf("function config %s requires \"timeout\" to be greater than 15 seconds", fn))
		}
	}

	return errList.Err()
}

func (g *gcpProvider) Configure(ctx context.Context, autoStack *auto.Stack) error {
	dc, dok := g.sc.Config["default"]

	for fn, f := range g.proj.Functions {
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

		fc, ok := g.sc.Config[f.Handler]
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

		g.proj.Functions[fn] = f
	}

	err := autoStack.SetConfig(ctx, "gcp:region", auto.ConfigValue{Value: g.sc.Region})
	if err != nil {
		return err
	}

	return autoStack.SetConfig(ctx, "gcp:project", auto.ConfigValue{Value: g.gcpProject})
}

func (g *gcpProvider) setToken() error {
	if g.token == nil { // for unit testing
		creds, err := google.FindDefaultCredentialsWithParams(context.Background(), google.CredentialsParams{
			Scopes: []string{
				"https://www.googleapis.com/auth/cloud-platform",
				"https://www.googleapis.com/auth/trace.append",
			},
		})
		if err != nil {
			return errors.WithMessage(err, "Unable to find credentials, try 'gcloud auth application-default login'")
		}

		g.token, err = creds.TokenSource.Token()
		if err != nil {
			return errors.WithMessage(err, "Unable to acquire token source")
		}
	}

	return nil
}

func (g *gcpProvider) Deploy(ctx *pulumi.Context) error {
	var err error

	g.tmpDir, err = os.MkdirTemp("", ctx.Stack()+"-*")
	if err != nil {
		return err
	}

	if err := g.setToken(); err != nil {
		return err
	}

	if g.projectId == "" {
		project, err := organizations.LookupProject(ctx, &organizations.LookupProjectArgs{
			ProjectId: &g.gcpProject,
		}, nil)
		if err != nil {
			return err
		}

		g.projectId = *project.ProjectId
		g.projectNumber = project.Number
	}

	nitricProj, err := newProject(ctx, "project", &ProjectArgs{
		ProjectId:     g.projectId,
		ProjectNumber: g.projectNumber,
	})
	if err != nil {
		return err
	}

	defaultResourceOptions := pulumi.DependsOn([]pulumi.Resource{nitricProj})

	for key := range g.proj.Buckets {
		g.buckets[key], err = storage.NewBucket(ctx, key, &storage.BucketArgs{
			Location: pulumi.String(g.sc.Region),
			Project:  pulumi.String(g.projectId),
			Labels:   common.Tags(ctx, key),
		}, defaultResourceOptions)
		if err != nil {
			return err
		}
	}

	var topicDelayQueue *cloudtasks.Queue
	if len(g.proj.Topics) > 0 {
		// create a shared queue for enabling delayed messaging
		// cloud run functions will create OIDC tokens for their own service accounts
		// to apply to push actions to pubsub, so their scope should still be limited to that
		topicDelayQueue, err = cloudtasks.NewQueue(ctx, "delay-queue", &cloudtasks.QueueArgs{
			Location: pulumi.String(g.sc.Region),
		})
		if err != nil {
			return err
		}
	}

	for key := range g.proj.Topics {
		g.topics[key], err = pubsub.NewTopic(ctx, key, &pubsub.TopicArgs{
			Name:   pulumi.String(key),
			Labels: common.Tags(ctx, key),
		}, defaultResourceOptions)
		if err != nil {
			return err
		}
	}

	for key := range g.proj.Queues {
		g.queueTopics[key], err = pubsub.NewTopic(ctx, key, &pubsub.TopicArgs{
			Name:   pulumi.String(key),
			Labels: common.Tags(ctx, key),
		}, defaultResourceOptions)
		if err != nil {
			return err
		}

		g.queueSubscriptions[key], err = pubsub.NewSubscription(ctx, key+"-sub", &pubsub.SubscriptionArgs{
			Name:  pulumi.Sprintf("%s-nitricqueue", key),
			Topic: g.queueTopics[key].Name,
		}, defaultResourceOptions)
		if err != nil {
			return err
		}
	}

	for k, sched := range g.proj.Schedules {
		if _, ok := g.topics[sched.Target.Name]; ok {
			payload := ""

			if len(sched.Event.Payload) > 0 {
				eventJSON, err := json.Marshal(sched.Event.Payload)
				if err != nil {
					return err
				}

				payload = base64.StdEncoding.EncodeToString(eventJSON)
			}

			_, err = cloudscheduler.NewJob(ctx, k, &cloudscheduler.JobArgs{
				TimeZone: pulumi.String("UTC"),
				PubsubTarget: cloudscheduler.JobPubsubTargetArgs{
					Attributes: pulumi.ToStringMap(map[string]string{"x-nitric-topic": sched.Target.Name}),
					TopicName:  pulumi.Sprintf("projects/%s/topics/%s", g.projectId, g.topics[sched.Target.Name].Name),
					Data:       pulumi.String(payload),
				},
				Schedule: pulumi.String(strings.ReplaceAll(sched.Expression, "'", "")),
			}, defaultResourceOptions)
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf("topic %s defined as target for schedule, but does not exist in the stack", sched.Target.Name)
		}
	}

	for name := range g.proj.Secrets {
		secId := pulumi.Sprintf("%s-%s", g.sc.Name, name)

		g.secrets[name], err = secretmanager.NewSecret(ctx, name, &secretmanager.SecretArgs{
			Replication: secretmanager.SecretReplicationArgs{
				Automatic: pulumi.Bool(true),
			},
			Project:  pulumi.String(g.projectId),
			SecretId: secId,
			Labels:   common.Tags(ctx, name),
		})
		if err != nil {
			return err
		}
	}

	principalMap := make(PrincipalMap)
	principalMap[v1.ResourceType_Function] = make(map[string]*serviceaccount.Account)

	baseCustomRoleId, err := random.NewRandomString(ctx, fmt.Sprintf("%s-base-role", g.sc.Name), &random.RandomStringArgs{
		Special: pulumi.Bool(false),
		Length:  pulumi.Int(8),
		Keepers: pulumi.ToMap(map[string]interface{}{
			"stack-name": g.sc.Name,
		}),
	})
	if err != nil {
		return errors.WithMessage(err, "base customRole id")
	}

	perms := []string{
		"storage.buckets.list",
		"storage.buckets.get",
		"cloudtasks.queues.get",
		"cloudtasks.tasks.create",
		"cloudtrace.traces.patch",
		"monitoring.timeSeries.create",
		// permission for blob signing
		// this is safe as only permissions this account has are delegated
		"iam.serviceAccounts.signBlob",
	}

	for _, fc := range g.sc.Config {
		if fc.Telemetry != nil && *fc.Telemetry {
			perms = append(perms, []string{
				"monitoring.metricDescriptors.create",
				"monitoring.metricDescriptors.get",
				"monitoring.metricDescriptors.list",
				"monitoring.monitoredResourceDescriptors.get",
				"monitoring.monitoredResourceDescriptors.list",
				"monitoring.timeSeries.create",
			}...)

			break
		}
	}

	// setup a basic IAM role for general access and resource discovery
	baseComputeRole, err := projects.NewIAMCustomRole(ctx, "base-role", &projects.IAMCustomRoleArgs{
		Title:       pulumi.String(g.sc.Name + "-functions-base-role"),
		Permissions: pulumi.ToStringArray(perms),
		RoleId:      baseCustomRoleId.ID(),
	})
	if err != nil {
		return errors.WithMessage(err, "base customRole")
	}

	for _, c := range g.proj.Computes() {
		if _, ok := g.images[c.Unit().Name]; !ok {
			g.images[c.Unit().Name], err = common.NewImage(ctx, c.Unit().Name+"Image", &common.ImageArgs{
				ProjectDir:    g.proj.Dir,
				Provider:      g.sc.Provider,
				Compute:       c,
				SourceImage:   fmt.Sprintf("%s-%s", g.proj.Name, c.Unit().Name),
				RepositoryUrl: pulumi.Sprintf("gcr.io/%s/%s", g.projectId, c.ImageTagName(g.proj, g.sc.Provider)),
				Username:      pulumi.String("oauth2accesstoken"),
				Password:      pulumi.String(g.token.AccessToken),
				Server:        pulumi.String("https://gcr.io"),
				TempDir:       g.tmpDir,
			}, defaultResourceOptions)
			if err != nil {
				return errors.WithMessage(err, "function image tag "+c.Unit().Name)
			}
		}

		// Create a service account for this cloud run instance
		sa, err := serviceaccount.NewAccount(ctx, c.Unit().Name+"-acct", &serviceaccount.AccountArgs{
			AccountId: pulumi.String(utils.StringTrunc(c.Unit().Name, 30-5) + "-acct"),
		})
		if err != nil {
			return errors.WithMessage(err, "function serviceaccount "+c.Unit().Name)
		}

		// apply basic project level permissions for nitric resource discovery
		_, err = projects.NewIAMMember(ctx, c.Unit().Name+"-project-member", &projects.IAMMemberArgs{
			Project: pulumi.String(g.projectId),
			Member:  pulumi.Sprintf("serviceAccount:%s", sa.Email),
			Role:    baseComputeRole.Name,
		})
		if err != nil {
			return errors.WithMessage(err, "function project membership "+c.Unit().Name)
		}

		// give the service account permission to use itself
		_, err = serviceaccount.NewIAMMember(ctx, c.Unit().Name+"-acct-member", &serviceaccount.IAMMemberArgs{
			ServiceAccountId: sa.Name,
			Member:           pulumi.Sprintf("serviceAccount:%s", sa.Email),
			Role:             pulumi.String("roles/iam.serviceAccountUser"),
		})
		if err != nil {
			return errors.WithMessage(err, "service account self membership "+c.Unit().Name)
		}

		g.cloudRunners[c.Unit().Name], err = g.newCloudRunner(ctx, c.Unit().Name, &CloudRunnerArgs{
			Location:       pulumi.String(g.sc.Region),
			ProjectId:      g.projectId,
			Topics:         g.topics,
			Compute:        c,
			Image:          g.images[c.Unit().Name],
			ServiceAccount: sa,
			EnvMap:         g.envMap,
			DelayQueue:     topicDelayQueue,
		}, defaultResourceOptions)
		if err != nil {
			return err
		}

		principalMap[v1.ResourceType_Function][c.Unit().Name] = sa
	}

	for k, doc := range g.proj.ApiDocs {
		v2doc, err := openapi2conv.FromV3(doc)
		if err != nil {
			return err
		}

		_, err = newApiGateway(ctx, k, &ApiGatewayArgs{
			Functions:           g.cloudRunners,
			OpenAPISpec:         v2doc,
			ProjectId:           pulumi.String(g.projectId),
			SecurityDefinitions: g.proj.SecurityDefinitions[k],
		}, defaultResourceOptions)
		if err != nil {
			return err
		}
	}

	uniquePolicies := map[string]*v1.PolicyResource{}

	for _, p := range g.proj.Policies {
		if len(p.Actions) == 0 {
			_ = ctx.Log.Debug("policy has no actions "+fmt.Sprint(p), &pulumi.LogArgs{Ephemeral: true})
			continue
		}

		policyName, err := policyResourceName(p)
		if err != nil {
			return err
		}

		uniquePolicies[policyName] = p
	}

	for name, p := range uniquePolicies {
		if _, err := newPolicy(ctx, name, &PolicyArgs{
			Policy:    p,
			ProjectID: pulumi.String(g.projectId),
			Resources: &StackResources{
				Topics:        g.topics,
				Queues:        g.queueTopics,
				Subscriptions: g.queueSubscriptions,
				Buckets:       g.buckets,
				Secrets:       g.secrets,
			},
			Principals: principalMap,
		}); err != nil {
			return err
		}
	}

	return nil
}

func (g *gcpProvider) CleanUp() {
	if g.tmpDir != "" {
		os.Remove(g.tmpDir)
	}
}
