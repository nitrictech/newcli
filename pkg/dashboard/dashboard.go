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

package dashboard

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/olahol/melody"
	"github.com/samber/lo"
	"github.com/spf13/afero"

	"github.com/nitrictech/cli/pkg/browser"
	"github.com/nitrictech/cli/pkg/cloud"
	"github.com/nitrictech/cli/pkg/collector"
	"github.com/nitrictech/cli/pkg/netx"
	"github.com/nitrictech/cli/pkg/project/dockerhost"
	resourcespb "github.com/nitrictech/nitric/core/pkg/proto/resources/v1"
	websocketspb "github.com/nitrictech/nitric/core/pkg/proto/websockets/v1"

	"github.com/nitrictech/cli/pkg/cloud/apis"
	"github.com/nitrictech/cli/pkg/cloud/gateway"
	httpproxy "github.com/nitrictech/cli/pkg/cloud/http"
	"github.com/nitrictech/cli/pkg/cloud/resources"
	"github.com/nitrictech/cli/pkg/cloud/schedules"
	"github.com/nitrictech/cli/pkg/cloud/secrets"
	"github.com/nitrictech/cli/pkg/cloud/sql"
	"github.com/nitrictech/cli/pkg/cloud/storage"
	"github.com/nitrictech/cli/pkg/cloud/topics"
	"github.com/nitrictech/cli/pkg/cloud/websockets"
	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/update"
	"github.com/nitrictech/cli/pkg/version"
)

type BaseResourceSpec struct {
	Name string `json:"name"`
	// TODO: Remove this field
	RequestingServices []string `json:"requestingServices"`
}

type ApiSpec struct {
	*BaseResourceSpec

	OpenApiSpec *openapi3.T `json:"spec"`
}
type WebsocketSpec struct {
	*BaseResourceSpec

	Targets map[string]string `json:"targets,omitempty"`
}
type ScheduleSpec struct {
	*BaseResourceSpec

	Expression string `json:"expression,omitempty"`
	Rate       string `json:"rate,omitempty"`
	Target     string `json:"target,omitempty"`
}

type TopicSpec struct {
	*BaseResourceSpec
}

type QueueSpec struct {
	*BaseResourceSpec
}

type BucketSpec struct {
	*BaseResourceSpec
}

type KeyValueSpec struct {
	*BaseResourceSpec
}

type SQLDatabaseSpec struct {
	*BaseResourceSpec

	ConnectionString string `json:"connectionString"`
	Status           string `json:"status"`
	MigrationsPath   string `json:"migrationsPath"`
}

type SecretSpec struct {
	*BaseResourceSpec
}

type NotifierSpec struct {
	Bucket string `json:"bucket"`
	Target string `json:"target"`
}

type SubscriberSpec struct {
	Topic  string `json:"topic"`
	Target string `json:"target"`
}

type BatchJobSpec struct {
	*BaseResourceSpec

	Target string `json:"target"`
}

type PolicyResource struct {
	Name string `json:"name"`
	Type string `json:"type"`
}
type PolicySpec struct {
	*BaseResourceSpec

	Principals []PolicyResource `json:"principals"`
	Actions    []string         `json:"actions"`
	Resources  []PolicyResource `json:"resources"`
}

type ServiceSpec struct {
	*BaseResourceSpec

	FilePath string `json:"filePath"`
}

type BatchSpec struct {
	*BaseResourceSpec

	FilePath string `json:"filePath"`
}

type HttpProxySpec struct {
	*BaseResourceSpec

	Target string `json:"target"`
}

type Dashboard struct {
	resourcesLock          sync.Mutex
	project                *project.Project
	storageService         *storage.LocalStorageService
	gatewayService         *gateway.LocalGatewayService
	databaseService        *sql.LocalSqlServer
	secretService          *secrets.DevSecretService
	apis                   []ApiSpec
	apiUseHttps            bool
	apiSecurityDefinitions map[string]map[string]*resourcespb.ApiSecurityDefinitionResource
	schedules              []ScheduleSpec
	topics                 []*TopicSpec
	batchJobs              []*BatchJobSpec
	buckets                []*BucketSpec
	stores                 []*KeyValueSpec
	secrets                []*SecretSpec
	sqlDatabases           []*SQLDatabaseSpec
	websockets             []WebsocketSpec
	subscriptions          []*SubscriberSpec
	notifications          []*NotifierSpec
	httpProxies            []*HttpProxySpec
	queues                 []*QueueSpec
	policies               map[string]PolicySpec
	envMap                 map[string]string

	stackWebSocket   *melody.Melody
	historyWebSocket *melody.Melody
	wsWebSocket      *melody.Melody
	websocketsInfo   map[string]*websockets.WebsocketInfo
	port             int
	browserHasOpened bool
	noBrowser        bool
	browserLock      sync.Mutex
	debouncedUpdate  func()
}

type DashboardResponse struct {
	Apis          []ApiSpec          `json:"apis"`
	ApisUseHttps  bool               `json:"apisUseHttps"`
	Batches       []*BatchSpec       `json:"batchServices"`
	BatchJobs     []*BatchJobSpec    `json:"jobs"`
	Buckets       []*BucketSpec      `json:"buckets"`
	Schedules     []ScheduleSpec     `json:"schedules"`
	Topics        []*TopicSpec       `json:"topics"`
	Websockets    []WebsocketSpec    `json:"websockets"`
	Subscriptions []*SubscriberSpec  `json:"subscriptions"`
	Notifications []*NotifierSpec    `json:"notifications"`
	Stores        []*KeyValueSpec    `json:"stores"`
	SQLDatabases  []*SQLDatabaseSpec `json:"sqlDatabases"`
	Secrets       []*SecretSpec      `json:"secrets"`
	Queues        []*QueueSpec       `json:"queues"`
	HttpProxies   []*HttpProxySpec   `json:"httpProxies"`

	Services []*ServiceSpec `json:"services"`

	Policies            map[string]PolicySpec `json:"policies"`
	ProjectName         string                `json:"projectName"`
	ApiAddresses        map[string]string     `json:"apiAddresses"`
	WebsocketAddresses  map[string]string     `json:"websocketAddresses"`
	HttpWorkerAddresses map[string]string     `json:"httpWorkerAddresses"`
	TriggerAddress      string                `json:"triggerAddress"`
	StorageAddress      string                `json:"storageAddress"`
	CurrentVersion      string                `json:"currentVersion"`
	LatestVersion       string                `json:"latestVersion"`
	Connected           bool                  `json:"connected"`
}

type Bucket struct {
	Name         string     `json:"name,omitempty"`
	CreationDate *time.Time `json:"creationDate,omitempty"`
}

//go:embed dist/*
var content embed.FS

func (d *Dashboard) getServices() ([]*ServiceSpec, error) {
	serviceSpecs := []*ServiceSpec{}

	for _, service := range d.project.GetServices() {
		absPath, err := service.GetAbsoluteFilePath()
		if err != nil {
			return nil, err
		}

		serviceSpecs = append(serviceSpecs, &ServiceSpec{
			BaseResourceSpec: &BaseResourceSpec{
				Name: service.GetFilePath(),
			},
			FilePath: absPath,
		})
	}

	return serviceSpecs, nil
}

func (d *Dashboard) getBatchServices() ([]*BatchSpec, error) {
	batchSpecs := []*BatchSpec{}

	for _, batch := range d.project.GetBatchServices() {
		absPath, err := batch.GetAbsoluteFilePath()
		if err != nil {
			return nil, err
		}

		batchSpecs = append(batchSpecs, &BatchSpec{
			BaseResourceSpec: &BaseResourceSpec{
				Name: batch.GetFilePath(),
			},
			FilePath: absPath,
		})
	}

	return batchSpecs, nil
}

func (d *Dashboard) updateResources(lrs resources.LocalResourcesState) {
	d.resourcesLock.Lock()
	defer d.resourcesLock.Unlock()

	d.buckets = []*BucketSpec{}
	d.batchJobs = []*BatchJobSpec{}
	d.topics = []*TopicSpec{}
	d.stores = []*KeyValueSpec{}
	d.queues = []*QueueSpec{}
	d.apiSecurityDefinitions = map[string]map[string]*resourcespb.ApiSecurityDefinitionResource{}
	d.secrets = []*SecretSpec{}

	d.policies = map[string]PolicySpec{}

	for keyvalue, resource := range lrs.KeyValueStores.GetAll() {
		exists := lo.ContainsBy(d.stores, func(item *KeyValueSpec) bool {
			return item.Name == keyvalue
		})

		if !exists {
			d.stores = append(d.stores, &KeyValueSpec{
				BaseResourceSpec: &BaseResourceSpec{
					Name:               keyvalue,
					RequestingServices: resource.RequestingServices,
				},
			})
		}
	}

	for queue, resource := range lrs.Queues.GetAll() {
		exists := lo.ContainsBy(d.queues, func(item *QueueSpec) bool {
			return item.Name == queue
		})

		if !exists {
			d.queues = append(d.queues, &QueueSpec{
				BaseResourceSpec: &BaseResourceSpec{
					Name:               queue,
					RequestingServices: resource.RequestingServices,
				},
			})
		}
	}

	for bucketName, resource := range lrs.Buckets.GetAll() {
		exists := lo.ContainsBy(d.buckets, func(item *BucketSpec) bool {
			return item.Name == bucketName
		})

		if !exists {
			d.buckets = append(d.buckets, &BucketSpec{
				BaseResourceSpec: &BaseResourceSpec{
					Name:               bucketName,
					RequestingServices: resource.RequestingServices,
				},
			})
		}
	}

	if len(d.buckets) > 0 {
		slices.SortFunc(d.buckets, func(a, b *BucketSpec) int {
			return compare(a.Name, b.Name)
		})
	}

	for topicName, resource := range lrs.Topics.GetAll() {
		exists := lo.ContainsBy(d.topics, func(item *TopicSpec) bool {
			return item.Name == topicName
		})

		if !exists {
			d.topics = append(d.topics, &TopicSpec{
				BaseResourceSpec: &BaseResourceSpec{
					Name:               topicName,
					RequestingServices: resource.RequestingServices,
				},
			})
		}
	}

	if len(d.topics) > 0 {
		slices.SortFunc(d.topics, func(a, b *TopicSpec) int {
			return compare(a.Name, b.Name)
		})
	}

	for secretName, resource := range lrs.Secrets.GetAll() {
		exists := lo.ContainsBy(d.secrets, func(item *SecretSpec) bool {
			return item.Name == secretName
		})

		if !exists {
			d.secrets = append(d.secrets, &SecretSpec{
				BaseResourceSpec: &BaseResourceSpec{
					Name:               secretName,
					RequestingServices: resource.RequestingServices,
				},
			})
		}
	}

	if len(d.secrets) > 0 {
		slices.SortFunc(d.secrets, func(a, b *SecretSpec) int {
			return compare(a.Name, b.Name)
		})
	}

	for jobName, resource := range lrs.BatchJobs.GetAll() {
		exists := lo.ContainsBy(d.batchJobs, func(item *BatchJobSpec) bool {
			return item.Name == jobName
		})

		if !exists {
			d.batchJobs = append(d.batchJobs, &BatchJobSpec{
				BaseResourceSpec: &BaseResourceSpec{
					Name:               jobName,
					RequestingServices: resource.RequestingServices,
				},
			})
		}
	}

	if len(d.batchJobs) > 0 {
		slices.SortFunc(d.batchJobs, func(a, b *BatchJobSpec) int {
			return compare(a.Name, b.Name)
		})
	}

	for policyName, policy := range lrs.Policies.GetAll() {
		d.policies[policyName] = PolicySpec{
			BaseResourceSpec: &BaseResourceSpec{
				Name:               policyName,
				RequestingServices: policy.RequestingServices,
			},
			Actions: lo.Map(policy.Resource.Actions, func(item resourcespb.Action, index int) string {
				return item.String()
			}),
			Resources: lo.Map(policy.Resource.Resources, func(item *resourcespb.ResourceIdentifier, index int) PolicyResource {
				return PolicyResource{
					Name: item.Name,
					Type: strings.ToLower(item.Type.String()),
				}
			}),
			Principals: lo.Map(policy.Resource.Principals, func(item *resourcespb.ResourceIdentifier, index int) PolicyResource {
				return PolicyResource{
					Name: item.Name,
					Type: strings.ToLower(item.Type.String()),
				}
			}),
		}
	}

	for schemeName, apiDefinition := range lrs.ApiSecurityDefinitions.GetAll() {
		d.apiSecurityDefinitions[apiDefinition.Resource.ApiName] = map[string]*resourcespb.ApiSecurityDefinitionResource{}

		d.apiSecurityDefinitions[apiDefinition.Resource.ApiName][schemeName] = apiDefinition.Resource
	}

	d.refresh()
}

func (d *Dashboard) updateApis(state apis.State) {
	d.resourcesLock.Lock()
	defer d.resourcesLock.Unlock()

	apiSpecs := []ApiSpec{}

	for apiName, rr := range state {
		resources := lo.Keys(rr)
		apiSpec := ApiSpec{
			BaseResourceSpec: &BaseResourceSpec{
				Name:               apiName,
				RequestingServices: resources,
			},
		}

		spec, _ := collector.ApiToOpenApiSpec(rr, d.apiSecurityDefinitions, &collector.ProjectErrors{})

		if spec != nil {
			// set title to api name
			spec.Info.Title = apiName

			apiSpec.OpenApiSpec = spec
		}

		apiSpecs = append(apiSpecs, apiSpec)
	}

	slices.SortFunc(apiSpecs, func(a, b ApiSpec) int {
		return compare(a.Name, b.Name)
	})

	d.apis = apiSpecs

	d.refresh()
}

func (d *Dashboard) updateWebsockets(state websockets.State) {
	d.resourcesLock.Lock()
	defer d.resourcesLock.Unlock()

	wsSpec := []WebsocketSpec{}

	for name, ws := range state {
		spec := WebsocketSpec{
			BaseResourceSpec: &BaseResourceSpec{
				Name:               name,
				RequestingServices: lo.Uniq(lo.Keys(ws)),
			},
			Targets: map[string]string{},
		}

		for target, serviceWs := range ws {
			for _, eventType := range serviceWs {
				switch eventType {
				case websocketspb.WebsocketEventType_Connect:
					spec.Targets["connect"] = target
				case websocketspb.WebsocketEventType_Disconnect:
					spec.Targets["disconnect"] = target
				case websocketspb.WebsocketEventType_Message:
					spec.Targets["message"] = target
				}
			}
		}

		wsSpec = append(wsSpec, spec)
	}

	slices.SortFunc(wsSpec, func(a, b WebsocketSpec) int {
		return compare(a.Name, b.Name)
	})

	d.websockets = wsSpec

	d.refresh()
}

func (d *Dashboard) updateTopicSubscriptions(state topics.State) {
	d.resourcesLock.Lock()
	defer d.resourcesLock.Unlock()

	d.subscriptions = []*SubscriberSpec{}

	for topicName, functions := range state {
		for functionName := range functions {
			d.subscriptions = append(d.subscriptions, &SubscriberSpec{
				Topic:  topicName,
				Target: functionName,
			})
		}
	}

	d.refresh()
}

func (d *Dashboard) updateSchedules(state schedules.State) {
	d.resourcesLock.Lock()
	defer d.resourcesLock.Unlock()

	schedules := []ScheduleSpec{}

	for _, srvc := range state {
		requestingServices := []string{srvc.ServiceName}

		schedules = append(schedules, ScheduleSpec{
			BaseResourceSpec: &BaseResourceSpec{
				Name:               srvc.Schedule.GetScheduleName(),
				RequestingServices: requestingServices,
			},
			Expression: srvc.Schedule.GetCron().GetExpression(),
			Rate:       srvc.Schedule.GetEvery().GetRate(),
			Target:     srvc.ServiceName,
		})
	}

	slices.SortFunc(schedules, func(a, b ScheduleSpec) int {
		return compare(a.Name, b.Name)
	})

	d.schedules = schedules

	d.refresh()
}

func (d *Dashboard) updateBucketNotifications(state storage.State) {
	d.resourcesLock.Lock()
	defer d.resourcesLock.Unlock()

	d.notifications = []*NotifierSpec{}

	for bucketName, functions := range state {
		for functionName, count := range functions {
			if count > 0 {
				d.notifications = append(d.notifications, &NotifierSpec{
					Bucket: bucketName,
					Target: functionName,
				})
			}
		}
	}

	d.refresh()
}

func (d *Dashboard) updateHttpProxies(state httpproxy.State) {
	d.resourcesLock.Lock()
	defer d.resourcesLock.Unlock()

	d.httpProxies = []*HttpProxySpec{}

	for host, srvc := range state {
		d.httpProxies = append(d.httpProxies, &HttpProxySpec{
			BaseResourceSpec: &BaseResourceSpec{
				Name: host,
			},
			Target: srvc.ServiceName,
		})
	}

	slices.SortFunc(d.httpProxies, func(a, b *HttpProxySpec) int {
		return compare(a.Name, b.Name)
	})

	d.refresh()
}

func (d *Dashboard) updateSqlDatabases(state sql.State) {
	d.resourcesLock.Lock()
	defer d.resourcesLock.Unlock()

	sqlDatabases := []*SQLDatabaseSpec{}

	for dbName, db := range state {
		// connection strings should always use localhost for dashboard
		strToReplace := dockerhost.GetInternalDockerHost()
		connectionString := strings.Replace(db.ConnectionString, strToReplace, "localhost", 1)

		sqlDatabases = append(sqlDatabases, &SQLDatabaseSpec{
			BaseResourceSpec: &BaseResourceSpec{
				Name:               dbName,
				RequestingServices: db.ResourceRegister.RequestingServices,
			},
			ConnectionString: connectionString,
			Status:           db.Status,
			MigrationsPath:   db.ResourceRegister.Resource.Migrations.GetMigrationsPath(),
		})
	}

	if len(sqlDatabases) > 0 {
		slices.SortFunc(sqlDatabases, func(a, b *SQLDatabaseSpec) int {
			return compare(a.Name, b.Name)
		})
	}

	d.sqlDatabases = sqlDatabases

	d.refresh()
}

func (d *Dashboard) refresh() {
	if !d.noBrowser && !d.browserHasOpened {
		d.openBrowser()
	}

	d.debouncedUpdate()
}

func (d *Dashboard) isConnected() bool {
	apisRegistered := len(d.apis) > 0
	batchServicesRegistered := len(d.batchJobs) > 0
	websocketsRegistered := len(d.websockets) > 0
	topicsRegistered := len(d.topics) > 0
	schedulesRegistered := len(d.schedules) > 0
	notificationsRegistered := len(d.notifications) > 0
	proxiesRegistered := len(d.httpProxies) > 0
	storesRegistered := len(d.stores) > 0
	sqlRegistered := len(d.sqlDatabases) > 0
	secretsRegistered := len(d.secrets) > 0

	return apisRegistered || batchServicesRegistered || websocketsRegistered || topicsRegistered || schedulesRegistered || notificationsRegistered || proxiesRegistered || storesRegistered || sqlRegistered || secretsRegistered
}

func (d *Dashboard) Start() error {
	// Get the embedded files from the 'dist' directory
	staticFiles, err := fs.Sub(content, "dist")
	if err != nil {
		return err
	}

	fs := http.FileServer(http.FS(staticFiles))

	// TODO: Inject this into start for testability
	aferoFs := afero.NewOsFs()

	// Serve the files using the http package
	http.Handle("/", fs)

	// handle websocket
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		err := d.stackWebSocket.HandleRequest(w, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	// Register a handler for when a client connects
	d.stackWebSocket.HandleConnect(func(s *melody.Session) {
		// Send a welcome message to the client
		err := d.sendStackUpdate()
		if err != nil {
			log.Fatal(err)
		}
	})

	d.stackWebSocket.HandleMessage(func(s *melody.Session, msg []byte) {
		err := d.stackWebSocket.Broadcast(msg)
		if err != nil {
			log.Print(err)
		}
	})

	// handle history websocket
	http.HandleFunc("/history", func(w http.ResponseWriter, r *http.Request) {
		err := d.historyWebSocket.HandleRequest(w, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	d.historyWebSocket.HandleConnect(func(s *melody.Session) {
		// Send a welcome message to the client
		err := d.sendHistoryUpdate()
		if err != nil {
			log.Fatal(err)
		}
	})

	d.historyWebSocket.HandleMessage(func(s *melody.Session, msg []byte) {
		err := d.historyWebSocket.Broadcast(msg)
		if err != nil {
			log.Print(err)
		}
	})

	http.HandleFunc("/api/history", d.createHistoryHttpHandler())

	// Define an API route under /call to proxy communication between app and apis
	http.HandleFunc("/api/call/", d.createCallProxyHttpHandler())

	http.HandleFunc("/api/storage", d.handleStorage())

	http.HandleFunc("/api/sql", d.createSqlQueryHandler())

	http.HandleFunc("/api/secrets", d.createSecretsHandler())

	http.HandleFunc("/api/sql/migrate", d.createApplySqlMigrationsHandler(aferoFs, false))

	// handle websockets
	http.HandleFunc("/ws-info", func(w http.ResponseWriter, r *http.Request) {
		err := d.wsWebSocket.HandleRequest(w, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	http.HandleFunc("/api/ws-clear-messages", d.handleWebsocketMessagesClear())

	http.HandleFunc("/api/logs", d.createServiceLogsHandler(d.project))

	d.wsWebSocket.HandleConnect(func(s *melody.Session) {
		// Send a welcome message to the client
		err := d.sendWebsocketsUpdate()
		if err != nil {
			log.Fatal(err)
		}
	})

	d.wsWebSocket.HandleMessage(func(s *melody.Session, msg []byte) {
		err := d.wsWebSocket.Broadcast(msg)
		if err != nil {
			log.Print(err)
		}
	})

	// using ephemeral ports, we will redirect to the dashboard on main api 4000
	dashListener, err := netx.GetNextListener(netx.MinPort(49152), netx.MaxPort(65535))
	if err != nil {
		return err
	}

	serveFn := func() {
		err = http.Serve(dashListener, nil)
		if err != nil {
			log.Fatal(err)
		}
	}
	go serveFn()

	d.port = dashListener.Addr().(*net.TCPAddr).Port

	return nil
}

func (d *Dashboard) openBrowser() {
	d.browserLock.Lock()
	defer d.browserLock.Unlock()

	if d.browserHasOpened {
		return // Browser already opened
	}

	err := browser.Open(d.GetDashboardUrl())
	if err != nil {
		fmt.Printf("Error opening dashboard in browser: %v\n", err)
		return
	}

	d.browserHasOpened = true
}

func (d *Dashboard) GetDashboardUrl() string {
	return fmt.Sprintf("http://localhost:%s", strconv.Itoa(d.port))
}

func handleResponseWriter(w http.ResponseWriter, data []byte) {
	_, err := w.Write(data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (d *Dashboard) sendStackUpdate() error {
	currentVersion := strings.TrimPrefix(version.Version, "v")
	latestVersion := update.FetchLatestCLIVersion()

	services, err := d.getServices()
	if err != nil {
		return err
	}

	batchServices, err := d.getBatchServices()
	if err != nil {
		return err
	}

	response := &DashboardResponse{
		Apis:                d.apis,
		Topics:              d.topics,
		Batches:             batchServices,
		BatchJobs:           d.batchJobs,
		Buckets:             d.buckets,
		Stores:              d.stores,
		SQLDatabases:        d.sqlDatabases,
		Schedules:           d.schedules,
		Websockets:          d.websockets,
		Policies:            d.policies,
		Queues:              d.queues,
		Secrets:             d.secrets,
		Services:            services,
		Subscriptions:       d.subscriptions,
		Notifications:       d.notifications,
		HttpProxies:         d.httpProxies,
		ProjectName:         d.project.Name,
		ApiAddresses:        d.gatewayService.GetApiAddresses(),
		WebsocketAddresses:  d.gatewayService.GetWebsocketAddresses(),
		HttpWorkerAddresses: d.gatewayService.GetHttpWorkerAddresses(),
		TriggerAddress:      d.gatewayService.GetTriggerAddress(),
		// StorageAddress:      d.storageService.GetStorageEndpoint(),
		CurrentVersion: currentVersion,
		LatestVersion:  latestVersion,
		Connected:      d.isConnected(),
	}

	// Encode the response as JSON
	jsonData, err := json.Marshal(response)
	if err != nil {
		return err
	}

	err = d.stackWebSocket.Broadcast(jsonData)

	return err
}

func (d *Dashboard) sendHistoryUpdate() error {
	response, err := d.ReadAllHistoryRecords()
	if err != nil {
		return err
	}

	jsonData, err := json.Marshal(response)
	if err != nil {
		return err
	}

	return d.historyWebSocket.Broadcast(jsonData)
}

func (d *Dashboard) sendWebsocketsUpdate() error {
	jsonData, err := json.Marshal(d.websocketsInfo)
	if err != nil {
		return err
	}

	err = d.wsWebSocket.Broadcast(jsonData)

	return err
}

func compare(a string, b string) int {
	if a > b {
		return 1
	}

	return -1
}

func New(noBrowser bool, localCloud *cloud.LocalCloud, project *project.Project) (*Dashboard, error) {
	stackWebSocket := melody.New()
	historyWebSocket := melody.New()
	wsWebSocket := melody.New()

	dash := &Dashboard{
		project:                project,
		storageService:         localCloud.Storage,
		gatewayService:         localCloud.Gateway,
		databaseService:        localCloud.Databases,
		secretService:          localCloud.Secrets,
		apis:                   []ApiSpec{},
		apiUseHttps:            localCloud.Gateway.ApiTlsCredentials != nil,
		apiSecurityDefinitions: map[string]map[string]*resourcespb.ApiSecurityDefinitionResource{},
		envMap:                 map[string]string{},
		stackWebSocket:         stackWebSocket,
		historyWebSocket:       historyWebSocket,
		wsWebSocket:            wsWebSocket,
		batchJobs:              []*BatchJobSpec{},
		buckets:                []*BucketSpec{},
		schedules:              []ScheduleSpec{},
		topics:                 []*TopicSpec{},
		subscriptions:          []*SubscriberSpec{},
		notifications:          []*NotifierSpec{},
		websockets:             []WebsocketSpec{},
		stores:                 []*KeyValueSpec{},
		sqlDatabases:           []*SQLDatabaseSpec{},
		secrets:                []*SecretSpec{},
		queues:                 []*QueueSpec{},
		httpProxies:            []*HttpProxySpec{},
		policies:               map[string]PolicySpec{},
		websocketsInfo:         map[string]*websockets.WebsocketInfo{},
		noBrowser:              noBrowser,
	}

	debouncedUpdate, _ := lo.NewDebounce(300*time.Millisecond, func() {
		err := dash.sendStackUpdate()
		if err != nil {
			fmt.Printf("Error sending stack update: %v\n", err)
			return
		}
	})

	dash.debouncedUpdate = debouncedUpdate

	// subscribe to resource, used for architecture and buckets
	localCloud.Resources.SubscribeToState(dash.updateResources)

	localCloud.Apis.SubscribeToState(dash.updateApis)
	localCloud.Websockets.SubscribeToState(dash.updateWebsockets)
	localCloud.Schedules.SubscribeToState(dash.updateSchedules)
	localCloud.Topics.SubscribeToState(dash.updateTopicSubscriptions)
	localCloud.Storage.SubscribeToState(dash.updateBucketNotifications)
	localCloud.Http.SubscribeToState(dash.updateHttpProxies)
	localCloud.Databases.SubscribeToState(dash.updateSqlDatabases)

	// subscribe to history events from gateway
	localCloud.Apis.SubscribeToAction(dash.handleApiHistory)
	localCloud.Topics.SubscribeToAction(dash.handleTopicsHistory)
	localCloud.Schedules.SubscribeToAction(dash.handleSchedulesHistory)
	localCloud.Batch.SubscribeToAction(dash.handleBatchJobsHistory)
	localCloud.Websockets.SubscribeToAction(dash.handleWebsocketEvents)

	return dash, nil
}
