package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Chichichkin/K8sLoggingAgent/internal/daemon"
	"github.com/Chichichkin/K8sLoggingAgent/internal/logging"
	"github.com/Chichichkin/K8sLoggingAgent/internal/logging/batch"
	"github.com/Chichichkin/K8sLoggingAgent/internal/logging/loki"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	StartDaemon(ctx)

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-signalChan
		log.Println("Received shutdown signal")
		cancel()
	}()

	<-ctx.Done()
	log.Println("Shutting down...")
}

func StartDaemon(ctx context.Context) {
	config := getConfig()

	lokiSender := loki.NewLokiSender(config.LokiURL, config.MaxRetries)

	batchConfig := logging.Config{
		BatchSize:    config.BatchSize,
		BatchTimeout: config.BatchTimeout,
		MaxRetries:   config.MaxRetries,
	}

	batchProcessor := batch.NewBatchProcessor(ctx, lokiSender, batchConfig)
	batchProcessor.Start()

	serviceConfig := daemon.Config{
		LogRootPath:        config.LogRootPath,
		ScanInterval:       config.ScanInterval,
		MinWorkers:         config.MinWorkers,
		MaxWorkers:         config.MaxWorkers,
		FileQueueSize:      config.QueueSize,
		NodeName:           config.NodeName,
		FileBufferSize:     config.FileBufferSize,
		ScaleUpThreshold:   config.ScaleUpThreshold,
		ScaleDownThreshold: config.ScaleDownThreshold,
		ScaleCheckInterval: config.ScaleCheckInterval,
		FileIdleTimeout:    config.FileIdleTimeout,
	}

	logDaemonService := daemon.NewLogDaemonService(ctx, serviceConfig, batchProcessor)

	logDaemonService.Start()
}

// ------------------------------------  code for reading config -----------------------------------------------------

type AppConfig struct {
	LokiURL            string
	LogRootPath        string
	BatchSize          int
	BatchTimeout       time.Duration
	MaxRetries         int
	NodeName           string
	MinWorkers         int
	MaxWorkers         int
	QueueSize          int
	ScanInterval       time.Duration
	FileBufferSize     int
	ScaleUpThreshold   float64
	ScaleDownThreshold float64
	ScaleCheckInterval time.Duration
	FileIdleTimeout    time.Duration
}

func getConfig() AppConfig {
	return AppConfig{
		LokiURL:            getEnv("LOKI_URL", "http://loki:3100"),
		LogRootPath:        getEnv("LOG_PATH", "/var/log/pods"),
		BatchSize:          getEnvAsInt("BATCH_SIZE", 1000000),
		BatchTimeout:       getEnvAsDuration("BATCH_TIMEOUT", 5*time.Second),
		MaxRetries:         getEnvAsInt("MAX_RETRIES", 3),
		NodeName:           getEnv("NODE_NAME", "unknown"),
		MinWorkers:         getEnvAsInt("MIN_WORKERS", 2),
		MaxWorkers:         getEnvAsInt("MAX_WORKERS", 10),
		QueueSize:          getEnvAsInt("QUEUE_SIZE", 50),
		ScanInterval:       getEnvAsDuration("SCAN_INTERVAL", 30*time.Second),
		FileBufferSize:     getEnvAsInt("FILE_BUFFER_SIZE", 1000),
		ScaleUpThreshold:   getEnvAsFloat("SCALE_UP_THRESHOLD", 0.9),
		ScaleDownThreshold: getEnvAsFloat("SCALE_DOWN_THRESHOLD", 0.3),
		ScaleCheckInterval: getEnvAsDuration("SCALE_CHECK_INTERVAL", 15*time.Second),
		FileIdleTimeout:    getEnvAsDuration("FILE_IDLE_TIMEOUT", 5*time.Minute),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var result int
		if _, err := fmt.Sscanf(value, "%d", &result); err == nil {
			return result
		}
	}
	return defaultValue
}

func getEnvAsFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if result, err := strconv.ParseFloat(value, 64); err == nil {
			return result
		}
	}
	return defaultValue
}

func getEnvAsDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if result, err := time.ParseDuration(value); err == nil {
			return result
		}
	}
	return defaultValue
}
