package sanitizer

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gps-data-receiver/pkg/logger"
	"go.uber.org/zap"
)

// SanitizeUTF8 cleans UTF-8 encoding issues in raw data.
// This replicates the Laravel sanitizeUtf8 method from GpsReportController.
func SanitizeUTF8(data []byte) []byte {
	if len(data) == 0 {
		return data
	}

	// Check if already valid UTF-8
	if !utf8.Valid(data) {
		logger.Debug("Invalid UTF-8 detected, attempting to sanitize")

		// Try to convert by replacing invalid UTF-8 sequences
		data = []byte(strings.ToValidUTF8(string(data), ""))

		// If still invalid after conversion, remove control characters
		if !utf8.Valid(data) {
			logger.Warn("UTF-8 conversion failed, removing control characters")
			data = removeControlChars(data)
		}

		// Final check - if still invalid, return empty
		if !utf8.Valid(data) {
			logger.Error("Failed to sanitize UTF-8 data, returning empty",
				zap.Int("original_length", len(data)))
			return []byte{}
		}
	}

	// Always remove control characters (even from valid UTF-8)
	// This matches Laravel's behavior of removing control chars
	sanitized := removeControlChars(data)

	if len(sanitized) != len(data) {
		logger.Debug("Data sanitized",
			zap.Int("original_length", len(data)),
			zap.Int("sanitized_length", len(sanitized)))
	}

	return sanitized
}

// removeControlChars removes control characters from the data.
// Removes: 0x00-0x08, 0x0B, 0x0C, 0x0E-0x1F, 0x7F
var controlCharsRegex = regexp.MustCompile(`[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]`)

func removeControlChars(data []byte) []byte {
	return controlCharsRegex.ReplaceAll(data, []byte{})
}
