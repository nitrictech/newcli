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

package local

import (
	"fmt"
	"sort"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nitrictech/cli/pkg/cloud"
	"github.com/nitrictech/cli/pkg/cloud/apis"
	"github.com/nitrictech/cli/pkg/cloud/http"
	"github.com/nitrictech/cli/pkg/cloud/resources"
	"github.com/nitrictech/cli/pkg/cloud/schedules"
	"github.com/nitrictech/cli/pkg/cloud/sql"
	"github.com/nitrictech/cli/pkg/cloud/topics"
	"github.com/nitrictech/cli/pkg/cloud/websockets"
	"github.com/nitrictech/cli/pkg/view/tui"
	viewr "github.com/nitrictech/cli/pkg/view/tui/components/view"
	"github.com/nitrictech/cli/pkg/view/tui/reactive"
	"github.com/nitrictech/cli/pkg/view/tui/teax"
	schedulespb "github.com/nitrictech/nitric/core/pkg/proto/schedules/v1"
)

type ApiSummary struct {
	Name               string
	Url                string
	RequestingServices []string
}

type WebsocketSummary struct {
	name string
	url  string
}

type HttpProxySummary struct {
	name string
	url  string
}

type TopicSummary struct {
	name            string
	url             string
	subscriberCount int
}

type ScheduleSummary struct {
	name string
	rate string
	url  string
}

type DatabaseSummary struct {
	name   string
	status string
}

type TuiModel struct {
	localCloud  *cloud.LocalCloud
	apis        []ApiSummary
	websockets  []WebsocketSummary
	httpProxies []HttpProxySummary
	topics      []TopicSummary
	schedules   []ScheduleSummary
	databases   []DatabaseSummary

	resources *resources.LocalResourcesState

	reactiveSub *reactive.Subscription

	dashboardUrl string
}

var _ tea.Model = &TuiModel{}

func (t *TuiModel) Init() tea.Cmd {
	t.reactiveSub = reactive.NewSubscriber()
	reactive.ListenFor(t.reactiveSub, t.localCloud.Apis.SubscribeToState)
	reactive.ListenFor(t.reactiveSub, t.localCloud.Databases.SubscribeToState)
	reactive.ListenFor(t.reactiveSub, t.localCloud.Websockets.SubscribeToState)
	reactive.ListenFor(t.reactiveSub, t.localCloud.Http.SubscribeToState)

	reactive.ListenFor(t.reactiveSub, t.localCloud.Resources.SubscribeToState)

	reactive.ListenFor(t.reactiveSub, t.localCloud.Schedules.SubscribeToState)
	reactive.ListenFor(t.reactiveSub, t.localCloud.Topics.SubscribeToState)

	return t.reactiveSub.AwaitNextMsg()
}

func (t *TuiModel) ReactiveUpdate(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch state := msg.(type) {
	case resources.LocalResourcesState:
		t.resources = &state
	case apis.State:
		// update the api state by getting the latest API addresses
		newApiSummary := []ApiSummary{}

		for apiName, serviceReg := range state {
			reqSrvs := []string{}
			for srv := range serviceReg {
				reqSrvs = append(reqSrvs, srv)
			}

			newApiSummary = append(newApiSummary, ApiSummary{
				Name:               apiName,
				Url:                t.localCloud.Gateway.GetApiAddresses()[apiName],
				RequestingServices: reqSrvs,
			})
		}

		// sort the apis by name
		sort.Slice(newApiSummary, func(i, j int) bool {
			return newApiSummary[i].Name < newApiSummary[j].Name
		})

		t.apis = newApiSummary
	case sql.State:
		newDatabaseSummary := []DatabaseSummary{}

		for database, db := range state {
			newDatabaseSummary = append(newDatabaseSummary, DatabaseSummary{
				name:   database,
				status: db.Status,
			})
		}

		// sort the databases by name
		sort.Slice(newDatabaseSummary, func(i, j int) bool {
			return newDatabaseSummary[i].name < newDatabaseSummary[j].name
		})

		t.databases = newDatabaseSummary
	case websockets.State:
		// update the api state by getting the latest API addresses
		newWebsocketsSummary := []WebsocketSummary{}

		for api, host := range t.localCloud.Gateway.GetWebsocketAddresses() {
			newWebsocketsSummary = append(newWebsocketsSummary, WebsocketSummary{
				name: api,
				url:  fmt.Sprintf("ws://%s", host),
			})
		}

		// sort by name
		sort.Slice(newWebsocketsSummary, func(i, j int) bool {
			return newWebsocketsSummary[i].name < newWebsocketsSummary[j].name
		})

		t.websockets = newWebsocketsSummary
	case http.State:
		// update the api state by getting the latest API addresses
		newHttpProxiesSummary := []HttpProxySummary{}

		for api, host := range t.localCloud.Gateway.GetHttpWorkerAddresses() {
			newHttpProxiesSummary = append(newHttpProxiesSummary, HttpProxySummary{
				name: api,
				url:  host,
			})
		}

		t.httpProxies = newHttpProxiesSummary
	case topics.State:
		// update the api state by getting the latest API addresses
		newTopicsSummary := []TopicSummary{}

		for topic, subscribedService := range state {
			// Each service can subscribe more than once.
			subCount := 0
			for _, numSubscribers := range subscribedService {
				subCount += numSubscribers
			}

			newTopicsSummary = append(newTopicsSummary, TopicSummary{
				name:            topic,
				url:             t.localCloud.Gateway.GetTopicTriggerUrl(topic),
				subscriberCount: subCount,
			})
		}

		t.topics = newTopicsSummary
	case schedules.State:
		// update the api state by getting the latest API addresses
		newSchedulesSummary := []ScheduleSummary{}

		for schedule, scheduledService := range state {
			var rate string

			switch t := scheduledService.Schedule.Cadence.(type) {
			case *schedulespb.RegistrationRequest_Cron:
				rate = t.Cron.Expression
			case *schedulespb.RegistrationRequest_Every:
				rate = t.Every.Rate
			default:
				rate = "unknown"
			}

			newSchedulesSummary = append(newSchedulesSummary, ScheduleSummary{
				name: schedule,
				url:  t.localCloud.Gateway.GetScheduleManualTriggerUrl(schedule),
				rate: rate,
			})
		}

		t.schedules = newSchedulesSummary
	}

	return t, t.reactiveSub.AwaitNextMsg()
}

func (t *TuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmds := []tea.Cmd{}

	switch typ := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(typ, tui.KeyMap.Quit):
			return t, teax.Quit
		}

	// Using a wrapper here
	case reactive.Message:
		return t.ReactiveUpdate(typ.Msg)
	default:
		break
	}

	return t, tea.Batch(cmds...)
}

func (t *TuiModel) View() string {
	v := viewr.New()

	apisRegistered := len(t.apis) > 0
	websocketsRegistered := len(t.websockets) > 0
	httpProxiesRegistered := len(t.httpProxies) > 0
	topicsRegistered := len(t.topics) > 0
	schedulesRegistered := len(t.schedules) > 0

	noWorkersRegistered := !apisRegistered && !websocketsRegistered && !httpProxiesRegistered && !topicsRegistered && !schedulesRegistered

	if t.dashboardUrl != "" && !noWorkersRegistered {
		v.Break()
		v.Add("dashboard: ").WithStyle(lipgloss.NewStyle().Foreground(tui.Colors.Purple))
		v.Addln(t.dashboardUrl).WithStyle(lipgloss.NewStyle().Bold(true).Foreground(tui.Colors.Purple))
		v.Break()
	} else {
		v.Break()
	}

	for _, api := range t.apis {
		v.Add("api:%s - ", api.Name)
		v.Addln(api.Url).WithStyle(lipgloss.NewStyle().Bold(true).Foreground(tui.Colors.Purple))
	}

	for _, httpProxy := range t.httpProxies {
		v.Add("http:%s - ", httpProxy.name)
		v.Addln(httpProxy.url).WithStyle(lipgloss.NewStyle().Bold(true).Foreground(tui.Colors.Purple))
	}

	for _, websocket := range t.websockets {
		v.Add("ws:%s - ", websocket.name)
		v.Addln(websocket.url).WithStyle(lipgloss.NewStyle().Bold(true).Foreground(tui.Colors.Purple))
	}

	for _, database := range t.databases {
		v.Add("db:%s - ", database.name)
		v.Addln(database.status).WithStyle(lipgloss.NewStyle().Bold(true).Foreground(tui.Colors.Purple))
	}

	if t.resources != nil {
		if len(t.resources.ServiceErrors) > 0 {
			v.Break()
			v.Addln("Project Errors:").WithStyle(lipgloss.NewStyle().Bold(true).Foreground(tui.Colors.Red))
		}

		for svcName, errs := range t.resources.ServiceErrors {
			v.Addln("%s:", svcName).WithStyle(lipgloss.NewStyle().Bold(true))
			for _, err := range errs {
				v.Addln(" - " + err.Error()).WithStyle(lipgloss.NewStyle().Bold(true).Foreground(tui.Colors.Red))
			}
		}
	}

	return v.Render()
}

func NewTuiModel(localCloud *cloud.LocalCloud, dashboardUrl string) *TuiModel {
	return &TuiModel{
		localCloud:   localCloud,
		dashboardUrl: dashboardUrl,
	}
}
