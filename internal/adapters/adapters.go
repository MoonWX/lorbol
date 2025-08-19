package adapters

import (
	"github.com/MoonWX/lorbol/internal/latency"
	"github.com/MoonWX/lorbol/internal/network"
	"github.com/MoonWX/lorbol/internal/routing"
	"github.com/MoonWX/lorbol/internal/tunnel"
)

// LatencyNodeManagerAdapter adapts network.NodeManager to latency.NodeManager
type LatencyNodeManagerAdapter struct {
	nodeManager *network.NodeManager
}

func NewLatencyNodeManagerAdapter(nm *network.NodeManager) latency.NodeManager {
	return &LatencyNodeManagerAdapter{nodeManager: nm}
}

func (a *LatencyNodeManagerAdapter) GetAllNodes() []*latency.Node {
	networkNodes := a.nodeManager.GetAllNodes()
	latencyNodes := make([]*latency.Node, len(networkNodes))
	
	for i, nn := range networkNodes {
		latencyNodes[i] = &latency.Node{
			ID:        nn.ID,
			Name:      nn.Name,
			VirtualIP: nn.VirtualIP,
			PublicIP:  nn.PublicIP,
			Port:      nn.Port,
			IsOnline:  nn.IsOnline,
		}
	}
	
	return latencyNodes
}

func (a *LatencyNodeManagerAdapter) GetOnlinePeers() []*latency.Node {
	networkNodes := a.nodeManager.GetOnlinePeers()
	latencyNodes := make([]*latency.Node, len(networkNodes))
	
	for i, nn := range networkNodes {
		latencyNodes[i] = &latency.Node{
			ID:        nn.ID,
			Name:      nn.Name,
			VirtualIP: nn.VirtualIP,
			PublicIP:  nn.PublicIP,
			Port:      nn.Port,
			IsOnline:  nn.IsOnline,
		}
	}
	
	return latencyNodes
}

// RoutingNodeManagerAdapter adapts network.NodeManager to routing.NodeManager
type RoutingNodeManagerAdapter struct {
	nodeManager *network.NodeManager
}

func NewRoutingNodeManagerAdapter(nm *network.NodeManager) routing.NodeManager {
	return &RoutingNodeManagerAdapter{nodeManager: nm}
}

func (a *RoutingNodeManagerAdapter) GetAllNodes() []*routing.NetworkNode {
	networkNodes := a.nodeManager.GetAllNodes()
	routingNodes := make([]*routing.NetworkNode, len(networkNodes))
	
	for i, nn := range networkNodes {
		routingNodes[i] = &routing.NetworkNode{
			ID:        nn.ID,
			Name:      nn.Name,
			VirtualIP: nn.VirtualIP,
			PublicIP:  nn.PublicIP,
			Port:      nn.Port,
			IsOnline:  nn.IsOnline,
		}
	}
	
	return routingNodes
}

func (a *RoutingNodeManagerAdapter) GetLocalNode() *routing.NetworkNode {
	localNode := a.nodeManager.GetLocalNode()
	if localNode == nil {
		return nil
	}
	
	return &routing.NetworkNode{
		ID:        localNode.ID,
		Name:      localNode.Name,
		VirtualIP: localNode.VirtualIP,
		PublicIP:  localNode.PublicIP,
		Port:      localNode.Port,
		IsOnline:  localNode.IsOnline,
	}
}

func (a *RoutingNodeManagerAdapter) GetOnlinePeers() []*routing.NetworkNode {
	networkNodes := a.nodeManager.GetOnlinePeers()
	routingNodes := make([]*routing.NetworkNode, len(networkNodes))
	
	for i, nn := range networkNodes {
		routingNodes[i] = &routing.NetworkNode{
			ID:        nn.ID,
			Name:      nn.Name,
			VirtualIP: nn.VirtualIP,
			PublicIP:  nn.PublicIP,
			Port:      nn.Port,
			IsOnline:  nn.IsOnline,
		}
	}
	
	return routingNodes
}

// TunnelNodeManagerAdapter adapts network.NodeManager to tunnel.NodeManager
type TunnelNodeManagerAdapter struct {
	nodeManager *network.NodeManager
}

func NewTunnelNodeManagerAdapter(nm *network.NodeManager) tunnel.NodeManager {
	return &TunnelNodeManagerAdapter{nodeManager: nm}
}

func (a *TunnelNodeManagerAdapter) GetAllNodes() []*tunnel.Node {
	networkNodes := a.nodeManager.GetAllNodes()
	tunnelNodes := make([]*tunnel.Node, len(networkNodes))
	
	for i, nn := range networkNodes {
		var publicKeyBytes []byte
		if pubKey, exists := nn.Metadata["wireguard_public_key"]; exists {
			publicKeyBytes = []byte(pubKey)
		}
		
		tunnelNodes[i] = &tunnel.Node{
			ID:        nn.ID,
			Name:      nn.Name,
			VirtualIP: nn.VirtualIP,
			PublicIP:  nn.PublicIP,
			Port:      nn.Port,
			IsOnline:  nn.IsOnline,
			PublicKey: publicKeyBytes,
		}
	}
	
	return tunnelNodes
}

func (a *TunnelNodeManagerAdapter) GetLocalNode() *tunnel.Node {
	localNode := a.nodeManager.GetLocalNode()
	if localNode == nil {
		return nil
	}
	
	var publicKeyBytes []byte
	if pubKey, exists := localNode.Metadata["wireguard_public_key"]; exists {
		publicKeyBytes = []byte(pubKey)
	}
	
	return &tunnel.Node{
		ID:        localNode.ID,
		Name:      localNode.Name,
		VirtualIP: localNode.VirtualIP,
		PublicIP:  localNode.PublicIP,
		Port:      localNode.Port,
		IsOnline:  localNode.IsOnline,
		PublicKey: publicKeyBytes,
	}
}

func (a *TunnelNodeManagerAdapter) GetOnlinePeers() []*tunnel.Node {
	networkNodes := a.nodeManager.GetOnlinePeers()
	tunnelNodes := make([]*tunnel.Node, len(networkNodes))
	
	for i, nn := range networkNodes {
		var publicKeyBytes []byte
		if pubKey, exists := nn.Metadata["wireguard_public_key"]; exists {
			publicKeyBytes = []byte(pubKey)
		}
		
		tunnelNodes[i] = &tunnel.Node{
			ID:        nn.ID,
			Name:      nn.Name,
			VirtualIP: nn.VirtualIP,
			PublicIP:  nn.PublicIP,
			Port:      nn.Port,
			IsOnline:  nn.IsOnline,
			PublicKey: publicKeyBytes,
		}
	}
	
	return tunnelNodes
}