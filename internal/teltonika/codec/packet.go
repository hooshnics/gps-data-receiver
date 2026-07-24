package codec

import (
	"encoding/binary"
	"fmt"
)

const maxDataFieldLength = 2048

// ParsePacket decodes a full Teltonika AVL TCP packet.
func ParsePacket(data []byte) (codecID byte, records []Record, err error) {
	if len(data) < 12 {
		return 0, nil, fmt.Errorf("packet too short: %d bytes", len(data))
	}
	if data[0]|data[1]|data[2]|data[3] != 0 {
		return 0, nil, fmt.Errorf("invalid preamble")
	}

	dataFieldLen := binary.BigEndian.Uint32(data[4:8])
	if dataFieldLen == 0 || dataFieldLen > maxDataFieldLength {
		return 0, nil, fmt.Errorf("invalid data field length: %d", dataFieldLen)
	}

	expectedLen := 8 + int(dataFieldLen) + 4
	if len(data) < expectedLen {
		return 0, nil, fmt.Errorf("packet truncated: have %d, need %d", len(data), expectedLen)
	}

	payload := data[8 : 8+dataFieldLen]
	crcBytes := data[8+dataFieldLen : 8+dataFieldLen+4]
	receivedCRC := binary.BigEndian.Uint32(crcBytes)
	computedCRC := uint32(CRC16IBM(payload))
	if receivedCRC != computedCRC {
		return 0, nil, fmt.Errorf("CRC mismatch: got 0x%08X want 0x%08X", receivedCRC, computedCRC)
	}

	if len(payload) < 3 {
		return 0, nil, fmt.Errorf("payload too short")
	}

	codecID = payload[0]
	numRecords := int(payload[1])
	numRecords2 := int(payload[len(payload)-1])
	if numRecords != numRecords2 {
		return 0, nil, fmt.Errorf("record count mismatch: %d != %d", numRecords, numRecords2)
	}

	off := 2
	end := len(payload) - 1
	records = make([]Record, 0, numRecords)
	for i := 0; i < numRecords; i++ {
		if off >= end {
			return codecID, nil, fmt.Errorf("unexpected end of records at index %d", i)
		}
		rec, consumed, err := decodeRecord(payload[off:end], codecID)
		if err != nil {
			return codecID, records, fmt.Errorf("record %d: %w", i, err)
		}
		records = append(records, rec)
		off += consumed
	}

	if off != end {
		return codecID, records, fmt.Errorf("unexpected trailing bytes in AVL data")
	}
	if len(records) != numRecords {
		return codecID, records, fmt.Errorf("parsed %d records, expected %d", len(records), numRecords)
	}

	return codecID, records, nil
}

// CountAVLRecords validates preamble + CRC and returns Number-of-Data for the TCP ACK.
// It intentionally does NOT decode IO/GPS records — keep that off the ACK hot path.
// Full decoding remains in ParsePacket (unchanged) and is used by the post-ACK worker.
func CountAVLRecords(data []byte) (uint32, error) {
	if len(data) < 12 {
		return 0, fmt.Errorf("packet too short: %d bytes", len(data))
	}
	if data[0]|data[1]|data[2]|data[3] != 0 {
		return 0, fmt.Errorf("invalid preamble")
	}

	dataFieldLen := binary.BigEndian.Uint32(data[4:8])
	if dataFieldLen == 0 || dataFieldLen > maxDataFieldLength {
		return 0, fmt.Errorf("invalid data field length: %d", dataFieldLen)
	}

	expectedLen := 8 + int(dataFieldLen) + 4
	if len(data) < expectedLen {
		return 0, fmt.Errorf("packet truncated: have %d, need %d", len(data), expectedLen)
	}

	payload := data[8 : 8+dataFieldLen]
	crcBytes := data[8+dataFieldLen : 8+dataFieldLen+4]
	receivedCRC := binary.BigEndian.Uint32(crcBytes)
	computedCRC := uint32(CRC16IBM(payload))
	if receivedCRC != computedCRC {
		return 0, fmt.Errorf("CRC mismatch: got 0x%08X want 0x%08X", receivedCRC, computedCRC)
	}

	if len(payload) < 3 {
		return 0, fmt.Errorf("payload too short")
	}

	numRecords := uint32(payload[1])
	numRecords2 := uint32(payload[len(payload)-1])
	if numRecords != numRecords2 {
		return 0, fmt.Errorf("record count mismatch: %d != %d", numRecords, numRecords2)
	}
	if numRecords == 0 {
		return 0, fmt.Errorf("zero records")
	}

	return numRecords, nil
}
