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
	"encoding/json"
	"fmt"
	"time"

	v1 "github.com/nitrictech/nitric/core/pkg/api/nitric/v1"
	"github.com/nitrictech/nitric/core/pkg/plugins/errors"
	"github.com/nitrictech/nitric/core/pkg/plugins/errors/codes"
	"github.com/nitrictech/nitric/core/pkg/plugins/events"
	"github.com/nitrictech/nitric/core/pkg/worker"
	"github.com/nitrictech/nitric/core/pkg/worker/pool"
)

type WorkerPoolEventService struct {
	events.UnimplementedeventsPlugin
	pool pool.WorkerPool
}

func (s *WorkerPoolEventService) deliverEvent(ctx context.Context, evt *v1.TriggerRequest) {
	topic := evt.GetTopic()
	if topic == nil {
		fmt.Printf("Cannot deliver trigger as it is not an event\n")
		// Just return
		return
	}

	targets := s.pool.GetWorkers(&pool.GetWorkerOptions{
		Trigger: evt,
	})

	fmt.Printf("Publishing to %s topic, %d subscriber(s)\n", topic.Topic, len(targets))

	for _, target := range targets {
		go func(target worker.Worker) {
			_, err := target.HandleTrigger(ctx, evt)
			if err != nil {
				// this is likely an error in the user's handler, we don't want it to bring the server down.
				// just log and move on.
				fmt.Println(err)
			}
		}(target)
	}
}

// Publish a message to a given topic
func (s *WorkerPoolEventService) Publish(ctx context.Context, topic string, delay int, event *events.NitricEvent) error {
	newErr := errors.ErrorsWithScope(
		"WorkerPoolEventService.Publish",
		map[string]interface{}{
			"topic": topic,
			"event": event,
		},
	)

	marshaledPayload, err := json.Marshal(event)
	if err != nil {
		return newErr(
			codes.Internal,
			"error marshalling event payload",
			err,
		)
	}

	trigger := &v1.TriggerRequest{
		Data: marshaledPayload,
		Context: &v1.TriggerRequest_Topic{
			Topic: &v1.TopicTriggerContext{
				Topic: topic,
			},
		},
	}

	if delay > 0 {
		go func(evt *v1.TriggerRequest) {
			// Wait to deliver the events
			time.Sleep(time.Duration(delay) * time.Second)
			s.deliverEvent(ctx, evt)
		}(trigger)
	} else {
		s.deliverEvent(ctx, trigger)
	}

	return nil
}

// Create new Dev EventService
func NewEvents(pool pool.WorkerPool) (events.EventService, error) {
	return &WorkerPoolEventService{
		pool: pool,
	}, nil
}
