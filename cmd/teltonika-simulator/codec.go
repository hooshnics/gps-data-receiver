package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/gps-data-receiver/internal/teltonika/codec"
)

// buildAVLPacket builds a minimal valid Codec 8 or Codec 8 Extended AVL frame.
func buildAVLPacket(codecName string, imeiSeed int, n int, baseTime time.Time, lat, lon float64) []byte {
	id := parseCodecID(codecName)
	if n < 1 {
		n = 1
	}
	if n > 40 {
		n = 40 // stay under typical data-field limits
	}

	var records []byte
	for i := 0; i < n; i++ {
		ts := uint64(baseTime.Add(time.Duration(i) * time.Second).UnixMilli())
		dlat := float64(imeiSeed%1000)*0.00001 + float64(i)*0.000001
		dlon := float64(imeiSeed%700)*0.00001 + float64(i)*0.000001
		records = append(records, encodeAVLRecord(id, ts, lat+dlat, lon+dlon)...)
	}

	dataField := make([]byte, 0, 2+len(records)+1)
	dataField = append(dataField, id, byte(n))
	dataField = append(dataField, records...)
	dataField = append(dataField, byte(n))

	crc := codec.CRC16IBM(dataField)
	frame := make([]byte, 0, 8+len(dataField)+4)
	frame = append(frame, 0, 0, 0, 0)
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(dataField)))
	frame = append(frame, lenBuf...)
	frame = append(frame, dataField...)
	crcBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(crcBuf, uint32(crc))
	frame = append(frame, crcBuf...)
	return frame
}

func parseCodecID(name string) byte {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "8e", "codec8e", "codec8ext", "ext":
		return 0x8E
	default:
		return 0x08
	}
}

func encodeAVLRecord(codecID byte, tsMs uint64, lat, lon float64) []byte {
	rawLon := int32(math.Round(lon * 10_000_000))
	rawLat := int32(math.Round(lat * 10_000_000))
	b := make([]byte, 0, 48)
	ts := make([]byte, 8)
	binary.BigEndian.PutUint64(ts, tsMs)
	b = append(b, ts...)
	b = append(b, 0x00) // priority
	lonB := make([]byte, 4)
	latB := make([]byte, 4)
	binary.BigEndian.PutUint32(lonB, uint32(rawLon))
	binary.BigEndian.PutUint32(latB, uint32(rawLat))
	b = append(b, lonB...)
	b = append(b, latB...)
	alt := make([]byte, 2)
	binary.BigEndian.PutUint16(alt, 1200)
	b = append(b, alt...)
	ang := make([]byte, 2)
	binary.BigEndian.PutUint16(ang, 0)
	b = append(b, ang...)
	b = append(b, 12) // satellites
	spd := make([]byte, 2)
	binary.BigEndian.PutUint16(spd, 4)
	b = append(b, spd...)
	b = append(b, encodeIO(codecID)...)
	return b
}

func encodeIO(codecID byte) []byte {
	if codecID == 0x8E {
		// Codec8E: 2-byte event/id/counts + variable-IO section (empty).
		return []byte{
			0x00, 0x00, // event IO id
			0x00, 0x01, // total IO elements
			0x00, 0x01, // n1
			0x00, 239, 0x01, // ignition AVL 239 = 1
			0x00, 0x00, // n2
			0x00, 0x00, // n4
			0x00, 0x00, // n8
			0x00, 0x00, // nx (variable)
		}
	}
	// Codec8: 1-byte event/id/counts
	return []byte{
		0x00,       // event IO id
		0x01,       // total IO count
		0x01,       // n1
		239, 0x01,  // ignition
		0x00,       // n2
		0x00,       // n4
		0x00,       // n8
	}
}

func syntheticIMEI(index int) string {
	// 15-digit numeric IMEI-like id
	return fmt.Sprintf("35689008%07d", index%10_000_000)
}
