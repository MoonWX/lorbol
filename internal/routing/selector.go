package routing

import (
	"fmt"
	"sort"
	"time"
)

type Selector struct {
	routeTable *RouteTable
}

type RouteScore struct {
	Route Route
	Score float64
}

func NewSelector(routeTable *RouteTable) *Selector {
	return &Selector{
		routeTable: routeTable,
	}
}

func (s *Selector) SelectBestRoute(destination string) (Route, error) {
	route, exists := s.routeTable.GetRoute(destination)
	if !exists {
		return Route{}, fmt.Errorf("no route found for destination %s", destination)
	}

	if time.Since(route.Timestamp) > 5*time.Minute {
		return Route{}, fmt.Errorf("route for %s is stale", destination)
	}

	return route, nil
}

func (s *Selector) SelectAlternativeRoutes(destination string, count int) ([]Route, error) {
	allRoutes := s.routeTable.GetAllRoutes()
	
	var candidates []RouteScore
	for _, route := range allRoutes {
		if route.Destination == destination {
			continue
		}
		
		score := s.calculateRouteScore(route)
		candidates = append(candidates, RouteScore{
			Route: route,
			Score: score,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	if count > len(candidates) {
		count = len(candidates)
	}

	routes := make([]Route, count)
	for i := 0; i < count; i++ {
		routes[i] = candidates[i].Route
	}

	return routes, nil
}

func (s *Selector) calculateRouteScore(route Route) float64 {
	latencyScore := 1000.0 / float64(route.Latency.Milliseconds())
	
	ageScore := 1.0
	age := time.Since(route.Timestamp)
	if age > time.Minute {
		ageScore = 1.0 / (1.0 + float64(age.Minutes()))
	}

	return latencyScore * ageScore
}