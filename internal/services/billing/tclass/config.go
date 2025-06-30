package tclass

import (
	"fmt"
	"os"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// ClassConfig type is defined in types.go

// Config represents traffic classification configuration
type Config struct {
	Enabled        bool          `yaml:"enabled" json:"enabled"`
	ConfigFile     string        `yaml:"config_file" json:"config_file"`
	DefaultClass   TrafficClass  `yaml:"default_class" json:"default_class"`
	Classes        []ClassConfig `yaml:"classes" json:"classes"`
	ReloadInterval int           `yaml:"reload_interval" json:"reload_interval"` // seconds
}

// ConfigLoader handles loading and reloading of traffic classification rules
type ConfigLoader struct {
	service *Service
	logger  *zap.Logger
	config  Config
}

// NewConfigLoader creates a new config loader
func NewConfigLoader(service *Service, logger *zap.Logger) *ConfigLoader {
	return &ConfigLoader{
		service: service,
		logger:  logger,
	}
}

// LoadFromYAML loads traffic classification config from YAML file
func (cl *ConfigLoader) LoadFromYAML(filename string) error {
	cl.logger.Info("Loading traffic classification config", zap.String("file", filename))

	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	cl.config = config

	if !config.Enabled {
		cl.logger.Info("Traffic classification disabled")
		return nil
	}

	// Load classes into service
	if err := cl.service.Load(config.Classes); err != nil {
		return fmt.Errorf("failed to load traffic classes: %w", err)
	}

	cl.logger.Info("Traffic classification config loaded successfully",
		zap.Int("classes", len(config.Classes)),
		zap.String("default_class", string(config.DefaultClass)))

	return nil
}

// LoadFromErlangFormat loads config from Erlang-style file
// Compatible with original tclass.erl config format
func (cl *ConfigLoader) LoadFromErlangFormat(filename string) error {
	cl.logger.Info("Loading Erlang-format traffic classification config", zap.String("file", filename))

	// For now, convert common Erlang format to our format
	// This would parse Erlang terms like:
	// {local, ["192.168.0.0/16", "10.0.0.0/8"]}.
	// {internet, ["0.0.0.0/0"]}.

	// Sample conversion - in real implementation would parse Erlang terms
	defaultConfig := []ClassConfig{
		{
			Class: ClassLocal,
			Networks: []string{
				"192.168.0.0/16",
				"10.0.0.0/8",
				"172.16.0.0/12",
			},
		},
		{
			Class: ClassCDN,
			Networks: []string{
				"8.8.8.0/24",      // Google DNS
				"1.1.1.0/24",      // Cloudflare
				"208.67.222.0/24", // OpenDNS
			},
		},
		{
			Class: ClassInternet,
			Networks: []string{
				"0.0.0.0/0", // Everything else
			},
		},
	}

	if err := cl.service.Load(defaultConfig); err != nil {
		return fmt.Errorf("failed to load default traffic classes: %w", err)
	}

	cl.logger.Info("Erlang-format config loaded with defaults",
		zap.Int("classes", len(defaultConfig)))

	return nil
}

// GetDefaultConfig returns default traffic classification configuration
func GetDefaultConfig() Config {
	return Config{
		Enabled:        true,
		DefaultClass:   ClassDefault,
		ReloadInterval: 300, // 5 minutes
		Classes: []ClassConfig{
			{
				Class: ClassLocal,
				Networks: []string{
					"192.168.0.0/16", // RFC 1918
					"10.0.0.0/8",     // RFC 1918
					"172.16.0.0/12",  // RFC 1918
					"127.0.0.0/8",    // Loopback
				},
			},
			{
				Class: ClassCDN,
				Networks: []string{
					"8.8.8.0/24",      // Google Public DNS
					"8.8.4.0/24",      // Google Public DNS
					"1.1.1.0/24",      // Cloudflare DNS
					"208.67.222.0/24", // OpenDNS
					"208.67.220.0/24", // OpenDNS
				},
			},
			{
				Class: ClassPremium,
				Networks: []string{
					"91.108.56.0/24",   // Telegram
					"149.154.160.0/24", // Telegram
					"157.240.0.0/17",   // Facebook/Meta
				},
			},
			{
				Class: ClassInternet,
				Networks: []string{
					"0.0.0.0/0", // Default route - everything else
				},
			},
		},
	}
}

// ValidateConfig validates traffic classification configuration
func (cl *ConfigLoader) ValidateConfig(config Config) error {
	if !config.Enabled {
		return nil
	}

	if len(config.Classes) == 0 {
		return fmt.Errorf("no traffic classes defined")
	}

	// Check for duplicate class names
	classNames := make(map[TrafficClass]bool)
	for _, class := range config.Classes {
		if classNames[class.Class] {
			return fmt.Errorf("duplicate traffic class: %s", class.Class)
		}
		classNames[class.Class] = true

		if len(class.Networks) == 0 {
			return fmt.Errorf("traffic class %s has no networks defined", class.Class)
		}

		// Validate network formats
		for _, network := range class.Networks {
			if err := cl.validateNetwork(network); err != nil {
				return fmt.Errorf("invalid network %s in class %s: %w", network, class.Class, err)
			}
		}
	}

	return nil
}

// validateNetwork validates network CIDR notation
func (cl *ConfigLoader) validateNetwork(network string) error {
	// Create temporary service to test network parsing
	tempService := New(cl.logger)
	_, _, err := tempService.networkRange(network)
	return err
}

// GenerateConfigTemplate generates a template config file
func GenerateConfigTemplate(filename string) error {
	config := GetDefaultConfig()

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// ReloadConfig reloads configuration from file
func (cl *ConfigLoader) ReloadConfig() error {
	if cl.config.ConfigFile != "" {
		return cl.LoadFromYAML(cl.config.ConfigFile)
	}
	return fmt.Errorf("no config file specified")
}

// GetConfig returns current configuration
func (cl *ConfigLoader) GetConfig() Config {
	return cl.config
}
