package workers

import (
	"sync"

	"github.com/sirupsen/logrus"
	"letraz-scrapper/pkg/utils"
)

// Dispatcher manages job distribution to workers
type Dispatcher struct {
	jobQueue    chan ScrapeJob
	workers     []*Worker
	workerQueue chan chan ScrapeJob
	quit        chan bool
	logger      *logrus.Logger
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
		logger:      utils.GetLogger().WithField("component", "dispatcher").Logger,
	}
}

// Start starts the dispatcher
func (d *Dispatcher) Start() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.running {
		return
	}

	d.logger.Info("Starting job dispatcher")

	// Start job dispatching
	go d.dispatch()

	d.running = true
	d.logger.WithField("workers", len(d.workers)).Info("Job dispatcher started")
}

// Stop stops the dispatcher
func (d *Dispatcher) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running {
		return
	}

	d.logger.Info("Stopping job dispatcher")

	// Send quit signal
	d.quit <- true

	d.running = false
	d.logger.Info("Job dispatcher stopped")
}

// dispatch handles the main job dispatching logic
func (d *Dispatcher) dispatch() {
	workerIndex := 0

	for {
		select {
		case job := <-d.jobQueue:
			d.logger.WithFields(logrus.Fields{
				"job_id": job.ID,
				"url":    job.URL,
			}).Debug("Received job for dispatch")

			// Simple round-robin assignment
			// This ensures each job is assigned to exactly one worker
		assignLoop:
			for {
				worker := d.workers[workerIndex]
				workerIndex = (workerIndex + 1) % len(d.workers)

				select {
				case worker.JobChan <- job:
					d.logger.WithFields(logrus.Fields{
						"job_id":    job.ID,
						"worker_id": worker.ID,
					}).Debug("Job assigned to worker")
					break assignLoop
				default:
					// Worker is busy, try next one
					continue
				}
			}

		case <-d.quit:
			d.logger.Info("Job dispatcher stopping")
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
