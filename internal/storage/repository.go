package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/gps-data-receiver/internal/models"
	"github.com/gps-data-receiver/pkg/logger"
	"go.uber.org/zap"
)

// FailedPacketRepository handles operations on failed packets
type FailedPacketRepository struct {
	mysql *MySQL
}

// NewFailedPacketRepository creates a new repository
func NewFailedPacketRepository(mysql *MySQL) *FailedPacketRepository {
	return &FailedPacketRepository{
		mysql: mysql,
	}
}

// Save saves a failed packet to the database
func (r *FailedPacketRepository) Save(ctx context.Context, packet *models.FailedPacket) error {
	result := r.mysql.db.WithContext(ctx).Create(packet)
	if result.Error != nil {
		logger.Error("Failed to save failed packet",
			zap.Error(result.Error),
			zap.String("target_server", packet.TargetServer))
		return fmt.Errorf("failed to save packet: %w", result.Error)
	}

	logger.Info("Failed packet saved to database",
		zap.Uint("id", packet.ID),
		zap.String("target_server", packet.TargetServer),
		zap.Int("retry_count", packet.RetryCount))

	return nil
}

// SaveFailedPacket is a convenience method that creates and saves a failed packet
func (r *FailedPacketRepository) SaveFailedPacket(ctx context.Context, payload []byte, retryCount int, lastError, targetServer string) error {
	packet := &models.FailedPacket{
		Payload:      string(payload),
		RetryCount:   retryCount,
		LastError:    lastError,
		TargetServer: targetServer,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	return r.Save(ctx, packet)
}

// GetAll retrieves all failed packets
func (r *FailedPacketRepository) GetAll(ctx context.Context, limit int) ([]models.FailedPacket, error) {
	var packets []models.FailedPacket
	
	query := r.mysql.db.WithContext(ctx).Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	
	result := query.Find(&packets)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to retrieve packets: %w", result.Error)
	}

	return packets, nil
}

// GetByID retrieves a failed packet by ID
func (r *FailedPacketRepository) GetByID(ctx context.Context, id uint) (*models.FailedPacket, error) {
	var packet models.FailedPacket
	
	result := r.mysql.db.WithContext(ctx).First(&packet, id)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to retrieve packet: %w", result.Error)
	}

	return &packet, nil
}

// DeleteOlderThan deletes failed packets older than the specified duration
func (r *FailedPacketRepository) DeleteOlderThan(ctx context.Context, duration time.Duration) (int64, error) {
	cutoffTime := time.Now().Add(-duration)
	
	result := r.mysql.db.WithContext(ctx).
		Where("created_at < ?", cutoffTime).
		Delete(&models.FailedPacket{})
	
	if result.Error != nil {
		return 0, fmt.Errorf("failed to delete old packets: %w", result.Error)
	}

	logger.Info("Deleted old failed packets",
		zap.Int64("count", result.RowsAffected),
		zap.Time("older_than", cutoffTime))

	return result.RowsAffected, nil
}

// Count returns the total number of failed packets
func (r *FailedPacketRepository) Count(ctx context.Context) (int64, error) {
	var count int64
	
	result := r.mysql.db.WithContext(ctx).Model(&models.FailedPacket{}).Count(&count)
	if result.Error != nil {
		return 0, fmt.Errorf("failed to count packets: %w", result.Error)
	}

	return count, nil
}

