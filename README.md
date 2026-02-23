# GPS Data Receiver

A high-performance, production-ready GPS data receiver built with Go, designed to handle **>10,000 packets per minute** with robust retry logic, round-robin load balancing, and failure persistence.

## 🚀 Features

- **High Performance**: Handles 10,000+ requests per minute
- **Pure Pass-Through**: Zero validation overhead - accepts any JSON payload
- **Robust Retry Logic**: 5 retry attempts with configurable delays (5s initial, 10s subsequent)
- **Round-Robin Load Balancing**: Distributes traffic evenly across multiple destination servers
- **Failure Persistence**: Failed packets stored in MySQL for investigation and replay
- **Rate Limiting**: Per-IP rate limiting to prevent abuse
- **Worker Pool**: Configurable goroutine pool for optimal resource utilization
- **Graceful Shutdown**: Ensures in-flight packets are processed before termination
- **Comprehensive Testing**: Unit tests, integration tests, and load testing scripts
- **Production Ready**: Docker support, health checks, structured logging
- **📊 Prometheus Metrics**: Complete metrics collection for monitoring
- **📈 Grafana Dashboards**: Pre-configured dashboards for visualization
- **🔍 Request Tracking**: Detailed tracking of every request with status history
- **📋 Monitoring API**: REST endpoints to query request details and statistics

## 📋 Architecture

```
GPS Device → POST JSON → API Endpoint → Redis Queue → Worker Pool → Round-Robin → Destination Servers
                              ↓                                           ↓
                          200 OK (immediate)                    Retry Logic (5 attempts)
                                                                         ↓
                                                                 MySQL (failed packets)
```

### Components

- **Gin API**: High-performance HTTP server with middleware (rate limiting, logging, recovery)
- **Redis Streams**: Message queue for reliable, high-throughput data ingestion
- **Worker Pool**: Concurrent goroutines for processing and forwarding data
- **HTTP Sender**: Retry logic with exponential backoff and round-robin load balancing
- **MySQL**: Persistent storage for failed packets

## 🛠️ Tech Stack

- **Language**: Go 1.21+
- **Web Framework**: Gin
- **Queue**: Redis (Streams)
- **Database**: MySQL 8.0
- **Logging**: Uber Zap
- **Containerization**: Docker & Docker Compose
- **Frontend**: Vue 3 (Vite) + Tailwind CSS (optional; in `web/`)

### Frontend (Vue.js + Tailwind)

The project includes an optional Vue 3 + Vite + Tailwind CSS frontend in `web/`.

- **Install dependencies**: `make web-install` or `cd web && npm install`
- **Development** (hot reload, proxies API to backend): `make web-dev` (runs on http://localhost:5173)
- **Production build**: `make web-build` (output in `web/dist/`)

When `web/dist` exists, the Go server serves the built frontend at `/` and uses SPA fallback for client-side routing.

**Real-time updates**: Each received GPS packet is broadcast over Socket.IO (`/socket.io/`). The frontend connects automatically and displays incoming packets in real time.

**Testing and debugging the WebSocket connection**

1. **Backend**: Run the API (e.g. `make run` with Redis, or `make docker-up` for the full stack). The Go Socket.IO server only supports **WebSocket** transport (no polling).
2. **Frontend**: Run `make web-dev` and open the URL shown (e.g. http://localhost:5173 or 5174). The page shows a **Connection debug** panel (Socket ID, status, **Send test packet** button).
3. **Browser DevTools**:
   - **Console**: In dev mode the app logs `[Socket.IO]` connection and packet events.
   - **Network → WS**: Filter by "WS" to see the Socket.IO WebSocket; check for 101 (upgrade) or errors.
4. If the status stays **Disconnected**, ensure the backend is reachable at the proxy target (default `http://localhost:8080`) and that no middleware is blocking `/socket.io/` (Content-Type checks are skipped for that path).

## 📦 Installation

### Prerequisites

- Go 1.21 or higher
- Redis 6.0+
- MySQL 8.0+
- Docker & Docker Compose (optional)

### Quick Start with Docker

```bash
# Clone the repository
git clone <repository-url>
cd gps-data-receiver

# Copy and configure environment variables
cp env.example .env
# Edit .env and set DESTINATION_SERVERS

# Start all services
docker-compose up -d

# Check logs
docker-compose logs -f app

# Health check
curl http://localhost:8080/health
```

### Manual Installation

```bash
# Install dependencies
go mod download

# Configure environment variables
cp env.example .env
# Edit .env with your configuration

# Start Redis
redis-server

# Start MySQL
mysql -u root -p

# Run the application
go run cmd/server/main.go
```

## ⚙️ Configuration

All configuration is done via environment variables. See `env.example` for all available options.

### Required Configuration

```bash
DESTINATION_SERVERS=http://server1.example.com/api/gps,http://server2.example.com/api/gps
```

### Key Configuration Options

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_PORT` | 8080 | HTTP server port |
| `DESTINATION_SERVERS` | (required) | Comma-separated list of destination server URLs |
| `WORKER_COUNT` | 50 | Number of worker goroutines |
| `MAX_RETRY_ATTEMPTS` | 5 | Maximum retry attempts per packet |
| `RETRY_DELAY_FIRST` | 5s | Delay before first retry |
| `RETRY_DELAY_SUBSEQUENT` | 10s | Delay for subsequent retries |
| `RATE_LIMIT_REQUESTS_PER_SECOND` | 1000 | Rate limit per IP |
| `REDIS_HOST` | localhost | Redis host |
| `MYSQL_HOST` | localhost | MySQL host |

## 📡 API Documentation

### Receive GPS Data

**Endpoint**: `POST /api/gps/reports`

**Description**: Receives GPS data packets and queues them for processing. No validation is performed - any JSON payload is accepted.

**Request**:
```bash
curl -X POST http://localhost:8080/api/gps/reports \
  -H "Content-Type: application/json" \
  -d '{
    "device_id": "GPS001",
    "lat": 37.7749,
    "lon": -122.4194,
    "timestamp": 1234567890,
    "speed": 45.5
  }'
```

**Response** (200 OK):
```json
{
  "status": "queued",
  "message_id": "1638360000000-0",
  "request_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

**Error Responses**:
- `400 Bad Request`: Empty request body
- `429 Too Many Requests`: Rate limit exceeded
- `503 Service Unavailable`: Queue unavailable

### Health Check

**Endpoint**: `GET /health`

**Response** (200 OK):
```json
{
  "status": "healthy",
  "service": "gps-data-receiver"
}
```

### Readiness Check

**Endpoint**: `GET /ready`

**Response** (200 OK):
```json
{
  "status": "ready"
}
```

### Prometheus Metrics

**Endpoint**: `GET /metrics`

**Description**: Exposes Prometheus metrics for monitoring

**Metrics Available**:
- `gps_receiver_http_requests_total` - Total HTTP requests
- `gps_receiver_http_request_duration_seconds` - Request duration histogram
- `gps_receiver_queue_depth` - Current queue depth
- `gps_receiver_sender_requests_total` - Requests sent to destination servers
- `gps_receiver_failed_packets_total` - Total failed packets
- `gps_receiver_worker_active_count` - Active worker count
- And many more...

### Monitoring Endpoints

#### List All Requests

**Endpoint**: `GET /monitoring/requests?status=queued`

**Query Parameters**:
- `status` (optional): Filter by status (received, queued, processing, sending, retrying, success, failed)

**Response** (200 OK):
```json
{
  "requests": [
    {
      "request_id": "550e8400-e29b-41d4-a716-446655440000",
      "received_at": "2025-12-28T10:00:00Z",
      "client_ip": "192.168.1.100",
      "payload_size": 256,
      "status": "queued",
      "retry_count": 0
    }
  ],
  "count": 1
}
```

#### Get Request Details

**Endpoint**: `GET /monitoring/requests/:id`

**Response** (200 OK):
```json
{
  "request_id": "550e8400-e29b-41d4-a716-446655440000",
  "received_at": "2025-12-28T10:00:00Z",
  "client_ip": "192.168.1.100",
  "payload_size": 256,
  "payload": {"device_id": "GPS001", "lat": 37.7749, "lon": -122.4194},
  "status": "success",
  "queued_at": "2025-12-28T10:00:00.1Z",
  "processed_at": "2025-12-28T10:00:01Z",
  "completed_at": "2025-12-28T10:00:02Z",
  "target_server": "http://server1.example.com",
  "retry_count": 0,
  "duration": 2000000000,
  "status_history": [
    {
      "status": "received",
      "timestamp": "2025-12-28T10:00:00Z",
      "message": "Request received"
    },
    {
      "status": "queued",
      "timestamp": "2025-12-28T10:00:00.1Z",
      "message": "Enqueued successfully"
    },
    {
      "status": "success",
      "timestamp": "2025-12-28T10:00:02Z",
      "message": ""
    }
  ]
}
```

#### Get Statistics

**Endpoint**: `GET /monitoring/statistics`

**Response** (200 OK):
```json
{
  "total_tracked": 1500,
  "total_received": 10000,
  "total_queued": 9950,
  "total_success": 9900,
  "total_failed": 50,
  "by_status": {
    "queued": 45,
    "processing": 5,
    "success": 1400,
    "failed": 50
  },
  "queued_waiting": 45,
  "processing": 5,
  "retrying": 0
}
```

## 🧪 Testing

### Run Unit Tests

```bash
make test
# or
go test -v ./tests/unit/...
```

### Run Integration Tests

```bash
# Ensure Redis is running
docker-compose up -d redis

# Run tests
make test-integration
# or
go test -v ./tests/integration/...
```

### Run Benchmarks

```bash
make benchmark
# or
go test -bench=. -benchmem ./tests/
```

### Load Testing

```bash
# Start the application
make run

# In another terminal, run load test
make load-test

# Or with custom parameters
TARGET_URL=http://localhost:8080/api/gps/reports \
DURATION=60s \
RATE=2000 \
./scripts/load_test.sh
```

## 🏗️ Development

### Build

```bash
make build
# Output: ./gps-receiver
```

### Run Locally

```bash
make run
```

### Format Code

```bash
make fmt
```

### Lint Code

```bash
make lint
```

### Clean Build Artifacts

```bash
make clean
```

## 🐳 Docker

### Build Docker Image

```bash
docker build -t gps-receiver:latest .
```

### Run with Docker Compose

```bash
# Start all services
docker-compose up -d

# View logs
docker-compose logs -f

# Stop services
docker-compose down

# Stop and remove volumes
docker-compose down -v
```

## 🔒 Security Features

- ✅ Per-IP rate limiting (prevents DDoS)
- ✅ Request size limits (prevents memory exhaustion)
- ✅ Connection timeouts (prevents resource leaks)
- ✅ SQL injection prevention (prepared statements)
- ✅ Panic recovery middleware
- ✅ Non-root Docker container
- ✅ Environment-based secrets (never hardcoded)

## 📊 Performance

Based on benchmarks and load tests:

- **Throughput**: 10,000+ requests/minute sustained
- **Latency**: <10ms p95 (without downstream servers)
- **Load Balancer**: 6+ million operations/second
- **Memory**: ~50MB baseline, scales with worker count
- **CPU**: Low utilization with proper worker pool sizing

### Optimization Tips

1. **Worker Count**: Start with 50, adjust based on downstream server capacity
2. **Redis Connection Pool**: Default 100 is optimal for most cases
3. **HTTP Connection Pool**: Tune `MAX_CONNS_PER_HOST` based on destination server limits
4. **Rate Limiting**: Adjust based on expected traffic patterns

## 🗄️ Database Schema

### Failed Packets Table

```sql
CREATE TABLE failed_packets (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  payload TEXT NOT NULL,
  retry_count INT NOT NULL DEFAULT 0,
  last_error TEXT,
  target_server VARCHAR(255),
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_created_at (created_at)
);
```

## 🔧 Troubleshooting

### Redis Connection Issues

```bash
# Check Redis is running
docker-compose ps redis

# Check Redis logs
docker-compose logs redis

# Test Redis connection
redis-cli ping
```

### MySQL Connection Issues

```bash
# Check MySQL is running
docker-compose ps mysql

# Check MySQL logs
docker-compose logs mysql

# Test MySQL connection
mysql -h localhost -u gpsuser -p gps_receiver
```

### High Memory Usage

- Reduce `WORKER_COUNT`
- Lower `REDIS_MAX_LEN` to limit queue size
- Check for failed packet accumulation in MySQL

### Rate Limiting Issues

- Adjust `RATE_LIMIT_REQUESTS_PER_SECOND`
- Increase `RATE_LIMIT_BURST` for spike handling

## 📈 Monitoring & Observability

### Prometheus + Grafana Stack

The application includes a complete monitoring stack with Prometheus and Grafana.

#### Access Monitoring Tools

After starting with `docker-compose up -d`:

- **Grafana Dashboard**: http://localhost:3000
  - Username: `admin`
  - Password: `admin`
  - Pre-configured dashboard: "GPS Data Receiver Dashboard"
  
- **Prometheus**: http://localhost:9090
  - Query metrics directly
  - View targets and alerts

- **Application Metrics**: http://localhost:8080/metrics
  - Raw Prometheus metrics endpoint

#### Grafana Dashboard Features

The pre-configured dashboard includes:

1. **Overview Panels**:
   - Requests per second
   - Current queue depth
   - Failed packets in database
   - Active workers

2. **Performance Metrics**:
   - HTTP request rate by status
   - Request duration (p95, p99)
   - Queue depth over time
   - Sender request rate by server

3. **Resource Usage**:
   - Memory allocation
   - Heap usage
   - Goroutine count
   - CPU usage (via Go runtime metrics)

4. **Reliability Metrics**:
   - Retry attempts by server
   - Failed packet trends
   - Rate limit hits

#### Request Tracking

Every request is tracked with detailed information:

```bash
# List all queued requests
curl http://localhost:8080/monitoring/requests?status=queued

# Get details of a specific request
curl http://localhost:8080/monitoring/requests/550e8400-e29b-41d4-a716-446655440000

# Get overall statistics
curl http://localhost:8080/monitoring/statistics
```

### Available Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `gps_receiver_http_requests_total` | Counter | Total HTTP requests by method, endpoint, status |
| `gps_receiver_http_request_duration_seconds` | Histogram | HTTP request duration |
| `gps_receiver_http_active_requests` | Gauge | Currently active HTTP requests |
| `gps_receiver_queue_depth` | Gauge | Current Redis queue depth |
| `gps_receiver_queue_enqueue_total` | Counter | Total messages enqueued |
| `gps_receiver_queue_processed_total` | Counter | Total messages processed |
| `gps_receiver_sender_requests_total` | Counter | Requests sent to destination servers |
| `gps_receiver_sender_retry_total` | Counter | Retry attempts by server and attempt number |
| `gps_receiver_sender_failed_total` | Counter | Permanently failed sends |
| `gps_receiver_failed_packets_total` | Counter | Total failed packets |
| `gps_receiver_failed_packets_in_db` | Gauge | Current failed packets in database |
| `gps_receiver_worker_pool_size` | Gauge | Total worker pool size |
| `gps_receiver_worker_active_count` | Gauge | Currently active workers |
| `gps_receiver_rate_limit_hits_total` | Counter | Rate limit hits by client IP |
| `go_*` | Various | Go runtime metrics (memory, goroutines, GC, etc.) |

### Alerting (Optional)

You can configure Prometheus alerts for critical conditions:

```yaml
# Example alert rules
groups:
  - name: gps_receiver
    rules:
      - alert: HighQueueDepth
        expr: gps_receiver_queue_depth > 1000
        for: 5m
        annotations:
          summary: "Queue depth is high"
      
      - alert: HighFailureRate
        expr: rate(gps_receiver_failed_packets_total[5m]) > 10
        for: 2m
        annotations:
          summary: "High failure rate detected"
```

## 📝 License

[Your License Here]

## 🤝 Contributing

Contributions are welcome! Please open an issue or submit a pull request.

## 📧 Support

For issues and questions, please open a GitHub issue.

---

**Built with ❤️ for high-performance GPS data processing**

