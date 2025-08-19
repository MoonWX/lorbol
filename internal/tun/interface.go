package tun

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/unix"
)

// TunInterface manages a TUN interface directly without external dependencies
type TunInterface struct {
	name string
	fd   int
	file *os.File
	mtu  int
}

type TunPacket struct {
	Data []byte
	DstIP net.IP
	SrcIP net.IP
}

func NewTunInterface(name string, ip net.IP, cidr string, mtu int) (*TunInterface, error) {
	// Create TUN device
	fd, err := createTunDevice(name)
	if err != nil {
		return nil, fmt.Errorf("failed to create TUN device: %w", err)
	}

	file := os.NewFile(uintptr(fd), name)
	
	tun := &TunInterface{
		name: name,
		fd:   fd,
		file: file,
		mtu:  mtu,
	}

	// Configure the interface
	if err := tun.configure(ip, cidr); err != nil {
		tun.Close()
		return nil, fmt.Errorf("failed to configure interface: %w", err)
	}

	return tun, nil
}

func (tun *TunInterface) Read() (*TunPacket, error) {
	buffer := make([]byte, tun.mtu)
	n, err := tun.file.Read(buffer)
	if err != nil {
		return nil, err
	}

	packet := buffer[:n]
	if len(packet) < 20 {
		return nil, fmt.Errorf("packet too short")
	}

	// Parse IP header
	dstIP := net.IP(packet[16:20])
	srcIP := net.IP(packet[12:16])

	return &TunPacket{
		Data:  packet,
		DstIP: dstIP,
		SrcIP: srcIP,
	}, nil
}

func (tun *TunInterface) Write(packet []byte) error {
	_, err := tun.file.Write(packet)
	return err
}

func (tun *TunInterface) Close() error {
	if tun.file != nil {
		return tun.file.Close()
	}
	return nil
}

func (tun *TunInterface) GetName() string {
	return tun.name
}

func (tun *TunInterface) configure(ip net.IP, cidr string) error {
	// Set IP address
	cmd := exec.Command("ip", "addr", "add", cidr, "dev", tun.name)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to set IP address: %w", err)
	}

	// Set MTU
	cmd = exec.Command("ip", "link", "set", "dev", tun.name, "mtu", fmt.Sprintf("%d", tun.mtu))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to set MTU: %w", err)
	}

	// Bring interface up
	cmd = exec.Command("ip", "link", "set", "up", "dev", tun.name)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to bring interface up: %w", err)
	}

	return nil
}

func (tun *TunInterface) AddRoute(destination, gateway string) error {
	cmd := exec.Command("ip", "route", "add", destination, "via", gateway, "dev", tun.name)
	return cmd.Run()
}

func (tun *TunInterface) RemoveRoute(destination string) error {
	cmd := exec.Command("ip", "route", "del", destination, "dev", tun.name)
	return cmd.Run()
}

// Platform-specific TUN device creation
func createTunDevice(name string) (int, error) {
	// Open the TUN clone device
	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR, 0)
	if err != nil {
		return -1, fmt.Errorf("failed to open /dev/net/tun: %w", err)
	}

	// Configure the TUN interface
	var ifr struct {
		name  [16]byte
		flags uint16
		_     [22]byte // padding
	}

	copy(ifr.name[:], name)
	ifr.flags = unix.IFF_TUN | unix.IFF_NO_PI

	// Set interface parameters using ioctl
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), 
		uintptr(unix.TUNSETIFF), uintptr(unsafe.Pointer(&ifr)))
	if errno != 0 {
		unix.Close(fd)
		return -1, fmt.Errorf("ioctl TUNSETIFF failed: %v", errno)
	}

	return fd, nil
}

// TunManager manages the TUN interface and packet routing
type TunManager struct {
	tun          *TunInterface
	tunnelManager TunnelManager
	localNetwork *net.IPNet
	stopCh       chan struct{}
	isRunning    bool
}

type TunnelManager interface {
	SendData(dstIP net.IP, data []byte) error
	GetActivePeers() []*TunnelPeer
}

type TunnelPeer struct {
	ID        string
	VirtualIP net.IP
	IsActive  bool
}

func NewTunManager(interfaceName, localIP, cidr string, mtu int, tunnelManager TunnelManager) (*TunManager, error) {
	ip := net.ParseIP(localIP)
	if ip == nil {
		return nil, fmt.Errorf("invalid local IP: %s", localIP)
	}

	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR: %w", err)
	}

	tun, err := NewTunInterface(interfaceName, ip, fmt.Sprintf("%s/%s", localIP, cidr[len(cidr)-2:]), mtu)
	if err != nil {
		return nil, err
	}

	return &TunManager{
		tun:           tun,
		tunnelManager: tunnelManager,
		localNetwork:  network,
		stopCh:        make(chan struct{}),
	}, nil
}

func (tm *TunManager) Start() error {
	if tm.isRunning {
		return fmt.Errorf("TUN manager is already running")
	}

	tm.isRunning = true
	go tm.packetLoop()

	return nil
}

func (tm *TunManager) Stop() error {
	if !tm.isRunning {
		return nil
	}

	close(tm.stopCh)
	tm.isRunning = false
	
	return tm.tun.Close()
}

func (tm *TunManager) packetLoop() {
	for {
		select {
		case <-tm.stopCh:
			return
		default:
		}

		packet, err := tm.tun.Read()
		if err != nil {
			continue
		}

		// Check if destination is in our virtual network
		if tm.localNetwork.Contains(packet.DstIP) {
			// Route through tunnel
			if err := tm.tunnelManager.SendData(packet.DstIP, packet.Data); err != nil {
				// Failed to send through tunnel, drop packet
				continue
			}
		} else {
			// Route to external network (would need additional routing logic)
			// For now, just drop non-local packets
		}
	}
}

func (tm *TunManager) InjectPacket(data []byte) error {
	return tm.tun.Write(data)
}

func (tm *TunManager) AddPeerRoute(peerIP net.IP) error {
	return tm.tun.AddRoute(peerIP.String()+"/32", "0.0.0.0")
}

func (tm *TunManager) RemovePeerRoute(peerIP net.IP) error {
	return tm.tun.RemoveRoute(peerIP.String() + "/32")
}