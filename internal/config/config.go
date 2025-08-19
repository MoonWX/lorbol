package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ConfigPath string         `yaml:"-"`
	Server     ServerConfig   `yaml:"server"`
	Network    NetworkConfig  `yaml:"network"`
	Node       NodeConfig     `yaml:"node"`
	Latency    LatencyConfig  `yaml:"latency"`
	Routing    RoutingConfig  `yaml:"routing"`
	VPN        VPNConfig      `yaml:"vpn"`
	Monitor    MonitorConfig  `yaml:"monitor"`
	Storage    StorageConfig  `yaml:"storage"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type NetworkConfig struct {
	Name      string   `yaml:"name"`
	CIDR      string   `yaml:"cidr"`
	Gateway   string   `yaml:"gateway"`
	DNSServers []string `yaml:"dns_servers"`
	Domain    string   `yaml:"domain"`
	MTU       int      `yaml:"mtu"`
}

type NodeConfig struct {
	Name      string `yaml:"name"`
	VirtualIP string `yaml:"virtual_ip"`
	PublicIP  string `yaml:"public_ip"`
	PrivateIP string `yaml:"private_ip"`
	Port      int    `yaml:"port"`
	AutoDetectIP bool `yaml:"auto_detect_ip"`
}

type LatencyConfig struct {
	Method         string        `yaml:"method"`
	Interval       time.Duration `yaml:"interval"`
	Timeout        time.Duration `yaml:"timeout"`
	SampleSize     int           `yaml:"sample_size"`
	TargetEndpoints []string     `yaml:"target_endpoints"`
}

type RoutingConfig struct {
	Algorithm        string        `yaml:"algorithm"`
	OptimizeInterval time.Duration `yaml:"optimize_interval"`
	LatencyThreshold time.Duration `yaml:"latency_threshold"`
	MaxRetries       int           `yaml:"max_retries"`
}

type VPNConfig struct {
	Interface   string   `yaml:"interface"`
	LocalIP     string   `yaml:"local_ip"`
	RemoteIPs   []string `yaml:"remote_ips"`
	Port        int      `yaml:"port"`
	Protocol    string   `yaml:"protocol"`
	Encryption  string   `yaml:"encryption"`
}

type MonitorConfig struct {
	Enabled        bool          `yaml:"enabled"`
	MetricsPort    int           `yaml:"metrics_port"`
	UpdateInterval time.Duration `yaml:"update_interval"`
}

type StorageConfig struct {
	Type     string `yaml:"type"`
	Path     string `yaml:"path"`
	MaxSize  int64  `yaml:"max_size"`
	Retention time.Duration `yaml:"retention"`
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

	config.ConfigPath = path
	
	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &config, nil
}

func (c *Config) validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}

	if c.Network.CIDR == "" {
		return fmt.Errorf("network CIDR is required")
	}

	if c.Node.VirtualIP == "" {
		return fmt.Errorf("node virtual IP is required")
	}

	if c.Node.Port <= 0 || c.Node.Port > 65535 {
		return fmt.Errorf("invalid node port: %d", c.Node.Port)
	}

	if c.Latency.Interval <= 0 {
		return fmt.Errorf("latency interval must be positive")
	}

	if c.Latency.Timeout <= 0 {
		return fmt.Errorf("latency timeout must be positive")
	}

	return nil
}

func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
		Network: NetworkConfig{
			Name:       "lorbol-net",
			CIDR:       "10.0.0.0/8",
			Gateway:    "10.0.0.1",
			DNSServers: []string{"8.8.8.8", "1.1.1.1"},
			Domain:     "lorbol.local",
			MTU:        1420,
		},
		Node: NodeConfig{
			Name:         "lorbol-node",
			VirtualIP:    "10.0.0.2",
			PublicIP:     "",
			PrivateIP:    "",
			Port:         51820,
			AutoDetectIP: true,
		},
		Latency: LatencyConfig{
			Method:     "icmp",
			Interval:   30 * time.Second,
			Timeout:    5 * time.Second,
			SampleSize: 5,
			TargetEndpoints: []string{
				"8.8.8.8",
				"1.1.1.1",
			},
		},
		Routing: RoutingConfig{
			Algorithm:        "dijkstra",
			OptimizeInterval: 5 * time.Minute,
			LatencyThreshold: 100 * time.Millisecond,
			MaxRetries:       3,
		},
		VPN: VPNConfig{
			Interface:  "lorbol0",
			LocalIP:    "10.0.0.2",
			RemoteIPs:  []string{},
			Port:       51820,
			Protocol:   "udp",
			Encryption: "chacha20poly1305",
		},
		Monitor: MonitorConfig{
			Enabled:        true,
			MetricsPort:    9090,
			UpdateInterval: 10 * time.Second,
		},
		Storage: StorageConfig{
			Type:      "file",
			Path:      "./data",
			MaxSize:   100 * 1024 * 1024, // 100MB
			Retention: 7 * 24 * time.Hour, // 7 days
		},
	}
}