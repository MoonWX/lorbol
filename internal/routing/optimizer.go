package routing

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/MoonWX/lorbol/internal/config"
)

type Optimizer interface {
	OptimizeRoute(destination string, measurements map[string]time.Duration) (Route, error)
	GetBestPath(destination string) (Path, error)
	UpdateMeasurements(measurements map[string]time.Duration)
	Start(ctx context.Context) error
	Stop() error
}

type Route struct {
	Destination string
	Gateway     string
	Interface   string
	Latency     time.Duration
	Timestamp   time.Time
}

type Path struct {
	Hops      []string
	TotalHops int
	Latency   time.Duration
}

type optimizer struct {
	config       config.RoutingConfig
	routeTable   *RouteTable
	measurements map[string]time.Duration
	measuresMu   sync.RWMutex
	stopCh       chan struct{}
	isRunning    bool
	mu           sync.Mutex
}

func NewOptimizer(cfg config.RoutingConfig) Optimizer {
	return &optimizer{
		config:       cfg,
		routeTable:   NewRouteTable(),
		measurements: make(map[string]time.Duration),
		stopCh:       make(chan struct{}),
	}
}

func (o *optimizer) OptimizeRoute(destination string, measurements map[string]time.Duration) (Route, error) {
	if len(measurements) == 0 {
		return Route{}, fmt.Errorf("no measurements available for optimization")
	}

	var bestGateway string
	var bestLatency time.Duration = time.Hour

	for gateway, latency := range measurements {
		if latency < bestLatency {
			bestLatency = latency
			bestGateway = gateway
		}
	}

	if bestGateway == "" {
		return Route{}, fmt.Errorf("no suitable gateway found for destination %s", destination)
	}

	route := Route{
		Destination: destination,
		Gateway:     bestGateway,
		Interface:   "tun0",
		Latency:     bestLatency,
		Timestamp:   time.Now(),
	}

	o.routeTable.AddRoute(route)
	
	return route, nil
}

func (o *optimizer) GetBestPath(destination string) (Path, error) {
	route, exists := o.routeTable.GetRoute(destination)
	if !exists {
		return Path{}, fmt.Errorf("no route found for destination %s", destination)
	}

	path := Path{
		Hops:      []string{route.Gateway, destination},
		TotalHops: 2,
		Latency:   route.Latency,
	}

	return path, nil
}

func (o *optimizer) UpdateMeasurements(measurements map[string]time.Duration) {
	o.measuresMu.Lock()
	defer o.measuresMu.Unlock()

	for target, latency := range measurements {
		o.measurements[target] = latency
	}
}

func (o *optimizer) Start(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.isRunning {
		return fmt.Errorf("optimizer is already running")
	}

	o.isRunning = true
	o.stopCh = make(chan struct{})

	go o.optimizationLoop(ctx)
	
	return nil
}

func (o *optimizer) Stop() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if !o.isRunning {
		return nil
	}

	close(o.stopCh)
	o.isRunning = false
	
	return nil
}

func (o *optimizer) optimizationLoop(ctx context.Context) {
	ticker := time.NewTicker(o.config.OptimizeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-o.stopCh:
			return
		case <-ticker.C:
			o.performOptimization()
		}
	}
}

func (o *optimizer) performOptimization() {
	o.measuresMu.RLock()
	measurements := make(map[string]time.Duration, len(o.measurements))
	for k, v := range o.measurements {
		measurements[k] = v
	}
	o.measuresMu.RUnlock()

	if len(measurements) == 0 {
		return
	}

	destinations := o.getDestinations(measurements)
	
	for _, dest := range destinations {
		gatewayMeasurements := o.getGatewayMeasurements(dest, measurements)
		if len(gatewayMeasurements) > 0 {
			_, err := o.OptimizeRoute(dest, gatewayMeasurements)
			if err != nil {
				continue
			}
		}
	}
}

func (o *optimizer) getDestinations(measurements map[string]time.Duration) []string {
	destSet := make(map[string]bool)
	for target := range measurements {
		destSet[target] = true
	}

	destinations := make([]string, 0, len(destSet))
	for dest := range destSet {
		destinations = append(destinations, dest)
	}

	sort.Strings(destinations)
	return destinations
}

func (o *optimizer) getGatewayMeasurements(destination string, allMeasurements map[string]time.Duration) map[string]time.Duration {
	gatewayMeasurements := make(map[string]time.Duration)
	
	for target, latency := range allMeasurements {
		if latency <= o.config.LatencyThreshold {
			gatewayMeasurements[target] = latency
		}
	}
	
	return gatewayMeasurements
}