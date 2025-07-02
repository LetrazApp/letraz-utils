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
	d.logger.Debug("DEBUG: About to start dispatch goroutine")

	// Start job dispatching
	go d.dispatch()

	d.running = true
	d.logger.WithField("workers", len(d.workers)).Info("Job dispatcher started")
	d.logger.Debug("DEBUG: Dispatcher Start method completed")
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

	d.logger.Info("🚀 DISPATCHER: dispatch loop started")

	for {
		select {
		case job := <-d.jobQueue:
			d.logger.WithFields(logrus.Fields{
				"job_id": job.ID,
				"url":    job.URL,
			}).Info("🎯 DISPATCHER: Received job for dispatch")

			d.logger.WithField("job_id", job.ID).Info("🔄 DISPATCHER: About to assign job to worker")

			// Simple round-robin assignment
			// This ensures each job is assigned to exactly one worker
		assignLoop:
			for {
				worker := d.workers[workerIndex]
				workerIndex = (workerIndex + 1) % len(d.workers)

				d.logger.WithFields(logrus.Fields{
					"job_id":    job.ID,
					"worker_id": worker.ID,
				}).Info("🎲 DISPATCHER: Trying to assign job to worker")

				select {
				case worker.JobChan <- job:
					d.logger.WithFields(logrus.Fields{
						"job_id":    job.ID,
						"worker_id": worker.ID,
					}).Info("✅ DISPATCHER: Job assigned to worker successfully")
					break assignLoop
				default:
					// Worker is busy, try next one
					d.logger.WithFields(logrus.Fields{
						"job_id":    job.ID,
						"worker_id": worker.ID,
					}).Info("⏳ DISPATCHER: Worker busy, trying next worker")
					continue
				}
			}

		case <-d.quit:
			d.logger.Info("🛑 DISPATCHER: Job dispatcher stopping")
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
