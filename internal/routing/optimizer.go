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
	UpdateDistributedLatencies(sourceNodeID string, measurements map[string]time.Duration)
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
	// Distributed latency information from all nodes
	distributedLatencies map[string]map[string]time.Duration // nodeID -> {destination -> latency}
	distributedMu        sync.RWMutex
	// Route stability tracking
	routeHistory         map[string][]RouteCandidate // destination -> recent route candidates
	routeChangePending   map[string]time.Time        // destination -> when change was first considered
	routeHistoryMu       sync.RWMutex
}

type RouteCandidate struct {
	Gateway   string
	Latency   time.Duration
	Timestamp time.Time
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
		config:               cfg,
		routeTable:           NewRouteTable(),
		measurements:         make(map[string]time.Duration),
		stopCh:               make(chan struct{}),
		graph:                NewGraph(),
		distributedLatencies: make(map[string]map[string]time.Duration),
		routeHistory:         make(map[string][]RouteCandidate),
		routeChangePending:   make(map[string]time.Time),
	}
}

func NewNetworkOptimizer(cfg config.RoutingConfig, nodeManager NodeManager) Optimizer {
	localNode := nodeManager.GetLocalNode()
	localNodeID := ""
	if localNode != nil {
		localNodeID = localNode.ID
	}
	
	return &optimizer{
		config:               cfg,
		routeTable:           NewRouteTable(),
		measurements:         make(map[string]time.Duration),
		stopCh:               make(chan struct{}),
		graph:                NewGraph(),
		nodeManager:          nodeManager,
		localNodeID:          localNodeID,
		distributedLatencies: make(map[string]map[string]time.Duration),
		routeHistory:         make(map[string][]RouteCandidate),
		routeChangePending:   make(map[string]time.Time),
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
	// Build complete latency graph from all distributed information
	o.buildCompleteLatencyGraph()
	
	// Find the shortest latency path using Dijkstra
	destinationNodeID := o.findNodeIDByVirtualIP(destination)
	if destinationNodeID == "" {
		// Fallback if we can't find the node ID
		if directLatency, hasDirectConnection := measurements[destination]; hasDirectConnection {
			route := Route{
				Destination: destination,
				Gateway:     destination,
				Interface:   "lorbol0",
				Latency:     directLatency,
				Timestamp:   time.Now(),
			}
			o.routeTable.AddRoute(route)
			fmt.Printf("Routing: Cannot find node ID for %s, using direct route (%v)\n", destination, directLatency)
			return route, nil
		}
		return Route{}, fmt.Errorf("cannot find node ID for destination %s", destination)
	}
	
	fmt.Printf("Routing: Finding path from %s to %s (VIP: %s)\n", o.localNodeID, destinationNodeID, destination)
	pathResult, err := o.graph.FindShortestPath(o.localNodeID, destinationNodeID)
	if err != nil {
		// Fallback to direct connection if no path found
		if directLatency, hasDirectConnection := measurements[destination]; hasDirectConnection {
			route := Route{
				Destination: destination,
				Gateway:     destination,
				Interface:   "lorbol0",
				Latency:     directLatency,
				Timestamp:   time.Now(),
			}
			o.routeTable.AddRoute(route)
			fmt.Printf("Routing: Fallback to direct route to %s (%v)\n", destination, directLatency)
			return route, nil
		}
		return Route{}, fmt.Errorf("no path found to %s: %w", destination, err)
	}

	var gateway string
	var pathDescription string
	if len(pathResult.Path) <= 1 {
		return Route{}, fmt.Errorf("invalid path to %s", destination)
	} else if len(pathResult.Path) == 2 {
		// Direct connection
		gateway = destination
		pathDescription = "direct"
		fmt.Printf("Routing: Direct path calculated: %s -> %s\n", o.localNodeID, destinationNodeID)
	} else {
		// Multi-hop: next hop is the second node in path (convert node ID to virtual IP)
		nextHopNodeID := pathResult.Path[1]
		gateway = o.findVirtualIPByNodeID(nextHopNodeID)
		if gateway == "" {
			gateway = nextHopNodeID // Fallback to node ID
			fmt.Printf("Routing: WARNING - Could not find virtual IP for next hop node %s\n", nextHopNodeID)
		} else {
			fmt.Printf("Routing: Multi-hop path calculated: %s -> %s -> %s (next hop: %s)\n", 
				o.localNodeID, nextHopNodeID, destinationNodeID, gateway)
		}
		pathDescription = fmt.Sprintf("via %s", nextHopNodeID)
	}

	newRoute := Route{
		Destination: destination,
		Gateway:     gateway,
		Interface:   "lorbol0",
		Latency:     pathResult.TotalLatency,
		Timestamp:   time.Now(),
	}

	// Check if we should actually apply this route change (stability check)
	if o.shouldUpdateRoute(destination, gateway, pathResult.TotalLatency) {
		o.routeTable.AddRoute(newRoute)
		fmt.Printf("Routing: Applied stable route to %s: %s (%v, %d hops)\n", 
			destination, pathDescription, pathResult.TotalLatency, pathResult.HopCount)
		
		// Clear any pending change since we applied it
		o.routeHistoryMu.Lock()
		delete(o.routeChangePending, destination)
		o.routeHistoryMu.Unlock()
	} else {
		fmt.Printf("Routing: Route change to %s pending stability check: %s (%v, %d hops)\n", 
			destination, pathDescription, pathResult.TotalLatency, pathResult.HopCount)
	}
	
	return newRoute, nil
}

func (o *optimizer) buildCompleteLatencyGraph() {
	// Clear existing graph
	o.graph = NewGraph()
	
	// Add all known nodes
	if o.nodeManager != nil {
		nodes := o.nodeManager.GetAllNodes()
		for _, node := range nodes {
			virtualIP := ""
			if node.VirtualIP != nil {
				virtualIP = node.VirtualIP.String()
			}
			o.graph.AddNode(node.ID, virtualIP, "")
			fmt.Printf("Routing: Added node %s (VIP: %s)\n", node.ID, virtualIP)
		}
	}
	
	// Add edges from our direct measurements
	o.measuresMu.RLock()
	localMeasurements := make(map[string]time.Duration)
	for dest, latency := range o.measurements {
		localMeasurements[dest] = latency
	}
	o.measuresMu.RUnlock()
	
	for dest, latency := range localMeasurements {
		// Convert virtual IP to node ID
		destNodeID := o.findNodeIDByVirtualIP(dest)
		if destNodeID != "" {
			o.graph.UpdateEdge(o.localNodeID, destNodeID, latency)
			fmt.Printf("Routing: Added edge %s -> %s: %v\n", o.localNodeID, destNodeID, latency)
		}
	}
	
	// Add edges from distributed latency information
	o.distributedMu.RLock()
	for sourceNodeID, measurements := range o.distributedLatencies {
		for dest, latency := range measurements {
			destNodeID := o.findNodeIDByVirtualIP(dest)
			if destNodeID != "" {
				o.graph.UpdateEdge(sourceNodeID, destNodeID, latency)
				fmt.Printf("Routing: Added distributed edge %s -> %s: %v\n", sourceNodeID, destNodeID, latency)
			}
		}
	}
	o.distributedMu.RUnlock()
	
	fmt.Printf("Routing: Built complete latency graph with %d nodes\n", len(o.graph.nodes))
}

func (o *optimizer) findNodeIDByVirtualIP(virtualIP string) string {
	if o.nodeManager == nil {
		return ""
	}
	
	nodes := o.nodeManager.GetAllNodes()
	for _, node := range nodes {
		if node.VirtualIP != nil && node.VirtualIP.String() == virtualIP {
			return node.ID
		}
	}
	return ""
}

func (o *optimizer) findVirtualIPByNodeID(nodeID string) string {
	if o.nodeManager == nil {
		return ""
	}
	
	nodes := o.nodeManager.GetAllNodes()
	for _, node := range nodes {
		if node.ID == nodeID && node.VirtualIP != nil {
			return node.VirtualIP.String()
		}
	}
	return ""
}

// shouldUpdateRoute implements route stability checking similar to WireGuard
func (o *optimizer) shouldUpdateRoute(destination, newGateway string, newLatency time.Duration) bool {
	o.routeHistoryMu.Lock()
	defer o.routeHistoryMu.Unlock()
	
	// Get current route
	currentRoute, hasCurrentRoute := o.routeTable.GetRoute(destination)
	
	// If no current route, allow the new route immediately
	if !hasCurrentRoute {
		fmt.Printf("Routing: No existing route to %s, applying new route immediately\n", destination)
		return true
	}
	
	// If it's the same gateway, always allow updates (just latency changes)
	if currentRoute.Gateway == newGateway {
		return true
	}
	
	// Different gateway - check if the improvement is significant and stable
	improvementThreshold := 20 * time.Millisecond // Must be at least 20ms better
	stabilityDuration := 30 * time.Second         // Must be stable for 30 seconds
	
	latencyImprovement := currentRoute.Latency - newLatency
	if latencyImprovement < improvementThreshold {
		fmt.Printf("Routing: Route change to %s rejected - improvement too small: %v < %v\n", 
			destination, latencyImprovement, improvementThreshold)
		return false
	}
	
	// Track this route candidate
	candidate := RouteCandidate{
		Gateway:   newGateway,
		Latency:   newLatency,
		Timestamp: time.Now(),
	}
	
	// Add to history
	if o.routeHistory[destination] == nil {
		o.routeHistory[destination] = make([]RouteCandidate, 0)
	}
	o.routeHistory[destination] = append(o.routeHistory[destination], candidate)
	
	// Keep only recent candidates (last 2 minutes)
	cutoff := time.Now().Add(-2 * time.Minute)
	recent := make([]RouteCandidate, 0)
	for _, c := range o.routeHistory[destination] {
		if c.Timestamp.After(cutoff) {
			recent = append(recent, c)
		}
	}
	o.routeHistory[destination] = recent
	
	// Check if this gateway has been consistently better for the stability duration
	if pendingTime, isPending := o.routeChangePending[destination]; isPending {
		// Already considering this change
		if time.Since(pendingTime) >= stabilityDuration {
			// Check if the new gateway has been consistently better
			consistent := true
			for _, c := range recent {
				if c.Gateway == newGateway {
					// This gateway should consistently show improvement
					if (currentRoute.Latency - c.Latency) < improvementThreshold {
						consistent = false
						break
					}
				}
			}
			
			if consistent {
				fmt.Printf("Routing: Route change to %s approved after stability check (%v)\n", 
					destination, time.Since(pendingTime))
				return true
			}
		}
	} else {
		// Start considering this change
		o.routeChangePending[destination] = time.Now()
		fmt.Printf("Routing: Route change to %s under consideration for stability check\n", destination)
	}
	
	return false
}

// periodicRouteDiscovery implements WireGuard-style automatic route discovery
func (o *optimizer) periodicRouteDiscovery() {
	// Every few minutes, probe alternative paths to ensure we have the best routes
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	
	for range ticker.C {
		if o.nodeManager == nil {
			continue
		}
		
		nodes := o.nodeManager.GetAllNodes()
		for _, node := range nodes {
			if node.ID == o.localNodeID || node.VirtualIP == nil {
				continue
			}
			
			destination := node.VirtualIP.String()
			
			// Check if we have multiple potential paths to this destination
			o.exploreAlternativePaths(destination)
		}
	}
}

func (o *optimizer) exploreAlternativePaths(destination string) {
	// Try to find alternative paths through different intermediate nodes
	// This is similar to WireGuard's automatic path discovery
	
	if o.nodeManager == nil {
		return
	}
	
	allNodes := o.nodeManager.GetAllNodes()
	localNode := o.nodeManager.GetLocalNode()
	if localNode == nil {
		return
	}
	
	// Find potential intermediate nodes
	var intermediates []*NetworkNode
	for _, node := range allNodes {
		if node.ID != localNode.ID && node.VirtualIP != nil && node.VirtualIP.String() != destination {
			intermediates = append(intermediates, node)
		}
	}
	
	fmt.Printf("Routing: Exploring alternative paths to %s through %d intermediates\n", 
		destination, len(intermediates))
	
	// For each intermediate, estimate the 2-hop path cost
	for _, intermediate := range intermediates {
		o.probePathViaIntermediate(destination, intermediate)
	}
}

func (o *optimizer) probePathViaIntermediate(destination string, intermediate *NetworkNode) {
	// This would probe the path: local -> intermediate -> destination
	// In a full implementation, this would send probe packets through the intermediate
	
	intermediateIP := intermediate.VirtualIP.String()
	
	// Get current latencies
	o.measuresMu.RLock()
	latencyToIntermediate, hasToIntermediate := o.measurements[intermediateIP]
	latencyToDest, hasToDest := o.measurements[destination]
	o.measuresMu.RUnlock()
	
	if !hasToIntermediate {
		return
	}
	
	// Estimate 2-hop latency (this is simplified - real implementation would probe)
	var estimatedViaIntermediate time.Duration
	if hasToDest {
		// If we have direct measurement, the 2-hop path needs to be significantly better
		estimatedViaIntermediate = latencyToIntermediate + (latencyToDest / 2) // Simplified estimation
	} else {
		// No direct path known, estimate based on intermediate latency
		estimatedViaIntermediate = latencyToIntermediate + (50 * time.Millisecond) // Conservative estimate
	}
	
	fmt.Printf("Routing: Estimated path %s -> %s -> %s: %v\n", 
		o.localNodeID, intermediate.ID, destination, estimatedViaIntermediate)
		
	// Update distributed latencies with this estimation
	o.distributedMu.Lock()
	if o.distributedLatencies[o.localNodeID] == nil {
		o.distributedLatencies[o.localNodeID] = make(map[string]time.Duration)
	}
	o.distributedLatencies[o.localNodeID][destination] = estimatedViaIntermediate
	o.distributedMu.Unlock()
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

	// If gateway equals destination, it means direct connection - return empty string to indicate direct
	if route.Gateway == destination {
		return "", nil
	}

	// Return the gateway as next hop for multi-hop routing
	return route.Gateway, nil
}

func (o *optimizer) UpdateMeasurements(measurements map[string]time.Duration) {
	o.measuresMu.Lock()
	defer o.measuresMu.Unlock()

	for target, latency := range measurements {
		o.measurements[target] = latency
	}
}

func (o *optimizer) UpdateDistributedLatencies(sourceNodeID string, measurements map[string]time.Duration) {
	o.distributedMu.Lock()
	defer o.distributedMu.Unlock()
	
	o.distributedLatencies[sourceNodeID] = make(map[string]time.Duration)
	for dest, latency := range measurements {
		o.distributedLatencies[sourceNodeID][dest] = latency
	}
	
	fmt.Printf("Routing: Updated distributed latencies from %s:\n", sourceNodeID)
	for dest, latency := range measurements {
		fmt.Printf("  %s -> %s: %v\n", sourceNodeID, dest, latency)
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
	
	// Start WireGuard-style route discovery if we have a node manager
	if o.nodeManager != nil {
		go o.periodicRouteDiscovery()
	}
	
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