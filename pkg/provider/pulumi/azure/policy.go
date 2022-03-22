package azure

import (
	"fmt"

	"github.com/google/uuid"
	authorization "github.com/pulumi/pulumi-azure-native/sdk/go/azure/authorization"
	"github.com/pulumi/pulumi-azure/sdk/v4/go/azure/cosmosdb"
	"github.com/pulumi/pulumi-azure/sdk/v4/go/azure/eventgrid"
	"github.com/pulumi/pulumi-azure/sdk/v4/go/azure/keyvault"
	"github.com/pulumi/pulumi-azure/sdk/v4/go/azure/storage"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	v1 "github.com/nitrictech/nitric/pkg/api/nitric/v1"
)

type Policy struct {
	pulumi.ResourceState

	Name         string
	RolePolicies []*authorization.RoleDefinition
}

type StackResources struct {
	Topics      map[string]*eventgrid.Topic
	Queues      map[string]*storage.Queue
	Buckets     map[string]*storage.Container
	Collections map[string]*cosmosdb.MongoCollection
	Secrets     map[string]*keyvault.Secret
}

type PolicyArgs struct {
	Policy *v1.PolicyResource
	// Resources in the stack that must be protected
	Resources         *StackResources
	ResourceGroupName pulumi.StringInput
	SubscriptionID    string
	principalMap      PrincipalMap
}

type AzureRole struct {
	Actions        []string
	DataActions    []string
	NotActions     []string
	NotDataActions []string
}

//List of action -> Azure built in role ID
var azureActionsMap map[v1.Action]*AzureRole = map[v1.Action]*AzureRole{
	v1.Action_BucketFileList: {
		Actions: []string{
			"Microsoft.Storage/storageAccounts/blobServices/containers/read",
			"Microsoft.Storage/storageAccounts/blobServices/generateUserDelegationKey/action",
		},
		DataActions: []string{
			"Microsoft.Storage/storageAccounts/blobServices/containers/blobs/read",
		},
	},
	v1.Action_BucketFileGet: {
		Actions: []string{
			"Microsoft.Storage/storageAccounts/blobServices/containers/read",
			"Microsoft.Storage/storageAccounts/blobServices/generateUserDelegationKey/action",
		},
		DataActions: []string{
			"Microsoft.Storage/storageAccounts/blobServices/containers/blobs/read",
		},
	},
	v1.Action_BucketFileDelete: {
		Actions: []string{
			"Microsoft.Storage/storageAccounts/blobServices/containers/delete",
			"Microsoft.Storage/storageAccounts/blobServices/generateUserDelegationKey/action",
		},
		DataActions: []string{
			"Microsoft.Storage/storageAccounts/blobServices/containers/blobs/delete",
		},
	},
	v1.Action_BucketFilePut: {
		Actions: []string{
			"Microsoft.Storage/storageAccounts/blobServices/containers/write",
			"Microsoft.Storage/storageAccounts/blobServices/generateUserDelegationKey/action",
		},
		DataActions: []string{
			"Microsoft.Storage/storageAccounts/blobServices/containers/blobs/write",
			"Microsoft.Storage/storageAccounts/blobServices/containers/blobs/add/action",
			"Microsoft.Storage/storageAccounts/blobServices/containers/blobs/move/action",
		},
	},
	v1.Action_QueueDetail: {
		Actions: []string{
			"Microsoft.Storage/storageAccounts/queueServices/queues/write",
		},
	},
	v1.Action_QueueSend: {
		DataActions: []string{
			"Microsoft.Storage/storageAccounts/queueServices/queues/messages/add/action",
		},
	},
	v1.Action_QueueReceive: {
		Actions: []string{
			"Microsoft.Storage/storageAccounts/queueServices/queues/read",
		},
		DataActions: []string{
			"Microsoft.Storage/storageAccounts/queueServices/queues/messages/read",
			"Microsoft.Storage/storageAccounts/queueServices/queues/messages/process/action",
		},
	},
	v1.Action_QueueList: {
		Actions: []string{
			"Microsoft.Storage/storageAccounts/queueServices/queues/read",
		},
	},
	v1.Action_CollectionDocumentRead: {
		Actions: []string{
			"Microsoft.DocumentDB/databaseAccounts/*",
			"Microsoft.DocumentDB/*/read",
			"Microsoft.Resources/deployments/*",
		},
	},
	v1.Action_CollectionDocumentWrite: {
		Actions: []string{
			"Microsoft.DocumentDB/databaseAccounts/*",
			"Microsoft.DocumentDB/*/read",
			"Microsoft.Resources/deployments/*",
		},
	},
	v1.Action_CollectionDocumentDelete: {
		Actions: []string{
			"Microsoft.DocumentDB/databaseAccounts/*",
			"Microsoft.DocumentDB/*/read",
			"Microsoft.Resources/deployments/*",
		},
	},
	v1.Action_CollectionList: {
		Actions: []string{
			"Microsoft.DocumentDB/databaseAccounts/*",
			"Microsoft.DocumentDB/*/read",
			"Microsoft.Resources/deployments/*",
		},
	},
	v1.Action_CollectionQuery: {
		Actions: []string{
			"Microsoft.DocumentDB/databaseAccounts/*",
			"Microsoft.DocumentDB/*/read",
			"Microsoft.Resources/deployments/*",
		},
	},
	v1.Action_TopicDetail: {
		Actions: []string{
			"Microsoft.EventGrid/topics/read",
			"Microsoft.Authorization/*/read",
			"Microsoft.EventGrid/eventSubscriptions/read",
		},
	},
	v1.Action_TopicEventPublish: {
		Actions: []string{
			"Microsoft.EventGrid/topics/read",
			"Microsoft.Authorization/*/read",
			"Microsoft.EventGrid/domains/read",
		},
		DataActions: []string{
			"Microsoft.EventGrid/events/send/action",
		},
	},
	v1.Action_TopicList: {
		Actions: []string{
			"Microsoft.EventGrid/topics/read",
			"Microsoft.Authorization/*/read",
			"Microsoft.EventGrid/eventSubscriptions/read",
		},
	},
	v1.Action_SecretPut: {
		Actions: []string{
			"Microsoft.KeyVault/vaults/secrets/write",
		},
		DataActions: []string{
			"Microsoft.KeyVault/vaults/secrets/update/action",
			"Microsoft.KeyVault/vaults/secrets/setSecret/action",
		},
	},
	v1.Action_SecretAccess: {
		Actions: []string{
			"Microsoft.KeyVault/vaults/secrets/read",
		},
		DataActions: []string{
			"Microsoft.KeyVault/vaults/secrets/getSecret/action",
			"Microsoft.KeyVault/vaults/secrets/readMetadata/action",
		},
	},
}

func actionsToAzureActions(actions []v1.Action) map[string]pulumi.StringArray {
	var azurePerm map[string]pulumi.StringArray = map[string]pulumi.StringArray{
		"Actions":        {},
		"DataActions":    {},
		"NotActions":     {},
		"NotDataActions": {},
	}

	for _, action := range actions {
		for _, a := range azureActionsMap[action].Actions {
			azurePerm["Actions"] = append(azurePerm["Actions"], pulumi.String(a))
		}
		for _, da := range azureActionsMap[action].DataActions {
			azurePerm["DataActions"] = append(azurePerm["DataActions"], pulumi.String(da))
		}
		for _, na := range azureActionsMap[action].NotActions {
			azurePerm["NotActions"] = append(azurePerm["NotActions"], pulumi.String(na))
		}
		for _, nda := range azureActionsMap[action].NotDataActions {
			azurePerm["NotDataActions"] = append(azurePerm["NotDataActions"], pulumi.String(nda))
		}
	}
	return azurePerm
}

func getResourceScope(resourceType v1.ResourceType, resourceName string, stackResources *StackResources) (pulumi.StringOutput, error) {
	switch resourceType {
	case v1.ResourceType_Bucket:
		return stackResources.Buckets[resourceName].ResourceManagerId, nil
	case v1.ResourceType_Queue:
		return stackResources.Queues[resourceName].ID().ToStringOutput(), nil
	case v1.ResourceType_Topic:
		return stackResources.Topics[resourceName].ID().ToStringOutput(), nil
	case v1.ResourceType_Collection:
		return stackResources.Collections[resourceName].ID().ToStringOutput(), nil
	case v1.ResourceType_Secret:
		return stackResources.Secrets[resourceName].ID().ToStringOutput(), nil
	default:
		return pulumi.StringOutput{}, fmt.Errorf("resource type %s is not a valid resource type", resourceType.String())
	}
}

func newRoleID() pulumi.String {
	uuid := uuid.New()
	return pulumi.String(uuid.String())
}

func newPolicy(ctx *pulumi.Context, name string, args *PolicyArgs, opts ...pulumi.ResourceOption) (*Policy, error) {
	res := &Policy{Name: name, RolePolicies: make([]*authorization.RoleDefinition, 0)}
	err := ctx.RegisterComponentResource("nitric:policy:AzurePolicyRoles", name, res, opts...)
	if err != nil {
		return nil, err
	}

	// Get Actions
	azurePerm := actionsToAzureActions(args.Policy.Actions)

	for _, principal := range args.Policy.Principals {
		for _, resource := range args.Policy.Resources {
			resourceScope, err := getResourceScope(resource.Type, resource.Name, args.Resources)
			if err != nil {
				return nil, err
			}

			roleName := pulumi.Sprintf("%s %s %s %s Role", principal.Type.String(), principal.Name, resource.Type.String(), resource.Name)
			roleID := newRoleID()

			role, err := authorization.NewRoleDefinition(ctx, resourceName(ctx, string(roleID), RoleDefinitionRT), &authorization.RoleDefinitionArgs{
				RoleName:         roleName,
				RoleDefinitionId: roleID,
				Scope:            resourceScope,
				Permissions: &authorization.PermissionArray{
					&authorization.PermissionArgs{
						Actions:        azurePerm["Actions"],
						DataActions:    azurePerm["DataActions"],
						NotActions:     azurePerm["NotActions"],
						NotDataActions: azurePerm["NotDataActions"],
					},
				},
				AssignableScopes: pulumi.StringArray{pulumi.String(args.SubscriptionID), pulumi.Sprintf("%s/resourceGroups/%s", args.SubscriptionID, args.ResourceGroupName), resourceScope},
			}, pulumi.Parent(res))
			if err != nil {
				return nil, err
			}

			servicePrincipalID := args.principalMap[principal.Type][principal.Name]
			_, assignmentErr := authorization.NewRoleAssignment(ctx, resourceName(ctx, string(roleID), RoleAssignmentRT), &authorization.RoleAssignmentArgs{
				PrincipalId:      servicePrincipalID,
				PrincipalType:    pulumi.String("ServicePrincipal"),
				RoleDefinitionId: role.ID(),
				Scope:            resourceScope,
			}, pulumi.Parent(res))
			if assignmentErr != nil {
				return nil, err
			}

			res.RolePolicies = append(res.RolePolicies, role)
		}
	}

	if err != nil {
		return nil, err
	}

	return nil, nil
}
