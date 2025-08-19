package vpn

import (
	"encoding/binary"
	"fmt"
	"net"
)

type Packet struct {
	Version    uint8
	IHL        uint8
	TOS        uint8
	Length     uint16
	ID         uint16
	Flags      uint16
	TTL        uint8
	Protocol   uint8
	Checksum   uint16
	SrcIP      net.IP
	DstIP      net.IP
	Data       []byte
}

type PacketHandler struct {
	localIP  net.IP
	remoteIP net.IP
}

func NewPacketHandler(localIP, remoteIP string) (*PacketHandler, error) {
	local := net.ParseIP(localIP)
	if local == nil {
		return nil, fmt.Errorf("invalid local IP: %s", localIP)
	}

	remote := net.ParseIP(remoteIP)
	if remote == nil {
		return nil, fmt.Errorf("invalid remote IP: %s", remoteIP)
	}

	return &PacketHandler{
		localIP:  local,
		remoteIP: remote,
	}, nil
}

func (ph *PacketHandler) ParsePacket(data []byte) (*Packet, error) {
	if len(data) < 20 {
		return nil, fmt.Errorf("packet too short: %d bytes", len(data))
	}

	packet := &Packet{}
	
	packet.Version = (data[0] >> 4) & 0x0F
	packet.IHL = data[0] & 0x0F
	packet.TOS = data[1]
	packet.Length = binary.BigEndian.Uint16(data[2:4])
	packet.ID = binary.BigEndian.Uint16(data[4:6])
	packet.Flags = binary.BigEndian.Uint16(data[6:8])
	packet.TTL = data[8]
	packet.Protocol = data[9]
	packet.Checksum = binary.BigEndian.Uint16(data[10:12])
	
	packet.SrcIP = net.IP(data[12:16])
	packet.DstIP = net.IP(data[16:20])
	
	headerLen := int(packet.IHL) * 4
	if len(data) > headerLen {
		packet.Data = data[headerLen:]
	}

	return packet, nil
}

func (ph *PacketHandler) BuildPacket(srcIP, dstIP net.IP, protocol uint8, data []byte) ([]byte, error) {
	headerLen := 20
	totalLen := headerLen + len(data)
	
	if totalLen > 65535 {
		return nil, fmt.Errorf("packet too large: %d bytes", totalLen)
	}

	packet := make([]byte, totalLen)
	
	packet[0] = (4 << 4) | 5
	packet[1] = 0
	binary.BigEndian.PutUint16(packet[2:4], uint16(totalLen))
	binary.BigEndian.PutUint16(packet[4:6], 0)
	binary.BigEndian.PutUint16(packet[6:8], 0)
	packet[8] = 64
	packet[9] = protocol
	binary.BigEndian.PutUint16(packet[10:12], 0)
	
	copy(packet[12:16], srcIP.To4())
	copy(packet[16:20], dstIP.To4())
	
	checksum := ph.calculateChecksum(packet[:20])
	binary.BigEndian.PutUint16(packet[10:12], checksum)
	
	copy(packet[20:], data)
	
	return packet, nil
}

func (ph *PacketHandler) calculateChecksum(header []byte) uint16 {
	var checksum uint32
	
	for i := 0; i < len(header); i += 2 {
		if i+1 < len(header) {
			checksum += uint32(binary.BigEndian.Uint16(header[i : i+2]))
		} else {
			checksum += uint32(header[i]) << 8
		}
	}
	
	for checksum>>16 > 0 {
		checksum = (checksum & 0xFFFF) + (checksum >> 16)
	}
	
	return uint16(^checksum)
}

func (ph *PacketHandler) IsValidPacket(packet *Packet) bool {
	if packet.Version != 4 {
		return false
	}
	
	if packet.IHL < 5 {
		return false
	}
	
	if packet.Length < uint16(packet.IHL*4) {
		return false
	}
	
	return true
}

func (ph *PacketHandler) ShouldForward(packet *Packet) bool {
	if packet.DstIP.Equal(ph.localIP) {
		return false
	}
	
	if packet.SrcIP.Equal(ph.localIP) {
		return true
	}
	
	return ph.isInNetwork(packet.DstIP)
}

func (ph *PacketHandler) isInNetwork(ip net.IP) bool {
	_, network, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		return false
	}
	
	return network.Contains(ip)
}