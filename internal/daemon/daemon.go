package daemon

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Chichichkin/K8sLoggingAgent/internal/logging"
	"github.com/hpcloud/tail"
)

type LogDaemonService struct {
	config         Config
	batchProcessor logging.BatchProcessor
	fileQueue      chan string
	workers        []*worker
	workersWg      sync.WaitGroup
	subServicesWg  sync.WaitGroup
	ctx            context.Context
	cancel         context.CancelFunc
	metrics        *LogDaemonMetrics

	scaleMutex     sync.RWMutex
	currentWorkers int
	maxWorkers     int
	minWorkers     int

	seenFiles map[string]struct{}
}

type worker struct {
	id     int
	ctx    context.Context
	cancel context.CancelFunc
}

type Config struct {
	LogRootPath        string
	ScanInterval       time.Duration
	MinWorkers         int
	MaxWorkers         int
	FileQueueSize      int
	NodeName           string
	FileBufferSize     int
	ScaleUpThreshold   float64 // default: 0.9
	ScaleDownThreshold float64 // default: 0.3
	ScaleCheckInterval time.Duration
	// If > 0, stop tailing a file after this period without new lines
	FileIdleTimeout time.Duration
}

// NewLogDaemonService always creates 3 + config.MinWorkers go routines on Start()
func NewLogDaemonService(ctx context.Context, config Config, batchProcessor logging.BatchProcessor) *LogDaemonService {
	nCtx, cancel := context.WithCancel(ctx)

	service := &LogDaemonService{
		config:         config,
		batchProcessor: batchProcessor,
		fileQueue:      make(chan string, config.FileQueueSize),
		ctx:            nCtx,
		cancel:         cancel,
		metrics: &LogDaemonMetrics{
			FilesQueueCapacity: config.FileQueueSize,
		},
		minWorkers:     config.MinWorkers,
		maxWorkers:     config.MaxWorkers,
		currentWorkers: config.MinWorkers,
		seenFiles:      make(map[string]struct{}),
	}

	service.workers = make([]*worker, config.MaxWorkers+1)

	return service
}
func (s *LogDaemonService) Start() {
	log.Printf("Starting log daemon service: min num workers=%d, maxnum workers=%d, queue size =%d",
		s.minWorkers, s.maxWorkers, s.config.FileQueueSize)

	for i := 0; i < s.minWorkers; i++ {
		s.startWorker(i)
	}

	s.subServicesWg.Add(1)
	go s.scanner()

	s.subServicesWg.Add(1)
	go s.monitorAndScale()

	s.subServicesWg.Add(1)
	go s.metricsReporter()

	log.Println("Log daemon service started")
}

func (s *LogDaemonService) Stop() {
	log.Println("Stopping log daemon service...")
	s.cancel()

	s.subServicesWg.Wait()

	close(s.fileQueue)
	s.workersWg.Wait()

	log.Println("Log daemon service stopped")
}

func (s *LogDaemonService) startWorker(id int) {
	if id >= len(s.workers) || s.workers[id] != nil {
		return
	}

	workerCtx, cancel := context.WithCancel(s.ctx)
	worker := &worker{
		id:     id,
		ctx:    workerCtx,
		cancel: cancel,
	}
	s.workers[id] = worker

	s.workersWg.Add(1)
	go s.worker(worker)

	s.metrics.IncWorkersActive()
	log.Printf("Worker %d started", id)
}

func (s *LogDaemonService) stopWorker(id int) {
	if id >= len(s.workers) || s.workers[id] == nil {
		return
	}

	s.workers[id].cancel()
	s.workers[id] = nil

	s.metrics.DecWorkersActive()
	log.Printf("Worker %d stopped", id)
}

func (s *LogDaemonService) worker(worker *worker) {
	defer s.workersWg.Done()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Worker %d panicked: %v", worker.id, r)
		}
	}()

	for {
		select {
		case filePath, ok := <-s.fileQueue:
			if !ok {
				return
			}
			s.metrics.DecAmountQueueFiles()
			s.metrics.IncWorkersBusy()
			s.processFile(worker.ctx, filePath)
			s.metrics.DecWorkersBusy()

		case <-worker.ctx.Done():
			return
		}
	}
}

func (s *LogDaemonService) processFile(ctx context.Context, filePath string) {
	defer s.metrics.IncFilesProcessed()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("File processing panicked for %s: %v", filePath, r)
			s.metrics.IncFilesFailed()
		}
	}()

	t, err := tail.TailFile(filePath, tail.Config{
		Follow:   true,
		ReOpen:   true,
		Poll:     true,
		Location: &tail.SeekInfo{Offset: 0, Whence: io.SeekEnd},
		Logger:   tail.DiscardingLogger,
	})
	if err != nil {
		log.Printf("Failed to tail file %s: %v", filePath, err)
		s.metrics.IncFilesFailed()
		return
	}
	defer t.Cleanup()

	checkTicker := time.NewTicker(1 * time.Second)
	defer checkTicker.Stop()

	lastActivity := time.Now()

	for {
		select {
		case line := <-t.Lines:
			if line == nil {
				continue
			}
			if line.Err != nil {
				log.Printf("Error reading from %s: %v", filePath, line.Err)
				continue
			}

			entry := logging.LogEntry{
				Timestamp: time.Now(),
				Message:   line.Text,
				File:      filePath,
				Labels:    s.extractLabels(filePath),
			}

			s.batchProcessor.AddEntry(entry)
			lastActivity = time.Now()

		case <-checkTicker.C:
			// waking up from blocking line reading to check context status and idle timeout
			if s.config.FileIdleTimeout > 0 && time.Since(lastActivity) > s.config.FileIdleTimeout {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (s *LogDaemonService) scanner() {
	defer s.subServicesWg.Done()

	ticker := time.NewTicker(s.config.ScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.scanFiles()

		case <-s.ctx.Done():
			return
		}
	}
}

func (s *LogDaemonService) scanFiles() {
	files, err := s.discoverLogFiles()
	if err != nil {
		log.Printf("Error discovering log files: %v", err)
		return
	}

	for _, file := range files {
		if _, ok := s.seenFiles[file]; !ok {
			s.metrics.IncFilesDiscovered()
			s.seenFiles[file] = struct{}{}
		}
		select {
		case s.fileQueue <- file:
			s.metrics.IncAmountQueueFiles()
		case <-s.ctx.Done():
			return

		default:
			log.Printf("File queue full (%d/%d), skipping %s",
				len(s.fileQueue), cap(s.fileQueue), file)
		}
	}
}

func (s *LogDaemonService) monitorAndScale() {
	defer s.subServicesWg.Done()

	ticker := time.NewTicker(s.config.ScaleCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.adjustWorkers()

		case <-s.ctx.Done():
			return
		}
	}
}

func (s *LogDaemonService) adjustWorkers() {
	metrics := s.metrics.GetMetricsStamp()

	if s.currentWorkers >= s.maxWorkers && s.currentWorkers <= s.minWorkers {
		return
	}

	queueUsage := metrics.GetQueueUsage()
	workerUtilization := 0.0
	if s.currentWorkers > 0 {
		workerUtilization = float64(metrics.WorkersBusy) / float64(s.currentWorkers)
	}

	if queueUsage > s.config.ScaleUpThreshold &&
		workerUtilization > s.config.ScaleUpThreshold &&
		s.currentWorkers < s.maxWorkers {
		s.scaleUp()
	} else if queueUsage < s.config.ScaleDownThreshold &&
		workerUtilization < s.config.ScaleDownThreshold &&
		s.currentWorkers > s.minWorkers {
		s.scaleDown()
	}
}

func (s *LogDaemonService) scaleUp() {
	s.scaleMutex.Lock()
	defer s.scaleMutex.Unlock()

	if s.currentWorkers >= s.maxWorkers {
		return
	}

	newWorkerID := s.currentWorkers
	s.currentWorkers++

	s.startWorker(newWorkerID)
	s.metrics.IncScaleUpOperations()

	log.Printf("Scaled up to %d workers (queue usage: %d%%)",
		s.currentWorkers, int(s.metrics.GetQueueUsage()*100))
}

func (s *LogDaemonService) scaleDown() {
	s.scaleMutex.Lock()
	defer s.scaleMutex.Unlock()

	if s.currentWorkers <= s.minWorkers {
		return
	}

	workerToStop := s.currentWorkers - 1
	s.currentWorkers--

	s.stopWorker(workerToStop)
	s.metrics.IncScaleDownOperations()

	log.Printf("Scaled down to %d workers (queue usage: %d%%)",
		s.currentWorkers, int(s.metrics.GetQueueUsage()*100))
}

func (s *LogDaemonService) metricsReporter() {
	defer s.subServicesWg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			metrics := s.metrics.GetMetricsStamp()
			queueUsage := s.metrics.GetQueueUsage()

			log.Printf(
				"Metrics: workers active/ max workers =%d/%d, workers busy=%d, queue status=%d/%d (%d%%), files=%d/%d, scale_up=%d, scale_down=%d",
				metrics.WorkersActive, s.maxWorkers,
				metrics.WorkersBusy,
				metrics.QueuedFiles, s.config.FileQueueSize, int(queueUsage*100),
				metrics.FilesProcessed, metrics.FilesDiscovered,
				metrics.ScaleUpOperations,
				metrics.ScaleDownOperations,
			)

		case <-s.ctx.Done():
			return
		}
	}
}

func (s *LogDaemonService) discoverLogFiles() ([]string, error) {
	var logFiles []string

	err := filepath.Walk(s.config.LogRootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("Error accessing path %s: %v", path, err)
			return nil
		}

		if !info.IsDir() && strings.HasSuffix(info.Name(), ".log") {
			logFiles = append(logFiles, path)
		}
		return nil
	})

	return logFiles, err
}

func (s *LogDaemonService) extractLabels(filePath string) map[string]string {
	labels := map[string]string{
		"node": s.config.NodeName,
		"file": filepath.Base(filePath),
	}

	parts := strings.Split(filePath, "/")
	if len(parts) >= 5 {
		podParts := strings.Split(parts[4], "_")
		if len(podParts) >= 3 {
			labels["namespace"] = podParts[0]
			labels["pod"] = podParts[1]
			labels["pod_uid"] = podParts[2]
		}

		if len(parts) >= 6 {
			labels["container"] = parts[5]
		}
	}

	return labels
}
