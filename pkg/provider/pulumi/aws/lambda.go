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
	"encoding/json"
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/iam"
	awslambda "github.com/pulumi/pulumi-aws/sdk/v4/go/aws/lambda"
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/sns"
	"github.com/pulumi/pulumi-docker/sdk/v3/go/docker"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/nitrictech/cli/pkg/provider/pulumi/common"
	"github.com/nitrictech/cli/pkg/stack"
)

type LambdaArgs struct {
	Topics      map[string]*sns.Topic
	DockerImage *docker.Image
	Compute     stack.Compute
}

type Lambda struct {
	pulumi.ResourceState

	Name     string
	Function *awslambda.Function
	Role     *iam.Role
}

func newLambda(ctx *pulumi.Context, name string, args *LambdaArgs, opts ...pulumi.ResourceOption) (*Lambda, error) {
	res := &Lambda{Name: name}
	err := ctx.RegisterComponentResource("nitric:func:AWSLambda", name, res, opts...)
	if err != nil {
		return nil, err
	}

	tmpJSON, err := json.Marshal(map[string]interface{}{
		"Version": "2012-10-17",
		"Statement": []map[string]interface{}{
			{
				"Sid":    "",
				"Effect": "Allow",
				"Principal": map[string]interface{}{
					"Service": "lambda.amazonaws.com",
				},
				"Action": "sts:AssumeRole",
			},
		},
	})
	if err != nil {
		return nil, err
	}

	res.Role, err = iam.NewRole(ctx, name+"LambdaRole", &iam.RoleArgs{
		AssumeRolePolicy: pulumi.String(tmpJSON),
		Tags:             common.Tags(ctx, name+"LambdaRole"),
	}, pulumi.Parent(res))
	if err != nil {
		return nil, err
	}

	_, err = iam.NewRolePolicyAttachment(ctx, name+"LambdaBasicExecution", &iam.RolePolicyAttachmentArgs{
		PolicyArn: iam.ManagedPolicyAWSLambdaBasicExecutionRole,
		Role:      res.Role.ID(),
	}, pulumi.Parent(res))
	if err != nil {
		return nil, err
	}

	// Add resource list permissions
	// Currently the membrane will use list operations
	tmpJSON, err = json.Marshal(map[string]interface{}{
		"Version": "2012-10-17",
		"Statement": []map[string]interface{}{
			{
				"Action": []string{
					// "sns:ConfirmSubscription",
					// "sns:Unsubscribe",
					"sns:ListTopics",
					"sqs:ListQueues",
					"dynamodb:ListTables",
					"s3:ListAllMyBuckets",
				},
				"Effect":   "Allow",
				"Resource": "*",
			},
		},
	})
	if err != nil {
		return nil, err
	}

	// TODO: Lock this SNS topics for which this function has pub definitions
	// FIXME: Limit to known resources
	_, err = iam.NewRolePolicy(ctx, name+"ListAccess", &iam.RolePolicyArgs{
		Role:   res.Role.ID(),
		Policy: pulumi.String(tmpJSON),
	}, pulumi.Parent(res))
	if err != nil {
		return nil, err
	}

	// tmpJSON, err = json.Marshal(map[string]interface{}{
	// 	"Version": "2012-10-17",
	// 	"Statement": []map[string]interface{}{
	// 		{
	// 			"Action": []string{
	// 				"secretsmanager:DescribeSecret",
	// 				"secretsmanager:PutSecretValue",
	// 				"secretsmanager:CreateSecret",
	// 				"secretsmanager:DeleteSecret",
	// 				"secretsmanager:CancelRotateSecret",
	// 				"secretsmanager:ListSecretVersionIds",
	// 				"secretsmanager:UpdateSecret",
	// 				"secretsmanager:GetRandomPassword",
	// 				"secretsmanager:GetResourcePolicy",
	// 				"secretsmanager:GetSecretValue",
	// 				"secretsmanager:StopReplicationToReplica",
	// 				"secretsmanager:ReplicateSecretToRegions",
	// 				"secretsmanager:RestoreSecret",
	// 				"secretsmanager:RotateSecret",
	// 				"secretsmanager:UpdateSecretVersionStage",
	// 				"secretsmanager:RemoveRegionsFromReplication",
	// 			},
	// 			"Effect":   "Allow",
	// 			"Resource": "*",
	// 		},
	// 	},
	// })
	// if err != nil {
	// 	return nil, err
	// }
	// _, err = iam.NewRolePolicy(ctx, name+"SecretsAccess", &iam.RolePolicyArgs{
	// 	Role:   res.Role.ID(),
	// 	Policy: pulumi.String(tmpJSON),
	// }, pulumi.Parent(res))
	// if err != nil {
	// 	return nil, err
	// }

	memory := 128
	if args.Compute.Unit().Memory > 0 {
		memory = args.Compute.Unit().Memory
	}
	res.Function, err = awslambda.NewFunction(ctx, name, &awslambda.FunctionArgs{
		ImageUri:    args.DockerImage.ImageName,
		MemorySize:  pulumi.IntPtr(memory),
		Timeout:     pulumi.IntPtr(15),
		PackageType: pulumi.String("Image"),
		Role:        res.Role.Arn,
		Tags:        common.Tags(ctx, name),
	}, pulumi.Parent(res))
	if err != nil {
		return nil, err
	}

	for _, t := range args.Compute.Unit().Triggers.Topics {
		topic, ok := args.Topics[t]
		if ok {
			_, err = awslambda.NewPermission(ctx, name+"Permission", &awslambda.PermissionArgs{
				SourceArn: topic.Arn,
				Function:  res.Function.Name,
				Principal: pulumi.String("sns.amazonaws.com"),
				Action:    pulumi.String("lambda:InvokeFunction"),
			}, pulumi.Parent(res))
			if err != nil {
				return nil, err
			}

			_, err = sns.NewTopicSubscription(ctx, name+"Subscription", &sns.TopicSubscriptionArgs{
				Endpoint: res.Function.Arn,
				Protocol: pulumi.String("lambda"),
				Topic:    topic.ID(), // TODO check (was topic.sns)
			}, pulumi.Parent(res))
			if err != nil {
				return nil, err
			}
		} else {
			fmt.Printf("WARNING: Function %s has a Trigger %s, but the topic is missing", name, t)
		}
	}

	return res, ctx.RegisterResourceOutputs(res, pulumi.Map{
		"name":   pulumi.String(res.Name),
		"lambda": res.Function,
	})
}
