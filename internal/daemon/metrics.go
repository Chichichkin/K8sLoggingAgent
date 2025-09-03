package daemon

import (
	"sync"
)

type LogDaemonMetrics struct {
	FilesDiscovered     int
	FilesProcessed      int
	FilesFailed         int
	QueuedFiles         int
	FilesQueueCapacity  int
	WorkersActive       int
	WorkersBusy         int
	ScaleUpOperations   int
	ScaleDownOperations int
	mu                  sync.RWMutex
}

func (m *LogDaemonMetrics) IncFilesDiscovered() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.FilesDiscovered++
}

func (m *LogDaemonMetrics) IncFilesProcessed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.FilesProcessed++
}

func (m *LogDaemonMetrics) IncFilesFailed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.FilesFailed++
}

func (m *LogDaemonMetrics) IncAmountQueueFiles() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.QueuedFiles++
}
func (m *LogDaemonMetrics) DecAmountQueueFiles() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.QueuedFiles--
}

func (m *LogDaemonMetrics) IncWorkersActive() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WorkersActive++
}

func (m *LogDaemonMetrics) DecWorkersActive() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WorkersActive--
}

func (m *LogDaemonMetrics) IncWorkersBusy() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WorkersBusy++
}

func (m *LogDaemonMetrics) DecWorkersBusy() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WorkersBusy--
}

func (m *LogDaemonMetrics) IncScaleUpOperations() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ScaleUpOperations++
}

func (m *LogDaemonMetrics) IncScaleDownOperations() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ScaleDownOperations++
}

func (m *LogDaemonMetrics) GetMetricsStamp() LogDaemonMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return LogDaemonMetrics{
		FilesDiscovered:     m.FilesDiscovered,
		FilesProcessed:      m.FilesProcessed,
		FilesFailed:         m.FilesFailed,
		QueuedFiles:         m.QueuedFiles,
		FilesQueueCapacity:  m.FilesQueueCapacity,
		WorkersActive:       m.WorkersActive,
		WorkersBusy:         m.WorkersBusy,
		ScaleUpOperations:   m.ScaleUpOperations,
		ScaleDownOperations: m.ScaleDownOperations,
	}
}

func (m *LogDaemonMetrics) GetQueueUsage() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.FilesQueueCapacity == 0 {
		return 0
	}
	return float64(m.QueuedFiles) / float64(m.FilesQueueCapacity)
}
