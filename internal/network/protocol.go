package network

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

type P2PProtocol struct {
	localNode     *Node
	nodeManager   *NodeManager
	connections   map[string]*P2PConnection
	listener      net.Listener
	mu            sync.RWMutex
	stopCh        chan struct{}
	isRunning     bool
	messageHandlers map[MessageType]MessageHandler
}

type P2PConnection struct {
	RemoteNode *Node
	Conn       net.Conn
	LastPing   time.Time
	IsActive   bool
	SendCh     chan *Message
	RecvCh     chan *Message
}

type MessageType string

const (
	MessageTypePing        MessageType = "ping"
	MessageTypePong        MessageType = "pong"
	MessageTypeData        MessageType = "data"
	MessageTypeRouteUpdate MessageType = "route_update"
	MessageTypeDiscovery   MessageType = "discovery"
	MessageTypeLatency     MessageType = "latency"
)

type Message struct {
	Type      MessageType `json:"type"`
	From      string      `json:"from"`
	To        string      `json:"to"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
	ID        string      `json:"id"`
}

type MessageHandler func(*Message, *P2PConnection) error

type PingData struct {
	Timestamp time.Time `json:"timestamp"`
}

type PongData struct {
	OriginalTimestamp time.Time `json:"original_timestamp"`
	Timestamp         time.Time `json:"timestamp"`
}

type LatencyData struct {
	Measurements map[string]time.Duration `json:"measurements"`
	NodeID       string                   `json:"node_id"`
}

type RouteUpdateData struct {
	Routes map[string]RouteInfo `json:"routes"`
	NodeID string               `json:"node_id"`
}

type RouteInfo struct {
	Destination string        `json:"destination"`
	NextHop     string        `json:"next_hop"`
	Latency     time.Duration `json:"latency"`
	HopCount    int           `json:"hop_count"`
}

func NewP2PProtocol(localNode *Node, nodeManager *NodeManager) *P2PProtocol {
	p2p := &P2PProtocol{
		localNode:       localNode,
		nodeManager:     nodeManager,
		connections:     make(map[string]*P2PConnection),
		stopCh:          make(chan struct{}),
		messageHandlers: make(map[MessageType]MessageHandler),
	}

	// Register default message handlers
	p2p.RegisterHandler(MessageTypePing, p2p.handlePing)
	p2p.RegisterHandler(MessageTypePong, p2p.handlePong)
	p2p.RegisterHandler(MessageTypeLatency, p2p.handleLatency)
	p2p.RegisterHandler(MessageTypeRouteUpdate, p2p.handleRouteUpdate)

	return p2p
}

func (p *P2PProtocol) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isRunning {
		return fmt.Errorf("P2P protocol is already running")
	}

	// Start listening for incoming connections
	addr := fmt.Sprintf(":%d", p.localNode.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to start listener: %w", err)
	}

	p.listener = listener
	p.isRunning = true
	p.stopCh = make(chan struct{})

	go p.acceptLoop(ctx)
	go p.maintenanceLoop(ctx)

	return nil
}

func (p *P2PProtocol) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.isRunning {
		return nil
	}

	close(p.stopCh)

	if p.listener != nil {
		p.listener.Close()
	}

	// Close all connections
	for _, conn := range p.connections {
		conn.Conn.Close()
	}

	p.connections = make(map[string]*P2PConnection)
	p.isRunning = false

	return nil
}

func (p *P2PProtocol) ConnectToPeer(node *Node) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.connections[node.ID]; exists {
		return nil // Already connected
	}

	addr := fmt.Sprintf("%s:%d", node.PublicIP.String(), node.Port)
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to peer %s: %w", node.ID, err)
	}

	p2pConn := &P2PConnection{
		RemoteNode: node,
		Conn:       conn,
		LastPing:   time.Now(),
		IsActive:   true,
		SendCh:     make(chan *Message, 100),
		RecvCh:     make(chan *Message, 100),
	}

	p.connections[node.ID] = p2pConn

	go p.handleConnection(p2pConn)

	return nil
}

func (p *P2PProtocol) SendMessage(nodeID string, msgType MessageType, data interface{}) error {
	p.mu.RLock()
	conn, exists := p.connections[nodeID]
	p.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no connection to node %s", nodeID)
	}

	msg := &Message{
		Type:      msgType,
		From:      p.localNode.ID,
		To:        nodeID,
		Timestamp: time.Now(),
		Data:      data,
		ID:        generateMessageID(),
	}

	select {
	case conn.SendCh <- msg:
		return nil
	default:
		return fmt.Errorf("send channel full for node %s", nodeID)
	}
}

func (p *P2PProtocol) BroadcastMessage(msgType MessageType, data interface{}) {
	p.mu.RLock()
	connections := make([]*P2PConnection, 0, len(p.connections))
	for _, conn := range p.connections {
		if conn.IsActive {
			connections = append(connections, conn)
		}
	}
	p.mu.RUnlock()

	for _, conn := range connections {
		p.SendMessage(conn.RemoteNode.ID, msgType, data)
	}
}

func (p *P2PProtocol) RegisterHandler(msgType MessageType, handler MessageHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messageHandlers[msgType] = handler
}

func (p *P2PProtocol) acceptLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopCh:
			return
		default:
		}

		if tcpListener, ok := p.listener.(*net.TCPListener); ok {
			tcpListener.SetDeadline(time.Now().Add(1 * time.Second))
		}
		conn, err := p.listener.Accept()
		if err != nil {
			continue
		}

		go p.handleIncomingConnection(conn)
	}
}

func (p *P2PProtocol) handleIncomingConnection(conn net.Conn) {
	// Simple handshake: expect first message to identify the peer
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	
	decoder := json.NewDecoder(conn)
	var msg Message
	if err := decoder.Decode(&msg); err != nil {
		conn.Close()
		return
	}

	// Find the node
	node, exists := p.nodeManager.GetPeer(msg.From)
	if !exists {
		conn.Close()
		return
	}

	p2pConn := &P2PConnection{
		RemoteNode: node,
		Conn:       conn,
		LastPing:   time.Now(),
		IsActive:   true,
		SendCh:     make(chan *Message, 100),
		RecvCh:     make(chan *Message, 100),
	}

	p.mu.Lock()
	p.connections[node.ID] = p2pConn
	p.mu.Unlock()

	// Handle the first message
	if handler, exists := p.messageHandlers[msg.Type]; exists {
		handler(&msg, p2pConn)
	}

	p.handleConnection(p2pConn)
}

func (p *P2PProtocol) handleConnection(conn *P2PConnection) {
	defer func() {
		conn.Conn.Close()
		conn.IsActive = false
		
		p.mu.Lock()
		delete(p.connections, conn.RemoteNode.ID)
		p.mu.Unlock()
	}()

	// Start sender goroutine
	go p.connectionSender(conn)

	// Receiver loop
	decoder := json.NewDecoder(conn.Conn)
	for {
		var msg Message
		conn.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		if err := decoder.Decode(&msg); err != nil {
			break
		}

		// Handle message
		if handler, exists := p.messageHandlers[msg.Type]; exists {
			go handler(&msg, conn)
		}
	}
}

func (p *P2PProtocol) connectionSender(conn *P2PConnection) {
	encoder := json.NewEncoder(conn.Conn)
	
	for {
		select {
		case msg := <-conn.SendCh:
			conn.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := encoder.Encode(msg); err != nil {
				return
			}
		case <-p.stopCh:
			return
		}
	}
}

func (p *P2PProtocol) maintenanceLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.sendHeartbeats()
			p.cleanupStaleConnections()
		}
	}
}

func (p *P2PProtocol) sendHeartbeats() {
	pingData := PingData{Timestamp: time.Now()}
	p.BroadcastMessage(MessageTypePing, pingData)
}

func (p *P2PProtocol) cleanupStaleConnections() {
	p.mu.Lock()
	defer p.mu.Unlock()

	timeout := 2 * time.Minute
	now := time.Now()

	for nodeID, conn := range p.connections {
		if now.Sub(conn.LastPing) > timeout {
			conn.Conn.Close()
			conn.IsActive = false
			delete(p.connections, nodeID)
		}
	}
}

// Message handlers
func (p *P2PProtocol) handlePing(msg *Message, conn *P2PConnection) error {
	pingData := msg.Data.(map[string]interface{})
	originalTime, _ := time.Parse(time.RFC3339Nano, pingData["timestamp"].(string))

	pongData := PongData{
		OriginalTimestamp: originalTime,
		Timestamp:         time.Now(),
	}

	return p.SendMessage(msg.From, MessageTypePong, pongData)
}

func (p *P2PProtocol) handlePong(msg *Message, conn *P2PConnection) error {
	conn.LastPing = time.Now()
	return nil
}

func (p *P2PProtocol) handleLatency(msg *Message, conn *P2PConnection) error {
	// Process latency measurements from peer
	return nil
}

func (p *P2PProtocol) handleRouteUpdate(msg *Message, conn *P2PConnection) error {
	// Process route updates from peer
	return nil
}

func generateMessageID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}