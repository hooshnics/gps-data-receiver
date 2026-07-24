package device_test

import (
	"encoding/binary"
	"encoding/hex"
	"testing"
	"time"

	"github.com/gps-data-receiver/internal/device"
	"github.com/stretchr/testify/require"
)

func codec8WikiFrame(t *testing.T) []byte {
	t.Helper()
	payload, err := hex.DecodeString("08010000016B40D8EA30010000000000000000000000000000000105021503010101425E0F01F10000601A014E000000000000000001")
	require.NoError(t, err)
	frame := make([]byte, 0, 8+len(payload)+4)
	frame = append(frame, 0, 0, 0, 0)
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(payload)))
	frame = append(frame, lenBuf...)
	frame = append(frame, payload...)
	crcBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(crcBuf, 0xC7CF)
	frame = append(frame, crcBuf...)
	return frame
}

func TestDefaultRegistry_TeltonikaDetectDecode(t *testing.T) {
	reg := device.DefaultRegistry(3*time.Hour + 30*time.Minute)
	frame := codec8WikiFrame(t)

	drv, err := reg.Detect(frame, device.Hints{IMEI: "352094081882015"})
	require.NoError(t, err)
	require.Equal(t, device.NameTeltonika, drv.Name())
	require.NoError(t, drv.Validate(frame))

	dec, err := drv.Decode(frame, device.Hints{IMEI: "352094081882015"})
	require.NoError(t, err)
	require.Equal(t, device.NameTeltonika, dec.DeviceType)
	require.Equal(t, "codec8", dec.Protocol)
	require.Len(t, dec.Points, 1)
	require.Equal(t, "352094081882015", dec.Points[0].IMEI)
}

func TestDefaultRegistry_HooshnicsDetectDecode(t *testing.T) {
	reg := device.DefaultRegistry(0)
	raw := []byte(`[{"data":"+Hooshnic:V1.06,3556.27680,05003.9190,000,260224,052019,000,000,0,3,1,861826074262144"}]`)
	drv, err := reg.Detect(raw, device.Hints{})
	require.NoError(t, err)
	require.Equal(t, device.NameHooshnics, drv.Name())

	dec, err := drv.Decode(raw, device.Hints{})
	require.NoError(t, err)
	require.Equal(t, device.NameHooshnics, dec.DeviceType)
	require.NotEmpty(t, dec.Points)
	require.Equal(t, "861826074262144", dec.Points[0].IMEI)
}
