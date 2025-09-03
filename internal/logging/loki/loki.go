package loki

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Chichichkin/K8sLoggingAgent/internal/logging"
)

type Sender struct {
	baseURL    string
	httpClient *http.Client
	maxRetries int
}

type Stream struct {
	Stream map[string]string `json:"stream"`
	Values [][2]string       `json:"values"`
}

type Payload struct {
	Streams []Stream `json:"streams"`
}

func NewLokiSender(baseURL string, maxRetries int) *Sender {
	return &Sender{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		maxRetries: maxRetries,
	}
}

func (ls *Sender) SendBatch(entries []logging.LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	payload := ls.createPayload(entries)
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	for i := 0; i < ls.maxRetries; i++ {
		err = ls.sendRequest(body)
		if err == nil {
			log.Printf("Successfully sent batch of %d entries to Loki", len(entries))
			return nil
		}

		if i < ls.maxRetries-1 {
			log.Printf("Retry %d/%d after error: %v", i+1, ls.maxRetries, err)
			time.Sleep(time.Duration(i+1) * time.Second)
		}
	}

	return fmt.Errorf("failed to send batch after %d attempts", ls.maxRetries)
}

func (ls *Sender) createPayload(entries []logging.LogEntry) Payload {
	streams := make(map[string]Stream)

	for _, entry := range entries {
		streamKey := ls.getStreamKey(entry)
		if _, exists := streams[streamKey]; !exists {
			// initializing new stream
			streams[streamKey] = Stream{
				Stream: ls.createLabels(entry),
				Values: [][2]string{},
			}
		}

		stream := streams[streamKey]
		timestamp := fmt.Sprintf("%d", entry.Timestamp.UnixNano())
		stream.Values = append(stream.Values, [2]string{timestamp, entry.Message})
		streams[streamKey] = stream
	}

	payload := Payload{
		Streams: make([]Stream, 0, len(streams)),
	}

	for _, stream := range streams {
		payload.Streams = append(payload.Streams, stream)
	}

	return payload
}

func (ls *Sender) getStreamKey(entry logging.LogEntry) string {
	return fmt.Sprintf("%s:%s", entry.Labels["pod"], entry.Labels["container"])
}

func (ls *Sender) createLabels(entry logging.LogEntry) map[string]string {
	labels := map[string]string{
		"job":  "node-logger",
		"node": os.Getenv("NODE_NAME"),
	}

	for k, v := range entry.Labels {
		labels[k] = v
	}

	return labels
}

func (ls *Sender) sendRequest(body []byte) error {
	req, err := http.NewRequest("POST", ls.baseURL+"/loki/api/v1/push", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := ls.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("loki returned status %d: %s", resp.StatusCode, string(responseBody))
	}

	return nil
}
