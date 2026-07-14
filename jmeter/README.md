# JMeter Load Testing

Apache JMeter test plans for load testing the GPS Data Receiver API.

## Prerequisites

1. **Running server** — start the stack before testing:

   ```bash
   make docker-up
   # or
   make run
   ```

2. **JMeter** — install Apache JMeter 5.6+:

   ```bash
   # macOS
   brew install jmeter

   # Debian/Ubuntu
   sudo apt install jmeter
   ```

## Quick Start

```bash
# Ingest throughput test (generic JSON, default 1000 req/s)
make jmeter-ingest

# Realistic Hooshnic device payloads from CSV
make jmeter-hooshnic

# Multi-record Hooshnic batches (3 records + empty object, trailing dots)
make jmeter-hooshnic-batch

# Read/query API endpoints (lower concurrency)
make jmeter-read
```

## Test Plans

| Plan | File | Target | Default load |
|------|------|--------|--------------|
| **Ingest** | `plans/gps-ingest-load.jmx` | `POST /api/gps/reports` | 100 threads, 1000 req/s |
| **Hooshnic** | `plans/gps-ingest-hooshnic.jmx` | `POST /api/gps/reports` (device format) | 50 threads, 500 req/s |
| **Hooshnic Batch** | `plans/gps-ingest-hooshnic-batch.jmx` | `POST /api/gps/reports` (multi-record files) | 50 threads, 500 req/s |
| **Read APIs** | `plans/gps-read-apis.jmx` | `GET /health`, `/api/gps/records`, `/path`, etc. | 10 threads |

### Ingest Load Test

Primary throughput test for the hot path. Sends generic JSON payloads with unique `device_id` and `timestamp` per request.

- Health check runs first (stops test if server is down)
- Asserts HTTP 200 and `"status":"queued"` in response
- Uses keep-alive connections and a Constant Throughput Timer

### Hooshnic Device Test

End-to-end pipeline test using real device payload format. Reads device data from `data/hooshnic-devices.csv` (IMEI, NMEA coordinates, timestamps).

### Hooshnic Batch Test

Load test using your real multi-record device payloads from `jmeter/data/`:

| File | Description |
|------|-------------|
| `hooshnic-batch-3records.json` | 3 GPS records plus an empty `{}` object |
| `hooshnic-single-trailing-dots.body` | Single record with trailing `.` artifacts |
| `hooshnic-payload-files.csv` | Rotates between the two payloads above |

By default the plan alternates between both payload shapes. Lock to one file with `PAYLOAD_FILE`:

```bash
# Rotate both payload shapes (default)
make jmeter-hooshnic-batch

# Only the 3-record batch
PAYLOAD_FILE=hooshnic-batch-3records.json make jmeter-hooshnic-batch

# Only trailing-dot artifact payload
PAYLOAD_FILE=hooshnic-single-trailing-dots.body make jmeter-hooshnic-batch
```

Quick smoke test before load testing:

```bash
make smoke-test-hooshnic
```

Go load tester with the same payloads:

```bash
make load-test-hooshnic-batch
make load-test-hooshnic-artifacts
make stress-test-hooshnic
```

### Read APIs Test

Tests query endpoints that hit PostgreSQL. Requires seeded data for meaningful results. Set `QUERY_DATE` and `QUERY_IMEI` to match data in your database.

## Configuration

All plans accept JMeter properties via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `HOST` | `localhost` | Server hostname |
| `PORT` | `8080` | Server port |
| `PROTOCOL` | `http` | `http` or `https` |
| `THREADS` | varies | Concurrent virtual users |
| `RAMP_UP` | `30` | Ramp-up time (seconds) |
| `DURATION` | `60` | Test duration (seconds) |
| `RATE` | `1000` / `500` | Target req/s (ingest plans) |
| `QUERY_DATE` | `2026-02-24` | Date for read API queries |
| `QUERY_IMEI` | `861826074262144` | IMEI for read API queries |
| `PAYLOAD_FILE` | (empty) | Fixed payload file in `jmeter/data/` for hooshnic-batch |
| `PAYLOAD_CSV` | `hooshnic-payload-files.csv` | Payload rotation list for hooshnic-batch |

### Examples

```bash
# Local high-intensity ingest test
HOST=localhost PORT=8080 THREADS=200 RAMP_UP=60 DURATION=120 RATE=5000 \
  ./jmeter/scripts/run-jmeter.sh ingest

# Production (use with caution — coordinate with ops first)
PROTOCOL=https HOST=api.example.com PORT=443 RATE=500 DURATION=30 \
  ./jmeter/scripts/run-jmeter.sh ingest

# Read APIs against seeded data
QUERY_DATE=2026-07-11 QUERY_IMEI=861826074262144 THREADS=20 \
  ./jmeter/scripts/run-jmeter.sh read

# Open test plan in JMeter GUI for editing
./jmeter/scripts/run-jmeter.sh ingest --gui
```

## Results

Each run writes timestamped output under `jmeter/results/`:

```
jmeter/results/ingest-20260711-143022/
  results.jtl       # raw sample data (CSV)
  jmeter.log        # JMeter execution log
  report/
    index.html      # HTML dashboard (latency, throughput, errors)
```

Open the HTML report:

```bash
open jmeter/results/ingest-*/report/index.html
```

## Rate Limiting Notes

The server applies **per-IP rate limiting** (`RATE_LIMIT_REQUESTS_PER_SECOND`, default 1000–15000 depending on env). A single JMeter instance sends from one IP, so you may see `429 Too Many Requests` at high rates.

Mitigations:

1. **Raise limits** in a dedicated load-test `.env`:

   ```env
   RATE_LIMIT_REQUESTS_PER_SECOND=20000
   RATE_LIMIT_BURST=30000
   ```

2. **Use distributed JMeter** — run slaves on multiple hosts/IPs.

3. **Reduce `RATE`** to stay under your configured limit.

## Monitoring During Tests

While a test runs, watch server metrics:

```bash
# Prometheus metrics
curl -s http://localhost:8080/metrics | grep gps_receiver

# Grafana dashboard (if docker-compose is up)
open http://localhost:3000
```

Key metrics:

- `gps_receiver_http_requests_total` — request count by status
- `gps_receiver_http_request_duration_seconds` — latency histogram
- `gps_receiver_queue_depth` — Redis queue depth
- `gps_receiver_rate_limit_hits_total` — rate limit rejections

## Comparison with Built-in Load Tester

This project also includes a Go-based load tester optimized for 10K+ req/s:

```bash
make load-test-high   # 10K req/s, 60s
```

Use JMeter when you need:

- Visual test plan editing (GUI)
- HTML reports with charts
- Complex scenarios (CSV data, multiple endpoints, assertions)
- Distributed load from multiple machines

Use the Go load tester for maximum single-machine throughput benchmarking.

## Directory Layout

```
jmeter/
  README.md
  plans/
    gps-ingest-load.jmx      # generic JSON throughput
    gps-ingest-hooshnic.jmx  # realistic device payloads
    gps-ingest-hooshnic-batch.jmx  # multi-record payloads from data files
    gps-read-apis.jmx        # query endpoints
  data/
    generic-payload.json     # sample payload reference
    hooshnic-devices.csv     # device data for CSV-driven tests
    hooshnic-batch-3records.json
    hooshnic-single-trailing-dots.body
    hooshnic-payload-files.csv
  scripts/
    run-jmeter.sh            # CLI runner
  results/                   # test output (gitignored except .gitkeep)
```
