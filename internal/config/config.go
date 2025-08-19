package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Network   NetworkConfig   `yaml:"network"`
	Node      NodeConfig      `yaml:"node"`
	Bootstrap BootstrapConfig `yaml:"bootstrap"`
	TUN       TUNConfig       `yaml:"tun"`
	Latency   LatencyConfig   `yaml:"latency"`
	Routing   RoutingConfig   `yaml:"routing"`
	Logging   LoggingConfig   `yaml:"logging"`
}

type NetworkConfig struct {
	Name string `yaml:"name"`
	CIDR string `yaml:"cidr"`
}

type NodeConfig struct {
	Name       string `yaml:"name"`
	VirtualIP  string `yaml:"virtual_ip"`
	PublicIP   string `yaml:"public_ip,omitempty"`
	ListenPort int    `yaml:"listen_port"`
}

type BootstrapConfig struct {
	Method string                 `yaml:"method"`
	Config map[string]interface{} `yaml:"config"`
}

type TUNConfig struct {
	InterfaceName string `yaml:"interface_name"`
	MTU           int    `yaml:"mtu"`
}

type LatencyConfig struct {
	Method          string        `yaml:"method"`
	Interval        time.Duration `yaml:"interval"`
	Timeout         time.Duration `yaml:"timeout"`
	SampleSize      int           `yaml:"sample_size"`
	TargetEndpoints []string      `yaml:"target_endpoints"`
}

type RoutingConfig struct {
	OptimizeInterval time.Duration `yaml:"optimize_interval"`
	Algorithm        string        `yaml:"algorithm"`
	LatencyThreshold time.Duration `yaml:"latency_threshold"`
}

type LoggingConfig struct {
	Level string `yaml:"level"`
	File  string `yaml:"file,omitempty"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Set defaults
	if config.Node.ListenPort == 0 {
		config.Node.ListenPort = 51820
	}
	if config.TUN.MTU == 0 {
		config.TUN.MTU = 1420
	}
	if config.TUN.InterfaceName == "" {
		config.TUN.InterfaceName = "lorbol0"
	}
	if config.Latency.Interval == 0 {
		config.Latency.Interval = 30 * time.Second
	}
	if config.Latency.Timeout == 0 {
		config.Latency.Timeout = 5 * time.Second
	}
	if config.Latency.SampleSize == 0 {
		config.Latency.SampleSize = 3
	}
	if config.Routing.OptimizeInterval == 0 {
		config.Routing.OptimizeInterval = 60 * time.Second
	}
	if config.Routing.LatencyThreshold == 0 {
		config.Routing.LatencyThreshold = 100 * time.Millisecond
	}
	if config.Logging.Level == "" {
		config.Logging.Level = "info"
	}

	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &config, nil
}

func (c *Config) validate() error {
	if c.Network.Name == "" {
		return fmt.Errorf("network name is required")
	}

	if c.Network.CIDR == "" {
		return fmt.Errorf("network CIDR is required")
	}

	if c.Node.Name == "" {
		return fmt.Errorf("node name is required")
	}

	if c.Node.VirtualIP == "" {
		return fmt.Errorf("node virtual IP is required")
	}

	if c.Node.ListenPort <= 0 || c.Node.ListenPort > 65535 {
		return fmt.Errorf("invalid listen port: %d", c.Node.ListenPort)
	}

	if c.Bootstrap.Method == "" {
		return fmt.Errorf("bootstrap method is required")
	}

	return nil
}