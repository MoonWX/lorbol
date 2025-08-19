package latency

import (
	"net"
	"time"
)

type UDPMeasurer struct {
	interval time.Duration
	timeout  time.Duration
}

func NewUDPMeasurer(interval, timeout time.Duration) *UDPMeasurer {
	return &UDPMeasurer{
		interval: interval,
		timeout:  timeout,
	}
}

func (u *UDPMeasurer) MeasureLatency(targetIP net.IP, endpoint string) (time.Duration, error) {
	start := time.Now()
	
	// Create UDP connection
	conn, err := net.DialTimeout("udp4", endpoint, u.timeout)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	
	// Send a simple ping packet
	_, err = conn.Write([]byte("ping"))
	if err != nil {
		return 0, err
	}
	
	// Measure connection establishment time only
	// Don't wait for response since there's no ping-pong protocol implemented
	return time.Since(start), nil
}