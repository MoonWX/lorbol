package latency

import (
	"fmt"
	"net"
	"os"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

type ICMPPinger struct{}

func (p *ICMPPinger) Ping(target string, timeout time.Duration) (time.Duration, error) {
	addr, err := net.ResolveIPAddr("ip4", target)
	if err != nil {
		return 0, fmt.Errorf("failed to resolve address %s: %w", target, err)
	}

	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return 0, fmt.Errorf("failed to create ICMP socket: %w", err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return 0, fmt.Errorf("failed to set deadline: %w", err)
	}

	message := &icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   os.Getpid() & 0xffff,
			Seq:  1,
			Data: []byte("LORBOL ping"),
		},
	}

	data, err := message.Marshal(nil)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal ICMP message: %w", err)
	}

	start := time.Now()
	_, err = conn.WriteTo(data, addr)
	if err != nil {
		return 0, fmt.Errorf("failed to send ICMP packet: %w", err)
	}

	reply := make([]byte, 1500)
	n, peer, err := conn.ReadFrom(reply)
	if err != nil {
		return 0, fmt.Errorf("failed to read ICMP reply: %w", err)
	}
	duration := time.Since(start)

	if peer.String() != addr.String() {
		return 0, fmt.Errorf("reply from unexpected peer %s", peer)
	}

	replyMsg, err := icmp.ParseMessage(ipv4.ICMPTypeEchoReply.Protocol(), reply[:n])
	if err != nil {
		return 0, fmt.Errorf("failed to parse ICMP reply: %w", err)
	}

	if replyMsg.Type != ipv4.ICMPTypeEchoReply {
		return 0, fmt.Errorf("unexpected ICMP message type: %v", replyMsg.Type)
	}

	return duration, nil
}