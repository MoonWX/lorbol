package utils

import (
	"crypto/rand"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

func GenerateID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)
	return fmt.Sprintf("%x", bytes)
}

func ValidateIPAddress(ip string) bool {
	return net.ParseIP(ip) != nil
}

func ValidatePort(port string) bool {
	p, err := strconv.Atoi(port)
	return err == nil && p > 0 && p <= 65535
}

func ParseEndpoint(endpoint string) (string, string, error) {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		return endpoint, "80", nil
	}
	return host, port, nil
}

func FormatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%.2fµs", float64(d.Nanoseconds())/1000)
	}
	if d < time.Second {
		return fmt.Sprintf("%.2fms", float64(d.Nanoseconds())/1000000)
	}
	if d < time.Minute {
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
	return d.String()
}

func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func IsPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() {
		return true
	}
	
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	}
	
	for _, cidr := range privateRanges {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}
	
	return false
}

func NormalizeEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if !strings.Contains(endpoint, ":") {
		return endpoint + ":80"
	}
	return endpoint
}

func CalculateAverage(values []time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}
	
	var total time.Duration
	for _, v := range values {
		total += v
	}
	
	return total / time.Duration(len(values))
}

func FilterExpiredEntries(entries map[string]time.Time, maxAge time.Duration) map[string]time.Time {
	filtered := make(map[string]time.Time)
	cutoff := time.Now().Add(-maxAge)
	
	for key, timestamp := range entries {
		if timestamp.After(cutoff) {
			filtered[key] = timestamp
		}
	}
	
	return filtered
}