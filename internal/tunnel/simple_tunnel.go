package tunnel

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/chacha20poly1305"
)

// RouteResolver interface for routing queries
type RouteResolver interface {
	GetBestNextHop(destination string) (string, error)
}

// SimpleTunnel implements a basic encrypted UDP tunnel without WireGuard dependency
type SimpleTunnel struct {
	localIP        net.IP
	listenPort     int
	conn           *net.UDPConn
	peers          map[string]*TunnelPeer
	mu             sync.RWMutex
	cipher         []byte // 32-byte key for ChaCha20Poly1305
	onDataReceived func([]byte) // Callback for received data
	routeResolver    RouteResolver // For routing optimization
	latencyCallback  func(string, map[string]time.Duration) // Callback for received latency info
}

type TunnelPeer struct {
	ID       string
	Endpoint *net.UDPAddr
	VirtualIP net.IP
	SharedKey []byte
	LastSeen  time.Time
	IsActive  bool
}

type TunnelPacket struct {
	Type      uint8  // 1=handshake, 2=data, 3=keepalive, 4=routed_data, 5=latency_info
	Nonce     [12]byte
	Payload   []byte
}

type RoutedPacket struct {
	FinalDestination net.IP // 最终目标IP
	OriginalData    []byte  // 原始数据包
}

type LatencyInfo struct {
	SourceNodeID string                        // 发送此信息的节点ID
	Measurements map[string]time.Duration      // 延迟测量表: 目标IP -> 延迟
	Timestamp    int64                         // 时间戳
}

type HandshakePacket struct {
	NodeID    string
	VirtualIP string
	Timestamp int64
	Signature []byte
}

func NewSimpleTunnel(localIP net.IP, listenPort int) (*SimpleTunnel, error) {
	// Generate random encryption key
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to generate encryption key: %w", err)
	}

	return &SimpleTunnel{
		localIP:    localIP,
		listenPort: listenPort,
		peers:      make(map[string]*TunnelPeer),
		cipher:     key,
	}, nil
}

func (st *SimpleTunnel) SetDataReceivedCallback(callback func([]byte)) {
	st.onDataReceived = callback
}

func (st *SimpleTunnel) SetRouteResolver(resolver RouteResolver) {
	st.routeResolver = resolver
}

func (st *SimpleTunnel) SetLatencyCallback(callback func(string, map[string]time.Duration)) {
	st.latencyCallback = callback
}

func (st *SimpleTunnel) BroadcastLatencyInfo(sourceNodeID string, measurements map[string]time.Duration) {
	latencyInfo := LatencyInfo{
		SourceNodeID: sourceNodeID,
		Measurements: measurements,
		Timestamp:    time.Now().Unix(),
	}
	
	serialized, err := st.serializeLatencyInfo(&latencyInfo)
	if err != nil {
		fmt.Printf("SimpleTunnel: Failed to serialize latency info: %v\n", err)
		return
	}
	
	// Broadcast to all active peers
	st.mu.RLock()
	for _, peer := range st.peers {
		if peer.IsActive {
			st.sendEncryptedPacket(peer, 5, serialized)
		}
	}
	st.mu.RUnlock()
	
	fmt.Printf("SimpleTunnel: Broadcasted latency info from %s to %d peers\n", sourceNodeID, len(st.peers))
}

func (st *SimpleTunnel) Start() error {
	// Use IPv4 socket for maximum compatibility
	// Most systems support IPv4, and we can always add IPv6 support later
	addr := &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: st.listenPort,
	}
	
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on UDP port %d: %w", st.listenPort, err)
	}
	
	fmt.Printf("SimpleTunnel: Listening on IPv4: %s\n", addr.String())
	st.conn = conn

	go st.handleIncomingPackets()
	go st.keepaliveLoop()

	return nil
}

func (st *SimpleTunnel) Stop() error {
	if st.conn != nil {
		return st.conn.Close()
	}
	return nil
}

func (st *SimpleTunnel) AddPeer(nodeID string, endpoint string, virtualIP net.IP) error {
	fmt.Printf("SimpleTunnel: Adding peer %s at %s with virtual IP %s\n", nodeID, endpoint, virtualIP.String())
	
	udpAddr, err := net.ResolveUDPAddr("udp4", endpoint)
	if err != nil {
		return fmt.Errorf("invalid endpoint %s: %w", endpoint, err)
	}

	// Generate shared key based on node IDs (simplified)
	sharedKey := st.generateSharedKey(nodeID)

	peer := &TunnelPeer{
		ID:        nodeID,
		Endpoint:  udpAddr,
		VirtualIP: virtualIP,
		SharedKey: sharedKey,
		LastSeen:  time.Now(),
		IsActive:  false,
	}

	st.mu.Lock()
	st.peers[nodeID] = peer
	st.mu.Unlock()

	fmt.Printf("SimpleTunnel: Sending handshake to peer %s\n", nodeID)
	// Send handshake
	return st.sendHandshake(peer)
}

func (st *SimpleTunnel) RemovePeer(nodeID string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	
	delete(st.peers, nodeID)
	return nil
}

func (st *SimpleTunnel) SendData(dstIP net.IP, data []byte) error {
	dstStr := dstIP.String()
	
	// Try to get optimized route first
	if st.routeResolver != nil {
		nextHopIP, err := st.routeResolver.GetBestNextHop(dstStr)
		if err == nil {
			if nextHopIP != "" && nextHopIP != dstStr {
				// Multi-hop routing: send via next hop
				fmt.Printf("SimpleTunnel: Using multi-hop route to %s via %s\n", dstStr, nextHopIP)
				
				// Find next hop peer
				st.mu.RLock()
				var nextHopPeer *TunnelPeer
				for _, peer := range st.peers {
					if peer.VirtualIP.String() == nextHopIP && peer.IsActive {
						nextHopPeer = peer
						break
					}
				}
				st.mu.RUnlock()
				
				if nextHopPeer != nil {
					return st.sendRoutedData(nextHopPeer, dstIP, data)
				} else {
					fmt.Printf("SimpleTunnel: Next hop peer %s not found, falling back to direct\n", nextHopIP)
				}
			} else {
				// nextHopIP == "" means direct connection is optimal
				fmt.Printf("SimpleTunnel: Using optimized direct route to %s\n", dstStr)
			}
		} else {
			fmt.Printf("SimpleTunnel: Route resolution failed for %s: %v, using direct\n", dstStr, err)
		}
	}
	
	// Direct connection to destination
	fmt.Printf("SimpleTunnel: Sending directly to %s\n", dstStr)
	st.mu.RLock()
	var targetPeer *TunnelPeer
	for _, peer := range st.peers {
		if peer.VirtualIP.Equal(dstIP) && peer.IsActive {
			targetPeer = peer
			break
		}
	}
	st.mu.RUnlock()

	if targetPeer == nil {
		return fmt.Errorf("no active peer found for IP %s", dstIP)
	}

	return st.sendEncryptedData(targetPeer, data)
}

func (st *SimpleTunnel) handleIncomingPackets() {
	buffer := make([]byte, 1500)

	for {
		n, addr, err := st.conn.ReadFromUDP(buffer)
		if err != nil {
			continue
		}

		packet := buffer[:n]
		if len(packet) < 13 { // Minimum packet size
			continue
		}

		packetType := packet[0]
		nonce := packet[1:13]
		payload := packet[13:]

		switch packetType {
		case 1: // Handshake
			st.handleHandshake(addr, nonce, payload)
		case 2: // Data
			st.handleDataPacket(addr, nonce, payload)
		case 3: // Keepalive
			st.handleKeepalive(addr)
		case 4: // Routed data
			st.handleRoutedPacket(addr, nonce, payload)
		case 5: // Latency info
			st.handleLatencyInfo(addr, nonce, payload)
		}
	}
}

func (st *SimpleTunnel) handleHandshake(addr *net.UDPAddr, nonce []byte, payload []byte) {
	fmt.Printf("SimpleTunnel: Received handshake from %s\n", addr.String())
	
	// Find peer by endpoint
	st.mu.RLock()
	var peer *TunnelPeer
	for _, p := range st.peers {
		if p.Endpoint.String() == addr.String() {
			peer = p
			break
		}
	}
	st.mu.RUnlock()

	if peer == nil {
		fmt.Printf("SimpleTunnel: Unknown peer %s, ignoring handshake\n", addr.String())
		return // Unknown peer
	}

	// Decrypt handshake payload
	decrypted, err := st.decrypt(peer.SharedKey, nonce, payload)
	if err != nil {
		fmt.Printf("SimpleTunnel: Failed to decrypt handshake from %s: %v\n", addr.String(), err)
		return
	}

	// Parse handshake
	var handshake HandshakePacket
	if err := st.parseHandshake(decrypted, &handshake); err != nil {
		fmt.Printf("SimpleTunnel: Failed to parse handshake from %s: %v\n", addr.String(), err)
		return
	}

	// Verify handshake - for now, accept any handshake from known peers
	fmt.Printf("SimpleTunnel: Activating peer %s (from handshake: %s)\n", peer.ID, handshake.NodeID)
	st.mu.Lock()
	wasActive := peer.IsActive
	peer.IsActive = true
	peer.LastSeen = time.Now()
	st.mu.Unlock()

	// Only send handshake response if peer wasn't already active
	// This prevents handshake loops
	if !wasActive {
		fmt.Printf("SimpleTunnel: Sending handshake response to newly activated peer %s\n", peer.ID)
		st.sendHandshake(peer)
	}
}

func (st *SimpleTunnel) handleDataPacket(addr *net.UDPAddr, nonce []byte, payload []byte) {
	// Find active peer
	st.mu.RLock()
	var peer *TunnelPeer
	for _, p := range st.peers {
		if p.Endpoint.String() == addr.String() && p.IsActive {
			peer = p
			break
		}
	}
	st.mu.RUnlock()

	if peer == nil {
		return
	}

	// Decrypt data
	decrypted, err := st.decrypt(peer.SharedKey, nonce, payload)
	if err != nil {
		return
	}

	// Update last seen
	st.mu.Lock()
	peer.LastSeen = time.Now()
	st.mu.Unlock()

	// Forward decrypted data to TUN interface
	fmt.Printf("SimpleTunnel: Received %d bytes of data from peer %s\n", len(decrypted), peer.ID)
	
	// We need a way to forward this to the TUN interface
	// For now, we'll need to implement a callback mechanism
	if st.onDataReceived != nil {
		st.onDataReceived(decrypted)
	}
}

func (st *SimpleTunnel) handleKeepalive(addr *net.UDPAddr) {
	st.mu.Lock()
	for _, peer := range st.peers {
		if peer.Endpoint.String() == addr.String() {
			peer.LastSeen = time.Now()
			break
		}
	}
	st.mu.Unlock()
}

func (st *SimpleTunnel) handleRoutedPacket(addr *net.UDPAddr, nonce []byte, payload []byte) {
	// Find peer who sent this
	st.mu.RLock()
	var peer *TunnelPeer
	for _, p := range st.peers {
		if p.Endpoint.String() == addr.String() && p.IsActive {
			peer = p
			break
		}
	}
	st.mu.RUnlock()

	if peer == nil {
		return
	}

	// Decrypt routed packet
	decrypted, err := st.decrypt(peer.SharedKey, nonce, payload)
	if err != nil {
		fmt.Printf("SimpleTunnel: Failed to decrypt routed packet: %v\n", err)
		return
	}

	// Parse routed packet
	var routedPacket RoutedPacket
	if err := st.parseRoutedPacket(decrypted, &routedPacket); err != nil {
		fmt.Printf("SimpleTunnel: Failed to parse routed packet: %v\n", err)
		return
	}

	// Update last seen
	st.mu.Lock()
	peer.LastSeen = time.Now()
	st.mu.Unlock()

	// Check if we are the final destination
	localIP := st.localIP
	if routedPacket.FinalDestination.Equal(localIP) {
		// This packet is for us - forward to TUN interface
		fmt.Printf("SimpleTunnel: Received routed packet for local interface\n")
		if st.onDataReceived != nil {
			st.onDataReceived(routedPacket.OriginalData)
		}
	} else {
		// Forward to next hop
		fmt.Printf("SimpleTunnel: Forwarding routed packet to %s\n", routedPacket.FinalDestination.String())
		err := st.SendData(routedPacket.FinalDestination, routedPacket.OriginalData)
		if err != nil {
			fmt.Printf("SimpleTunnel: Failed to forward routed packet: %v\n", err)
		}
	}
}

func (st *SimpleTunnel) handleLatencyInfo(addr *net.UDPAddr, nonce []byte, payload []byte) {
	// Find peer who sent this
	st.mu.RLock()
	var peer *TunnelPeer
	for _, p := range st.peers {
		if p.Endpoint.String() == addr.String() && p.IsActive {
			peer = p
			break
		}
	}
	st.mu.RUnlock()

	if peer == nil {
		return
	}

	// Decrypt latency info
	decrypted, err := st.decrypt(peer.SharedKey, nonce, payload)
	if err != nil {
		fmt.Printf("SimpleTunnel: Failed to decrypt latency info: %v\n", err)
		return
	}

	// Parse latency info
	var latencyInfo LatencyInfo
	if err := st.parseLatencyInfo(decrypted, &latencyInfo); err != nil {
		fmt.Printf("SimpleTunnel: Failed to parse latency info: %v\n", err)
		return
	}

	// Update last seen
	st.mu.Lock()
	peer.LastSeen = time.Now()
	st.mu.Unlock()

	fmt.Printf("SimpleTunnel: Received latency info from %s (%d measurements)\n", 
		latencyInfo.SourceNodeID, len(latencyInfo.Measurements))

	// Forward to routing optimizer
	if st.latencyCallback != nil {
		st.latencyCallback(latencyInfo.SourceNodeID, latencyInfo.Measurements)
	}
}

func (st *SimpleTunnel) sendHandshake(peer *TunnelPeer) error {
	localNodeID := fmt.Sprintf("local-%s", st.localIP.String())
	handshake := HandshakePacket{
		NodeID:    localNodeID,
		VirtualIP: st.localIP.String(),
		Timestamp: time.Now().Unix(),
	}

	data, err := st.serializeHandshake(&handshake)
	if err != nil {
		return err
	}

	return st.sendEncryptedPacket(peer, 1, data)
}

func (st *SimpleTunnel) sendEncryptedData(peer *TunnelPeer, data []byte) error {
	return st.sendEncryptedPacket(peer, 2, data)
}

func (st *SimpleTunnel) sendRoutedData(nextHopPeer *TunnelPeer, finalDest net.IP, data []byte) error {
	// Create routed packet
	routedPacket := RoutedPacket{
		FinalDestination: finalDest,
		OriginalData:    data,
	}
	
	// Serialize routed packet
	serialized, err := st.serializeRoutedPacket(&routedPacket)
	if err != nil {
		return err
	}
	
	return st.sendEncryptedPacket(nextHopPeer, 4, serialized)
}

func (st *SimpleTunnel) sendEncryptedPacket(peer *TunnelPeer, packetType uint8, data []byte) error {
	// Generate random nonce
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}

	// Encrypt payload
	encrypted, err := st.encrypt(peer.SharedKey, nonce, data)
	if err != nil {
		return err
	}

	// Build packet
	packet := make([]byte, 1+12+len(encrypted))
	packet[0] = packetType
	copy(packet[1:13], nonce)
	copy(packet[13:], encrypted)

	// Send packet
	_, err = st.conn.WriteToUDP(packet, peer.Endpoint)
	return err
}

func (st *SimpleTunnel) encrypt(key, nonce, plaintext []byte) ([]byte, error) {
	cipher, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}

	return cipher.Seal(nil, nonce, plaintext, nil), nil
}

func (st *SimpleTunnel) decrypt(key, nonce, ciphertext []byte) ([]byte, error) {
	cipher, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}

	return cipher.Open(nil, nonce, ciphertext, nil)
}

func (st *SimpleTunnel) generateSharedKey(nodeID string) []byte {
	// Use a simple network-wide shared secret based on network name
	// This ensures all nodes in the same network can communicate
	networkSecret := "lorbol-default-network-key-v1"
	
	fmt.Printf("SimpleTunnel: Generating shared key for peer %s using network secret\n", nodeID)
	
	hash := sha256.Sum256([]byte(networkSecret))
	return hash[:]
}

func (st *SimpleTunnel) serializeHandshake(hs *HandshakePacket) ([]byte, error) {
	// Simple binary serialization
	data := make([]byte, 0, 1024)
	data = append(data, []byte(hs.NodeID)...)
	data = append(data, 0) // Null separator
	data = append(data, []byte(hs.VirtualIP)...)
	data = append(data, 0) // Null separator
	
	timestamp := make([]byte, 8)
	binary.BigEndian.PutUint64(timestamp, uint64(hs.Timestamp))
	data = append(data, timestamp...)
	
	return data, nil
}

func (st *SimpleTunnel) parseHandshake(data []byte, hs *HandshakePacket) error {
	// Simple binary deserialization
	parts := make([][]byte, 0, 3)
	start := 0
	
	for i, b := range data {
		if b == 0 && len(parts) < 2 {
			parts = append(parts, data[start:i])
			start = i + 1
		}
	}
	
	if len(parts) < 2 || len(data) < start+8 {
		return fmt.Errorf("invalid handshake data")
	}
	
	hs.NodeID = string(parts[0])
	hs.VirtualIP = string(parts[1])
	hs.Timestamp = int64(binary.BigEndian.Uint64(data[start:start+8]))
	
	return nil
}

func (st *SimpleTunnel) serializeRoutedPacket(rp *RoutedPacket) ([]byte, error) {
	// Simple binary serialization: 16 bytes for IP + length(4) + data
	data := make([]byte, 0, 20+len(rp.OriginalData))
	
	// Add destination IP (16 bytes for IPv6 compatibility)
	ipBytes := rp.FinalDestination.To16()
	data = append(data, ipBytes...)
	
	// Add data length (4 bytes)
	dataLen := make([]byte, 4)
	binary.BigEndian.PutUint32(dataLen, uint32(len(rp.OriginalData)))
	data = append(data, dataLen...)
	
	// Add original data
	data = append(data, rp.OriginalData...)
	
	return data, nil
}

func (st *SimpleTunnel) parseRoutedPacket(data []byte, rp *RoutedPacket) error {
	if len(data) < 20 {
		return fmt.Errorf("routed packet too short")
	}
	
	// Parse destination IP (16 bytes)
	rp.FinalDestination = net.IP(data[0:16])
	
	// Parse data length (4 bytes)
	dataLen := binary.BigEndian.Uint32(data[16:20])
	
	// Check if we have enough data
	if len(data) < 20+int(dataLen) {
		return fmt.Errorf("routed packet data truncated")
	}
	
	// Parse original data
	rp.OriginalData = make([]byte, dataLen)
	copy(rp.OriginalData, data[20:20+dataLen])
	
	return nil
}

func (st *SimpleTunnel) keepaliveLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		st.mu.RLock()
		for _, peer := range st.peers {
			if peer.IsActive {
				// Send keepalive
				st.sendEncryptedPacket(peer, 3, []byte{})
			}
		}
		st.mu.RUnlock()

		// Remove inactive peers
		st.cleanupInactivePeers()
	}
}

func (st *SimpleTunnel) cleanupInactivePeers() {
	timeout := 2 * time.Minute
	now := time.Now()

	st.mu.Lock()
	for _, peer := range st.peers {
		if now.Sub(peer.LastSeen) > timeout {
			peer.IsActive = false
			// Could remove completely if needed
		}
	}
	st.mu.Unlock()
}

func (st *SimpleTunnel) GetActivePeers() []*TunnelPeer {
	st.mu.RLock()
	defer st.mu.RUnlock()

	var activePeers []*TunnelPeer
	for _, peer := range st.peers {
		if peer.IsActive {
			activePeers = append(activePeers, peer)
		}
	}

	return activePeers
}

func (st *SimpleTunnel) serializeLatencyInfo(li *LatencyInfo) ([]byte, error) {
	// Simple JSON serialization for latency info
	jsonData, err := json.Marshal(li)
	if err != nil {
		return nil, err
	}
	return jsonData, nil
}

func (st *SimpleTunnel) parseLatencyInfo(data []byte, li *LatencyInfo) error {
	// Simple JSON deserialization
	return json.Unmarshal(data, li)
}