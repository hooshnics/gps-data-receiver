package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gps-data-receiver/internal/storage"
	"github.com/gps-data-receiver/pkg/logger"
	"go.uber.org/zap"
)

// QueryGPSPath handles GET /api/gps/path
// Query params:
// - date: required, YYYY-MM-DD (Gregorian; interpreted in Asia/Tehran timezone like other endpoints)
// - imei: required, 15 digits
func (h *Handler) QueryGPSPath(c *gin.Context) {
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
	if imei == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "imei query parameter is required"})
		return
	}
	if !imeiQueryPattern.MatchString(imei) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "imei must be exactly 15 digits"})
		return
	}

	filter := storage.PathQueryFilter{
		DateStart: day,
		DateEnd:   day.Add(24 * time.Hour),
		IMEI:      imei,
		Limit:     50000,
	}

	points, err := h.store.QueryPathPoints(c.Request.Context(), filter)
	if err != nil {
		logger.Error("Failed to query GPS path points", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query path points"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"points": points,
		"count":  len(points),
		"date":   dateStr,
		"imei":   imei,
	})
}
