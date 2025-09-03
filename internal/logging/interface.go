package logging

import (
	"time"
)

type LogEntry struct {
	Timestamp time.Time
	Message   string
	File      string
	Labels    map[string]string
}

type BatchProcessor interface {
	AddEntry(entry LogEntry)
	Start()
	Stop()
}

type LogSender interface {
	SendBatch(entries []LogEntry) error
}

type Config struct {
	BatchSize    int
	BatchTimeout time.Duration
	MaxRetries   int
}
