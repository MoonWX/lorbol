# LORBOL 云服务器部署指南

## 🚀 快速部署

### 前提条件

- 2台或以上Linux云服务器（推荐Ubuntu 20.04+）
- Go 1.21+ 环境
- 服务器间网络互通
- 开放端口：51820 (UDP), 8080 (TCP), 9090 (TCP)

### 方法一：自动安装脚本

1. **在每台服务器上克隆代码**：
```bash
git clone <你的仓库地址>
cd lorbol
```

2. **运行安装脚本**：
```bash
chmod +x scripts/install.sh
./scripts/install.sh
```

3. **配置每台服务器**：
```bash
# 服务器1
sudo cp examples/server1-config.yaml /etc/lorbol/config.yaml

# 服务器2  
sudo cp examples/server2-config.yaml /etc/lorbol/config.yaml

# 根据实际情况修改配置
sudo nano /etc/lorbol/config.yaml
```

4. **启动服务**：
```bash
sudo systemctl start lorbol
sudo systemctl status lorbol
```

### 方法二：手动部署

1. **构建二进制文件**：
```bash
make build
```

2. **创建目录结构**：
```bash
sudo mkdir -p /etc/lorbol
sudo mkdir -p /var/lib/lorbol/data
sudo mkdir -p /var/log/lorbol
```

3. **安装二进制文件**：
```bash
sudo cp build/lorbol /usr/local/bin/
sudo cp build/lorbol-cli /usr/local/bin/
sudo chmod +x /usr/local/bin/lorbol*
```

4. **配置服务**：
```bash
# 复制配置文件
sudo cp examples/server1-config.yaml /etc/lorbol/config.yaml

# 修改配置（重要！）
sudo nano /etc/lorbol/config.yaml
```

## ⚙️ 配置详解

### 关键配置项

**网络配置**：
```yaml
network:
  name: "lorbol-testnet"     # 网络名称（所有节点必须相同）
  cidr: "10.100.0.0/16"     # 虚拟网络段
  gateway: "10.100.0.1"     # 网关IP
```

**节点配置**：
```yaml
node:
  name: "server-1"          # 节点名称（每个节点唯一）
  virtual_ip: "10.100.0.2"  # 虚拟IP（每个节点唯一）
  port: 51820               # 通信端口
  auto_detect_ip: true      # 自动检测公网IP
```

**延迟测量**：
```yaml
latency:
  method: "tcp"             # 使用TCP方式（云服务器兼容性更好）
  interval: 15s             # 测量间隔
  timeout: 3s               # 超时时间
```

### 多节点配置示例

**服务器1配置**：
```yaml
node:
  name: "beijing-server"
  virtual_ip: "10.100.0.2"
  port: 51820
```

**服务器2配置**：
```yaml
node:
  name: "shanghai-server"
  virtual_ip: "10.100.0.3"
  port: 51820
```

**服务器3配置**：
```yaml
node:
  name: "guangzhou-server"
  virtual_ip: "10.100.0.4"
  port: 51820
```

## 🔥 启动和测试

### 启动服务

```bash
# 方法1：使用systemd（推荐）
sudo systemctl start lorbol
sudo systemctl enable lorbol

# 方法2：直接运行
lorbol -config /etc/lorbol/config.yaml
```

### 检查服务状态

```bash
# 查看服务状态
sudo systemctl status lorbol

# 查看实时日志
sudo journalctl -u lorbol -f

# 查看错误日志
sudo journalctl -u lorbol --since "1 hour ago" -p err
```

### 使用CLI工具测试

```bash
# 查看网络状态
lorbol-cli -config /etc/lorbol/config.yaml -cmd status

# 测量到特定目标的延迟
lorbol-cli -config /etc/lorbol/config.yaml -cmd measure -target 8.8.8.8

# 手动触发路由优化
lorbol-cli -config /etc/lorbol/config.yaml -cmd optimize
```

## 🔍 验证部署

### 1. 检查节点发现

在任一服务器上查看日志，应该能看到：
```
LORBOL server started successfully
Local node: 10.100.0.2 (beijing-server)
Network: 10.100.0.0/16
```

### 2. 检查节点互连

等待1-2分钟后，检查是否发现了其他节点：
```bash
# 查看发现的节点
grep "node_announcement" /var/log/lorbol/lorbol.log

# 查看延迟测量
grep "latency" /var/log/lorbol/lorbol.log
```

### 3. 检查路由优化

查看路由表更新：
```bash
# 查看路由优化日志
grep "route.*optimiz" /var/log/lorbol/lorbol.log
```

## 🐛 故障排除

### 常见问题

1. **节点发现失败**：
   - 检查防火墙：`sudo ufw allow 51820/udp`
   - 检查多播支持：`ping -c 1 224.0.0.251`

2. **延迟测量失败**：
   - 切换到TCP模式：配置中设置 `method: "tcp"`
   - 检查网络连通性

3. **权限问题**：
   - 确保lorbol用户有正确权限
   - 检查目录权限：`ls -la /var/lib/lorbol`

### 调试命令

```bash
# 检查网络连通性
ping <其他服务器IP>
telnet <其他服务器IP> 51820

# 检查进程状态
ps aux | grep lorbol

# 检查端口监听
netstat -tulpn | grep 51820
ss -tulpn | grep 51820

# 检查配置文件
lorbol -config /etc/lorbol/config.yaml -validate
```

## 📊 监控和管理

### 日志管理

```bash
# 设置日志轮转
sudo nano /etc/logrotate.d/lorbol

# 内容：
/var/log/lorbol/*.log {
    daily
    rotate 30
    compress
    delaycompress
    missingok
    notifempty
    postrotate
        systemctl reload lorbol
    endscript
}
```

### 性能监控

```bash
# 查看指标（如果启用了监控）
curl http://localhost:9090/metrics

# 查看节点状态
lorbol-cli -cmd status | jq .
```

## 🚧 生产环境建议

1. **安全配置**：
   - 使用防火墙限制访问
   - 定期更新配置密钥
   - 监控异常连接

2. **性能优化**：
   - 调整测量间隔
   - 设置合适的超时时间
   - 监控系统资源使用

3. **高可用性**：
   - 部署多个节点
   - 设置自动重启
   - 配置健康检查

## 📞 技术支持

如果遇到问题：
1. 查看日志文件
2. 检查配置文件
3. 确认网络连通性
4. 提交Issue并附上相关日志