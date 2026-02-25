package parser

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_ValidSingleRecord(t *testing.T) {
	// Line 1 from sample-data.txt
	data := []byte(`[{"data":"+Hooshnic:V1.06,3556.27680,05003.9190,000,260224,052019,000,000,0,3,1,861826074262144"}]`)

	result, err := Parse(data)
	require.NoError(t, err)
	require.Len(t, result, 1)

	record := result[0]
	assert.Equal(t, "861826074262144", record.IMEI)
	assert.Equal(t, 0, record.Speed)
	assert.Equal(t, 0, record.Status)
	assert.Equal(t, 3, record.Directions.EW)
	assert.Equal(t, 1, record.Directions.NS)

	// Check coordinate conversion
	assert.InDelta(t, 35.937947, record.Coordinate[0], 0.000001) // Latitude
	assert.InDelta(t, 50.065317, record.Coordinate[1], 0.000001) // Longitude

	// Check date/time with timezone offset (+3h 30m)
	expectedTime := time.Date(2026, 2, 24, 8, 50, 19, 0, time.UTC)
	assert.Equal(t, expectedTime, record.DateTime)
}

func TestParse_MultipleRecords(t *testing.T) {
	// Line 4 from sample-data.txt
	data := []byte(`[{"data":"+Hooshnic:V1.07,3558.49300,05008.0972,000,260224,050306,000,000,1,3,1,863070043373009"},{"data":"+Hooshnic:V1.07,3558.49300,05008.0972,000,260224,050307,000,000,1,3,1,863070043373009"},{"data":"+Hooshnic:V1.07,3558.49300,05008.0972,000,260224,050302,000,000,1,3,1,863070043373009"}]`)

	result, err := Parse(data)
	require.NoError(t, err)
	require.Len(t, result, 3)

	// Check all records have the same IMEI
	for _, record := range result {
		assert.Equal(t, "863070043373009", record.IMEI)
		assert.Equal(t, 1, record.Status)
	}

	// Check they are sorted by date_time
	for i := 1; i < len(result); i++ {
		assert.True(t, result[i-1].DateTime.Before(result[i].DateTime) || result[i-1].DateTime.Equal(result[i].DateTime))
	}
}

func TestParse_MalformedJSONWithMissingCommas(t *testing.T) {
	// Line 7 from sample-data.txt - missing commas between objects
	data := []byte(`[{"data":"+Hooshnic:V1.06,3556.89710,05004.1183,000,260224,045140,008,000,1,3,1,867994064030931"}{"data":"+Hooshnic:V1.06,3557.30520,05004.0061,000,260224,044121,004,000,1,3,1,867994064030931"}]`)

	result, err := Parse(data)
	require.NoError(t, err, "Parser should fix missing commas")
	require.Len(t, result, 2)

	assert.Equal(t, "867994064030931", result[0].IMEI)
	assert.Equal(t, "867994064030931", result[1].IMEI)
}

func TestParse_TrailingDots(t *testing.T) {
	// Simulating trailing dots like in sample-data.txt
	data := []byte(`[{"data":"+Hooshnic:V1.06,3556.27680,05003.9190,000,260224,052019,000,000,0,3,1,861826074262144"}]......`)

	result, err := Parse(data)
	require.NoError(t, err, "Parser should handle trailing dots")
	require.Len(t, result, 1)

	assert.Equal(t, "861826074262144", result[0].IMEI)
}

func TestParse_EmptyObjects(t *testing.T) {
	// Simulating empty objects like in sample-data.txt line 9
	data := []byte(`[{"data":"+Hooshnic:V1.07,3557.49830,05006.6209,000,260131,091810,002,006,1,3,1,867717034651472"},{}]`)

	result, err := Parse(data)
	require.NoError(t, err, "Parser should handle empty objects")
	require.Len(t, result, 1)

	assert.Equal(t, "867717034651472", result[0].IMEI)
}

func TestParse_EmptyArray(t *testing.T) {
	// Line 11 from sample-data.txt
	data := []byte(`[]......`)

	result, err := Parse(data)
	require.NoError(t, err)
	assert.Empty(t, result, "Empty array should return empty result")
}

func TestParse_InvalidGPSFormat(t *testing.T) {
	// Line 13 from sample-data.txt - invalid format
	data := []byte(`[{"data":"863070046119607,+MZKR:V0.0,"}]`)

	result, err := Parse(data)
	require.NoError(t, err, "Should not error, just skip invalid records")
	assert.Empty(t, result, "Invalid GPS format should be skipped")
}

func TestParse_MixedValidAndInvalid(t *testing.T) {
	data := []byte(`[{"data":"+Hooshnic:V1.06,3556.27680,05003.9190,000,260224,052019,000,000,0,3,1,861826074262144"},{"data":"invalid"},{"data":"+Hooshnic:V1.06,3557.30520,05004.0061,000,260224,044121,004,000,1,3,1,867994064030931"}]`)

	result, err := Parse(data)
	require.NoError(t, err)
	require.Len(t, result, 2, "Should parse valid records and skip invalid")

	// Records are sorted by date_time, so the order might differ from input order
	imeis := []string{result[0].IMEI, result[1].IMEI}
	assert.Contains(t, imeis, "861826074262144")
	assert.Contains(t, imeis, "867994064030931")
}

func TestParse_EmptyInput(t *testing.T) {
	_, err := Parse([]byte{})
	assert.Error(t, err, "Empty input should return error")
}

func TestParse_InvalidJSON(t *testing.T) {
	data := []byte(`{not valid json}`)

	_, err := Parse(data)
	assert.Error(t, err, "Invalid JSON should return error")
}

func TestConvertNMEAToDecimalDegrees(t *testing.T) {
	tests := []struct {
		name        string
		nmeaLat     string
		nmeaLon     string
		expectedLat float64
		expectedLon float64
	}{
		{
			name:        "sample data coordinates",
			nmeaLat:     "3556.27680",
			nmeaLon:     "05003.9190",
			expectedLat: 35.937947,
			expectedLon: 50.065317,
		},
		{
			name:        "another sample",
			nmeaLat:     "3558.49300",
			nmeaLon:     "05008.0972",
			expectedLat: 35.974883,
			expectedLon: 50.134953,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lat, lon, err := convertNMEAToDecimalDegrees(tt.nmeaLat, tt.nmeaLon)
			require.NoError(t, err)
			assert.InDelta(t, tt.expectedLat, lat, 0.000001)
			assert.InDelta(t, tt.expectedLon, lon, 0.000001)
		})
	}
}

func TestNMEAToDecimalDegrees(t *testing.T) {
	tests := []struct {
		name     string
		nmea     float64
		expected float64
	}{
		{
			name:     "latitude",
			nmea:     3556.27680,
			expected: 35.937947,
		},
		{
			name:     "longitude",
			nmea:     5003.9190,
			expected: 50.065317,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := nmeaToDecimalDegrees(tt.nmea)
			assert.InDelta(t, tt.expected, result, 0.000001)
		})
	}
}

func TestParseDateTime(t *testing.T) {
	tests := []struct {
		name     string
		date     string
		time     string
		expected time.Time
	}{
		{
			name:     "sample date",
			date:     "260224",
			time:     "052019",
			expected: time.Date(2026, 2, 24, 8, 50, 19, 0, time.UTC), // +3h 30m offset
		},
		{
			name:     "another date",
			date:     "260131",
			time:     "091810",
			expected: time.Date(2026, 1, 31, 12, 48, 10, 0, time.UTC), // +3h 30m offset
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseDateTime(tt.date, tt.time)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsValidDataFormat(t *testing.T) {
	tests := []struct {
		name  string
		data  string
		valid bool
	}{
		{
			name:  "valid format V1.06",
			data:  "+Hooshnic:V1.06,3556.27680,05003.9190,000,260224,052019,000,000,0,3,1,861826074262144",
			valid: true,
		},
		{
			name:  "valid format V1.07",
			data:  "+Hooshnic:V1.07,3558.49300,05008.0972,000,260224,050306,000,000,1,3,1,863070043373009",
			valid: true,
		},
		{
			name:  "invalid - missing fields",
			data:  "+Hooshnic:V1.06,3556.27680,05003.9190",
			valid: false,
		},
		{
			name:  "invalid - wrong format",
			data:  "863070046119607,+MZKR:V0.0,",
			valid: false,
		},
		{
			name:  "invalid - empty",
			data:  "",
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidDataFormat(tt.data)
			assert.Equal(t, tt.valid, result)
		})
	}
}

func TestDecodeJSONData_CleaningOperations(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		validate func(t *testing.T, result []DataItem, err error)
	}{
		{
			name:  "trailing dots",
			input: `[{"data":"test"}]......`,
			validate: func(t *testing.T, result []DataItem, err error) {
				require.NoError(t, err)
				require.Len(t, result, 1)
				assert.Equal(t, "test", result[0].Data)
			},
		},
		{
			name:  "missing commas between objects",
			input: `[{"data":"test1"}{"data":"test2"}]`,
			validate: func(t *testing.T, result []DataItem, err error) {
				require.NoError(t, err)
				require.Len(t, result, 2)
				assert.Equal(t, "test1", result[0].Data)
				assert.Equal(t, "test2", result[1].Data)
			},
		},
		{
			name:  "empty objects",
			input: `[{"data":"test"},{}]`,
			validate: func(t *testing.T, result []DataItem, err error) {
				require.NoError(t, err)
				require.Len(t, result, 1)
				assert.Equal(t, "test", result[0].Data)
			},
		},
		{
			name:  "trailing commas before bracket",
			input: `[{"data":"test"},]`,
			validate: func(t *testing.T, result []DataItem, err error) {
				require.NoError(t, err)
				require.Len(t, result, 1)
				assert.Equal(t, "test", result[0].Data)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := decodeJSONData(tt.input)
			tt.validate(t, result, err)
		})
	}
}

func BenchmarkParse_SingleRecord(b *testing.B) {
	data := []byte(`[{"data":"+Hooshnic:V1.06,3556.27680,05003.9190,000,260224,052019,000,000,0,3,1,861826074262144"}]`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Parse(data)
	}
}

func BenchmarkParse_MultipleRecords(b *testing.B) {
	data := []byte(`[{"data":"+Hooshnic:V1.07,3558.49300,05008.0972,000,260224,050306,000,000,1,3,1,863070043373009"},{"data":"+Hooshnic:V1.07,3558.49300,05008.0972,000,260224,050307,000,000,1,3,1,863070043373009"},{"data":"+Hooshnic:V1.07,3558.49300,05008.0972,000,260224,050302,000,000,1,3,1,863070043373009"}]`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Parse(data)
	}
}
