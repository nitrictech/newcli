package gcp

import (
	"fmt"
	"strings"

	"github.com/nitrictech/cli/pkg/project"
	"github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/cloudscheduler"
	"github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/pubsub"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type ScheduleArgs struct {
	Schedule  project.Schedule
	Topics    map[string]*pubsub.Topic
	Functions map[string]*CloudRunner
}

type Schedule struct {
	pulumi.ResourceState

	Name string
	Job  *cloudscheduler.Job
}

// Create a new schedule
func newSchedule(ctx *pulumi.Context, name string, args *ScheduleArgs, opts ...pulumi.ResourceOption) (*Schedule, error) {
	res := &Schedule{
		Name: name,
	}
	err := ctx.RegisterComponentResource("nitric:schedule:GCPCloudSchedulerJob", name, res, opts...)
	if err != nil {
		return nil, err
	}

	var functionTarget cloudscheduler.JobHttpTargetPtrInput = nil
	var topicTarget cloudscheduler.JobPubsubTargetPtrInput = nil

	switch args.Schedule.Target.Type {
	case "topic":
		// ensure the topic exists and target it
		if t, ok := args.Topics[args.Schedule.Target.Name]; ok {
			topicTarget = cloudscheduler.JobPubsubTargetArgs{
				Attributes: pulumi.ToStringMap(map[string]string{"x-nitric-topic": args.Schedule.Target.Name}),
				TopicName:  pulumi.Sprintf("projects/%s/topics/%s", t.Project, t.Name),
				Data:       pulumi.String(""),
			}
		}
	case "function":
		// ensure the function exists and target it
		if f, ok := args.Functions[args.Schedule.Target.Name]; ok {
			functionTarget = cloudscheduler.JobHttpTargetArgs{
				HttpMethod: pulumi.String("POST"),
				Body:       pulumi.String(""),
				Uri:        pulumi.Sprintf("%s/x-nitric-schedule/%s", f.Url, f.Name),
				OidcToken: cloudscheduler.JobHttpTargetOidcTokenArgs{
					Audience:            f.Url,
					ServiceAccountEmail: f.Invoker.Email,
				},
			}
		}
	default:
		// return an error
		return nil, fmt.Errorf("")
	}

	if topicTarget == nil && functionTarget == nil {
		return nil, fmt.Errorf("unable to resolve schedule target")
	}

	// If the target type is a function then setup a HttpTarget
	res.Job, err = cloudscheduler.NewJob(ctx, name, &cloudscheduler.JobArgs{
		TimeZone:     pulumi.String("UTC"),
		HttpTarget:   functionTarget,
		PubsubTarget: topicTarget,
		Schedule:     pulumi.String(strings.ReplaceAll(args.Schedule.Expression, "'", "")),
	}, pulumi.Parent(res))

	if err != nil {
		return nil, err
	}

	return res, nil
}
