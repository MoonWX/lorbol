package tunnel

import (
	"crypto/rand"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/curve25519"
)

type WireGuardTunnel struct {
	interfaceName string
	privateKey    []byte
	publicKey     []byte
	localIP       net.IP
	listenPort    int
	peers         map[string]*WireGuardPeer
	mu            sync.RWMutex
}

type WireGuardPeer struct {
	PublicKey    []byte
	Endpoint     string
	AllowedIPs   []net.IPNet
	LastHandshake time.Time
	RxBytes      uint64
	TxBytes      uint64
}

func NewWireGuardTunnel(interfaceName string, localIP net.IP, listenPort int) (*WireGuardTunnel, error) {
	// Generate private key
	privateKey := make([]byte, 32)
	if _, err := rand.Read(privateKey); err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	// Calculate public key
	publicKey, err := curve25519.X25519(privateKey, curve25519.Basepoint)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate public key: %w", err)
	}

	return &WireGuardTunnel{
		interfaceName: interfaceName,
		privateKey:    privateKey,
		publicKey:     publicKey,
		localIP:       localIP,
		listenPort:    listenPort,
		peers:         make(map[string]*WireGuardPeer),
	}, nil
}

func (wg *WireGuardTunnel) GetPublicKey() []byte {
	return wg.publicKey
}

func (wg *WireGuardTunnel) SetupInterface() error {
	// Create WireGuard interface
	cmd := exec.Command("ip", "link", "add", "dev", wg.interfaceName, "type", "wireguard")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create interface: %w", err)
	}

	// Set IP address
	cidr := fmt.Sprintf("%s/24", wg.localIP.String())
	cmd = exec.Command("ip", "addr", "add", cidr, "dev", wg.interfaceName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to set IP address: %w", err)
	}

	// Set private key
	cmd = exec.Command("wg", "set", wg.interfaceName, "private-key", "/dev/stdin")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("%x", wg.privateKey))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to set private key: %w", err)
	}

	// Set listen port
	cmd = exec.Command("wg", "set", wg.interfaceName, "listen-port", fmt.Sprintf("%d", wg.listenPort))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to set listen port: %w", err)
	}

	// Bring interface up
	cmd = exec.Command("ip", "link", "set", "up", "dev", wg.interfaceName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to bring interface up: %w", err)
	}

	return nil
}

func (wg *WireGuardTunnel) AddPeer(peerID string, publicKey []byte, endpoint string, allowedIPs []net.IPNet) error {
	wg.mu.Lock()
	defer wg.mu.Unlock()

	peer := &WireGuardPeer{
		PublicKey:  publicKey,
		Endpoint:   endpoint,
		AllowedIPs: allowedIPs,
	}

	wg.peers[peerID] = peer

	// Configure peer in WireGuard
	pubKeyStr := fmt.Sprintf("%x", publicKey)
	
	// Add peer
	cmd := exec.Command("wg", "set", wg.interfaceName, "peer", pubKeyStr, "endpoint", endpoint)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to add peer: %w", err)
	}

	// Set allowed IPs
	var allowedIPStrs []string
	for _, ip := range allowedIPs {
		allowedIPStrs = append(allowedIPStrs, ip.String())
	}
	
	if len(allowedIPStrs) > 0 {
		cmd = exec.Command("wg", "set", wg.interfaceName, "peer", pubKeyStr, "allowed-ips", strings.Join(allowedIPStrs, ","))
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to set allowed IPs: %w", err)
		}
	}

	// Set persistent keepalive
	cmd = exec.Command("wg", "set", wg.interfaceName, "peer", pubKeyStr, "persistent-keepalive", "25")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to set keepalive: %w", err)
	}

	return nil
}

func (wg *WireGuardTunnel) RemovePeer(peerID string) error {
	wg.mu.Lock()
	defer wg.mu.Unlock()

	peer, exists := wg.peers[peerID]
	if !exists {
		return fmt.Errorf("peer %s not found", peerID)
	}

	pubKeyStr := fmt.Sprintf("%x", peer.PublicKey)
	cmd := exec.Command("wg", "set", wg.interfaceName, "peer", pubKeyStr, "remove")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to remove peer: %w", err)
	}

	delete(wg.peers, peerID)
	return nil
}

func (wg *WireGuardTunnel) UpdatePeerEndpoint(peerID, newEndpoint string) error {
	wg.mu.Lock()
	defer wg.mu.Unlock()

	peer, exists := wg.peers[peerID]
	if !exists {
		return fmt.Errorf("peer %s not found", peerID)
	}

	peer.Endpoint = newEndpoint
	pubKeyStr := fmt.Sprintf("%x", peer.PublicKey)
	
	cmd := exec.Command("wg", "set", wg.interfaceName, "peer", pubKeyStr, "endpoint", newEndpoint)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to update peer endpoint: %w", err)
	}

	return nil
}

func (wg *WireGuardTunnel) GetPeerStats() (map[string]*WireGuardPeer, error) {
	cmd := exec.Command("wg", "show", wg.interfaceName, "dump")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get peer stats: %w", err)
	}

	// Parse WireGuard dump output
	lines := strings.Split(string(output), "\n")
	stats := make(map[string]*WireGuardPeer)

	for _, line := range lines {
		if line == "" {
			continue
		}
		
		fields := strings.Split(line, "\t")
		if len(fields) < 6 {
			continue
		}

		// Find peer by public key
		pubKey := fields[0]
		for peerID, peer := range wg.peers {
			if fmt.Sprintf("%x", peer.PublicKey) == pubKey {
				stats[peerID] = &WireGuardPeer{
					PublicKey: peer.PublicKey,
					Endpoint:  fields[2],
					// Parse other fields as needed
				}
				break
			}
		}
	}

	return stats, nil
}

func (wg *WireGuardTunnel) Close() error {
	// Remove interface
	cmd := exec.Command("ip", "link", "del", wg.interfaceName)
	return cmd.Run()
}

func (wg *WireGuardTunnel) AddRoute(destination, gateway string) error {
	cmd := exec.Command("ip", "route", "add", destination, "via", gateway, "dev", wg.interfaceName)
	return cmd.Run()
}

func (wg *WireGuardTunnel) RemoveRoute(destination string) error {
	cmd := exec.Command("ip", "route", "del", destination, "dev", wg.interfaceName)
	return cmd.Run()
}