package sync

import "sync"

type Job[Result any] struct {
	Description string
	Func        func() Result
}

type JobResult[Result any] struct {
	Description string
	Result      Result
}

// WorkerPool is not thread safe
// Only use Go/Wait on the same thread
type WorkerPool[Result any] struct {
	in        chan Job[Result]
	triage    []Job[Result]
	work      chan Job[Result]
	done      chan JobResult[Result]
	results   []JobResult[Result]
	waitGroup sync.WaitGroup
}

// startSupervisor - Starts the supervisor goroutine
// the supervisor is responsible for accepting new work, distributing it to workers and collecting results
func (pool *WorkerPool[Result]) startSupervisor() {
	go func() {
		for {
			// If there is work in the triage, send as much as possible to workers
			workerAvailable := true
			for workerAvailable && len(pool.triage) > 0 {
				nextWork := pool.triage[0]
				select {
				case pool.work <- nextWork:
					// trim the slice
					pool.triage = pool.triage[1:]
				default:
					// couldn't find a worker, none available
					workerAvailable = false
				}
			}

			// wait for new work or results
			select {
			case in := <-pool.in:
				pool.waitGroup.Add(1)
				pool.triage = append(pool.triage, in)
			case result, ok := <-pool.done:
				if !ok {
					// no more result can be collected, we're done.
					return
				}
				// add a result
				pool.results = append(pool.results, result)
				pool.waitGroup.Done()
			}
		}
	}()
}

// startWorker - Starts a worker goroutine
func (pool *WorkerPool[Result]) startWorker() {
	go func() {
		for work := range pool.work {
			result := work.Func()

			pool.done <- JobResult[Result]{
				Description: work.Description,
				Result:      result,
			}
		}
	}()
}

// NewWorkerPool - Creates a new worker pool with the specified number of workers
func NewWorkerPool[Result any](workers int) *WorkerPool[Result] {
	pool := &WorkerPool[Result]{
		in:        make(chan Job[Result]),
		triage:    []Job[Result]{},
		work:      make(chan Job[Result], workers),
		done:      make(chan JobResult[Result], workers),
		results:   []JobResult[Result]{},
		waitGroup: sync.WaitGroup{},
	}

	pool.startSupervisor()

	for i := 0; i < workers; i++ {
		pool.startWorker()
	}

	return pool
}

// Go - Adds a job to the worker pool
// if there are no workers available, the job will be queued until one is available
func (pool *WorkerPool[Result]) Go(description string, fun func() Result) {
	if pool.in == nil {
		panic("cannot add jobs to a worker pool after calling Wait()")
	}
	pool.in <- Job[Result]{
		Description: description,
		Func:        fun,
	}
}

// Wait - Waits for all jobs to complete and returns the results
func (pool *WorkerPool[Result]) Wait() []JobResult[Result] {
	pool.in = nil
	pool.waitGroup.Wait()
	close(pool.done) // signal the supervisor to stop
	close(pool.work) // signal the workers to stop
	return pool.results
}
