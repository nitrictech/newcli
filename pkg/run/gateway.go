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

package run

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/fasthttp/router"
	"github.com/samber/lo"
	"github.com/valyala/fasthttp"

	"github.com/nitrictech/cli/pkg/dashboard"
	"github.com/nitrictech/cli/pkg/history"
	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/utils"
	base_http "github.com/nitrictech/nitric/cloud/common/runtime/gateway"
	v1 "github.com/nitrictech/nitric/core/pkg/api/nitric/v1"
	"github.com/nitrictech/nitric/core/pkg/plugins/gateway"
	"github.com/nitrictech/nitric/core/pkg/worker"
	"github.com/nitrictech/nitric/core/pkg/worker/pool"
)

type HttpMiddleware func(*fasthttp.RequestCtx, pool.WorkerPool) bool

type apiServer struct {
	lis net.Listener
	srv *fasthttp.Server
}

type BaseHttpGateway struct {
	apiServers      []*apiServer
	apis            []string
	httpWorkers     []int
	serviceServer   *fasthttp.Server
	serviceListener net.Listener
	gateway.UnimplementedGatewayPlugin
	stop     chan bool
	pool     pool.WorkerPool
	dashPort int
	project  *project.Project
	dash     *dashboard.Dashboard
}

var _ gateway.GatewayService = &BaseHttpGateway{}

func apiWorkerFilter(apiName string) func(w worker.Worker) bool {
	return func(w worker.Worker) bool {
		if _, ok := w.(*worker.HttpWorker); ok {
			return true
		}

		if api, ok := w.(*worker.RouteWorker); ok {
			return api.Api() == apiName
		}

		return false
	}
}

// GetTriggerAddress - Returns the address built-in nitric services
// this can be used to publishing messages to topics or triggering schedules
func (s *BaseHttpGateway) GetTriggerAddress() string {
	if s.serviceListener != nil {
		return strings.Replace(s.serviceListener.Addr().String(), "[::]", "localhost", 1)
	}

	return ""
}

func (s *BaseHttpGateway) GetApiAddresses() map[string]string {
	addresses := make(map[string]string)

	for idx, api := range s.apis {
		addresses[api] = strings.Replace(s.apiServers[idx].lis.Addr().String(), "[::]", "localhost", 1)
	}

	return addresses
}

func (s *BaseHttpGateway) GetHttpWorkerAddresses() map[int]string {
	addresses := make(map[int]string)

	for idx, port := range s.httpWorkers {
		addresses[port] = strings.Replace(s.apiServers[idx].lis.Addr().String(), "[::]", "localhost", 1)
	}

	return addresses
}

func (s *BaseHttpGateway) handleHttpRequest(apiIdx int) func(ctx *fasthttp.RequestCtx) {
	return func(ctx *fasthttp.RequestCtx) {
		if len(s.httpWorkers) == 0 && apiIdx >= len(s.apis) {
			ctx.Error("Sorry, nitric is listening on this port but is waiting for an API to be available to handle, you may have removed an API during development this port will be assigned to an API when one becomes available", 404)
			return
		}

		apiName := ""

		// API names are only relevant for api workers
		if len(s.apis) > 0 {
			apiName = s.apis[apiIdx]
		}

		headerMap := base_http.HttpHeadersToMap(&ctx.Request.Header)

		headers := map[string]*v1.HeaderValue{}
		for k, v := range headerMap {
			headers[k] = &v1.HeaderValue{Value: v}
		}

		query := map[string]*v1.QueryValue{}

		ctx.QueryArgs().VisitAll(func(key []byte, val []byte) {
			k := string(key)

			if query[k] == nil {
				query[k] = &v1.QueryValue{}
			}

			query[k].Value = append(query[k].Value, string(val))
		})

		httpTrigger := &v1.TriggerRequest{
			Data: ctx.Request.Body(),
			Context: &v1.TriggerRequest_Http{
				Http: &v1.HttpTriggerContext{
					Method:      string(ctx.Request.Header.Method()),
					Path:        string(ctx.URI().PathOriginal()),
					Headers:     headers,
					QueryParams: query,
				},
			},
		}

		worker, err := s.pool.GetWorker(&pool.GetWorkerOptions{
			Trigger: httpTrigger,
			Filter:  apiWorkerFilter(apiName),
		})
		if err != nil {
			ctx.Redirect(fmt.Sprintf("http://localhost:%v/not-found", s.dashPort), fasthttp.StatusTemporaryRedirect)
			return
		}

		resp, err := worker.HandleTrigger(context.TODO(), httpTrigger)
		if err != nil {
			ctx.Error(fmt.Sprintf("Error handling HTTP Request: %v", err), 500)
			return
		}

		if http := resp.GetHttp(); http != nil {
			// Copy headers across
			for k, v := range http.Headers {
				for _, val := range v.Value {
					ctx.Response.Header.Add(k, val)
				}
			}

			// Avoid content length header duplication
			ctx.Response.Header.Del("Content-Length")
			ctx.Response.SetStatusCode(int(http.Status))
			ctx.Response.SetBody(resp.Data)

			var queryParams []history.Param

			for k, v := range query {
				for _, val := range v.Value {
					queryParams = append(queryParams, history.Param{
						Key:   k,
						Value: val,
					})
				}
			}

			// Write history if it was an API request
			if len(s.apis) > 0 {
				err = s.project.History.WriteHistoryRecord(history.API, &history.HistoryRecord{
					Success: http.Status < 400,
					Time:    time.Now().UnixMilli(),
					ApiHistoryItem: history.ApiHistoryItem{
						Api: s.GetApiAddresses()[apiName],
						Request: &history.RequestHistory{
							Method:      string(ctx.Request.Header.Method()),
							Path:        string(ctx.URI().PathOriginal()),
							QueryParams: queryParams,
							Headers: lo.MapEntries(headers, func(k string, v *v1.HeaderValue) (string, []string) {
								return k, v.Value
							}),
							Body:       ctx.Request.Body(),
							PathParams: []history.Param{},
						},
						Response: &history.ResponseHistory{
							Headers: lo.MapEntries(http.Headers, func(k string, v *v1.HeaderValue) (string, []string) {
								return k, v.Value
							}),
							Time:   time.Since(ctx.ConnTime()).Milliseconds(),
							Status: http.Status,
							Data:   resp.Data,
							Size:   len(resp.Data),
						},
					},
				})
				if err != nil {
					fmt.Println(err.Error())
				}

				err = s.dash.RefreshHistory()
				if err != nil {
					fmt.Println(err.Error())
				}
			}

			return
		}

		ctx.Error("Response was not a Http response", 500)
	}
}

func (s *BaseHttpGateway) handleTopicRequest(ctx *fasthttp.RequestCtx) {
	topicName := ctx.UserValue("name").(string)

	trigger := &v1.TriggerRequest{
		Data: ctx.Request.Body(),
		Context: &v1.TriggerRequest_Topic{
			Topic: &v1.TopicTriggerContext{
				Topic: topicName,
			},
		},
	}

	ws := s.pool.GetWorkers(&pool.GetWorkerOptions{
		Trigger: trigger,
	})

	if len(ws) == 0 {
		ctx.Error("no subscribers found for topic", 404)
	}

	errList := make([]error, 0)

	for _, w := range ws {
		resp, err := w.HandleTrigger(context.TODO(), trigger)
		if err != nil {
			errList = append(errList, err)
		}

		if !resp.GetTopic().Success {
			errList = append(errList, fmt.Errorf("topic delivery was unsuccessful"))
		}

		var topicType history.RecordType

		switch w.(type) {
		case *worker.ScheduleWorker:
			topicType = history.SCHEDULE
		case *worker.SubscriptionWorker:
			topicType = history.TOPIC
		}

		err = s.project.History.WriteHistoryRecord(topicType, &history.HistoryRecord{
			Success: resp.GetTopic().Success,
			Time:    time.Now().UnixMilli(),
			EventHistoryItem: history.EventHistoryItem{
				Event: &history.EventRecord{
					TopicKey:  strings.ToLower(strings.ReplaceAll(topicName, " ", "-")),
					WorkerKey: topicName,
				},
				Payload: string(ctx.Request.Body()),
			},
		})
		if err != nil {
			fmt.Println(err.Error())
		}
	}

	statusCode := 200
	if len(errList) > 0 {
		statusCode = 500
	}

	ctx.Error(fmt.Sprintf("%d successful & %d failed deliveries", len(ws)-len(errList), len(errList)), statusCode)
}

func (s *BaseHttpGateway) refreshApis() {
	s.apis = make([]string, 0)

	workers := s.pool.GetWorkers(&pool.GetWorkerOptions{})
	uniqApis := lo.Reduce(workers, func(agg []string, w worker.Worker, idx int) []string {
		if api, ok := w.(*worker.RouteWorker); ok {
			if !lo.Contains(agg, api.Api()) {
				agg = append(agg, api.Api())
			}
		}

		return agg
	}, []string{})

	// sort the APIs by alphabetical order
	sort.Strings(uniqApis)

	s.apis = append(s.apis, uniqApis...)
}

func (s *BaseHttpGateway) refreshHttpWorkers() {
	s.httpWorkers = make([]int, 0)

	workers := s.pool.GetWorkers(&pool.GetWorkerOptions{})
	uniqHttpWorkers := lo.Reduce(workers, func(agg []int, w worker.Worker, idx int) []int {
		if http, ok := w.(*worker.HttpWorker); ok {
			if !lo.Contains(agg, http.GetPort()) {
				agg = append(agg, http.GetPort())
			}
		}

		return agg
	}, []int{})

	// sort the Http Worker Ports lowest to highest
	sort.Ints(uniqHttpWorkers)

	s.httpWorkers = append(s.httpWorkers, uniqHttpWorkers...)
}

// Update the gateway and API based on the worker pool
func (s *BaseHttpGateway) Refresh() error {
	s.refreshApis()

	s.refreshHttpWorkers()

	// instansiate servers if not done
	if s.apiServers == nil {
		s.apiServers = make([]*apiServer, 0)
	}

	// create an api server for every API and HTTP worker
	for len(s.apiServers) < len(s.apis)+len(s.httpWorkers) {
		fhttp := &fasthttp.Server{
			ReadTimeout:     time.Second * 1,
			IdleTimeout:     time.Second * 1,
			CloseOnShutdown: true,
			Handler:         s.handleHttpRequest(len(s.apiServers)),
		}
		// Expand servers to account for apis
		lis, err := utils.GetNextListener()
		if err != nil {
			return err
		}

		srv := &apiServer{
			lis: lis,
			srv: fhttp,
		}

		// get a free port and listen on that for this API
		go func(srv *apiServer) {
			err := srv.srv.Serve(srv.lis)
			if err != nil {
				fmt.Println(err)
			}
		}(srv)

		s.apiServers = append(s.apiServers, srv)
	}

	return nil
}

func (s *BaseHttpGateway) Start(pool pool.WorkerPool) error {
	var err error
	// Assign the pool and block
	s.pool = pool
	s.stop = make(chan bool)

	// Setup routes
	r := router.New()
	// Publish to a topic
	r.POST("/topic/{name}", s.handleTopicRequest)

	r.NotFound = func(ctx *fasthttp.RequestCtx) {
		if string(ctx.Path()) == "/" {
			ctx.Redirect(fmt.Sprintf("http://localhost:%v", s.dashPort), fasthttp.StatusMovedPermanently)
		} else {
			ctx.Redirect(fmt.Sprintf("http://localhost:%v/not-found", s.dashPort), fasthttp.StatusTemporaryRedirect)
		}
	}

	s.serviceServer = &fasthttp.Server{
		ReadTimeout:     time.Second * 1,
		IdleTimeout:     time.Second * 1,
		CloseOnShutdown: true,
		Handler:         r.Handler,
	}

	s.serviceListener, err = utils.GetNextListener()
	if err != nil {
		return err
	}

	go func() {
		_ = s.serviceServer.Serve(s.serviceListener)
	}()

	// block on a stop signal
	<-s.stop

	return nil
}

func (s *BaseHttpGateway) Stop() error {
	for _, s := range s.apiServers {
		// shutdown all the servers
		// this will allow Start to exit
		_ = s.srv.Shutdown()
	}

	s.stop <- true

	return nil
}

// Create new HTTP gateway
// XXX: No External Args for function atm (currently the plugin loader does not pass any argument information)
func NewGateway() (*BaseHttpGateway, error) {
	return &BaseHttpGateway{}, nil
}
