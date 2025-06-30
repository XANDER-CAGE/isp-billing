package models

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
)

// TrafficClassRule represents a traffic classification rule
// Equivalent to traffic class configuration in tclass.erl
type TrafficClassRule struct {
	Name     string   `yaml:"name"`
	Networks []string `yaml:"networks"`
	Priority int      `yaml:"priority"` // For handling overlaps
	CostIn   float64  `yaml:"cost_in"`
	CostOut  float64  `yaml:"cost_out"`
}

// IPClassRange represents an IP address range for classification
// Equivalent to {Start, End, Class} triple in tclass.erl
type IPClassRange struct {
	Start uint32 // Start IP as 32-bit integer
	End   uint32 // End IP as 32-bit integer
	Class string // Traffic class name
}

// IPSearchTree represents binary search tree for IP classification
// Equivalent to ip_search_tree() in tclass.erl
type IPSearchTree struct {
	Root *TreeNode
}

// TreeNode represents a node in the binary search tree
// Equivalent to tree_node in tclass.erl
type TreeNode struct {
	Start uint32    // Start of IP range
	End   uint32    // End of IP range
	Class string    // Traffic class
	Left  *TreeNode // Left subtree
	Right *TreeNode // Right subtree
}

// ClassificationRule represents a complete rule with metadata
type ClassificationRule struct {
	Class    string        `json:"class"`
	Network  string        `json:"network"`
	Priority int           `json:"priority"`
	CostIn   float64       `json:"cost_in"`
	CostOut  float64       `json:"cost_out"`
	Range    *IPClassRange `json:"range"`
}

// ClassificationResult represents the result of IP classification
type ClassificationResult struct {
	Class   string  `json:"class"`
	CostIn  float64 `json:"cost_in"`
	CostOut float64 `json:"cost_out"`
	Network string  `json:"network,omitempty"`
	Found   bool    `json:"found"`
}

// TrafficClassConfig represents traffic class configuration
// Equivalent to traffic class configuration file format
type TrafficClassConfig struct {
	Classes []TrafficClassRule `yaml:"classes"`
}

// NewIPSearchTree creates a new empty search tree
// Equivalent to empty_tree() in tclass.erl
func NewIPSearchTree() *IPSearchTree {
	return &IPSearchTree{Root: nil}
}

// BuildTree constructs binary search tree from IP ranges
// Equivalent to tree_from_list/2 in tclass.erl
func (tree *IPSearchTree) BuildTree(ranges []IPClassRange) error {
	if len(ranges) == 0 {
		tree.Root = nil
		return nil
	}

	// Sort ranges by start IP for balanced tree construction
	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].Start < ranges[j].Start
	})

	// Check for overlaps (like check_overlaps in tclass.erl)
	if err := CheckOverlaps(ranges); err != nil {
		return err
	}

	tree.Root = tree.buildTreeRecursive(ranges, 0, len(ranges))
	return nil
}

// buildTreeRecursive recursively builds balanced binary tree
func (tree *IPSearchTree) buildTreeRecursive(ranges []IPClassRange, start, end int) *TreeNode {
	if start >= end {
		return nil
	}

	// Find middle element for balanced tree
	mid := start + (end-start)/2
	node := &TreeNode{
		Start: ranges[mid].Start,
		End:   ranges[mid].End,
		Class: ranges[mid].Class,
	}

	// Recursively build left and right subtrees
	node.Left = tree.buildTreeRecursive(ranges, start, mid)
	node.Right = tree.buildTreeRecursive(ranges, mid+1, end)

	return node
}

// Search finds traffic class for given IP
// Equivalent to tree_search/2 in tclass.erl
func (tree *IPSearchTree) Search(ip uint32) (string, bool) {
	return tree.searchRecursive(tree.Root, ip)
}

// searchRecursive performs recursive search in tree
func (tree *IPSearchTree) searchRecursive(node *TreeNode, ip uint32) (string, bool) {
	if node == nil {
		return "", false
	}

	// IP is in current range
	if ip >= node.Start && ip <= node.End {
		return node.Class, true
	}

	// Search left subtree
	if ip < node.Start {
		return tree.searchRecursive(node.Left, ip)
	}

	// Search right subtree
	return tree.searchRecursive(node.Right, ip)
}

// CheckOverlaps detects overlapping IP ranges
// Equivalent to check_overlaps/1 in tclass.erl
func CheckOverlaps(ranges []IPClassRange) error {
	if len(ranges) <= 1 {
		return nil
	}

	for i := 0; i < len(ranges)-1; i++ {
		current := ranges[i]
		next := ranges[i+1]

		// Check if ranges overlap
		if next.Start <= current.End {
			return fmt.Errorf("overlapping ranges detected: %s [%s - %s] and %s [%s - %s]",
				current.Class, IPToString(current.Start), IPToString(current.End),
				next.Class, IPToString(next.Start), IPToString(next.End))
		}
	}

	return nil
}

// ParseNetwork converts network string to IP range
// Equivalent to network_range/1 in tclass.erl
func ParseNetwork(network string) (*IPClassRange, error) {
	var ip string
	var mask int
	var err error

	// Parse CIDR notation (192.168.1.0/24) or single IP
	if strings.Contains(network, "/") {
		parts := strings.Split(network, "/")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid network format: %s", network)
		}
		ip = parts[0]
		mask, err = strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid mask in network %s: %v", network, err)
		}
	} else {
		ip = network
		mask = 32 // Single IP
	}

	// Parse IP address
	ipAddr := net.ParseIP(ip)
	if ipAddr == nil {
		return nil, fmt.Errorf("invalid IP address: %s", ip)
	}

	// Convert to IPv4 if needed
	ipv4 := ipAddr.To4()
	if ipv4 == nil {
		return nil, fmt.Errorf("IPv6 not supported: %s", ip)
	}

	// Convert IP to 32-bit integer
	startIP := IPToUint32(ipv4)

	// Calculate network range
	if mask < 0 || mask > 32 {
		return nil, fmt.Errorf("invalid mask: %d", mask)
	}

	// Calculate network start and end
	maskBits := uint32(0xFFFFFFFF << (32 - mask))
	networkStart := startIP & maskBits
	networkEnd := networkStart | (0xFFFFFFFF >> mask)

	return &IPClassRange{
		Start: networkStart,
		End:   networkEnd,
	}, nil
}

// IPToUint32 converts net.IP to 32-bit integer
func IPToUint32(ip net.IP) uint32 {
	ipv4 := ip.To4()
	if ipv4 == nil {
		return 0
	}
	return uint32(ipv4[0])<<24 | uint32(ipv4[1])<<16 | uint32(ipv4[2])<<8 | uint32(ipv4[3])
}

// Uint32ToIP converts 32-bit integer to net.IP
func Uint32ToIP(ip uint32) net.IP {
	return net.IPv4(
		byte(ip>>24),
		byte(ip>>16),
		byte(ip>>8),
		byte(ip),
	)
}

// IPToString converts 32-bit integer IP to string
func IPToString(ip uint32) string {
	return Uint32ToIP(ip).String()
}

// StringToUint32IP converts IP string to 32-bit integer
func StringToUint32IP(ipStr string) (uint32, error) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return 0, fmt.Errorf("invalid IP address: %s", ipStr)
	}
	return IPToUint32(ip), nil
}

// ClassesToIPRanges converts traffic classes to IP ranges
// Equivalent to class_to_triples/1 in tclass.erl
func ClassesToIPRanges(classes []TrafficClassRule) ([]IPClassRange, error) {
	var ranges []IPClassRange

	for _, class := range classes {
		for _, network := range class.Networks {
			ipRange, err := ParseNetwork(network)
			if err != nil {
				return nil, fmt.Errorf("error parsing network %s for class %s: %v",
					network, class.Name, err)
			}
			ipRange.Class = class.Name
			ranges = append(ranges, *ipRange)
		}
	}

	return ranges, nil
}

// GetTreeStats returns statistics about the search tree
func (tree *IPSearchTree) GetTreeStats() map[string]interface{} {
	stats := make(map[string]interface{})

	if tree.Root == nil {
		stats["nodes"] = 0
		stats["height"] = 0
		stats["ranges"] = 0
		return stats
	}

	stats["nodes"] = tree.countNodes(tree.Root)
	stats["height"] = tree.getHeight(tree.Root)
	stats["ranges"] = tree.countRanges(tree.Root)

	return stats
}

// countNodes counts total nodes in tree
func (tree *IPSearchTree) countNodes(node *TreeNode) int {
	if node == nil {
		return 0
	}
	return 1 + tree.countNodes(node.Left) + tree.countNodes(node.Right)
}

// getHeight calculates tree height
func (tree *IPSearchTree) getHeight(node *TreeNode) int {
	if node == nil {
		return 0
	}

	leftHeight := tree.getHeight(node.Left)
	rightHeight := tree.getHeight(node.Right)

	if leftHeight > rightHeight {
		return leftHeight + 1
	}
	return rightHeight + 1
}

// countRanges counts total IP ranges in tree
func (tree *IPSearchTree) countRanges(node *TreeNode) int {
	if node == nil {
		return 0
	}
	return 1 + tree.countRanges(node.Left) + tree.countRanges(node.Right)
}

// ListAllRanges returns all IP ranges in the tree
func (tree *IPSearchTree) ListAllRanges() []ClassificationRule {
	var rules []ClassificationRule
	tree.collectRanges(tree.Root, &rules)
	return rules
}

// collectRanges recursively collects all ranges from tree
func (tree *IPSearchTree) collectRanges(node *TreeNode, rules *[]ClassificationRule) {
	if node == nil {
		return
	}

	// Add current node
	rule := ClassificationRule{
		Class: node.Class,
		Range: &IPClassRange{
			Start: node.Start,
			End:   node.End,
			Class: node.Class,
		},
	}
	*rules = append(*rules, rule)

	// Recursively collect from subtrees
	tree.collectRanges(node.Left, rules)
	tree.collectRanges(node.Right, rules)
}

// ValidateConfiguration validates traffic class configuration
func ValidateConfiguration(config *TrafficClassConfig) error {
	classNames := make(map[string]bool)

	for _, class := range config.Classes {
		// Check for duplicate class names
		if classNames[class.Name] {
			return fmt.Errorf("duplicate class name: %s", class.Name)
		}
		classNames[class.Name] = true

		// Validate networks
		if len(class.Networks) == 0 {
			return fmt.Errorf("class %s has no networks defined", class.Name)
		}

		for _, network := range class.Networks {
			_, err := ParseNetwork(network)
			if err != nil {
				return fmt.Errorf("invalid network %s in class %s: %v",
					network, class.Name, err)
			}
		}

		// Validate costs
		if class.CostIn < 0 || class.CostOut < 0 {
			return fmt.Errorf("negative costs not allowed in class %s", class.Name)
		}
	}

	return nil
}
