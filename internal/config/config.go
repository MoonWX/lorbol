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

	if c.Latency.Interval <= 0 {
		return fmt.Errorf("latency interval must be positive")
	}

	if c.Latency.Timeout <= 0 {
		return fmt.Errorf("latency timeout must be positive")
	}

	if len(c.Latency.TargetEndpoints) == 0 {
		return fmt.Errorf("at least one target endpoint is required")
	}

	return nil
}

func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host: "localhost",
			Port: 8080,
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
			Algorithm:        "shortest_latency",
			OptimizeInterval: 5 * time.Minute,
			LatencyThreshold: 100 * time.Millisecond,
			MaxRetries:       3,
		},
		VPN: VPNConfig{
			Interface: "tun0",
			LocalIP:   "10.0.0.1",
			Port:      1194,
			Protocol:  "udp",
			Encryption: "aes256",
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