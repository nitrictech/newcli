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
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/olahol/melody"

	"github.com/nitrictech/cli/pkg/codeconfig"
	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/utils"
	"github.com/nitrictech/nitric/core/pkg/plugins/storage"
	"github.com/nitrictech/nitric/core/pkg/worker/pool"
)

type Dashboard struct {
	project              *project.Project
	apis                 []*openapi3.T
	schedules            []*codeconfig.TopicResult
	topics               []*codeconfig.TopicResult
	buckets              []string
	collections          []string
	queues               []string
	secrets              []string
	envMap               map[string]string
	melody               *melody.Melody
	triggerAddress       string
	storageAddress       string
	apiAddresses         map[string]string
	resourcesLastUpdated time.Time
	bucketNotifications  []*codeconfig.BucketNotification
}

type Api struct {
	Name    string                 `json:"name,omitempty"`
	OpenApi map[string]interface{} `json:"spec,omitempty"` // not sure which spec version yet
}

type DashboardResponse struct {
	Apis                []*openapi3.T                    `json:"apis"`
	Buckets             []string                         `json:"buckets"`
	Collections         []string                         `json:"collections"`
	Queues              []string                         `json:"queues"`
	Secrets             []string                         `json:"secrets"`
	Schedules           []*codeconfig.TopicResult        `json:"schedules"`
	Topics              []*codeconfig.TopicResult        `json:"topics"`
	ProjectName         string                           `json:"projectName,omitempty"`
	ApiAddresses        map[string]string                `json:"apiAddresses,omitempty"`
	TriggerAddress      string                           `json:"triggerAddress,omitempty"`
	StorageAddress      string                           `json:"storageAddress,omitempty"`
	BucketNotifications []*codeconfig.BucketNotification `json:"bucketNotifications,omitempty"`
}

type Bucket struct {
	Name         string     `json:"name,omitempty"`
	CreationDate *time.Time `json:"creationDate,omitempty"`
}

type RefreshOptions struct {
	Pool            pool.WorkerPool
	TriggerAddress  string
	StorageAddress  string
	ApiAddresses    map[string]string
	ServiceListener net.Listener
}

//go:embed dist/*
var content embed.FS

func New(p *project.Project, envMap map[string]string) (*Dashboard, error) {
	m := melody.New()

	return &Dashboard{
		project:             p,
		apis:                []*openapi3.T{},
		envMap:              envMap,
		melody:              m,
		bucketNotifications: []*codeconfig.BucketNotification{},
		schedules:           []*codeconfig.TopicResult{},
		topics:              []*codeconfig.TopicResult{},
	}, nil
}

func (d *Dashboard) AddBucket(name string) {
	// reset buckets to allow for most recent resources only
	if !d.resourcesLastUpdated.IsZero() && time.Since(d.resourcesLastUpdated) > time.Second*5 {
		d.buckets = []string{}
	}

	for _, b := range d.buckets {
		if b == name {
			return
		}
	}

	d.buckets = append(d.buckets, name)

	d.resourcesLastUpdated = time.Now()
}

func (d *Dashboard) AddQueue(name string) {
	// reset buckets to allow for most recent resources only
	if !d.resourcesLastUpdated.IsZero() && time.Since(d.resourcesLastUpdated) > time.Second*5 {
		d.queues = []string{}
	}

	for _, q := range d.queues {
		if q == name {
			return
		}
	}

	d.queues = append(d.queues, name)

	d.resourcesLastUpdated = time.Now()
}

func (d *Dashboard) AddCollection(name string) {
	// reset buckets to allow for most recent resources only
	if !d.resourcesLastUpdated.IsZero() && time.Since(d.resourcesLastUpdated) > time.Second*5 {
		d.collections = []string{}
	}

	for _, c := range d.collections {
		if c == name {
			return
		}
	}

	d.collections = append(d.collections, name)

	d.resourcesLastUpdated = time.Now()
}

func (d *Dashboard) AddSecret(name string) {
	// reset buckets to allow for most recent resources only
	if !d.resourcesLastUpdated.IsZero() && time.Since(d.resourcesLastUpdated) > time.Second*5 {
		d.secrets = []string{}
	}

	for _, s := range d.secrets {
		if s == name {
			return
		}
	}

	d.secrets = append(d.secrets, name)

	d.resourcesLastUpdated = time.Now()
}

func (d *Dashboard) Refresh(opts *RefreshOptions) error {
	cc, err := codeconfig.New(d.project, d.envMap)
	if err != nil {
		return err
	}

	spec, err := cc.SpecFromWorkerPool(opts.Pool)
	if err != nil {
		return err
	}

	d.apis = spec.Apis
	d.schedules = spec.Schedules
	d.topics = spec.Topics
	d.bucketNotifications = spec.BucketNotifications

	d.triggerAddress = opts.TriggerAddress
	d.apiAddresses = opts.ApiAddresses
	d.storageAddress = opts.StorageAddress

	err = d.sendUpdate()
	if err != nil {
		return err
	}

	return nil
}

func (d *Dashboard) Serve(sp storage.StorageService) (*int, error) {
	// Get the embedded files from the 'dist' directory
	staticFiles, err := fs.Sub(content, "dist")
	if err != nil {
		return nil, err
	}

	fs := http.FileServer(http.FS(staticFiles))

	// Serve the files using the http package
	http.Handle("/", fs)

	// handle websocket
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		err := d.melody.HandleRequest(w, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	// Register a handler for when a client connects
	d.melody.HandleConnect(func(s *melody.Session) {
		// Send a welcome message to the client
		err := d.sendUpdate()
		if err != nil {
			log.Fatal(err)
		}
	})

	d.melody.HandleMessage(func(s *melody.Session, msg []byte) {
		err := d.melody.Broadcast(msg)
		if err != nil {
			log.Print(err)
		}
	})

	// Define an API route under /call to proxy communication between app and apis
	http.HandleFunc("/api/call/", func(w http.ResponseWriter, r *http.Request) {
		// Set CORs headers
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// find call callAddress
		callAddress := r.Header.Get("X-Nitric-Local-Call-Address")

		// Remove "/api/call/" prefix from URL path
		path := strings.TrimPrefix(r.URL.Path, "/api/call/")

		// Build proxy request URL with query parameters
		query := r.URL.RawQuery
		if query != "" {
			query = "?" + query
		}
		url := fmt.Sprintf("http://%s/%s%s", callAddress, path, query)

		// Create a new request object
		req, err := http.NewRequest(r.Method, url, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Copy the headers from the original request to the new request
		for key, value := range r.Header {
			req.Header.Set(key, value[0])
		}

		// Send the new request and handle the response
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		// Copy the headers from the response to the response writer
		for key, value := range resp.Header {
			w.Header().Set(key, value[0])
		}

		// Copy the status code from the response to the response writer
		w.WriteHeader(resp.StatusCode)

		// Copy the response body to the response writer
		_, err = io.Copy(w, resp.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	http.HandleFunc("/api/storage", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			return
		}

		ctx := context.Background()
		bucket := r.URL.Query().Get("bucket")
		action := r.URL.Query().Get("action")

		if bucket == "" && action != "list-buckets" {
			w.WriteHeader(http.StatusBadRequest)
			w.Header().Set("Content-Type", "application/json")
			handleResponseWriter(w, []byte(`{"error": "Bucket is required"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")

		switch action {
		case "list-files":
			fileList, err := sp.ListFiles(ctx, bucket, nil)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			jsonResponse, _ := json.Marshal(fileList)
			handleResponseWriter(w, jsonResponse)
		case "write-file":
			fileKey := r.URL.Query().Get("fileKey")
			if fileKey == "" {
				w.WriteHeader(http.StatusBadRequest)
				handleResponseWriter(w, []byte(`{"error": "fileKey is required for delete-file action"}`))
				return
			}

			// Read the contents of the file
			contents, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				handleResponseWriter(w, []byte(fmt.Sprintf(`{"error": "%s"}`, err.Error())))
				return
			}

			err = sp.Write(ctx, bucket, fileKey, contents)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			handleResponseWriter(w, []byte(`{"success": true}`))
		case "delete-file":
			fileKey := r.URL.Query().Get("fileKey")
			if fileKey == "" {
				w.WriteHeader(http.StatusBadRequest)
				handleResponseWriter(w, []byte(`{"error": "fileKey is required for delete-file action"}`))
				return
			}

			err = sp.Delete(ctx, bucket, fileKey)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			handleResponseWriter(w, []byte(`{"success": true}`))
		default:
			handleResponseWriter(w, []byte(`{"error": "Invalid action"}`))
		}
	})

	// Define an API route under /call to proxy communication between app and apis
	http.HandleFunc("/api/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "DELETE, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Content-Type", "application/json")

		historyType := r.URL.Query().Get("type")

		switch r.Method {
		case "OPTIONS":
			return
		case "DELETE":
			err := DeleteHistoryRecord(d.project.Dir, RecordType(historyType))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		case "GET":
			history, err := ReadHistoryRecords(d.project.Dir, RecordType(historyType))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			data, err := json.Marshal(history)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			handleResponseWriter(w, data)
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
	})

	// using ephemeral ports, we will redirect to the dashboard on main api 4000
	dashListener, err := utils.GetNextListener(utils.MinPort(49152), utils.MaxPort(65535))
	if err != nil {
		return nil, err
	}

	serveFn := func() {
		err = http.Serve(dashListener, nil)
		if err != nil {
			log.Fatal(err)
		}
	}
	go serveFn()

	port := dashListener.Addr().(*net.TCPAddr).Port

	return &port, nil
}

func handleResponseWriter(w http.ResponseWriter, data []byte) {
	_, err := w.Write(data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (d *Dashboard) sendUpdate() error {
	// ignore if no apis
	if len(d.apis) == 0 {
		return nil
	}

	response := &DashboardResponse{
		Apis:                d.apis,
		Topics:              d.topics,
		Buckets:             d.buckets,
		Schedules:           d.schedules,
		Queues:              d.queues,
		Collections:         d.collections,
		Secrets:             d.secrets,
		ProjectName:         d.project.Name,
		ApiAddresses:        d.apiAddresses,
		TriggerAddress:      d.triggerAddress,
		StorageAddress:      d.storageAddress,
		BucketNotifications: d.bucketNotifications,
	}

	// Encode the response as JSON
	jsonData, err := json.Marshal(response)
	if err != nil {
		return err
	}

	err = d.melody.Broadcast(jsonData)

	return err
}
