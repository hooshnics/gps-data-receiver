package parser_test

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gps-data-receiver/internal/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var hexLinePattern = regexp.MustCompile(`[0-9a-fA-F]{16,}`)

func loadSampleHex(t *testing.T, filename string) []byte {
	t.Helper()
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, filename))
	require.NoError(t, err)

	text := string(data)
	match := hexLinePattern.FindString(text)
	require.NotEmpty(t, match)
	raw, err := hex.DecodeString(strings.TrimSpace(match))
	require.NoError(t, err)
	return raw
}

func TestParseWithIMEI_TeltonikaBinary(t *testing.T) {
	raw := loadSampleHex(t, "sample data.txt")
	result, err := parser.ParseWithIMEI(raw, "3520930864036555", 3*time.Hour+30*time.Minute)
	require.NoError(t, err)
	require.Len(t, result.Records, 3)
	assert.Equal(t, "3520930864036555", result.Records[0].IMEI)
	assert.NotEmpty(t, result.Records[0].DateTime)
}

func TestParseWithIMEI_TeltonikaEnvelope(t *testing.T) {
	raw := loadSampleHex(t, "sample with 10 records.txt")
	parsed, err := parser.ParseWithIMEI(raw, "3520930864036555", 3*time.Hour+30*time.Minute)
	require.NoError(t, err)
	require.Len(t, parsed.Records, 10)

	envelope, err := parser.ParseTeltonikaEnvelope("3520930864036555", parsed.Records)
	require.NoError(t, err)

	result, err := parser.ParseWithIMEI(envelope, "", 3*time.Hour+30*time.Minute)
	require.NoError(t, err)
	require.Len(t, result.Records, 10)
	assert.Equal(t, "3520930864036555", result.Records[0].IMEI)
}

func TestParse_HooshnicStillWorks(t *testing.T) {
	data := []byte(`[{"data":"+Hooshnic:V1.06,3556.27680,05003.9190,000,260224,052019,000,000,0,3,1,861826074262144"}]`)
	result, err := parser.Parse(data)
	require.NoError(t, err)
	require.Len(t, result.Records, 1)
	assert.Equal(t, "861826074262144", result.Records[0].IMEI)
}
