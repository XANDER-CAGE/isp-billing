package handlers

import (
	"net"
	"net/http"

	"isp-billing/internal/models"
	"isp-billing/internal/services/ippool"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// IPPoolHandler handles IP pool HTTP requests
// Equivalent to hooks in mod_ippool.erl: ippool_lease_ip, ippool_renew_ip, ippool_release_ip
type IPPoolHandler struct {
	ipPool *ippool.Service
	logger *zap.Logger
}

// NewIPPoolHandler creates a new IP pool handler
func NewIPPoolHandler(ipPoolService *ippool.Service, logger *zap.Logger) *IPPoolHandler {
	return &IPPoolHandler{
		ipPool: ipPoolService,
		logger: logger,
	}
}

// LeaseIP handles IP lease requests from FreeRADIUS
// Equivalent to add_framed_ip/1 in mod_ippool.erl
// POST /api/v1/ippool/lease
func (h *IPPoolHandler) LeaseIP(c *gin.Context) {
	var req models.IPPoolRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid lease request", zap.Error(err))
		c.JSON(http.StatusBadRequest, models.IPPoolResponse{
			Success: false,
			Error:   "Invalid request format",
		})
		return
	}

	// Determine pool name - either from request or use default
	poolName := req.Pool
	if poolName == "" {
		poolName = "main" // Default pool like in Erlang
	}

	// Lease IP from pool
	ip, err := h.ipPool.Lease(poolName)
	if err != nil {
		h.logger.Warn("Failed to lease IP",
			zap.String("pool", poolName),
			zap.String("username", req.Username),
			zap.Error(err))

		c.JSON(http.StatusServiceUnavailable, models.IPPoolResponse{
			Success: false,
			Error:   "No available IPs in pool",
		})
		return
	}

	h.logger.Info("IP leased successfully",
		zap.String("ip", ip.String()),
		zap.String("pool", poolName),
		zap.String("username", req.Username),
		zap.String("sid", req.SID))

	c.JSON(http.StatusOK, models.IPPoolResponse{
		Success: true,
		IP:      ip.String(),
		Pool:    poolName,
		Message: "IP leased successfully",
	})
}

// RenewIP handles IP renewal requests from FreeRADIUS
// Equivalent to renew_framed_ip/1 in mod_ippool.erl
// POST /api/v1/ippool/renew
func (h *IPPoolHandler) RenewIP(c *gin.Context) {
	var req models.IPPoolRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid renew request", zap.Error(err))
		c.JSON(http.StatusBadRequest, models.IPPoolResponse{
			Success: false,
			Error:   "Invalid request format",
		})
		return
	}

	if req.IP == "" {
		c.JSON(http.StatusBadRequest, models.IPPoolResponse{
			Success: false,
			Error:   "IP address is required",
		})
		return
	}

	ip := net.ParseIP(req.IP)
	if ip == nil {
		c.JSON(http.StatusBadRequest, models.IPPoolResponse{
			Success: false,
			Error:   "Invalid IP address format",
		})
		return
	}

	// Renew IP lease
	err := h.ipPool.Renew(ip)
	if err != nil {
		h.logger.Warn("Failed to renew IP",
			zap.String("ip", ip.String()),
			zap.String("username", req.Username),
			zap.Error(err))

		c.JSON(http.StatusNotFound, models.IPPoolResponse{
			Success: false,
			Error:   "IP not found or renewal failed",
		})
		return
	}

	h.logger.Info("IP renewed successfully",
		zap.String("ip", ip.String()),
		zap.String("username", req.Username),
		zap.String("sid", req.SID))

	c.JSON(http.StatusOK, models.IPPoolResponse{
		Success: true,
		IP:      ip.String(),
		Message: "IP renewed successfully",
	})
}

// ReleaseIP handles IP release requests from FreeRADIUS
// Equivalent to release_framed_ip/1 in mod_ippool.erl
// POST /api/v1/ippool/release
func (h *IPPoolHandler) ReleaseIP(c *gin.Context) {
	var req models.IPPoolRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid release request", zap.Error(err))
		c.JSON(http.StatusBadRequest, models.IPPoolResponse{
			Success: false,
			Error:   "Invalid request format",
		})
		return
	}

	if req.IP == "" {
		c.JSON(http.StatusBadRequest, models.IPPoolResponse{
			Success: false,
			Error:   "IP address is required",
		})
		return
	}

	ip := net.ParseIP(req.IP)
	if ip == nil {
		c.JSON(http.StatusBadRequest, models.IPPoolResponse{
			Success: false,
			Error:   "Invalid IP address format",
		})
		return
	}

	// Release IP back to pool
	err := h.ipPool.Release(ip)
	if err != nil {
		h.logger.Warn("Failed to release IP",
			zap.String("ip", ip.String()),
			zap.String("username", req.Username),
			zap.Error(err))

		// Don't return error for release failures (like Erlang version)
		h.logger.Debug("Release failed, but continuing", zap.Error(err))
	}

	h.logger.Info("IP released successfully",
		zap.String("ip", ip.String()),
		zap.String("username", req.Username),
		zap.String("sid", req.SID))

	c.JSON(http.StatusOK, models.IPPoolResponse{
		Success: true,
		IP:      ip.String(),
		Message: "IP released successfully",
	})
}

// GetPoolInfo returns information about IP pools
// Equivalent to info/0 in mod_ippool.erl
// GET /api/v1/ippool/info
func (h *IPPoolHandler) GetPoolInfo(c *gin.Context) {
	entries, err := h.ipPool.Info()
	if err != nil {
		h.logger.Error("Failed to get pool info", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to retrieve pool information",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"pools": entries,
		"count": len(entries),
	})
}

// GetPoolStats returns statistics for IP pools
// GET /api/v1/ippool/stats or /api/v1/ippool/stats/:pool
func (h *IPPoolHandler) GetPoolStats(c *gin.Context) {
	poolName := c.Param("pool")

	stats, err := h.ipPool.GetStats(poolName)
	if err != nil {
		h.logger.Error("Failed to get pool stats",
			zap.String("pool", poolName),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to retrieve pool statistics",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"stats": stats,
	})
}

// CleanupExpired manually triggers cleanup of expired IP leases
// POST /api/v1/ippool/cleanup
func (h *IPPoolHandler) CleanupExpired(c *gin.Context) {
	err := h.ipPool.CleanupExpiredIPs()
	if err != nil {
		h.logger.Error("Failed to cleanup expired IPs", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to cleanup expired IPs",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Expired IPs cleanup completed",
	})
}
