package routing

import (
	"container/heap"
	"fmt"
	"math"
	"time"
)

type Graph struct {
	nodes map[string]*GraphNode
	edges map[string]map[string]*Edge
}

type GraphNode struct {
	ID       string
	VirtualIP string
	PublicIP  string
}

type Edge struct {
	From     string
	To       string
	Latency  time.Duration
	Weight   float64
	LastUpdated time.Time
}

type PathResult struct {
	Path     []string
	TotalLatency time.Duration
	HopCount int
}

type priorityQueueItem struct {
	nodeID   string
	distance float64
	index    int
}

type PriorityQueue []*priorityQueueItem

func (pq PriorityQueue) Len() int { return len(pq) }

func (pq PriorityQueue) Less(i, j int) bool {
	return pq[i].distance < pq[j].distance
}

func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *PriorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*priorityQueueItem)
	item.index = n
	*pq = append(*pq, item)
}

func (pq *PriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*pq = old[0 : n-1]
	return item
}

func NewGraph() *Graph {
	return &Graph{
		nodes: make(map[string]*GraphNode),
		edges: make(map[string]map[string]*Edge),
	}
}

func (g *Graph) AddNode(id, virtualIP, publicIP string) {
	g.nodes[id] = &GraphNode{
		ID:        id,
		VirtualIP: virtualIP,
		PublicIP:  publicIP,
	}
	if g.edges[id] == nil {
		g.edges[id] = make(map[string]*Edge)
	}
}

func (g *Graph) RemoveNode(id string) {
	delete(g.nodes, id)
	delete(g.edges, id)
	
	// Remove edges pointing to this node
	for fromID := range g.edges {
		delete(g.edges[fromID], id)
	}
}

func (g *Graph) AddEdge(from, to string, latency time.Duration) {
	if g.edges[from] == nil {
		g.edges[from] = make(map[string]*Edge)
	}
	
	weight := float64(latency.Nanoseconds())
	edge := &Edge{
		From:        from,
		To:          to,
		Latency:     latency,
		Weight:      weight,
		LastUpdated: time.Now(),
	}
	
	g.edges[from][to] = edge
}

func (g *Graph) UpdateEdge(from, to string, latency time.Duration) {
	if edges, exists := g.edges[from]; exists {
		if edge, exists := edges[to]; exists {
			edge.Latency = latency
			edge.Weight = float64(latency.Nanoseconds())
			edge.LastUpdated = time.Now()
		} else {
			g.AddEdge(from, to, latency)
		}
	} else {
		g.AddEdge(from, to, latency)
	}
}

func (g *Graph) GetEdge(from, to string) (*Edge, bool) {
	if edges, exists := g.edges[from]; exists {
		edge, exists := edges[to]
		return edge, exists
	}
	return nil, false
}

func (g *Graph) GetNeighbors(nodeID string) []string {
	var neighbors []string
	if edges, exists := g.edges[nodeID]; exists {
		for neighborID := range edges {
			neighbors = append(neighbors, neighborID)
		}
	}
	return neighbors
}

func (g *Graph) FindShortestPath(from, to string) (*PathResult, error) {
	if _, exists := g.nodes[from]; !exists {
		return nil, fmt.Errorf("source node %s not found", from)
	}
	if _, exists := g.nodes[to]; !exists {
		return nil, fmt.Errorf("destination node %s not found", to)
	}
	
	if from == to {
		return &PathResult{
			Path:         []string{from},
			TotalLatency: 0,
			HopCount:     0,
		}, nil
	}

	return g.dijkstra(from, to)
}

func (g *Graph) dijkstra(start, end string) (*PathResult, error) {
	dist := make(map[string]float64)
	prev := make(map[string]string)
	visited := make(map[string]bool)
	
	// Initialize distances
	for nodeID := range g.nodes {
		dist[nodeID] = math.Inf(1)
	}
	dist[start] = 0
	
	pq := &PriorityQueue{}
	heap.Init(pq)
	heap.Push(pq, &priorityQueueItem{nodeID: start, distance: 0})
	
	for pq.Len() > 0 {
		current := heap.Pop(pq).(*priorityQueueItem)
		currentNode := current.nodeID
		
		if visited[currentNode] {
			continue
		}
		visited[currentNode] = true
		
		if currentNode == end {
			break
		}
		
		// Check all neighbors
		if edges, exists := g.edges[currentNode]; exists {
			for neighborID, edge := range edges {
				if visited[neighborID] {
					continue
				}
				
				newDist := dist[currentNode] + edge.Weight
				if newDist < dist[neighborID] {
					dist[neighborID] = newDist
					prev[neighborID] = currentNode
					heap.Push(pq, &priorityQueueItem{
						nodeID:   neighborID,
						distance: newDist,
					})
				}
			}
		}
	}
	
	// Check if path exists
	if dist[end] == math.Inf(1) {
		return nil, fmt.Errorf("no path found from %s to %s", start, end)
	}
	
	// Reconstruct path
	path := []string{}
	current := end
	for current != "" {
		path = append([]string{current}, path...)
		current = prev[current]
	}
	
	// Calculate total latency
	totalLatency := time.Duration(0)
	for i := 0; i < len(path)-1; i++ {
		if edge, exists := g.GetEdge(path[i], path[i+1]); exists {
			totalLatency += edge.Latency
		}
	}
	
	return &PathResult{
		Path:         path,
		TotalLatency: totalLatency,
		HopCount:     len(path) - 1,
	}, nil
}

func (g *Graph) FindAllShortestPaths(from string) (map[string]*PathResult, error) {
	if _, exists := g.nodes[from]; !exists {
		return nil, fmt.Errorf("source node %s not found", from)
	}
	
	results := make(map[string]*PathResult)
	
	for nodeID := range g.nodes {
		if nodeID != from {
			if path, err := g.FindShortestPath(from, nodeID); err == nil {
				results[nodeID] = path
			}
		}
	}
	
	return results, nil
}

func (g *Graph) GetGraphStats() map[string]interface{} {
	nodeCount := len(g.nodes)
	edgeCount := 0
	
	for _, edges := range g.edges {
		edgeCount += len(edges)
	}
	
	return map[string]interface{}{
		"node_count": nodeCount,
		"edge_count": edgeCount,
		"density":    float64(edgeCount) / float64(nodeCount*(nodeCount-1)),
	}
}

func (g *Graph) CleanupStaleEdges(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)
	
	for _, edges := range g.edges {
		for toID, edge := range edges {
			if edge.LastUpdated.Before(cutoff) {
				delete(edges, toID)
			}
		}
	}
}