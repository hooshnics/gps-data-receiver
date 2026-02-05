# GPS Data Receiver - Monitoring Guide

## Overview

The GPS Data Receiver includes a comprehensive monitoring system with:
- **Prometheus** for metrics collection
- **Grafana** for visualization
- **Request Tracking** for detailed request inspection
- **REST API** for programmatic access to monitoring data

## Quick Start

```bash
# Start all services including monitoring
docker-compose up -d

# Access monitoring tools
# Grafana: http://localhost:3000 (admin/admin)
# Prometheus: http://localhost:9090
# Metrics API: http://localhost:8080/metrics
```

## Grafana Dashboard

### Accessing Grafana

1. Open http://localhost:3000
2. Login with `admin` / `admin`
3. Navigate to "Dashboards" → "GPS Data Receiver Dashboard"

### Resetting Grafana password

To reset the Grafana admin password (keeps existing dashboards and data):

```bash
# Replace yournewpassword with your desired password
make grafana-reset-password NEW_PASSWORD=yournewpassword
```

Requires the Grafana container to be running (`docker-compose up -d`). Then log in at http://localhost:3000 with username `admin` and your new password.

To start with a completely fresh Grafana and the default password from `docker-compose.yml` (admin/admin), remove the Grafana volume and recreate:

```bash
docker-compose stop grafana
docker volume rm gps-data-receiver_grafana-data 2>/dev/null || true
docker-compose up -d grafana
```

### Dashboard Panels

#### 1. Overview Section
- **Requests/sec**: Real-time request rate
- **Queue Depth**: Current number of messages waiting in Redis
- **Failed Packets in DB**: Total failed packets stored in MySQL
- **Active Workers**: Number of currently processing workers

#### 2. Performance Metrics
- **HTTP Request Rate**: Requests per second by status code
- **HTTP Request Duration**: p95 and p99 latency percentiles
- **Queue Depth Over Time**: Historical queue depth trends
- **Sender Request Rate by Server**: Success/error rates per destination server

#### 3. Resource Usage
- **Memory Usage**: Allocated memory and heap usage
- **Goroutines**: Number of active goroutines
- **CPU Usage**: Derived from Go runtime metrics

#### 4. Reliability
- **Retry Attempts**: Retry distribution by server and attempt number
- **Failed Packet Trends**: Rate of permanent failures

## Request Tracking System

### Overview

Every request is tracked with detailed information including:
- Request ID and timestamp
- Client IP address
- Payload size and content
- Status history with timestamps
- Retry count and errors
- Target server and duration

### API Endpoints

#### List Requests

```bash
# Get all tracked requests
curl http://localhost:8080/monitoring/requests

# Filter by status
curl http://localhost:8080/monitoring/requests?status=queued
curl http://localhost:8080/monitoring/requests?status=failed
curl http://localhost:8080/monitoring/requests?status=retrying
```

**Available Statuses**:
- `received` - Request just received
- `queued` - Enqueued in Redis
- `processing` - Being processed by worker
- `sending` - Being sent to destination server
- `retrying` - In retry cycle
- `success` - Successfully delivered
- `failed` - Permanently failed

#### Get Request Details

```bash
# Get full details of a specific request
curl http://localhost:8080/monitoring/requests/550e8400-e29b-41d4-a716-446655440000
```

**Response includes**:
- Full payload (JSON)
- Complete status history
- Retry count and errors
- Target server
- Timing information

#### Get Statistics

```bash
# Get overall system statistics
curl http://localhost:8080/monitoring/statistics
```

**Returns**:
- Total requests received
- Total queued
- Success/failure counts
- Current status breakdown
- Requests waiting in each state

## Prometheus Metrics

### Accessing Prometheus

- **UI**: http://localhost:9090
- **Metrics Endpoint**: http://localhost:8080/metrics

### Key Metrics

#### HTTP Metrics

```promql
# Request rate
rate(gps_receiver_http_requests_total[5m])

# Request duration (p95)
histogram_quantile(0.95, rate(gps_receiver_http_request_duration_seconds_bucket[5m]))

# Active requests
gps_receiver_http_active_requests
```

#### Queue Metrics

```promql
# Queue depth
gps_receiver_queue_depth

# Enqueue rate
rate(gps_receiver_queue_enqueue_total[5m])

# Processing rate
rate(gps_receiver_queue_processed_total[5m])
```

#### Sender Metrics

```promql
# Success rate by server
rate(gps_receiver_sender_requests_total{status="success"}[5m])

# Error rate by server
rate(gps_receiver_sender_requests_total{status="error"}[5m])

# Retry rate
rate(gps_receiver_sender_retry_total[5m])
```

#### Failure Metrics

```promql
# Failed packets rate
rate(gps_receiver_failed_packets_total[5m])

# Failed packets in database
gps_receiver_failed_packets_in_db
```

#### Resource Metrics

```promql
# Memory usage
go_memstats_alloc_bytes
go_memstats_heap_inuse_bytes

# Goroutines
go_goroutines

# GC duration
rate(go_gc_duration_seconds_sum[5m])
```

## Monitoring Best Practices

### 1. Set Up Alerts

Create Prometheus alert rules for critical conditions:

```yaml
# prometheus-alerts.yml
groups:
  - name: gps_receiver_alerts
    rules:
      # High queue depth
      - alert: HighQueueDepth
        expr: gps_receiver_queue_depth > 5000
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Queue depth is high ({{ $value }})"
          description: "Queue has been above 5000 for 5 minutes"
      
      # High failure rate
      - alert: HighFailureRate
        expr: rate(gps_receiver_failed_packets_total[5m]) > 10
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "High failure rate detected"
          description: "More than 10 failures per second"
      
      # Service down
      - alert: ServiceDown
        expr: up{job="gps-receiver"} == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "GPS Receiver service is down"
      
      # High memory usage
      - alert: HighMemoryUsage
        expr: go_memstats_alloc_bytes > 500000000
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Memory usage is high ({{ $value }} bytes)"
      
      # Rate limit hits
      - alert: HighRateLimitHits
        expr: rate(gps_receiver_rate_limit_hits_total[5m]) > 100
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "High rate of rate limit hits"
```

### 2. Monitor Key Indicators

**Performance**:
- Request latency (p95, p99)
- Throughput (requests/sec)
- Queue depth

**Reliability**:
- Success rate
- Failure rate
- Retry rate

**Resources**:
- Memory usage
- CPU usage (via goroutines)
- Database connections

### 3. Dashboard Organization

Organize Grafana dashboards by:
- **Overview**: High-level metrics
- **Performance**: Latency and throughput
- **Reliability**: Errors and retries
- **Resources**: CPU, memory, goroutines
- **Business**: Request tracking and statistics

### 4. Log Correlation

Use request IDs to correlate:
- Logs (structured logging with request_id)
- Metrics (labels include request metadata)
- Traces (request tracking system)

Example:
```bash
# Find request in logs
grep "550e8400-e29b-41d4-a716-446655440000" /var/log/gps-receiver.log

# Get request details from API
curl http://localhost:8080/monitoring/requests/550e8400-e29b-41d4-a716-446655440000
```

## Troubleshooting

### High Queue Depth

**Symptoms**: `gps_receiver_queue_depth` is consistently high

**Possible Causes**:
1. Destination servers are slow or down
2. Not enough workers
3. High incoming traffic

**Solutions**:
```bash
# Check destination server status
curl -I http://destination-server.com

# Increase worker count
# Edit docker-compose.yml: WORKER_COUNT: "100"
docker-compose up -d app

# Check worker metrics
curl http://localhost:8080/metrics | grep worker
```

### High Failure Rate

**Symptoms**: `gps_receiver_failed_packets_total` increasing rapidly

**Possible Causes**:
1. Destination servers returning errors
2. Network issues
3. Invalid destination URLs

**Solutions**:
```bash
# Check failed packets in database
docker-compose exec mysql mysql -u gpsuser -pgpspass gps_receiver \
  -e "SELECT * FROM failed_packets ORDER BY created_at DESC LIMIT 10;"

# Review error messages
curl http://localhost:8080/monitoring/requests?status=failed

# Check sender metrics by server
curl http://localhost:8080/metrics | grep sender_requests_total
```

### Memory Issues

**Symptoms**: `go_memstats_alloc_bytes` growing continuously

**Possible Causes**:
1. Too many tracked requests in memory
2. Memory leak
3. Large payloads

**Solutions**:
```bash
# Check tracked request count
curl http://localhost:8080/monitoring/statistics

# Reduce tracking size (edit code)
# tracking.InitGlobalTracker(5000) // Reduce from 10000

# Monitor GC activity
curl http://localhost:8080/metrics | grep go_gc
```

### Slow Request Processing

**Symptoms**: High `gps_receiver_http_request_duration_seconds`

**Possible Causes**:
1. Queue is full
2. Redis is slow
3. Rate limiting

**Solutions**:
```bash
# Check Redis latency
docker-compose exec redis redis-cli --latency

# Check active requests
curl http://localhost:8080/metrics | grep http_active_requests

# Review rate limit hits
curl http://localhost:8080/metrics | grep rate_limit_hits
```

## Advanced Monitoring

### Custom Queries

#### Request Success Rate
```promql
sum(rate(gps_receiver_http_requests_total{status=~"2.."}[5m])) /
sum(rate(gps_receiver_http_requests_total[5m])) * 100
```

#### Average Queue Processing Time
```promql
rate(gps_receiver_queue_processing_duration_seconds_sum[5m]) /
rate(gps_receiver_queue_processed_total[5m])
```

#### Retry Success Rate
```promql
sum(rate(gps_receiver_sender_requests_total{status="success"}[5m])) /
sum(rate(gps_receiver_sender_retry_total[5m]))
```

### Exporting Data

```bash
# Export Prometheus data
curl 'http://localhost:9090/api/v1/query?query=gps_receiver_http_requests_total'

# Export request tracking data
curl http://localhost:8080/monitoring/requests > requests.json

# Export statistics
curl http://localhost:8080/monitoring/statistics > stats.json
```

## Integration with External Systems

### Sending Alerts to Slack

Configure Alertmanager:

```yaml
# alertmanager.yml
receivers:
  - name: 'slack'
    slack_configs:
      - api_url: 'YOUR_SLACK_WEBHOOK_URL'
        channel: '#gps-alerts'
        title: 'GPS Receiver Alert'
        text: '{{ range .Alerts }}{{ .Annotations.summary }}{{ end }}'
```

### Forwarding Metrics to DataDog

Use Prometheus remote write:

```yaml
# prometheus.yml
remote_write:
  - url: "https://api.datadoghq.com/api/v1/series"
    bearer_token: "YOUR_DATADOG_API_KEY"
```

## Maintenance

### Cleanup Old Data

```bash
# Clean up old tracked requests (automatic every 5 minutes)
# Keeps last 30 minutes of data

# Manual cleanup of failed packets
docker-compose exec mysql mysql -u gpsuser -pgpspass gps_receiver \
  -e "DELETE FROM failed_packets WHERE created_at < DATE_SUB(NOW(), INTERVAL 30 DAY);"
```

### Backup Monitoring Data

```bash
# Backup Prometheus data
docker-compose exec prometheus tar czf /prometheus/backup.tar.gz /prometheus

# Backup Grafana dashboards
docker-compose exec grafana tar czf /var/lib/grafana/backup.tar.gz /var/lib/grafana
```

---

For more information, see the main [README.md](README.md).

