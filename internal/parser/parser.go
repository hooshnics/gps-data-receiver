package parser

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gps-data-receiver/pkg/logger"
	"go.uber.org/zap"
)

// ParsedGPSData represents a parsed GPS record
type ParsedGPSData struct {
	Coordinate [2]float64    `json:"coordinate"` // [lat, lon]
	Speed      int           `json:"speed"`
	Status     int           `json:"status"`
	Directions DirectionData `json:"directions"`
	DateTime   string        `json:"date_time"` // MySQL datetime format: YYYY-MM-DD HH:MM:SS
	IMEI       string        `json:"imei"`
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
var gpsDataPattern = regexp.MustCompile(`^\+[A-Za-z]+:[A-Za-z0-9.]+,\d{4}\.\d{5},\d{5}\.\d{4},\d{3},\d{6},\d{6},\d{3},\d{3},\d,\d,\d,\d{15}$`)

// Parse parses GPS data from JSON byte array into structured format
func Parse(data []byte) ([]ParsedGPSData, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("input data cannot be empty")
	}

	// Decode JSON data with cleaning
	decodedData, err := decodeJSONData(string(data))
	if err != nil {
		return nil, fmt.Errorf("failed to decode JSON data: %w", err)
	}

	if len(decodedData) == 0 {
		return []ParsedGPSData{}, nil
	}

	// Process all data items
	processedData := processDataItems(decodedData)

	// Sort by date_time (string comparison works for YYYY-MM-DD HH:MM:SS format)
	sort.Slice(processedData, func(i, j int) bool {
		return processedData[i].DateTime < processedData[j].DateTime
	})

	return processedData, nil
}

// decodeJSONData decodes and cleans the JSON data
func decodeJSONData(jsonData string) ([]DataItem, error) {
	// Clean JSON artifacts found in sample data
	trimmedData := strings.TrimRight(jsonData, ".")

	// Fix missing commas between objects: }{ -> },{
	trimmedData = strings.ReplaceAll(trimmedData, "}{", "},{")

	// Remove empty objects: ,{} -> ""
	trimmedData = strings.ReplaceAll(trimmedData, ",{}", "")

	// Remove trailing commas before closing brackets: },] -> }]
	trimmedData = regexp.MustCompile(`\},\s*\]`).ReplaceAllString(trimmedData, "}]")

	// Remove trailing commas
	trimmedData = strings.TrimRight(trimmedData, ",")

	// Decode JSON
	var decodedData []DataItem
	if err := json.Unmarshal([]byte(trimmedData), &decodedData); err != nil {
		logger.Debug("Failed to decode JSON",
			zap.Error(err),
			zap.String("cleaned_data", trimmedData))
		return nil, fmt.Errorf("invalid JSON format: %w", err)
	}

	return decodedData, nil
}

// processDataItems processes multiple data items
func processDataItems(items []DataItem) []ParsedGPSData {
	processedData := make([]ParsedGPSData, 0, len(items))

	for _, item := range items {
		if item.Data == "" {
			continue
		}

		parsed, err := processDataItem(item.Data)
		if err != nil {
			logger.Debug("Skipping invalid GPS record",
				zap.Error(err),
				zap.String("data", item.Data))
			continue
		}

		processedData = append(processedData, *parsed)
	}

	return processedData
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
	}, nil
}

// isValidDataFormat validates if the data follows the expected GPS device format
func isValidDataFormat(data string) bool {
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
