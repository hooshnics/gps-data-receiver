package storage

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	gojson "github.com/goccy/go-json"
	"github.com/gps-data-receiver/internal/parser"
	"github.com/gps-data-receiver/pkg/logger"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"
)

var imeiPattern = regexp.MustCompile(`^\d{15}$`)

// Record represents a stored GPS record row.
type Record struct {
	ID         int64             `json:"id"`
	IMEI       string            `json:"imei"`
	RawData    string            `json:"raw_data"`
	ParsedData gojson.RawMessage `json:"parsed_data"`
	CreatedAt  time.Time         `json:"created_at"`
}

// FailedRecord represents a stored failed delivery row.
type FailedRecord struct {
	ID           int64             `json:"id"`
	IMEI         string            `json:"imei"`
	RawData      string            `json:"raw_data"`
	ParsedData   gojson.RawMessage `json:"parsed_data"`
	TargetServer string            `json:"target_server"`
	ErrorMessage string            `json:"error_message"`
	FailedAt     time.Time         `json:"failed_at"`
}

// QueryFilter filters stored records by device date_time and optional IMEI.
type QueryFilter struct {
	DateStart time.Time // inclusive start of day in query timezone
	DateEnd   time.Time // exclusive end of day in query timezone
	IMEI      string
	Limit     int
}

// PostgresStore persists GPS records to PostgreSQL.
type PostgresStore struct {
	db *sql.DB
}

type gpsRecordRow struct {
	imei       string
	rawData    string
	parsedJSON []byte
}

type gpsFailedRecordRow struct {
	gpsRecordRow
	targetServer string
	errorMessage string
}

// NewPostgresStore opens a PostgreSQL connection and ensures the schema exists.
func NewPostgresStore(dsn string, maxOpenConns int) (*PostgresStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	if maxOpenConns < 1 {
		maxOpenConns = 25
	}
	db.SetMaxOpenConns(maxOpenConns)
	idleConns := maxOpenConns / 4
	if idleConns < 5 {
		idleConns = 5
	}
	db.SetMaxIdleConns(idleConns)
	db.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	store := &PostgresStore{db: db}
	if err := store.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}

	return store, nil
}

func (s *PostgresStore) migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS gps_records (
    id BIGSERIAL PRIMARY KEY,
    imei VARCHAR(15) NOT NULL,
    raw_data TEXT NOT NULL,
    parsed_data JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_gps_records_created_at ON gps_records (created_at);
CREATE INDEX IF NOT EXISTS idx_gps_records_imei_created_at ON gps_records (imei, created_at);
CREATE TABLE IF NOT EXISTS gps_failed_records (
    id BIGSERIAL PRIMARY KEY,
    imei VARCHAR(15) NOT NULL,
    raw_data TEXT NOT NULL,
    parsed_data JSONB NOT NULL,
    target_server TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    failed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_gps_failed_records_failed_at ON gps_failed_records (failed_at);
CREATE INDEX IF NOT EXISTS idx_gps_failed_records_imei_failed_at ON gps_failed_records (imei, failed_at);
`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("migrate postgres schema: %w", err)
	}
	return nil
}

func prepareGPSRecordRows(records []parser.ParsedGPSData) []gpsRecordRow {
	rows := make([]gpsRecordRow, 0, len(records))
	for _, record := range records {
		if record.IMEI == "" || !imeiPattern.MatchString(record.IMEI) {
			continue
		}

		parsedPayload := map[string]interface{}{
			"coordinate": record.Coordinate,
			"speed":      record.Speed,
			"status":     record.Status,
			"directions": record.Directions,
			"date_time":  record.DateTime,
			"imei":       record.IMEI,
		}
		parsedJSON, err := gojson.Marshal(parsedPayload)
		if err != nil {
			logger.Warn("Failed to marshal parsed GPS record for storage",
				zap.String("imei", record.IMEI),
				zap.Error(err))
			continue
		}

		rawData := record.RawData
		if rawData == "" {
			rawData = " "
		}

		rows = append(rows, gpsRecordRow{
			imei:       record.IMEI,
			rawData:    rawData,
			parsedJSON: parsedJSON,
		})
	}
	return rows
}

func execBatchInsert(ctx context.Context, tx *sql.Tx, query string, args []interface{}) error {
	if len(args) == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("batch insert: %w", err)
	}
	return nil
}

// StoreRecords inserts successfully delivered GPS records.
func (s *PostgresStore) StoreRecords(ctx context.Context, records []parser.ParsedGPSData) error {
	if s == nil || len(records) == 0 {
		return nil
	}

	rows := prepareGPSRecordRows(records)
	if len(rows) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	const columnsPerRow = 4
	valuePlaceholders := make([]string, 0, len(rows))
	args := make([]interface{}, 0, len(rows)*columnsPerRow)

	for i, row := range rows {
		base := i*columnsPerRow + 1
		valuePlaceholders = append(valuePlaceholders, fmt.Sprintf(
			"($%d, $%d, $%d, $%d)",
			base, base+1, base+2, base+3,
		))
		args = append(args, row.imei, row.rawData, row.parsedJSON, now)
	}

	query := `INSERT INTO gps_records (imei, raw_data, parsed_data, created_at) VALUES ` +
		strings.Join(valuePlaceholders, ", ")

	if err := execBatchInsert(ctx, tx, query, args); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// QueryRecords returns stored records for the given filter.
func (s *PostgresStore) QueryRecords(ctx context.Context, filter QueryFilter) ([]Record, error) {
	if s == nil {
		return nil, fmt.Errorf("postgres store not initialized")
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 5000
	}

	dateStart := filter.DateStart.Format("2006-01-02") + " 00:00:00"
	dateEnd := filter.DateEnd.Format("2006-01-02") + " 00:00:00"

	query := `
SELECT id, imei, raw_data, parsed_data, created_at
FROM gps_records
WHERE parsed_data->>'date_time' >= $1 AND parsed_data->>'date_time' < $2
`
	args := []interface{}{dateStart, dateEnd}

	if filter.IMEI != "" {
		query += ` AND imei = $3`
		args = append(args, filter.IMEI)
	}

	query += fmt.Sprintf(` ORDER BY created_at DESC LIMIT %d`, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query records: %w", err)
	}
	defer rows.Close()

	records := make([]Record, 0)
	for rows.Next() {
		var rec Record
		if err := rows.Scan(&rec.ID, &rec.IMEI, &rec.RawData, &rec.ParsedData, &rec.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan record: %w", err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate records: %w", err)
	}

	return records, nil
}

func prepareGPSFailedRecordRows(records []parser.ParsedGPSData, targetServer, errorMessage string) []gpsFailedRecordRow {
	baseRows := prepareGPSRecordRows(records)
	rows := make([]gpsFailedRecordRow, 0, len(baseRows))
	for _, row := range baseRows {
		rows = append(rows, gpsFailedRecordRow{
			gpsRecordRow: row,
			targetServer: targetServer,
			errorMessage: errorMessage,
		})
	}
	return rows
}

// StoreFailedRecords inserts GPS records that failed delivery.
func (s *PostgresStore) StoreFailedRecords(ctx context.Context, records []parser.ParsedGPSData, targetServer, errorMessage string) error {
	if s == nil || len(records) == 0 {
		return nil
	}

	rows := prepareGPSFailedRecordRows(records, targetServer, errorMessage)
	if len(rows) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	const columnsPerRow = 6
	valuePlaceholders := make([]string, 0, len(rows))
	args := make([]interface{}, 0, len(rows)*columnsPerRow)

	for i, row := range rows {
		base := i*columnsPerRow + 1
		valuePlaceholders = append(valuePlaceholders, fmt.Sprintf(
			"($%d, $%d, $%d, $%d, $%d, $%d)",
			base, base+1, base+2, base+3, base+4, base+5,
		))
		args = append(args, row.imei, row.rawData, row.parsedJSON, row.targetServer, row.errorMessage, now)
	}

	query := `INSERT INTO gps_failed_records (imei, raw_data, parsed_data, target_server, error_message, failed_at) VALUES ` +
		strings.Join(valuePlaceholders, ", ")

	if err := execBatchInsert(ctx, tx, query, args); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// QueryFailedRecords returns failed delivery records for the given filter.
func (s *PostgresStore) QueryFailedRecords(ctx context.Context, filter QueryFilter) ([]FailedRecord, error) {
	if s == nil {
		return nil, fmt.Errorf("postgres store not initialized")
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 5000
	}

	dateStart := filter.DateStart.Format("2006-01-02") + " 00:00:00"
	dateEnd := filter.DateEnd.Format("2006-01-02") + " 00:00:00"

	query := `
SELECT id, imei, raw_data, parsed_data, target_server, error_message, failed_at
FROM gps_failed_records
WHERE parsed_data->>'date_time' >= $1 AND parsed_data->>'date_time' < $2
`
	args := []interface{}{dateStart, dateEnd}

	if filter.IMEI != "" {
		query += ` AND imei = $3`
		args = append(args, filter.IMEI)
	}

	query += fmt.Sprintf(` ORDER BY failed_at DESC LIMIT %d`, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed records: %w", err)
	}
	defer rows.Close()

	records := make([]FailedRecord, 0)
	for rows.Next() {
		var rec FailedRecord
		if err := rows.Scan(&rec.ID, &rec.IMEI, &rec.RawData, &rec.ParsedData, &rec.TargetServer, &rec.ErrorMessage, &rec.FailedAt); err != nil {
			return nil, fmt.Errorf("scan failed record: %w", err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate failed records: %w", err)
	}

	return records, nil
}

// Close closes the database connection.
func (s *PostgresStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}
