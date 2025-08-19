# LORBOL 配置详解

## 📋 配置项说明

### 🔧 必须不同的配置项

以下配置项在每台服务器上**必须不同**：

| 配置项 | 服务器1 | 服务器2 | 说明 |
|--------|---------|---------|------|
| `node.name` | "beijing-server" | "shanghai-server" | 节点唯一标识 |
| `node.virtual_ip` | "10.100.0.2" | "10.100.0.3" | 虚拟网络IP |
| `vpn.local_ip` | "10.100.0.2" | "10.100.0.3" | 必须与virtual_ip相同 |

### 🔄 可选不同的配置项

以下配置项**可以不同**，但建议根据需要调整：

| 配置项 | 建议 | 说明 |
|--------|------|------|
| `node.port` | 51820, 51821, ... | 避免端口冲突 |
| `vpn.port` | 与node.port相同 | VPN通信端口 |
| `monitor.metrics_port` | 9090, 9091, ... | 监控端口 |
| `server.port` | 都用8080或分别8080,8081 | HTTP管理端口 |

### ✅ 必须相同的配置项

以下配置项在所有服务器上**必须完全相同**：

```yaml
network:
  name: "lorbol-testnet"     # 网络名称
  cidr: "10.100.0.0/16"     # 网络段
  gateway: "10.100.0.1"     # 网关
  domain: "lorbol.local"    # 域名

routing:
  algorithm: "dijkstra"     # 路由算法

vpn:
  protocol: "udp"           # 通信协议
  encryption: "chacha20poly1305" # 加密方式
  interface: "lorbol0"      # 虚拟网卡名
```

## 🌍 实际部署示例

### 场景：2台阿里云服务器

**服务器1（北京）- 公网IP: 47.93.123.456**
```yaml
node:
  name: "aliyun-beijing"
  virtual_ip: "10.100.0.10"
  public_ip: ""  # 自动检测
  port: 51820

monitor:
  metrics_port: 9090

latency:
  target_endpoints:
    - "8.8.8.8"
    - "47.94.124.789"  # 上海服务器的公网IP
```

**服务器2（上海）- 公网IP: 47.94.124.789**
```yaml
node:
  name: "aliyun-shanghai"
  virtual_ip: "10.100.0.11"
  public_ip: ""  # 自动检测
  port: 51820

monitor:
  metrics_port: 9090

latency:
  target_endpoints:
    - "8.8.8.8"
    - "47.93.123.456"  # 北京服务器的公网IP
```

### 场景：3台跨云服务器

**阿里云北京**
```yaml
node:
  name: "aliyun-beijing"
  virtual_ip: "10.100.0.10"
  port: 51820
```

**腾讯云上海**
```yaml
node:
  name: "tencent-shanghai"
  virtual_ip: "10.100.0.20"
  port: 51820
```

**华为云广州**
```yaml
node:
  name: "huawei-guangzhou"
  virtual_ip: "10.100.0.30"
  port: 51820
```

## 🔥 自定义网络段示例

### 使用100.64.0.0/17网段

```yaml
network:
  name: "company-mesh"
  cidr: "100.64.0.0/17"      # CGNAT地址段
  gateway: "100.64.0.1"
  
# 节点IP分配
# 服务器1: 100.64.0.10
# 服务器2: 100.64.0.11
# 服务器3: 100.64.0.12
```

### 使用172.16.0.0/12网段

```yaml
network:
  name: "private-mesh"
  cidr: "172.16.0.0/12"      # RFC1918私有地址
  gateway: "172.16.0.1"

# 节点IP分配
# 服务器1: 172.16.1.10
# 服务器2: 172.16.1.11
# 服务器3: 172.16.1.12
```

## ⚙️ 性能调优配置

### 低延迟优化

```yaml
latency:
  method: "icmp"            # ICMP更快，如果云服务商支持
  interval: 10s             # 更频繁的测量
  timeout: 1s               # 更短的超时
  sample_size: 5            # 更多采样

routing:
  optimize_interval: 1m     # 更频繁的路由优化
  latency_threshold: 20ms   # 更严格的延迟要求
```

### 高稳定性配置

```yaml
latency:
  method: "tcp"             # TCP更稳定
  interval: 30s             # 较长的测量间隔
  timeout: 5s               # 较长的超时
  sample_size: 7            # 更多采样求平均

routing:
  optimize_interval: 5m     # 较长的优化间隔
  latency_threshold: 100ms  # 较宽松的阈值
  max_retries: 5            # 更多重试
```

### 节约带宽配置

```yaml
latency:
  interval: 60s             # 最长测量间隔
  sample_size: 1            # 最少采样
  target_endpoints:         # 减少测试目标
    - "8.8.8.8"

monitor:
  update_interval: 30s      # 较长的监控更新间隔
```

## 🔒 安全配置建议

### 生产环境配置

```yaml
server:
  host: "127.0.0.1"         # 只监听本地，增加安全性

monitor:
  enabled: false            # 生产环境可关闭监控端口

storage:
  path: "/var/lib/lorbol/data"
  retention: 24h            # 较短的数据保留时间
```

### 开发环境配置

```yaml
server:
  host: "0.0.0.0"           # 监听所有接口方便调试

monitor:
  enabled: true             # 启用监控便于调试
  
latency:
  interval: 5s              # 更频繁的测试便于观察
```

## 🚀 快速配置模板

复制对应的完整配置文件：
- 服务器1: `examples/server1-complete.yaml`
- 服务器2: `examples/server2-complete.yaml`

然后只需要修改：
1. `node.name` - 给每台服务器起个有意义的名字
2. `node.virtual_ip` - 分配不同的虚拟IP
3. `node.port` - 如果需要避免端口冲突

其他配置项通常保持默认即可！