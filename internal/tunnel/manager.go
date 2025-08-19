package tunnel

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/MoonWX/lorbol/internal/config"
)

type TunnelManager struct {
	config       config.VPNConfig
	wireguard    *WireGuardTunnel
	nodeManager  NodeManager
	localNodeID  string
	mu           sync.RWMutex
	isRunning    bool
	stopCh       chan struct{}
}

type NodeManager interface {
	GetAllNodes() []*Node
	GetLocalNode() *Node
	GetOnlinePeers() []*Node
}

type Node struct {
	ID        string
	Name      string
	VirtualIP net.IP
	PublicIP  net.IP
	Port      int
	IsOnline  bool
	PublicKey []byte
}

type KeyExchangeMessage struct {
	Type      string `json:"type"`
	NodeID    string `json:"node_id"`
	PublicKey string `json:"public_key"`
	Endpoint  string `json:"endpoint"`
}

func NewTunnelManager(cfg config.VPNConfig, nodeManager NodeManager) (*TunnelManager, error) {
	localNode := nodeManager.GetLocalNode()
	if localNode == nil {
		return nil, fmt.Errorf("local node not found")
	}

	localIP := net.ParseIP(cfg.LocalIP)
	if localIP == nil {
		return nil, fmt.Errorf("invalid local IP: %s", cfg.LocalIP)
	}

	wg, err := NewWireGuardTunnel(cfg.Interface, localIP, cfg.Port)
	if err != nil {
		return nil, fmt.Errorf("failed to create WireGuard tunnel: %w", err)
	}

	return &TunnelManager{
		config:      cfg,
		wireguard:   wg,
		nodeManager: nodeManager,
		localNodeID: localNode.ID,
		stopCh:      make(chan struct{}),
	}, nil
}

func (tm *TunnelManager) Start(ctx context.Context) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.isRunning {
		return fmt.Errorf("tunnel manager is already running")
	}

	// Setup WireGuard interface
	if err := tm.wireguard.SetupInterface(); err != nil {
		return fmt.Errorf("failed to setup interface: %w", err)
	}

	tm.isRunning = true
	tm.stopCh = make(chan struct{})

	// Start peer management loop
	go tm.peerManagementLoop(ctx)

	return nil
}

func (tm *TunnelManager) Stop() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if !tm.isRunning {
		return nil
	}

	close(tm.stopCh)
	
	// Close WireGuard interface
	if err := tm.wireguard.Close(); err != nil {
		return fmt.Errorf("failed to close WireGuard interface: %w", err)
	}

	tm.isRunning = false
	return nil
}

func (tm *TunnelManager) GetPublicKey() []byte {
	return tm.wireguard.GetPublicKey()
}

func (tm *TunnelManager) peerManagementLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tm.stopCh:
			return
		case <-ticker.C:
			tm.managePeers()
		}
	}
}

func (tm *TunnelManager) managePeers() {
	peers := tm.nodeManager.GetOnlinePeers()
	
	for _, peer := range peers {
		if peer.ID == tm.localNodeID {
			continue
		}

		// Check if peer has public key (received via key exchange)
		if len(peer.PublicKey) == 0 {
			continue
		}

		// Create allowed IPs for this peer
		allowedIPs := []net.IPNet{
			{
				IP:   peer.VirtualIP,
				Mask: net.CIDRMask(32, 32), // /32 for single host
			},
		}

		// Add peer endpoint
		endpoint := fmt.Sprintf("%s:%d", peer.PublicIP.String(), peer.Port)

		// Add or update peer
		if err := tm.wireguard.AddPeer(peer.ID, peer.PublicKey, endpoint, allowedIPs); err != nil {
			fmt.Printf("Failed to add peer %s: %v\n", peer.ID, err)
			continue
		}

		fmt.Printf("Added WireGuard peer: %s (%s)\n", peer.Name, peer.VirtualIP)
	}
}

func (tm *TunnelManager) HandleKeyExchange(nodeID string, publicKey []byte, endpoint string) error {
	// This would be called when receiving a key exchange message
	peer := tm.findPeerByID(nodeID)
	if peer == nil {
		return fmt.Errorf("peer %s not found", nodeID)
	}

	// Store the public key
	peer.PublicKey = publicKey

	// Create allowed IPs
	allowedIPs := []net.IPNet{
		{
			IP:   peer.VirtualIP,
			Mask: net.CIDRMask(32, 32),
		},
	}

	// Add peer to WireGuard
	return tm.wireguard.AddPeer(nodeID, publicKey, endpoint, allowedIPs)
}

func (tm *TunnelManager) findPeerByID(nodeID string) *Node {
	peers := tm.nodeManager.GetAllNodes()
	for _, peer := range peers {
		if peer.ID == nodeID {
			return peer
		}
	}
	return nil
}

func (tm *TunnelManager) AddRoute(destination, gateway string) error {
	return tm.wireguard.AddRoute(destination, gateway)
}

func (tm *TunnelManager) RemoveRoute(destination string) error {
	return tm.wireguard.RemoveRoute(destination)
}

func (tm *TunnelManager) GetTunnelStats() (map[string]*WireGuardPeer, error) {
	return tm.wireguard.GetPeerStats()
}

func (tm *TunnelManager) UpdatePeerEndpoint(peerID, newEndpoint string) error {
	return tm.wireguard.UpdatePeerEndpoint(peerID, newEndpoint)
}