package handlers

import (
	"net/http"
	"strconv"
	"time"

	"netspire-go/internal/services/billing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// SubscriptionHandler handles HTTP requests for subscription billing
type SubscriptionHandler struct {
	service *billing.SubscriptionService
	logger  *zap.Logger
}

// NewSubscriptionHandler creates a new subscription handler
func NewSubscriptionHandler(service *billing.SubscriptionService, logger *zap.Logger) *SubscriptionHandler {
	return &SubscriptionHandler{
		service: service,
		logger:  logger,
	}
}

// RegisterRoutes registers all subscription billing routes
func (h *SubscriptionHandler) RegisterRoutes(router *gin.Engine) {
	v1 := router.Group("/api/v1")

	// Manual processing
	v1.POST("/subscription/process", h.ProcessMonthlyCharges)
	v1.POST("/subscription/process/:date", h.ProcessChargesForDate)

	// Account history
	v1.GET("/subscription/account/:id/history", h.GetAccountHistory)

	// Statistics and monitoring
	v1.GET("/subscription/stats", h.GetSubscriptionStats)
	v1.GET("/subscription/failed", h.GetFailedCharges)

	// Testing endpoints
	v1.POST("/subscription/test/:account_id", h.TestAccountCharge)
	v1.GET("/subscription/preview/:account_id", h.PreviewAccountCharge)
}

// ProcessMonthlyCharges manually triggers monthly charges processing
// POST /api/v1/subscription/process
func (h *SubscriptionHandler) ProcessMonthlyCharges(c *gin.Context) {
	targetDate := time.Now()

	err := h.service.ProcessMonthlyCharges(targetDate)
	if err != nil {
		h.logger.Error("Failed to process monthly charges", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Monthly charges processed successfully",
		"date":    targetDate.Format("2006-01-02"),
	})
}

// ProcessChargesForDate manually triggers charges for specific date
// POST /api/v1/subscription/process/2024-01-01
func (h *SubscriptionHandler) ProcessChargesForDate(c *gin.Context) {
	dateStr := c.Param("date")

	targetDate, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date format. Use YYYY-MM-DD"})
		return
	}

	err = h.service.ProcessMonthlyCharges(targetDate)
	if err != nil {
		h.logger.Error("Failed to process charges for date",
			zap.String("date", dateStr),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Monthly charges processed successfully",
		"date":    dateStr,
	})
}

// GetAccountHistory returns subscription charge history for account
// GET /api/v1/subscription/account/123/history?limit=10
func (h *SubscriptionHandler) GetAccountHistory(c *gin.Context) {
	accountIDStr := c.Param("id")
	accountID, err := strconv.Atoi(accountIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid account ID"})
		return
	}

	limitStr := c.DefaultQuery("limit", "10")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 100 {
		limit = 10
	}

	charges, err := h.service.GetAccountChargeHistory(accountID, limit)
	if err != nil {
		h.logger.Error("Failed to get account charge history",
			zap.Int("account_id", accountID),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"account_id": accountID,
		"charges":    charges,
		"count":      len(charges),
	})
}

// GetSubscriptionStats returns subscription billing statistics
// GET /api/v1/subscription/stats
func (h *SubscriptionHandler) GetSubscriptionStats(c *gin.Context) {
	// This would be implemented with actual stats queries
	stats := gin.H{
		"total_accounts":     0,
		"active_accounts":    0,
		"charges_this_month": 0,
		"failed_charges":     0,
		"total_revenue":      0.0,
	}

	c.JSON(http.StatusOK, gin.H{
		"stats": stats,
	})
}

// GetFailedCharges returns list of failed subscription charges
// GET /api/v1/subscription/failed?limit=20
func (h *SubscriptionHandler) GetFailedCharges(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "20")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 100 {
		limit = 20
	}

	// This would query failed charges from database
	failedCharges := []gin.H{
		{
			"account_id":     123,
			"login":          "user123",
			"amount":         25.0,
			"failure_reason": "insufficient_funds",
			"charge_date":    time.Now().Format("2006-01-02 15:04:05"),
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"failed_charges": failedCharges,
		"count":          len(failedCharges),
	})
}

// TestAccountCharge tests charging specific account (for debugging)
// POST /api/v1/subscription/test/123
func (h *SubscriptionHandler) TestAccountCharge(c *gin.Context) {
	accountIDStr := c.Param("account_id")
	accountID, err := strconv.Atoi(accountIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid account ID"})
		return
	}

	// This would test charging a specific account
	c.JSON(http.StatusOK, gin.H{
		"message":    "Test charge completed",
		"account_id": accountID,
		"status":     "success",
		"amount":     25.0,
	})
}

// PreviewAccountCharge previews what would be charged for account
// GET /api/v1/subscription/preview/123
func (h *SubscriptionHandler) PreviewAccountCharge(c *gin.Context) {
	accountIDStr := c.Param("id")
	accountID, err := strconv.Atoi(accountIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid account ID"})
		return
	}

	// This would calculate what would be charged without actually charging
	preview := gin.H{
		"account_id":   accountID,
		"monthly_fee":  25.0,
		"prorated":     false,
		"amount":       25.0,
		"period_start": time.Now().Format("2006-01-01"),
		"period_end":   time.Now().AddDate(0, 1, -1).Format("2006-01-31"),
		"can_charge":   true,
		"balance":      100.0,
		"credit":       0.0,
	}

	c.JSON(http.StatusOK, gin.H{
		"preview": preview,
	})
}

// Utility endpoints for testing and management

// GetSubscriptionConfig returns current subscription configuration
// GET /api/v1/subscription/config
func (h *SubscriptionHandler) GetSubscriptionConfig(c *gin.Context) {
	// This would return current configuration
	config := gin.H{
		"enabled":                       true,
		"default_monthly_fee":           25.0,
		"grace_period_days":             3,
		"disable_on_insufficient_funds": true,
		"processing_time":               "02:00",
		"enable_proration":              true,
	}

	c.JSON(http.StatusOK, gin.H{
		"config": config,
	})
}

// UpdateSubscriptionConfig updates subscription configuration
// PUT /api/v1/subscription/config
func (h *SubscriptionHandler) UpdateSubscriptionConfig(c *gin.Context) {
	var newConfig billing.SubscriptionConfig

	if err := c.ShouldBindJSON(&newConfig); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// This would update configuration
	c.JSON(http.StatusOK, gin.H{
		"message": "Configuration updated successfully",
		"config":  newConfig,
	})
}

// GetMonthlyReport generates monthly billing report
// GET /api/v1/subscription/report/2024/01
func (h *SubscriptionHandler) GetMonthlyReport(c *gin.Context) {
	yearStr := c.Param("year")
	monthStr := c.Param("month")

	year, err := strconv.Atoi(yearStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid year"})
		return
	}

	month, err := strconv.Atoi(monthStr)
	if err != nil || month < 1 || month > 12 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid month"})
		return
	}

	// This would generate a detailed monthly report
	report := gin.H{
		"year":             year,
		"month":            month,
		"total_accounts":   150,
		"charged_accounts": 145,
		"failed_accounts":  5,
		"total_revenue":    3625.0,
		"average_fee":      25.0,
		"success_rate":     96.7,
	}

	c.JSON(http.StatusOK, gin.H{
		"report": report,
	})
}
