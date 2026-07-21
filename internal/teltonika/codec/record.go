package codec

import (
	"encoding/binary"
	"fmt"
)

// Record holds decoded fields from one Teltonika AVL record.
type Record struct {
	TimestampMs uint64
	Priority    uint8
	Lat         float64
	Lon         float64
	Altitude    uint16
	Angle       uint16
	Satellites  uint8
	Speed       uint16
	EventIOID   uint16
	Generation  uint8 // Codec 16 only
	IOs         map[uint16]uint64
	RawBytes    []byte
}

func parseGPS(buf []byte) (lat, lon float64, altitude, angle, speed uint16, satellites uint8) {
	rawLon := int32(binary.BigEndian.Uint32(buf[0:4]))
	rawLat := int32(binary.BigEndian.Uint32(buf[4:8]))
	altitude = binary.BigEndian.Uint16(buf[8:10])
	angle = binary.BigEndian.Uint16(buf[10:12])
	satellites = buf[12]
	speed = binary.BigEndian.Uint16(buf[13:15])
	lon = float64(rawLon) / 10_000_000.0
	lat = float64(rawLat) / 10_000_000.0
	return
}

func readIOCount(buf []byte, off int, width int) (count uint16, next int, err error) {
	if width == 1 {
		if off >= len(buf) {
			return 0, off, fmt.Errorf("unexpected end of IO block")
		}
		return uint16(buf[off]), off + 1, nil
	}
	if off+2 > len(buf) {
		return 0, off, fmt.Errorf("unexpected end of IO block")
	}
	return binary.BigEndian.Uint16(buf[off : off+2]), off + 2, nil
}

func readIOID(buf []byte, off int, width int) (id uint16, next int, err error) {
	if width == 1 {
		if off >= len(buf) {
			return 0, off, fmt.Errorf("unexpected end of IO ID")
		}
		return uint16(buf[off]), off + 1, nil
	}
	if off+2 > len(buf) {
		return 0, off, fmt.Errorf("unexpected end of IO ID")
	}
	return binary.BigEndian.Uint16(buf[off : off+2]), off + 2, nil
}

func parseIOBlock(buf []byte, off int, cfg ioLayout) (map[uint16]uint64, int, error) {
	ios := make(map[uint16]uint64)

	var err error
	var eventIOID uint16
	if cfg.eventIDWidth == 1 {
		if off >= len(buf) {
			return nil, off, fmt.Errorf("missing event IO ID")
		}
		eventIOID = uint16(buf[off])
		off++
	} else {
		if off+2 > len(buf) {
			return nil, off, fmt.Errorf("missing event IO ID")
		}
		eventIOID = binary.BigEndian.Uint16(buf[off : off+2])
		off += 2
	}
	_ = eventIOID

	if cfg.hasGeneration {
		if off >= len(buf) {
			return nil, off, fmt.Errorf("missing generation type")
		}
		off++
	}

	if _, off, err = readIOCount(buf, off, cfg.countWidth); err != nil {
		return nil, off, err
	}

	parseGroup := func(valueSize int) error {
		var n uint16
		n, off, err = readIOCount(buf, off, cfg.countWidth)
		if err != nil {
			return err
		}
		for i := uint16(0); i < n; i++ {
			var id uint16
			id, off, err = readIOID(buf, off, cfg.idWidth)
			if err != nil {
				return err
			}
			if off+valueSize > len(buf) {
				return fmt.Errorf("unexpected end of IO value")
			}
			var val uint64
			switch valueSize {
			case 1:
				val = uint64(buf[off])
			case 2:
				val = uint64(binary.BigEndian.Uint16(buf[off : off+2]))
			case 4:
				val = uint64(binary.BigEndian.Uint32(buf[off : off+4]))
			case 8:
				val = binary.BigEndian.Uint64(buf[off : off+8])
			}
			off += valueSize
			ios[id] = val
		}
		return nil
	}

	for _, size := range []int{1, 2, 4, 8} {
		if err = parseGroup(size); err != nil {
			return nil, off, err
		}
	}

	if cfg.hasVariableIO {
		var nx uint16
		nx, off, err = readIOCount(buf, off, cfg.countWidth)
		if err != nil {
			return nil, off, err
		}
		for i := uint16(0); i < nx; i++ {
			var id uint16
			id, off, err = readIOID(buf, off, cfg.idWidth)
			if err != nil {
				return nil, off, err
			}
			var length uint16
			if off+2 > len(buf) {
				return nil, off, fmt.Errorf("missing variable IO length")
			}
			length = binary.BigEndian.Uint16(buf[off : off+2])
			off += 2
			if off+int(length) > len(buf) {
				return nil, off, fmt.Errorf("unexpected end of variable IO value")
			}
			var val uint64
			for j := 0; j < int(length) && j < 8; j++ {
				val = (val << 8) | uint64(buf[off+j])
			}
			off += int(length)
			ios[id] = val
		}
	}

	return ios, off, nil
}

type ioLayout struct {
	eventIDWidth  int
	countWidth    int
	idWidth       int
	hasGeneration bool
	hasVariableIO bool
}

func layoutForCodec(codecID byte) (ioLayout, error) {
	switch codecID {
	case 0x08:
		return ioLayout{eventIDWidth: 1, countWidth: 1, idWidth: 1}, nil
	case 0x8E:
		return ioLayout{eventIDWidth: 2, countWidth: 2, idWidth: 2, hasVariableIO: true}, nil
	case 0x10:
		return ioLayout{eventIDWidth: 2, countWidth: 1, idWidth: 2, hasGeneration: true}, nil
	default:
		return ioLayout{}, fmt.Errorf("unsupported codec ID: 0x%02X", codecID)
	}
}

func decodeRecord(buf []byte, codecID byte) (Record, int, error) {
	if len(buf) < 9 {
		return Record{}, 0, fmt.Errorf("record too short")
	}

	start := 0
	rec := Record{
		TimestampMs: binary.BigEndian.Uint64(buf[0:8]),
		Priority:    buf[8],
	}
	off := 9

	lat, lon, alt, angle, speed, sats := parseGPS(buf[off : off+15])
	rec.Lat = lat
	rec.Lon = lon
	rec.Altitude = alt
	rec.Angle = angle
	rec.Speed = speed
	rec.Satellites = sats
	off += 15

	cfg, err := layoutForCodec(codecID)
	if err != nil {
		return Record{}, 0, err
	}

	ios, next, err := parseIOBlock(buf, off, cfg)
	if err != nil {
		return Record{}, 0, err
	}
	rec.IOs = ios
	off = next

	rec.RawBytes = append([]byte(nil), buf[start:off]...)
	return rec, off, nil
}
