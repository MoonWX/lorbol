package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

type Node struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	VirtualIP string `json:"virtual_ip"`
	PublicIP  string `json:"public_ip"`
	Port      int    `json:"port"`
	Network   string `json:"network"`
	Timestamp int64  `json:"timestamp"`
}

type DiscoveryServer struct {
	nodes map[string]*Node // network -> nodes
	mu    sync.RWMutex
}

func NewDiscoveryServer() *DiscoveryServer {
	return &DiscoveryServer{
		nodes: make(map[string]*Node),
	}
}

func (ds *DiscoveryServer) registerNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST allowed", http.StatusMethodNotAllowed)
		return
	}

	var node Node
	if err := json.NewDecoder(r.Body).Decode(&node); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Set timestamp
	node.Timestamp = time.Now().Unix()

	// Create unique key: network:id
	key := fmt.Sprintf("%s:%s", node.Network, node.ID)

	ds.mu.Lock()
	ds.nodes[key] = &node
	ds.mu.Unlock()

	log.Printf("Registered node: %s (%s) in network %s", node.Name, node.PublicIP, node.Network)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (ds *DiscoveryServer) discoverNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Only GET allowed", http.StatusMethodNotAllowed)
		return
	}

	network := r.URL.Query().Get("network")
	if network == "" {
		http.Error(w, "network parameter required", http.StatusBadRequest)
		return
	}

	ds.mu.RLock()
	var nodes []*Node
	cutoff := time.Now().Unix() - 300 // 5 minutes timeout

	for _, node := range ds.nodes {
		if node.Network == network && node.Timestamp > cutoff {
			nodes = append(nodes, node)
		}
	}
	ds.mu.RUnlock()

	log.Printf("Discovery request for network %s: found %d nodes", network, len(nodes))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"nodes": nodes,
		"count": len(nodes),
	})
}

func (ds *DiscoveryServer) healthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"time":   time.Now().Unix(),
	})
}

func (ds *DiscoveryServer) stats(w http.ResponseWriter, r *http.Request) {
	ds.mu.RLock()
	networks := make(map[string]int)
	totalNodes := 0
	
	for _, node := range ds.nodes {
		networks[node.Network]++
		totalNodes++
	}
	ds.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_nodes": totalNodes,
		"networks":    networks,
	})
}

func (ds *DiscoveryServer) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		cutoff := time.Now().Unix() - 600 // 10 minutes timeout
		
		ds.mu.Lock()
		for nodeKey, node := range ds.nodes {
			if node.Timestamp < cutoff {
				delete(ds.nodes, nodeKey)
				log.Printf("Cleaned up expired node: %s", node.Name)
			}
		}
		ds.mu.Unlock()
	}
}

func main() {
	port := ":8080"
	if len(os.Args) > 1 {
		port = ":" + os.Args[1]
	}

	server := NewDiscoveryServer()

	// Start cleanup routine
	go server.cleanup()

	// Setup routes
	http.HandleFunc("/register", server.registerNode)
	http.HandleFunc("/discover", server.discoverNodes)
	http.HandleFunc("/health", server.healthCheck)
	http.HandleFunc("/stats", server.stats)

	// Serve static page at root
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `
<!DOCTYPE html>
<html>
<head><title>LORBOL Discovery Server</title></head>
<body>
	<h1>LORBOL Discovery Server</h1>
	<p>Status: Running</p>
	<ul>
		<li><a href="/health">Health Check</a></li>
		<li><a href="/stats">Statistics</a></li>
	</ul>
	<h2>API Endpoints:</h2>
	<ul>
		<li>POST /register - Register a node</li>
		<li>GET /discover?network=NAME - Discover nodes</li>
	</ul>
</body>
</html>`)
	})

	log.Printf("LORBOL Discovery Server starting on port %s", port)
	log.Fatal(http.ListenAndServe(port, nil))
}