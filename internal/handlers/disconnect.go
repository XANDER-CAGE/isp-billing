package handlers

import (
	"net"
	"net/http"

	"netspire-go/internal/services/disconnect"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// DisconnectHandler handles disconnect HTTP requests
type DisconnectHandler struct {
	disconnect *disconnect.Service
	logger     *zap.Logger
}

// DisconnectRequest represents a disconnect request
type DisconnectRequest struct {
	Username string                 `json:"username,omitempty"`
	SID      string                 `json:"sid,omitempty"`
	IP       string                 `json:"ip,omitempty"`
	Reason   string                 `json:"reason"`
	NASSpec  map[string]interface{} `json:"nas_spec,omitempty"`
}

// DisconnectResponse represents a disconnect response
type DisconnectResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

// NewDisconnectHandler creates a new disconnect handler
func NewDisconnectHandler(disconnectService *disconnect.Service, logger *zap.Logger) *DisconnectHandler {
	return &DisconnectHandler{
		disconnect: disconnectService,
		logger:     logger,
	}
}

// DisconnectSession handles session disconnect requests
// POST /api/v1/disconnect/session
func (h *DisconnectHandler) DisconnectSession(c *gin.Context) {
	var req DisconnectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid disconnect request", zap.Error(err))
		c.JSON(http.StatusBadRequest, DisconnectResponse{
			Success: false,
			Error:   "Invalid request format",
		})
		return
	}

	// Validate required fields
	if req.Username == "" || req.SID == "" || req.IP == "" {
		c.JSON(http.StatusBadRequest, DisconnectResponse{
			Success: false,
			Error:   "Username, SID, and IP are required",
		})
		return
	}

	ip := net.ParseIP(req.IP)
	if ip == nil {
		c.JSON(http.StatusBadRequest, DisconnectResponse{
			Success: false,
			Error:   "Invalid IP address format",
		})
		return
	}

	if req.NASSpec == nil {
		req.NASSpec = make(map[string]interface{})
	}

	if req.Reason == "" {
		req.Reason = "Administrative disconnect"
	}

	// Execute disconnect
	err := h.disconnect.DisconnectSession(req.Username, req.SID, ip, req.NASSpec)
	if err != nil {
		h.logger.Error("Failed to disconnect session", zap.Error(err))
		c.JSON(http.StatusInternalServerError, DisconnectResponse{
			Success: false,
			Error:   "Failed to disconnect session: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, DisconnectResponse{
		Success: true,
		Message: "Session disconnected successfully",
	})
}

// DisconnectByIP handles disconnect by IP address
func (h *DisconnectHandler) DisconnectByIP(c *gin.Context) {
	var req DisconnectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, DisconnectResponse{
			Success: false,
			Error:   "Invalid request format",
		})
		return
	}

	if req.IP == "" {
		c.JSON(http.StatusBadRequest, DisconnectResponse{
			Success: false,
			Error:   "IP address is required",
		})
		return
	}

	ip := net.ParseIP(req.IP)
	if ip == nil {
		c.JSON(http.StatusBadRequest, DisconnectResponse{
			Success: false,
			Error:   "Invalid IP address format",
		})
		return
	}

	if req.Reason == "" {
		req.Reason = "Administrative disconnect by IP"
	}

	err := h.disconnect.DisconnectByIP(ip, req.Reason)
	if err != nil {
		c.JSON(http.StatusInternalServerError, DisconnectResponse{
			Success: false,
			Error:   "Failed to disconnect by IP: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, DisconnectResponse{
		Success: true,
		Message: "Sessions disconnected by IP successfully",
	})
}
