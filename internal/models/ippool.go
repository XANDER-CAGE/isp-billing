package models

import (
	"net"
	"time"
)

// IPPoolEntry represents an IP pool entry
// Equivalent to #ippool_entry{} record in mod_ippool.erl
type IPPoolEntry struct {
	IP        net.IP `json:"ip" redis:"ip"`
	Pool      string `json:"pool" redis:"pool"`
	ExpiresAt int64  `json:"expires_at" redis:"expires_at"`
}

// IPRange represents an IP range for pool allocation
type IPRange struct {
	Start net.IP
	End   net.IP
}

// PoolConfig represents IP pool configuration
type PoolConfig struct {
	Name   string   `yaml:"name" json:"name"`
	Ranges []string `yaml:"ranges" json:"ranges"`
}

// IsExpired checks if IP lease has expired
func (e *IPPoolEntry) IsExpired() bool {
	if e.ExpiresAt == 0 {
		return false // Never expires (free)
	}
	return time.Now().Unix() >= e.ExpiresAt
}

// IsFree checks if IP is available for lease
func (e *IPPoolEntry) IsFree() bool {
	return e.ExpiresAt == 0 || e.IsExpired()
}

// LeaseIP leases the IP with given timeout
func (e *IPPoolEntry) LeaseIP(timeout int) {
	e.ExpiresAt = time.Now().Unix() + int64(timeout)
}

// ReleaseIP releases the IP back to pool
func (e *IPPoolEntry) ReleaseIP() {
	e.ExpiresAt = 0
}

// IPPoolStats represents IP pool statistics
type IPPoolStats struct {
	PoolName   string `json:"pool_name"`
	TotalIPs   int    `json:"total_ips"`
	UsedIPs    int    `json:"used_ips"`
	FreeIPs    int    `json:"free_ips"`
	ExpiredIPs int    `json:"expired_ips"`
}

// IPPoolRequest represents request for IP lease/renew/release
type IPPoolRequest struct {
	Pool     string `json:"pool,omitempty"`     // For lease
	IP       string `json:"ip,omitempty"`       // For renew/release
	Username string `json:"username,omitempty"` // Optional context
	SID      string `json:"sid,omitempty"`      // Session ID
}

// IPPoolResponse represents response from IP pool operations
type IPPoolResponse struct {
	Success bool   `json:"success"`
	IP      string `json:"ip,omitempty"`
	Pool    string `json:"pool,omitempty"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}
