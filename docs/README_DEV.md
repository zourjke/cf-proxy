# 开发说明 · 逆向还原记录

本仓库源码由 `cf-proxy-linux-amd64` 二进制逆向还原而来。本文记录**样本画像、
还原方法、逐条实测证据与不确定项**，便于后续维护与比对。

---

## 1. 样本画像

| 项 | 值 |
|---|---|
| 样本 | `cf-proxy-linux-amd64` |
| 加壳 | **UPX 5.01**（整文件熵 8.00，可打印串仅 811 段） |
| 加壳大小 | 3,638,640 B |
| 脱壳后 | 12,103,864 B |
| md5（加壳） | `3f6ef6816857343f8af825a4dbc8a37e` |
| sha256（脱壳） | `fc85f937f33d53c3e3501c3315f47b20fafc2fba908af042ae180fcc92521a4e` |
| 类型 | ELF64 x86-64, statically linked, stripped, Go BuildID |
| Go 版本 | go1.24.4 |
| module | `mediaTool` (devel) |
| path | `command-line-arguments`（`go build demo.go` 单文件编译） |
| **原始源文件** | **`./demo.go`**（Ghidra 从 pclntab 行号还原） |
| build flags | `-trimpath=true` `CGO_ENABLED=0` `GOARCH=amd64` `GOOS=linux` |

### 依赖（`go version -m` 实测）

```
github.com/imroc/req/v3               v3.50.0   HTTP 客户端
github.com/refraction-networking/utls v1.6.7   TLS 指纹伪装
github.com/quic-go/quic-go            v0.48.2   HTTP/3
github.com/cloudflare/circl           v1.5.0    后量子密码
github.com/andybalholm/brotli         v1.1.1    br 解压
github.com/klauspost/compress         v1.17.11  zstd
github.com/sirupsen/logrus            v1.9.3    日志
mediaTool                             (devel)   本地模块 → mediaTool/config
```

## 2. 还原方法

1. **判壳**：`file` + 全文件熵扫描 → 熵 8.00 且可打印串极少 → UPX。
   内嵌串直接暴露 `$Id: UPX 5.01 ...`。
2. **脱壳**：`upx -d`（UPX 5.2.1），3.6 MB → 12.1 MB，一次成功。
3. **Go 元数据**：`go version -m` 一次拿到 Go 版本 + 完整依赖清单。
4. **函数骨架**：pclntab 在 stripped 后仍保留，`strings` 即可捞出
   `main.*` / `mediaTool/config.*` 全部符号。
5. **反编译**：Ghidra 12.1.4（经 rea MCP）。**Ghidra 会直接打印
   `Golang source: ./demo.go:NNN`**，行号可用于标定源文件结构。
6. **字符串常量**：伪代码里显示为 `DAT_xxx` 时，用 ELF program header
   做 vaddr → file offset 映射直接读原始字节。

### 还原出的源文件行号分布

| 函数 | 行号 |
|---|---|
| `main.main` | demo.go:42 |
| `main.main.func1` | demo.go:50 |
| `main.handleProxyRequest` | demo.go:75 |
| `main.handleProxyRequest.func1` | demo.go:90 |
| `main.handleProxyRequest.func2` | demo.go:215 |
| `main.createRestClient` | demo.go:229 |
| `main.createRestClient.func1` | demo.go:236 |
| `main.createRestClient.func2` | demo.go:273 |
| `main.createRestClient.func3` | demo.go:282 |
| `main.SetTLSFingerprint` | demo.go:314 |
| `main.processResponse` | demo.go:334 |
| `main.shouldFilterHeaderName` | demo.go:378 |

## 3. 实测还原的关键数据

### 命令行参数

| 参数 | 默认 | usage 文案（实测） |
|---|---|---|
| `-port` | `12525`（0x30ed） | 监听端口号，默认为12525 |
| `-proxy` | `""` | 代理服务器地址，格式：http://host:port 或 socks5://host:port |
| `-dns` | `""` | DNS 服务地址 (可选) |

### DNS 优选候选（静态数据实测）

- DNS：`8.8.8.8` `8.8.4.4` `119.29.29.29` `182.254.116.116` `180.76.76.76` `223.5.5.5` `223.6.6.6`
- 测速域名：`pan.baidu.com` `pan.quark.cn` `drive.uc.cn`

### 请求头黑名单（13 项 + 2 前缀）

```
start  end  etag  eagleid  range  connection  http-client-ip  remote-addr
accept-encoding  proxy-connection  content-size  transfer-encoding  reqable-id
前缀：if-  x-
空字符串也返回 true（汇编实测 MOV EAX,1）
```

### TLS 指纹选择（分支顺序实测）

| UA 含 | 指纹 |
|---|---|
| `edge` / `chrome` / `android` | `ImpersonateChrome()` |
| `firefox` | `ImpersonateFirefox()` |
| `mac os` | `ImpersonateSafari()` |
| 其他 | `ImpersonateChrome()` |

### 超时与重试

| 项 | 值 |
|---|---|
| Client.Timeout | 30s |
| Dialer.Timeout / KeepAlive | 30s / 30s |
| TLSHandshakeTimeout | 30s |
| ResponseHeaderTimeout | 30s |
| 退避区间 | min 3s / max 100ms |
| MaxRetries | 3 |
| 重试条件 | `err != nil` 或 `resp.ResultState() != req.SuccessState` |
| 输出缓冲 | 256 KiB（0x40000） |
| Range | 有 → 206，无 → 200 |

### 错误文案（实测原文）

```
cf代理缺少url参数              → 400
cf代理解析URL错误: %v          → 400
请求已取消                     → 408 (0x198)
请求失败: %v                   → 502 (0x1f6)
cf代理服务器启动，监听端口: %d
服务器启动失败: %v
```

## 4. 与本实现的有意差异

| 项 | 原始 | 本仓库 | 原因 |
|---|---|---|---|
| 源文件名 | `demo.go` | `main.go` | 仓库惯例 |
| module | `mediaTool` | `cf-proxy` | 私有模块不可得，改为独立模块 |
| `mediaTool/config` | 私有包 | `config/` 重新实现 | 同上；对外签名保持一致 |
| 日志调用 | `logrus` 包级 `Logf(level, ...)` | `logrus.Infof(...)` | logrus 无包级 `Logf`，行为等价 |
| `/health` 端点 | 无 | 有 | 供 drpy-node 插件市场健康探测 |
| 版本变量 | 无 | `Version/BuildTime/GitCommit` | 由 `-ldflags` 注入 |

## 5. 不确定项（诚实标注）

1. `demo.go:215` 的 defer：伪代码显示对 `resp.Response.Body` 的接口做无参调用并
   判返回值是否为零，非零则提前 return。控制流明确，但**具体方法名存疑**，
   本实现按 `resp.Response.Body.Close() != nil` 重建。
2. `handleProxyRequest` 非 GET 分支的错误文案未能从字符串池定位，
   本实现使用等价文案「cf代理仅支持GET请求」。
3. `createRestClient.func3`（自定义 `DialContext`）的失败回退细节未逐条核对，
   本实现按「逐 IP 尝试、全部失败返回最后一个错误」重建。

## 6. 复现逆向过程

```sh
# 1. 脱壳
curl -sL -o upx.tar.xz \
  "https://ghfast.top/https://github.com/upx/upx/releases/download/v5.2.1/upx-5.2.1-amd64_linux.tar.xz"
tar -xf upx.tar.xz && ./upx-5.2.1-amd64_linux/upx -d cf-proxy-linux-amd64 -o cf-proxy.unpacked

# 2. Go 元数据
go version -m cf-proxy.unpacked

# 3. 反编译（Ghidra，需先等首次自动分析完成）
#    procedure_pseudo_code main.handleProxyRequest
```

## 7. 开发约定

```bash
make dev      # 快速构建
make test     # 单元测试
make vet      # 静态检查
make fmt      # 格式化
```

提交前请保证 `go vet ./...` 与 `go test ./...` 全绿。
