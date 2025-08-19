package network

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/MoonWX/lorbol/pkg/utils"
)

type Node struct {
	ID           string
	Name         string
	VirtualIP    net.IP
	PublicIP     net.IP
	PrivateIP    net.IP
	Port         int
	LastSeen     time.Time
	IsOnline     bool
	Capabilities []string
	Metadata     map[string]string
}

type NodeManager struct {
	localNode    *Node
	peers        map[string]*Node
	network      *VirtualNetwork
	mu           sync.RWMutex
	stopCh       chan struct{}
	isRunning    bool
}

type NodeDiscovery interface {
	DiscoverPeers() ([]*Node, error)
	RegisterNode(node *Node) error
	UpdateNode(node *Node) error
	RemoveNode(nodeID string) error
	GetNode(nodeID string) (*Node, error)
	ListNodes() ([]*Node, error)
}

func NewNode(name, virtualIP, publicIP string, port int) (*Node, error) {
	vip := net.ParseIP(virtualIP)
	if vip == nil {
		return nil, fmt.Errorf("invalid virtual IP: %s", virtualIP)
	}

	pip := net.ParseIP(publicIP)
	if pip == nil {
		return nil, fmt.Errorf("invalid public IP: %s", publicIP)
	}

	return &Node{
		ID:           utils.GenerateID(),
		Name:         name,
		VirtualIP:    vip,
		PublicIP:     pip,
		Port:         port,
		LastSeen:     time.Now(),
		IsOnline:     true,
		Capabilities: []string{"routing", "tunneling"},
		Metadata:     make(map[string]string),
	}, nil
}

func NewNodeManager(localNode *Node, network *VirtualNetwork) *NodeManager {
	return &NodeManager{
		localNode: localNode,
		peers:     make(map[string]*Node),
		network:   network,
		stopCh:    make(chan struct{}),
	}
}

func (nm *NodeManager) Start(ctx context.Context) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if nm.isRunning {
		return fmt.Errorf("node manager is already running")
	}

	nm.isRunning = true
	nm.stopCh = make(chan struct{})

	go nm.discoveryLoop(ctx)
	go nm.heartbeatLoop(ctx)

	return nil
}

func (nm *NodeManager) Stop() error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if !nm.isRunning {
		return nil
	}

	close(nm.stopCh)
	nm.isRunning = false

	return nil
}

func (nm *NodeManager) AddPeer(node *Node) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if node.ID == nm.localNode.ID {
		return fmt.Errorf("cannot add self as peer")
	}

	if !nm.network.IsIPInNetwork(node.VirtualIP) {
		return fmt.Errorf("node virtual IP %s is not in network %s", node.VirtualIP, nm.network.CIDR)
	}

	nm.peers[node.ID] = node
	return nil
}

func (nm *NodeManager) RemovePeer(nodeID string) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	delete(nm.peers, nodeID)
	return nil
}

func (nm *NodeManager) GetPeer(nodeID string) (*Node, bool) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	node, exists := nm.peers[nodeID]
	return node, exists
}

func (nm *NodeManager) GetLocalNode() *Node {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return nm.localNode
}

func (nm *NodeManager) ListPeers() []*Node {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	peers := make([]*Node, 0, len(nm.peers))
	for _, node := range nm.peers {
		peers = append(peers, node)
	}

	return peers
}

func (nm *NodeManager) GetAllNodes() []*Node {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	nodes := make([]*Node, 0, len(nm.peers)+1)
	nodes = append(nodes, nm.localNode)
	
	for _, node := range nm.peers {
		nodes = append(nodes, node)
	}

	return nodes
}

func (nm *NodeManager) UpdatePeerStatus(nodeID string, isOnline bool) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if node, exists := nm.peers[nodeID]; exists {
		node.IsOnline = isOnline
		node.LastSeen = time.Now()
		return nil
	}

	return fmt.Errorf("peer not found: %s", nodeID)
}

func (nm *NodeManager) GetOnlinePeers() []*Node {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	var onlinePeers []*Node
	for _, node := range nm.peers {
		if node.IsOnline {
			onlinePeers = append(onlinePeers, node)
		}
	}

	return onlinePeers
}

func (nm *NodeManager) discoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-nm.stopCh:
			return
		case <-ticker.C:
			nm.performDiscovery()
		}
	}
}

func (nm *NodeManager) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-nm.stopCh:
			return
		case <-ticker.C:
			nm.checkPeerHeartbeats()
		}
	}
}

func (nm *NodeManager) performDiscovery() {
	// Implementation for peer discovery
	// This could use multicast, broadcast, or a central registry
}

func (nm *NodeManager) checkPeerHeartbeats() {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	timeout := 60 * time.Second
	now := time.Now()

	for _, node := range nm.peers {
		if now.Sub(node.LastSeen) > timeout {
			node.IsOnline = false
		}
	}
}