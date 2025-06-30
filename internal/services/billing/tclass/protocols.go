package tclass

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

// ProtocolClass and ProtocolRule types are defined in types.go

// ProtocolClassifier handles protocol-based traffic classification
type ProtocolClassifier struct {
	rules  []ProtocolRule
	logger *zap.Logger
}

// NewProtocolClassifier creates a new protocol classifier
func NewProtocolClassifier(logger *zap.Logger) *ProtocolClassifier {
	pc := &ProtocolClassifier{
		logger: logger,
	}

	// Load default protocol rules
	pc.loadDefaultRules()

	return pc
}

// ClassifyByPort classifies traffic by destination port
func (pc *ProtocolClassifier) ClassifyByPort(port uint16) ProtocolClass {
	for _, rule := range pc.rules {
		for _, rulePort := range rule.Ports {
			if port == rulePort {
				return rule.Protocol
			}
		}
	}
	return ProtocolUnknown
}

// ClassifyByPortRange classifies traffic by port range
func (pc *ProtocolClassifier) ClassifyByPortRange(port uint16, srcPort uint16) ProtocolClass {
	// Check destination port first (higher priority)
	if class := pc.ClassifyByPort(port); class != ProtocolUnknown {
		return class
	}

	// Check source port (for responses)
	if class := pc.ClassifyByPort(srcPort); class != ProtocolUnknown {
		return class
	}

	// Special cases for port ranges
	switch {
	case port >= 1024 && port <= 5000:
		return ProtocolClass("high_ports")
	case port >= 49152 && port <= 65535:
		return ProtocolClass("ephemeral")
	default:
		return ProtocolUnknown
	}
}

// loadDefaultRules loads standard protocol port mappings
func (pc *ProtocolClassifier) loadDefaultRules() {
	pc.rules = []ProtocolRule{
		// Web protocols
		{Protocol: ProtocolHTTP, Ports: []uint16{80, 8080, 8000, 3000}, Priority: 10},
		{Protocol: ProtocolHTTPS, Ports: []uint16{443, 8443}, Priority: 10},

		// File transfer
		{Protocol: ProtocolFTP, Ports: []uint16{20, 21}, Priority: 8},

		// Remote access
		{Protocol: ProtocolSSH, Ports: []uint16{22}, Priority: 9},
		{Protocol: ProtocolTelnet, Ports: []uint16{23}, Priority: 7},

		// Email protocols
		{Protocol: ProtocolSMTP, Ports: []uint16{25, 587, 465}, Priority: 8},
		{Protocol: ProtocolPOP3, Ports: []uint16{110, 995}, Priority: 7},
		{Protocol: ProtocolIMAP, Ports: []uint16{143, 993}, Priority: 7},

		// Network services
		{Protocol: ProtocolDNS, Ports: []uint16{53}, Priority: 9},
		{Protocol: ProtocolDHCP, Ports: []uint16{67, 68}, Priority: 8},
		{Protocol: ProtocolSNMP, Ports: []uint16{161, 162}, Priority: 6},

		// VoIP protocols
		{Protocol: ProtocolVOIP, Ports: []uint16{5060, 5061, 1720, 2427}, Priority: 8},

		// Gaming ports (common ranges)
		{Protocol: ProtocolGaming, Ports: []uint16{27015, 7777, 25565, 19132}, Priority: 6},

		// P2P protocols
		{Protocol: ProtocolP2P, Ports: []uint16{6881, 6882, 6883, 6884, 6885}, Priority: 5},

		// Streaming protocols
		{Protocol: ProtocolStreaming, Ports: []uint16{554, 1935, 8554}, Priority: 7},
	}

	pc.logger.Info("Loaded default protocol rules", zap.Int("count", len(pc.rules)))
}

// AddCustomRule adds a custom protocol classification rule
func (pc *ProtocolClassifier) AddCustomRule(rule ProtocolRule) {
	pc.rules = append(pc.rules, rule)
	pc.logger.Info("Added custom protocol rule",
		zap.String("protocol", string(rule.Protocol)),
		zap.Int("ports", len(rule.Ports)),
		zap.Int("priority", rule.Priority))
}

// LoadRulesFromConfig loads protocol rules from configuration
func (pc *ProtocolClassifier) LoadRulesFromConfig(config []ProtocolRule) {
	pc.rules = append(pc.rules, config...)
	pc.logger.Info("Loaded protocol rules from config", zap.Int("count", len(config)))
}

// EnhancedClassification type is defined in types.go

// EnhancedClassifier combines IP and protocol classification
type EnhancedClassifier struct {
	ipClassifier       *Service
	protocolClassifier *ProtocolClassifier
	logger             *zap.Logger
}

// NewEnhancedClassifier creates a new enhanced classifier
func NewEnhancedClassifier(ipClassifier *Service, protocolClassifier *ProtocolClassifier, logger *zap.Logger) *EnhancedClassifier {
	return &EnhancedClassifier{
		ipClassifier:       ipClassifier,
		protocolClassifier: protocolClassifier,
		logger:             logger,
	}
}

// ClassifyTraffic performs comprehensive traffic classification
func (ec *EnhancedClassifier) ClassifyTraffic(srcIP, dstIP net.IP, srcPort, dstPort uint16) EnhancedClassification {
	// IP-based classification
	ipClass := ec.ipClassifier.Classify(dstIP, ClassDefault)

	// Protocol-based classification
	protocolClass := ec.protocolClassifier.ClassifyByPortRange(dstPort, srcPort)

	// Determine if encrypted
	isEncrypted := ec.isEncryptedTraffic(dstPort, protocolClass)

	// Calculate priority
	priority := ec.calculatePriority(ipClass, protocolClass, isEncrypted)

	return EnhancedClassification{
		IPClass:       ipClass,
		ProtocolClass: protocolClass,
		Port:          dstPort,
		IsEncrypted:   isEncrypted,
		Priority:      priority,
	}
}

// isEncryptedTraffic determines if traffic is encrypted based on port and protocol
func (ec *EnhancedClassifier) isEncryptedTraffic(port uint16, protocol ProtocolClass) bool {
	// Known encrypted protocols
	encryptedPorts := map[uint16]bool{
		443:  true, // HTTPS
		993:  true, // IMAPS
		995:  true, // POP3S
		465:  true, // SMTPS
		5061: true, // SIP TLS
		22:   true, // SSH
	}

	if encryptedPorts[port] {
		return true
	}

	// Check protocol type
	switch protocol {
	case ProtocolHTTPS, ProtocolClass("imaps"), ProtocolClass("pop3s"), ProtocolClass("smtps"):
		return true
	default:
		return false
	}
}

// calculatePriority calculates traffic priority based on classification
func (ec *EnhancedClassifier) calculatePriority(ipClass TrafficClass, protocolClass ProtocolClass, isEncrypted bool) int {
	priority := 5 // Default priority

	// Adjust based on IP class
	switch ipClass {
	case ClassLocal:
		priority += 2
	case ClassPremium:
		priority += 3
	case ClassCDN:
		priority += 1
	case ClassInternet:
		priority += 0
	}

	// Adjust based on protocol
	switch protocolClass {
	case ProtocolVOIP:
		priority += 4 // High priority for VoIP
	case ProtocolDNS:
		priority += 3 // High priority for DNS
	case ProtocolHTTPS, ProtocolHTTP:
		priority += 2 // Medium-high for web
	case ProtocolP2P:
		priority -= 2 // Lower priority for P2P
	case ProtocolStreaming:
		priority += 1 // Medium priority for streaming
	}

	// Bonus for encrypted traffic
	if isEncrypted {
		priority += 1
	}

	// Ensure priority is within bounds
	if priority < 1 {
		priority = 1
	}
	if priority > 10 {
		priority = 10
	}

	return priority
}

// GetProtocolStats returns protocol classification statistics
func (pc *ProtocolClassifier) GetProtocolStats() map[string]interface{} {
	stats := map[string]interface{}{
		"total_rules": len(pc.rules),
		"protocols":   make(map[string]int),
	}

	protocolCounts := make(map[ProtocolClass]int)
	for _, rule := range pc.rules {
		protocolCounts[rule.Protocol] += len(rule.Ports)
	}

	protocolStats := make(map[string]int)
	for protocol, count := range protocolCounts {
		protocolStats[string(protocol)] = count
	}
	stats["protocols"] = protocolStats

	return stats
}

// ParsePortRange parses port range string like "80,443,8080-8090"
func ParsePortRange(portStr string) ([]uint16, error) {
	var ports []uint16

	parts := strings.Split(portStr, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)

		if strings.Contains(part, "-") {
			// Range like "8080-8090"
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) != 2 {
				return nil, fmt.Errorf("invalid port range: %s", part)
			}

			start, err := strconv.ParseUint(strings.TrimSpace(rangeParts[0]), 10, 16)
			if err != nil {
				return nil, fmt.Errorf("invalid start port: %s", rangeParts[0])
			}

			end, err := strconv.ParseUint(strings.TrimSpace(rangeParts[1]), 10, 16)
			if err != nil {
				return nil, fmt.Errorf("invalid end port: %s", rangeParts[1])
			}

			for port := start; port <= end; port++ {
				ports = append(ports, uint16(port))
			}
		} else {
			// Single port
			port, err := strconv.ParseUint(part, 10, 16)
			if err != nil {
				return nil, fmt.Errorf("invalid port: %s", part)
			}
			ports = append(ports, uint16(port))
		}
	}

	return ports, nil
}
