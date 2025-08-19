package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/MoonWX/lorbol/internal/config"
	"github.com/MoonWX/lorbol/internal/latency"
	"github.com/MoonWX/lorbol/internal/routing"
)

func main() {
	var (
		configPath = flag.String("config", "config.yaml", "path to configuration file")
		command    = flag.String("cmd", "", "command to execute: measure, optimize, status")
		target     = flag.String("target", "", "target address for measurement")
	)
	flag.Parse()

	if *command == "" {
		fmt.Fprintf(os.Stderr, "Usage: %s -cmd <command> [options]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  measure -target <address>  : measure latency to target\n")
		fmt.Fprintf(os.Stderr, "  optimize                   : run route optimization\n")
		fmt.Fprintf(os.Stderr, "  status                     : show current status\n")
		os.Exit(1)
	}

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	switch *command {
	case "measure":
		if *target == "" {
			log.Fatal("target address is required for measure command")
		}
		measureLatency(cfg, *target)
	case "optimize":
		optimizeRoutes(cfg)
	case "status":
		showStatus(cfg)
	default:
		log.Fatalf("unknown command: %s", *command)
	}
}

func measureLatency(cfg *config.Config, target string) {
	measurer := latency.NewMeasurer(cfg.Latency)
	duration, err := measurer.Measure(target)
	if err != nil {
		log.Fatalf("failed to measure latency: %v", err)
	}
	fmt.Printf("Latency to %s: %v\n", target, duration)
}

func optimizeRoutes(cfg *config.Config) {
	optimizer := routing.NewOptimizer(cfg.Routing)
	fmt.Println("Running route optimization...")
	_ = optimizer
}

func showStatus(cfg *config.Config) {
	fmt.Println("LORBOL Status:")
	fmt.Printf("Network: %s\n", cfg.Network.Name)
	fmt.Printf("Node: %s\n", cfg.Node.Name)
	fmt.Printf("Virtual IP: %s\n", cfg.Node.VirtualIP)
	fmt.Printf("Bootstrap method: %s\n", cfg.Bootstrap.Method)
}