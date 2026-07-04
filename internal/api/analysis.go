package api

import (
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gps-data-receiver/internal/storage"
	"github.com/gps-data-receiver/pkg/logger"
	"go.uber.org/zap"
)

var imeiQueryPattern = regexp.MustCompile(`^\d{15}$`)

var tehranLocation *time.Location

func init() {
	loc, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		loc = time.FixedZone("IRST", 3*3600+30*60)
	}
	tehranLocation = loc
}

// QueryGPSRecords handles GET /api/gps/records
func (h *Handler) QueryGPSRecords(c *gin.Context) {
	if h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Database storage is not enabled"})
		return
	}

	dateStr := c.Query("date")
	if dateStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "date query parameter is required (YYYY-MM-DD)"})
		return
	}

	day, err := parseQueryDate(dateStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format, expected YYYY-MM-DD"})
		return
	}

	imei := c.Query("imei")
	if imei != "" && !imeiQueryPattern.MatchString(imei) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "imei must be exactly 15 digits"})
		return
	}

	filter := storage.QueryFilter{
		DateStart: day,
		DateEnd:   day.Add(24 * time.Hour),
		IMEI:      imei,
		Limit:     5000,
	}

	records, err := h.store.QueryRecords(c.Request.Context(), filter)
	if err != nil {
		logger.Error("Failed to query GPS records", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query records"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"records": records,
		"count":   len(records),
		"date":    dateStr,
		"imei":    imei,
	})
}

func parseQueryDate(dateStr string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", dateStr, tehranLocation)
}
