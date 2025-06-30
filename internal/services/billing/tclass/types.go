package tclass

// TrafficClass represents a traffic classification result
type TrafficClass string

const (
	ClassDefault  TrafficClass = "default"
	ClassLocal    TrafficClass = "local"
	ClassCDN      TrafficClass = "cdn"
	ClassInternet TrafficClass = "internet"
	ClassPremium  TrafficClass = "premium"
)

// ClassConfig represents traffic class configuration
// Equivalent to {Class, Networks} tuple in tclass.erl
type ClassConfig struct {
	Class    TrafficClass `yaml:"class" json:"class"`
	Networks []string     `yaml:"networks" json:"networks"`
}

// IPRange represents an IP range with classification
type IPRange struct {
	Start uint32       `json:"start"`
	End   uint32       `json:"end"`
	Class TrafficClass `json:"class"`
}

// ProtocolClass represents protocol-based traffic classification
type ProtocolClass string

const (
	ProtocolHTTP      ProtocolClass = "http"
	ProtocolHTTPS     ProtocolClass = "https"
	ProtocolFTP       ProtocolClass = "ftp"
	ProtocolSSH       ProtocolClass = "ssh"
	ProtocolTelnet    ProtocolClass = "telnet"
	ProtocolSMTP      ProtocolClass = "smtp"
	ProtocolPOP3      ProtocolClass = "pop3"
	ProtocolIMAP      ProtocolClass = "imap"
	ProtocolDNS       ProtocolClass = "dns"
	ProtocolDHCP      ProtocolClass = "dhcp"
	ProtocolSNMP      ProtocolClass = "snmp"
	ProtocolVOIP      ProtocolClass = "voip"
	ProtocolGaming    ProtocolClass = "gaming"
	ProtocolP2P       ProtocolClass = "p2p"
	ProtocolStreaming ProtocolClass = "streaming"
	ProtocolUnknown   ProtocolClass = "unknown"
)

// ProtocolRule represents a protocol classification rule
type ProtocolRule struct {
	Protocol ProtocolClass `yaml:"protocol" json:"protocol"`
	Ports    []uint16      `yaml:"ports" json:"ports"`
	Priority int           `yaml:"priority" json:"priority"`
}

// EnhancedClassification provides enhanced traffic classification
type EnhancedClassification struct {
	IPClass       TrafficClass  `json:"ip_class"`
	ProtocolClass ProtocolClass `json:"protocol_class"`
	Port          uint16        `json:"port"`
	IsEncrypted   bool          `json:"is_encrypted"`
	Priority      int           `json:"priority"`
}
