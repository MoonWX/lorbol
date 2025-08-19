package routing

import (
	"sync"
	"time"
)

type RouteTable struct {
	routes map[string]Route
	mu     sync.RWMutex
}

func NewRouteTable() *RouteTable {
	return &RouteTable{
		routes: make(map[string]Route),
	}
}

func (rt *RouteTable) AddRoute(route Route) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.routes[route.Destination] = route
}

func (rt *RouteTable) GetRoute(destination string) (Route, bool) {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	route, exists := rt.routes[destination]
	return route, exists
}

func (rt *RouteTable) RemoveRoute(destination string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	delete(rt.routes, destination)
}

func (rt *RouteTable) GetAllRoutes() map[string]Route {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	
	result := make(map[string]Route, len(rt.routes))
	for k, v := range rt.routes {
		result[k] = v
	}
	return result
}

func (rt *RouteTable) UpdateRoute(destination string, latency time.Duration) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	
	if route, exists := rt.routes[destination]; exists {
		route.Latency = latency
		route.Timestamp = time.Now()
		rt.routes[destination] = route
		return true
	}
	return false
}

func (rt *RouteTable) Clear() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.routes = make(map[string]Route)
}

func (rt *RouteTable) Size() int {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return len(rt.routes)
}