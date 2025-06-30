package handlers

import (
	"net"
	"net/http"
	"strconv"
	"strings"

	"isp-billing/internal/models"
	"isp-billing/internal/services/session"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// SessionHandler handles HTTP requests for session management
type SessionHandler struct {
	sessionService *session.Service
	logger         *zap.Logger
}

// NewSessionHandler creates a new session handler
func NewSessionHandler(sessionService *session.Service, logger *zap.Logger) *SessionHandler {
	return &SessionHandler{
		sessionService: sessionService,
		logger:         logger,
	}
}

// RegisterRoutes registers all session management routes
func (h *SessionHandler) RegisterRoutes(router *gin.Engine) {
	v1 := router.Group("/api/v1")

	// Session lifecycle
	v1.POST("/session/init", h.InitSession)
	v1.POST("/session/prepare", h.PrepareSession)
	v1.POST("/session/start", h.StartSession)
	v1.POST("/session/interim", h.InterimUpdate)
	v1.POST("/session/stop", h.StopSession)
	v1.POST("/session/expire", h.ExpireSession)

	// Session queries
	v1.GET("/session/ip/:ip", h.GetSessionByIP)
	v1.GET("/session/username/:username", h.GetSessionByUsername)
	v1.GET("/session/sid/:sid", h.GetSessionBySID)
	v1.GET("/sessions", h.GetAllSessions)
	v1.GET("/sessions/stats", h.GetSessionStats)

	// NetFlow integration
	v1.POST("/session/netflow", h.HandleNetFlow)
}

// InitSession initializes a new session for a user
// POST /api/v1/session/init
func (h *SessionHandler) InitSession(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	session, err := h.sessionService.InitSession(req.Username)
	if err != nil {
		h.logger.Error("Failed to init session", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"session": session,
		"message": "Session initialized successfully",
	})
}

// PrepareSession prepares session with context data
// POST /api/v1/session/prepare
func (h *SessionHandler) PrepareSession(c *gin.Context) {
	var req struct {
		SessionUUID string                 `json:"session_uuid" binding:"required"`
		AccountID   int                    `json:"account_id" binding:"required"`
		Username    string                 `json:"username" binding:"required"`
		Password    string                 `json:"password"`
		PlanID      int                    `json:"plan_id" binding:"required"`
		PlanData    map[string]interface{} `json:"plan_data"`
		Currency    int                    `json:"currency"`
		Balance     float64                `json:"balance"`
		AuthAlgo    string                 `json:"auth_algo"`
		AcctAlgo    string                 `json:"acct_algo"`
		Replies     []models.RADIUSReply   `json:"replies"`
		NASSpec     map[string]interface{} `json:"nas_spec"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := &models.SessionContext{
		AccountID: req.AccountID,
		Username:  req.Username,
		Password:  req.Password,
		PlanID:    req.PlanID,
		PlanData:  req.PlanData,
		Currency:  req.Currency,
		Balance:   req.Balance,
		AuthAlgo:  req.AuthAlgo,
		AcctAlgo:  req.AcctAlgo,
		Replies:   req.Replies,
		NASSpec:   req.NASSpec,
	}

	if err := h.sessionService.PrepareSession(req.SessionUUID, ctx); err != nil {
		h.logger.Error("Failed to prepare session", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Session prepared successfully",
	})
}

// StartSession activates session with accounting start
// POST /api/v1/session/start
func (h *SessionHandler) StartSession(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		SID      string `json:"sid" binding:"required"`
		CID      string `json:"cid" binding:"required"`
		IP       string `json:"ip" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ip := net.ParseIP(req.IP)
	if ip == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid IP address"})
		return
	}

	if err := h.sessionService.StartSession(req.Username, req.SID, req.CID, ip); err != nil {
		h.logger.Error("Failed to start session", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Session started successfully",
	})
}

// InterimUpdate handles interim accounting updates
// POST /api/v1/session/interim
func (h *SessionHandler) InterimUpdate(c *gin.Context) {
	var req struct {
		SID string `json:"sid" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.sessionService.InterimUpdate(req.SID); err != nil {
		h.logger.Error("Failed to process interim update", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Interim update processed successfully",
	})
}

// StopSession handles accounting stop
// POST /api/v1/session/stop
func (h *SessionHandler) StopSession(c *gin.Context) {
	var req struct {
		SID string `json:"sid" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.sessionService.StopSession(req.SID); err != nil {
		h.logger.Error("Failed to stop session", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Session stop initiated successfully",
	})
}

// ExpireSession marks session as expired
// POST /api/v1/session/expire
func (h *SessionHandler) ExpireSession(c *gin.Context) {
	var req struct {
		SessionUUID string `json:"session_uuid" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.sessionService.ExpireSession(req.SessionUUID); err != nil {
		h.logger.Error("Failed to expire session", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Session expired successfully",
	})
}

// GetSessionByIP retrieves session by IP address
// GET /api/v1/session/ip/:ip
func (h *SessionHandler) GetSessionByIP(c *gin.Context) {
	ip := c.Param("ip")
	if net.ParseIP(ip) == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid IP address"})
		return
	}

	session := h.sessionService.FindSessionByIP(ip)
	if session == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Session not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"session": session,
	})
}

// GetSessionByUsername retrieves session by username
// GET /api/v1/session/username/:username
func (h *SessionHandler) GetSessionByUsername(c *gin.Context) {
	username := c.Param("username")

	session := h.sessionService.FindSessionByUsername(username)
	if session == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Session not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"session": session,
	})
}

// GetSessionBySID retrieves session by session ID
// GET /api/v1/session/sid/:sid
func (h *SessionHandler) GetSessionBySID(c *gin.Context) {
	sid := c.Param("sid")

	session := h.sessionService.FindSessionBySID(sid)
	if session == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Session not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"session": session,
	})
}

// GetAllSessions returns all active sessions
// GET /api/v1/sessions
func (h *SessionHandler) GetAllSessions(c *gin.Context) {
	// Parse query parameters
	limit := 100 // default
	offset := 0  // default

	if l := c.Query("limit"); l != "" {
		if parsedLimit, err := strconv.Atoi(l); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	if o := c.Query("offset"); o != "" {
		if parsedOffset, err := strconv.Atoi(o); err == nil && parsedOffset >= 0 {
			offset = parsedOffset
		}
	}

	sessions := h.sessionService.GetAllSessions()

	// Apply pagination
	start := offset
	end := offset + limit
	if start > len(sessions) {
		start = len(sessions)
	}
	if end > len(sessions) {
		end = len(sessions)
	}

	paginatedSessions := sessions[start:end]

	// Format response
	response := make([]map[string]interface{}, len(paginatedSessions))
	for i, session := range paginatedSessions {
		response[i] = formatSessionForResponse(session)
	}

	c.JSON(http.StatusOK, gin.H{
		"sessions": response,
		"total":    len(sessions),
		"limit":    limit,
		"offset":   offset,
	})
}

// GetSession returns a session by ID
// GET /api/v1/session/:id
func (h *SessionHandler) GetSession(c *gin.Context) {
	id := c.Param("id")

	// Try to find session by different identifiers
	var session *models.IPTrafficSession

	// First try as UUID
	if len(id) == 36 && strings.Count(id, "-") == 4 {
		// Looks like UUID, search in all sessions
		sessions := h.sessionService.GetAllSessions()
		for _, s := range sessions {
			if s.UUID == id {
				session = s
				break
			}
		}
	} else {
		// Try as SID
		session = h.sessionService.FindSessionBySID(id)
		if session == nil {
			// Try as IP
			session = h.sessionService.FindSessionByIP(id)
		}
		if session == nil {
			// Try as username
			session = h.sessionService.FindSessionByUsername(id)
		}
	}

	if session == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Session not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"session": formatSessionForResponse(session),
	})
}

// GetSessionStats returns session statistics
// GET /api/v1/sessions/stats
func (h *SessionHandler) GetSessionStats(c *gin.Context) {
	stats := h.sessionService.GetSessionStats()

	c.JSON(http.StatusOK, gin.H{
		"stats": stats,
	})
}

// HandleNetFlow processes NetFlow data for sessions
// POST /api/v1/session/netflow
func (h *SessionHandler) HandleNetFlow(c *gin.Context) {
	var req struct {
		Direction string `json:"direction" binding:"required,oneof=in out"`
		SrcIP     string `json:"src_ip" binding:"required"`
		DstIP     string `json:"dst_ip" binding:"required"`
		Octets    uint64 `json:"octets" binding:"required"`
		Packets   uint64 `json:"packets" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	srcIP := net.ParseIP(req.SrcIP)
	dstIP := net.ParseIP(req.DstIP)
	if srcIP == nil || dstIP == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid IP addresses"})
		return
	}

	if err := h.sessionService.HandleNetFlow(req.Direction, srcIP, dstIP, req.Octets, req.Packets); err != nil {
		h.logger.Error("Failed to handle NetFlow", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "NetFlow processed successfully",
	})
}

// Additional helper endpoints

// ForceSync forces synchronization of all sessions to database
// POST /api/v1/session/sync
func (h *SessionHandler) ForceSync(c *gin.Context) {
	// This would trigger immediate sync of all sessions
	// Implementation depends on adding this method to session service

	c.JSON(http.StatusOK, gin.H{
		"message": "Session synchronization initiated",
	})
}

// CleanupExpired manually triggers cleanup of expired sessions
// POST /api/v1/session/cleanup
func (h *SessionHandler) CleanupExpired(c *gin.Context) {
	// This would trigger immediate cleanup of expired sessions
	// Implementation depends on adding this method to session service

	c.JSON(http.StatusOK, gin.H{
		"message": "Expired session cleanup initiated",
	})
}

// Utility function to format session data for JSON response
func formatSessionForResponse(session *models.IPTrafficSession) map[string]interface{} {
	response := map[string]interface{}{
		"uuid":        session.UUID,
		"sid":         session.SID,
		"cid":         session.CID,
		"username":    session.Username,
		"status":      session.Status,
		"started_at":  session.StartedAt,
		"expires_at":  session.ExpiresAt,
		"stopped_at":  session.StoppedAt,
		"shaper":      session.Shaper,
		"node":        session.Node,
		"in_octets":   session.InOctets,
		"out_octets":  session.OutOctets,
		"in_packets":  session.InPackets,
		"out_packets": session.OutPackets,
		"amount":      session.Amount,
		"plan_id":     session.PlanID,
		"currency":    session.Currency,
		"balance":     session.Balance,
		"auth_algo":   session.AuthAlgo,
		"acct_algo":   session.AcctAlgo,
	}

	if session.IP != nil {
		response["ip"] = session.IP.String()
	}

	if len(session.NASSpec) > 0 {
		response["nas_spec"] = session.NASSpec
	}

	if len(session.PlanData) > 0 {
		response["plan_data"] = session.PlanData
	}

	if len(session.TrafficDetails) > 0 {
		response["traffic_details"] = session.TrafficDetails
	}

	return response
}
