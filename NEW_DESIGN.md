# LORBOL 新设计：无需WireGuard的智能网状网络

## 🎯 解决的问题

你说得对，之前的设计有几个重大问题：

1. **依赖WireGuard** - 需要用户手动安装和配置
2. **多播发现不可行** - 公网环境下多播不work
3. **虚拟网关问题** - 10.100.0.1实际不存在
4. **复杂度过高** - 过度工程化

## 🚀 新设计方案

### 核心原理：去中心化的智能UDP隧道

```
节点A (公网IP: 47.93.1.1)     节点B (公网IP: 47.94.2.2)
虚拟IP: 10.100.0.2            虚拟IP: 10.100.0.3
         |                            |
         |  1. Bootstrap发现            |
         |     (GitHub/DNS/HTTP)      |
         |                            |
         |<------ 2. 直接UDP连接 ----->|
         |                            |
         |  3. 密钥交换和隧道建立       |
         |                            |
         |<===== 4. 加密数据传输 =====>|
         |                            |
    TUN接口: lorbol0              TUN接口: lorbol0
```

## 🔧 技术架构

### 1. 节点发现机制

**无需中心化服务器，支持多种发现方式：**

#### A. GitHub-based Bootstrap (免费)
```yaml
# 节点信息存储在GitHub仓库
# https://github.com/用户名/lorbol-nodes/blob/main/nodes.json
{
  "nodes": [
    {
      "id": "node-abc123",
      "name": "beijing-server",
      "virtual_ip": "10.100.0.2",
      "public_ip": "47.93.1.1", 
      "port": 51820,
      "network": "my-network",
      "last_seen": 1640995200
    }
  ]
}
```

#### B. DNS TXT记录发现
```bash
# 通过DNS TXT记录发布节点信息
_lorbol._udp.example.com. TXT "node=beijing-server,ip=47.93.1.1,port=51820,vip=10.100.0.2"
```

#### C. HTTP API Bootstrap
```bash
# 简单的HTTP API服务器
POST /api/nodes/register
GET /api/nodes/discover?network=my-network
```

### 2. 简化的UDP隧道

**无需WireGuard，直接实现加密隧道：**

```go
type SimpleTunnel struct {
    // ChaCha20Poly1305加密
    // 直接UDP通信
    // 自动NAT穿透
    // 密钥自动生成和交换
}
```

**数据包格式：**
```
| Type(1) | Nonce(12) | Encrypted Payload |
| 1=握手  | 随机数     | 加密数据           |
| 2=数据  |           |                   |
| 3=心跳  |           |                   |
```

### 3. TUN接口直接管理

**无需外部工具，直接创建和管理TUN接口：**

```go
// 直接调用Linux系统调用创建TUN设备
fd := unix.Open("/dev/net/tun", unix.O_RDWR, 0)
unix.Syscall(unix.SYS_IOCTL, fd, unix.TUNSETIFF, ...)
```

### 4. 智能路由和NAT穿透

**自动处理复杂网络环境：**

- **NAT穿透**: UDP hole punching
- **中继模式**: 无法直连时通过其他节点中继
- **延迟优化**: 持续测量并选择最优路径

## 📋 配置简化

### 最小化配置文件

```yaml
# 只需要配置核心信息
network:
  name: "my-company-net"
  cidr: "10.100.0.0/16"

node:
  name: "beijing-server"
  virtual_ip: "10.100.0.2"
  # public_ip 自动检测
  # port 自动选择可用端口

# Bootstrap方法（选择一种）
bootstrap:
  method: "github"  # github/dns/http/static
  config:
    github_repo: "username/lorbol-nodes"
    # 或者
    # dns_domain: "nodes.example.com"
    # 或者
    # http_url: "https://api.example.com/lorbol"
    # 或者静态节点列表
    # static_peers: ["47.94.2.2:51820"]
```

## 🔄 工作流程

### 1. 节点启动
```bash
sudo ./lorbol -config config.yaml

# 输出：
# [INFO] Creating TUN interface lorbol0 with IP 10.100.0.2/16
# [INFO] Starting UDP tunnel on port 51820
# [INFO] Discovering peers via github...
# [INFO] Found peer: shanghai-server (47.94.2.2:51820)
# [INFO] Establishing tunnel to shanghai-server...
# [INFO] Tunnel established, testing connectivity...
# [INFO] Network ready: 1 peers connected
```

### 2. 自动隧道建立
```
1. 节点A启动，读取配置
2. 创建TUN接口 lorbol0 (10.100.0.2/16)
3. 启动UDP监听 (0.0.0.0:51820)
4. 查询bootstrap服务发现节点B
5. 向节点B发送握手包
6. 密钥交换，建立加密隧道
7. 设置路由: 10.100.0.3 -> tunnel
8. 开始数据传输和延迟测量
```

### 3. 数据传输
```
应用程序 -> TUN接口 -> 路由判断 -> 加密隧道 -> 远程节点
ping 10.100.0.3  ->  lorbol0  ->  UDP tunnel  ->  节点B
```

## 🎯 关键优势

### 1. **零依赖**
- 无需安装WireGuard
- 无需root权限配置网络（只需启动时）
- 单一二进制文件

### 2. **自动化**
- 自动节点发现
- 自动密钥交换
- 自动路由配置
- 自动NAT穿透

### 3. **智能路由**
- 实时延迟测量
- 动态路径选择
- 故障自动切换

### 4. **去中心化**
- 无需中心服务器
- 支持多种bootstrap方式
- 节点对等连接

## 🚀 快速部署

### 服务器1 (北京)
```bash
# 1. 配置
cat > config.yaml << EOF
network:
  name: "test-net"
  cidr: "10.100.0.0/16"
node:
  name: "beijing"
  virtual_ip: "10.100.0.2"
bootstrap:
  method: "static"
  config:
    static_peers: ["47.94.2.2:51820"]  # 上海服务器
EOF

# 2. 启动
sudo ./lorbol -config config.yaml
```

### 服务器2 (上海)
```bash
# 1. 配置
cat > config.yaml << EOF
network:
  name: "test-net" 
  cidr: "10.100.0.0/16"
node:
  name: "shanghai"
  virtual_ip: "10.100.0.3"
bootstrap:
  method: "static"
  config:
    static_peers: ["47.93.1.1:51820"]  # 北京服务器
EOF

# 2. 启动
sudo ./lorbol -config config.yaml
```

### 验证连接
```bash
# 在任一服务器上
ping 10.100.0.3  # 应该能ping通
ip route show    # 查看路由表
./lorbol-cli status  # 查看连接状态
```

## 🔧 故障排除

### 1. **连接问题**
```bash
# 检查UDP端口
netstat -ulpn | grep 51820

# 检查防火墙
sudo ufw allow 51820/udp

# 测试直连
nc -u 对方IP 51820
```

### 2. **TUN接口问题**
```bash
# 检查TUN接口
ip addr show lorbol0

# 手动创建测试
sudo ip tuntap add dev test0 mode tun
```

## 💡 网关问题解决

**虚拟网关不是真实设备，而是路由概念：**

- `10.100.0.1` 只存在于配置中作为网关地址
- 实际路由通过各节点的TUN接口
- 每个节点维护到其他节点的直接路由
- 无需真实的网关设备

这样的设计更简单、更可靠，完全不依赖外部工具！🎉