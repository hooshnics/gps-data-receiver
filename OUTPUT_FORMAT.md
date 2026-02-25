# GPS Data Output Format

## Output Structure

The parsed GPS data is now wrapped in a `data` field before being sent to destination servers.

### Format

```json
{
  "data": [
    {
      "coordinate": [35.937947, 50.065317],
      "speed": 0,
      "status": 0,
      "directions": {
        "ew": 3,
        "ns": 1
      },
      "date_time": "2026-02-24T08:50:19Z",
      "imei": "861826074262144"
    }
  ]
}
```

## Single Record Example

**Input (raw):**
```json
[{"data":"+Hooshnic:V1.06,3556.27680,05003.9190,000,260224,052019,000,000,0,3,1,861826074262144"}]
```

**Output (sent to destination servers):**
```json
{
  "data": [
    {
      "coordinate": [35.937947, 50.065317],
      "speed": 0,
      "status": 0,
      "directions": {
        "ew": 3,
        "ns": 1
      },
      "date_time": "2026-02-24T08:50:19Z",
      "imei": "861826074262144"
    }
  ]
}
```

## Multiple Records Example

**Input (raw):**
```json
[
  {"data":"+Hooshnic:V1.07,3558.49300,05008.0972,000,260224,050306,000,000,1,3,1,863070043373009"},
  {"data":"+Hooshnic:V1.07,3558.49300,05008.0972,000,260224,050307,000,000,1,3,1,863070043373009"}
]
```

**Output (sent to destination servers):**
```json
{
  "data": [
    {
      "coordinate": [35.974883, 50.134953],
      "speed": 0,
      "status": 1,
      "directions": {
        "ew": 3,
        "ns": 1
      },
      "date_time": "2026-02-24T08:33:36Z",
      "imei": "863070043373009"
    },
    {
      "coordinate": [35.974883, 50.134953],
      "speed": 0,
      "status": 1,
      "directions": {
        "ew": 3,
        "ns": 1
      },
      "date_time": "2026-02-24T08:33:37Z",
      "imei": "863070043373009"
    }
  ]
}
```

## Key Points

1. **Top-level `data` field**: All parsed GPS records are wrapped in a `data` array
2. **Array format**: Even single records are wrapped in an array for consistency
3. **Sorted by time**: Records are sorted chronologically by `date_time`
4. **Structured data**: Clean, typed JSON with proper coordinate conversion
5. **Timezone aware**: Timestamps include Iran timezone offset (+3h 30m)

## Processing Pipeline

```
Raw GPS Data
    ↓
Sanitize UTF-8
    ↓
Parse & Validate
    ↓
Convert Coordinates (NMEA → Decimal)
    ↓
Parse DateTime (with timezone)
    ↓
Sort by DateTime
    ↓
Wrap in {"data": [...]}
    ↓
Send to Destination Servers
```

## Destination Server Integration

Destination servers should expect:
- Top-level object with `data` field
- `data` is always an array (even for single records)
- Array can be empty `{"data": []}` if no valid records
- Each record has the structure shown above
