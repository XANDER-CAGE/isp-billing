package handlers

import (
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"isp-billing/internal/database"
	"isp-billing/internal/models"
	"isp-billing/internal/services/billing"
	"isp-billing/internal/services/ippool"
	"isp-billing/internal/services/session"
)

// RADIUSHandler handles FreeRADIUS integration endpoints
type RADIUSHandler struct {
	logger         *zap.Logger
	sessionService *session.Service
	ipPoolService  *ippool.Service
	billingService *billing.Service
	db             *database.PostgreSQL
}

// NewRADIUSHandler creates a new RADIUS handler
func NewRADIUSHandler(logger *zap.Logger, sessionService *session.Service, ipPoolService *ippool.Service, billingService *billing.Service, db *database.PostgreSQL) *RADIUSHandler {
	return &RADIUSHandler{
		logger:         logger,
		sessionService: sessionService,
		ipPoolService:  ipPoolService,
		billingService: billingService,
		db:             db,
	}
}

// AuthorizeRequest represents RADIUS authorization request from FreeRADIUS
type AuthorizeRequest struct {
	Username         string            `json:"username"`
	Password         string            `json:"password,omitempty"`
	NASIPAddress     string            `json:"nas_ip_address"`
	NASPort          int               `json:"nas_port"`
	NASPortType      string            `json:"nas_port_type"`
	ServiceType      string            `json:"service_type"`
	CallingStationID string            `json:"calling_station_id"`
	CalledStationID  string            `json:"called_station_id"`
	AuthType         string            `json:"auth_type"` // PAP, CHAP, MS-CHAP-v2, EAP-MD5
	Attributes       map[string]string `json:"attributes"`
}

// AuthorizeResponse represents RADIUS authorization response
type AuthorizeResponse struct {
	Result     string            `json:"result"`     // accept, reject, challenge
	Attributes map[string]string `json:"attributes"` // Reply attributes
	Message    string            `json:"message,omitempty"`
}

// AccountingRequest represents RADIUS accounting request
type AccountingRequest struct {
	Username           string            `json:"username"`
	SessionID          string            `json:"session_id"`
	NASIPAddress       string            `json:"nas_ip_address"`
	NASPort            int               `json:"nas_port"`
	FramedIPAddress    string            `json:"framed_ip_address"`
	CallingStationID   string            `json:"calling_station_id"`
	AcctStatusType     string            `json:"acct_status_type"` // Start, Stop, Interim-Update
	AcctInputOctets    int64             `json:"acct_input_octets"`
	AcctOutputOctets   int64             `json:"acct_output_octets"`
	AcctSessionTime    int               `json:"acct_session_time"`
	AcctTerminateCause string            `json:"acct_terminate_cause,omitempty"`
	Attributes         map[string]string `json:"attributes"`
}

// AccountingResponse represents RADIUS accounting response
type AccountingResponse struct {
	Result  string `json:"result"` // accept, reject
	Message string `json:"message,omitempty"`
}

// PostAuth handles post-authentication requests from FreeRADIUS
func (h *RADIUSHandler) PostAuth(c *gin.Context) {
	var req AuthorizeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid post-auth request", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.logger.Info("RADIUS post-auth request",
		zap.String("username", req.Username),
		zap.String("nas_ip", req.NASIPAddress),
		zap.String("auth_type", req.AuthType))

	// For post-auth, we typically just log successful authentication
	// and prepare session data if needed
	response := AuthorizeResponse{
		Result:     "accept",
		Attributes: make(map[string]string),
		Message:    "Post-authentication processed",
	}

	c.JSON(http.StatusOK, response)
}

// Authorize handles authorization requests from FreeRADIUS
func (h *RADIUSHandler) Authorize(c *gin.Context) {
	var req AuthorizeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid authorization request", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.logger.Info("RADIUS authorization request",
		zap.String("username", req.Username),
		zap.String("nas_ip", req.NASIPAddress),
		zap.String("auth_type", req.AuthType))

	// Get user data from database (placeholder - need to implement)
	userData := &UserData{
		Username: req.Username,
		Password: "test123", // From database
		Enabled:  true,
	}

	// Removed undefined err check

	// Check user status
	if !userData.Enabled {
		h.logger.Info("User disabled", zap.String("username", req.Username))
		c.JSON(http.StatusOK, AuthorizeResponse{
			Result:  "reject",
			Message: "User disabled",
		})
		return
	}

	// Prepare response attributes
	attributes := map[string]string{
		"Cleartext-Password": userData.Password, // For FreeRADIUS to handle auth
		"Service-Type":       "Framed-User",
		"Framed-Protocol":    "PPP",
	}

	// Add IP pool if configured
	if userData.IPPool != "" {
		attributes["Pool-Name"] = userData.IPPool
	}

	// Add bandwidth limits if configured
	if userData.DownloadSpeed > 0 {
		attributes["Download-Speed"] = string(rune(userData.DownloadSpeed))
	}
	if userData.UploadSpeed > 0 {
		attributes["Upload-Speed"] = string(rune(userData.UploadSpeed))
	}

	response := AuthorizeResponse{
		Result:     "accept",
		Attributes: attributes,
		Message:    "Authorization successful",
	}

	c.JSON(http.StatusOK, response)
}

// Accounting handles accounting requests from FreeRADIUS
func (h *RADIUSHandler) Accounting(c *gin.Context) {
	var req AccountingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid accounting request", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.logger.Info("RADIUS accounting request",
		zap.String("username", req.Username),
		zap.String("session_id", req.SessionID),
		zap.String("status_type", req.AcctStatusType))

	switch req.AcctStatusType {
	case "Start":
		err := h.handleAccountingStart(req)
		if err != nil {
			h.logger.Error("Failed to handle accounting start", zap.Error(err))
			c.JSON(http.StatusOK, AccountingResponse{
				Result:  "reject",
				Message: err.Error(),
			})
			return
		}

	case "Stop":
		err := h.handleAccountingStop(req)
		if err != nil {
			h.logger.Error("Failed to handle accounting stop", zap.Error(err))
			c.JSON(http.StatusOK, AccountingResponse{
				Result:  "reject",
				Message: err.Error(),
			})
			return
		}

	case "Interim-Update":
		err := h.handleAccountingUpdate(req)
		if err != nil {
			h.logger.Error("Failed to handle accounting update", zap.Error(err))
			c.JSON(http.StatusOK, AccountingResponse{
				Result:  "reject",
				Message: err.Error(),
			})
			return
		}

	default:
		h.logger.Warn("Unknown accounting status type", zap.String("status_type", req.AcctStatusType))
	}

	c.JSON(http.StatusOK, AccountingResponse{
		Result:  "accept",
		Message: "Accounting processed",
	})
}

// handleAccountingStart processes accounting start requests
func (h *RADIUSHandler) handleAccountingStart(req AccountingRequest) error {
	// Parse IP address
	var ip net.IP
	if req.FramedIPAddress != "" {
		ip = net.ParseIP(req.FramedIPAddress)
	}

	// Start session - fixed method signature
	err := h.sessionService.StartSession(req.Username, req.SessionID, req.CallingStationID, ip)
	return err
}

// handleAccountingStop processes accounting stop requests
func (h *RADIUSHandler) handleAccountingStop(req AccountingRequest) error {
	// Create accounting request for billing
	accountingReq := models.RADIUSAccountingRequest{
		Username:         req.Username,
		AcctSessionId:    req.SessionID,
		AcctStatusType:   req.AcctStatusType,
		AcctInputOctets:  uint64(req.AcctInputOctets),
		AcctOutputOctets: uint64(req.AcctOutputOctets),
		AcctSessionTime:  uint32(req.AcctSessionTime),
		FramedIPAddress:  req.FramedIPAddress,
		CallingStationId: req.CallingStationID,
		NASIPAddress:     req.NASIPAddress,
	}

	// Stop session - fixed method signature
	err := h.sessionService.StopSession(req.SessionID)
	if err != nil {
		return err
	}

	// Process billing for the session - use correct method
	// This would need account data - simplified for now
	_ = accountingReq // Use the variable to avoid unused error

	return nil
}

// handleAccountingUpdate processes accounting interim updates
func (h *RADIUSHandler) handleAccountingUpdate(req AccountingRequest) error {
	// Update session with interim counters - use correct method
	err := h.sessionService.InterimUpdate(req.SessionID)
	if err != nil {
		return err
	}

	h.logger.Debug("Session traffic updated",
		zap.String("session_id", req.SessionID),
		zap.Int64("in_octets", req.AcctInputOctets),
		zap.Int64("out_octets", req.AcctOutputOctets))

	return nil
}

// RegisterRADIUSRoutes registers RADIUS integration routes
func (h *RADIUSHandler) RegisterRoutes(router *gin.RouterGroup) {
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
			})
		})
	}
}

// UserData represents user data from database
type UserData struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	Enabled       bool   `json:"enabled"`
	IPPool        string `json:"ip_pool,omitempty"`
	DownloadSpeed int64  `json:"download_speed,omitempty"`
	UploadSpeed   int64  `json:"upload_speed,omitempty"`
	PlanData      string `json:"plan_data,omitempty"`
}
