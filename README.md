# cf-proxy

**Cloudflare 过盾代理服务** —— uTLS 指纹伪装 + HTTP/3 + DNS 优选 + 智能重试。

为 drpy-node / TVBox 生态里的爬虫源提供「本地过盾代理」：源站被 Cloudflare 挡住的
请求，改走本服务转发，由本服务以**真实浏览器 TLS 指纹**发起上游请求。

> 本仓库是 `cf-proxy-linux-amd64` 二进制的**逆向重建实现**，逐条证据与还原过程见
> [docs/README_DEV.md](docs/README_DEV.md)。

---

## 快速开始

```bash
# 1. 下载对应平台二进制（见 Releases）
chmod +x cf-proxy-linux

# 2. 启动（默认监听 12525）
./cf-proxy-linux -port 12525

# 3. 发起代理请求
curl "http://127.0.0.1:12525?url=https%3A%2F%2Fexample.com%2Fpath"
```

### 在 drpy-node 源里使用

```python
self.cfproxy = 'http://127.0.0.1:12525?url='
html = requests.get(f"{self.cfproxy}{target_url}", headers=self.headers, proxies=self.proxy).content
```

## 命令行参数

| 参数 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `-port` | int | `12525` | 监听端口号 |
| `-proxy` | string | `""` | 上游代理，格式 `http://host:port` 或 `socks5://host:port` |
| `-dns` | string | `""` | 指定 DNS 服务器；留空则自动优选最快的一个 |

## 请求格式

```
GET http://127.0.0.1:<port>?url=<urlencode(目标URL)>[&<其余query参数原样透传>]
```

- 除 `url` 外的 query 参数会被拼回目标 URL
- 只接受 `GET`，其他方法返回 405
- 带 `Range` 请求头时返回 206，支持拖动/分段

## 核心特性

| 特性 | 说明 |
|---|---|
| **TLS 指纹伪装** | 按 UA 自动选择 Chrome / Firefox / Safari 指纹（`utls`） |
| **HTTP/3** | 经 `quic-go` 支持 QUIC 上游 |
| **DNS 优选** | 启动时并发测速 7 个公共 DNS，选最快者 |
| **智能重试** | 网络错误或非成功状态（含 CF 挑战）自动重试，指数退避 3s→100ms，最多 3 次 |
| **请求头黑名单** | 过滤 13 类跳端/指纹头 + `if-` / `x-` 前缀 |
| **Referer 改写** | 仅同源 Referer 保留，并改写为本次目标 URL |
| **流式转发** | 256 KiB 缓冲，边收边发 |

### 默认 User-Agent

```
Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36
```

### 过滤的请求头

```
start  end  etag  eagleid  range  connection  http-client-ip  remote-addr
accept-encoding  proxy-connection  content-size  transfer-encoding  reqable-id
前缀：if-*  x-*
```

### 健康检查

```
GET /health → {"ok":true,"version":"...","dns":"..."}
```

> `/health` 是插件化改造时新增的端点，用于 drpy-node 插件市场的健康探测，
> 不属于原始二进制行为，不影响 `/?url=` 代理路径。

## 构建

### Linux / macOS

```bash
make build-all          # 全平台 + 插件市场二进制
make build-market       # 只出插件市场二进制
make build-platform PLATFORM=linux/amd64
```

或直接用脚本：

```bash
./scripts/build.sh --all
./scripts/build.sh --market
./scripts/build.sh --platform windows/amd64
./scripts/build.sh --all --no-upx    # 禁用 UPX 压缩
```

### Windows

```powershell
powershell -ExecutionPolicy Bypass -File scripts\build.ps1
scripts\build.bat
```

### 产物

```
build/                       # 插件市场固定命名（与 plugin.json 的 binaries 对应）
├── cf-proxy-linux           # linux/amd64
├── cf-proxy-win.exe         # windows/amd64
├── cf-proxy-android         # linux/arm64
├── cf-proxy_<os>_<arch>     # 全平台命名
└── SHA256SUMS

dist/                        # 各平台发布包（tar.gz / zip）
```

支持的平台：`linux/amd64` `linux/arm64` `linux/386` `windows/amd64` `windows/386`
`darwin/amd64` `darwin/arm64` `freebsd/amd64`

## 作为 drpy-node 插件安装

```bash
# 生成市场包
python3 market_zip.py --ver 1.0.0 --platform android --android build/cf-proxy-android
python3 market_zip.py --ver 1.0.0 --platform win32   --win build/cf-proxy-win.exe
```

产物在 `dist/`，内含 `plugin.json` + `bin/<binary>`，可直接被 drpy-node
插件市场（`utils/pluginMarket.js`）安装。

## 项目结构

```
cf-proxy/
├── main.go                 # 主程序（代理 + 指纹 + 响应处理）
├── main_test.go            # 单元测试
├── config/dns.go           # DNS 优选（原始为私有模块 mediaTool/config）
├── plugin.json             # drpy-node 插件清单
├── market_zip.py           # 插件市场打包
├── Makefile
├── scripts/
│   ├── build.sh            # 跨平台构建（Linux/macOS）
│   ├── build.ps1           # 跨平台构建（PowerShell）
│   └── build.bat           # 跨平台构建（cmd）
├── docs/
│   ├── README_DEV.md       # 逆向还原记录 + 开发说明
│   └── BUILD_WINDOWS.md    # Windows 构建说明
└── static/index.html       # 简易状态页
```

## 测试

```bash
go test ./...
```

## 许可

MIT。本实现由二进制逆向还原而来，仅用于互操作与学习。
