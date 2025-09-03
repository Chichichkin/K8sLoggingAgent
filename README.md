# Kubernetes Node Logging Agent

A high-performance, adaptive logging agent for Kubernetes that collects logs from all nodes and forwards them to Loki. Built in Go with automatic worker scaling, file discovery, and intelligent log processing.

## Features

- **Node-level log collection**: Runs as a DaemonSet on every Kubernetes node
- **Adaptive worker scaling**: Automatically scales workers based on queue usage
- **Smart file discovery**: Automatically discovers and monitors new log files
- **Efficient batching**: Configurable batch size and timeout for optimal performance
- **Loki integration**: Direct integration with Grafana Loki for centralized logging
- **Metrics and monitoring**: Built-in metrics for monitoring agent performance
- **File idle timeout**: Stops tailing inactive files to save resources
- **Unique file tracking**: Only counts unique files discovered and processed

## Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Kubernetes    │    │   Log Daemon     │    │      Loki      │
│   Node Logs     │───▶│     Service      │───▶│   (Storage)    │
│                 │    │                  │    │                 │
└─────────────────┘    └──────────────────┘    └─────────────────┘
                              │
                              ▼
                       ┌──────────────────┐
                       │  Batch Processor │
                       │                  │
                       └──────────────────┘
```

### Components and relationships

- Log Daemon Service: scans and reads logs, forms `LogEntry` and send them to `BatchProcessor`.
- Batch Processor: creates `LogEntry` batches by size/time and send them to `LogSender`.
- Loki Sender: an implementation of `LogSender` interface, sends batches to Loki (`/loki/api/v1/push`).

Log Daemon → BatchProcessor → LogSender(Loki).

## Prerequisites

- Kubernetes cluster (tested with minikube)
- Docker
- Go 1.24+ (for building)
- kubectl configured

## Quick Start

### 1. Start Loki and Grafana (required)

Follow the official Docker Compose instructions to run Loki and Grafana locally. Reference: [Install with Docker Compose](https://grafana.com/docs/loki/latest/setup/install/docker/#install-with-docker-compose).

Example:

```bash
mkdir -p loki && cd loki
wget https://raw.githubusercontent.com/grafana/loki/v3.4.1/production/docker-compose.yaml -O docker-compose.yaml
docker compose -f docker-compose.yaml up -d
# Loki ready check: http://localhost:3100/ready
# Grafana: http://localhost:3000 (default credentials may be required)
```

Ensure Loki is reachable on your host at `http://localhost:3100` or note the URL you will use in the DaemonSet env `LOKI_URL`.
Since Loki and grafana is not part of the minikube you would have to use your actual IP.

```bash
# run to get your IP to use it for LOKI_URL
hostname -I | awk '{print $1}'
```


### 2. Point Docker to Minikube before building

Run this in the same shell so images build into the Minikube Docker daemon:

```bash
eval $(minikube docker-env)
```

### 3. Start minikube and Enable Registry

```bash
# Start minikube
minikube start --driver=docker

# Enable registry addon
minikube addons enable registry

# Build the agent image (now built inside Minikube's Docker)
docker build -t localhost:5000/node-logging-agent:latest .
docker push localhost:5000/node-logging-agent:latest
```

### 4. Configure Daemon Loki URL

Update `deploy/daemonset.yaml` to set the correct `LOKI_URL` that points to your Loki endpoint. If Loki runs locally via Docker Compose on the same host as your `kubectl`, `http://localhost:3100` works, otherwise set a resolvable host/IP.

### 5. Deploy the Logging Agent

```bash
# Apply RBAC configuration
kubectl apply -f deploy/rbac.yaml

# Apply DaemonSet
kubectl apply -f deploy/daemonset.yaml

# Check deployment status
kubectl get daemonset -n kube-system
kubectl get pods -n kube-system -l app=node-logging-agent
```

### 6. Verify Operation

```bash
# Check agent logs
kubectl logs -n kube-system -l app=node-logging-agent

# Check if Loki receives data
curl -s "http://localhost:3100/loki/api/v1/labels" | jq

# Check specific labels
curl -s "http://localhost:3100/loki/api/v1/label/pod/values" | jq
curl -s "http://localhost:3100/loki/api/v1/label/namespace/values" | jq

# Open Grafana to view logs
open http://localhost:3000  # or xdg-open on Linux
```

### 7. Generate log noise for testing

Deploy the provided `counter-pod` to generate steady logs that the agent can ship to Loki:

```bash
kubectl apply -f deploy/counter-pod.yaml
kubectl logs -f pod/counter
```

## Configuration

### Environment Variables

| Variable               | Default                 | Description                                |
|------------------------|-------------------------|--------------------------------------------|
| `LOKI_URL`             | `http://localhost:3100` | Loki endpoint URL                          |
| `LOG_PATH`             | `/var/log/pods`         | Root path for log discovery                |
| `BATCH_SIZE`           | `1000`                  | Number of log entries per batch            |
| `BATCH_TIMEOUT`        | `2s`                    | Timeout for batch processing               |
| `MIN_WORKERS`          | `2`                     | Minimum number of worker goroutines        |
| `MAX_WORKERS`          | `10`                    | Maximum number of worker goroutines        |
| `QUEUE_SIZE`           | `100`                   | File queue capacity                        |
| `SCAN_INTERVAL`        | `10s`                   | Log file discovery interval                |
| `SCALE_UP_THRESHOLD`   | `0.9`                   | Queue usage threshold for scaling up       |
| `SCALE_DOWN_THRESHOLD` | `0.3`                   | Queue usage threshold for scaling down     |
| `SCALE_CHECK_INTERVAL` | `10s`                   | Worker scaling check interval              |
| `FILE_IDLE_TIMEOUT`    | `5m`                    | Stop tailing files after this idle period  |
| `FILE_BUFFER_SIZE`     | `1000`                  | Buffer size for file reading               |
| `MAX_RETRIES`          | `3`                     | Maximum retry attempts for failed requests |

### DaemonSet Configuration

The agent runs with:
- **Security Context**: Runs as root (UID 0) with necessary capabilities for log access
- **Volume Mounts**: 
  - `/var/log/pods` - Kubernetes pod logs
  - `/var/lib/docker/containers` - Docker container logs
  - `/var/log` - System logs
- **Resource Limits**: Configurable CPU and memory limits

## Monitoring

### Built-in Metrics

The agent provides the following metrics:

- **Files Discovered**: Number of unique log files found
- **Files Processed**: Number of time files successfully read
- **Files Failed**: Number of time when files read failed
- **Queue Usage**: Current file queue utilization
- **Workers Active**: Number of active worker goroutines
- **Workers Busy**: Number of busy workers
- **Scaling Operations**: Scale up/down operations count

### Viewing Metrics

```bash
# Check agent logs for metrics
kubectl logs -n kube-system -l app=node-logging-agent | grep "Metrics:"

# Example output:
# Metrics: workers active/ max workers =2/10, workers busy=0, queue status=0/100 (0%), files=0/0, scale_up=0, scale_down=0
```

## Troubleshooting

### Common Issues

1. #### Permission Denied Errors
   ```
   Error accessing path /var/log/pods: open /var/log/pods: permission denied
   ```
   **Solution**: Ensure the agent runs as root (UID 0) with proper capabilities

2.  #### Connection Refused to Loki
   ```
   dial tcp [::1]:3100: connect: connection refused
   ```
   **Solution**: Use host IP instead of localhost in LOKI_URL

3. #### File Queue Full
   ```
   File queue full (100/100), skipping /var/log/pods/...
   ```
   **Solution**: Increase QUEUE_SIZE or SCAN_INTERVAL

4. #### Batch Processing Failures
   ```
   Failed to send batch: failed to send batch after 3 attempts
   ```
   **Solution**: Check Loki connectivity and increase MAX_RETRIES

### Debug Mode

Enable verbose logging by checking agent logs:

```bash
kubectl logs -n kube-system -l app=node-logging-agent -f
```
