package batch

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Chichichkin/K8sLoggingAgent/internal/logging"
	"github.com/Chichichkin/K8sLoggingAgent/internal/testutils"
	"github.com/stretchr/testify/assert"
)

type MockSender struct {
	sentBatches [][]logging.LogEntry
	mu          sync.Mutex
	fail        bool
}

func (m *MockSender) SendBatch(entries []logging.LogEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.fail {
		return fmt.Errorf("mock send failed")
	}

	m.sentBatches = append(m.sentBatches, entries)
	return nil
}

func (m *MockSender) GetSentBatches() [][]logging.LogEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sentBatches
}

func TestBatchProcessor_AddEntry(t *testing.T) {
	mockSender := &MockSender{}
	config := logging.Config{
		BatchSize:    2,
		BatchTimeout: 1 * time.Second,
		MaxRetries:   3,
	}

	processor := NewBatchProcessor(context.TODO(), mockSender, config)

	processor.Start()

	entries := []logging.LogEntry{
		{Message: "test1", Timestamp: time.Now()},
		{Message: "test2", Timestamp: time.Now()},
		{Message: "test3", Timestamp: time.Now()},
	}

	for _, entry := range entries {
		processor.AddEntry(entry)
	}

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	batches := mockSender.GetSentBatches()
	assert.Greater(t, len(batches), 0)

	totalEntries := 0
	for _, batch := range batches {
		totalEntries += len(batch)
	}

	assert.Equal(t, 2, totalEntries)
}

func TestBatchProcessor_BatchTimeout(t *testing.T) {
	mockSender := &MockSender{}
	config := logging.Config{
		BatchSize:    100,
		BatchTimeout: 100 * time.Millisecond,
		MaxRetries:   3,
	}

	processor := NewBatchProcessor(context.TODO(), mockSender, config)

	processor.Start()

	processor.AddEntry(logging.LogEntry{
		Message:   "timeout test",
		Timestamp: time.Now(),
	})

	time.Sleep(200 * time.Millisecond)

	batches := mockSender.GetSentBatches()
	assert.Greater(t, len(batches), 0)
}

func TestBatchProcessor_Stop(t *testing.T) {
	mockSender := &MockSender{}
	config := logging.Config{
		BatchSize:    100,
		BatchTimeout: 1 * time.Second,
		MaxRetries:   3,
	}

	processor := NewBatchProcessor(context.TODO(), mockSender, config)

	processor.Start()

	for i := 0; i < 5; i++ {
		processor.AddEntry(logging.LogEntry{
			Message:   fmt.Sprintf("test %d", i),
			Timestamp: time.Now(),
		})
	}

	processor.Stop()
}

func TestBatchProcessor_ChannelFull(t *testing.T) {
	mockSender := &MockSender{fail: true}
	config := logging.Config{
		BatchSize:    1, // Small batch size
		BatchTimeout: 1 * time.Second,
		MaxRetries:   3,
	}

	processor := NewBatchProcessor(context.TODO(), mockSender, config)

	processor.Start()

	for i := 0; i < 150; i++ {
		processor.AddEntry(logging.LogEntry{
			Message:   fmt.Sprintf("test %d", i),
			Timestamp: time.Now(),
		})
	}
}

func TestBatchProcessor_IntegrationWithMockSender(t *testing.T) {
	mockSender := &testutils.MockLogSender{}
	config := logging.Config{
		BatchSize:    3,
		BatchTimeout: 50 * time.Millisecond,
		MaxRetries:   2,
	}

	processor := NewBatchProcessor(context.TODO(), mockSender, config)

	processor.Start()

	entries := []logging.LogEntry{
		{Message: "test1", Timestamp: time.Now(), Labels: map[string]string{"pod": "pod-1"}},
		{Message: "test2", Timestamp: time.Now(), Labels: map[string]string{"pod": "pod-1"}},
		{Message: "test3", Timestamp: time.Now(), Labels: map[string]string{"pod": "pod-1"}},
		{Message: "test4", Timestamp: time.Now(), Labels: map[string]string{"pod": "pod-2"}},
		{Message: "test5", Timestamp: time.Now(), Labels: map[string]string{"pod": "pod-2"}},
		{Message: "test6", Timestamp: time.Now(), Labels: map[string]string{"pod": "pod-2"}},
	}

	for _, entry := range entries {
		processor.AddEntry(entry)
	}

	time.Sleep(100 * time.Millisecond)

	batches := mockSender.GetSentBatches()
	assert.Greater(t, len(batches), 1)

	for _, batch := range batches {
		assert.Greater(t, len(batch), 0)
	}
}

func TestBatchProcessor_ConcurrentAddAndFlush(t *testing.T) {
	mockSender := &MockSender{}
	config := logging.Config{
		BatchSize:    5,
		BatchTimeout: 50 * time.Millisecond,
		MaxRetries:   1,
	}

	processor := NewBatchProcessor(context.TODO(), mockSender, config)
	processor.Start()
	defer processor.Stop()

	var wg sync.WaitGroup
	worker := func(id int) {
		for i := 0; i < 50; i++ {
			processor.AddEntry(logging.LogEntry{
				Message:   fmt.Sprintf("w%d-%d", id, i),
				Timestamp: time.Now(),
			})
			if i%10 == 0 {
				time.Sleep(1 * time.Millisecond)
			}
		}
		wg.Done()
	}

	wg.Add(5)
	for w := 0; w < 5; w++ {
		go worker(w)
	}
	wg.Wait()

	time.Sleep(200 * time.Millisecond)
	batches := mockSender.GetSentBatches()
	total := 0
	for _, b := range batches {
		total += len(b)
	}
	assert.GreaterOrEqual(t, total, 250)
}
