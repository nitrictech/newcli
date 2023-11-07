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
	"log"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/fasthttp/router"
	"github.com/fasthttp/websocket"
	"github.com/google/uuid"
	"github.com/pterm/pterm"
	"github.com/samber/lo"
	"github.com/valyala/fasthttp"

	"github.com/nitrictech/cli/pkg/dashboard"
	"github.com/nitrictech/cli/pkg/history"
	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/utils"
	"github.com/nitrictech/nitric/cloud/common/cors"
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

type socketServer struct {
	lis net.Listener
	srv *fasthttp.Server

	workerCount int
}

var upgrader = websocket.FastHTTPUpgrader{}

type BaseHttpGateway struct {
	apiServers       []*apiServer
	httpServers      []*apiServer
	apis             []string
	httpWorkers      []int
	websocketWorkers []string
	socketServer     map[string]*socketServer
	serviceServer    *fasthttp.Server
	websocketPlugin  *RunWebsocketService
	serviceListener  net.Listener
	gateway.UnimplementedGatewayPlugin
	stop      chan bool
	pool      pool.WorkerPool
	project   *project.Project
	dash      *dashboard.Dashboard
	corsCache map[string]map[string]string
}

var _ gateway.GatewayService = &BaseHttpGateway{}

func apiWorkerFilter(apiName string) func(w worker.Worker) bool {
	return func(w worker.Worker) bool {
		if api, ok := w.(*worker.RouteWorker); ok {
			return api.Api() == apiName
		}

		return false
	}
}

func httpWorkerFilter(port int) func(w worker.Worker) bool {
	return func(w worker.Worker) bool {
		if http, ok := w.(*worker.HttpWorker); ok {
			return http.GetPort() == port
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
		addresses[port] = strings.Replace(s.httpServers[idx].lis.Addr().String(), "[::]", "localhost", 1)
	}

	return addresses
}

func (s *BaseHttpGateway) GetWebsocketAddresses() map[string]string {
	addresses := make(map[string]string)

	for socket, srv := range s.socketServer {
		if srv.workerCount > 0 {
			srvAddress := strings.Replace(srv.lis.Addr().String(), "[::]", "localhost", 1)
			addresses[socket] = srvAddress
		}
	}

	return addresses
}

func (s *BaseHttpGateway) handleHttpProxyRequest(idx int) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		port := s.httpWorkers[idx]

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
			Filter:  httpWorkerFilter(port),
		})
		if err != nil {
			ctx.Redirect(fmt.Sprintf("http://localhost:%v/not-found", s.dash.GetPort()), fasthttp.StatusTemporaryRedirect)
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

			return
		}

		ctx.Error("Response was not a Http response", 500)
	}
}

func (s *BaseHttpGateway) handleApiHttpRequest(idx int) fasthttp.RequestHandler {
	corsMiddleware := cors.CreateCorsMiddleware(s.corsCache)

	return func(ctx *fasthttp.RequestCtx) {
		if idx >= len(s.apis) {
			ctx.Error("Sorry, nitric is listening on this port but is waiting for an API to be available to handle, you may have removed an API during development this port will be assigned to an API when one becomes available", 404)
			return
		}

		apiName := s.apis[idx]

		ctx.Request.Header.Add("X-Nitric-Api", apiName)

		corsMiddleware(ctx, s.pool)

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

		path := string(ctx.URI().Path())

		_, err := url.Parse(path)
		if err != nil {
			ctx.Error(fmt.Sprintf("Bad Request: %v", err), 400)
			return
		}

		httpTrigger := &v1.TriggerRequest{
			Data: ctx.Request.Body(),
			Context: &v1.TriggerRequest_Http{
				Http: &v1.HttpTriggerContext{
					Method:      string(ctx.Request.Header.Method()),
					Path:        path,
					Headers:     headers,
					QueryParams: query,
				},
			},
		}

		worker, err := s.pool.GetWorker(&pool.GetWorkerOptions{
			Trigger: httpTrigger,
			Filter:  apiWorkerFilter(apiName),
		})

		if worker == nil && s.corsCache[apiName] != nil && string(ctx.Request.Header.Method()) == "OPTIONS" {
			ctx.Response.SetStatusCode(204)
			return
		}

		if err != nil {
			ctx.Redirect(fmt.Sprintf("http://localhost:%v/not-found", s.dash.GetPort()), fasthttp.StatusTemporaryRedirect)
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

			return
		}

		ctx.Error("Response was not a Http response", 500)
	}
}

// websocket request handler
// TODO: Add broadcast capability
func (s *BaseHttpGateway) handleWebsocketRequest(socketName string) func(ctx *fasthttp.RequestCtx) {
	return func(ctx *fasthttp.RequestCtx) {
		upgrader.CheckOrigin = func(ctx *fasthttp.RequestCtx) bool {
			return true
		}

		connectionId := uuid.New().String()

		query := map[string]*v1.QueryValue{}

		ctx.QueryArgs().VisitAll(func(key []byte, val []byte) {
			k := string(key)

			if query[k] == nil {
				query[k] = &v1.QueryValue{}
			}

			query[k].Value = append(query[k].Value, string(val))
		})

		connectionRequest := &v1.TriggerRequest{
			Context: &v1.TriggerRequest_Websocket{
				Websocket: &v1.WebsocketTriggerContext{
					Socket:       socketName,
					Event:        v1.WebsocketEvent_Connect,
					ConnectionId: connectionId,
					QueryParams:  query,
				},
			},
		}

		w, err := s.pool.GetWorker(&pool.GetWorkerOptions{
			Trigger: connectionRequest,
		})
		if err != nil {
			ctx.Error("No worker found to handle connection request", 404)
			return
		}

		res, err := w.HandleTrigger(context.TODO(), connectionRequest)
		// handshake error...
		if err != nil {
			return
		}

		if res.GetWebsocket() == nil || !res.GetWebsocket().Success {
			// close the connection
			ctx.Error("Connection Refused", 500)
			return
		}

		err = upgrader.Upgrade(ctx, func(ws *websocket.Conn) {
			// generate a new connection ID for this client
			defer func() {
				// close within the websocket plugin will also call ws.Close
				err = s.websocketPlugin.Close(ctx, socketName, connectionId)
				if err != nil {
					pterm.Error.Println(err)
					return
				}
			}()

			err = s.websocketPlugin.RegisterConnection(socketName, connectionId, ws)
			if err != nil {
				pterm.Error.Println(err)
				return
			}

			// Handshake successful send a registration message with connection ID to the socket worker
			for {
				// We have successfully connected a new client
				// We can read/write messages to/from this client
				// Need to create a unique ID for this connection and store in a central location
				// This will allow connected clients to message eachother and broadcast to all clients as well
				// We'll only read new messages on this connection here, writing will be done by a separate runtime API
				_, message, err := ws.ReadMessage()
				if err != nil {
					log.Println("read:", err)
					break
				}

				// Send the message to the worker
				messageRequest := &v1.TriggerRequest{
					Data: message,
					Context: &v1.TriggerRequest_Websocket{
						Websocket: &v1.WebsocketTriggerContext{
							Socket:       socketName,
							Event:        v1.WebsocketEvent_Message,
							ConnectionId: connectionId,
						},
					},
				}

				w, err := s.pool.GetWorker(&pool.GetWorkerOptions{
					Trigger: messageRequest,
				})
				// error getting worker
				if err != nil {
					pterm.Error.Println("unable to find worker for websocket message request")
					return
				}

				_, err = w.HandleTrigger(context.TODO(), messageRequest)
				// handshake error...
				if err != nil {
					pterm.Error.Println(err)
					return
				}
			}

			// send disconnection message to the websocket worker
			disconnectionRequest := &v1.TriggerRequest{
				Context: &v1.TriggerRequest_Websocket{
					Websocket: &v1.WebsocketTriggerContext{
						Socket:       socketName,
						Event:        v1.WebsocketEvent_Disconnect,
						ConnectionId: connectionId,
					},
				},
			}

			w, err = s.pool.GetWorker(&pool.GetWorkerOptions{
				Trigger: disconnectionRequest,
			})
			if err != nil {
				pterm.Error.Println("unable to find worker for websocket disconnection request")
				return
			}

			_, err = w.HandleTrigger(context.TODO(), disconnectionRequest)
			if err != nil {
				pterm.Error.Println(err)
				return
			}
		})

		if err != nil {
			if _, ok := err.(websocket.HandshakeError); ok {
				pterm.Error.Println(err)
			}

			return
		}
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

func (s *BaseHttpGateway) refreshWebsocketWorkers() {
	s.websocketWorkers = make([]string, 0)

	workers := s.pool.GetWorkers(&pool.GetWorkerOptions{})
	websockets := lo.Reduce(workers, func(agg []string, w worker.Worker, idx int) []string {
		if api, ok := w.(*worker.WebsocketWorker); ok {
			if !lo.Contains(agg, api.Socket()) {
				agg = append(agg, api.Socket())
			}
		}

		return agg
	}, []string{})

	// sort the Http Worker Ports lowest to highest
	sort.Strings(websockets)

	s.websocketWorkers = append(s.websocketWorkers, websockets...)
}

func (s *BaseHttpGateway) createApiServers() error {
	// create an api server for every API worker
	for len(s.apiServers) < len(s.apis) {
		fhttp := &fasthttp.Server{
			ReadTimeout:     time.Second * 1,
			IdleTimeout:     time.Second * 1,
			CloseOnShutdown: true,
			Handler:         s.handleApiHttpRequest(len(s.apiServers)),
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

func (s *BaseHttpGateway) createWebsocketServers() error {
	if s.socketServer == nil {
		s.socketServer = make(map[string]*socketServer)
	}

	for _, sock := range s.websocketWorkers {
		currSocket, ok := s.socketServer[sock]

		if !ok {
			fhttp := &fasthttp.Server{
				ReadTimeout:     time.Second * 1,
				IdleTimeout:     time.Second * 1,
				CloseOnShutdown: true,
				Handler:         s.handleWebsocketRequest(sock),
			}

			lis, err := utils.GetNextListener()
			if err != nil {
				return err
			}

			srv := &socketServer{
				lis:         lis,
				srv:         fhttp,
				workerCount: 0,
			}

			go func(srv *socketServer) {
				err := srv.srv.Serve(srv.lis)
				if err != nil {
					fmt.Println(err)
				}
			}(srv)

			currSocket = srv
			// append to the server collection
			s.socketServer[sock] = currSocket

			// this is a brand new server we need to start up
			// lets start it and add it to the active list of servers
			// we can then filter the servers by their active worker count
			currSocket.workerCount = 0
		}

		currSocket.workerCount = currSocket.workerCount + 1
	}

	return nil
}

func (s *BaseHttpGateway) createHttpServers() error {
	// create an api server for every API worker
	for len(s.httpServers) < len(s.httpWorkers) {
		fhttp := &fasthttp.Server{
			ReadTimeout:     time.Second * 1,
			IdleTimeout:     time.Second * 1,
			CloseOnShutdown: true,
			Handler:         s.handleHttpProxyRequest(len(s.httpServers)),
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

		s.httpServers = append(s.httpServers, srv)
	}

	return nil
}

// Update the gateway and API based on the worker pool
func (s *BaseHttpGateway) Refresh() error {
	s.refreshApis()

	s.refreshHttpWorkers()

	s.refreshWebsocketWorkers()

	var err error

	err = s.createApiServers()
	if err != nil {
		return err
	}

	err = s.createHttpServers()
	if err != nil {
		return err
	}

	err = s.createWebsocketServers()
	if err != nil {
		return err
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
			ctx.Redirect(fmt.Sprintf("http://localhost:%v", s.dash.GetPort()), fasthttp.StatusMovedPermanently)
		} else {
			ctx.Redirect(fmt.Sprintf("http://localhost:%v/not-found", s.dash.GetPort()), fasthttp.StatusTemporaryRedirect)
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

func (s *BaseHttpGateway) AddCors(apiName string, def *v1.ApiCorsDefinition) {
	if def == nil {
		s.corsCache[apiName] = nil
		return
	}

	headers, err := cors.GetCorsHeaders(def)
	if err != nil {
		s.corsCache[apiName] = nil
	} else {
		s.corsCache[apiName] = *headers
	}
}

// Create new HTTP gateway
// XXX: No External Args for function atm (currently the plugin loader does not pass any argument information)
func NewGateway(wsPlugin *RunWebsocketService) (*BaseHttpGateway, error) {
	return &BaseHttpGateway{
		websocketPlugin: wsPlugin,
		corsCache:       map[string]map[string]string{},
	}, nil
}
