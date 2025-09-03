package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Chichichkin/K8sLoggingAgent/internal/testutils"
	"github.com/stretchr/testify/assert"
)

const (
	defaultScanInterval       = 10 * time.Millisecond
	defaultScaleCheckInterval = 10 * time.Millisecond
)

func makeTestConfig(root string) Config {
	return Config{
		LogRootPath:        root,
		ScanInterval:       defaultScanInterval,
		MinWorkers:         1,
		MaxWorkers:         3,
		FileQueueSize:      10,
		NodeName:           "node-1",
		ScaleUpThreshold:   0.5,
		ScaleDownThreshold: 0.25,
		ScaleCheckInterval: defaultScaleCheckInterval,
	}
}

func TestDaemonService_ContextCancellation(t *testing.T) {
	mockProcessor := &testutils.MockBatchProcessor{}
	tempDir := t.TempDir()
	config := makeTestConfig(tempDir)
	config.MaxWorkers = 2

	ctx, cancel := context.WithCancel(context.Background())
	s := NewLogDaemonService(ctx, config, mockProcessor)
	s.Start()

	cancel()
	time.Sleep(20 * time.Millisecond)

	select {
	case <-s.ctx.Done():
	default:
		t.Fatalf("service context not cancelled")
	}

	s.Stop()
}

func TestAdjustWorkers_ScaleUpAndDown(t *testing.T) {
	mockProcessor := &testutils.MockBatchProcessor{}
	tempDir := t.TempDir()
	config := makeTestConfig(tempDir)
	config.MinWorkers = 1
	config.MaxWorkers = 3

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	s := NewLogDaemonService(ctx, config, mockProcessor)
	defer cancel()
	for i := 0; i < s.currentWorkers; i++ {
		s.metrics.IncWorkersBusy()
	}
	prev := s.currentWorkers

	s.adjustWorkers()
	assert.GreaterOrEqual(t, s.currentWorkers, prev)

	s.adjustWorkers()
	assert.GreaterOrEqual(t, s.currentWorkers, prev)

	for s.metrics.GetMetricsStamp().WorkersBusy > 0 {
		s.metrics.DecWorkersBusy()
	}

	s.currentWorkers = 2
	s.adjustWorkers()
	assert.GreaterOrEqual(t, s.currentWorkers, s.minWorkers)
}

func TestExtractLabels(t *testing.T) {
	mockProcessor := &testutils.MockBatchProcessor{}
	config := makeTestConfig("/tmp")
	config.MinWorkers = 1
	config.MaxWorkers = 1
	config.FileQueueSize = 1
	config.NodeName = "node-1"
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	s := NewLogDaemonService(ctx, config, mockProcessor)
	defer cancel()

	path := "/var/log/pods/default_pod-1_uid123/container-1/app.log"
	labels := s.extractLabels(path)
	assert.Equal(t, "node-1", labels["node"])
	assert.Equal(t, "app.log", labels["file"])
	assert.Equal(t, "default", labels["namespace"])
	assert.Equal(t, "pod-1", labels["pod"])
	assert.Equal(t, "uid123", labels["pod_uid"])
	assert.Equal(t, "container-1", labels["container"])

	shortPath := "/tmp/a.log"
	labels = s.extractLabels(shortPath)
	assert.Equal(t, "node-1", labels["node"])
	assert.Equal(t, "a.log", labels["file"])
	_, hasNs := labels["namespace"]
	_, hasPod := labels["pod"]
	_, hasUID := labels["pod_uid"]
	_, hasContainer := labels["container"]
	assert.False(t, hasNs || hasPod || hasUID || hasContainer)
}

func TestDiscoverLogFiles_UsesTempStructure(t *testing.T) {
	root := testutils.CreateTempLogStructure(t)
	mockProcessor := &testutils.MockBatchProcessor{}
	config := makeTestConfig(root)
	config.MinWorkers = 1
	config.MaxWorkers = 1

	s := NewLogDaemonService(context.TODO(), config, mockProcessor)
	files, err := s.discoverLogFiles()
	assert.NoError(t, err)
	assert.Equal(t, len(files), 6)
}

func TestScanner_DiscoveredFiles(t *testing.T) {
	mockProcessor := &testutils.MockBatchProcessor{}
	tempDir := t.TempDir()

	log1 := filepath.Join(tempDir, "a.log")
	log2 := filepath.Join(tempDir, "b.log")
	nonLog := filepath.Join(tempDir, "c.txt")

	_ = os.WriteFile(log1, []byte("one\n"), 0644)
	_ = os.WriteFile(log2, []byte("two\n"), 0644)
	_ = os.WriteFile(nonLog, []byte("ignore\n"), 0644)

	config := makeTestConfig(tempDir)
	config.MinWorkers = 1
	config.MaxWorkers = 1
	config.FileQueueSize = 10
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	s := NewLogDaemonService(ctx, config, mockProcessor)

	// run single scan
	s.scanFiles()

	// Expect only .log files enqueued
	metrics := s.metrics.GetMetricsStamp()
	assert.Equal(t, 2, metrics.QueuedFiles)
	assert.GreaterOrEqual(t, metrics.FilesDiscovered, 2)
}

func TestProcessFile_TailsAppendedLines(t *testing.T) {
	mockProcessor := &testutils.MockBatchProcessor{}
	tempDir := t.TempDir()
	file := filepath.Join(tempDir, "tailme.log")
	if err := os.WriteFile(file, []byte("start\n"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	config := makeTestConfig(tempDir)
	config.ScanInterval = 100 * time.Millisecond
	config.MinWorkers = 1
	config.MaxWorkers = 1
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	s := NewLogDaemonService(ctx, config, mockProcessor)
	defer cancel()

	s.Start()

	time.Sleep(200 * time.Millisecond)

	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open append: %v", err)
	}
	_, _ = f.WriteString("l1\n")
	_, _ = f.WriteString("l2\n")
	_ = f.Close()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		entries, calls, _ := mockProcessor.GetStats()
		if entries >= 2 && calls >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	entries, calls, _ := mockProcessor.GetStats()
	assert.GreaterOrEqual(t, entries, 2)
	assert.GreaterOrEqual(t, calls, 2)

	s.Stop()
}
