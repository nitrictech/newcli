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

package common

import (
	"path/filepath"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/runtime"
	"github.com/nitrictech/pulumi-docker-buildkit/sdk/v0.1.17/dockerbuildkit"
)

type ImageArgs struct {
	SourceImage   string
	ProjectDir    string
	Provider      string
	Compute       project.Compute
	RepositoryUrl pulumi.StringInput
	TempDir       string
	Server        pulumi.StringInput
	Username      pulumi.StringInput
	Password      pulumi.StringInput
}

type Image struct {
	pulumi.ResourceState

	Name        string
	DockerImage *dockerbuildkit.Image
}

func NewImage(ctx *pulumi.Context, name string, args *ImageArgs, opts ...pulumi.ResourceOption) (*Image, error) {
	res := &Image{Name: name}

	err := ctx.RegisterComponentResource("nitric:Image", name, res, opts...)
	if err != nil {
		return nil, err
	}

	buildArgs, err := runtime.WrapperBuildArgs(
		&runtime.WrapperBuildArgsConfig{
			ProjectDir:           args.ProjectDir,
			ImageName:            args.SourceImage,
			Provider:             args.Provider,
			MembraneVersion:      project.DefaultMembraneVersion,
			OtelCollectorVersion: project.DefaultOTELCollectorVersion,
			Telemetry:            args.Compute.Unit().Telemetry,
		})
	if err != nil {
		return nil, err
	}

	dockerFilePath, err := dockerfile(args.ProjectDir, buildArgs.Dockerfile, args.Compute)
	if err != nil {
		return nil, err
	}

	relDocker, err := filepath.Rel(args.ProjectDir, dockerFilePath)
	if err != nil {
		return nil, err
	}

	bArgs := make(dockerbuildkit.BuildArgArray, 0)
	for k, v := range buildArgs.Args {
		bArgs = append(bArgs, dockerbuildkit.BuildArgArgs{
			Name:  pulumi.String(k),
			Value: pulumi.String(v),
		})
	}

	imageArgs := &dockerbuildkit.ImageArgs{
		Name:       args.RepositoryUrl,
		Context:    pulumi.String(args.ProjectDir),
		Dockerfile: pulumi.String(relDocker),
		Args:       bArgs,
		Registry: dockerbuildkit.RegistryArgs{
			Server:   args.Server,
			Username: args.Username,
			Password: args.Password,
		},
	}

	res.DockerImage, err = dockerbuildkit.NewImage(ctx, name+"-image", imageArgs, append(opts, pulumi.Parent(res))...)
	if err != nil {
		return nil, err
	}

	return res, ctx.RegisterResourceOutputs(res, pulumi.Map{
		"name":     pulumi.String(res.Name),
		"imageUri": res.DockerImage.Name,
	})
}

func (d *Image) URI() pulumi.StringOutput {
	return d.DockerImage.RepoDigest
}
