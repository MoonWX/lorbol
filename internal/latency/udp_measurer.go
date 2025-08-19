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
	conn, err := net.DialTimeout("udp", endpoint, u.timeout)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	
	// Send a simple ping packet
	_, err = conn.Write([]byte("ping"))
	if err != nil {
		return 0, err
	}
	
	// Try to read response (this may fail for most cases, but we measure connection time)
	conn.SetReadDeadline(time.Now().Add(u.timeout))
	buffer := make([]byte, 4)
	conn.Read(buffer) // Ignore errors, we're just measuring connection latency
	
	return time.Since(start), nil
}