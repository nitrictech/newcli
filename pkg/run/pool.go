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
	"strconv"

	"github.com/spf13/cobra"

	"github.com/nitrictech/nitric/core/pkg/utils"
	"github.com/nitrictech/nitric/core/pkg/worker"
	"github.com/nitrictech/nitric/core/pkg/worker/pool"
)

type WorkerEventType string

const (
	WorkerEventType_Add    WorkerEventType = "add"
	WorkerEventType_Remove WorkerEventType = "remove"
)

type WorkerEvent struct {
	Type   WorkerEventType
	Worker worker.Worker
}

type WorkerListener = func(WorkerEvent)

type RunProcessPool struct {
	pool.WorkerPool
	listeners []WorkerListener
}

func (r *RunProcessPool) notifyListeners(evt WorkerEvent) {
	for _, l := range r.listeners {
		l(evt)
	}
}

func (r *RunProcessPool) AddWorker(w worker.Worker) error {
	if err := r.WorkerPool.AddWorker(w); err != nil {
		return err
	}

	// notify listener of successfully added worker
	r.notifyListeners(WorkerEvent{
		Type:   WorkerEventType_Add,
		Worker: w,
	})

	return nil
}

func (r *RunProcessPool) RemoveWorker(w worker.Worker) error {
	if err := r.WorkerPool.RemoveWorker(w); err != nil {
		return err
	}

	// notify listener of successfully removed worker
	r.notifyListeners(WorkerEvent{
		Type:   WorkerEventType_Remove,
		Worker: w,
	})

	return nil
}

func (r *RunProcessPool) Listen(l WorkerListener) {
	r.listeners = append(r.listeners, l)
}

func NewRunProcessPool() *RunProcessPool {
	maxWorkers, err := strconv.Atoi(utils.GetEnv("MAX_WORKERS", "300"))
	cobra.CheckErr(err)

	return &RunProcessPool{
		listeners: make([]WorkerListener, 0),
		WorkerPool: pool.NewProcessPool(&pool.ProcessPoolOptions{
			MinWorkers: 0,
			MaxWorkers: maxWorkers,
		}),
	}
}
