package vpn

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/MoonWX/lorbol/internal/config"
)

type Tunnel interface {
	Establish(endpoint string) error
	Send(packet []byte) error
	Receive() ([]byte, error)
	Close() error
	IsConnected() bool
	GetLocalAddress() string
	GetRemoteAddress() string
}

type tunnel struct {
	config    config.VPNConfig
	conn      net.Conn
	localAddr string
	remoteAddr string
	isConnected bool
	mu         sync.RWMutex
	stopCh     chan struct{}
}

func NewTunnel(cfg config.VPNConfig) Tunnel {
	return &tunnel{
		config: cfg,
		stopCh: make(chan struct{}),
	}
}

func (t *tunnel) Establish(endpoint string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.isConnected {
		return fmt.Errorf("tunnel is already established")
	}

	var network string
	switch t.config.Protocol {
	case "tcp":
		network = "tcp"
	case "udp":
		network = "udp"
	default:
		network = "udp"
	}

	conn, err := net.DialTimeout(network, endpoint, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to establish tunnel to %s: %w", endpoint, err)
	}

	t.conn = conn
	t.localAddr = conn.LocalAddr().String()
	t.remoteAddr = conn.RemoteAddr().String()
	t.isConnected = true

	return nil
}

func (t *tunnel) Send(packet []byte) error {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if !t.isConnected || t.conn == nil {
		return fmt.Errorf("tunnel is not established")
	}

	if err := t.conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}

	_, err := t.conn.Write(packet)
	if err != nil {
		return fmt.Errorf("failed to send packet: %w", err)
	}

	return nil
}

func (t *tunnel) Receive() ([]byte, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if !t.isConnected || t.conn == nil {
		return nil, fmt.Errorf("tunnel is not established")
	}

	if err := t.conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return nil, fmt.Errorf("failed to set read deadline: %w", err)
	}

	buffer := make([]byte, 1500)
	n, err := t.conn.Read(buffer)
	if err != nil {
		return nil, fmt.Errorf("failed to receive packet: %w", err)
	}

	return buffer[:n], nil
}

func (t *tunnel) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.isConnected {
		return nil
	}

	if t.conn != nil {
		err := t.conn.Close()
		t.conn = nil
		if err != nil {
			return fmt.Errorf("failed to close connection: %w", err)
		}
	}

	t.isConnected = false
	close(t.stopCh)
	
	return nil
}

func (t *tunnel) IsConnected() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.isConnected
}

func (t *tunnel) GetLocalAddress() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.localAddr
}

func (t *tunnel) GetRemoteAddress() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.remoteAddr
}