package archive_test

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gps-data-receiver/internal/archive"
	"github.com/stretchr/testify/require"
)

func TestWriter_DailyGzipNDJSON(t *testing.T) {
	dir := t.TempDir()
	w, err := archive.NewWriter(dir, false)
	require.NoError(t, err)
	defer w.Close()

	ctx := context.Background()
	require.NoError(t, w.Archive(ctx, "1-0", "356890080000001", "deadbeef", time.Now().UTC().Format(time.RFC3339Nano), "tcp"))
	require.NoError(t, w.Close())

	now := time.Now().UTC()
	path := filepath.Join(dir, now.Format("2006"), now.Format("01"), now.Format("02"), "raw-"+now.Format("20060102")+".ndjson.gz")
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	gz, err := gzip.NewReader(f)
	require.NoError(t, err)
	defer gz.Close()

	var rec archive.Record
	require.NoError(t, json.NewDecoder(gz).Decode(&rec))
	require.Equal(t, "1-0", rec.ID)
	require.Equal(t, "356890080000001", rec.IMEI)
	require.Equal(t, "deadbeef", rec.FrameHex)
	require.NotEmpty(t, rec.ArchivedAt)
}

func TestEnsureWritable_CreatesDayLayout(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, archive.EnsureWritable(dir))
	now := time.Now().UTC()
	dayDir := filepath.Join(dir, now.Format("2006"), now.Format("01"), now.Format("02"))
	st, err := os.Stat(dayDir)
	require.NoError(t, err)
	require.True(t, st.IsDir())
}

func TestEnsureWritable_RejectsEmpty(t *testing.T) {
	require.Error(t, archive.EnsureWritable(""))
}
