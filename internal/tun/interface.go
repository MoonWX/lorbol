package tun

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Interface manages a TUN interface directly without external dependencies
type Interface struct {
	name string
	fd   int
	file *os.File
	mtu  int
}

type Packet struct {
	Data  []byte
	DstIP net.IP
	SrcIP net.IP
}

func NewInterface(name string, ip net.IP, cidr string, mtu int) (*Interface, error) {
	// Create TUN device
	fd, err := createTunDevice(name)
	if err != nil {
		return nil, fmt.Errorf("failed to create TUN device: %w", err)
	}

	file := os.NewFile(uintptr(fd), name)

	tun := &Interface{
		name: name,
		fd:   fd,
		file: file,
		mtu:  mtu,
	}

	// Configure the interface
	if err := tun.configure(ip, cidr); err != nil {
		tun.Stop()
		return nil, fmt.Errorf("failed to configure interface: %w", err)
	}

	return tun, nil
}

func (t *Interface) Start() error {
	// TUN interface is already created and configured
	return nil
}

func (t *Interface) Stop() error {
	if t.file != nil {
		return t.file.Close()
	}
	return nil
}

func (t *Interface) ReadPacket(buffer []byte) ([]byte, error) {
	n, err := t.file.Read(buffer)
	if err != nil {
		return nil, err
	}
	return buffer[:n], nil
}

func (t *Interface) WritePacket(packet []byte) error {
	_, err := t.file.Write(packet)
	return err
}

func createTunDevice(name string) (int, error) {
	// Open /dev/net/tun
	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR, 0)
	if err != nil {
		return 0, fmt.Errorf("failed to open /dev/net/tun: %w", err)
	}

	// Create ifreq structure
	var ifr struct {
		name  [16]byte
		flags uint16
		pad   [22]byte
	}

	// Set interface name
	copy(ifr.name[:], name)
	ifr.flags = 0x0001 | 0x1000 // IFF_TUN | IFF_NO_PI

	// Call TUNSETIFF ioctl
	_, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(fd),
		0x400454ca, // TUNSETIFF
		uintptr(unsafe.Pointer(&ifr)),
	)

	if errno != 0 {
		unix.Close(fd)
		return 0, fmt.Errorf("TUNSETIFF failed: %v", errno)
	}

	return fd, nil
}

func (t *Interface) configure(ip net.IP, cidr string) error {
	// Bring up the interface and assign IP
	cmd := exec.Command("ip", "link", "set", "dev", t.name, "up")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to bring up interface: %w", err)
	}

	// Assign IP address
	cmd = exec.Command("ip", "addr", "add", fmt.Sprintf("%s/%s", ip.String(), cidr[len(cidr)-2:]), "dev", t.name)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to assign IP address: %w", err)
	}

	// Set MTU
	cmd = exec.Command("ip", "link", "set", "dev", t.name, "mtu", fmt.Sprintf("%d", t.mtu))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to set MTU: %w", err)
	}

	return nil
}