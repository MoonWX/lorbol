package latency

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/MoonWX/lorbol/internal/config"
)

type Measurer interface {
	Measure(target string) (time.Duration, error)
	MeasureBatch(targets []string) (map[string]time.Duration, error)
	MeasureWithContext(ctx context.Context, target string) (time.Duration, error)
	Start(ctx context.Context) error
	Stop() error
	GetLatestMeasurements() map[string]time.Duration
}

type measurer struct {
	config       config.LatencyConfig
	impl         MeasurementImpl
	cache        map[string]time.Duration
	cacheMu      sync.RWMutex
	stopCh       chan struct{}
	isRunning    bool
	mu           sync.Mutex
	nodeManager  NodeManager
	networkNodes map[string]string // nodeID -> endpoint
	nodesMu      sync.RWMutex
}

type NodeManager interface {
	GetAllNodes() []*Node
	GetOnlinePeers() []*Node
}

type Node struct {
	ID        string
	Name      string
	VirtualIP net.IP
	PublicIP  net.IP
	Port      int
	IsOnline  bool
}

type MeasurementImpl interface {
	Ping(target string, timeout time.Duration) (time.Duration, error)
}

func NewMeasurer(cfg config.LatencyConfig) Measurer {
	var impl MeasurementImpl

	switch cfg.Method {
	case "icmp":
		impl = &ICMPPinger{}
	case "tcp":
		impl = &TCPPinger{}
	default:
		impl = &ICMPPinger{}
	}

	return &measurer{
		config:       cfg,
		impl:         impl,
		cache:        make(map[string]time.Duration),
		stopCh:       make(chan struct{}),
		networkNodes: make(map[string]string),
	}
}

func NewNetworkMeasurer(cfg config.LatencyConfig, nodeManager NodeManager) Measurer {
	m := NewMeasurer(cfg).(*measurer)
	m.nodeManager = nodeManager
	return m
}

func (m *measurer) Measure(target string) (time.Duration, error) {
	return m.MeasureWithContext(context.Background(), target)
}

func (m *measurer) MeasureWithContext(ctx context.Context, target string) (time.Duration, error) {
	var totalLatency time.Duration
	var successCount int

	for i := 0; i < m.config.SampleSize; i++ {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}

		latency, err := m.impl.Ping(target, m.config.Timeout)
		if err == nil {
			totalLatency += latency
			successCount++
		}

		if i < m.config.SampleSize-1 {
			time.Sleep(100 * time.Millisecond)
		}
	}

	if successCount == 0 {
		return 0, fmt.Errorf("all ping attempts failed for target %s", target)
	}

	avgLatency := totalLatency / time.Duration(successCount)

	m.cacheMu.Lock()
	m.cache[target] = avgLatency
	m.cacheMu.Unlock()

	return avgLatency, nil
}

func (m *measurer) MeasureBatch(targets []string) (map[string]time.Duration, error) {
	results := make(map[string]time.Duration)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var lastErr error

	for _, target := range targets {
		wg.Add(1)
		go func(t string) {
			defer wg.Done()

			latency, err := m.Measure(t)

			mu.Lock()
			if err != nil {
				lastErr = err
			} else {
				results[t] = latency
			}
			mu.Unlock()
		}(target)
	}

	wg.Wait()

	if len(results) == 0 && lastErr != nil {
		return nil, lastErr
	}

	return results, nil
}

func (m *measurer) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isRunning {
		return fmt.Errorf("measurer is already running")
	}

	m.isRunning = true
	m.stopCh = make(chan struct{})

	go m.measurementLoop(ctx)

	return nil
}

func (m *measurer) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isRunning {
		return nil
	}

	close(m.stopCh)
	m.isRunning = false

	return nil
}

func (m *measurer) GetLatestMeasurements() map[string]time.Duration {
	m.cacheMu.RLock()
	defer m.cacheMu.RUnlock()

	result := make(map[string]time.Duration, len(m.cache))
	for k, v := range m.cache {
		result[k] = v
	}

	return result
}

func (m *measurer) measurementLoop(ctx context.Context) {
	ticker := time.NewTicker(m.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case <-ticker.C:
			targets := m.getAllTargets()
			if len(targets) > 0 {
				m.MeasureBatch(targets)
			}
		}
	}
}

func (m *measurer) getAllTargets() []string {
	var targets []string

	// Add configured static endpoints
	targets = append(targets, m.config.TargetEndpoints...)

	// Add network nodes if node manager is available
	if m.nodeManager != nil {
		nodes := m.nodeManager.GetOnlinePeers()
		for _, node := range nodes {
			// Use virtual IP for internal network measurements
			if node.VirtualIP != nil {
				targets = append(targets, node.VirtualIP.String())
			}
			// Also measure to public IP for external connectivity
			if node.PublicIP != nil {
				targets = append(targets, node.PublicIP.String())
			}
		}
	}

	// Add manually tracked network nodes
	m.nodesMu.RLock()
	for _, endpoint := range m.networkNodes {
		targets = append(targets, endpoint)
	}
	m.nodesMu.RUnlock()

	return targets
}

func (m *measurer) AddNetworkNode(nodeID, endpoint string) {
	m.nodesMu.Lock()
	defer m.nodesMu.Unlock()
	m.networkNodes[nodeID] = endpoint
}

func (m *measurer) RemoveNetworkNode(nodeID string) {
	m.nodesMu.Lock()
	defer m.nodesMu.Unlock()
	delete(m.networkNodes, nodeID)
}

func (m *measurer) GetNetworkLatencies() map[string]time.Duration {
	m.cacheMu.RLock()
	defer m.cacheMu.RUnlock()

	networkLatencies := make(map[string]time.Duration)

	// Filter out only network node latencies
	if m.nodeManager != nil {
		nodes := m.nodeManager.GetAllNodes()
		for _, node := range nodes {
			if node.VirtualIP != nil {
				if latency, exists := m.cache[node.VirtualIP.String()]; exists {
					networkLatencies[node.ID] = latency
				}
			}
		}
	}

	return networkLatencies
}
