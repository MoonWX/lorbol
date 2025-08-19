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

	"github.com/MoonWX/lorbol/internal/adapters"
	"github.com/MoonWX/lorbol/internal/config"
	"github.com/MoonWX/lorbol/internal/latency"
	"github.com/MoonWX/lorbol/internal/network"
	"github.com/MoonWX/lorbol/internal/routing"
	"github.com/MoonWX/lorbol/internal/tunnel"
)

func main() {
	var configPath = flag.String("config", "config.yaml", "path to configuration file")
	flag.Parse()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create virtual network
	virtualNet, err := network.NewVirtualNetwork(cfg.Network.Name, cfg.Network.CIDR)
	if err != nil {
		log.Fatalf("failed to create virtual network: %v", err)
	}

	// Create local node
	localNode, err := createLocalNode(cfg)
	if err != nil {
		log.Fatalf("failed to create local node: %v", err)
	}

	// Create node manager
	nodeManager := network.NewNodeManager(localNode, virtualNet)

	// Create adapters for interface compatibility
	latencyAdapter := adapters.NewLatencyNodeManagerAdapter(nodeManager)
	routingAdapter := adapters.NewRoutingNodeManagerAdapter(nodeManager)
	
	// Create services with network awareness
	measurer := latency.NewNetworkMeasurer(cfg.Latency, latencyAdapter)
	optimizer := routing.NewNetworkOptimizer(cfg.Routing, routingAdapter)
	
	// Create tunnel manager (WireGuard-based VPN)
	tunnelAdapter := adapters.NewTunnelNodeManagerAdapter(nodeManager)
	tunnelManager, err := tunnel.NewTunnelManager(cfg.VPN, tunnelAdapter)
	if err != nil {
		log.Fatalf("failed to create tunnel manager: %v", err)
	}
	
	// Create discovery service
	discovery := network.NewDiscoveryService(localNode, nodeManager)
	
	// Create P2P protocol
	p2p := network.NewP2PProtocol(localNode, nodeManager)

	server := &Server{
		config:        cfg,
		virtualNet:    virtualNet,
		localNode:     localNode,
		nodeManager:   nodeManager,
		measurer:      measurer,
		optimizer:     optimizer,
		tunnelManager: tunnelManager,
		discovery:     discovery,
		p2p:           p2p,
	}

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
	config        *config.Config
	virtualNet    *network.VirtualNetwork
	localNode     *network.Node
	nodeManager   *network.NodeManager
	measurer      latency.Measurer
	optimizer     routing.Optimizer
	tunnelManager *tunnel.TunnelManager
	discovery     *network.DiscoveryService
	p2p           *network.P2PProtocol
}

func (s *Server) Start(ctx context.Context) error {
	log.Printf("starting LORBOL server for node %s in network %s...", s.localNode.Name, s.virtualNet.Name)
	
	// Start all services
	if err := s.nodeManager.Start(ctx); err != nil {
		return fmt.Errorf("failed to start node manager: %w", err)
	}
	
	if err := s.discovery.Start(ctx); err != nil {
		return fmt.Errorf("failed to start discovery service: %w", err)
	}
	
	if err := s.p2p.Start(ctx); err != nil {
		return fmt.Errorf("failed to start P2P protocol: %w", err)
	}
	
	if err := s.measurer.Start(ctx); err != nil {
		return fmt.Errorf("failed to start latency measurer: %w", err)
	}
	
	if err := s.optimizer.Start(ctx); err != nil {
		return fmt.Errorf("failed to start route optimizer: %w", err)
	}
	
	if err := s.tunnelManager.Start(ctx); err != nil {
		return fmt.Errorf("failed to start tunnel manager: %w", err)
	}
	
	log.Printf("LORBOL server started successfully")
	log.Printf("Local node: %s (%s)", s.localNode.VirtualIP, s.localNode.Name)
	log.Printf("Network: %s", s.virtualNet.CIDR)
	
	return nil
}

func (s *Server) Stop() error {
	log.Println("stopping LORBOL server...")
	
	if err := s.tunnelManager.Stop(); err != nil {
		log.Printf("error stopping tunnel manager: %v", err)
	}
	
	if err := s.optimizer.Stop(); err != nil {
		log.Printf("error stopping optimizer: %v", err)
	}
	
	if err := s.measurer.Stop(); err != nil {
		log.Printf("error stopping measurer: %v", err)
	}
	
	if err := s.p2p.Stop(); err != nil {
		log.Printf("error stopping P2P protocol: %v", err)
	}
	
	if err := s.discovery.Stop(); err != nil {
		log.Printf("error stopping discovery service: %v", err)
	}
	
	if err := s.nodeManager.Stop(); err != nil {
		log.Printf("error stopping node manager: %v", err)
	}
	
	log.Println("LORBOL server stopped")
	return nil
}

func createLocalNode(cfg *config.Config) (*network.Node, error) {
	virtualIP := cfg.Node.VirtualIP
	if virtualIP == "" {
		return nil, fmt.Errorf("virtual IP is required")
	}
	
	publicIP := cfg.Node.PublicIP
	if publicIP == "" && cfg.Node.AutoDetectIP {
		// Auto-detect public IP
		if detectedIP, err := detectPublicIP(); err == nil {
			publicIP = detectedIP
		}
	}
	
	if publicIP == "" {
		return nil, fmt.Errorf("public IP is required and auto-detection failed")
	}
	
	return network.NewNode(cfg.Node.Name, virtualIP, publicIP, cfg.Node.Port)
}

func detectPublicIP() (string, error) {
	// Simple implementation: use local IP for now
	// In production, you'd want to use STUN or similar
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", err
	}
	defer conn.Close()
	
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String(), nil
}