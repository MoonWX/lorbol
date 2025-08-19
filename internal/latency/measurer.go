package latency

import (
	"context"
	"fmt"
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
	config    config.LatencyConfig
	impl      MeasurementImpl
	cache     map[string]time.Duration
	cacheMu   sync.RWMutex
	stopCh    chan struct{}
	isRunning bool
	mu        sync.Mutex
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
		config: cfg,
		impl:   impl,
		cache:  make(map[string]time.Duration),
		stopCh: make(chan struct{}),
	}
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
			m.MeasureBatch(m.config.TargetEndpoints)
		}
	}
}