package codec_test

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gps-data-receiver/internal/teltonika/codec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var hexLinePattern = regexp.MustCompile(`[0-9a-fA-F]{16,}`)

func loadSampleHex(t *testing.T, filename string) []byte {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	data, err := os.ReadFile(filepath.Join(root, filename))
	require.NoError(t, err)

	text := string(data)
	match := hexLinePattern.FindString(text)
	require.NotEmpty(t, match, "no hex payload found in %s", filename)
	match = strings.TrimSpace(match)

	raw, err := hex.DecodeString(match)
	require.NoError(t, err)
	return raw
}

func TestParsePacket_Sample3Records(t *testing.T) {
	raw := loadSampleHex(t, "sample data.txt")

	codecID, records, err := codec.ParsePacket(raw)
	require.NoError(t, err)
	assert.Equal(t, byte(0x8E), codecID)
	require.Len(t, records, 3)

	for _, rec := range records {
		assert.NotZero(t, rec.TimestampMs)
		assert.NotZero(t, rec.Lat)
		assert.NotZero(t, rec.Lon)
	}
}

func TestParsePacket_Sample10Records(t *testing.T) {
	raw := loadSampleHex(t, "sample with 10 records.txt")

	codecID, records, err := codec.ParsePacket(raw)
	require.NoError(t, err)
	assert.Equal(t, byte(0x8E), codecID)
	assert.Len(t, records, 10)
}

func TestDetectFormat(t *testing.T) {
	raw := loadSampleHex(t, "sample data.txt")
	assert.Equal(t, codec.FormatTeltonikaAVL, codec.DetectFormat(raw))
	assert.Equal(t, codec.FormatHooshnicJSON, codec.DetectFormat([]byte(`[{"data":"x"}]`)))
	assert.Equal(t, codec.FormatTeltonikaQueued, codec.DetectFormat([]byte(`{"_teltonika":true}`)))
}

func TestCRC16IBM_KnownExample(t *testing.T) {
	// Codec8 example payload from Teltonika wiki (codec through num data 2).
	payload, err := hex.DecodeString("08010000016B40D8EA30010000000000000000000000000000000105021503010101425E0F01F10000601A014E000000000000000001")
	require.NoError(t, err)
	assert.Equal(t, uint16(0xC7CF), codec.CRC16IBM(payload))
}
