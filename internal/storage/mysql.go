package storage

import (
	"fmt"
	"time"

	"github.com/gps-data-receiver/internal/config"
	"github.com/gps-data-receiver/internal/models"
	"github.com/gps-data-receiver/pkg/logger"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// MySQL handles MySQL database operations
type MySQL struct {
	db *gorm.DB
}

// NewMySQL creates a new MySQL connection
func NewMySQL(cfg *config.MySQLConfig) (*MySQL, error) {
	dsn := cfg.GetDSN()

	// Configure GORM logger based on log level
	gormLogLevel := gormlogger.Error
	if cfg.Host != "" {
		gormLogLevel = gormlogger.Warn
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormLogLevel),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})

	if err != nil {
		return nil, fmt.Errorf("failed to connect to MySQL: %w", err)
	}

	// Get underlying SQL DB for connection pool configuration
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get SQL DB: %w", err)
	}

	// Configure connection pool
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	// Test connection
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping MySQL: %w", err)
	}

	logger.Info("MySQL connected successfully",
		zap.String("host", cfg.Host),
		zap.String("database", cfg.Database))

	// Auto-migrate the schema
	if err := db.AutoMigrate(&models.FailedPacket{}); err != nil {
		return nil, fmt.Errorf("failed to migrate schema: %w", err)
	}

	logger.Info("Database schema migrated successfully")

	return &MySQL{db: db}, nil
}

// GetDB returns the underlying GORM DB instance
func (m *MySQL) GetDB() *gorm.DB {
	return m.db
}

// Close closes the database connection
func (m *MySQL) Close() error {
	sqlDB, err := m.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Health checks the database connection health
func (m *MySQL) Health() error {
	sqlDB, err := m.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}

