package handlers

import (
	"net/http"
	"strconv"

	"netspire-go/internal/models"
	"netspire-go/internal/services/tclass"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// TClassHandler handles HTTP requests for traffic classification
type TClassHandler struct {
	tclassService *tclass.Service
	logger        *zap.Logger
}

// NewTClassHandler creates a new traffic classification handler
func NewTClassHandler(tclassService *tclass.Service, logger *zap.Logger) *TClassHandler {
	return &TClassHandler{
		tclassService: tclassService,
		logger:        logger,
	}
}

// RegisterRoutes registers all traffic classification routes
func (h *TClassHandler) RegisterRoutes(router *gin.Engine) {
	v1 := router.Group("/api/v1")

	// Classification operations
	v1.GET("/tclass/classify/:ip", h.ClassifyIP)
	v1.POST("/tclass/classify", h.ClassifyBatch)
	v1.GET("/tclass/classify/:ip/default/:default", h.ClassifyWithDefault)

	// Class management
	v1.GET("/tclass/classes", h.GetAllClasses)
	v1.GET("/tclass/classes/:name", h.GetClass)
	v1.POST("/tclass/classes", h.AddClass)
	v1.PUT("/tclass/classes/:name", h.UpdateClass)
	v1.DELETE("/tclass/classes/:name", h.RemoveClass)

	// Tree management
	v1.GET("/tclass/tree/stats", h.GetTreeStats)
	v1.GET("/tclass/tree/ranges", h.GetAllRanges)
	v1.GET("/tclass/tree/path/:ip", h.GetClassificationPath)

	// Configuration management
	v1.POST("/tclass/reload", h.ReloadConfig)
	v1.POST("/tclass/load", h.LoadConfig)

	// Debug and utilities
	v1.POST("/tclass/validate/ip", h.ValidateIP)
	v1.POST("/tclass/validate/config", h.ValidateConfig)
}

// ClassifyIP classifies a single IP address
// GET /api/v1/tclass/classify/:ip
func (h *TClassHandler) ClassifyIP(c *gin.Context) {
	ip := c.Param("ip")

	result, err := h.tclassService.Classify(ip)
	if err != nil {
		h.logger.Error("Failed to classify IP", zap.String("ip", ip), zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ip":     ip,
		"result": result,
	})
}

// ClassifyBatch classifies multiple IP addresses
// POST /api/v1/tclass/classify
func (h *TClassHandler) ClassifyBatch(c *gin.Context) {
	var req struct {
		IPs []string `json:"ips" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	results := make(map[string]*models.ClassificationResult)
	errors := make(map[string]string)

	for _, ip := range req.IPs {
		result, err := h.tclassService.Classify(ip)
		if err != nil {
			errors[ip] = err.Error()
		} else {
			results[ip] = result
		}
	}

	response := gin.H{
		"results": results,
	}

	if len(errors) > 0 {
		response["errors"] = errors
	}

	c.JSON(http.StatusOK, response)
}

// ClassifyWithDefault classifies IP with fallback to default class
// GET /api/v1/tclass/classify/:ip/default/:default
func (h *TClassHandler) ClassifyWithDefault(c *gin.Context) {
	ip := c.Param("ip")
	defaultClass := c.Param("default")

	result, err := h.tclassService.ClassifyWithDefault(ip, defaultClass)
	if err != nil {
		h.logger.Error("Failed to classify IP with default",
			zap.String("ip", ip),
			zap.String("default", defaultClass),
			zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ip":            ip,
		"default_class": defaultClass,
		"result":        result,
	})
}

// GetAllClasses returns all configured traffic classes
// GET /api/v1/tclass/classes
func (h *TClassHandler) GetAllClasses(c *gin.Context) {
	classes := h.tclassService.GetAllClasses()

	c.JSON(http.StatusOK, gin.H{
		"classes": classes,
		"count":   len(classes),
	})
}

// GetClass returns specific traffic class by name
// GET /api/v1/tclass/classes/:name
func (h *TClassHandler) GetClass(c *gin.Context) {
	name := c.Param("name")

	class, exists := h.tclassService.GetClass(name)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Class not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"class": class,
	})
}

// AddClass adds a new traffic class
// POST /api/v1/tclass/classes
func (h *TClassHandler) AddClass(c *gin.Context) {
	var class models.TrafficClassRule

	if err := c.ShouldBindJSON(&class); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.tclassService.AddClass(&class); err != nil {
		h.logger.Error("Failed to add traffic class",
			zap.String("name", class.Name),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Traffic class added successfully",
		"class":   class,
	})
}

// UpdateClass updates an existing traffic class
// PUT /api/v1/tclass/classes/:name
func (h *TClassHandler) UpdateClass(c *gin.Context) {
	name := c.Param("name")
	var class models.TrafficClassRule

	if err := c.ShouldBindJSON(&class); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Ensure the name matches the URL parameter
	class.Name = name

	if err := h.tclassService.AddClass(&class); err != nil {
		h.logger.Error("Failed to update traffic class",
			zap.String("name", name),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Traffic class updated successfully",
		"class":   class,
	})
}

// RemoveClass removes a traffic class
// DELETE /api/v1/tclass/classes/:name
func (h *TClassHandler) RemoveClass(c *gin.Context) {
	name := c.Param("name")

	if err := h.tclassService.RemoveClass(name); err != nil {
		h.logger.Error("Failed to remove traffic class",
			zap.String("name", name),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Traffic class removed successfully",
		"name":    name,
	})
}

// GetTreeStats returns statistics about the classification tree
// GET /api/v1/tclass/tree/stats
func (h *TClassHandler) GetTreeStats(c *gin.Context) {
	stats := h.tclassService.GetTreeStats()

	c.JSON(http.StatusOK, gin.H{
		"stats": stats,
	})
}

// GetAllRanges returns all IP ranges in the classification tree
// GET /api/v1/tclass/tree/ranges
func (h *TClassHandler) GetAllRanges(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "100")
	offsetStr := c.DefaultQuery("offset", "0")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 1000 {
		limit = 100
	}

	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	allRanges := h.tclassService.ListAllRanges()

	// Apply pagination
	total := len(allRanges)
	end := offset + limit
	if end > total {
		end = total
	}

	ranges := allRanges[offset:end]

	c.JSON(http.StatusOK, gin.H{
		"ranges": ranges,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// GetClassificationPath returns the search path for IP classification (debugging)
// GET /api/v1/tclass/tree/path/:ip
func (h *TClassHandler) GetClassificationPath(c *gin.Context) {
	ip := c.Param("ip")

	path, err := h.tclassService.GetClassificationPath(ip)
	if err != nil {
		h.logger.Error("Failed to get classification path",
			zap.String("ip", ip),
			zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ip":   ip,
		"path": path,
	})
}

// ReloadConfig reloads traffic classification configuration
// POST /api/v1/tclass/reload
func (h *TClassHandler) ReloadConfig(c *gin.Context) {
	if err := h.tclassService.Reload(); err != nil {
		h.logger.Error("Failed to reload configuration", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Configuration reloaded successfully",
	})
}

// LoadConfig loads traffic classification configuration from request
// POST /api/v1/tclass/load
func (h *TClassHandler) LoadConfig(c *gin.Context) {
	var config models.TrafficClassConfig

	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.tclassService.LoadFromConfig(&config); err != nil {
		h.logger.Error("Failed to load configuration", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Configuration loaded successfully",
		"classes": len(config.Classes),
	})
}

// ValidateIP validates if a string is a valid IP address
// POST /api/v1/tclass/validate/ip
func (h *TClassHandler) ValidateIP(c *gin.Context) {
	var req struct {
		IP string `json:"ip" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := tclass.ValidateIPAddress(req.IP); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"ip":    req.IP,
			"valid": false,
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ip":    req.IP,
		"valid": true,
	})
}

// ValidateConfig validates traffic classification configuration
// POST /api/v1/tclass/validate/config
func (h *TClassHandler) ValidateConfig(c *gin.Context) {
	var config models.TrafficClassConfig

	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := models.ValidateConfiguration(&config); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"valid": false,
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"valid":   true,
		"classes": len(config.Classes),
	})
}

// Utility endpoints for testing

// GetClassificationExample provides example requests for testing
// GET /api/v1/tclass/example
func (h *TClassHandler) GetClassificationExample(c *gin.Context) {
	example := gin.H{
		"classify_single": gin.H{
			"method": "GET",
			"url":    "/api/v1/tclass/classify/192.168.1.10",
		},
		"classify_batch": gin.H{
			"method": "POST",
			"url":    "/api/v1/tclass/classify",
			"body": gin.H{
				"ips": []string{"192.168.1.10", "8.8.8.8", "10.0.0.1"},
			},
		},
		"add_class": gin.H{
			"method": "POST",
			"url":    "/api/v1/tclass/classes",
			"body": gin.H{
				"name":     "local",
				"networks": []string{"192.168.0.0/16", "10.0.0.0/8"},
				"priority": 1,
				"cost_in":  0.005,
				"cost_out": 0.005,
			},
		},
		"load_config": gin.H{
			"method": "POST",
			"url":    "/api/v1/tclass/load",
			"body": gin.H{
				"classes": []gin.H{
					{
						"name":     "local",
						"networks": []string{"192.168.0.0/16", "10.0.0.0/8"},
						"cost_in":  0.005,
						"cost_out": 0.005,
					},
					{
						"name":     "internet",
						"networks": []string{"0.0.0.0/0"},
						"cost_in":  0.01,
						"cost_out": 0.01,
					},
				},
			},
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"examples": example,
	})
}
