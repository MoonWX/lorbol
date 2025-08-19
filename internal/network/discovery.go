package network

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

type DiscoveryService struct {
	localNode     *Node
	nodeManager   *NodeManager
	multicastAddr *net.UDPAddr
	unicastConn   *net.UDPConn
	multicastConn *net.UDPConn
	peers         map[string]*PeerInfo
	mu            sync.RWMutex
	stopCh        chan struct{}
	isRunning     bool
}

type PeerInfo struct {
	Node         *Node
	LastAnnounce time.Time
	Attempts     int
}

type AnnouncementMessage struct {
	Type      string    `json:"type"`
	NodeID    string    `json:"node_id"`
	Name      string    `json:"name"`
	VirtualIP string    `json:"virtual_ip"`
	PublicIP  string    `json:"public_ip"`
	Port      int       `json:"port"`
	Timestamp time.Time `json:"timestamp"`
	Network   string    `json:"network"`
}

type PeerRequestMessage struct {
	Type        string `json:"type"`
	RequestorID string `json:"requestor_id"`
	Network     string `json:"network"`
}

type PeerResponseMessage struct {
	Type  string  `json:"type"`
	Peers []*Node `json:"peers"`
}

const (
	DefaultMulticastAddr = "224.0.0.251:5353"
	AnnouncementInterval = 30 * time.Second
	PeerTimeout          = 90 * time.Second
)

func NewDiscoveryService(localNode *Node, nodeManager *NodeManager) *DiscoveryService {
	multicastAddr, _ := net.ResolveUDPAddr("udp", DefaultMulticastAddr)
	
	return &DiscoveryService{
		localNode:     localNode,
		nodeManager:   nodeManager,
		multicastAddr: multicastAddr,
		peers:         make(map[string]*PeerInfo),
		stopCh:        make(chan struct{}),
	}
}

func (ds *DiscoveryService) Start(ctx context.Context) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if ds.isRunning {
		return fmt.Errorf("discovery service is already running")
	}

	// Setup multicast connection
	multicastConn, err := net.ListenMulticastUDP("udp", nil, ds.multicastAddr)
	if err != nil {
		return fmt.Errorf("failed to setup multicast listener: %w", err)
	}
	ds.multicastConn = multicastConn

	// Setup unicast connection
	unicastAddr := &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: ds.localNode.Port,
	}
	unicastConn, err := net.ListenUDP("udp", unicastAddr)
	if err != nil {
		multicastConn.Close()
		return fmt.Errorf("failed to setup unicast listener: %w", err)
	}
	ds.unicastConn = unicastConn

	ds.isRunning = true
	ds.stopCh = make(chan struct{})

	go ds.announceLoop(ctx)
	go ds.listenLoop(ctx)
	go ds.cleanupLoop(ctx)

	return nil
}

func (ds *DiscoveryService) Stop() error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if !ds.isRunning {
		return nil
	}

	close(ds.stopCh)
	
	if ds.multicastConn != nil {
		ds.multicastConn.Close()
	}
	if ds.unicastConn != nil {
		ds.unicastConn.Close()
	}

	ds.isRunning = false
	return nil
}

func (ds *DiscoveryService) announceLoop(ctx context.Context) {
	ticker := time.NewTicker(AnnouncementInterval)
	defer ticker.Stop()

	// Send initial announcement
	ds.sendAnnouncement()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ds.stopCh:
			return
		case <-ticker.C:
			ds.sendAnnouncement()
		}
	}
}

func (ds *DiscoveryService) listenLoop(ctx context.Context) {
	buffer := make([]byte, 1024)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ds.stopCh:
			return
		default:
		}

		ds.multicastConn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, addr, err := ds.multicastConn.ReadFromUDP(buffer)
		if err != nil {
			continue
		}

		ds.handleMessage(buffer[:n], addr)
	}
}

func (ds *DiscoveryService) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ds.stopCh:
			return
		case <-ticker.C:
			ds.cleanupStaleePeers()
		}
	}
}

func (ds *DiscoveryService) sendAnnouncement() {
	msg := AnnouncementMessage{
		Type:      "node_announcement",
		NodeID:    ds.localNode.ID,
		Name:      ds.localNode.Name,
		VirtualIP: ds.localNode.VirtualIP.String(),
		PublicIP:  ds.localNode.PublicIP.String(),
		Port:      ds.localNode.Port,
		Timestamp: time.Now(),
		Network:   ds.nodeManager.network.CIDR,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	ds.multicastConn.WriteToUDP(data, ds.multicastAddr)
}

func (ds *DiscoveryService) handleMessage(data []byte, addr *net.UDPAddr) {
	var baseMsg map[string]interface{}
	if err := json.Unmarshal(data, &baseMsg); err != nil {
		return
	}

	msgType, ok := baseMsg["type"].(string)
	if !ok {
		return
	}

	switch msgType {
	case "node_announcement":
		ds.handleAnnouncement(data, addr)
	case "peer_request":
		ds.handlePeerRequest(data, addr)
	case "peer_response":
		ds.handlePeerResponse(data, addr)
	}
}

func (ds *DiscoveryService) handleAnnouncement(data []byte, addr *net.UDPAddr) {
	var msg AnnouncementMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	// Ignore our own announcements
	if msg.NodeID == ds.localNode.ID {
		return
	}

	// Check if the node is in the same network
	if msg.Network != ds.nodeManager.network.CIDR {
		return
	}

	// Create node from announcement
	node, err := NewNode(msg.Name, msg.VirtualIP, msg.PublicIP, msg.Port)
	if err != nil {
		return
	}
	node.ID = msg.NodeID

	// Update peer info
	ds.mu.Lock()
	ds.peers[msg.NodeID] = &PeerInfo{
		Node:         node,
		LastAnnounce: msg.Timestamp,
		Attempts:     0,
	}
	ds.mu.Unlock()

	// Add to node manager
	ds.nodeManager.AddPeer(node)
}

func (ds *DiscoveryService) handlePeerRequest(data []byte, addr *net.UDPAddr) {
	var msg PeerRequestMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	// Ignore our own requests
	if msg.RequestorID == ds.localNode.ID {
		return
	}

	// Check network match
	if msg.Network != ds.nodeManager.network.CIDR {
		return
	}

	// Send peer list
	peers := ds.nodeManager.ListPeers()
	response := PeerResponseMessage{
		Type:  "peer_response",
		Peers: peers,
	}

	responseData, err := json.Marshal(response)
	if err != nil {
		return
	}

	// Send unicast response
	responseAddr := &net.UDPAddr{
		IP:   addr.IP,
		Port: ds.localNode.Port,
	}
	ds.unicastConn.WriteToUDP(responseData, responseAddr)
}

func (ds *DiscoveryService) handlePeerResponse(data []byte, addr *net.UDPAddr) {
	var msg PeerResponseMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	// Add received peers
	for _, peer := range msg.Peers {
		if peer.ID != ds.localNode.ID {
			ds.nodeManager.AddPeer(peer)
		}
	}
}

func (ds *DiscoveryService) RequestPeers() error {
	msg := PeerRequestMessage{
		Type:        "peer_request",
		RequestorID: ds.localNode.ID,
		Network:     ds.nodeManager.network.CIDR,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	_, err = ds.multicastConn.WriteToUDP(data, ds.multicastAddr)
	return err
}

func (ds *DiscoveryService) cleanupStaleePeers() {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	now := time.Now()
	for nodeID, peerInfo := range ds.peers {
		if now.Sub(peerInfo.LastAnnounce) > PeerTimeout {
			delete(ds.peers, nodeID)
			ds.nodeManager.RemovePeer(nodeID)
		}
	}
}