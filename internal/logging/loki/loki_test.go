package loki

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/Chichichkin/K8sLoggingAgent/internal/logging"
)

func TestLokiSender_SendBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)

		assert.Equal(t, "/loki/api/v1/push", r.URL.Path)

		var payload Payload
		err := json.NewDecoder(r.Body).Decode(&payload)
		assert.NoError(t, err)

		assert.Equal(t, 1, len(payload.Streams))

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sender := NewLokiSender(server.URL, 3)

	entries := []logging.LogEntry{
		{
			Timestamp: time.Now(),
			Message:   "test message 1",
			File:      "test.log",
			Labels:    map[string]string{"pod": "test-pod", "container": "test-container"},
		},
	}

	err := sender.SendBatch(entries)
	assert.NoError(t, err)
}

func TestLokiSender_SendBatch_Retry(t *testing.T) {
	retryCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		retryCount++
		if retryCount < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sender := NewLokiSender(server.URL, 3)

	entries := []logging.LogEntry{
		{
			Timestamp: time.Now(),
			Message:   "test message",
			Labels:    map[string]string{"pod": "test-pod"},
		},
	}

	err := sender.SendBatch(entries)
	assert.NoError(t, err)

	assert.Equal(t, 2, retryCount)
}

func TestLokiSender_SendBatch_AllRetriesFail(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	sender := NewLokiSender(server.URL, 2)

	entries := []logging.LogEntry{
		{
			Timestamp: time.Now(),
			Message:   "test message",
			Labels:    map[string]string{"pod": "test-pod"},
		},
	}

	err := sender.SendBatch(entries)
	assert.Error(t, err)

	assert.Equal(t, 2, attempts)
}

func TestLokiSender_CreatePayload(t *testing.T) {
	sender := NewLokiSender("http://test:3100", 3)

	now := time.Now()
	entries := []logging.LogEntry{
		{
			Timestamp: now,
			Message:   "message 1",
			Labels:    map[string]string{"pod": "pod-1", "container": "container-1"},
		},
		{
			Timestamp: now.Add(time.Second),
			Message:   "message 2",
			Labels:    map[string]string{"pod": "pod-1", "container": "container-1"},
		},
		{
			Timestamp: now.Add(2 * time.Second),
			Message:   "message 3",
			Labels:    map[string]string{"pod": "pod-2", "container": "container-2"},
		},
	}

	payload := sender.createPayload(entries)

	assert.Equal(t, 2, len(payload.Streams))

	for _, stream := range payload.Streams {
		pod := stream.Stream["pod"]
		if pod == "pod-1" {
			assert.Equal(t, 2, len(stream.Values))
		} else if pod == "pod-2" {
			assert.Equal(t, 1, len(stream.Values))
		}
	}
}
