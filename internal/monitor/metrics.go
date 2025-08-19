package monitor

import (
	"sync"
	"time"
)

type Metrics struct {
	LatencyMeasurements map[string]LatencyMetric
	RouteOptimizations  map[string]RouteMetric
	VPNConnections      map[string]ConnectionMetric
	SystemMetrics       SystemMetric
	mu                  sync.RWMutex
}

type LatencyMetric struct {
	Target        string
	CurrentLatency time.Duration
	MinLatency    time.Duration
	MaxLatency    time.Duration
	AvgLatency    time.Duration
	SampleCount   int64
	LastMeasured  time.Time
}

type RouteMetric struct {
	Destination   string
	Gateway       string
	Latency       time.Duration
	OptimizedAt   time.Time
	SwitchCount   int64
	SuccessRate   float64
}

type ConnectionMetric struct {
	Endpoint        string
	IsConnected     bool
	BytesSent       int64
	BytesReceived   int64
	PacketsSent     int64
	PacketsReceived int64
	ConnectedAt     time.Time
	LastActivity    time.Time
	ReconnectCount  int64
}

type SystemMetric struct {
	CPUUsage    float64
	MemoryUsage float64
	Uptime      time.Duration
	LastUpdated time.Time
}

func NewMetrics() *Metrics {
	return &Metrics{
		LatencyMeasurements: make(map[string]LatencyMetric),
		RouteOptimizations:  make(map[string]RouteMetric),
		VPNConnections:      make(map[string]ConnectionMetric),
	}
}

func (m *Metrics) UpdateLatency(target string, latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	metric, exists := m.LatencyMeasurements[target]
	if !exists {
		metric = LatencyMetric{
			Target:       target,
			MinLatency:   latency,
			MaxLatency:   latency,
			AvgLatency:   latency,
			SampleCount:  1,
			LastMeasured: time.Now(),
		}
	} else {
		if latency < metric.MinLatency {
			metric.MinLatency = latency
		}
		if latency > metric.MaxLatency {
			metric.MaxLatency = latency
		}
		
		totalLatency := time.Duration(float64(metric.AvgLatency) * float64(metric.SampleCount))
		metric.SampleCount++
		metric.AvgLatency = time.Duration(float64(totalLatency+latency) / float64(metric.SampleCount))
		metric.LastMeasured = time.Now()
	}

	metric.CurrentLatency = latency
	m.LatencyMeasurements[target] = metric
}

func (m *Metrics) UpdateRoute(destination, gateway string, latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	metric, exists := m.RouteOptimizations[destination]
	if !exists {
		metric = RouteMetric{
			Destination: destination,
			Gateway:     gateway,
			Latency:     latency,
			OptimizedAt: time.Now(),
			SwitchCount: 1,
			SuccessRate: 1.0,
		}
	} else {
		if metric.Gateway != gateway {
			metric.SwitchCount++
		}
		metric.Gateway = gateway
		metric.Latency = latency
		metric.OptimizedAt = time.Now()
	}

	m.RouteOptimizations[destination] = metric
}

func (m *Metrics) UpdateConnection(endpoint string, isConnected bool, bytesSent, bytesReceived int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	metric, exists := m.VPNConnections[endpoint]
	if !exists {
		metric = ConnectionMetric{
			Endpoint:     endpoint,
			IsConnected:  isConnected,
			ConnectedAt:  time.Now(),
			LastActivity: time.Now(),
		}
	}

	metric.IsConnected = isConnected
	metric.BytesSent += bytesSent
	metric.BytesReceived += bytesReceived
	if bytesSent > 0 || bytesReceived > 0 {
		metric.PacketsSent++
		metric.LastActivity = time.Now()
	}

	m.VPNConnections[endpoint] = metric
}

func (m *Metrics) GetLatencyMetrics() map[string]LatencyMetric {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]LatencyMetric, len(m.LatencyMeasurements))
	for k, v := range m.LatencyMeasurements {
		result[k] = v
	}
	return result
}

func (m *Metrics) GetRouteMetrics() map[string]RouteMetric {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]RouteMetric, len(m.RouteOptimizations))
	for k, v := range m.RouteOptimizations {
		result[k] = v
	}
	return result
}

func (m *Metrics) GetConnectionMetrics() map[string]ConnectionMetric {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]ConnectionMetric, len(m.VPNConnections))
	for k, v := range m.VPNConnections {
		result[k] = v
	}
	return result
}

func (m *Metrics) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.LatencyMeasurements = make(map[string]LatencyMetric)
	m.RouteOptimizations = make(map[string]RouteMetric)
	m.VPNConnections = make(map[string]ConnectionMetric)
}