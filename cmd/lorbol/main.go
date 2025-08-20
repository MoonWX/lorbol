package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/MoonWX/lorbol/internal/config"
	"github.com/MoonWX/lorbol/internal/discovery"
	"github.com/MoonWX/lorbol/internal/latency"
	"github.com/MoonWX/lorbol/internal/routing"
	"github.com/MoonWX/lorbol/internal/tun"
	"github.com/MoonWX/lorbol/internal/tunnel"
)

func main() {
	var configPath = flag.String("config", "config.yaml", "path to configuration file")
	flag.Parse()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	server, err := NewServer(cfg)
	if err != nil {
		log.Fatalf("failed to create server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := server.Start(ctx); err != nil {
			log.Printf("server error: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("shutting down...")
	if err := server.Stop(); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}

type Server struct {
	config      *config.Config
	tunInterface *tun.Interface
	tunnel      *tunnel.SimpleTunnel
	discovery   *discovery.BootstrapService
	measurer    *latency.UDPMeasurer
	optimizer   routing.Optimizer
	localNode   *discovery.Node
	nodeManager *SimpleNodeManager
}

// SimpleNodeManager implements routing.NodeManager interface
type SimpleNodeManager struct {
	localNode *discovery.Node
	peers     map[string]*discovery.Node
	mu        sync.RWMutex
}

func (nm *SimpleNodeManager) GetAllNodes() []*routing.NetworkNode {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	
	var nodes []*routing.NetworkNode
	
	// Add local node
	if nm.localNode != nil {
		nodes = append(nodes, &routing.NetworkNode{
			ID:        nm.localNode.ID,
			Name:      nm.localNode.Name,
			VirtualIP: net.ParseIP(nm.localNode.VirtualIP),
			PublicIP:  net.ParseIP(nm.localNode.PublicIP),
			Port:      nm.localNode.Port,
			IsOnline:  true,
		})
	}
	
	// Add peers
	for _, peer := range nm.peers {
		nodes = append(nodes, &routing.NetworkNode{
			ID:        peer.ID,
			Name:      peer.Name,
			VirtualIP: net.ParseIP(peer.VirtualIP),
			PublicIP:  net.ParseIP(peer.PublicIP),
			Port:      peer.Port,
			IsOnline:  true, // Assume discovered peers are online
		})
	}
	
	return nodes
}

func (nm *SimpleNodeManager) GetLocalNode() *routing.NetworkNode {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	
	if nm.localNode == nil {
		return nil
	}
	
	return &routing.NetworkNode{
		ID:        nm.localNode.ID,
		Name:      nm.localNode.Name,
		VirtualIP: net.ParseIP(nm.localNode.VirtualIP),
		PublicIP:  net.ParseIP(nm.localNode.PublicIP),
		Port:      nm.localNode.Port,
		IsOnline:  true,
	}
}

func (nm *SimpleNodeManager) GetOnlinePeers() []*routing.NetworkNode {
	return nm.GetAllNodes() // For simplicity, assume all discovered peers are online
}

func (nm *SimpleNodeManager) UpdatePeers(peers []*discovery.Node) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	
	// Clear existing peers
	nm.peers = make(map[string]*discovery.Node)
	
	// Add new peers
	for _, peer := range peers {
		if peer.ID != nm.localNode.ID { // Don't add ourselves
			nm.peers[peer.ID] = peer
		}
	}
}

func NewServer(cfg *config.Config) (*Server, error) {
	// Parse virtual IP
	virtualIP := net.ParseIP(cfg.Node.VirtualIP)
	if virtualIP == nil {
		return nil, fmt.Errorf("invalid virtual IP: %s", cfg.Node.VirtualIP)
	}

	// Create local node with stable ID based on name and virtual IP
	// This prevents duplicate registrations on restarts
	localNode := &discovery.Node{
		ID:        fmt.Sprintf("%s-%s", cfg.Node.Name, virtualIP.String()),
		Name:      cfg.Node.Name,
		VirtualIP: virtualIP.String(),
		PublicIP:  "", // Will be detected or set from config
		Port:      cfg.Node.ListenPort,
		Network:   cfg.Network.Name,
		Timestamp: time.Now().Unix(),
	}

	// Auto-detect public IPs if not specified
	if cfg.Node.PublicIP == "" {
		ipv4, ipv6, err := detectPublicIPs()
		if err != nil {
			log.Printf("Warning: failed to detect public IPs: %v", err)
		} else {
			localNode.PublicIPv4 = ipv4
			localNode.PublicIPv6 = ipv6
			// Set primary PublicIP to IPv4 if available, otherwise IPv6
			if ipv4 != "" {
				localNode.PublicIP = ipv4
			} else {
				localNode.PublicIP = ipv6
			}
			log.Printf("Detected public IPs - IPv4: %s, IPv6: %s", ipv4, ipv6)
		}
	} else {
		localNode.PublicIP = cfg.Node.PublicIP
		// Determine if configured IP is IPv4 or IPv6
		if parsedIP := net.ParseIP(cfg.Node.PublicIP); parsedIP != nil {
			if parsedIP.To4() != nil {
				localNode.PublicIPv4 = cfg.Node.PublicIP
			} else {
				localNode.PublicIPv6 = cfg.Node.PublicIP
			}
		}
	}

	// Create TUN interface
	tunIface, err := tun.NewInterface(cfg.TUN.InterfaceName, virtualIP, cfg.Network.CIDR, cfg.TUN.MTU)
	if err != nil {
		return nil, fmt.Errorf("failed to create TUN interface: %w", err)
	}

	// Create simple tunnel
	simpleTunnel, err := tunnel.NewSimpleTunnel(virtualIP, cfg.Node.ListenPort)
	if err != nil {
		return nil, fmt.Errorf("failed to create tunnel: %w", err)
	}
	
	// Set callback to forward tunnel data to TUN interface
	simpleTunnel.SetDataReceivedCallback(func(data []byte) {
		if tunIface != nil {
			log.Printf("Forwarding %d bytes from tunnel to TUN interface", len(data))
			if err := tunIface.WritePacket(data); err != nil {
				log.Printf("Failed to write packet to TUN: %v", err)
			}
		}
	})
	
	// Create routing optimizer with node manager
	nodeManager := &SimpleNodeManager{
		localNode: localNode,
		peers:     make(map[string]*discovery.Node),
	}
	optimizer := routing.NewNetworkOptimizer(cfg.Routing, nodeManager)
	
	// Set route resolver for tunnel
	simpleTunnel.SetRouteResolver(optimizer)
	
	// Set latency callback for distributed routing
	simpleTunnel.SetLatencyCallback(func(sourceNodeID string, measurements map[string]time.Duration) {
		optimizer.UpdateDistributedLatencies(sourceNodeID, measurements)
	})

	// Create bootstrap discovery service
	var bootstrapURLs []string
	discoveryService := discovery.NewBootstrapService(localNode, bootstrapURLs)
	
	// Handle discovery configuration based on method
	if cfg.Bootstrap.Method == "static" {
		if staticPeersInterface, exists := cfg.Bootstrap.Config["static_peers"]; exists {
			if staticPeers, ok := staticPeersInterface.([]interface{}); ok {
				for _, peerInterface := range staticPeers {
					if peerStr, ok := peerInterface.(string); ok {
						// Parse peer endpoint (e.g., "8.211.175.127:51820" or "[::1]:51820")
						var publicIP string
						var port int
						
						if strings.HasPrefix(peerStr, "[") {
							// IPv6 format: [::1]:51820
							if endBracket := strings.Index(peerStr, "]"); endBracket != -1 {
								publicIP = peerStr[1:endBracket]
								portPart := peerStr[endBracket+2:] // Skip ]:
								fmt.Sscanf(portPart, "%d", &port)
							}
						} else {
							// IPv4 format: 1.2.3.4:51820
							parts := strings.Split(peerStr, ":")
							if len(parts) == 2 {
								publicIP = parts[0]
								fmt.Sscanf(parts[1], "%d", &port)
							}
						}
						
						if publicIP != "" && port > 0 {
							// Create a static peer node
							staticPeer := &discovery.Node{
								ID:        fmt.Sprintf("static-%s", publicIP),
								Name:      fmt.Sprintf("static-peer-%s", publicIP),
								VirtualIP: "", // Will be discovered during handshake
								PublicIP:  publicIP,
								Port:      port,
								Network:   cfg.Network.Name,
								Timestamp: time.Now().Unix(),
							}
							discoveryService.AddKnownPeer(staticPeer)
							log.Printf("Added static peer: %s:%d", publicIP, port)
						}
					}
				}
			}
		}
	} else if cfg.Bootstrap.Method == "http" {
		// Handle HTTP discovery configuration
		if discoveryURLsInterface, exists := cfg.Bootstrap.Config["discovery_urls"]; exists {
			if discoveryURLs, ok := discoveryURLsInterface.([]interface{}); ok {
				for _, urlInterface := range discoveryURLs {
					if urlStr, ok := urlInterface.(string); ok {
						bootstrapURLs = append(bootstrapURLs, urlStr)
						log.Printf("Added HTTP discovery server: %s", urlStr)
					}
				}
			}
		}
		// Update the discovery service with HTTP URLs
		discoveryService = discovery.NewBootstrapService(localNode, bootstrapURLs)
	}

	// Create latency measurer
	measurer := latency.NewUDPMeasurer(cfg.Latency.Interval, cfg.Latency.Timeout)

	return &Server{
		config:       cfg,
		tunInterface: tunIface,
		tunnel:       simpleTunnel,
		discovery:    discoveryService,
		measurer:     measurer,
		optimizer:    optimizer,
		localNode:    localNode,
		nodeManager:  nodeManager,
	}, nil
}

func (s *Server) Start(ctx context.Context) error {
	log.Printf("starting LORBOL server for node %s...", s.localNode.Name)
	log.Printf("Virtual IP: %s", s.localNode.VirtualIP)
	log.Printf("Public IP: %s", s.localNode.PublicIP)
	log.Printf("Listen Port: %d", s.localNode.Port)

	// Start TUN interface
	if err := s.tunInterface.Start(); err != nil {
		return fmt.Errorf("failed to start TUN interface: %w", err)
	}

	// Start tunnel
	if err := s.tunnel.Start(); err != nil {
		return fmt.Errorf("failed to start tunnel: %w", err)
	}

	// Start discovery service
	if err := s.discovery.Start(ctx); err != nil {
		return fmt.Errorf("failed to start discovery service: %w", err)
	}

	// Start peer management loop
	go s.managePeers(ctx)

	// Start packet forwarding
	go s.forwardPackets(ctx)

	log.Printf("LORBOL server started successfully")
	return nil
}

func (s *Server) Stop() error {
	log.Println("stopping LORBOL server...")

	if err := s.discovery.Stop(); err != nil {
		log.Printf("error stopping discovery service: %v", err)
	}

	if err := s.tunnel.Stop(); err != nil {
		log.Printf("error stopping tunnel: %v", err)
	}

	if err := s.tunInterface.Stop(); err != nil {
		log.Printf("error stopping TUN interface: %v", err)
	}

	log.Println("LORBOL server stopped")
	return nil
}

func (s *Server) managePeers(ctx context.Context) {
	// Initial peer management
	s.processPeers()
	
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processPeers()
		}
	}
}

func (s *Server) processPeers() {
	// Get discovered peers
	peers := s.discovery.GetKnownPeers()
	log.Printf("Processing %d discovered peers", len(peers))
	
	// Update node manager with discovered peers
	s.nodeManager.UpdatePeers(peers)
	
	// Add new peers to tunnel
	for _, peer := range peers {
		log.Printf("Checking peer: %s (ID:%s, PublicIP:%s, VirtualIP:%s)", peer.Name, peer.ID, peer.PublicIP, peer.VirtualIP)
		
		if peer.ID != s.localNode.ID && (peer.PublicIPv4 != "" || peer.PublicIPv6 != "") {
			// Choose best endpoint based on availability and latency
			bestEndpoint := s.chooseBestEndpoint(peer)
			if bestEndpoint == "" {
				log.Printf("No reachable endpoint for peer %s", peer.Name)
				continue
			}
			
			// Parse peer's virtual IP
			var virtualIP net.IP
			if peer.VirtualIP != "" {
				virtualIP = net.ParseIP(peer.VirtualIP)
			} else {
				log.Printf("Warning: Peer %s has no virtual IP, skipping", peer.Name)
				continue
			}
			
			if virtualIP != nil {
				// Check if peer is already added and active
				activePeers := s.tunnel.GetActivePeers()
				peerExists := false
				for _, activePeer := range activePeers {
					if activePeer.VirtualIP.Equal(virtualIP) {
						peerExists = true
						break
					}
				}
				
				if !peerExists {
					log.Printf("Adding peer to tunnel: %s -> %s (virtual: %s)", peer.Name, bestEndpoint, virtualIP.String())
					if err := s.tunnel.AddPeer(peer.ID, bestEndpoint, virtualIP); err != nil {
						log.Printf("failed to add peer %s: %v", peer.Name, err)
					} else {
						log.Printf("successfully added peer: %s (%s)", peer.Name, virtualIP.String())
					}
				}
			} else {
				log.Printf("Cannot determine virtual IP for peer %s", peer.Name)
			}
		}
	}

	// Measure latencies to active peers and update routing
	activePeers := s.tunnel.GetActivePeers()
	log.Printf("Found %d active tunnel peers", len(activePeers))
	
	measurements := make(map[string]time.Duration)
	for _, peer := range activePeers {
		latency, err := s.measurer.MeasureLatency(peer.VirtualIP, peer.Endpoint.String())
		if err != nil {
			log.Printf("failed to measure latency to %s: %v", peer.VirtualIP, err)
		} else {
			log.Printf("latency to %s: %v", peer.VirtualIP, latency)
			measurements[peer.VirtualIP.String()] = latency
		}
	}
	
	// Update routing optimizer with measurements
	if len(measurements) > 0 {
		s.optimizer.UpdateMeasurements(measurements)
		
		// Collect ALL known latencies from the graph for intelligent routing
		allLatencies := s.collectAllKnownLatencies(measurements)
		
		// Optimize routes for each destination
		for dest := range measurements {
			_, err := s.optimizer.OptimizeRoute(dest, allLatencies)
			if err != nil {
				log.Printf("Failed to optimize route to %s: %v", dest, err)
			} else {
				log.Printf("Optimized route to %s", dest)
			}
		}
		
		// Broadcast our latency measurements to other nodes
		s.tunnel.BroadcastLatencyInfo(s.localNode.ID, measurements)
	}
}

// collectAllKnownLatencies gathers latencies from all sources
func (s *Server) collectAllKnownLatencies(directMeasurements map[string]time.Duration) map[string]time.Duration {
	allLatencies := make(map[string]time.Duration)
	
	// Add our direct measurements
	for dest, latency := range directMeasurements {
		allLatencies[dest] = latency
	}
	
	// TODO: In future, we could:
	// 1. Query other nodes for their latency tables
	// 2. Use gossip protocol to share latency information
	// 3. Implement distributed routing table synchronization
	
	// For now, we assume other nodes will share their measurements
	// through some mechanism (could be periodic broadcasts)
	
	return allLatencies
}

func (s *Server) forwardPackets(ctx context.Context) {
	buffer := make([]byte, 1500)
	
	// Parse network CIDR for filtering
	_, virtualNet, err := net.ParseCIDR(s.config.Network.CIDR)
	if err != nil {
		log.Printf("Invalid network CIDR: %v", err)
		return
	}
	
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Read packet from TUN interface
			packet, err := s.tunInterface.ReadPacket(buffer)
			if err != nil {
				continue
			}

			// Parse destination IP from packet
			if len(packet) < 20 { // Minimum IPv4 header size
				continue
			}

			dstIP := net.IPv4(packet[16], packet[17], packet[18], packet[19])
			
			// Only forward packets destined for our virtual network
			if !virtualNet.Contains(dstIP) {
				log.Printf("Ignoring packet to %s (outside virtual network %s)", dstIP, s.config.Network.CIDR)
				continue
			}
			
			// Don't forward packets to ourselves
			if dstIP.Equal(net.ParseIP(s.localNode.VirtualIP)) {
				continue
			}
			
			log.Printf("Forwarding packet to %s through tunnel", dstIP)
			
			// Forward packet through tunnel
			if err := s.tunnel.SendData(dstIP, packet); err != nil {
				log.Printf("failed to send packet to %s: %v", dstIP, err)
			}
		}
	}
}

func detectPublicIPs() (ipv4 string, ipv6 string, err error) {
	// IPv4 detection services
	ipv4Services := []string{
		"https://ipv4.icanhazip.com",
		"https://api.ipify.org",
		"https://checkip.amazonaws.com",
	}
	
	// IPv6 detection services  
	ipv6Services := []string{
		"https://ipv6.icanhazip.com",
		"https://api6.ipify.org",
	}
	
	client := &http.Client{Timeout: 10 * time.Second}
	
	// Try to get IPv4
	for _, service := range ipv4Services {
		resp, err := client.Get(service)
		if err != nil {
			continue
		}
		defer resp.Body.Close()
		
		if resp.StatusCode == 200 {
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				continue
			}
			
			ip := strings.TrimSpace(string(body))
			if parsedIP := net.ParseIP(ip); parsedIP != nil && parsedIP.To4() != nil {
				ipv4 = ip
				break
			}
		}
	}
	
	// Try to get IPv6
	for _, service := range ipv6Services {
		resp, err := client.Get(service)
		if err != nil {
			continue
		}
		defer resp.Body.Close()
		
		if resp.StatusCode == 200 {
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				continue
			}
			
			ip := strings.TrimSpace(string(body))
			if parsedIP := net.ParseIP(ip); parsedIP != nil && parsedIP.To4() == nil {
				ipv6 = ip
				break
			}
		}
	}
	
	if ipv4 == "" && ipv6 == "" {
		return "", "", fmt.Errorf("failed to detect any public IP")
	}
	
	return ipv4, ipv6, nil
}

func (s *Server) chooseBestEndpoint(peer *discovery.Node) string {
	var candidates []struct {
		endpoint string
		isIPv6   bool
	}
	
	// Add IPv4 endpoint if available and we have IPv4 connectivity
	if peer.PublicIPv4 != "" && s.localNode.PublicIPv4 != "" {
		candidates = append(candidates, struct {
			endpoint string
			isIPv6   bool
		}{
			endpoint: fmt.Sprintf("%s:%d", peer.PublicIPv4, peer.Port),
			isIPv6:   false,
		})
	}
	
	// Add IPv6 endpoint if available and we have IPv6 connectivity  
	if peer.PublicIPv6 != "" && s.localNode.PublicIPv6 != "" {
		candidates = append(candidates, struct {
			endpoint string
			isIPv6   bool
		}{
			endpoint: fmt.Sprintf("[%s]:%d", peer.PublicIPv6, peer.Port),
			isIPv6:   true,
		})
	}
	
	// If no candidates, fall back to legacy PublicIP
	if len(candidates) == 0 && peer.PublicIP != "" {
		if strings.Contains(peer.PublicIP, ":") {
			candidates = append(candidates, struct {
				endpoint string
				isIPv6   bool
			}{
				endpoint: fmt.Sprintf("[%s]:%d", peer.PublicIP, peer.Port),
				isIPv6:   true,
			})
		} else {
			candidates = append(candidates, struct {
				endpoint string
				isIPv6   bool
			}{
				endpoint: fmt.Sprintf("%s:%d", peer.PublicIP, peer.Port),
				isIPv6:   false,
			})
		}
	}
	
	if len(candidates) == 0 {
		return ""
	}
	
	// Test latency for each candidate and choose the best one
	bestEndpoint := ""
	bestLatency := time.Hour // Start with very high latency
	
	for _, candidate := range candidates {
		latency, err := s.testEndpointLatency(candidate.endpoint)
		if err != nil {
			log.Printf("Failed to test latency to %s: %v", candidate.endpoint, err)
			continue
		}
		
		log.Printf("Latency to %s: %v", candidate.endpoint, latency)
		
		if latency < bestLatency {
			bestLatency = latency
			bestEndpoint = candidate.endpoint
		}
	}
	
	// If no endpoint responded, return the first candidate (prefer IPv4)
	if bestEndpoint == "" {
		log.Printf("No endpoints responded, using first candidate: %s", candidates[0].endpoint)
		return candidates[0].endpoint
	}
	
	log.Printf("Chose best endpoint for %s: %s (latency: %v)", peer.Name, bestEndpoint, bestLatency)
	return bestEndpoint
}

func (s *Server) testEndpointLatency(endpoint string) (time.Duration, error) {
	// Extract IP from endpoint
	host, _, err := net.SplitHostPort(endpoint)
	if err != nil {
		host = endpoint
	}
	
	// Use ICMP ping for accurate latency measurement
	pinger := &latency.ICMPPinger{}
	latency, err := pinger.Ping(host, 3*time.Second)
	if err != nil {
		// Fallback to UDP test if ICMP fails
		return s.fallbackEndpointTest(endpoint)
	}
	
	return latency, nil
}

func (s *Server) fallbackEndpointTest(endpoint string) (time.Duration, error) {
	start := time.Now()
	
	conn, err := net.DialTimeout("udp4", endpoint, 3*time.Second)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	
	// Send a test packet
	_, err = conn.Write([]byte("ping"))
	if err != nil {
		return 0, err
	}
	
	// Better estimation for fallback
	connectionTime := time.Since(start)
	estimatedLatency := connectionTime * 8
	
	minLatency := 5 * time.Millisecond
	if estimatedLatency < minLatency {
		estimatedLatency = minLatency
	}
	
	return estimatedLatency, nil
}