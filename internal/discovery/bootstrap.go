package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// BootstrapService handles initial node discovery in public networks
type BootstrapService struct {
	localNode     *Node
	knownPeers    map[string]*PeerInfo
	bootstrapURLs []string
	httpClient    *http.Client
	mu            sync.RWMutex
	stopCh        chan struct{}
	isRunning     bool
}

type Node struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	VirtualIP string `json:"virtual_ip"`
	PublicIPv4 string `json:"public_ipv4,omitempty"`
	PublicIPv6 string `json:"public_ipv6,omitempty"`
	PublicIP  string `json:"public_ip"` // 保留兼容性，自动选择最佳IP
	Port      int    `json:"port"`
	Network   string `json:"network"`
	Timestamp int64  `json:"timestamp"`
}

type PeerInfo struct {
	Node       *Node
	LastSeen   time.Time
	Reachable  bool
	Endpoint   string
}

type BootstrapRequest struct {
	Action string `json:"action"` // "register", "discover", "heartbeat"
	Node   *Node  `json:"node"`
}

type BootstrapResponse struct {
	Success bool    `json:"success"`
	Message string  `json:"message"`
	Peers   []*Node `json:"peers,omitempty"`
}

// Default bootstrap servers (can be public HTTP endpoints)
var DefaultBootstrapURLs = []string{
	"https://api.github.com/repos/yourusername/lorbol-bootstrap/contents/nodes.json", // Use GitHub as free bootstrap
	// Can add more bootstrap servers
}

func NewBootstrapService(localNode *Node, bootstrapURLs []string) *BootstrapService {
	if len(bootstrapURLs) == 0 {
		bootstrapURLs = DefaultBootstrapURLs
	}

	return &BootstrapService{
		localNode:     localNode,
		knownPeers:    make(map[string]*PeerInfo),
		bootstrapURLs: bootstrapURLs,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		stopCh: make(chan struct{}),
	}
}

func (bs *BootstrapService) Start(ctx context.Context) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	if bs.isRunning {
		return fmt.Errorf("bootstrap service is already running")
	}

	bs.isRunning = true
	bs.stopCh = make(chan struct{})

	// Perform initial discovery
	go bs.discoveryLoop(ctx)

	return nil
}

func (bs *BootstrapService) Stop() error {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	if !bs.isRunning {
		return nil
	}

	close(bs.stopCh)
	bs.isRunning = false

	return nil
}

func (bs *BootstrapService) discoveryLoop(ctx context.Context) {
	// Initial discovery
	bs.performDiscovery()

	ticker := time.NewTicker(60 * time.Second) // Discovery every minute
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-bs.stopCh:
			return
		case <-ticker.C:
			bs.performDiscovery()
		}
	}
}

func (bs *BootstrapService) performDiscovery() {
	// Only use configured discovery methods
	if len(bs.bootstrapURLs) > 0 {
		bs.discoverViaBootstrapServers()
	}
	
	// Always test reachability of known peers
	bs.discoverViaDirectConnect()
}

func (bs *BootstrapService) discoverViaBootstrapServers() {
	for _, url := range bs.bootstrapURLs {
		peers, err := bs.queryBootstrapServer(url)
		if err != nil {
			continue // Try next server
		}

		bs.mu.Lock()
		for _, peer := range peers {
			if peer.ID != bs.localNode.ID && peer.Network == bs.localNode.Network {
				bs.knownPeers[peer.ID] = &PeerInfo{
					Node:     peer,
					LastSeen: time.Now(),
					Endpoint: fmt.Sprintf("%s:%d", peer.PublicIP, peer.Port),
				}
			}
		}
		bs.mu.Unlock()
		break // Success, no need to try other servers
	}
}

func (bs *BootstrapService) queryBootstrapServer(url string) ([]*Node, error) {
	// Try HTTP discovery server
	if strings.Contains(url, "/discover") {
		return bs.queryHTTPDiscoveryServer(url)
	}
	
	// Fallback to generic bootstrap request
	req := &BootstrapRequest{
		Action: "discover",
		Node:   bs.localNode,
	}

	reqBody, _ := json.Marshal(req)
	
	// This would be a real HTTP request to bootstrap server
	_ = reqBody
	
	// For now, return empty list
	return []*Node{}, nil
}

func (bs *BootstrapService) queryHTTPDiscoveryServer(baseURL string) ([]*Node, error) {
	// Register ourselves first
	if err := bs.registerToHTTPServer(baseURL); err != nil {
		fmt.Printf("HTTP Discovery: Failed to register: %v\n", err)
	}

	// Query for other nodes
	discoverURL := fmt.Sprintf("%s?network=%s", baseURL, bs.localNode.Network)
	
	resp, err := bs.httpClient.Get(discoverURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var result struct {
		Nodes []*Node `json:"nodes"`
		Count int     `json:"count"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	fmt.Printf("HTTP Discovery: Found %d nodes in network %s\n", result.Count, bs.localNode.Network)
	return result.Nodes, nil
}

func (bs *BootstrapService) registerToHTTPServer(baseURL string) error {
	registerURL := strings.Replace(baseURL, "/discover", "/register", 1)
	
	reqBody, err := json.Marshal(bs.localNode)
	if err != nil {
		return err
	}

	resp, err := bs.httpClient.Post(registerURL, "application/json", strings.NewReader(string(reqBody)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	fmt.Printf("HTTP Discovery: Registered node %s to %s\n", bs.localNode.Name, registerURL)
	return nil
}

func (bs *BootstrapService) discoverViaDirectConnect() {
	// Try to connect directly to known peers to check reachability
	bs.mu.RLock()
	peers := make([]*PeerInfo, 0, len(bs.knownPeers))
	for _, peer := range bs.knownPeers {
		peers = append(peers, peer)
	}
	bs.mu.RUnlock()

	for _, peer := range peers {
		reachable := bs.testPeerReachability(peer.Endpoint)
		
		bs.mu.Lock()
		if peerInfo, exists := bs.knownPeers[peer.Node.ID]; exists {
			peerInfo.Reachable = reachable
			if reachable {
				peerInfo.LastSeen = time.Now()
			}
		}
		bs.mu.Unlock()
	}
}

func (bs *BootstrapService) testPeerReachability(endpoint string) bool {
	// Simple UDP ping test
	// In real implementation, this would be more sophisticated
	return true // Placeholder
}

func (bs *BootstrapService) GetKnownPeers() []*Node {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	var peers []*Node
	for _, peerInfo := range bs.knownPeers {
		if peerInfo.Reachable {
			peers = append(peers, peerInfo.Node)
		}
	}

	return peers
}

func (bs *BootstrapService) AddKnownPeer(node *Node) {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	bs.knownPeers[node.ID] = &PeerInfo{
		Node:      node,
		LastSeen:  time.Now(),
		Reachable: true, // Assume static peers are reachable initially
		Endpoint:  fmt.Sprintf("%s:%d", node.PublicIP, node.Port),
	}
}

// GitHub-based discovery implementation
func (bs *BootstrapService) discoverViaGitHub() {
	// First register ourselves
	bs.registerToGitHub()
	
	// Then discover other nodes
	bs.queryGitHubNodes()
}

func (bs *BootstrapService) registerToGitHub() error {
	// TODO: Implement GitHub-based node registration
	// This would use GitHub API to create/update a file with node info
	return nil
}

func (bs *BootstrapService) queryGitHubNodes() []*Node {
	// TODO: Implement GitHub-based node discovery  
	// This would query a GitHub repository for nodes.json file
	return []*Node{}
}

// DNS-based discovery
func (bs *BootstrapService) discoverViaDNS() []*Node {
	// This would implement DNS TXT record discovery
	// e.g., _lorbol._udp.example.com TXT "node=id:abc,ip:1.2.3.4,port:51820"
	return []*Node{}
}