# Scaling strategy — GPS Data Receiver

## Architecture (unchanged)

```
Device → TCP → CRC → Redis XADD → ACK → Async consumers → PG / PiStat / Hooshnics
```

Scale **around** this path. Never move ACK before durable Redis write.

## Current capacity estimate

Based on Zero-Drop design + Phase 1 load framework (fill measured numbers in `docs/performance/load-test-report.md`):

| Layer | Expected bottleneck signal | Rough guidance |
|-------|---------------------------|----------------|
| TCP accept / sessions | `ulimit`, goroutines, conntrack | 5k–10k idle/active sessions per well-tuned node is plausible |
| ACK path (CRC+XADD) | Redis RTT + AOF everysec | Target: ACK p99 ≪ device timeout; profile before optimizing |
| Raw consumers | `WORKER_COUNT`, parse CPU, destination RPS | Scale workers first; watch `gps_raw_pending_count` |
| Redis memory | Stream depth under lag + AOF | `noeviction` → errors (correct); add RAM / catch up consumers |
| Postgres | insert rate, indexes | Batch async writer; UNIQUE dedupe absorbs retransmits |
| External HTTP | PiStat / Hooshnics rate limits | Retry streams already isolate from ACK path |

**Planning envelope (pre-measurement):** design for **~200–1000 pkt/s** steady on a single ingest node with Sentinel Redis; validate with `cmd/teltonika-simulator` before claiming higher.

## Expected bottlenecks (in order)

1. **Redis primary CPU / disk** under AOF + high XADD concurrency  
2. **Consumer lag** if destinations or parse slower than ingest  
3. **Single TCP process** file descriptors / GC under 10k+ sessions  
4. **Postgres** if every record is stored synchronously under spike (mitigated by async writer)  
5. **Cross-region RTT** if Redis is remote from TCP (keep co-located)

## When to introduce what

| Technology | Introduce when | Notes |
|------------|----------------|-------|
| **More workers / larger Redis** | Pending grows but CPU has headroom | First lever; cheapest |
| **Multiple ingest nodes + LB** | TCP sessions or CPU saturate one host | L4 TCP LB; sessions sticky optional; each node uses same Redis Sentinel |
| **Redis Sentinel** | Already Phase 1 | Availability, not horizontal write scale |
| **Redis Cluster** | Single primary cannot hold memory/QPS even after vertical scale | Avoid until Streams + consumer groups ops plan is ready; hash tags required |
| **Separate ingest vs dispatch deployments** | Dispatch/HTTP retries starve parse workers | Split processes sharing Redis streams |
| **Kafka** | Multi-datacenter fan-out, long retention bus, many independent consumers beyond Redis comfort | Not needed while Redis Streams + archive cover retention; adds ops cost |
| **Partitioning (per-IMEI)** | Strict global order across nodes, or hot-IMEI isolation | Optional; current design tolerates per-frame parallelism with telemetry dedupe |
| **Object-storage archive** | Retention > Redis lifetime | ADR 0020 — does not sit on ACK path |

## Scaling patterns that preserve Zero-Drop

**Do**
- Horizontal TCP listeners behind LB, one durable Redis primary (via Sentinel)
- Scale `gps-raw-workers` consumer group members
- Archive asynchronously; replay offline

**Do not**
- ACK from memory / local disk only
- Put Kafka/HTTP on the ACK critical path
- Trim `gps:raw_incoming` aggressively while pending > 0

## Evolution vs ADR 0016

ADR 0016 foresaw Sentinel/Cluster as future options. **Sentinel is now implemented (ADR 0017).** Cluster and Kafka remain conditional per table above.

## References

- ADR 0002, 0005, 0014, 0016, 0017, 0018, 0019
- `docs/performance/load-test-report.md`
- `cmd/teltonika-simulator`, `cmd/ack-latency-bench`
