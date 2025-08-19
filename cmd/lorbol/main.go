package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/MoonWX/lorbol/internal/config"
	"github.com/MoonWX/lorbol/internal/latency"
	"github.com/MoonWX/lorbol/internal/routing"
	"github.com/MoonWX/lorbol/internal/vpn"
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

	measurer := latency.NewMeasurer(cfg.Latency)
	optimizer := routing.NewOptimizer(cfg.Routing)
	tunnel := vpn.NewTunnel(cfg.VPN)

	server := &Server{
		config:    cfg,
		measurer:  measurer,
		optimizer: optimizer,
		tunnel:    tunnel,
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
	config    *config.Config
	measurer  latency.Measurer
	optimizer routing.Optimizer
	tunnel    vpn.Tunnel
}

func (s *Server) Start(ctx context.Context) error {
	log.Println("starting LORBOL server...")
	return nil
}

func (s *Server) Stop() error {
	log.Println("stopping LORBOL server...")
	return nil
}