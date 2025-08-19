package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
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

	// Create bootstrap discovery service
	var bootstrapURLs []string
	if cfg.Bootstrap.Method == "static" {
		// For static method, we'll handle peers directly
		bootstrapURLs = []string{}
	} else {
		// For other methods, use default URLs for now
		bootstrapURLs = []string{}
	}
	discoveryService := discovery.NewBootstrapService(localNode, bootstrapURLs)

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
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Get discovered peers
			peers := s.discovery.GetKnownPeers()
			
			// Add new peers to tunnel
			for _, peer := range peers {
				if peer.ID != s.localNode.ID && peer.PublicIP != "" {
					endpoint := fmt.Sprintf("%s:%d", peer.PublicIP, peer.Port)
					virtualIP := net.ParseIP(peer.VirtualIP)
					if virtualIP != nil {
						if err := s.tunnel.AddPeer(peer.ID, endpoint, virtualIP); err != nil {
							log.Printf("failed to add peer %s: %v", peer.Name, err)
						} else {
							log.Printf("added peer: %s (%s)", peer.Name, peer.VirtualIP)
						}
					}
				}
			}

			// Measure latencies to active peers
			activePeers := s.tunnel.GetActivePeers()
			for _, peer := range activePeers {
				latency, err := s.measurer.MeasureLatency(peer.VirtualIP, peer.Endpoint.String())
				if err != nil {
					log.Printf("failed to measure latency to %s: %v", peer.VirtualIP, err)
				} else {
					log.Printf("latency to %s: %v", peer.VirtualIP, latency)
				}
			}
		}
	}
}

func (s *Server) forwardPackets(ctx context.Context) {
	buffer := make([]byte, 1500)
	
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
			
			// Forward packet through tunnel
			if err := s.tunnel.SendData(dstIP, packet); err != nil {
				log.Printf("failed to send packet to %s: %v", dstIP, err)
			}
		}
	}
}

func detectPublicIP() (string, error) {
	// Try to detect public IP by connecting to a remote server
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", err
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String(), nil
}