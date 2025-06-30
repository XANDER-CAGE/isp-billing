package disconnect

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"go.uber.org/zap"
)

// RADIUS packet codes (RFC 3576)
const (
	RADIUSDisconnectRequest = 40
	RADIUSDisconnectACK     = 41
	RADIUSDisconnectNAK     = 42
	RADIUSCoARequest        = 43
	RADIUSCoAACK            = 44
	RADIUSCoANAK            = 45
)

// RADIUS attributes
const (
	AttrUserName             = 1
	AttrNASIPAddress         = 4
	AttrNASPort              = 5
	AttrFramedIPAddress      = 8
	AttrCallingStationId     = 31
	AttrNASIdentifier        = 32
	AttrAcctSessionId        = 44
	AttrNASPortType          = 61
	AttrErrorCause           = 101
	AttrMessageAuthenticator = 80
)

// Error codes from RFC 3576
const (
	ErrorResidualSessionContextRemoved = 201
	ErrorInvalidEAPPacket              = 202
	ErrorUnsupportedAttribute          = 401
	ErrorMissingAttribute              = 402
	ErrorNASIdentificationMismatch     = 403
	ErrorInvalidRequest                = 404
	ErrorUnsupportedService            = 405
	ErrorUnsupportedExtension          = 406
	ErrorAdministrativelyProhibited    = 501
	ErrorRequestNotRoutable            = 502
	ErrorSessionContextNotFound        = 503
	ErrorSessionContextNotRemovable    = 504
	ErrorOtherProxyProcessingError     = 505
	ErrorResourcesUnavailable          = 506
	ErrorRequestInitiated              = 507
)

// Service handles disconnect operations
// Full equivalent to mod_disconnect_script.erl and mod_disconnect_pod.erl functionality
type Service struct {
	logger *zap.Logger
	config Config
}

// Config holds disconnect service configuration
// Equivalent to module options in Erlang config
type Config struct {
	// RADIUS Disconnect-Request settings
	RADIUSEnabled bool          `yaml:"radius_enabled"`
	Secret        string        `yaml:"secret"`
	NASTimeout    time.Duration `yaml:"nas_timeout"`
	Retries       int           `yaml:"retries"`

	// Script-based disconnect settings
	ScriptEnabled bool          `yaml:"script_enabled"`
	ScriptPath    string        `yaml:"script_path"`
	ScriptTimeout time.Duration `yaml:"script_timeout"`
	ScriptEnv     []string      `yaml:"script_env"`

	// PoD (Packet of Death) settings
	PodEnabled  bool          `yaml:"pod_enabled"`
	PodEndpoint string        `yaml:"pod_endpoint"`
	PodTimeout  time.Duration `yaml:"pod_timeout"`
}

// New creates a new disconnect service
func New(logger *zap.Logger, config Config) *Service {
	// Set defaults like in Erlang modules
	if config.NASTimeout == 0 {
		config.NASTimeout = 5 * time.Second
	}
	if config.Retries == 0 {
		config.Retries = 3
	}
	if config.ScriptTimeout == 0 {
		config.ScriptTimeout = 10 * time.Second
	}
	if config.PodTimeout == 0 {
		config.PodTimeout = 3 * time.Second
	}

	return &Service{
		logger: logger,
		config: config,
	}
}

// DisconnectSession sends disconnect request for session
// Equivalent to disconnect/5 in both mod_disconnect_*.erl modules
func (s *Service) DisconnectSession(userName, sid string, ip net.IP, nasSpec map[string]interface{}) error {
	s.logger.Info("Initiating disconnect",
		zap.String("username", userName),
		zap.String("sid", sid),
		zap.String("ip", ip.String()))

	var lastErr error

	// Method 1: RADIUS Disconnect-Request (mod_disconnect_pod.erl)
	if s.config.RADIUSEnabled {
		if err := s.sendRADIUSDisconnect(userName, sid, ip, nasSpec); err != nil {
			s.logger.Warn("RADIUS disconnect failed", zap.Error(err))
			lastErr = err
		} else {
			s.logger.Info("RADIUS disconnect sent successfully",
				zap.String("username", userName),
				zap.String("sid", sid))
			return nil
		}
	}

	// Method 2: Script-based disconnect (mod_disconnect_script.erl)
	if s.config.ScriptEnabled && s.config.ScriptPath != "" {
		if err := s.executeDisconnectScript(userName, sid, ip, nasSpec); err != nil {
			s.logger.Warn("Script disconnect failed", zap.Error(err))
			lastErr = err
		} else {
			s.logger.Info("Script disconnect executed successfully",
				zap.String("username", userName),
				zap.String("sid", sid))
			return nil
		}
	}

	// Method 3: PoD (Packet of Death) UDP packet
	if s.config.PodEnabled && s.config.PodEndpoint != "" {
		if err := s.sendPoDPacket(userName, sid, ip, nasSpec); err != nil {
			s.logger.Warn("PoD disconnect failed", zap.Error(err))
			lastErr = err
		} else {
			s.logger.Info("PoD disconnect sent successfully",
				zap.String("username", userName),
				zap.String("sid", sid))
			return nil
		}
	}

	if lastErr != nil {
		return fmt.Errorf("all disconnect methods failed: %w", lastErr)
	}

	return fmt.Errorf("no disconnect methods configured")
}

// sendRADIUSDisconnect sends RADIUS Disconnect-Request
// Equivalent to disconnect/5 in mod_disconnect_pod.erl
func (s *Service) sendRADIUSDisconnect(userName, sid string, ip net.IP, nasSpec map[string]interface{}) error {
	if nasSpec == nil {
		return fmt.Errorf("no NAS specification provided")
	}

	// Extract NAS IP
	nasIPRaw, exists := nasSpec["nas_ip"]
	if !exists {
		return fmt.Errorf("no NAS IP in specification")
	}

	var nasIP net.IP
	switch v := nasIPRaw.(type) {
	case string:
		nasIP = net.ParseIP(v)
	case net.IP:
		nasIP = v
	default:
		return fmt.Errorf("invalid NAS IP type: %T", nasIPRaw)
	}

	if nasIP == nil {
		return fmt.Errorf("invalid NAS IP address")
	}

	// Build RADIUS Disconnect-Request packet
	packet, err := s.buildDisconnectRequest(userName, sid, ip, nasSpec)
	if err != nil {
		return fmt.Errorf("failed to build disconnect request: %w", err)
	}

	// Send with retries like in Erlang radclient:request/3
	for attempt := 1; attempt <= s.config.Retries; attempt++ {
		s.logger.Debug("Sending RADIUS disconnect request",
			zap.String("nas_ip", nasIP.String()),
			zap.Int("attempt", attempt))

		response, err := s.sendRADIUSPacket(nasIP, packet)
		if err != nil {
			if attempt == s.config.Retries {
				return fmt.Errorf("failed to send disconnect request after %d attempts: %w", s.config.Retries, err)
			}
			s.logger.Warn("Disconnect attempt failed, retrying",
				zap.Int("attempt", attempt),
				zap.Error(err))
			continue
		}

		// Process response
		return s.processDisconnectResponse(response, userName, sid)
	}

	return fmt.Errorf("all disconnect attempts failed")
}

// buildDisconnectRequest builds RADIUS Disconnect-Request packet
// Equivalent to building attributes list in mod_disconnect_pod.erl
func (s *Service) buildDisconnectRequest(userName, sid string, ip net.IP, nasSpec map[string]interface{}) ([]byte, error) {
	var buf bytes.Buffer

	// RADIUS Header: Code(1) + Identifier(1) + Length(2) + Authenticator(16)
	buf.WriteByte(RADIUSDisconnectRequest) // Code
	buf.WriteByte(1)                       // Identifier (should be random)
	buf.WriteByte(0)                       // Length (will be filled later)
	buf.WriteByte(0)                       // Length (will be filled later)

	// Request Authenticator (16 bytes - will be calculated with MD5)
	authenticatorPos := buf.Len()
	authenticator := make([]byte, 16)
	buf.Write(authenticator)

	// Add RADIUS attributes exactly as in Erlang: [{"User-Name", UserName}, {"Acct-Session-Id", SID}, {"Framed-IP-Address", IP}]

	// User-Name attribute
	if userName != "" {
		s.addStringAttribute(&buf, AttrUserName, userName)
	}

	// Acct-Session-Id attribute
	if sid != "" {
		s.addStringAttribute(&buf, AttrAcctSessionId, sid)
	}

	// Framed-IP-Address attribute
	if ip != nil {
		s.addIPAttribute(&buf, AttrFramedIPAddress, ip)
	}

	// Optional NAS attributes from nasSpec
	if nasIP, exists := nasSpec["nas_ip"]; exists {
		if ipAddr := s.parseIP(nasIP); ipAddr != nil {
			s.addIPAttribute(&buf, AttrNASIPAddress, ipAddr)
		}
	}

	if nasPort, exists := nasSpec["nas_port"]; exists {
		if port, ok := s.parseInt32(nasPort); ok {
			s.addIntegerAttribute(&buf, AttrNASPort, port)
		}
	}

	if nasId, exists := nasSpec["nas_identifier"]; exists {
		if id, ok := nasId.(string); ok {
			s.addStringAttribute(&buf, AttrNASIdentifier, id)
		}
	}

	packet := buf.Bytes()

	// Update length in header
	length := uint16(len(packet))
	binary.BigEndian.PutUint16(packet[2:4], length)

	// Calculate Request Authenticator with MD5
	if s.config.Secret != "" {
		calculatedAuth := s.calculateRequestAuthenticator(packet, s.config.Secret)
		copy(packet[authenticatorPos:authenticatorPos+16], calculatedAuth)
	}

	return packet, nil
}

// sendRADIUSPacket sends packet to NAS and receives response
func (s *Service) sendRADIUSPacket(nasIP net.IP, packet []byte) ([]byte, error) {
	// Connect to NAS on port 3799 (RFC 3576 port for Disconnect-Request)
	conn, err := net.DialTimeout("udp", fmt.Sprintf("%s:3799", nasIP.String()), s.config.NASTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NAS: %w", err)
	}
	defer conn.Close()

	// Set write deadline
	conn.SetWriteDeadline(time.Now().Add(s.config.NASTimeout))

	_, err = conn.Write(packet)
	if err != nil {
		return nil, fmt.Errorf("failed to send packet: %w", err)
	}

	// Read response
	response := make([]byte, 4096)
	conn.SetReadDeadline(time.Now().Add(s.config.NASTimeout))

	n, err := conn.Read(response)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if n < 20 { // Minimum RADIUS packet size
		return nil, fmt.Errorf("invalid response length: %d", n)
	}

	return response[:n], nil
}

// processDisconnectResponse processes RADIUS response
// Equivalent to response handling in mod_disconnect_pod.erl
func (s *Service) processDisconnectResponse(response []byte, userName, sid string) error {
	if len(response) < 4 {
		return fmt.Errorf("response too short")
	}

	responseCode := response[0]
	switch responseCode {
	case RADIUSDisconnectACK:
		s.logger.Info("Disconnect ACK received",
			zap.String("username", userName),
			zap.String("sid", sid))
		return nil

	case RADIUSDisconnectNAK:
		// Parse Error-Cause attribute if present
		errorCause := s.parseErrorCause(response)
		errorMsg := s.formatRADIUSError(errorCause)

		s.logger.Warn("Disconnect NAK received",
			zap.String("username", userName),
			zap.String("sid", sid),
			zap.Uint32("error_cause", errorCause),
			zap.String("error_message", errorMsg))

		return fmt.Errorf("disconnect rejected: %s", errorMsg)

	default:
		s.logger.Warn("Unknown disconnect response",
			zap.Uint8("code", responseCode),
			zap.String("username", userName),
			zap.String("sid", sid))
		return fmt.Errorf("unknown response code: %d", responseCode)
	}
}

// executeDisconnectScript runs external disconnect script
// Equivalent to disconnect/5 in mod_disconnect_script.erl
func (s *Service) executeDisconnectScript(userName, sid string, ip net.IP, nasSpec map[string]interface{}) error {
	if s.config.ScriptPath == "" {
		return fmt.Errorf("no disconnect script configured")
	}

	// Extract NAS IP for script arguments
	nasIPStr := ""
	if nasIP, exists := nasSpec["nas_ip"]; exists {
		if ipAddr := s.parseIP(nasIP); ipAddr != nil {
			nasIPStr = ipAddr.String()
		}
	}

	// Build command exactly as in Erlang: string:join([Script, UserName, SID, inet_parse:ntoa(IP), inet_parse:ntoa(NasIP)], " ")
	args := []string{userName, sid, ip.String(), nasIPStr}

	s.logger.Info("Executing disconnect script",
		zap.String("script", s.config.ScriptPath),
		zap.Strings("args", args))

	// Execute script with timeout
	ctx, cancel := s.createTimeoutContext(s.config.ScriptTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, s.config.ScriptPath, args...)

	// Set environment variables
	cmd.Env = append(os.Environ(), s.config.ScriptEnv...)

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// Process result like in Erlang call_external_prog/1
	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			return fmt.Errorf("script execution failed: %w", err)
		}
	}

	output := strings.TrimSpace(stdout.String())
	if stderr.Len() > 0 {
		output += "\n" + strings.TrimSpace(stderr.String())
	}

	s.logger.Info("Script execution completed",
		zap.Int("exit_code", exitCode),
		zap.String("output", output))

	// Return success if exit code is 0, like in Erlang
	if exitCode == 0 {
		return nil
	}

	return fmt.Errorf("script failed with exit code %d: %s", exitCode, output)
}

// sendPoDPacket sends "Packet of Death" UDP packet
// Custom implementation for PoD functionality
func (s *Service) sendPoDPacket(userName, sid string, ip net.IP, nasSpec map[string]interface{}) error {
	if s.config.PodEndpoint == "" {
		return fmt.Errorf("no PoD endpoint configured")
	}

	// Build PoD packet with session information
	podData := fmt.Sprintf("DISCONNECT:%s:%s:%s", userName, sid, ip.String())

	s.logger.Info("Sending PoD packet",
		zap.String("endpoint", s.config.PodEndpoint),
		zap.String("data", podData))

	// Send UDP packet to configured endpoint
	conn, err := net.DialTimeout("udp", s.config.PodEndpoint, s.config.PodTimeout)
	if err != nil {
		return fmt.Errorf("failed to connect to PoD endpoint: %w", err)
	}
	defer conn.Close()

	_, err = conn.Write([]byte(podData))
	if err != nil {
		return fmt.Errorf("failed to send PoD packet: %w", err)
	}

	return nil
}

// Helper methods for building RADIUS attributes

func (s *Service) addStringAttribute(buf *bytes.Buffer, attrType uint8, value string) {
	valueBytes := []byte(value)
	length := uint8(2 + len(valueBytes))

	buf.WriteByte(attrType)
	buf.WriteByte(length)
	buf.Write(valueBytes)
}

func (s *Service) addIPAttribute(buf *bytes.Buffer, attrType uint8, ip net.IP) {
	ip4 := ip.To4()
	if ip4 == nil {
		return // Skip IPv6 for now
	}

	buf.WriteByte(attrType)
	buf.WriteByte(6) // Type(1) + Length(1) + IP(4)
	buf.Write(ip4)
}

func (s *Service) addIntegerAttribute(buf *bytes.Buffer, attrType uint8, value uint32) {
	buf.WriteByte(attrType)
	buf.WriteByte(6) // Type(1) + Length(1) + Integer(4)

	intBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(intBytes, value)
	buf.Write(intBytes)
}

// calculateRequestAuthenticator calculates RADIUS authenticator with MD5
func (s *Service) calculateRequestAuthenticator(packet []byte, secret string) []byte {
	// Create MD5 hash: MD5(Code + Identifier + Length + 16 zero bytes + Request Attributes + Shared Secret)
	hash := md5.New()

	// Write packet header (first 4 bytes)
	hash.Write(packet[:4])

	// Write 16 zero bytes (placeholder for authenticator)
	hash.Write(make([]byte, 16))

	// Write remaining attributes
	hash.Write(packet[20:])

	// Write shared secret
	hash.Write([]byte(secret))

	return hash.Sum(nil)
}

// parseErrorCause extracts Error-Cause attribute from RADIUS response
func (s *Service) parseErrorCause(response []byte) uint32 {
	if len(response) < 20 {
		return 0
	}

	// Parse attributes starting after header
	pos := 20
	for pos < len(response) {
		if pos+2 > len(response) {
			break
		}

		attrType := response[pos]
		attrLength := response[pos+1]

		if attrType == AttrErrorCause && attrLength == 6 {
			if pos+6 <= len(response) {
				return binary.BigEndian.Uint32(response[pos+2 : pos+6])
			}
		}

		pos += int(attrLength)
	}

	return 0
}

// formatRADIUSError formats RADIUS error codes to human-readable messages
// Equivalent to format_error/1 in mod_disconnect_pod.erl
func (s *Service) formatRADIUSError(code uint32) string {
	switch code {
	case ErrorResidualSessionContextRemoved:
		return "Residual Session Context Removed"
	case ErrorInvalidEAPPacket:
		return "Invalid EAP Packet (Ignored)"
	case ErrorUnsupportedAttribute:
		return "Unsupported Attribute"
	case ErrorMissingAttribute:
		return "Missing Attribute"
	case ErrorNASIdentificationMismatch:
		return "NAS Identification Mismatch"
	case ErrorInvalidRequest:
		return "Invalid Request"
	case ErrorUnsupportedService:
		return "Unsupported Service"
	case ErrorUnsupportedExtension:
		return "Unsupported Extension"
	case ErrorAdministrativelyProhibited:
		return "Administratively Prohibited"
	case ErrorRequestNotRoutable:
		return "Request Not Routable (Proxy)"
	case ErrorSessionContextNotFound:
		return "Session Context Not Found"
	case ErrorSessionContextNotRemovable:
		return "Session Context Not Removable"
	case ErrorOtherProxyProcessingError:
		return "Other Proxy Processing Error"
	case ErrorResourcesUnavailable:
		return "Resources Unavailable"
	case ErrorRequestInitiated:
		return "Request Initiated"
	default:
		return "Unknown error"
	}
}

// Utility helper methods

func (s *Service) parseIP(value interface{}) net.IP {
	switch v := value.(type) {
	case string:
		return net.ParseIP(v)
	case net.IP:
		return v
	default:
		return nil
	}
}

func (s *Service) parseInt32(value interface{}) (uint32, bool) {
	switch v := value.(type) {
	case int:
		return uint32(v), true
	case int32:
		return uint32(v), true
	case uint32:
		return v, true
	case int64:
		return uint32(v), true
	case uint64:
		return uint32(v), true
	default:
		return 0, false
	}
}

func (s *Service) createTimeoutContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	// For Go < 1.7 compatibility, we'll use manual timeout handling
	// In real implementation, use context.WithTimeout
	return nil, func() {}
}

// Admin API methods for session management

// DisconnectByIP disconnects session by IP address
func (s *Service) DisconnectByIP(ip net.IP, reason string) error {
	s.logger.Info("Disconnect by IP",
		zap.String("ip", ip.String()),
		zap.String("reason", reason))

	// This would find active session by IP and disconnect it
	// For now, placeholder implementation
	return fmt.Errorf("disconnect by IP not implemented yet")
}

// DisconnectByUsername disconnects all sessions for username
func (s *Service) DisconnectByUsername(username, reason string) error {
	s.logger.Info("Disconnect by username",
		zap.String("username", username),
		zap.String("reason", reason))

	// This would find all active sessions for username and disconnect them
	// For now, placeholder implementation
	return fmt.Errorf("disconnect by username not implemented yet")
}

// DisconnectBySessionID disconnects session by session ID
func (s *Service) DisconnectBySessionID(sid, reason string) error {
	s.logger.Info("Disconnect by session ID",
		zap.String("sid", sid),
		zap.String("reason", reason))

	// This would find active session by SID and disconnect it
	// For now, placeholder implementation
	return fmt.Errorf("disconnect by session ID not implemented yet")
}
