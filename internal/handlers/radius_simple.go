package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// SimpleRADIUSHandler handles FreeRADIUS integration with simplified API
type SimpleRADIUSHandler struct {
	logger *zap.Logger
}

// NewSimpleRADIUSHandler creates a new simple RADIUS handler
func NewSimpleRADIUSHandler(logger *zap.Logger) *SimpleRADIUSHandler {
	return &SimpleRADIUSHandler{
		logger: logger,
	}
}

// SimpleAuthorizeRequest represents basic RADIUS authorization request
type SimpleAuthorizeRequest struct {
	Username      string `json:"username"`
	Password      string `json:"password,omitempty"`
	NASIPAddress  string `json:"nas_ip_address"`
	NASIdentifier string `json:"nas_identifier"`
	AuthType      string `json:"auth_type"`
}

// SimpleAuthorizeResponse represents basic RADIUS authorization response
type SimpleAuthorizeResponse struct {
	Result     string            `json:"result"`     // accept, reject
	Attributes map[string]string `json:"attributes"` // Reply attributes
}

// SimpleAccountingRequest represents basic RADIUS accounting request
type SimpleAccountingRequest struct {
	Username         string `json:"username"`
	SessionID        string `json:"session_id"`
	AcctStatusType   string `json:"acct_status_type"`
	AcctInputOctets  int64  `json:"acct_input_octets"`
	AcctOutputOctets int64  `json:"acct_output_octets"`
	AcctSessionTime  int    `json:"acct_session_time"`
	FramedIPAddress  string `json:"framed_ip_address"`
}

// SimpleAccountingResponse represents basic RADIUS accounting response
type SimpleAccountingResponse struct {
	Result string `json:"result"` // accept, reject
}

// Authorize handles FreeRADIUS authorization requests
func (h *SimpleRADIUSHandler) Authorize(c *gin.Context) {
	var req SimpleAuthorizeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid authorization request", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.logger.Info("RADIUS authorization request",
		zap.String("username", req.Username),
		zap.String("nas_ip", req.NASIPAddress),
		zap.String("auth_type", req.AuthType))

	// Simple user validation (in real implementation - check database)
	if req.Username == "" {
		c.JSON(http.StatusOK, SimpleAuthorizeResponse{
			Result: "reject",
		})
		return
	}

	// Return success with basic attributes
	response := SimpleAuthorizeResponse{
		Result: "accept",
		Attributes: map[string]string{
			"Cleartext-Password": "test123", // From database
			"Service-Type":       "Framed-User",
			"Framed-Protocol":    "PPP",
		},
	}

	c.JSON(http.StatusOK, response)
}

// Accounting handles FreeRADIUS accounting requests
func (h *SimpleRADIUSHandler) Accounting(c *gin.Context) {
	var req SimpleAccountingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid accounting request", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.logger.Info("RADIUS accounting request",
		zap.String("username", req.Username),
		zap.String("session_id", req.SessionID),
		zap.String("status_type", req.AcctStatusType))

	// Process accounting based on type
	switch req.AcctStatusType {
	case "Start":
		h.logger.Info("Session started",
			zap.String("username", req.Username),
			zap.String("session_id", req.SessionID),
			zap.String("ip", req.FramedIPAddress))

	case "Stop":
		h.logger.Info("Session stopped",
			zap.String("username", req.Username),
			zap.String("session_id", req.SessionID),
			zap.Int64("in_octets", req.AcctInputOctets),
			zap.Int64("out_octets", req.AcctOutputOctets),
			zap.Int("session_time", req.AcctSessionTime))

	case "Interim-Update":
		h.logger.Debug("Session update",
			zap.String("session_id", req.SessionID),
			zap.Int64("in_octets", req.AcctInputOctets),
			zap.Int64("out_octets", req.AcctOutputOctets))
	}

	c.JSON(http.StatusOK, SimpleAccountingResponse{
		Result: "accept",
	})
}

// PostAuth handles FreeRADIUS post-authentication requests
func (h *SimpleRADIUSHandler) PostAuth(c *gin.Context) {
	var req SimpleAuthorizeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid post-auth request", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.logger.Info("RADIUS post-auth request",
		zap.String("username", req.Username),
		zap.String("auth_type", req.AuthType))

	c.JSON(http.StatusOK, gin.H{"result": "accept"})
}

// RegisterSimpleRADIUSRoutes registers simplified RADIUS routes
func (h *SimpleRADIUSHandler) RegisterRoutes(router *gin.RouterGroup) {
	radius := router.Group("/radius")
	{
		radius.POST("/authorize", h.Authorize)
		radius.POST("/post-auth", h.PostAuth)
		radius.POST("/accounting", h.Accounting)

		// Health check for FreeRADIUS integration
		radius.GET("/health", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"status":    "ok",
				"service":   "netspire-radius-rest",
				"timestamp": time.Now().Unix(),
				"auth_methods": []string{
					"PAP", "CHAP", "MS-CHAP-v2", "EAP-MD5",
				},
			})
		})

		// Info endpoint
		radius.GET("/info", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"name":        "Netspire RADIUS REST API",
				"version":     "1.0.0",
				"description": "FreeRADIUS integration for Netspire billing system",
				"endpoints": []string{
					"/radius/authorize",
					"/radius/accounting",
					"/radius/post-auth",
					"/radius/health",
				},
				"supported_auth": []string{
					"PAP (Password Authentication Protocol)",
					"CHAP (Challenge Handshake Authentication Protocol)",
					"MS-CHAP-v2 (Microsoft CHAP version 2)",
					"EAP-MD5 (Extensible Authentication Protocol - MD5)",
				},
			})
		})
	}
}
