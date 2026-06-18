package rawlog

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gps-data-receiver/internal/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogger_WritesSingleRecordAsJSONArray(t *testing.T) {
	dir := t.TempDir()
	logger := New(true, dir, 16)
	require.NotNil(t, logger)

	imei := "861826074262144"
	raw := "+Hooshnic:V1.06,3556.27680,05003.9190,000,260224,052019,000,000,0,3,1," + imei

	logger.LogRecords([]parser.ParsedGPSData{{
		IMEI:    imei,
		RawData: raw,
	}})
	logger.Close()

	filePath := filepath.Join(dir, imei, time.Now().Format("2006-01-02")+".txt")
	content, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, `[{"data":"`+raw+`"}]`+"\n", string(content))
}

func TestLogger_WritesMultipleRecordsAsJSONArray(t *testing.T) {
	dir := t.TempDir()
	logger := New(true, dir, 16)
	require.NotNil(t, logger)

	imei := "861826074262144"
	raw1 := "+Hooshnic:V1.06,3556.27680,05003.9190,000,260224,052019,000,000,0,3,1," + imei
	raw2 := "+Hooshnic:V1.06,3556.27700,05003.9200,000,260224,052100,010,000,1,3,1," + imei

	logger.LogRecords([]parser.ParsedGPSData{
		{IMEI: imei, RawData: raw1},
		{IMEI: imei, RawData: raw2},
	})
	logger.Close()

	filePath := filepath.Join(dir, imei, time.Now().Format("2006-01-02")+".txt")
	content, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, `[{"data":"`+raw1+`"},{"data":"`+raw2+`"}]`+"\n", string(content))
}

func TestLogger_GroupsRecordsByIMEI(t *testing.T) {
	dir := t.TempDir()
	logger := New(true, dir, 16)
	require.NotNil(t, logger)

	imei1 := "861826074262144"
	imei2 := "861826074262145"
	raw1 := "+Hooshnic:V1.06,3556.27680,05003.9190,000,260224,052019,000,000,0,3,1," + imei1
	raw2 := "+Hooshnic:V1.06,3556.27700,05003.9200,000,260224,052100,010,000,1,3,1," + imei2

	logger.LogRecords([]parser.ParsedGPSData{
		{IMEI: imei1, RawData: raw1},
		{IMEI: imei2, RawData: raw2},
	})
	logger.Close()

	content1, err := os.ReadFile(filepath.Join(dir, imei1, time.Now().Format("2006-01-02")+".txt"))
	require.NoError(t, err)
	assert.Equal(t, `[{"data":"`+raw1+`"}]`+"\n", string(content1))

	content2, err := os.ReadFile(filepath.Join(dir, imei2, time.Now().Format("2006-01-02")+".txt"))
	require.NoError(t, err)
	assert.Equal(t, `[{"data":"`+raw2+`"}]`+"\n", string(content2))
}

func TestLogger_DisabledReturnsNil(t *testing.T) {
	assert.Nil(t, New(false, "logs/raw", 16))
}

func TestLogger_SkipsInvalidIMEI(t *testing.T) {
	dir := t.TempDir()
	logger := New(true, dir, 16)
	require.NotNil(t, logger)

	logger.LogRecords([]parser.ParsedGPSData{{
		IMEI:    "../etc/passwd",
		RawData: "payload",
	}})
	logger.Close()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}
