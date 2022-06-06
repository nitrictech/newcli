package azure

import (
	"fmt"
	"strings"

	"github.com/nitrictech/cli/pkg/project"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-azure-native/sdk/go/azure/app"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type ScheduleArgs struct {
	Schedule          project.Schedule
	Functions         *ContainerApps
	ResourceGroupName pulumi.StringInput
}

type Schedule struct {
	pulumi.ResourceState

	Name      string
	Component *app.DaprComponent
}

func newContainerApp(ctx *pulumi.Context, name string, args *ScheduleArgs, opts ...pulumi.ResourceOption) (*Schedule, error) {
	res := &Schedule{
		Name: name,
	}
	err := ctx.RegisterComponentResource("nitric:func:ContainerApp", name, res, opts...)
	if err != nil {
		return nil, err
	}

	if args.Schedule.Target.Type == "function" {
		if a, ok := args.Functions.Apps[args.Schedule.Target.Name]; ok {
			res.Component, err = app.NewDaprComponent(ctx, name+"-component", &app.DaprComponentArgs{
				ResourceGroupName: args.ResourceGroupName,
				EnvironmentName:   a.Environment.Name,
				// Bind this component by it's description key
				// It will POST to the given component on this name
				// e.g host/<NAME>
				Name:          pulumi.String(strings.ReplaceAll(strings.ToLower(name), " ", "-")),
				ComponentType: pulumi.String("bindings.cron"),
				Version:       pulumi.String("v1"),
				Metadata: app.DaprMetadataArray{
					app.DaprMetadataArgs{
						Name:  pulumi.String("schedule"),
						Value: pulumi.String(args.Schedule.Expression),
					},
				},
				Scopes: pulumi.StringArray{
					// Limit the scope to the target container app
					a.App.Name,
				},
			})

			if err != nil {
				return nil, errors.WithMessage(err, "could not create DaprComponent for app")
			}
		} else {
			return nil, fmt.Errorf("could not resolve container app")
		}
	} else {
		return nil, fmt.Errorf("unsupported schedule target type")
	}

	return res, nil
}
