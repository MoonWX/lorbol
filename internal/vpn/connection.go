package vpn

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/MoonWX/lorbol/internal/config"
)

type ConnectionManager struct {
	config      config.VPNConfig
	tunnels     map[string]Tunnel
	activeTunnel string
	mu          sync.RWMutex
	stopCh      chan struct{}
	isRunning   bool
}

type ConnectionStats struct {
	BytesSent     int64
	BytesReceived int64
	PacketsSent   int64
	PacketsReceived int64
	LastActivity  time.Time
	Uptime        time.Duration
}

func NewConnectionManager(cfg config.VPNConfig) *ConnectionManager {
	return &ConnectionManager{
		config:  cfg,
		tunnels: make(map[string]Tunnel),
		stopCh:  make(chan struct{}),
	}
}

func (cm *ConnectionManager) Start(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.isRunning {
		return fmt.Errorf("connection manager is already running")
	}

	cm.isRunning = true
	cm.stopCh = make(chan struct{})

	for _, remoteIP := range cm.config.RemoteIPs {
		tunnel := NewTunnel(cm.config)
		cm.tunnels[remoteIP] = tunnel
	}

	go cm.managementLoop(ctx)
	
	return nil
}

func (cm *ConnectionManager) Stop() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if !cm.isRunning {
		return nil
	}

	close(cm.stopCh)
	
	for _, tunnel := range cm.tunnels {
		tunnel.Close()
	}
	
	cm.tunnels = make(map[string]Tunnel)
	cm.activeTunnel = ""
	cm.isRunning = false
	
	return nil
}

func (cm *ConnectionManager) ConnectTo(endpoint string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	tunnel, exists := cm.tunnels[endpoint]
	if !exists {
		tunnel = NewTunnel(cm.config)
		cm.tunnels[endpoint] = tunnel
	}

	if err := tunnel.Establish(endpoint); err != nil {
		return fmt.Errorf("failed to connect to %s: %w", endpoint, err)
	}

	if cm.activeTunnel != "" && cm.activeTunnel != endpoint {
		if oldTunnel, exists := cm.tunnels[cm.activeTunnel]; exists {
			oldTunnel.Close()
		}
	}

	cm.activeTunnel = endpoint
	
	return nil
}

func (cm *ConnectionManager) GetActiveTunnel() (Tunnel, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.activeTunnel == "" {
		return nil, fmt.Errorf("no active tunnel")
	}

	tunnel, exists := cm.tunnels[cm.activeTunnel]
	if !exists {
		return nil, fmt.Errorf("active tunnel not found")
	}

	return tunnel, nil
}

func (cm *ConnectionManager) SwitchTunnel(endpoint string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.activeTunnel == endpoint {
		return nil
	}

	tunnel, exists := cm.tunnels[endpoint]
	if !exists {
		return fmt.Errorf("tunnel to %s does not exist", endpoint)
	}

	if !tunnel.IsConnected() {
		if err := tunnel.Establish(endpoint); err != nil {
			return fmt.Errorf("failed to establish tunnel to %s: %w", endpoint, err)
		}
	}

	if cm.activeTunnel != "" {
		if oldTunnel, exists := cm.tunnels[cm.activeTunnel]; exists {
			oldTunnel.Close()
		}
	}

	cm.activeTunnel = endpoint
	
	return nil
}

func (cm *ConnectionManager) GetConnectionStats() (ConnectionStats, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return ConnectionStats{
		BytesSent:       0,
		BytesReceived:   0,
		PacketsSent:     0,
		PacketsReceived: 0,
		LastActivity:    time.Now(),
		Uptime:          time.Since(time.Now()),
	}, nil
}

func (cm *ConnectionManager) ListTunnels() map[string]bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	result := make(map[string]bool)
	for endpoint, tunnel := range cm.tunnels {
		result[endpoint] = tunnel.IsConnected()
	}
	
	return result
}

func (cm *ConnectionManager) managementLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-cm.stopCh:
			return
		case <-ticker.C:
			cm.healthCheck()
		}
	}
}

func (cm *ConnectionManager) healthCheck() {
	cm.mu.RLock()
	tunnels := make(map[string]Tunnel)
	for k, v := range cm.tunnels {
		tunnels[k] = v
	}
	activeTunnel := cm.activeTunnel
	cm.mu.RUnlock()

	for endpoint, tunnel := range tunnels {
		if !tunnel.IsConnected() && endpoint == activeTunnel {
			tunnel.Establish(endpoint)
		}
	}
}