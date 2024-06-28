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

package collector

import (
	"crypto/md5"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/samber/lo"
	"github.com/spf13/afero"

	"github.com/nitrictech/cli/pkg/project/runtime"
	"github.com/nitrictech/cli/pkg/view/tui/components/view"
	apispb "github.com/nitrictech/nitric/core/pkg/proto/apis/v1"
	deploymentspb "github.com/nitrictech/nitric/core/pkg/proto/deployments/v1"
	resourcespb "github.com/nitrictech/nitric/core/pkg/proto/resources/v1"
	schedulespb "github.com/nitrictech/nitric/core/pkg/proto/schedules/v1"
	websocketspb "github.com/nitrictech/nitric/core/pkg/proto/websockets/v1"
)

type ProjectErrors struct {
	errors []error
}

func (pe *ProjectErrors) Add(err error) {
	pe.errors = append(pe.errors, err)
}

func (pe ProjectErrors) Error() error {
	if len(pe.errors) > 0 {
		errorView := view.New()

		errorView.Addln("Errors found in project:")

		for _, err := range pe.errors {
			errorView.Addln("- %s", err.Error()).WithStyle(lipgloss.NewStyle().MarginLeft(2))
		}

		return fmt.Errorf(errorView.Render())
	}

	return nil
}

// buildBucketRequirements gathers and deduplicates all bucket requirements
func buildBucketRequirements(allServiceRequirements []*ServiceRequirements, allBatchRequirements []*BatchRequirements, projectErrors *ProjectErrors) ([]*deploymentspb.Resource, error) {
	resources := []*deploymentspb.Resource{}

	for _, serviceRequirements := range allServiceRequirements {
		for bucketName := range serviceRequirements.buckets {
			notifications := []*deploymentspb.BucketListener{}

			for _, v := range serviceRequirements.listeners {
				notifications = append(notifications, &deploymentspb.BucketListener{
					Config: v,
					Target: &deploymentspb.BucketListener_Service{
						Service: serviceRequirements.serviceName,
					},
				})
			}

			res, exists := lo.Find(resources, func(item *deploymentspb.Resource) bool {
				return item.Id.Name == bucketName
			})

			if exists {
				// add the listeners to the bucket configuration
				res.GetBucket().Listeners = append(res.GetBucket().Listeners, notifications...)
			} else {
				res := &deploymentspb.Resource{
					Id: &resourcespb.ResourceIdentifier{
						Name: bucketName,
						Type: resourcespb.ResourceType_Bucket,
					},
					Config: &deploymentspb.Resource_Bucket{
						Bucket: &deploymentspb.Bucket{
							Listeners: notifications,
						},
					},
				}
				resources = append(resources, res)
			}
		}
	}

	// TODO: Consolidate duplicate code for batch requirement handling
	for _, serviceRequirements := range allBatchRequirements {
		for bucketName := range serviceRequirements.buckets {
			notifications := []*deploymentspb.BucketListener{}

			_, exists := lo.Find(resources, func(item *deploymentspb.Resource) bool {
				return item.Id.Name == bucketName
			})

			if !exists {
				res := &deploymentspb.Resource{
					Id: &resourcespb.ResourceIdentifier{
						Name: bucketName,
						Type: resourcespb.ResourceType_Bucket,
					},
					Config: &deploymentspb.Resource_Bucket{
						Bucket: &deploymentspb.Bucket{
							Listeners: notifications,
						},
					},
				}
				resources = append(resources, res)
			}
		}
	}

	return resources, nil
}

// buildHttpRequirements gathers and deduplicates all http requirements
func buildHttpRequirements(allServiceRequirements []*ServiceRequirements, projectErrors *ProjectErrors) ([]*deploymentspb.Resource, error) {
	resources := []*deploymentspb.Resource{}

	for _, serviceRequirements := range allServiceRequirements {
		if serviceRequirements.proxy != nil {
			resources = append(resources, &deploymentspb.Resource{
				Id: &resourcespb.ResourceIdentifier{
					Name: serviceRequirements.serviceName,
					Type: resourcespb.ResourceType_Http,
				},
				Config: &deploymentspb.Resource_Http{
					Http: &deploymentspb.Http{
						Target: &deploymentspb.HttpTarget{
							Target: &deploymentspb.HttpTarget_Service{
								Service: serviceRequirements.serviceName,
							},
						},
					},
				},
			})
		}
	}

	return resources, nil
}

//go:embed default-migrations.dockerfile
var defaultMigrationFileContents string

// TODO: validate scheme types and paths
var schemeRegex = regexp.MustCompile(`(?P<Scheme>^[a-z]+)://(?P<Path>.*)$`)

func parseMigrationsScheme(migrationsPath string) (string, string, error) {
	match := schemeRegex.FindStringSubmatch(migrationsPath)
	if match == nil {
		return "", "", fmt.Errorf("invalid migrations URI: %s", migrationsPath)
	}

	result := make(map[string]string)

	for i, name := range schemeRegex.SubexpNames() {
		if i != 0 && name != "" {
			result[name] = match[i]
		}
	}

	return result["Scheme"], result["Path"], nil
}

// Collect a list of migration images that need to be built
// these requirements need to be supplied to the deployment serviceS
func GetMigrationImageBuildContexts(allServiceRequirements []*ServiceRequirements, fs afero.Fs) (map[string]*runtime.RuntimeBuildContext, error) {
	imageBuildContexts := map[string]*runtime.RuntimeBuildContext{}
	declaredConfigs := map[string]string{}

	for _, serviceRequirements := range allServiceRequirements {
		for databaseName, databaseConfig := range serviceRequirements.sqlDatabases {
			if databaseConfig.Migrations != nil && databaseConfig.Migrations.GetMigrationsPath() != "" {
				scheme, path, err := parseMigrationsScheme(databaseConfig.Migrations.GetMigrationsPath())
				if err != nil {
					return nil, err
				}

				// if the db has already been declared check that it dies not differ from a previous declaration
				if _, exists := imageBuildContexts[databaseName]; exists {
					if declaredConfigs[databaseName] != databaseConfig.Migrations.GetMigrationsPath() {
						return nil, fmt.Errorf("multiple migrations paths declared for database '%s'", databaseName)
					}
					// otherwise set named config to the already build config
					continue
				}

				declaredConfigs[databaseName] = databaseConfig.Migrations.GetMigrationsPath()

				switch scheme {
				case "dockerfile":
					// Read the referenced dockerfile
					dockerfileContents, err := afero.ReadFile(fs, path)
					if err != nil {
						return nil, err
					}

					imageBuildContexts[databaseName] = &runtime.RuntimeBuildContext{
						BuildArguments:     map[string]string{},
						DockerfileContents: string(dockerfileContents),
						BaseDirectory:      ".",
					}
				case "file":
					// Default dockerfile build context for the given path
					imageBuildContexts[databaseName] = &runtime.RuntimeBuildContext{
						BuildArguments: map[string]string{
							"MIGRATIONS_PATH": path,
						},
						DockerfileContents: defaultMigrationFileContents,
						BaseDirectory:      ".",
					}
				default:
					return nil, fmt.Errorf("unsupported migration path scheme: %s, must be one of dockerfile or file", scheme)
				}
			}
		}
	}

	return imageBuildContexts, nil
}

func buildDatabaseRequirements(allServiceRequirements []*ServiceRequirements, allBatchRequirements []*BatchRequirements, projectErrors *ProjectErrors) ([]*deploymentspb.Resource, error) {
	resources := []*deploymentspb.Resource{}

	allDatabases := []map[string]*resourcespb.SqlDatabaseResource{}

	for _, serviceRequirements := range allServiceRequirements {
		allDatabases = append(allDatabases, serviceRequirements.sqlDatabases)
	}

	for _, batchRequirements := range allBatchRequirements {
		allDatabases = append(allDatabases, batchRequirements.sqlDatabases)
	}

	for _, dbs := range allDatabases {
		for databaseName, dbConfig := range dbs {
			_, exists := lo.Find(resources, func(item *deploymentspb.Resource) bool {
				return item.Id.Name == databaseName
			})

			var migrations *deploymentspb.SqlDatabase_ImageUri = nil
			if dbConfig.Migrations != nil && dbConfig.Migrations.GetMigrationsPath() != "" {
				migrations = &deploymentspb.SqlDatabase_ImageUri{
					// FIXME: make this repeatable
					ImageUri: databaseName + "-migrations",
				}
			}

			if !exists {
				res := &deploymentspb.Resource{
					Id: &resourcespb.ResourceIdentifier{
						Name: databaseName,
						Type: resourcespb.ResourceType_SqlDatabase,
					},
					Config: &deploymentspb.Resource_SqlDatabase{
						SqlDatabase: &deploymentspb.SqlDatabase{
							Migrations: migrations,
						},
					},
				}
				resources = append(resources, res)
			}
		}
	}

	return resources, nil
}

// buildTopicRequirements gathers and deduplicates all topic requirements
func buildTopicRequirements(allServiceRequirements []*ServiceRequirements, allBatchRequirements []*BatchRequirements, projectErrors *ProjectErrors) ([]*deploymentspb.Resource, error) {
	resources := []*deploymentspb.Resource{}

	for _, serviceRequirements := range allServiceRequirements {
		for topicName := range serviceRequirements.topics {
			res, exists := lo.Find(resources, func(item *deploymentspb.Resource) bool {
				return item.Id.Name == topicName
			})

			if !exists {
				res = &deploymentspb.Resource{
					Id: &resourcespb.ResourceIdentifier{
						Name: topicName,
						Type: resourcespb.ResourceType_Topic,
					},
					Config: &deploymentspb.Resource_Topic{
						Topic: &deploymentspb.Topic{
							Subscriptions: []*deploymentspb.SubscriptionTarget{},
						},
					},
				}
				resources = append(resources, res)
			}

			if len(serviceRequirements.subscriptions[topicName]) > 0 {
				res.GetTopic().Subscriptions = append(res.GetTopic().Subscriptions, &deploymentspb.SubscriptionTarget{
					Target: &deploymentspb.SubscriptionTarget_Service{
						Service: serviceRequirements.serviceName,
					},
				})
			}
		}
	}

	// FIXME: Reduce duplicate code
	// TODO: May be unecessary as any topic requirements here would be publishing for services to respond to already
	for _, batchRequirements := range allBatchRequirements {
		for topicName := range batchRequirements.topics {
			res, exists := lo.Find(resources, func(item *deploymentspb.Resource) bool {
				return item.Id.Name == topicName
			})

			if !exists {
				res = &deploymentspb.Resource{
					Id: &resourcespb.ResourceIdentifier{
						Name: topicName,
						Type: resourcespb.ResourceType_Topic,
					},
					Config: &deploymentspb.Resource_Topic{
						Topic: &deploymentspb.Topic{
							Subscriptions: []*deploymentspb.SubscriptionTarget{},
						},
					},
				}
				resources = append(resources, res)
			}
		}
	}

	return resources, nil
}

// buildQueueRequirements gathers and deduplicates all queue requirements
func buildQueueRequirements(allServiceRequirements []*ServiceRequirements, allBatchRequirements []*BatchRequirements, projectErrors *ProjectErrors) ([]*deploymentspb.Resource, error) {
	resources := []*deploymentspb.Resource{}

	allQueues := []map[string]*resourcespb.QueueResource{}

	for _, serviceRequirements := range allServiceRequirements {
		allQueues = append(allQueues, serviceRequirements.queues)
	}

	for _, batchRequirements := range allBatchRequirements {
		allQueues = append(allQueues, batchRequirements.queues)
	}

	for _, queues := range allQueues {
		for queueName := range queues {
			_, exists := lo.Find(resources, func(item *deploymentspb.Resource) bool {
				return item.Id.Name == queueName
			})

			if !exists {
				res := &deploymentspb.Resource{
					Id: &resourcespb.ResourceIdentifier{
						Name: queueName,
						Type: resourcespb.ResourceType_Queue,
					},
					Config: &deploymentspb.Resource_Queue{
						Queue: &deploymentspb.Queue{},
					},
				}
				resources = append(resources, res)
			}
		}
	}

	return resources, nil
}

// buildSecretRequirements gathers and deduplicates all secret requirements
func buildSecretRequirements(allServiceRequirements []*ServiceRequirements, allBatchRequirements []*BatchRequirements, projectErrors *ProjectErrors) ([]*deploymentspb.Resource, error) {
	resources := []*deploymentspb.Resource{}

	allSecrets := []map[string]*resourcespb.SecretResource{}

	for _, serviceRequirements := range allServiceRequirements {
		allSecrets = append(allSecrets, serviceRequirements.secrets)
	}

	for _, batchRequirements := range allBatchRequirements {
		allSecrets = append(allSecrets, batchRequirements.secrets)
	}

	for _, secrets := range allSecrets {
		for secretName := range secrets {
			_, exists := lo.Find(resources, func(item *deploymentspb.Resource) bool {
				return item.Id.Name == secretName
			})

			if !exists {
				res := &deploymentspb.Resource{
					Id: &resourcespb.ResourceIdentifier{
						Name: secretName,
						Type: resourcespb.ResourceType_Secret,
					},
					Config: &deploymentspb.Resource_Secret{
						Secret: &deploymentspb.Secret{},
					},
				}
				// add the listeners to the bucket configuration
				resources = append(resources, res)
			}
		}
	}

	return resources, nil
}

func ensureOneTrailingSlash(p string) string {
	if len(p) > 0 && string(p[len(p)-1]) == "/" {
		return p
	}

	return p + "/"
}

// openAPIPathAndParams splits a path into an OpenAPI 3 path and a list of OpenAPI 3 parameters
// this is done by splitting the path on '/' and looking for path parameters (e.g. /foo/:bar)
// and replacing them with OpenAPI 3 parameters (e.g. /foo/{bar})
func openAPIPathAndParams(workerPath string) (string, openapi3.Parameters) {
	normalizedPath := ""
	params := make(openapi3.Parameters, 0)

	for _, p := range strings.Split(workerPath, "/") {
		if strings.HasPrefix(p, ":") {
			paramName := strings.Replace(p, ":", "", -1)

			params = append(params, &openapi3.ParameterRef{
				Value: &openapi3.Parameter{
					In:       "path",
					Name:     paramName,
					Required: true,
					Schema: &openapi3.SchemaRef{
						Value: &openapi3.Schema{
							Type: "string",
						},
					},
				},
			})
			normalizedPath = ensureOneTrailingSlash(normalizedPath + "{" + paramName + "}")
		} else {
			normalizedPath = ensureOneTrailingSlash(normalizedPath + p)
		}
	}
	// trim off trailing slash
	if normalizedPath != "/" {
		normalizedPath = strings.TrimSuffix(normalizedPath, "/")
	}

	return normalizedPath, params
}

var notAlphaNumeric, _ = regexp.Compile("[^a-zA-Z0-9]+")

// buildApiRequirements gathers and deduplicates all api requirements
func buildApiRequirements(allServiceRequirements []*ServiceRequirements, projectErrors *ProjectErrors) ([]*deploymentspb.Resource, error) {
	resources := []*deploymentspb.Resource{}

	apis := map[string]*openapi3.T{}

	for _, serviceRequirements := range allServiceRequirements {
		for apiName, apiResource := range serviceRequirements.apis {
			api, exists := apis[apiName]
			if !exists {
				api = &openapi3.T{
					Paths: make(openapi3.Paths),
					Info: &openapi3.Info{
						Title:   apiName,
						Version: "v1",
					},
					OpenAPI: "3.0.1",
					Components: &openapi3.Components{
						SecuritySchemes: make(openapi3.SecuritySchemes),
					},
				}

				apis[apiName] = api
			}

			// Add security schemes to the documement
			for schemeName, securityScheme := range serviceRequirements.apiSecurityDefinition[apiName] {
				switch securityScheme.GetDefinition().(type) {
				case *resourcespb.ApiSecurityDefinitionResource_Oidc:
					issuerUrl := securityScheme.GetOidc().GetIssuer()

					if issuerUrl == "" {
						projectErrors.Add(fmt.Errorf("service %s attempted to register an OIDC security scheme with an empty issuer", serviceRequirements.serviceName))
					}

					if len(securityScheme.GetOidc().GetAudiences()) == 0 {
						projectErrors.Add(fmt.Errorf("service %s attempted to register an OIDC security scheme with no audiences", serviceRequirements.serviceName))
					}

					oidSec := openapi3.NewOIDCSecurityScheme(issuerUrl)
					oidSec.Extensions = map[string]interface{}{
						"x-nitric-audiences": securityScheme.GetOidc().GetAudiences(),
					}
					oidSec.Name = schemeName

					api.Components.SecuritySchemes[schemeName] = &openapi3.SecuritySchemeRef{
						Value: oidSec,
					}
				default:
					return nil, fmt.Errorf("unknown security definition type: %T", securityScheme.GetDefinition())
				}
			}

			// apply top level security rules
			for schemeName, scopes := range apiResource.Security {
				existing, exists := lo.Find(api.Security, func(item openapi3.SecurityRequirement) bool {
					_, ok := item[schemeName]

					return ok
				})

				if !exists {
					if scopes.Scopes == nil {
						scopes.Scopes = []string{}
					}

					api.Security.With(openapi3.SecurityRequirement{
						schemeName: scopes.Scopes,
					})
				} else {
					if len(scopes.Scopes) != len(existing[schemeName]) {
						projectErrors.Add(fmt.Errorf("service %s attempted to register conflicting security scopes for API '%s' and security scheme '%s'", serviceRequirements.serviceName, apiName, schemeName))
					}

					for _, scope := range scopes.Scopes {
						if !lo.Contains(existing[schemeName], scope) {
							projectErrors.Add(fmt.Errorf("service %s attempted to register conflicting security scopes for API '%s' and security scheme '%s'", serviceRequirements.serviceName, apiName, schemeName))
						}
					}
				}
			}

			// apply route level security rules
			for _, route := range serviceRequirements.routes[apiName] {
				if !strings.HasPrefix(route.Path, "/") {
					projectErrors.Add(fmt.Errorf("service %s attempted to register path '%s' which is missing a leading slash", serviceRequirements.serviceName, route.Path))
				}

				normalizedPath, params := openAPIPathAndParams(route.Path)
				pathItem := api.Paths.Find(normalizedPath)

				if pathItem == nil {
					// Add the parameters at the path level
					pathItem = &openapi3.PathItem{
						Parameters: params,
					}
					// Add the path item to the document
					api.Paths[normalizedPath] = pathItem
				}

				for _, method := range route.Methods {
					if pathItem.Operations() != nil && pathItem.Operations()[method] != nil {
						operation := pathItem.Operations()[method]

						existingServiceName := operation.Extensions["x-nitric-target"].(map[string]string)["name"]

						existingService, _ := lo.Find(allServiceRequirements, func(item *ServiceRequirements) bool {
							return existingServiceName == item.serviceName
						})

						projectErrors.Add(fmt.Errorf("service %s attempted to register duplicate route %s: %s for API '%s' which is already defined in service %s", serviceRequirements.serviceFile, method, route.Path, apiName, existingService.serviceFile))

						continue
					}

					exts := map[string]interface{}{
						"x-nitric-target": map[string]string{
							"type": "function",
							"name": serviceRequirements.serviceName,
						},
					}

					var sr *openapi3.SecurityRequirements = nil

					if route.GetOptions() != nil {
						if route.GetOptions().SecurityDisabled {
							sr = &openapi3.SecurityRequirements{}
						} else if len(route.GetOptions().Security) > 0 {
							sr = &openapi3.SecurityRequirements{}

							if !route.GetOptions().SecurityDisabled {
								for key, scopes := range route.GetOptions().GetSecurity() {
									if scopes.Scopes == nil {
										scopes.Scopes = []string{}
									}

									sr.With(openapi3.SecurityRequirement{
										key: scopes.Scopes,
									})
								}
							}
						}
					}

					pathItem.SetOperation(method, &openapi3.Operation{
						OperationID: strings.ToLower(notAlphaNumeric.ReplaceAllString(normalizedPath+method, "")),
						Responses:   openapi3.NewResponses(),
						Extensions:  exts,
						Security:    sr,
					})
				}
			}
		}
	}

	for apiName, api := range apis {
		openApiJsonDoc, err := json.Marshal(api)
		if err != nil {
			return nil, err
		}

		resources = append(resources, &deploymentspb.Resource{
			Id: &resourcespb.ResourceIdentifier{
				Name: apiName,
				Type: resourcespb.ResourceType_Api,
			},
			Config: &deploymentspb.Resource_Api{
				Api: &deploymentspb.Api{
					Document: &deploymentspb.Api_Openapi{
						Openapi: string(openApiJsonDoc),
					},
				},
			},
		})
	}

	return resources, nil
}

// buildWebsocketRequirements gathers and deduplicates all websocket requirements
func buildWebsocketRequirements(allServiceRequirements []*ServiceRequirements, projectErrors *ProjectErrors) ([]*deploymentspb.Resource, error) {
	resources := []*deploymentspb.Resource{}

	for _, serviceRequirements := range allServiceRequirements {
		for socketName, registrations := range serviceRequirements.websockets {
			res, exists := lo.Find(resources, func(item *deploymentspb.Resource) bool {
				return item.Id.Name == socketName
			})

			if !exists {
				res = &deploymentspb.Resource{
					Id: &resourcespb.ResourceIdentifier{
						Name: socketName,
						Type: resourcespb.ResourceType_Websocket,
					},
					Config: &deploymentspb.Resource_Websocket{
						Websocket: &deploymentspb.Websocket{},
					},
				}
				resources = append(resources, res)
			}

			for _, registration := range registrations {
				switch registration.EventType {
				case *websocketspb.WebsocketEventType_Connect.Enum():
					res.GetWebsocket().ConnectTarget = &deploymentspb.WebsocketTarget{
						Target: &deploymentspb.WebsocketTarget_Service{
							Service: serviceRequirements.serviceName,
						},
					}
				case *websocketspb.WebsocketEventType_Disconnect.Enum():
					res.GetWebsocket().DisconnectTarget = &deploymentspb.WebsocketTarget{
						Target: &deploymentspb.WebsocketTarget_Service{
							Service: serviceRequirements.serviceName,
						},
					}
				case *websocketspb.WebsocketEventType_Message.Enum():
					res.GetWebsocket().MessageTarget = &deploymentspb.WebsocketTarget{
						Target: &deploymentspb.WebsocketTarget_Service{
							Service: serviceRequirements.serviceName,
						},
					}
				}
			}
		}
	}

	// loop over all websockets and make sure all methods are handled
	for _, websocketResource := range resources {
		ws := websocketResource.GetWebsocket()

		if ws.ConnectTarget == nil {
			projectErrors.Add(fmt.Errorf("missing connect handler for websocket %s", websocketResource.Id.Name))
		}

		if ws.DisconnectTarget == nil {
			projectErrors.Add(fmt.Errorf("missing disconnect handler for websocket %s", websocketResource.Id.Name))
		}

		if ws.MessageTarget == nil {
			projectErrors.Add(fmt.Errorf("missing message handler for websocket %s", websocketResource.Id.Name))
		}
	}

	return resources, nil
}

// buildScheduleRequirements gathers all schedule requirements, erroring on duplicate schedule names
func buildScheduleRequirements(allServiceRequirements []*ServiceRequirements, projectErrors *ProjectErrors) ([]*deploymentspb.Resource, error) {
	resources := []*deploymentspb.Resource{}

	for _, serviceRequirements := range allServiceRequirements {
		for scheduleName, scheduleConfig := range serviceRequirements.schedules {
			_, exists := lo.Find(resources, func(item *deploymentspb.Resource) bool {
				return item.Id.Name == scheduleName
			})

			if !exists {
				schedule := &deploymentspb.Schedule{}

				switch t := scheduleConfig.Cadence.(type) {
				case *schedulespb.RegistrationRequest_Cron:
					schedule.Cadence = &deploymentspb.Schedule_Cron{
						Cron: &deploymentspb.ScheduleCron{
							Expression: t.Cron.Expression,
						},
					}
				case *schedulespb.RegistrationRequest_Every:
					schedule.Cadence = &deploymentspb.Schedule_Every{
						Every: &deploymentspb.ScheduleEvery{
							Rate: t.Every.Rate,
						},
					}
				}

				schedule.Target = &deploymentspb.ScheduleTarget{
					Target: &deploymentspb.ScheduleTarget_Service{
						Service: serviceRequirements.serviceName,
					},
				}

				res := &deploymentspb.Resource{
					Id: &resourcespb.ResourceIdentifier{
						Name: scheduleName,
						Type: resourcespb.ResourceType_Schedule,
					},
					Config: &deploymentspb.Resource_Schedule{
						Schedule: schedule,
					},
				}
				resources = append(resources, res)
			} else {
				projectErrors.Add(fmt.Errorf("multiple schedules registered with name '%s', schedule names must be unique", scheduleName))
			}
		}
	}

	return resources, nil
}

// policyResourceName generates a unique name for a policy resource by hashing the policy document
func policyResourceName(policy *resourcespb.PolicyResource) (string, error) {
	policyDoc, err := json.Marshal(policy)
	if err != nil {
		return "", err
	}

	hasher := md5.New()
	hasher.Write(policyDoc)

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// buildKeyValueRequirements gathers and deduplicates all key/value requirements
func buildKeyValueRequirements(allServiceRequirements []*ServiceRequirements, allBatchRequirements []*BatchRequirements, projectErrors *ProjectErrors) ([]*deploymentspb.Resource, error) {
	resources := []*deploymentspb.Resource{}

	allKeyValueStores := []map[string]*resourcespb.KeyValueStoreResource{}

	for _, serviceRequirements := range allServiceRequirements {
		allKeyValueStores = append(allKeyValueStores, serviceRequirements.keyValueStores)
	}

	for _, batchRequirements := range allBatchRequirements {
		allKeyValueStores = append(allKeyValueStores, batchRequirements.keyValueStores)
	}

	for _, kvStores := range allKeyValueStores {
		for kvStoreName := range kvStores {
			_, exists := lo.Find(resources, func(item *deploymentspb.Resource) bool {
				return item.Id.Name == kvStoreName
			})

			if !exists {
				resources = append(resources, &deploymentspb.Resource{
					Id: &resourcespb.ResourceIdentifier{
						Name: kvStoreName,
						Type: resourcespb.ResourceType_KeyValueStore,
					},
					Config: &deploymentspb.Resource_KeyValueStore{},
				})
			}
		}
	}

	return resources, nil
}

// buildPolicyRequirements gathers, compacts, and deduplicates all policy requirements
// compaction is done by grouping policies by their principals and actions
// i.e. two or more policies with identical principals and actions, but different resources, will be combined into a single policy covering all resources.
func buildPolicyRequirements(allServiceRequirements []*ServiceRequirements, allBatchRequirements []*BatchRequirements, projectErrors *ProjectErrors) ([]*deploymentspb.Resource, error) {
	resources := []*deploymentspb.Resource{}

	allPolicies := [][]*resourcespb.PolicyResource{}

	for _, serviceRequirements := range allServiceRequirements {
		allPolicies = append(allPolicies, serviceRequirements.policies)
	}

	for _, batchRequirements := range allBatchRequirements {
		allPolicies = append(allPolicies, batchRequirements.policies)
	}

	for _, policies := range allPolicies {
		compactedPoliciesByKey := lo.GroupBy(policies, func(item *resourcespb.PolicyResource) string {
			// get the princpals and actions as a unique key (make sure they're sorted for consistency)
			principalNames := lo.Reduce(item.Principals, func(agg []string, principal *resourcespb.ResourceIdentifier, idx int) []string {
				return append(agg, principal.Name)
			}, []string{})
			slices.Sort(principalNames)

			principals := strings.Join(principalNames, ":")

			slices.Sort(item.Actions)
			actions := lo.Reduce(item.Actions, func(agg string, action resourcespb.Action, idx int) string {
				return agg + action.String()
			}, "")

			return principals + "-" + actions
		})

		compactedPolicies := []*resourcespb.PolicyResource{}
		// for each key of the compacted policies we want to make a single policy object that appends all of the policies resources together
		for _, pols := range compactedPoliciesByKey {
			newPol := pols[0]

			for _, pol := range pols[1:] {
				newPol.Resources = append(newPol.Resources, pol.Resources...)
			}

			compactedPolicies = append(compactedPolicies, newPol)
		}

		dedupedPolicies := map[string]*resourcespb.PolicyResource{}

		for _, v := range compactedPolicies {
			policyName, err := policyResourceName(v)
			if err != nil {
				return nil, err
			}

			dedupedPolicies[policyName] = v
		}

		for policyName, policy := range dedupedPolicies {
			principals := []*deploymentspb.Resource{}
			policyResources := []*deploymentspb.Resource{}

			for _, r := range policy.Resources {
				policyResources = append(policyResources, &deploymentspb.Resource{
					Id: &resourcespb.ResourceIdentifier{
						Name: r.Name,
						Type: r.Type,
					},
				})
			}

			for _, p := range policy.Principals {
				principals = append(principals, &deploymentspb.Resource{
					Id: &resourcespb.ResourceIdentifier{
						Name: p.Name,
						Type: p.Type,
					},
				})
			}

			res := &deploymentspb.Resource{
				Id: &resourcespb.ResourceIdentifier{
					Name: policyName,
					Type: resourcespb.ResourceType_Policy,
				},
				Config: &deploymentspb.Resource_Policy{
					Policy: &deploymentspb.Policy{
						Principals: principals,
						Actions:    policy.Actions,
						Resources:  policyResources,
					},
				},
			}

			resources = append(resources, res)
		}
	}

	return resources, nil
}

func checkServiceRequirementErrors(allServiceRequirements []*ServiceRequirements) error {
	allServiceErrors := []error{}

	for _, serviceRequirements := range allServiceRequirements {
		serviceRequirementsErrors := serviceRequirements.Error()
		if serviceRequirementsErrors != nil {
			allServiceErrors = append(allServiceErrors, serviceRequirementsErrors)
		}
	}

	if len(allServiceErrors) > 0 {
		return errors.Join(allServiceErrors...)
	}

	return nil
}

// convert service requirements to a cloud bill of materials
func ServiceRequirementsToSpec(projectName string, environmentVariables map[string]string, allServiceRequirements []*ServiceRequirements, allBatchRequirements []*BatchRequirements) (*deploymentspb.Spec, error) {
	if err := checkServiceRequirementErrors(allServiceRequirements); err != nil {
		return nil, err
	}

	projectErrors := &ProjectErrors{}

	newSpec := &deploymentspb.Spec{
		Resources: []*deploymentspb.Resource{},
	}

	databaseResources, err := buildDatabaseRequirements(allServiceRequirements, allBatchRequirements, projectErrors)
	if err != nil {
		return nil, err
	}

	newSpec.Resources = append(newSpec.Resources, databaseResources...)

	bucketResources, err := buildBucketRequirements(allServiceRequirements, allBatchRequirements, projectErrors)
	if err != nil {
		return nil, err
	}

	newSpec.Resources = append(newSpec.Resources, bucketResources...)

	topicResources, err := buildTopicRequirements(allServiceRequirements, allBatchRequirements, projectErrors)
	if err != nil {
		return nil, err
	}

	newSpec.Resources = append(newSpec.Resources, topicResources...)

	queueResources, err := buildQueueRequirements(allServiceRequirements, allBatchRequirements, projectErrors)
	if err != nil {
		return nil, err
	}

	newSpec.Resources = append(newSpec.Resources, queueResources...)

	secretResrources, err := buildSecretRequirements(allServiceRequirements, allBatchRequirements, projectErrors)
	if err != nil {
		return nil, err
	}

	newSpec.Resources = append(newSpec.Resources, secretResrources...)

	websocketResources, err := buildWebsocketRequirements(allServiceRequirements, projectErrors)
	if err != nil {
		return nil, err
	}

	newSpec.Resources = append(newSpec.Resources, websocketResources...)

	scheduleResources, err := buildScheduleRequirements(allServiceRequirements, projectErrors)
	if err != nil {
		return nil, err
	}

	newSpec.Resources = append(newSpec.Resources, scheduleResources...)

	httpResources, err := buildHttpRequirements(allServiceRequirements, projectErrors)
	if err != nil {
		return nil, err
	}

	newSpec.Resources = append(newSpec.Resources, httpResources...)

	apiResources, err := buildApiRequirements(allServiceRequirements, projectErrors)
	if err != nil {
		return nil, err
	}

	newSpec.Resources = append(newSpec.Resources, apiResources...)

	keyValueResources, err := buildKeyValueRequirements(allServiceRequirements, allBatchRequirements, projectErrors)
	if err != nil {
		return nil, err
	}

	newSpec.Resources = append(newSpec.Resources, keyValueResources...)

	policyResources, err := buildPolicyRequirements(allServiceRequirements, allBatchRequirements, projectErrors)
	if err != nil {
		return nil, err
	}

	newSpec.Resources = append(newSpec.Resources, policyResources...)

	for _, serviceRequirements := range allServiceRequirements {
		newSpec.Resources = append(newSpec.Resources, &deploymentspb.Resource{
			Id: &resourcespb.ResourceIdentifier{
				Name: serviceRequirements.serviceName,
				Type: resourcespb.ResourceType_Service,
			},
			Config: &deploymentspb.Resource_Service{
				Service: &deploymentspb.Service{
					Source: &deploymentspb.Service_Image{
						Image: &deploymentspb.ImageSource{
							Uri: fmt.Sprintf(serviceRequirements.serviceName),
						},
					},
					Workers: int32(serviceRequirements.WorkerCount()),
					Type:    serviceRequirements.serviceType,
					Env:     environmentVariables,
				},
			},
		})
	}

	for _, batchRequirements := range allBatchRequirements {
		newSpec.Resources = append(newSpec.Resources, &deploymentspb.Resource{
			Id: &resourcespb.ResourceIdentifier{
				Name: batchRequirements.batchName,
				Type: resourcespb.ResourceType_Batch,
			},
			Config: &deploymentspb.Resource_Batch{
				Batch: &deploymentspb.Batch{
					Source: &deploymentspb.Batch_Image{
						Image: &deploymentspb.ImageSource{
							Uri: fmt.Sprintf(batchRequirements.batchName),
						},
					},
					// TODO: Remove this
					Type: "default",
					Env:  environmentVariables,
				},
			},
		})
	}

	return newSpec, projectErrors.Error()
}

func ApiToOpenApiSpec(apiRegistrationRequests map[string][]*apispb.RegistrationRequest, apiSecurityDefinitions map[string]map[string]*resourcespb.ApiSecurityDefinitionResource, projectErrors *ProjectErrors) (*openapi3.T, error) {
	allServiceRequirements := []*ServiceRequirements{}

	for serviceName, registrationRequests := range apiRegistrationRequests {
		apiRouteMap := map[string][]*apispb.RegistrationRequest{}
		allApis := map[string]*resourcespb.ApiResource{}

		for _, registrationRequest := range registrationRequests {
			if registrationRequest != nil {
				allApis[registrationRequest.Api] = &resourcespb.ApiResource{}
			}
		}

		for _, registrationRequest := range registrationRequests {
			apiRouteMap[registrationRequest.Api] = append(apiRouteMap[registrationRequest.Api], registrationRequest)
		}

		allServiceRequirements = append(allServiceRequirements, &ServiceRequirements{
			serviceName:           serviceName,
			apis:                  allApis,
			routes:                apiRouteMap,
			apiSecurityDefinition: apiSecurityDefinitions,
		})
	}

	apiRequirements, err := buildApiRequirements(allServiceRequirements, projectErrors)
	if err != nil {
		return nil, err
	}

	if len(apiRequirements) != 1 {
		return nil, fmt.Errorf("there should only be one api requirement")
	}

	r := apiRequirements[0]

	openapiString := r.GetApi().GetOpenapi()

	// Unmarshal the OpenAPI JSON string into openapi3.T
	var openapiDoc *openapi3.T

	err = json.Unmarshal([]byte(openapiString), &openapiDoc)
	if err != nil {
		return nil, err
	}

	return openapiDoc, nil
}
