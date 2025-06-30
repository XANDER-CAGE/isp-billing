package models

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// SessionStatus represents session status
// Equivalent to session states in iptraffic_session.erl
type SessionStatus string

const (
	StatusNew      SessionStatus = "new"      // Session prepared but not started
	StatusActive   SessionStatus = "active"   // Session running
	StatusStopped  SessionStatus = "stopped"  // Session stopped
	StatusExpired  SessionStatus = "expired"  // Session expired
	StatusStarting SessionStatus = "starting" // Session being started
	StatusStopping SessionStatus = "stopping" // Session being stopped
)

// IPTrafficSession represents an active session
// Full equivalent to #ipt_session{} record in iptraffic_session.erl
type IPTrafficSession struct {
	UUID        string                 `json:"uuid" redis:"uuid"`
	SID         string                 `json:"sid" redis:"sid"` // Session ID
	CID         string                 `json:"cid" redis:"cid"` // Client MAC address
	Username    string                 `json:"username" redis:"username"`
	IP          net.IP                 `json:"ip" redis:"ip"`
	Status      SessionStatus          `json:"status" redis:"status"`
	StartedAt   int64                  `json:"started_at" redis:"started_at"`
	ExpiresAt   int64                  `json:"expires_at" redis:"expires_at"`
	StoppedAt   int64                  `json:"stopped_at" redis:"stopped_at"`
	NASSpec     map[string]interface{} `json:"nas_spec" redis:"nas_spec"`           // NAS client info
	Data        map[string]interface{} `json:"data" redis:"data"`                   // Context data (balance, plan_data, etc.)
	Shaper      string                 `json:"shaper" redis:"shaper"`               // Current shaper
	DiscReqSent bool                   `json:"disc_req_sent" redis:"disc_req_sent"` // Disconnect request sent
	Node        string                 `json:"node" redis:"node"`                   // Node name

	// Traffic counters (updated by NetFlow and RADIUS)
	InOctets   uint64 `json:"in_octets" redis:"in_octets"`
	OutOctets  uint64 `json:"out_octets" redis:"out_octets"`
	InPackets  uint64 `json:"in_packets" redis:"in_packets"`
	OutPackets uint64 `json:"out_packets" redis:"out_packets"`

	// Database session ID for sync (equivalent to Mnesia key)
	DBSessionID int64 `json:"db_session_id" redis:"db_session_id"`

	// Billing data
	Amount      float64 `json:"amount" redis:"amount"`             // Total amount charged
	LastSync    int64   `json:"last_sync" redis:"last_sync"`       // Last sync to DB
	LastTraffic int64   `json:"last_traffic" redis:"last_traffic"` // Last traffic update

	// Session timeout management (like in Erlang)
	TimeoutRef    string `json:"timeout_ref" redis:"timeout_ref"`       // Timer reference
	SessionExpiry int64  `json:"session_expiry" redis:"session_expiry"` // Session expiry time

	// Plan and billing context (from RADIUS authorization)
	PlanID   int                    `json:"plan_id" redis:"plan_id"`
	PlanData map[string]interface{} `json:"plan_data" redis:"plan_data"`
	Currency int                    `json:"currency" redis:"currency"`
	Balance  float64                `json:"balance" redis:"balance"`
	AuthAlgo string                 `json:"auth_algo" redis:"auth_algo"`
	AcctAlgo string                 `json:"acct_algo" redis:"acct_algo"`

	// Traffic details by class (equivalent to session_details table)
	TrafficDetails map[string]*TrafficClassDetail `json:"traffic_details" redis:"traffic_details"`
}

// TrafficClassDetail represents traffic details for a specific class
// Equivalent to session_details table record
type TrafficClassDetail struct {
	Class      string  `json:"class"`
	InOctets   uint64  `json:"in_octets"`
	OutOctets  uint64  `json:"out_octets"`
	InPackets  uint64  `json:"in_packets"`
	OutPackets uint64  `json:"out_packets"`
	Amount     float64 `json:"amount"`
}

// SessionContext represents session initialization context
// Equivalent to context data passed in Erlang
type SessionContext struct {
	AccountID int                    `json:"account_id"`
	Username  string                 `json:"username"`
	Password  string                 `json:"password"`
	PlanID    int                    `json:"plan_id"`
	PlanData  map[string]interface{} `json:"plan_data"`
	Currency  int                    `json:"currency"`
	Balance   float64                `json:"balance"`
	AuthAlgo  string                 `json:"auth_algo"`
	AcctAlgo  string                 `json:"acct_algo"`
	Replies   []RADIUSReply          `json:"replies"`
	NASSpec   map[string]interface{} `json:"nas_spec"`
}

// SessionEvent represents session lifecycle events
type SessionEvent struct {
	Type      string      `json:"type"`
	SessionID string      `json:"session_id"`
	Username  string      `json:"username"`
	Data      interface{} `json:"data"`
	Timestamp int64       `json:"timestamp"`
}

// NewIPTrafficSession creates a new session with UUID
// Equivalent to creating #ipt_session{} record
func NewIPTrafficSession(uuid, username string) *IPTrafficSession {
	now := time.Now().Unix()
	return &IPTrafficSession{
		UUID:           uuid,
		Username:       username,
		Status:         StatusNew,
		StartedAt:      now,
		ExpiresAt:      now + 60, // Default 60 seconds timeout
		Data:           make(map[string]interface{}),
		NASSpec:        make(map[string]interface{}),
		PlanData:       make(map[string]interface{}),
		Node:           "netspire-go", // Static node name
		DiscReqSent:    false,
		TrafficDetails: make(map[string]*TrafficClassDetail),
	}
}

// Prepare initializes session with context data
// Equivalent to prepare/5 in iptraffic_session.erl
func (s *IPTrafficSession) Prepare(ctx *SessionContext) error {
	s.PlanID = ctx.PlanID
	s.PlanData = ctx.PlanData
	s.Currency = ctx.Currency
	s.Balance = ctx.Balance
	s.AuthAlgo = ctx.AuthAlgo
	s.AcctAlgo = ctx.AcctAlgo
	s.NASSpec = ctx.NASSpec

	// Set context data for billing algorithms
	s.Data["account_id"] = ctx.AccountID
	s.Data["plan_id"] = ctx.PlanID
	s.Data["currency"] = ctx.Currency
	s.Data["balance"] = ctx.Balance
	s.Data["auth_algo"] = ctx.AuthAlgo
	s.Data["acct_algo"] = ctx.AcctAlgo

	return nil
}

// Activate changes session status to active and sets SID, CID, IP
// Equivalent to start/4 in iptraffic_session.erl
func (s *IPTrafficSession) Activate(sid, cid string, ip net.IP) {
	s.SID = sid
	s.CID = cid
	s.IP = ip
	s.Status = StatusActive
	s.StartedAt = time.Now().Unix()
}

// Stop marks session as stopped
// Equivalent to stop/1 in iptraffic_session.erl
func (s *IPTrafficSession) Stop() {
	s.Status = StatusStopped
	s.StoppedAt = time.Now().Unix()
}

// Expire marks session as expired
// Equivalent to expire/1 in iptraffic_session.erl
func (s *IPTrafficSession) Expire() {
	s.Status = StatusExpired
	s.StoppedAt = time.Now().Unix()
}

// IsExpired checks if session has expired
func (s *IPTrafficSession) IsExpired() bool {
	return time.Now().Unix() >= s.ExpiresAt
}

// IsActive checks if session is in active state
func (s *IPTrafficSession) IsActive() bool {
	return s.Status == StatusActive
}

// IsNew checks if session is in new state (prepared but not started)
func (s *IPTrafficSession) IsNew() bool {
	return s.Status == StatusNew
}

// RenewTimeout extends session timeout
// Equivalent to interim/1 in iptraffic_session.erl
func (s *IPTrafficSession) RenewTimeout(timeout int) {
	s.ExpiresAt = time.Now().Unix() + int64(timeout)
}

// UpdateTraffic updates traffic counters from NetFlow or RADIUS
// Equivalent to handle_cast({netflow, Dir, ...}) in iptraffic_session.erl
func (s *IPTrafficSession) UpdateTraffic(direction string, octets, packets uint64) {
	switch direction {
	case "in":
		s.InOctets += octets
		s.InPackets += packets
	case "out":
		s.OutOctets += octets
		s.OutPackets += packets
	}
	s.LastTraffic = time.Now().Unix()
}

// UpdateTrafficByClass updates traffic counters for specific class
func (s *IPTrafficSession) UpdateTrafficByClass(class, direction string, octets, packets uint64, amount float64) {
	if s.TrafficDetails == nil {
		s.TrafficDetails = make(map[string]*TrafficClassDetail)
	}

	detail, exists := s.TrafficDetails[class]
	if !exists {
		detail = &TrafficClassDetail{
			Class: class,
		}
		s.TrafficDetails[class] = detail
	}

	switch direction {
	case "in":
		detail.InOctets += octets
		detail.InPackets += packets
	case "out":
		detail.OutOctets += octets
		detail.OutPackets += packets
	}

	detail.Amount += amount
	s.Amount += amount

	// Update total counters
	s.UpdateTraffic(direction, octets, packets)
}

// SetShaper updates current shaper
func (s *IPTrafficSession) SetShaper(shaper string) {
	s.Shaper = shaper
}

// GetContextValue gets value from session context data
func (s *IPTrafficSession) GetContextValue(key string) (interface{}, bool) {
	if s.Data == nil {
		return nil, false
	}
	value, exists := s.Data[key]
	return value, exists
}

// SetContextValue sets value in session context data
func (s *IPTrafficSession) SetContextValue(key string, value interface{}) {
	if s.Data == nil {
		s.Data = make(map[string]interface{})
	}
	s.Data[key] = value
}

// UpdatePlanData updates plan data and marks for sync
func (s *IPTrafficSession) UpdatePlanData(newPlanData map[string]interface{}) {
	s.PlanData = newPlanData
	// Mark as needing sync to DB
	s.SetContextValue("plan_data_changed", true)
}

// NeedsSync checks if session needs to be synced to database
func (s *IPTrafficSession) NeedsSync() bool {
	// Sync if traffic has been updated since last sync
	if s.LastTraffic > s.LastSync {
		return true
	}

	// Sync if plan data has changed
	if changed, exists := s.GetContextValue("plan_data_changed"); exists && changed.(bool) {
		return true
	}

	// Sync if amount has changed significantly
	if s.Amount > 0 {
		return true
	}

	return false
}

// MarkSynced marks session as synced to database
func (s *IPTrafficSession) MarkSynced() {
	s.LastSync = time.Now().Unix()
	s.SetContextValue("plan_data_changed", false)
}

// ToRedisHash converts session to Redis hash map
func (s *IPTrafficSession) ToRedisHash() map[string]interface{} {
	hash := make(map[string]interface{})

	hash["uuid"] = s.UUID
	hash["sid"] = s.SID
	hash["cid"] = s.CID
	hash["username"] = s.Username
	hash["status"] = string(s.Status)
	hash["started_at"] = s.StartedAt
	hash["expires_at"] = s.ExpiresAt
	hash["stopped_at"] = s.StoppedAt
	hash["shaper"] = s.Shaper
	hash["disc_req_sent"] = s.DiscReqSent
	hash["node"] = s.Node
	hash["in_octets"] = s.InOctets
	hash["out_octets"] = s.OutOctets
	hash["in_packets"] = s.InPackets
	hash["out_packets"] = s.OutPackets
	hash["db_session_id"] = s.DBSessionID
	hash["amount"] = s.Amount
	hash["last_sync"] = s.LastSync
	hash["last_traffic"] = s.LastTraffic
	hash["timeout_ref"] = s.TimeoutRef
	hash["session_expiry"] = s.SessionExpiry
	hash["plan_id"] = s.PlanID
	hash["currency"] = s.Currency
	hash["balance"] = s.Balance
	hash["auth_algo"] = s.AuthAlgo
	hash["acct_algo"] = s.AcctAlgo

	if s.IP != nil {
		hash["ip"] = s.IP.String()
	}

	// Serialize complex fields to JSON
	if nasSpecJSON, err := json.Marshal(s.NASSpec); err == nil {
		hash["nas_spec"] = string(nasSpecJSON)
	}

	if dataJSON, err := json.Marshal(s.Data); err == nil {
		hash["data"] = string(dataJSON)
	}

	if planDataJSON, err := json.Marshal(s.PlanData); err == nil {
		hash["plan_data"] = string(planDataJSON)
	}

	if trafficDetailsJSON, err := json.Marshal(s.TrafficDetails); err == nil {
		hash["traffic_details"] = string(trafficDetailsJSON)
	}

	return hash
}

// FromRedisHash populates session from Redis hash map
func (s *IPTrafficSession) FromRedisHash(hash map[string]string) error {
	s.UUID = hash["uuid"]
	s.SID = hash["sid"]
	s.CID = hash["cid"]
	s.Username = hash["username"]
	s.Status = SessionStatus(hash["status"])
	s.Shaper = hash["shaper"]
	s.Node = hash["node"]
	s.TimeoutRef = hash["timeout_ref"]
	s.AuthAlgo = hash["auth_algo"]
	s.AcctAlgo = hash["acct_algo"]

	if hash["ip"] != "" {
		s.IP = net.ParseIP(hash["ip"])
	}

	// Parse numeric fields
	if val := hash["started_at"]; val != "" {
		if parsed, err := parseint64(val); err == nil {
			s.StartedAt = parsed
		}
	}

	if val := hash["expires_at"]; val != "" {
		if parsed, err := parseint64(val); err == nil {
			s.ExpiresAt = parsed
		}
	}

	if val := hash["stopped_at"]; val != "" {
		if parsed, err := parseint64(val); err == nil {
			s.StoppedAt = parsed
		}
	}

	if val := hash["db_session_id"]; val != "" {
		if parsed, err := parseint64(val); err == nil {
			s.DBSessionID = parsed
		}
	}

	if val := hash["last_sync"]; val != "" {
		if parsed, err := parseint64(val); err == nil {
			s.LastSync = parsed
		}
	}

	if val := hash["last_traffic"]; val != "" {
		if parsed, err := parseint64(val); err == nil {
			s.LastTraffic = parsed
		}
	}

	if val := hash["session_expiry"]; val != "" {
		if parsed, err := parseint64(val); err == nil {
			s.SessionExpiry = parsed
		}
	}

	if val := hash["plan_id"]; val != "" {
		if parsed, err := parseint(val); err == nil {
			s.PlanID = parsed
		}
	}

	if val := hash["currency"]; val != "" {
		if parsed, err := parseint(val); err == nil {
			s.Currency = parsed
		}
	}

	if val := hash["amount"]; val != "" {
		if parsed, err := parsefloat64(val); err == nil {
			s.Amount = parsed
		}
	}

	if val := hash["balance"]; val != "" {
		if parsed, err := parsefloat64(val); err == nil {
			s.Balance = parsed
		}
	}

	// Parse boolean fields
	if val := hash["disc_req_sent"]; val == "true" {
		s.DiscReqSent = true
	}

	// Parse traffic counters
	if val := hash["in_octets"]; val != "" {
		if parsed, err := parseuint64(val); err == nil {
			s.InOctets = parsed
		}
	}

	if val := hash["out_octets"]; val != "" {
		if parsed, err := parseuint64(val); err == nil {
			s.OutOctets = parsed
		}
	}

	if val := hash["in_packets"]; val != "" {
		if parsed, err := parseuint64(val); err == nil {
			s.InPackets = parsed
		}
	}

	if val := hash["out_packets"]; val != "" {
		if parsed, err := parseuint64(val); err == nil {
			s.OutPackets = parsed
		}
	}

	// Parse JSON fields
	if nasSpecJSON := hash["nas_spec"]; nasSpecJSON != "" && nasSpecJSON != "null" {
		s.NASSpec = make(map[string]interface{})
		json.Unmarshal([]byte(nasSpecJSON), &s.NASSpec)
	}

	if dataJSON := hash["data"]; dataJSON != "" && dataJSON != "null" {
		s.Data = make(map[string]interface{})
		json.Unmarshal([]byte(dataJSON), &s.Data)
	}

	if planDataJSON := hash["plan_data"]; planDataJSON != "" && planDataJSON != "null" {
		s.PlanData = make(map[string]interface{})
		json.Unmarshal([]byte(planDataJSON), &s.PlanData)
	}

	if trafficDetailsJSON := hash["traffic_details"]; trafficDetailsJSON != "" && trafficDetailsJSON != "null" {
		s.TrafficDetails = make(map[string]*TrafficClassDetail)
		json.Unmarshal([]byte(trafficDetailsJSON), &s.TrafficDetails)
	}

	return nil
}

// Helper functions for parsing
func parseint64(s string) (int64, error) {
	var result int64
	_, err := fmt.Sscanf(s, "%d", &result)
	return result, err
}

func parseint(s string) (int, error) {
	var result int
	_, err := fmt.Sscanf(s, "%d", &result)
	return result, err
}

func parseuint64(s string) (uint64, error) {
	var result uint64
	_, err := fmt.Sscanf(s, "%d", &result)
	return result, err
}

func parsefloat64(s string) (float64, error) {
	var result float64
	_, err := fmt.Sscanf(s, "%f", &result)
	return result, err
}
