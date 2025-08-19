# LORBOL

## What is LORBOL?

Lorbol (Learning Optimized Routes Based On Latency) is a high-performance, open-source solution to create an optimized virtual private network that automatically learns and optimizes routes based on real-time latency measurements.

## Features

### 🌐 Virtual Network Management
- **Custom Network CIDR**: Support for custom network segments like `10.0.0.0/8`, `100.64.0.0/17`, etc.
- **Auto IP Allocation**: Automatic virtual IP allocation within the network
- **Subnet Management**: Organized subnets for nodes, services, and reserved ranges

### 🔍 Node Discovery & Management
- **Automatic Discovery**: Multicast-based peer discovery within the network
- **P2P Communication**: Direct peer-to-peer communication protocol
- **Node Registration**: Automatic node registration and heartbeat monitoring

### 📊 Intelligent Routing
- **Latency-Based Optimization**: Routes optimized based on real-time latency measurements
- **Dijkstra Algorithm**: Uses Dijkstra's shortest path algorithm for optimal routing
- **Dynamic Updates**: Continuous route optimization as network conditions change
- **Multi-hop Support**: Support for multi-hop routing through intermediate nodes

### 📈 Network Monitoring
- **Real-time Latency**: Continuous latency measurements to all network nodes
- **Multiple Measurement Methods**: Support for ICMP ping and TCP connect measurements  
- **Performance Metrics**: Comprehensive network performance monitoring
- **Statistics Collection**: Detailed statistics on routes, connections, and performance

### 🔒 Secure Communication
- **Encrypted Tunnels**: Secure VPN tunnels between nodes
- **ChaCha20Poly1305**: Modern encryption for secure communication
- **P2P Connections**: Direct encrypted connections between peers

## Architecture

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Node A        │    │   Node B        │    │   Node C        │
│ 10.0.0.2        │◄──►│ 10.0.0.3        │◄──►│ 10.0.0.4        │
│                 │    │                 │    │                 │
│ ┌─────────────┐ │    │ ┌─────────────┐ │    │ ┌─────────────┐ │
│ │   Latency   │ │    │ │   Latency   │ │    │ │   Latency   │ │
│ │  Measurer   │ │    │ │  Measurer   │ │    │ │  Measurer   │ │
│ └─────────────┘ │    │ └─────────────┘ │    │ └─────────────┘ │
│ ┌─────────────┐ │    │ ┌─────────────┐ │    │ ┌─────────────┐ │
│ │   Route     │ │    │ │   Route     │ │    │ │   Route     │ │
│ │ Optimizer   │ │    │ │ Optimizer   │ │    │ │ Optimizer   │ │
│ └─────────────┘ │    │ └─────────────┘ │    │ └─────────────┘ │
│ ┌─────────────┐ │    │ ┌─────────────┐ │    │ ┌─────────────┐ │
│ │ Discovery   │ │    │ │ Discovery   │ │    │ │ Discovery   │ │
│ │  Service    │ │    │ │  Service    │ │    │ │  Service    │ │
│ └─────────────┘ │    │ └─────────────┘ │    │ └─────────────┘ │
└─────────────────┘    └─────────────────┘    └─────────────────┘
```

## Quick Start

### 1. Build the Application

```bash
make build
```

### 2. Configure Your Network

Edit `config.yaml`:

```yaml
network:
  name: "my-network"
  cidr: "10.0.0.0/8"  # or "100.64.0.0/17" for custom ranges
  gateway: "10.0.0.1"

node:
  name: "node-1"
  virtual_ip: "10.0.0.2"
  port: 51820
  auto_detect_ip: true
```

### 3. Start the Node

```bash
./build/lorbol -config config.yaml
```

### 4. Use CLI Tools

```bash
# Measure latency to a target
./build/lorbol-cli -cmd measure -target 10.0.0.3

# Show current network status
./build/lorbol-cli -cmd status

# Run route optimization
./build/lorbol-cli -cmd optimize
```

## Configuration

### Network Configuration

```yaml
network:
  name: "lorbol-net"           # Network name
  cidr: "10.0.0.0/8"          # Network CIDR block
  gateway: "10.0.0.1"         # Gateway IP
  dns_servers:                # DNS servers
    - "8.8.8.8"
    - "1.1.1.1"
  domain: "lorbol.local"      # Domain suffix
  mtu: 1420                   # MTU size
```

### Node Configuration

```yaml
node:
  name: "lorbol-node-1"       # Node name
  virtual_ip: "10.0.0.2"     # Virtual IP in network
  public_ip: ""               # Public IP (auto-detect if empty)
  port: 51820                 # Port for communication
  auto_detect_ip: true        # Auto-detect public IP
```

### Routing Configuration

```yaml
routing:
  algorithm: "dijkstra"       # Routing algorithm
  optimize_interval: 5m       # How often to optimize routes
  latency_threshold: 100ms    # Latency threshold for route decisions
  max_retries: 3              # Max retries for failed operations
```

## Use Cases

1. **Optimized Gaming Networks**: Create low-latency networks for gaming
2. **Distributed Teams**: Connect remote team members with optimal routing
3. **IoT Networks**: Manage IoT devices with intelligent routing
4. **Development Networks**: Create isolated development environments
5. **Multi-cloud Connectivity**: Connect resources across cloud providers

## How It Works

1. **Node Discovery**: Nodes automatically discover each other using multicast
2. **Latency Measurement**: Each node continuously measures latency to all other nodes
3. **Route Optimization**: Dijkstra algorithm finds the shortest latency paths
4. **Dynamic Updates**: Routes are updated as network conditions change
5. **Secure Communication**: All traffic is encrypted using modern cryptography

## Development

### Build Commands

```bash
make build    # Build binaries
make test     # Run tests
make fmt      # Format code
make vet      # Run go vet
make clean    # Clean build artifacts
```

### Project Structure

```
├── cmd/                    # Application entry points
│   ├── lorbol/            # Main application
│   └── lorbol-cli/        # CLI tool
├── internal/              # Core business logic
│   ├── adapters/          # Interface adapters
│   ├── config/            # Configuration management
│   ├── latency/           # Latency measurement
│   ├── monitor/           # Performance monitoring
│   ├── network/           # Network management
│   ├── routing/           # Route optimization
│   ├── storage/           # Data persistence
│   └── vpn/              # VPN functionality
├── pkg/                   # Reusable packages
│   └── utils/            # Utility functions
└── config.yaml           # Default configuration
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## License

This project is open-source. Please see the LICENSE file for details.

## Status

This project is under active development. Current functionality includes:

- ✅ Virtual network management
- ✅ Node discovery and registration
- ✅ Latency measurement (ICMP/TCP)
- ✅ Dijkstra-based route optimization
- ✅ P2P communication protocol
- ✅ Configuration management
- 🚧 VPN tunnel implementation
- 🚧 Web management interface
- 🚧 Advanced monitoring dashboard