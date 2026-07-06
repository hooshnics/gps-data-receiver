package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gps-data-receiver/internal/storage"
	"github.com/gps-data-receiver/pkg/logger"
	"go.uber.org/zap"
)

// QueryInvalidGPSRecords handles GET /api/gps/invalid-records
func (h *Handler) QueryInvalidGPSRecords(c *gin.Context) {
	if h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Database storage is not enabled"})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))

	filter := storage.PaginatedQueryFilter{
		Page:  page,
		Limit: limit,
	}

	if dateStr := c.Query("date"); dateStr != "" {
		day, err := parseQueryDate(dateStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format, expected YYYY-MM-DD"})
			return
		}
		filter.DateStart = day
		filter.DateEnd = day.Add(24 * time.Hour)
	}

	result, err := h.store.QueryInvalidRecords(c.Request.Context(), filter)
	if err != nil {
		logger.Error("Failed to query invalid GPS records", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query invalid records"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"records": result.Records,
		"total":   result.Total,
		"page":    result.Page,
		"limit":   result.Limit,
	})
}
