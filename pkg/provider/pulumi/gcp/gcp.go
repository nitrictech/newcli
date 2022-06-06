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
	"io/ioutil"
	"os"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/docker/docker/api/types"
	"github.com/getkin/kin-openapi/openapi2conv"
	"github.com/golangci/golangci-lint/pkg/sliceutil"
	multierror "github.com/missionMeteora/toolkit/errors"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/organizations"
	"github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/projects"
	"github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/pubsub"
	"github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/secretmanager"
	"github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/serviceaccount"
	"github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/storage"
	"github.com/pulumi/pulumi-random/sdk/v4/go/random"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/nitrictech/cli/pkg/containerengine"
	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/provider/pulumi/common"
	"github.com/nitrictech/cli/pkg/stack"
	"github.com/nitrictech/cli/pkg/utils"
	v1 "github.com/nitrictech/nitric/pkg/api/nitric/v1"
)

type gcpProvider struct {
	sc         *stack.Config
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

func New(s *project.Project, t *stack.Config, envMap map[string]string) common.PulumiProvider {
	return &gcpProvider{
		proj:               s,
		sc:                 t,
		envMap:             envMap,
		buckets:            map[string]*storage.Bucket{},
		topics:             map[string]*pubsub.Topic{},
		queueTopics:        map[string]*pubsub.Topic{},
		queueSubscriptions: map[string]*pubsub.Subscription{},
		images:             map[string]*common.Image{},
		cloudRunners:       map[string]*CloudRunner{},
		secrets:            map[string]*secretmanager.Secret{},
	}
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

func (a *gcpProvider) Ask() (*stack.Config, error) {
	answers := struct {
		Region  string
		Project string
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
			Name: "project",
			Prompt: &survey.Input{
				Message: "Provide the gcp project to use",
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
	sc.Extra["project"] = answers.Project

	return sc, nil
}

func (g *gcpProvider) Validate() error {
	errList := &multierror.ErrorList{}

	if g.sc.Region == "" {
		errList.Push(fmt.Errorf("target %s requires \"region\"", g.sc.Provider))
	} else if !sliceutil.Contains(g.SupportedRegions(), g.sc.Region) {
		errList.Push(utils.NewNotSupportedErr(fmt.Sprintf("region %s not supported on provider %s", g.sc.Region, g.sc.Provider)))
	}

	if proj, ok := g.sc.Extra["project"]; !ok || proj == nil {
		errList.Push(fmt.Errorf("target %s requires GCP \"project\"", g.sc.Provider))
	} else {
		g.gcpProject = proj.(string)
	}

	return errList.Err()
}

func (g *gcpProvider) Configure(ctx context.Context, autoStack *auto.Stack) error {
	err := autoStack.SetConfig(ctx, "gcp:region", auto.ConfigValue{Value: g.sc.Region})
	if err != nil {
		return err
	}

	return autoStack.SetConfig(ctx, "gcp:project", auto.ConfigValue{Value: g.gcpProject})
}

func (g *gcpProvider) TryPullImages() error {
	ce, err := containerengine.Discover()
	if err != nil {
		return err
	}

	if proj, ok := g.sc.Extra["project"]; !ok || proj == nil {
		return fmt.Errorf("target %s requires GCP \"project\"", g.sc.Provider)
	} else {
		g.gcpProject = proj.(string)
	}

	if err := g.setToken(); err != nil {
		return errors.WithMessage(err, "setToken")
	}

	authConfig := types.AuthConfig{
		Username:      "oauth2accesstoken",
		Password:      g.token.AccessToken,
		ServerAddress: "https://gcr.io",
	}

	encodedJSON, err := json.Marshal(authConfig)
	if err != nil {
		return errors.WithMessage(err, "json.Marshal auth")
	}
	authStr := base64.URLEncoding.EncodeToString(encodedJSON)

	for _, c := range g.proj.Computes() {
		image := fmt.Sprintf("gcr.io/%s/%s:latest", g.gcpProject, c.ImageTagName(g.proj, g.sc.Provider))
		err = ce.ImagePull(image, types.ImagePullOptions{RegistryAuth: authStr})
		if err != nil {
			return errors.WithMessage(err, "imagePull")
		}
	}

	return nil
}

func (g *gcpProvider) setToken() error {
	if g.token == nil { // for unit testing
		creds, err := google.FindDefaultCredentialsWithParams(context.Background(), google.CredentialsParams{
			Scopes: []string{
				"https://www.googleapis.com/auth/cloud-platform",
			}})
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
	g.tmpDir, err = ioutil.TempDir("", ctx.Stack()+"-*")
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
		_, err := newSchedule(ctx, k, &ScheduleArgs{
			Schedule:  sched,
			Topics:    g.topics,
			Functions: g.cloudRunners,
		}, defaultResourceOptions)
		if err != nil {
			return errors.WithMessage(err, "unable to deploy schedule")
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

	// setup a basic IAM role for general access and resource discovery
	baseComputeRole, err := projects.NewIAMCustomRole(ctx, "base-role", &projects.IAMCustomRoleArgs{
		Title: pulumi.String(g.sc.Name + "-functions-base-role"),
		Permissions: pulumi.ToStringArray([]string{
			"storage.buckets.list",
			"storage.buckets.get",
			// permission for blob signing
			// this is safe as only permissions this account has are delegated
			"iam.serviceAccounts.signBlob",
		}),
		RoleId: baseCustomRoleId.ID(),
	})

	if err != nil {
		return errors.WithMessage(err, "base customRole")
	}

	for _, c := range g.proj.Computes() {
		if _, ok := g.images[c.Unit().Name]; !ok {
			g.images[c.Unit().Name], err = common.NewImage(ctx, c.Unit().Name+"Image", &common.ImageArgs{
				LocalImageName:  c.ImageTagName(g.proj, ""),
				SourceImageName: c.ImageTagName(g.proj, g.sc.Provider),
				RepositoryUrl:   pulumi.Sprintf("gcr.io/%s/%s", g.projectId, c.ImageTagName(g.proj, g.sc.Provider)),
				Username:        pulumi.String("oauth2accesstoken"),
				Password:        pulumi.String(g.token.AccessToken),
				Server:          pulumi.String("https://gcr.io"),
				TempDir:         g.tmpDir,
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

		g.cloudRunners[c.Unit().Name], err = g.newCloudRunner(ctx, c.Unit().Name, &CloudRunnerArgs{
			Location:       pulumi.String(g.sc.Region),
			ProjectId:      g.projectId,
			Topics:         g.topics,
			Compute:        c,
			Image:          g.images[c.Unit().Name],
			ServiceAccount: sa,
			EnvMap:         g.envMap,
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
			Functions:   g.cloudRunners,
			OpenAPISpec: v2doc,
			ProjectId:   pulumi.String(g.projectId),
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
				Topics:  g.topics,
				Queues:  g.queueTopics,
				Buckets: g.buckets,
				Secrets: g.secrets,
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
