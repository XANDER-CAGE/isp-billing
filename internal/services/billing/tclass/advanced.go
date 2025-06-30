package tclass

import (
	"fmt"
	"net"
	"sort"
	"sync"

	"go.uber.org/zap"
)

// Types are defined in types.go

// IPSearchTree represents a binary search tree for IP classification
// Equivalent to ip_search_tree() in tclass.erl
type IPSearchTree struct {
	Start     uint32        `json:"start"`
	End       uint32        `json:"end"`
	Class     TrafficClass  `json:"class"`
	LeftTree  *IPSearchTree `json:"left_tree"`
	RightTree *IPSearchTree `json:"right_tree"`
}

// Service handles traffic classification
// Equivalent to tclass.erl functionality
type Service struct {
	mu     sync.RWMutex
	tree   *IPSearchTree
	logger *zap.Logger
}

// New creates a new traffic classification service
func New(logger *zap.Logger) *Service {
	return &Service{
		tree:   nil, // empty tree
		logger: logger,
	}
}

// Classify classifies IP address and returns class or default
// Equivalent to classify/2 in tclass.erl
func (s *Service) Classify(ip net.IP, defaultClass TrafficClass) TrafficClass {
	if class, ok := s.ClassifyIP(ip); ok {
		return class
	}
	return defaultClass
}

// ClassifyIP classifies IP address and returns result
// Equivalent to classify/1 in tclass.erl
func (s *Service) ClassifyIP(ip net.IP) (TrafficClass, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ipv4 := ip.To4()
	if ipv4 == nil {
		return ClassDefault, false
	}

	ipInt := ipToUint32(ipv4)
	return s.treeSearch(ipInt, s.tree)
}

// Load loads traffic classification rules from config
// Equivalent to load/1 in tclass.erl
func (s *Service) Load(config []ClassConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info("Loading traffic classes", zap.Int("count", len(config)))

	tree, err := s.buildTree(config)
	if err != nil {
		return fmt.Errorf("failed to build classification tree: %w", err)
	}

	s.tree = tree
	s.logger.Info("Traffic classification tree loaded successfully")
	return nil
}

// treeSearch searches for IP in binary search tree
// Equivalent to tree_search/2 in tclass.erl
func (s *Service) treeSearch(ip uint32, tree *IPSearchTree) (TrafficClass, bool) {
	if tree == nil {
		return "", false
	}

	if ip < tree.Start {
		return s.treeSearch(ip, tree.LeftTree)
	}

	if ip > tree.End {
		return s.treeSearch(ip, tree.RightTree)
	}

	// ip >= tree.Start && ip <= tree.End
	return tree.Class, true
}

// buildTree builds binary search tree from config
// Equivalent to build_tree/1 in tclass.erl
func (s *Service) buildTree(config []ClassConfig) (*IPSearchTree, error) {
	if len(config) == 0 {
		return nil, nil // empty tree
	}

	// Convert config to sorted triples
	var triples []IPRange
	for _, classConfig := range config {
		for _, network := range classConfig.Networks {
			start, end, err := s.networkRange(network)
			if err != nil {
				return nil, fmt.Errorf("invalid network %s in class %s: %w", network, classConfig.Class, err)
			}

			triples = append(triples, IPRange{
				Start: start,
				End:   end,
				Class: classConfig.Class,
			})
		}
	}

	// Sort by start IP
	sort.Slice(triples, func(i, j int) bool {
		return triples[i].Start < triples[j].Start
	})

	// Check for overlaps
	if err := s.checkOverlaps(triples); err != nil {
		return nil, err
	}

	// Build balanced binary search tree
	tree, _ := s.treeFromList(triples, len(triples))
	return tree, nil
}

// checkOverlaps checks for overlapping IP ranges
// Equivalent to check_overlaps/1 in tclass.erl
func (s *Service) checkOverlaps(triples []IPRange) error {
	if len(triples) <= 1 {
		return nil
	}

	for i := 1; i < len(triples); i++ {
		prev := triples[i-1]
		curr := triples[i]

		if curr.Start <= prev.End {
			return fmt.Errorf("overlapping IP ranges: [%s-%s] class=%s overlaps with [%s-%s] class=%s",
				uint32ToIP(prev.Start), uint32ToIP(prev.End), prev.Class,
				uint32ToIP(curr.Start), uint32ToIP(curr.End), curr.Class)
		}
	}

	return nil
}

// treeFromList builds balanced binary search tree from sorted list
// Equivalent to tree_from_list/2 in tclass.erl
func (s *Service) treeFromList(list []IPRange, n int) (*IPSearchTree, []IPRange) {
	if n == 0 {
		return nil, list
	}

	firstHalf := (n - 1) / 2
	secondHalf := n - 1 - firstHalf

	leftTree, remaining := s.treeFromList(list, firstHalf)

	if len(remaining) == 0 {
		return nil, remaining
	}

	// Take middle element
	middle := remaining[0]
	remaining = remaining[1:]

	rightTree, finalRemaining := s.treeFromList(remaining, secondHalf)

	node := &IPSearchTree{
		Start:     middle.Start,
		End:       middle.End,
		Class:     middle.Class,
		LeftTree:  leftTree,
		RightTree: rightTree,
	}

	return node, finalRemaining
}

// networkRange converts CIDR notation to start/end IP range
// Equivalent to network_range/1 in tclass.erl
func (s *Service) networkRange(network string) (uint32, uint32, error) {
	// Parse CIDR notation
	ip, ipNet, err := net.ParseCIDR(network)
	if err != nil {
		// Try parsing as single IP
		ip = net.ParseIP(network)
		if ip == nil {
			return 0, 0, fmt.Errorf("invalid IP or CIDR: %s", network)
		}

		ipv4 := ip.To4()
		if ipv4 == nil {
			return 0, 0, fmt.Errorf("IPv6 not supported: %s", network)
		}

		ipInt := ipToUint32(ipv4)
		return ipInt, ipInt, nil
	}

	ipv4 := ip.To4()
	if ipv4 == nil {
		return 0, 0, fmt.Errorf("IPv6 not supported: %s", network)
	}

	maskSize, _ := ipNet.Mask.Size()
	start := ipToUint32(ipv4) & (0xFFFFFFFF << (32 - maskSize))
	end := start + (1 << (32 - maskSize)) - 1

	return start, end, nil
}

// Helper functions for IP conversion

// ipToUint32 converts IPv4 to uint32
func ipToUint32(ip net.IP) uint32 {
	return uint32(ip[0])<<24 + uint32(ip[1])<<16 + uint32(ip[2])<<8 + uint32(ip[3])
}

// uint32ToIP converts uint32 to IPv4 string
func uint32ToIP(ip uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		(ip>>24)&0xFF,
		(ip>>16)&0xFF,
		(ip>>8)&0xFF,
		ip&0xFF)
}

// GetStats returns classification statistics
func (s *Service) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := map[string]interface{}{
		"loaded": s.tree != nil,
		"depth":  s.treeDepth(s.tree),
		"nodes":  s.treeNodes(s.tree),
	}

	return stats
}

// treeDepth calculates tree depth
func (s *Service) treeDepth(tree *IPSearchTree) int {
	if tree == nil {
		return 0
	}

	leftDepth := s.treeDepth(tree.LeftTree)
	rightDepth := s.treeDepth(tree.RightTree)

	if leftDepth > rightDepth {
		return leftDepth + 1
	}
	return rightDepth + 1
}

// treeNodes counts tree nodes
func (s *Service) treeNodes(tree *IPSearchTree) int {
	if tree == nil {
		return 0
	}

	return 1 + s.treeNodes(tree.LeftTree) + s.treeNodes(tree.RightTree)
}

// TestClassification tests classification with sample IPs
func (s *Service) TestClassification() {
	testIPs := []string{
		"192.168.1.1",
		"10.0.0.1",
		"8.8.8.8",
		"1.1.1.1",
		"172.16.0.1",
	}

	s.logger.Info("Testing traffic classification")
	for _, ipStr := range testIPs {
		ip := net.ParseIP(ipStr)
		if ip != nil {
			class := s.Classify(ip, ClassDefault)
			s.logger.Info("Classification result",
				zap.String("ip", ipStr),
				zap.String("class", string(class)))
		}
	}
}
