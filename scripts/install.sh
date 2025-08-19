#!/bin/bash

# LORBOL Installation Script
set -e

BINARY_NAME="lorbol"
CLI_BINARY_NAME="lorbol-cli"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/lorbol"
SERVICE_DIR="/etc/systemd/system"

echo "🚀 Installing LORBOL..."

# Check if running as root
if [[ $EUID -eq 0 ]]; then
   echo "This script should not be run as root. Please run as a regular user with sudo privileges."
   exit 1
fi

# Create directories
echo "📁 Creating directories..."
sudo mkdir -p $CONFIG_DIR
sudo mkdir -p /var/log/lorbol
sudo mkdir -p /var/lib/lorbol/data

# Build binaries
echo "🔨 Building binaries..."
make build

# Install binaries
echo "📦 Installing binaries..."
sudo cp build/$BINARY_NAME $INSTALL_DIR/
sudo cp build/$CLI_BINARY_NAME $INSTALL_DIR/
sudo chmod +x $INSTALL_DIR/$BINARY_NAME
sudo chmod +x $INSTALL_DIR/$CLI_BINARY_NAME

# Install configuration
echo "⚙️  Installing configuration..."
sudo cp config.yaml $CONFIG_DIR/config.yaml

# Create systemd service
echo "🔧 Creating systemd service..."
sudo tee $SERVICE_DIR/lorbol.service > /dev/null <<EOF
[Unit]
Description=LORBOL - Learning Optimized Routes Based On Latency
After=network.target
Wants=network.target

[Service]
Type=simple
User=lorbol
Group=lorbol
ExecStart=$INSTALL_DIR/$BINARY_NAME -config $CONFIG_DIR/config.yaml
ExecReload=/bin/kill -HUP \$MAINPID
KillMode=process
Restart=on-failure
RestartSec=5s
StandardOutput=journal
StandardError=journal
SyslogIdentifier=lorbol

# Security settings
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/lorbol /var/log/lorbol
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF

# Create lorbol user
echo "👤 Creating lorbol user..."
if ! id "lorbol" &>/dev/null; then
    sudo useradd --system --home-dir /var/lib/lorbol --shell /bin/false lorbol
fi

# Set permissions
echo "🔐 Setting permissions..."
sudo chown -R lorbol:lorbol /var/lib/lorbol
sudo chown -R lorbol:lorbol /var/log/lorbol
sudo chown lorbol:lorbol $CONFIG_DIR/config.yaml

# Enable and start service
echo "🔄 Enabling and starting service..."
sudo systemctl daemon-reload
sudo systemctl enable lorbol.service

echo "✅ LORBOL installed successfully!"
echo ""
echo "📝 Next steps:"
echo "1. Edit configuration: sudo nano $CONFIG_DIR/config.yaml"
echo "2. Start service: sudo systemctl start lorbol"
echo "3. Check status: sudo systemctl status lorbol"
echo "4. View logs: sudo journalctl -u lorbol -f"
echo ""
echo "🔧 CLI usage:"
echo "- lorbol-cli -cmd status"
echo "- lorbol-cli -cmd measure -target <ip>"
echo "- lorbol-cli -cmd optimize"