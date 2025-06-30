package tclass

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"netspire-go/internal/models"

	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
)

// Service handles traffic classification
// Full equivalent to tclass.erl gen_server
type Service struct {
	tree    *models.IPSearchTree
	config  *models.TrafficClassConfig
	classes map[string]*models.TrafficClassRule // name -> class mapping
	logger  *zap.Logger
	mu      sync.RWMutex
}

// Config holds traffic classification service configuration
type Config struct {
	ConfigFile     string `yaml:"config_file"`      // Path to traffic classes config file
	DefaultClass   string `yaml:"default_class"`    // Default traffic class
	ReloadOnChange bool   `yaml:"reload_on_change"` // Auto-reload on file change
}

// New creates a new traffic classification service
// Equivalent to start_link/0 in tclass.erl
func New(logger *zap.Logger, config Config) *Service {
	return &Service{
		tree:    models.NewIPSearchTree(),
		classes: make(map[string]*models.TrafficClassRule),
		logger:  logger,
	}
}

// Start initializes the traffic classification service
func (s *Service) Start(config Config) error {
	s.logger.Info("Starting traffic classification service",
		zap.String("config_file", config.ConfigFile),
		zap.String("default_class", config.DefaultClass))

	// Load configuration if file specified
	if config.ConfigFile != "" {
		if err := s.LoadFromFile(config.ConfigFile); err != nil {
			return fmt.Errorf("failed to load traffic classes config: %w", err)
		}
	}

	return nil
}

// LoadFromFile loads traffic classification rules from file
// Equivalent to load/1 in tclass.erl
func (s *Service) LoadFromFile(filename string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info("Loading traffic classes from file", zap.String("file", filename))

	// Read file
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filename, err)
	}

	// Parse YAML configuration
	var config models.TrafficClassConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse YAML in %s: %w", filename, err)
	}

	// Validate configuration
	if err := models.ValidateConfiguration(&config); err != nil {
		return fmt.Errorf("invalid configuration in %s: %w", filename, err)
	}

	// Build classification tree
	if err := s.buildTreeFromConfig(&config); err != nil {
		return fmt.Errorf("failed to build classification tree: %w", err)
	}

	s.config = &config
	s.logger.Info("Successfully loaded traffic classes",
		zap.String("file", filename),
		zap.Int("classes", len(config.Classes)))

	return nil
}

// LoadFromConfig loads traffic classification rules from config object
func (s *Service) LoadFromConfig(config *models.TrafficClassConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info("Loading traffic classes from config object")

	// Validate configuration
	if err := models.ValidateConfiguration(config); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Build classification tree
	if err := s.buildTreeFromConfig(config); err != nil {
		return fmt.Errorf("failed to build classification tree: %w", err)
	}

	s.config = config
	s.logger.Info("Successfully loaded traffic classes from config",
		zap.Int("classes", len(config.Classes)))

	return nil
}

// Classify classifies an IP address and returns traffic class
// Equivalent to classify/1 in tclass.erl
func (s *Service) Classify(ip string) (*models.ClassificationResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Convert IP string to uint32
	ipUint32, err := models.StringToUint32IP(ip)
	if err != nil {
		return nil, fmt.Errorf("invalid IP address %s: %w", ip, err)
	}

	// Search in tree
	className, found := s.tree.Search(ipUint32)
	if !found {
		return &models.ClassificationResult{
			Class: "",
			Found: false,
		}, nil
	}

	// Get class details
	class, exists := s.classes[className]
	if !exists {
		return nil, fmt.Errorf("class %s not found in configuration", className)
	}

	return &models.ClassificationResult{
		Class:   className,
		CostIn:  class.CostIn,
		CostOut: class.CostOut,
		Found:   true,
	}, nil
}

// ClassifyWithDefault classifies IP and returns default if not found
// Equivalent to classify/2 in tclass.erl
func (s *Service) ClassifyWithDefault(ip string, defaultClass string) (*models.ClassificationResult, error) {
	result, err := s.Classify(ip)
	if err != nil {
		return nil, err
	}

	if !result.Found {
		// Get default class details
		class, exists := s.classes[defaultClass]
		if !exists {
			return &models.ClassificationResult{
				Class:   defaultClass,
				CostIn:  0.0,
				CostOut: 0.0,
				Found:   true,
			}, nil
		}

		return &models.ClassificationResult{
			Class:   defaultClass,
			CostIn:  class.CostIn,
			CostOut: class.CostOut,
			Found:   true,
		}, nil
	}

	return result, nil
}

// GetAllClasses returns all configured traffic classes
func (s *Service) GetAllClasses() map[string]*models.TrafficClassRule {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Return copy to prevent external modification
	result := make(map[string]*models.TrafficClassRule)
	for name, class := range s.classes {
		classCopy := *class
		result[name] = &classCopy
	}

	return result
}

// GetClass returns specific traffic class by name
func (s *Service) GetClass(name string) (*models.TrafficClassRule, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	class, exists := s.classes[name]
	if !exists {
		return nil, false
	}

	// Return copy to prevent external modification
	classCopy := *class
	return &classCopy, true
}

// GetTreeStats returns statistics about classification tree
func (s *Service) GetTreeStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := s.tree.GetTreeStats()
	stats["total_classes"] = len(s.classes)

	// Add class statistics
	classStats := make(map[string]interface{})
	for name, class := range s.classes {
		classStats[name] = map[string]interface{}{
			"networks": len(class.Networks),
			"cost_in":  class.CostIn,
			"cost_out": class.CostOut,
			"priority": class.Priority,
		}
	}
	stats["classes"] = classStats

	return stats
}

// ListAllRanges returns all IP ranges in classification tree
func (s *Service) ListAllRanges() []models.ClassificationRule {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rules := s.tree.ListAllRanges()

	// Add class details to rules
	for i, rule := range rules {
		if class, exists := s.classes[rule.Class]; exists {
			rules[i].CostIn = class.CostIn
			rules[i].CostOut = class.CostOut
			rules[i].Priority = class.Priority
		}
	}

	return rules
}

// AddClass adds or updates a traffic class
func (s *Service) AddClass(class *models.TrafficClassRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate the class
	tempConfig := &models.TrafficClassConfig{
		Classes: []models.TrafficClassRule{*class},
	}
	if err := models.ValidateConfiguration(tempConfig); err != nil {
		return fmt.Errorf("invalid class configuration: %w", err)
	}

	// Add to classes map
	s.classes[class.Name] = class

	// Rebuild tree with updated classes
	var allClasses []models.TrafficClassRule
	for _, c := range s.classes {
		allClasses = append(allClasses, *c)
	}

	config := &models.TrafficClassConfig{Classes: allClasses}
	if err := s.buildTreeFromConfig(config); err != nil {
		// Rollback on error
		delete(s.classes, class.Name)
		return fmt.Errorf("failed to rebuild classification tree: %w", err)
	}

	s.logger.Info("Added traffic class", zap.String("name", class.Name))
	return nil
}

// RemoveClass removes a traffic class
func (s *Service) RemoveClass(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.classes[name]; !exists {
		return fmt.Errorf("class %s not found", name)
	}

	// Remove from classes map
	delete(s.classes, name)

	// Rebuild tree without the removed class
	var allClasses []models.TrafficClassRule
	for _, c := range s.classes {
		allClasses = append(allClasses, *c)
	}

	config := &models.TrafficClassConfig{Classes: allClasses}
	if err := s.buildTreeFromConfig(config); err != nil {
		return fmt.Errorf("failed to rebuild classification tree: %w", err)
	}

	s.logger.Info("Removed traffic class", zap.String("name", name))
	return nil
}

// Reload reloads configuration from file
func (s *Service) Reload() error {
	if s.config == nil {
		return fmt.Errorf("no configuration file loaded")
	}

	// Note: This would need the original filename to be stored
	// For now, this is a placeholder
	s.logger.Info("Reloading traffic classification configuration")
	return nil
}

// ParseConfigFile parses a traffic classification config file
func ParseConfigFile(filename string) (*models.TrafficClassConfig, error) {
	ext := strings.ToLower(filepath.Ext(filename))

	switch ext {
	case ".yaml", ".yml":
		return parseYAMLConfig(filename)
	default:
		return nil, fmt.Errorf("unsupported config file format: %s", ext)
	}
}

// buildTreeFromConfig builds search tree from configuration
func (s *Service) buildTreeFromConfig(config *models.TrafficClassConfig) error {
	// Convert classes to IP ranges
	ranges, err := models.ClassesToIPRanges(config.Classes)
	if err != nil {
		return fmt.Errorf("failed to convert classes to IP ranges: %w", err)
	}

	// Build search tree
	tree := models.NewIPSearchTree()
	if err := tree.BuildTree(ranges); err != nil {
		return fmt.Errorf("failed to build search tree: %w", err)
	}

	// Update classes map
	newClasses := make(map[string]*models.TrafficClassRule)
	for i := range config.Classes {
		class := &config.Classes[i]
		newClasses[class.Name] = class
	}

	// Update service state
	s.tree = tree
	s.classes = newClasses

	return nil
}

// parseYAMLConfig parses YAML configuration file
func parseYAMLConfig(filename string) (*models.TrafficClassConfig, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", filename, err)
	}

	var config models.TrafficClassConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	return &config, nil
}

// ValidateIPAddress validates if string is a valid IP address
func ValidateIPAddress(ip string) error {
	_, err := models.StringToUint32IP(ip)
	return err
}

// GetClassificationPath returns the search path through the tree for debugging
func (s *Service) GetClassificationPath(ip string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ipUint32, err := models.StringToUint32IP(ip)
	if err != nil {
		return nil, fmt.Errorf("invalid IP address %s: %w", ip, err)
	}

	var path []string
	s.traceSearchPath(s.tree.Root, ipUint32, &path)
	return path, nil
}

// traceSearchPath traces the search path through tree for debugging
func (s *Service) traceSearchPath(node *models.TreeNode, ip uint32, path *[]string) bool {
	if node == nil {
		*path = append(*path, "NULL")
		return false
	}

	nodeInfo := fmt.Sprintf("Node[%s-%s:%s]",
		models.IPToString(node.Start),
		models.IPToString(node.End),
		node.Class)
	*path = append(*path, nodeInfo)

	// IP is in current range
	if ip >= node.Start && ip <= node.End {
		*path = append(*path, "MATCH")
		return true
	}

	// Search left subtree
	if ip < node.Start {
		*path = append(*path, "LEFT")
		return s.traceSearchPath(node.Left, ip, path)
	}

	// Search right subtree
	*path = append(*path, "RIGHT")
	return s.traceSearchPath(node.Right, ip, path)
}

// Stop gracefully stops the traffic classification service
func (s *Service) Stop() error {
	s.logger.Info("Stopping traffic classification service")
	return nil
}
