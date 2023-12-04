package sync

import (
	"fmt"
	"sync"
)

type ExecutionIdentifier = string

type WorkErr struct {
	Identifier ExecutionIdentifier
	Err        error
}

type Work struct {
	Identifier ExecutionIdentifier
	Func       func() error
}

type SuperPool struct {
	queue           []Work
	errors          SyncMap[ExecutionIdentifier, error]
	workerLock      sync.Mutex
	maxWorkers      uint32
	activeWorkers   uint32
	workerReadyChan chan chan Work
	jobWaitGroup    sync.WaitGroup
}

func NewSuperPool(maxConcurrent uint32) *SuperPool {
	return &SuperPool{
		jobWaitGroup: sync.WaitGroup{},
		maxWorkers:   maxConcurrent,
	}
}

func (s *SuperPool) enqueue(job Work) {
	s.jobWaitGroup.Add(1)
	s.workerLock.Lock()
	defer s.workerLock.Unlock()

	s.errors.Set(job.Identifier, nil)
	s.queue = append(s.queue, job)
}

// Go - Adds a new job to the pool
func (s *SuperPool) Go(jobId ExecutionIdentifier, f func() error) error {
	if _, found := s.errors.Get(jobId); found {
		return fmt.Errorf("process already exists")
	}
	s.enqueue(Work{
		Identifier: jobId,
		Func:       f,
	})

	s.processWorkQueue()

	return nil
}

// processWorkQueue ensures the supervisor and workers are running and processing the work queue.
func (s *SuperPool) processWorkQueue() {
	s.startSupervisor()
	s.startWorkers()
}

// Wait blocks until all jobs are complete
func (s *SuperPool) Wait() map[ExecutionIdentifier]error {
	s.jobWaitGroup.Wait()

	close(s.workerReadyChan)

	return s.errors.AsMap()
}

// startSupervisor ensures the supervisor is running.
// the supervisor is responsible for assigning work to workers when they're ready.
func (s *SuperPool) startSupervisor() {
	s.workerLock.Lock()
	defer s.workerLock.Unlock()

	// if this is the first call, start the supervisor
	if s.workerReadyChan == nil {
		s.workerReadyChan = make(chan chan Work)
		go func() {
			for {
				workerChan, ok := <-s.workerReadyChan
				if !ok {
					break
				}

				s.workerLock.Lock()
				if len(s.queue) > 0 {
					workerChan <- s.queue[0]
					s.queue = s.queue[1:]
				} else {
					// got no new work for you bud
					close(workerChan)
					s.activeWorkers--
				}
				s.workerLock.Unlock()
			}
		}()
	}
}

// startWorkers ensures the worker pool is running.
// if more work is waiting than there are workers, new workers are started up to the max.
func (s *SuperPool) startWorkers() {
	s.workerLock.Lock()
	defer s.workerLock.Unlock()

	for i := 0; s.activeWorkers < s.maxWorkers && i < len(s.queue); i++ {
		if s.activeWorkers >= s.maxWorkers {
			return
		}
		s.activeWorkers++

		go func() {
			workChan := make(chan Work)
			for {
				// notify ready to receive new work
				s.workerReadyChan <- workChan
				// Get new work
				newWork, ok := <-workChan
				if !ok {
					break
				}
				err := newWork.Func()
				if err != nil {
					s.errors.Set(newWork.Identifier, err)
				}
				s.jobWaitGroup.Done()
			}
		}()
	}
}
