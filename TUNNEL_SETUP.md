# LORBOL 隧道建立详解

## 🔐 隧道建立流程

### 完整的隧道建立过程：

```
节点A (10.100.0.2)              节点B (10.100.0.3)
      |                              |
      |  1. 生成WireGuard密钥对        |  1. 生成WireGuard密钥对
      |  2. 创建虚拟网卡lorbol0        |  2. 创建虚拟网卡lorbol0
      |  3. 启动节点发现              |  3. 启动节点发现
      |                              |
      |------ 4. 多播发现消息 -------->|
      |<----- 5. 响应发现消息 ---------|
      |                              |
      |------ 6. 密钥交换消息 -------->|
      |<----- 7. 密钥交换响应 ---------|
      |                              |
      |  8. 配置WireGuard对等点       |  8. 配置WireGuard对等点
      |  9. 建立加密隧道              |  9. 建立加密隧道
      |                              |
      |<===== 10. 加密数据传输 =====>|
```

## 🛠️ 系统依赖

### 必需的系统组件：

1. **WireGuard内核模块**：
```bash
# Ubuntu/Debian
sudo apt update
sudo apt install wireguard wireguard-tools

# CentOS/RHEL
sudo yum install epel-release
sudo yum install wireguard-tools

# 检查是否安装成功
sudo modprobe wireguard
lsmod | grep wireguard
```

2. **网络工具**：
```bash
# 确保有ip和wg命令
which ip
which wg
```

3. **权限要求**：
```bash
# LORBOL需要root权限来管理网络接口
sudo ./build/lorbol -config config.yaml
```

## 🔧 隧道建立的具体步骤

### 步骤1：节点启动
```bash
# 节点A启动
sudo ./build/lorbol -config /etc/lorbol/config.yaml

# 日志显示：
# 2024/01/01 10:00:00 starting LORBOL server for node beijing-server in network lorbol-testnet...
# 2024/01/01 10:00:00 Generated WireGuard keypair
# 2024/01/01 10:00:00 Created interface lorbol0 with IP 10.100.0.2
# 2024/01/01 10:00:00 Starting discovery service on port 51820
```

### 步骤2：自动节点发现
```bash
# 多播发现过程
# 节点A广播：
{
  "type": "node_announcement",
  "node_id": "node-abc123",
  "name": "beijing-server",
  "virtual_ip": "10.100.0.2",
  "public_ip": "47.93.123.456",
  "port": 51820,
  "network": "10.100.0.0/16"
}

# 节点B响应发现并交换密钥
```

### 步骤3：WireGuard对等点配置
```bash
# 系统自动执行以下命令：

# 在节点A上：
sudo wg set lorbol0 peer <节点B的公钥> endpoint 47.94.124.789:51820 allowed-ips 10.100.0.3/32 persistent-keepalive 25

# 在节点B上：
sudo wg set lorbol0 peer <节点A的公钥> endpoint 47.93.123.456:51820 allowed-ips 10.100.0.2/32 persistent-keepalive 25
```

### 步骤4：验证隧道连接
```bash
# 查看WireGuard状态
sudo wg show

# 应该显示：
interface: lorbol0
  public key: <本地公钥>
  private key: (hidden)
  listening port: 51820

peer: <对等点公钥>
  endpoint: 47.94.124.789:51820
  allowed ips: 10.100.0.3/32
  latest handshake: 1 minute, 23 seconds ago
  transfer: 1.27 KiB received, 1.15 KiB sent
  persistent keepalive: every 25 seconds
```

### 步骤5：测试连通性
```bash
# 通过虚拟网络ping对等点
ping 10.100.0.3

# 应该能正常ping通，延迟会显示实际的网络延迟
```

## 🚀 部署完整示例

### 服务器1部署：

```bash
# 1. 安装依赖
sudo apt update
sudo apt install wireguard wireguard-tools golang-go

# 2. 构建LORBOL
git clone <你的仓库>
cd lorbol
make build

# 3. 配置
sudo mkdir -p /etc/lorbol
sudo cp examples/server1-complete.yaml /etc/lorbol/config.yaml

# 4. 修改配置
sudo nano /etc/lorbol/config.yaml
# 设置：
# node.name: "server-1"
# node.virtual_ip: "10.100.0.2"

# 5. 启动（需要root权限）
sudo ./build/lorbol -config /etc/lorbol/config.yaml
```

### 服务器2部署：

```bash
# 1-3步骤相同

# 4. 修改配置
sudo nano /etc/lorbol/config.yaml
# 设置：
# node.name: "server-2"  
# node.virtual_ip: "10.100.0.3"

# 5. 启动
sudo ./build/lorbol -config /etc/lorbol/config.yaml
```

## 📊 监控隧道状态

### 查看连接状态：
```bash
# 使用WireGuard工具
sudo wg show lorbol0

# 使用LORBOL CLI
lorbol-cli -config /etc/lorbol/config.yaml -cmd status

# 查看路由表
ip route show dev lorbol0

# 查看网络接口
ip addr show lorbol0
```

### 日志监控：
```bash
# 查看LORBOL日志
sudo journalctl -u lorbol -f

# 查看关键事件
sudo journalctl -u lorbol | grep -E "(tunnel|peer|handshake)"
```

## 🔧 故障排除

### 常见问题：

1. **WireGuard模块未加载**：
```bash
sudo modprobe wireguard
# 如果失败，检查内核版本和WireGuard安装
```

2. **权限不足**：
```bash
# LORBOL必须以root权限运行
sudo ./build/lorbol -config config.yaml
```

3. **端口被占用**：
```bash
# 检查端口占用
sudo netstat -tulpn | grep 51820
# 修改配置文件中的端口
```

4. **防火墙阻止**：
```bash
# 开放WireGuard端口
sudo ufw allow 51820/udp
sudo firewall-cmd --permanent --add-port=51820/udp
```

5. **节点发现失败**：
```bash
# 检查多播支持
ping -c 1 224.0.0.251
# 检查network.name是否一致
```

## ✅ 验证隧道工作

### 完整验证流程：

```bash
# 1. 检查WireGuard接口
ip addr show lorbol0

# 2. 检查对等点
sudo wg show

# 3. 测试连通性
ping 10.100.0.3

# 4. 检查路由
ip route get 10.100.0.3

# 5. 查看延迟优化
lorbol-cli -cmd measure -target 10.100.0.3

# 6. 验证加密传输
sudo tcpdump -i any host 47.94.124.789 and port 51820
```

## 🎯 预期结果

部署成功后，你将得到：

1. **自动建立的加密隧道**：两台服务器通过WireGuard加密连接
2. **虚拟局域网**：可以使用10.100.0.x IP直接通信
3. **智能路由**：系统根据延迟自动选择最优路径
4. **动态适应**：网络条件变化时自动调整路由
5. **安全通信**：所有流量通过ChaCha20Poly1305加密

这就是完整的隧道建立和虚拟网络创建过程！🎉