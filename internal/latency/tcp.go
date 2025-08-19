package latency

import (
	"fmt"
	"net"
	"time"
)

type TCPPinger struct{}

func (p *TCPPinger) Ping(target string, timeout time.Duration) (time.Duration, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		host = target
		port = "80"
	}

	start := time.Now()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), timeout)
	if err != nil {
		return 0, fmt.Errorf("failed to connect to %s: %w", target, err)
	}
	duration := time.Since(start)
	
	conn.Close()
	
	return duration, nil
}