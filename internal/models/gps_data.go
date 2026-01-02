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

// FailedPacket represents a packet that failed to be delivered
type FailedPacket struct {
	ID           uint      `gorm:"primaryKey"`
	Payload      string    `gorm:"type:text;not null"`
	RetryCount   int       `gorm:"not null;default:0"`
	LastError    string    `gorm:"type:text"`
	TargetServer string    `gorm:"type:varchar(255)"`
	CreatedAt    time.Time `gorm:"index"`
	UpdatedAt    time.Time
}

// TableName specifies the table name for FailedPacket
func (FailedPacket) TableName() string {
	return "failed_packets"
}

