package network

import (
	"fmt"
	"net"
	"sync"
)

type VirtualNetwork struct {
	Name        string
	CIDR        string
	Network     *net.IPNet
	Gateway     net.IP
	DNSServers  []net.IP
	Routes      map[string]*Route
	Subnets     map[string]*Subnet
	mu          sync.RWMutex
}

type Route struct {
	Destination *net.IPNet
	Gateway     net.IP
	Interface   string
	Metric      int
	IsStatic    bool
}

type Subnet struct {
	CIDR    string
	Network *net.IPNet
	Purpose string // "nodes", "services", "reserved"
}

func NewVirtualNetwork(name, cidr string) (*VirtualNetwork, error) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR: %w", err)
	}

	gateway := make(net.IP, len(network.IP))
	copy(gateway, network.IP)
	// Set gateway to first IP in network (e.g., 10.0.0.1 for 10.0.0.0/8)
	gateway[len(gateway)-1] = 1

	vn := &VirtualNetwork{
		Name:    name,
		CIDR:    cidr,
		Network: network,
		Gateway: gateway,
		Routes:  make(map[string]*Route),
		Subnets: make(map[string]*Subnet),
	}

	// Create default subnets
	vn.createDefaultSubnets()

	return vn, nil
}

func (vn *VirtualNetwork) createDefaultSubnets() {
	// For 10.0.0.0/8, create subnets like:
	// 10.0.0.0/16 for nodes
	// 10.1.0.0/16 for services
	// 10.255.0.0/16 for reserved

	ip := vn.Network.IP
	ones, _ := vn.Network.Mask.Size()

	// Create smaller subnets for different purposes
	if ones < 16 {
		// Nodes subnet
		nodesCIDR := fmt.Sprintf("%d.%d.0.0/16", ip[0], ip[1])
		vn.addSubnet("nodes", nodesCIDR, "nodes")

		// Services subnet
		servicesCIDR := fmt.Sprintf("%d.%d.0.0/16", ip[0], ip[1]+1)
		vn.addSubnet("services", servicesCIDR, "services")

		// Reserved subnet
		reservedCIDR := fmt.Sprintf("%d.255.0.0/16", ip[0])
		vn.addSubnet("reserved", reservedCIDR, "reserved")
	} else {
		// For smaller networks, use the whole network for nodes
		vn.addSubnet("nodes", vn.CIDR, "nodes")
	}
}

func (vn *VirtualNetwork) addSubnet(name, cidr, purpose string) error {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("invalid subnet CIDR: %w", err)
	}

	subnet := &Subnet{
		CIDR:    cidr,
		Network: network,
		Purpose: purpose,
	}

	vn.Subnets[name] = subnet
	return nil
}

func (vn *VirtualNetwork) IsIPInNetwork(ip net.IP) bool {
	vn.mu.RLock()
	defer vn.mu.RUnlock()
	return vn.Network.Contains(ip)
}

func (vn *VirtualNetwork) IsIPInSubnet(ip net.IP, subnetName string) bool {
	vn.mu.RLock()
	defer vn.mu.RUnlock()

	subnet, exists := vn.Subnets[subnetName]
	if !exists {
		return false
	}

	return subnet.Network.Contains(ip)
}

func (vn *VirtualNetwork) AllocateNodeIP() (net.IP, error) {
	vn.mu.Lock()
	defer vn.mu.Unlock()

	nodesSubnet, exists := vn.Subnets["nodes"]
	if !exists {
		return nil, fmt.Errorf("nodes subnet not found")
	}

	// Simple allocation: start from .2 and increment
	// In a real implementation, you'd track allocated IPs
	ip := make(net.IP, len(nodesSubnet.Network.IP))
	copy(ip, nodesSubnet.Network.IP)
	
	// Start from .2 (assuming .1 is gateway)
	ip[len(ip)-1] = 2

	// TODO: Implement proper IP allocation with tracking
	// This is a simplified version
	
	return ip, nil
}

func (vn *VirtualNetwork) AddRoute(destination, gateway, iface string, metric int, isStatic bool) error {
	vn.mu.Lock()
	defer vn.mu.Unlock()

	_, destNet, err := net.ParseCIDR(destination)
	if err != nil {
		return fmt.Errorf("invalid destination CIDR: %w", err)
	}

	gatewayIP := net.ParseIP(gateway)
	if gatewayIP == nil {
		return fmt.Errorf("invalid gateway IP: %s", gateway)
	}

	route := &Route{
		Destination: destNet,
		Gateway:     gatewayIP,
		Interface:   iface,
		Metric:      metric,
		IsStatic:    isStatic,
	}

	vn.Routes[destination] = route
	return nil
}

func (vn *VirtualNetwork) RemoveRoute(destination string) error {
	vn.mu.Lock()
	defer vn.mu.Unlock()

	delete(vn.Routes, destination)
	return nil
}

func (vn *VirtualNetwork) GetRoute(destination string) (*Route, bool) {
	vn.mu.RLock()
	defer vn.mu.RUnlock()

	route, exists := vn.Routes[destination]
	return route, exists
}

func (vn *VirtualNetwork) ListRoutes() map[string]*Route {
	vn.mu.RLock()
	defer vn.mu.RUnlock()

	routes := make(map[string]*Route, len(vn.Routes))
	for k, v := range vn.Routes {
		routes[k] = v
	}

	return routes
}

func (vn *VirtualNetwork) GetNetworkInfo() map[string]interface{} {
	vn.mu.RLock()
	defer vn.mu.RUnlock()

	info := map[string]interface{}{
		"name":        vn.Name,
		"cidr":        vn.CIDR,
		"gateway":     vn.Gateway.String(),
		"dns_servers": make([]string, len(vn.DNSServers)),
		"subnets":     make(map[string]string),
		"route_count": len(vn.Routes),
	}

	for i, dns := range vn.DNSServers {
		info["dns_servers"].([]string)[i] = dns.String()
	}

	for name, subnet := range vn.Subnets {
		info["subnets"].(map[string]string)[name] = subnet.CIDR
	}

	return info
}

func (vn *VirtualNetwork) ValidateNodeIP(ip net.IP) error {
	if !vn.IsIPInNetwork(ip) {
		return fmt.Errorf("IP %s is not in network %s", ip, vn.CIDR)
	}

	if ip.Equal(vn.Gateway) {
		return fmt.Errorf("IP %s is reserved for gateway", ip)
	}

	// Check if IP is in nodes subnet
	if !vn.IsIPInSubnet(ip, "nodes") {
		return fmt.Errorf("IP %s is not in nodes subnet", ip)
	}

	return nil
}