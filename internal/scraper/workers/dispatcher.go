package workers

import (
	"sync"

	"letraz-utils/internal/logging"
)

// Dispatcher manages job distribution to workers
type Dispatcher struct {
	jobQueue    chan ScrapeJob
	workers     []*Worker
	workerQueue chan chan ScrapeJob
	quit        chan bool
	logger      logging.Logger
	mu          sync.RWMutex
	running     bool
}

// NewDispatcher creates a new job dispatcher
func NewDispatcher(jobQueue chan ScrapeJob, workers []*Worker) *Dispatcher {
	workerQueue := make(chan chan ScrapeJob, len(workers))

	return &Dispatcher{
		jobQueue:    jobQueue,
		workers:     workers,
		workerQueue: workerQueue,
		quit:        make(chan bool),
		logger:      logging.GetGlobalLogger(),
	}
}

// Start starts the dispatcher
func (d *Dispatcher) Start() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.running {
		return
	}

	d.logger.Info("Starting job dispatcher", nil)
	d.logger.Debug("DEBUG: About to start dispatch goroutine", nil)

	// Start job dispatching
	go d.dispatch()

	d.running = true
	d.logger.Info("Job dispatcher started", map[string]interface{}{
		"workers": len(d.workers),
	})
	d.logger.Debug("DEBUG: Dispatcher Start method completed", nil)
}

// Stop stops the dispatcher
func (d *Dispatcher) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running {
		return
	}

	d.logger.Info("Stopping job dispatcher", nil)

	// Send quit signal
	d.quit <- true

	d.running = false
	d.logger.Info("Job dispatcher stopped", nil)
}

// dispatch handles the main job dispatching logic
func (d *Dispatcher) dispatch() {
	workerIndex := 0

	for {
		select {
		case job := <-d.jobQueue:
			// Simple round-robin assignment
			// This ensures each job is assigned to exactly one worker
		assignLoop:
			for {
				worker := d.workers[workerIndex]
				workerIndex = (workerIndex + 1) % len(d.workers)
				select {
				case worker.JobChan <- job:
					break assignLoop
				default:
					// Worker is busy, try next one
					continue
				}
			}

		case <-d.quit:
			return
		}
	}
}

// IsRunning returns true if dispatcher is running
func (d *Dispatcher) IsRunning() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.running
}
