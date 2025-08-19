package routing

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/MoonWX/lorbol/internal/config"
)

type Optimizer interface {
	OptimizeRoute(destination string, measurements map[string]time.Duration) (Route, error)
	GetBestPath(destination string) (Path, error)
	GetBestNextHop(destination string) (string, error)
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
	graph        *Graph
	nodeManager  NodeManager
	localNodeID  string
}

type NodeManager interface {
	GetAllNodes() []*NetworkNode
	GetLocalNode() *NetworkNode
	GetOnlinePeers() []*NetworkNode
}

type NetworkNode struct {
	ID        string
	Name      string
	VirtualIP net.IP
	PublicIP  net.IP
	Port      int
	IsOnline  bool
}

func NewOptimizer(cfg config.RoutingConfig) Optimizer {
	return &optimizer{
		config:       cfg,
		routeTable:   NewRouteTable(),
		measurements: make(map[string]time.Duration),
		stopCh:       make(chan struct{}),
		graph:        NewGraph(),
	}
}

func NewNetworkOptimizer(cfg config.RoutingConfig, nodeManager NodeManager) Optimizer {
	localNode := nodeManager.GetLocalNode()
	localNodeID := ""
	if localNode != nil {
		localNodeID = localNode.ID
	}
	
	return &optimizer{
		config:       cfg,
		routeTable:   NewRouteTable(),
		measurements: make(map[string]time.Duration),
		stopCh:       make(chan struct{}),
		graph:        NewGraph(),
		nodeManager:  nodeManager,
		localNodeID:  localNodeID,
	}
}

func (o *optimizer) OptimizeRoute(destination string, measurements map[string]time.Duration) (Route, error) {
	if len(measurements) == 0 {
		return Route{}, fmt.Errorf("no measurements available for optimization")
	}

	// If we have node manager, use graph-based routing
	if o.nodeManager != nil && o.localNodeID != "" {
		return o.optimizeWithGraph(destination, measurements)
	}

	// Fallback: simple direct routing
	return o.optimizeDirectRoute(destination, measurements)
}

func (o *optimizer) optimizeWithGraph(destination string, measurements map[string]time.Duration) (Route, error) {
	// Update graph with current measurements
	o.updateNetworkGraph(measurements)
	
	// Find shortest path using Dijkstra
	pathResult, err := o.graph.FindShortestPath(o.localNodeID, destination)
	if err != nil {
		return Route{}, fmt.Errorf("no path found to %s: %w", destination, err)
	}

	var gateway string
	if len(pathResult.Path) <= 1 {
		return Route{}, fmt.Errorf("invalid path to %s", destination)
	} else if len(pathResult.Path) == 2 {
		// Direct connection
		gateway = destination
	} else {
		// Multi-hop: next hop is the second node in path
		gateway = pathResult.Path[1]
	}

	route := Route{
		Destination: destination,
		Gateway:     gateway,
		Interface:   "lorbol0",
		Latency:     pathResult.TotalLatency,
		Timestamp:   time.Now(),
	}

	o.routeTable.AddRoute(route)
	fmt.Printf("Routing: Optimized route to %s via %s (latency: %v, hops: %d)\n", 
		destination, gateway, pathResult.TotalLatency, pathResult.HopCount)
	
	return route, nil
}

func (o *optimizer) optimizeDirectRoute(destination string, measurements map[string]time.Duration) (Route, error) {
	// Check if we can reach destination directly
	if directLatency, canDirectConnect := measurements[destination]; canDirectConnect {
		route := Route{
			Destination: destination,
			Gateway:     destination, // Direct connection
			Interface:   "lorbol0",
			Latency:     directLatency,
			Timestamp:   time.Now(),
		}

		o.routeTable.AddRoute(route)
		return route, nil
	}

	return Route{}, fmt.Errorf("no direct route to destination %s available", destination)
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

func (o *optimizer) GetBestNextHop(destination string) (string, error) {
	route, exists := o.routeTable.GetRoute(destination)
	if !exists {
		return "", fmt.Errorf("no route found for destination %s", destination)
	}

	// If gateway equals destination, it means direct connection
	if route.Gateway == destination {
		return "", fmt.Errorf("direct connection to %s, no next hop needed", destination)
	}

	// Return the gateway as next hop
	return route.Gateway, nil
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

	// Update graph with current measurements if we have node manager
	if o.nodeManager != nil {
		o.updateNetworkGraph(measurements)
		o.optimizeNetworkRoutes()
	} else {
		// Fallback to simple optimization
		o.performSimpleOptimization(measurements)
	}
}

func (o *optimizer) updateNetworkGraph(measurements map[string]time.Duration) {
	nodes := o.nodeManager.GetAllNodes()
	
	// Add all nodes to graph
	for _, node := range nodes {
		virtualIP := ""
		publicIP := ""
		if node.VirtualIP != nil {
			virtualIP = node.VirtualIP.String()
		}
		if node.PublicIP != nil {
			publicIP = node.PublicIP.String()
		}
		o.graph.AddNode(node.ID, virtualIP, publicIP)
	}
	
	// Add edges based on measurements
	localNode := o.nodeManager.GetLocalNode()
	if localNode == nil {
		return
	}
	
	for _, node := range nodes {
		if node.ID == localNode.ID {
			continue
		}
		
		// Look for latency measurement to this node
		var latency time.Duration
		var found bool
		
		if node.VirtualIP != nil {
			if l, exists := measurements[node.VirtualIP.String()]; exists {
				latency = l
				found = true
			}
		}
		
		if !found && node.PublicIP != nil {
			if l, exists := measurements[node.PublicIP.String()]; exists {
				latency = l
				found = true
			}
		}
		
		if found {
			o.graph.UpdateEdge(localNode.ID, node.ID, latency)
		}
	}
	
	// Clean up stale edges
	o.graph.CleanupStaleEdges(5 * time.Minute)
}

func (o *optimizer) optimizeNetworkRoutes() {
	if o.localNodeID == "" {
		return
	}
	
	// Find shortest paths to all other nodes
	paths, err := o.graph.FindAllShortestPaths(o.localNodeID)
	if err != nil {
		return
	}
	
	// Update route table with optimized paths
	for nodeID, pathResult := range paths {
		if len(pathResult.Path) > 1 {
			nextHop := pathResult.Path[1] // Next node in the path
			
			// Find the node to get its virtual IP
			nodes := o.nodeManager.GetAllNodes()
			for _, node := range nodes {
				if node.ID == nodeID && node.VirtualIP != nil {
					route := Route{
						Destination: node.VirtualIP.String(),
						Gateway:     nextHop,
						Interface:   "lorbol0",
						Latency:     pathResult.TotalLatency,
						Timestamp:   time.Now(),
					}
					o.routeTable.AddRoute(route)
					break
				}
			}
		}
	}
}

func (o *optimizer) performSimpleOptimization(measurements map[string]time.Duration) {
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

func (o *optimizer) getGatewayMeasurements(_ string, allMeasurements map[string]time.Duration) map[string]time.Duration {
	gatewayMeasurements := make(map[string]time.Duration)
	
	for target, latency := range allMeasurements {
		if latency <= o.config.LatencyThreshold {
			gatewayMeasurements[target] = latency
		}
	}
	
	return gatewayMeasurements
}