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
	optimizer   *routing.ShortestPathOptimizer
	localNode   *discovery.Node
}

func NewServer(cfg *config.Config) (*Server, error) {
	// Parse virtual IP
	virtualIP := net.ParseIP(cfg.Node.VirtualIP)
	if virtualIP == nil {
		return nil, fmt.Errorf("invalid virtual IP: %s", cfg.Node.VirtualIP)
	}

	// Create local node
	localNode := &discovery.Node{
		ID:        fmt.Sprintf("%s-%d", cfg.Node.Name, time.Now().Unix()),
		Name:      cfg.Node.Name,
		VirtualIP: virtualIP.String(),
		PublicIP:  "", // Will be detected or set from config
		Port:      cfg.Node.ListenPort,
		Network:   cfg.Network.Name,
		Timestamp: time.Now().Unix(),
	}

	// Auto-detect public IP if not specified
	if cfg.Node.PublicIP == "" {
		if detectedIP, err := detectPublicIP(); err == nil {
			localNode.PublicIP = detectedIP
		} else {
			log.Printf("Warning: failed to detect public IP: %v", err)
		}
	} else {
		localNode.PublicIP = cfg.Node.PublicIP
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

	// Create bootstrap discovery service
	var bootstrapURLs []string
	discoveryService := discovery.NewBootstrapService(localNode, bootstrapURLs)
	
	// Handle discovery configuration based on method
	if cfg.Bootstrap.Method == "static" {
		if staticPeersInterface, exists := cfg.Bootstrap.Config["static_peers"]; exists {
			if staticPeers, ok := staticPeersInterface.([]interface{}); ok {
				for _, peerInterface := range staticPeers {
					if peerStr, ok := peerInterface.(string); ok {
						// Parse peer endpoint (e.g., "8.211.175.127:51820")
						parts := strings.Split(peerStr, ":")
						if len(parts) == 2 {
							publicIP := parts[0]
							var port int
							if _, err := fmt.Sscanf(parts[1], "%d", &port); err == nil {
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

	// Create routing optimizer
	optimizer := routing.NewShortestPathOptimizer(cfg.Routing.OptimizeInterval)

	return &Server{
		config:       cfg,
		tunInterface: tunIface,
		tunnel:       simpleTunnel,
		discovery:    discoveryService,
		measurer:     measurer,
		optimizer:    optimizer,
		localNode:    localNode,
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
	
	// Add new peers to tunnel
	for _, peer := range peers {
		log.Printf("Checking peer: %s (ID:%s, PublicIP:%s, VirtualIP:%s)", peer.Name, peer.ID, peer.PublicIP, peer.VirtualIP)
		
		if peer.ID != s.localNode.ID && peer.PublicIP != "" {
			endpoint := fmt.Sprintf("%s:%d", peer.PublicIP, peer.Port)
			
			// For static peers, we need to set their virtual IP from our knowledge
			// Since static discovery doesn't know their virtual IP initially
			var virtualIP net.IP
			if peer.VirtualIP != "" {
				virtualIP = net.ParseIP(peer.VirtualIP)
			} else {
				// Assign virtual IP based on the peer
				// This is a temporary solution - in real implementation,
				// virtual IPs should be exchanged during handshake
				if peer.PublicIP == "47.245.15.86" {
					virtualIP = net.ParseIP("10.100.0.3")
				} else if peer.PublicIP == "8.211.175.127" {
					virtualIP = net.ParseIP("10.100.0.2")
				}
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
					log.Printf("Adding peer to tunnel: %s -> %s (virtual: %s)", peer.Name, endpoint, virtualIP.String())
					if err := s.tunnel.AddPeer(peer.ID, endpoint, virtualIP); err != nil {
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

	// Measure latencies to active peers
	activePeers := s.tunnel.GetActivePeers()
	log.Printf("Found %d active tunnel peers", len(activePeers))
	for _, peer := range activePeers {
		latency, err := s.measurer.MeasureLatency(peer.VirtualIP, peer.Endpoint.String())
		if err != nil {
			log.Printf("failed to measure latency to %s: %v", peer.VirtualIP, err)
		} else {
			log.Printf("latency to %s: %v", peer.VirtualIP, latency)
		}
	}
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

func detectPublicIP() (string, error) {
	// Use HTTP service to get real public IP
	services := []string{
		"https://ifconfig.me/ip",
		"https://ipinfo.io/ip", 
		"https://api.ipify.org",
		"https://checkip.amazonaws.com",
	}
	
	client := &http.Client{Timeout: 10 * time.Second}
	
	for _, service := range services {
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
			if net.ParseIP(ip) != nil {
				return ip, nil
			}
		}
	}
	
	return "", fmt.Errorf("failed to detect public IP from all services")
}