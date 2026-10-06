# qb-guard

专为 qBittorrent / qB Enhanced Edition 设计的高性能、轻量级反吸血与流量统计守护进程。

使用 Go 编写，单静态二进制部署，运行时内存占用约 15MB，无额外运行时依赖。本项目参考了 **[PeerBanHelper](https://github.com/Ghost-chu/PeerBanHelper)** 的核心反吸血机制与规则设计，并集成了详细流量统计与数据分析看板。

---

## 相关截图

<details>
<summary>点击查看</summary>

<img width="1906" height="906" alt="image" src="https://github.com/user-attachments/assets/ed4cc288-f0b7-4bd1-b4ae-e647c5b544f6" />

<img width="1898" height="894" alt="image" src="https://github.com/user-attachments/assets/6bd4b084-daef-44d9-b40b-3dc813b39607" />

<img width="1904" height="912" alt="image" src="https://github.com/user-attachments/assets/03f1b8a0-cc49-42a2-9153-618fbbe481d2" />

<img width="1887" height="900" alt="image" src="https://github.com/user-attachments/assets/48e14b7d-b7c3-4b7b-9f19-e996c2b6955c" />

</details>


---

## 核心特性

### 1. 全方位反吸血防护（参考 PeerBanHelper）

- **客户端特征黑名单**：
  - **PeerID 黑名单**：拦截已知吸血/流氓客户端 PeerID（支持前缀、后缀、包含、全等及正则表达式）。
  - **客户端名称黑名单**：过滤常见吸血软件名称（迅雷、影音播放器等）。
- **智能行为分析（虚假进度检查器 PCB）**：
  - 对比本地实际上传数据量与对端汇报进度，有效检测虚假进度、进度回退及超量下载行为。
  - 支持连接重置预热测试（快速 PCB 测试），有效防范多端口洗记录作弊。
- **网段打击与追猎**：
  - **自动连锁封禁（Auto Range Ban）**：Peer 命中封禁后，连带封禁其所在的 `/30` (IPv4) 或 `/48` (IPv6) 网段。
  - **多拨追猎（Multi-dialing Blocker）**：自动检测同一网段内多 IP 并发连接同一任务的行为，打击多拨吸血与 PCDN。
- **自定义脚本引擎**：
  - 内置基于 [expr](https://expr-lang.org) 的表达式引擎，支持编写基于 Peer 属性和种子属性的高级自定义判定规则。
- **IP 黑名单与规则订阅**：
  - 支持静态 CIDR / 端口黑名单。
  - 支持定时拉取第三方远程 IP 规则集（兼容 IP/CIDR 与 DAT 格式）。
- **灵活的封禁策略**：
  - 支持普通封禁（`ban`）与 qB EE 影子封禁（`shadowban`，保持连接但不给数据）。
  - 自动保留并同步用户在 qBittorrent 界面中手动添加的外部封禁，避免覆盖。

### 2. 流量与数据监控（集成 qb-stats）

- **实时全局指标**：监控实时上传/下载速率、做种总体积与历史累计传输量。
- **多维历史趋势**：
  - 提供采样级（Recent）、小时级（Hourly）、天级（Daily）三档传输趋势曲线。
  - 提供按日历日汇总的上传/下载柱状图看板。
- **站点 / Tracker 统计**：自动按 Tracker 域名归并统计各站点做种数、上传量与下载量。
- **单任务明细穿透**：实时获取种子详情，查看任务速率、完成度、做种时长与分享率。

### 3. 公共 Tracker 自动聚合

- 内置优质公共 Tracker 订阅源（XIU2 精选、ngosang 精选）。
- 支持定时拉取、自动去重合并，并同步写入 qBittorrent 的自动添加 Tracker 列表中。

### 4. 现代化 Web 控制台

- 内置 Vue 3 + Tailwind CSS + ECharts 单页管理面板（嵌入二进制，无需额外前端容器或反向代理）。
- 支持 WebUI 在线调整模块开关与配置，**配置保存后绝大多数热重载生效**，无需频繁重启容器。
- 支持 SSE 实时状态推送，原生适配深色/浅色主题。

---

## 快速部署

### Docker Compose（推荐）

创建 `docker-compose.yml`：

```yaml
services:
  qb-guard:
    image: ghcr.io/misaka10843/qb-guard:latest
    container_name: qb-guard
    restart: unless-stopped
    # 必须使用 host 网络模式，否则 Docker NAT 会掩盖对端真实 IP，导致所有基于 IP 的检测失效
    network_mode: host
    environment:
      - TZ=Asia/Shanghai
    volumes:
      - ./config.yaml:/config.yaml:ro
      - ./data:/data
```

启动容器：

```bash
docker compose up -d
```

默认 WebUI 地址：`http://<主机IP>:9091/`。

---

## 基础配置

在与 `docker-compose.yml` 同级目录下准备 `config.yaml`（完整示例可直接参考仓库中的 [`config.yaml`](config.yaml)）：

```yaml
qbittorrent:
  url: http://127.0.0.1:8080
  username: admin
  password: adminadmin
  ban-method: ban # ban: 常规封禁; shadowban: qB EE 影子封禁

server:
  address: 0.0.0.0
  http: 9091

# 检测循环与全局时长
check-interval: 5s
ban-duration: 14d
persist-interval: 30s
data-dir: /data

# 流量统计配置
stats:
  sample-interval: 5m
  recent-keep: 48h
  hourly-keep: 30d
  daily-keep: 365d

# 内网/回环白名单（跳过检查）
ignore-peers-from-addresses:
  - 10.0.0.0/8
  - 172.16.0.0/12
  - 192.168.0.0/16
  - 127.0.0.0/8
  - 100.64.0.0/10

# 模块配置（具体参数可在 WebUI 中直接调整）
module:
  peer-id-blacklist:
    enabled: true
    ban-duration: 72h
  client-name-blacklist:
    enabled: true
    ban-duration: 72h
  ip-address-blocker:
    enabled: true
    ban-duration: 72h
  progress-cheat-blocker:
    enabled: true
    ban-duration: 30d
  auto-range-ban:
    enabled: true
    ban-duration: 7d
  multi-dialing-blocker:
    enabled: true
    ban-duration: 15d
  expression-engine:
    enabled: true
    ban-duration: 72h
  ip-address-blocker-rules:
    enabled: true
    ban-duration: 72h
    rules: {}
  active-monitoring:
    enabled: true
```

> **说明**：
>
> - Web 控制台登录使用 `config.yaml` 中配置的 qBittorrent 账号密码，并在本地安全验证。
> - 除了修改监听地址端口（`server.address` / `server.http`）与存储目录（`data-dir`）外，其余所有配置项保存后均会**热重载立即生效**。

---

## 自定义表达式规则

表达式脚本存放于 `<data-dir>/scripts/*.expr`，语言为 [expr](https://expr-lang.org)。

可访问变量：

- `peer`: `ip`, `port`, `peerId`, `clientName`, `progress`, `uploadSpeed`, `downloadSpeed`, `uploaded`, `downloaded`, `flags`
- `torrent`: `id`, `name`, `hash`, `size`, `completedSize`, `progress`, `uploadSpeed`, `downloadSpeed`, `isPrivate`, `category`, `tags`

返回值：

- `true` 或 `1`：执行封禁
- `false`：不处理
- `2`：白名单放行，跳过后续所有检查

示例：

```javascript
// 封禁指定客户端或上传量大于0但汇报进度为0的 Peer
peer.clientName == 'StellarPlayer 3.0' ||
  (peer.uploaded > 0 && peer.progress == 0)
```

---

## 本地编译与构建

构建依赖：Go 1.24+、Node.js 22+（构建前端静态资源）。

```bash
# 1. 编译 WebUI 静态文件
cd webui
npm install
npm run build
cd ..

# 2. 编译 Go 二进制文件
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o qb-guard .

# 3. 运行
./qb-guard config.yaml
```

---

## 致谢与参考

- **[PeerBanHelper](https://github.com/Ghost-chu/PeerBanHelper)**：本项目核心反吸血逻辑及默认过滤规则集参考了 PeerBanHelper 的优秀设计。
- **[TrackersListCollection](https://github.com/XIU2/TrackersListCollection)** & **[trackerslist](https://github.com/ngosang/trackerslist)**：公共 Tracker 订阅源支持。
