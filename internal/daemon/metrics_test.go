package daemon

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAdaptiveMetrics_BasicOperations(t *testing.T) {
	metrics := &LogDaemonMetrics{}

	metrics.IncFilesDiscovered()
	metrics.IncFilesProcessed()
	metrics.IncFilesFailed()
	metrics.IncWorkersActive()
	metrics.IncWorkersBusy()
	metrics.IncScaleUpOperations()
	metrics.IncScaleDownOperations()

	result := metrics.GetMetricsStamp()

	assert.Equal(t, 1, result.ScaleUpOperations)
	assert.Equal(t, 1, result.ScaleDownOperations)
	assert.Equal(t, 1, result.FilesDiscovered)
	assert.Equal(t, 1, result.FilesProcessed)
	assert.Equal(t, 1, result.FilesFailed)
	assert.Equal(t, 1, result.WorkersActive)
	assert.Equal(t, 1, result.WorkersBusy)
}

func TestAdaptiveMetrics_QueueUsage(t *testing.T) {
	metrics := &LogDaemonMetrics{
		FilesQueueCapacity: 10,
	}
	usage := metrics.GetQueueUsage()
	assert.Equal(t, 0.0, usage)

	metrics.FilesQueueCapacity = 10
	for i := 0; i < 5; i++ {
		metrics.IncAmountQueueFiles()
	}
	usage = metrics.GetQueueUsage()
	assert.InDelta(t, 0.5, usage, 1e-9)
}

func TestAdaptiveMetrics_DecrementOperations(t *testing.T) {
	metrics := &LogDaemonMetrics{}

	metrics.IncWorkersActive()
	metrics.IncWorkersBusy()

	metrics.DecWorkersActive()
	metrics.DecWorkersBusy()

	result := metrics.GetMetricsStamp()
	assert.Equal(t, 0, result.WorkersBusy)
	assert.Equal(t, 0, result.WorkersActive)
}

func TestAdaptiveMetrics_ConcurrentUpdates(t *testing.T) {
	metrics := &LogDaemonMetrics{FilesQueueCapacity: 1000}

	var wg sync.WaitGroup
	inc := func(fn func()) {
		for i := 0; i < 1000; i++ {
			fn()
		}
		wg.Done()
	}

	wg.Add(7)
	go inc(metrics.IncFilesDiscovered)
	go inc(metrics.IncFilesProcessed)
	go inc(metrics.IncFilesFailed)
	go inc(metrics.IncWorkersActive)
	go inc(metrics.IncWorkersBusy)
	go inc(metrics.IncScaleUpOperations)
	go inc(metrics.IncScaleDownOperations)
	wg.Wait()

	stamp := metrics.GetMetricsStamp()
	assert.Equal(t, 1000, stamp.FilesDiscovered)
	assert.Equal(t, 1000, stamp.FilesProcessed)
	assert.Equal(t, 1000, stamp.FilesFailed)
	assert.Equal(t, 1000, stamp.WorkersActive)
	assert.Equal(t, 1000, stamp.WorkersBusy)
	assert.Equal(t, 1000, stamp.ScaleUpOperations)
	assert.Equal(t, 1000, stamp.ScaleDownOperations)
}

func TestAdaptiveMetrics_QueueUsageConcurrent(t *testing.T) {
	metrics := &LogDaemonMetrics{FilesQueueCapacity: 200}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			for j := 0; j < 2; j++ {
				metrics.IncAmountQueueFiles()
			}
			wg.Done()
		}()
	}
	wg.Wait()

	usage := metrics.GetQueueUsage()
	assert.InDelta(t, 1.0, usage, 0.001)
}
