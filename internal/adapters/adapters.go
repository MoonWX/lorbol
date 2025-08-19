package adapters

import (
	"github.com/MoonWX/lorbol/internal/latency"
	"github.com/MoonWX/lorbol/internal/network"
	"github.com/MoonWX/lorbol/internal/routing"
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