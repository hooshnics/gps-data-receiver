# DateTime Format Change - MySQL Compatible

## Issue
Destination servers were receiving datetime in RFC3339 format (`2026-02-25T15:29:27Z`) which caused the error:
```
Incorrect datetime value: '2026-02-25T15:29:27Z'
```

## Solution
Changed the `date_time` field to use MySQL datetime format: `YYYY-MM-DD HH:MM:SS`

## Changes Made

### 1. Parser Data Structure
**File:** `internal/parser/parser.go`

Changed `DateTime` field from `time.Time` to `string`:
```go
type ParsedGPSData struct {
    Coordinate [2]float64    `json:"coordinate"`
    Speed      int           `json:"speed"`
    Status     int           `json:"status"`
    Directions DirectionData `json:"directions"`
    DateTime   string        `json:"date_time"` // MySQL datetime format
    IMEI       string        `json:"imei"`
}
```

### 2. DateTime Parsing Function
Updated `parseDateTime` to return MySQL-formatted string:
```go
func parseDateTime(date, timeStr string) (string, error) {
    // ... parse logic ...
    t = t.Add(3*time.Hour + 30*time.Minute) // Apply timezone offset
    
    // Format as MySQL datetime: YYYY-MM-DD HH:MM:SS
    return t.Format("2006-01-02 15:04:05"), nil
}
```

### 3. Sorting
Updated sorting to use string comparison (which works correctly for YYYY-MM-DD HH:MM:SS format):
```go
sort.Slice(processedData, func(i, j int) bool {
    return processedData[i].DateTime < processedData[j].DateTime
})
```

## Output Format Examples

### Before (RFC3339 - Not MySQL Compatible)
```json
{
  "data": [
    {
      "coordinate": [35.937947, 50.065317],
      "date_time": "2026-02-24T08:50:19Z",
      "imei": "861826074262144"
    }
  ]
}
```

### After (MySQL DATETIME Format - Compatible)
```json
{
  "data": [
    {
      "coordinate": [35.937947, 50.065317],
      "date_time": "2026-02-24 08:50:19",
      "imei": "861826074262144"
    }
  ]
}
```

## MySQL Compatibility

The format `YYYY-MM-DD HH:MM:SS` can be directly inserted into MySQL DATETIME columns:

```sql
INSERT INTO gps_data (date_time, imei, latitude, longitude) 
VALUES ('2026-02-24 08:50:19', '861826074262144', 35.937947, 50.065317);
```

No conversion needed!

## Verification

✅ All tests pass (50+ unit and integration tests)
✅ Build successful
✅ Format compatible with MySQL DATETIME columns
✅ Timezone offset properly applied (+3h 30m for Iran)
✅ Chronological sorting still works correctly

## Benefits

1. **MySQL Compatible**: Can be directly stored in DATETIME columns
2. **ISO 8601 Compliant**: Still follows international date format standard
3. **Human Readable**: Easy to read and debug
4. **Sortable**: String comparison works correctly for chronological sorting
5. **Consistent**: All timestamps use the same format throughout the system
