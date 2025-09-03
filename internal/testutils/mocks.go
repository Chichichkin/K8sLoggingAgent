package testutils

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Chichichkin/K8sLoggingAgent/internal/logging"
)

type MockLogSender struct {
	SentBatches [][]logging.LogEntry
	mu          sync.Mutex
	ShouldFail  bool
	Delay       time.Duration
}

func (m *MockLogSender) SendBatch(entries []logging.LogEntry) error {
	if m.Delay > 0 {
		time.Sleep(m.Delay)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ShouldFail {
		return fmt.Errorf("mock send failed")
	}

	m.SentBatches = append(m.SentBatches, entries)
	return nil
}

func (m *MockLogSender) GetSentBatches() [][]logging.LogEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.SentBatches
}

type MockBatchProcessor struct {
	Entries       []logging.LogEntry
	mu            sync.Mutex
	AddEntryDelay time.Duration
	ShouldFail    bool
	AddEntryCalls int
	StartCalls    int
	StopCalls     int
}

func (m *MockBatchProcessor) AddEntry(entry logging.LogEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.AddEntryDelay > 0 {
		time.Sleep(m.AddEntryDelay)
	}

	if m.ShouldFail {
		panic("mock AddEntry failed")
	}

	m.Entries = append(m.Entries, entry)
	m.AddEntryCalls++
}

func (m *MockBatchProcessor) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.StartCalls++
}

func (m *MockBatchProcessor) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.StopCalls++
}

func (m *MockBatchProcessor) GetStats() (int, int, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Entries), m.AddEntryCalls, m.StartCalls
}

func CreateTempLogStructure(t *testing.T) string {
	tempDir := t.TempDir()

	structure := map[string]string{
		"default_pod-1_uid123/container-1/app.log":          "log content 1\nline 2\n",
		"default_pod-1_uid123/container-2/app.log":          "log content 2\nerror log\n",
		"kube-system_pod-2_uid456/container/app.log":        "log content 3\ninfo message\n",
		"default_pod-3_uid789/container/app.log":            "log content 4\n",
		"monitoring_pod-4_uid101/grafana/grafana.log":       "grafana starting\n",
		"monitoring_pod-4_uid101/prometheus/prometheus.log": "prometheus ready\n",
	}

	for path, content := range structure {
		fullPath := filepath.Join(tempDir, path)
		dir := filepath.Dir(fullPath)

		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}

		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write file %s: %v", fullPath, err)
		}
	}

	return tempDir
}
