package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gps-data-receiver/internal/api"
	"github.com/gps-data-receiver/internal/parser"
	"github.com/gps-data-receiver/internal/sanitizer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParserIntegration_ValidSingleRecord(t *testing.T) {
	// Test with real sample data
	data := []byte(`[{"data":"+Hooshnic:V1.06,3556.27680,05003.9190,000,260224,052019,000,000,0,3,1,861826074262144"}]`)

	// Sanitize
	sanitized := sanitizer.SanitizeUTF8(data)
	assert.NotEmpty(t, sanitized)

	// Parse
	parsed, err := parser.Parse(sanitized)
	require.NoError(t, err)
	require.Len(t, parsed, 1)

	// Verify parsed data structure
	record := parsed[0]
	assert.Equal(t, "861826074262144", record.IMEI)
	assert.Equal(t, 0, record.Speed)
	assert.Equal(t, 0, record.Status)
	assert.InDelta(t, 35.937947, record.Coordinate[0], 0.000001)
	assert.InDelta(t, 50.065317, record.Coordinate[1], 0.000001)

	// Verify it can be marshaled to JSON
	jsonData, err := json.Marshal(parsed)
	require.NoError(t, err)
	assert.NotEmpty(t, jsonData)

	// Verify JSON structure
	var unmarshaled []parser.ParsedGPSData
	err = json.Unmarshal(jsonData, &unmarshaled)
	require.NoError(t, err)
	assert.Equal(t, record.IMEI, unmarshaled[0].IMEI)
}

func TestParserIntegration_MultipleRecords(t *testing.T) {
	data := []byte(`[{"data":"+Hooshnic:V1.07,3558.49300,05008.0972,000,260224,050306,000,000,1,3,1,863070043373009"},{"data":"+Hooshnic:V1.07,3558.49300,05008.0972,000,260224,050307,000,000,1,3,1,863070043373009"},{"data":"+Hooshnic:V1.07,3558.49300,05008.0972,000,260224,050302,000,000,1,3,1,863070043373009"}]`)

	sanitized := sanitizer.SanitizeUTF8(data)
	parsed, err := parser.Parse(sanitized)
	require.NoError(t, err)
	require.Len(t, parsed, 3)

	// Verify all records
	for _, record := range parsed {
		assert.Equal(t, "863070043373009", record.IMEI)
		assert.Equal(t, 1, record.Status)
	}

	// Verify chronological sorting
	for i := 1; i < len(parsed); i++ {
		assert.True(t,
			parsed[i-1].DateTime.Before(parsed[i].DateTime) || parsed[i-1].DateTime.Equal(parsed[i].DateTime),
			"Records should be sorted by DateTime")
	}
}

func TestParserIntegration_MalformedJSON(t *testing.T) {
	// Missing commas between objects
	data := []byte(`[{"data":"+Hooshnic:V1.06,3556.89710,05004.1183,000,260224,045140,008,000,1,3,1,867994064030931"}{"data":"+Hooshnic:V1.06,3557.30520,05004.0061,000,260224,044121,004,000,1,3,1,867994064030931"}]`)

	sanitized := sanitizer.SanitizeUTF8(data)
	parsed, err := parser.Parse(sanitized)
	require.NoError(t, err, "Parser should fix malformed JSON")
	require.Len(t, parsed, 2)
}

func TestParserIntegration_InvalidGPSFormat(t *testing.T) {
	data := []byte(`[{"data":"863070046119607,+MZKR:V0.0,"}]`)

	sanitized := sanitizer.SanitizeUTF8(data)
	parsed, err := parser.Parse(sanitized)
	require.NoError(t, err, "Should not error, just return empty")
	assert.Empty(t, parsed, "Invalid GPS format should result in empty array")
}

func TestParserIntegration_MixedValidInvalid(t *testing.T) {
	data := []byte(`[{"data":"+Hooshnic:V1.06,3556.27680,05003.9190,000,260224,052019,000,000,0,3,1,861826074262144"},{"data":"invalid"},{"data":"+Hooshnic:V1.06,3557.30520,05004.0061,000,260224,044121,004,000,1,3,1,867994064030931"}]`)

	sanitized := sanitizer.SanitizeUTF8(data)
	parsed, err := parser.Parse(sanitized)
	require.NoError(t, err)
	require.Len(t, parsed, 2, "Should parse valid and skip invalid")

	imeis := []string{parsed[0].IMEI, parsed[1].IMEI}
	assert.Contains(t, imeis, "861826074262144")
	assert.Contains(t, imeis, "867994064030931")
}

func TestParserIntegration_EmptyArray(t *testing.T) {
	data := []byte(`[]`)

	sanitized := sanitizer.SanitizeUTF8(data)
	parsed, err := parser.Parse(sanitized)
	require.NoError(t, err)
	assert.Empty(t, parsed)
}

func TestAPIIntegration_ReceiveAndParseGPSData(t *testing.T) {
	gin.SetMode(gin.TestMode)

	redisQueue := setupTestRedis(t)
	defer redisQueue.Close()

	handler := api.NewHandler(redisQueue, 0, nil)

	router := gin.New()
	router.Use(api.RequestIDMiddleware())
	router.POST("/api/gps/reports", handler.ReceiveGPSData)

	// Test with real GPS data format
	payload := []byte(`[{"data":"+Hooshnic:V1.06,3556.27680,05003.9190,000,260224,052019,000,000,0,3,1,861826074262144"}]`)

	// Send request
	req := httptest.NewRequest("POST", "/api/gps/reports", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "queued", response["status"])
	assert.NotEmpty(t, response["message_id"])

	// Verify data is in Redis (raw, not parsed yet)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	length, err := redisQueue.GetClient().XLen(ctx, redisQueue.GetStreamName()).Result()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, length, int64(1))
}

func TestSanitizerIntegration_UTF8Cleaning(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected func(t *testing.T, result []byte)
	}{
		{
			name:  "valid UTF-8",
			input: []byte(`[{"data":"test"}]`),
			expected: func(t *testing.T, result []byte) {
				assert.Equal(t, `[{"data":"test"}]`, string(result))
			},
		},
		{
			name:  "with control characters",
			input: []byte{0x00, '[', '{', '"', 'd', 'a', 't', 'a', '"', ':', '"', 't', 'e', 's', 't', '"', '}', ']', 0x7F},
			expected: func(t *testing.T, result []byte) {
				assert.Equal(t, `[{"data":"test"}]`, string(result))
				assert.NotContains(t, string(result), "\x00")
				assert.NotContains(t, string(result), "\x7F")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizer.SanitizeUTF8(tt.input)
			tt.expected(t, result)
		})
	}
}

func BenchmarkParserIntegration_EndToEnd(b *testing.B) {
	data := []byte(`[{"data":"+Hooshnic:V1.06,3556.27680,05003.9190,000,260224,052019,000,000,0,3,1,861826074262144"}]`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sanitized := sanitizer.SanitizeUTF8(data)
		parsed, _ := parser.Parse(sanitized)
		_, _ = json.Marshal(parsed)
	}
}
