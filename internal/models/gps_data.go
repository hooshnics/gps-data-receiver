package models

import (
	"encoding/json"
	"time"
)

// GPSPacket represents a raw GPS data packet
// We use json.RawMessage to avoid parsing/validation and pass through as-is
type GPSPacket struct {
	ID        string          `json:"id"`
	Data      json.RawMessage `json:"data"`
	Timestamp time.Time       `json:"timestamp"`
}

// NewGPSPacket creates a new GPS packet with raw data
func NewGPSPacket(data []byte) *GPSPacket {
	return &GPSPacket{
		Data:      json.RawMessage(data),
		Timestamp: time.Now(),
	}
}
