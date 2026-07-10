package parser

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	gojson "github.com/goccy/go-json"
	"github.com/gps-data-receiver/pkg/logger"
	"go.uber.org/zap"
)

// InvalidRecord holds raw payload that could not be parsed into GPS data.
type InvalidRecord struct {
	RawData string `json:"raw_data"`
	Reason  string `json:"reason"`
}

// ParseResult contains successfully parsed records and any invalid payloads.
type ParseResult struct {
	Records []ParsedGPSData
	Invalid []InvalidRecord
}

// ParsedGPSData represents a parsed GPS record
type ParsedGPSData struct {
	Coordinate [2]float64    `json:"coordinate"` // [lat, lon]
	Speed      int           `json:"speed"`
	Status     int           `json:"status"`
	Directions DirectionData `json:"directions"`
	DateTime   string        `json:"date_time"` // MySQL datetime format: YYYY-MM-DD HH:MM:SS
	IMEI       string        `json:"imei"`
	RawData    string        `json:"-"` // Original device payload; not forwarded to destination servers
}

// DirectionData represents direction indicators
type DirectionData struct {
	EW int `json:"ew"` // East/West
	NS int `json:"ns"` // North/South
}

// DataItem represents a raw GPS data item from JSON
type DataItem struct {
	Data string `json:"data"`
}

// GPS data format validation regex
// Pattern: +Hooshnic:V{version},{lat},{lon},{field3},{date},{time},{speed},{field7},{status},{ew},{ns},{imei}
var gpsDataPattern = regexp.MustCompile(`^\+[^:]+:[^,]+(,[^,]+){12}$`)

var (
	dataObjectStartPattern = regexp.MustCompile(`\{"data"\s*:\s*"`)
	trailingCommaPattern   = regexp.MustCompile(`\},\s*\]`)
)

// Parse parses GPS data from JSON byte array into structured format.
func Parse(data []byte) (ParseResult, error) {
	if len(data) == 0 {
		return ParseResult{}, fmt.Errorf("input data cannot be empty")
	}

	decodedData, decodeInvalid, err := decodeJSONData(string(data))
	if err != nil {
		return ParseResult{}, fmt.Errorf("failed to decode JSON data: %w", err)
	}

	if len(decodedData) == 0 && len(decodeInvalid) == 0 {
		return ParseResult{}, nil
	}

	processedData, processInvalid := processDataItems(decodedData)
	invalid := append(decodeInvalid, processInvalid...)

	sort.Slice(processedData, func(i, j int) bool {
		return processedData[i].DateTime < processedData[j].DateTime
	})

	return ParseResult{
		Records: processedData,
		Invalid: invalid,
	}, nil
}

// preprocessJSON applies common cleanup for device payload artifacts.
func preprocessJSON(jsonData string) string {
	trimmedData := strings.TrimRight(jsonData, ".")
	trimmedData = strings.ReplaceAll(trimmedData, "}{", "},{")
	trimmedData = strings.ReplaceAll(trimmedData, ",{}", "")
	trimmedData = trailingCommaPattern.ReplaceAllString(trimmedData, "}]")
	return strings.TrimRight(trimmedData, ",")
}

// decodeJSONData decodes and cleans the JSON data.
// When the full array cannot be decoded, individual {"data":"..."} objects are
// extracted so only malformed records are dropped instead of the entire group.
func decodeJSONData(jsonData string) ([]DataItem, []InvalidRecord, error) {
	trimmedData := preprocessJSON(jsonData)

	var decodedData []DataItem
	if err := gojson.Unmarshal([]byte(trimmedData), &decodedData); err != nil {
		logger.Debug("Bulk JSON decode failed, extracting individual records",
			zap.Error(err),
			zap.String("cleaned_data", trimmedData))

		decodedData, extractionInvalid := extractDataItems(trimmedData)
		if len(decodedData) == 0 && len(extractionInvalid) == 0 {
			return nil, nil, fmt.Errorf("invalid JSON format: %w", err)
		}
		return decodedData, extractionInvalid, nil
	}

	return decodedData, nil, nil
}

// extractDataItems recovers data records from malformed JSON arrays.
func extractDataItems(jsonData string) ([]DataItem, []InvalidRecord) {
	matches := dataObjectStartPattern.FindAllStringIndex(jsonData, -1)
	if len(matches) == 0 {
		return nil, nil
	}

	items := make([]DataItem, 0, len(matches))
	invalid := make([]InvalidRecord, 0)
	for _, match := range matches {
		valueStart := match[1]
		data, nextIdx := readJSONStringValue(jsonData, valueStart)
		if nextIdx == -1 {
			fragment := jsonData[match[0]:]
			if len(fragment) > 512 {
				fragment = fragment[:512]
			}
			invalid = append(invalid, InvalidRecord{
				RawData: fragment,
				Reason:  "malformed JSON data object",
			})
			continue
		}

		if data == "" {
			invalid = append(invalid, InvalidRecord{
				RawData: data,
				Reason:  "empty data field",
			})
			continue
		}

		items = append(items, DataItem{Data: data})
	}

	return items, invalid
}

// readJSONStringValue reads a JSON string value and returns the decoded content
// along with the index after the closing "}.
func readJSONStringValue(raw string, start int) (string, int) {
	var data strings.Builder

	for i := start; i < len(raw); i++ {
		switch raw[i] {
		case '\\':
			if i+1 >= len(raw) {
				return "", -1
			}
			switch raw[i+1] {
			case '"', '\\', '/':
				data.WriteByte(raw[i+1])
			case 'b':
				data.WriteByte('\b')
			case 'f':
				data.WriteByte('\f')
			case 'n':
				data.WriteByte('\n')
			case 'r':
				data.WriteByte('\r')
			case 't':
				data.WriteByte('\t')
			default:
				// Keep unknown escapes as-is; GPS payloads rarely use them.
				data.WriteByte(raw[i+1])
			}
			i++
		case '"':
			if i+1 < len(raw) && raw[i+1] == '}' {
				return data.String(), i + 2
			}
			return "", -1
		default:
			data.WriteByte(raw[i])
		}
	}

	return "", -1
}

// processDataItems processes multiple data items.
func processDataItems(items []DataItem) ([]ParsedGPSData, []InvalidRecord) {
	processedData := make([]ParsedGPSData, 0, len(items))
	invalid := make([]InvalidRecord, 0)

	for _, item := range items {
		if item.Data == "" {
			invalid = append(invalid, InvalidRecord{
				RawData: item.Data,
				Reason:  "empty data field",
			})
			continue
		}

		parsed, err := processDataItem(item.Data)
		if err != nil {
			logger.Debug("Skipping invalid GPS record",
				zap.Error(err),
				zap.String("data", item.Data))
			invalid = append(invalid, InvalidRecord{
				RawData: item.Data,
				Reason:  err.Error(),
			})
			continue
		}

		processedData = append(processedData, *parsed)
	}

	return processedData, invalid
}

// processDataItem processes a single GPS data string
func processDataItem(data string) (*ParsedGPSData, error) {
	// Validate data format
	if !isValidDataFormat(data) {
		return nil, fmt.Errorf("invalid GPS data format")
	}

	// Split by comma
	fields := strings.Split(data, ",")
	if len(fields) != 12 {
		return nil, fmt.Errorf("expected 12 fields, got %d", len(fields))
	}

	// Convert coordinates from NMEA to decimal degrees
	lat, lon, err := convertNMEAToDecimalDegrees(fields[1], fields[2])
	if err != nil {
		return nil, fmt.Errorf("failed to convert coordinates: %w", err)
	}

	// Parse date/time
	dateTime, err := parseDateTime(fields[4], fields[5])
	if err != nil {
		return nil, fmt.Errorf("failed to parse date/time: %w", err)
	}

	// Parse integer fields
	speed, _ := strconv.Atoi(fields[6])
	status, _ := strconv.Atoi(fields[8])
	ewDirection, _ := strconv.Atoi(fields[9])
	nsDirection, _ := strconv.Atoi(fields[10])

	return &ParsedGPSData{
		Coordinate: [2]float64{lat, lon},
		Speed:      speed,
		Status:     status,
		Directions: DirectionData{
			EW: ewDirection,
			NS: nsDirection,
		},
		DateTime: dateTime,
		IMEI:     fields[11],
		RawData:  data,
	}, nil
}

// isValidDataFormat validates if the data follows the expected GPS device format
// Uses length pre-check to avoid expensive regex on obviously invalid data
func isValidDataFormat(data string) bool {
	// Quick length check before regex (GPS data is typically 75-95 chars)
	n := len(data)
	if n < 70 || n > 100 {
		return false
	}
	return gpsDataPattern.MatchString(data)
}

// convertNMEAToDecimalDegrees converts NMEA coordinates to decimal degrees
func convertNMEAToDecimalDegrees(nmeaLat, nmeaLon string) (float64, float64, error) {
	lat, err := parseFloat(nmeaLat)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid latitude: %w", err)
	}

	lon, err := parseFloat(nmeaLon)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid longitude: %w", err)
	}

	latitude := nmeaToDecimalDegrees(lat)
	longitude := nmeaToDecimalDegrees(lon)

	// Round to 6 decimal places (~0.1m precision)
	latitude = math.Round(latitude*1000000) / 1000000
	longitude = math.Round(longitude*1000000) / 1000000

	return latitude, longitude, nil
}

// nmeaToDecimalDegrees converts a single NMEA coordinate to decimal degrees
func nmeaToDecimalDegrees(nmea float64) float64 {
	degrees := math.Floor(nmea / 100)
	minutes := (nmea - (degrees * 100)) / 60
	return degrees + minutes
}

// parseDateTime parses date and time strings in YYMMDD and HHMMSS format
// Returns a MySQL-compatible datetime string: YYYY-MM-DD HH:MM:SS
func parseDateTime(date, timeStr string) (string, error) {
	// Combine date and time: YYMMDDHHMMSS
	combined := date + timeStr

	// Parse using the format
	t, err := time.Parse("060102150405", combined)
	if err != nil {
		return "", fmt.Errorf("invalid date/time format: %w", err)
	}

	// Add timezone offset: +3h 30m (Iran Standard Time)
	t = t.Add(3*time.Hour + 30*time.Minute)

	// Format as MySQL datetime: YYYY-MM-DD HH:MM:SS
	return t.Format("2006-01-02 15:04:05"), nil
}

// parseFloat parses a float64 from a string
func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}
