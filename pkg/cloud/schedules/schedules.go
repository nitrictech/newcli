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

package schedules

import (
	"fmt"
	"log"
	"maps"
	"strconv"
	"strings"
	"sync"

	"github.com/asaskevich/EventBus"
	"github.com/robfig/cron/v3"

	"github.com/nitrictech/cli/pkg/cloud/errorsx"
	"github.com/nitrictech/cli/pkg/grpcx"
	"github.com/nitrictech/cli/pkg/validation"
	"github.com/nitrictech/nitric/core/pkg/logger"
	resourcespb "github.com/nitrictech/nitric/core/pkg/proto/resources/v1"
	schedulespb "github.com/nitrictech/nitric/core/pkg/proto/schedules/v1"
	"github.com/nitrictech/nitric/core/pkg/workers/schedules"
)

type (
	scheduleName = string
	serviceName  = string
)

type ScheduledService struct {
	ServiceName serviceName
	Schedule    *schedulespb.RegistrationRequest
}

type State = map[scheduleName]*ScheduledService

type ActionState struct {
	ScheduleName string
	Success      bool
}
type LocalSchedulesService struct {
	*schedules.ScheduleWorkerManager
	cron *cron.Cron

	schedulesLock sync.RWMutex

	errorLogger errorsx.ServiceErrorLogger

	schedules State
	bus       EventBus.Bus
}

const localSchedulesTopic = "local_schedules"

const localSchedulesActionTopic = "local_schedules_action"

func (l *LocalSchedulesService) publishState() {
	l.bus.Publish(localSchedulesTopic, maps.Clone(l.schedules))
}

func (l *LocalSchedulesService) SubscribeToState(fn func(State)) {
	// ignore the error, it's only returned if the fn param isn't a function
	_ = l.bus.Subscribe(localSchedulesTopic, fn)
}

func (l *LocalSchedulesService) publishAction(action ActionState) {
	l.bus.Publish(localSchedulesActionTopic, action)
}

func (l *LocalSchedulesService) SubscribeToAction(subscription func(ActionState)) {
	// ignore the error, it's only returned if the fn param isn't a function
	_ = l.bus.Subscribe(localSchedulesActionTopic, subscription)
}

var _ schedulespb.SchedulesServer = (*LocalSchedulesService)(nil)

func (l *LocalSchedulesService) GetSchedules() State {
	l.schedulesLock.RLock()
	defer l.schedulesLock.RUnlock()

	return l.schedules
}

func (l *LocalSchedulesService) registerSchedule(serviceName string, registrationRequest *schedulespb.RegistrationRequest) error {
	l.schedulesLock.Lock()
	defer l.schedulesLock.Unlock()

	if !validation.IsValidResourceName(registrationRequest.ScheduleName) {
		l.errorLogger(serviceName,
			fmt.Errorf("invalid name: \"%s\" for %s resource", registrationRequest.ScheduleName, resourcespb.ResourceType_Schedule))
		return nil
	}

	if l.schedules[registrationRequest.ScheduleName] != nil {
		existing := l.schedules[registrationRequest.ScheduleName]

		return fmt.Errorf("conflict: schedule \"%s\" already taken by service %s", existing.Schedule.ScheduleName, existing.ServiceName)
	}

	l.schedules[registrationRequest.ScheduleName] = &ScheduledService{
		ServiceName: serviceName,
		Schedule:    registrationRequest,
	}

	l.publishState()

	return nil
}

func (l *LocalSchedulesService) unregisterSchedule(serviceName string, registrationRequest *schedulespb.RegistrationRequest) {
	l.schedulesLock.Lock()
	defer l.schedulesLock.Unlock()

	delete(l.schedules, registrationRequest.ScheduleName)

	l.publishState()
}

func (l *LocalSchedulesService) HandleRequest(request *schedulespb.ServerMessage) (*schedulespb.ClientMessage, error) {
	resp, err := l.ScheduleWorkerManager.HandleRequest(request)

	scheduleName := request.GetIntervalRequest().ScheduleName

	l.publishAction(ActionState{ScheduleName: scheduleName, Success: true})

	return resp, err
}

func (l *LocalSchedulesService) createCronSchedule(scheduleName, expression string) (cron.EntryID, error) {
	return l.cron.AddFunc(expression, func() {
		_, err := l.HandleRequest(&schedulespb.ServerMessage{
			Content: &schedulespb.ServerMessage_IntervalRequest{
				IntervalRequest: &schedulespb.IntervalRequest{
					ScheduleName: scheduleName,
				},
			},
		})
		if err != nil {
			logger.Errorf("Error handling schedule: %s", err.Error())
		}
	})
}

func (l *LocalSchedulesService) Schedule(stream schedulespb.Schedules_ScheduleServer) error {
	serviceName, err := grpcx.GetServiceNameFromStream(stream)
	if err != nil {
		return err
	}

	peekableStream := grpcx.NewPeekableStreamServer(stream)

	firstRequest, err := peekableStream.Peek()
	if err != nil {
		return err
	}

	if firstRequest.GetRegistrationRequest() == nil {
		return fmt.Errorf("first request must be a registration request")
	}

	err = l.registerSchedule(serviceName, firstRequest.GetRegistrationRequest())
	if err != nil {
		l.errorLogger(serviceName, err)
		return nil
	}

	defer l.unregisterSchedule(serviceName, firstRequest.GetRegistrationRequest())

	scheduleName := firstRequest.GetRegistrationRequest().ScheduleName
	cronExpression := ""

	switch t := firstRequest.GetRegistrationRequest().Cadence.(type) {
	case *schedulespb.RegistrationRequest_Cron:
		cronExpression = t.Cron.Expression
	case *schedulespb.RegistrationRequest_Every:
		parts := strings.Split(strings.TrimSpace(t.Every.Rate), " ")
		if len(parts) != 2 {
			return fmt.Errorf("invalid schedule rate: %s", t.Every.Rate)
		}

		initialRate, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("invalid schedule rate, must start with an integer")
		}

		// Dapr cron bindings only support hours, minutes and seconds. Convert days to hours
		if strings.HasPrefix(parts[1], "day") {
			parts[0] = fmt.Sprintf("%d", initialRate*24)
			parts[1] = "hours"
		}

		cronExpression = fmt.Sprintf("@every %s%c", parts[0], parts[1][0])
	default:
		return fmt.Errorf("unknown schedule type, must be one of: cron, every")
	}

	cronEntryId, err := l.createCronSchedule(scheduleName, cronExpression)
	if err != nil {
		return err
	}

	defer l.cron.Remove(cronEntryId)

	if err != nil {
		log.Fatal(err)
	}

	// Start the cron scheduler
	l.cron.Start()

	return l.ScheduleWorkerManager.Schedule(peekableStream)
}

func NewLocalSchedulesService(errorLogger errorsx.ServiceErrorLogger) *LocalSchedulesService {
	return &LocalSchedulesService{
		errorLogger:           errorLogger,
		ScheduleWorkerManager: schedules.New(),
		cron:                  cron.New(),
		bus:                   EventBus.New(),
		schedules:             make(State),
	}
}
