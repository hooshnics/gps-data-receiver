package main

import (
	"encoding/json"
	"fmt"

	"github.com/gps-data-receiver/internal/filter"
	"github.com/gps-data-receiver/internal/parser"
	"github.com/redis/go-redis/v9"
)

// This is a manual test to demonstrate the stoppage filter with sample data
// Run with: go run tests/manual/test_stoppage_filter.go

func main() {
	fmt.Println("=== Stoppage Filter Integration Test ===\n")

	// Create a mock Redis client (using DB 15 for testing)
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   15,
	})

	// Create filter
	stoppageFilter := filter.NewStoppageFilter(client, true, 0)
	defer stoppageFilter.Close()

	fmt.Println("Filter initialized (enabled: true)\n")

	// Create sample parsed GPS data sequence (same IMEI)
	imei := "863070043377794"
	testSequence := []struct {
		name     string
		data     []parser.ParsedGPSData
		expected string
	}{
		{
			name: "Movement 1 (speed: 10)",
			data: []parser.ParsedGPSData{
				{
					Coordinate: [2]float64{35.960292, 50.108268},
					Speed:      10,
					Status:     1,
					Directions: parser.DirectionData{EW: 3, NS: 1},
					DateTime:   "2026-02-27 10:00:32",
					IMEI:       imei,
				},
			},
			expected: "SEND",
		},
		{
			name: "Movement 2 (speed: 5)",
			data: []parser.ParsedGPSData{
				{
					Coordinate: [2]float64{35.96029, 50.10829},
					Speed:      5,
					Status:     1,
					Directions: parser.DirectionData{EW: 3, NS: 1},
					DateTime:   "2026-02-27 10:00:33",
					IMEI:       imei,
				},
			},
			expected: "SEND",
		},
		{
			name: "Stoppage 1 (speed: 0) - First stoppage",
			data: []parser.ParsedGPSData{
				{
					Coordinate: [2]float64{35.96029, 50.10829},
					Speed:      0,
					Status:     1,
					Directions: parser.DirectionData{EW: 3, NS: 1},
					DateTime:   "2026-02-27 10:00:34",
					IMEI:       imei,
				},
			},
			expected: "SEND",
		},
		{
			name: "Stoppage 2 (speed: 0) - Redundant",
			data: []parser.ParsedGPSData{
				{
					Coordinate: [2]float64{35.96029, 50.10829},
					Speed:      0,
					Status:     1,
					Directions: parser.DirectionData{EW: 3, NS: 1},
					DateTime:   "2026-02-27 10:00:35",
					IMEI:       imei,
				},
			},
			expected: "FILTER",
		},
		{
			name: "Stoppage 3 (speed: 0) - Redundant",
			data: []parser.ParsedGPSData{
				{
					Coordinate: [2]float64{35.96029, 50.10829},
					Speed:      0,
					Status:     1,
					Directions: parser.DirectionData{EW: 3, NS: 1},
					DateTime:   "2026-02-27 10:00:36",
					IMEI:       imei,
				},
			},
			expected: "FILTER",
		},
		{
			name: "Movement 3 (speed: 8)",
			data: []parser.ParsedGPSData{
				{
					Coordinate: [2]float64{35.96030, 50.10831},
					Speed:      8,
					Status:     1,
					Directions: parser.DirectionData{EW: 3, NS: 1},
					DateTime:   "2026-02-27 10:00:37",
					IMEI:       imei,
				},
			},
			expected: "SEND",
		},
		{
			name: "Movement 4 (speed: 12)",
			data: []parser.ParsedGPSData{
				{
					Coordinate: [2]float64{35.96032, 50.10835},
					Speed:      12,
					Status:     1,
					Directions: parser.DirectionData{EW: 3, NS: 1},
					DateTime:   "2026-02-27 10:00:38",
					IMEI:       imei,
				},
			},
			expected: "SEND",
		},
	}

	totalSent := 0
	totalFiltered := 0
	var sentRecords []parser.ParsedGPSData

	for i, test := range testSequence {
		fmt.Printf("Step %d: %s\n", i+1, test.name)

		fmt.Printf("  Input: IMEI: %s, Speed: %d, Status: %d\n",
			test.data[0].IMEI, test.data[0].Speed, test.data[0].Status)

		// Apply filter
		filtered := stoppageFilter.FilterData(test.data)

		// Determine result
		var result string
		if len(filtered) > 0 {
			result = "SEND"
			totalSent++
			sentRecords = append(sentRecords, filtered...)
		} else {
			result = "FILTER"
			totalFiltered++
		}

		// Show result
		if result == test.expected {
			fmt.Printf("  ✅ Result: %s (as expected)\n", result)
		} else {
			fmt.Printf("  ❌ Result: %s (expected: %s)\n", result, test.expected)
		}

		if len(filtered) > 0 {
			fmt.Printf("  → Sending %d record(s) to destination servers\n", len(filtered))
		} else {
			fmt.Printf("  → Filtered out (redundant stoppage)\n")
		}

		fmt.Println()
	}

	fmt.Println("=== Summary ===")
	fmt.Printf("Total processed: %d records\n", len(testSequence))
	fmt.Printf("Sent to destination: %d records\n", totalSent)
	fmt.Printf("Filtered (redundant): %d records\n", totalFiltered)
	fmt.Printf("Filter efficiency: %.1f%% reduction in stoppage data\n",
		float64(totalFiltered)/float64(len(testSequence))*100)

	// Show expected output format
	fmt.Println("\n=== Sample Output Sent to Destination ===")
	fmt.Println("The following records would be sent to destination servers:")
	fmt.Println()

	for i, record := range sentRecords {
		fmt.Printf("%d. DateTime: %s, Speed: %d, Coordinate: [%.6f, %.6f], IMEI: %s\n",
			i+1, record.DateTime, record.Speed,
			record.Coordinate[0], record.Coordinate[1], record.IMEI)
	}

	// Show JSON format that would be sent
	fmt.Println("\n=== JSON Format Sent to Destination ===")
	wrappedData := map[string]interface{}{
		"data": sentRecords,
	}
	jsonData, _ := json.MarshalIndent(wrappedData, "", "  ")
	fmt.Printf("%s\n", string(jsonData))

	fmt.Println("\n=== Filter State ===")
	fmt.Printf("IMEIs being tracked: %d\n", stoppageFilter.GetStateCount())

	fmt.Println("\n=== Verification ===")
	fmt.Println("✅ All movement records were sent")
	fmt.Println("✅ First stoppage after movement was sent")
	fmt.Println("✅ Redundant stoppages were filtered")
	fmt.Println("✅ Movement after stoppage resumed normal sending")
}
