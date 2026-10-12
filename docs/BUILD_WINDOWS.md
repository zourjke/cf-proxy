# Windows 构建说明

## 前置条件

1. **Go 1.22+** —— https://go.dev/dl/ ，安装后确认：
   ```powershell
   go version
   ```
2. **UPX**（可选，用于压缩二进制）—— https://github.com/upx/upx/releases
   下载 `upx-*-win64.zip`，解压后把 `upx.exe` 所在目录加入 `PATH`。
3. **Git**（用于读取版本号；没有则版本号回退为 `dev`）

## 构建

### 方式一：PowerShell（推荐）

```powershell
# 全平台 + 插件市场二进制
powershell -ExecutionPolicy Bypass -File scripts\build.ps1

# 只出插件市场二进制
powershell -ExecutionPolicy Bypass -File scripts\build.ps1 -MarketOnly

# 指定平台
powershell -ExecutionPolicy Bypass -File scripts\build.ps1 -Platform linux/amd64

# 不压缩
powershell -ExecutionPolicy Bypass -File scripts\build.ps1 -NoUpx
```

### 方式二：cmd

```bat
scripts\build.bat
scripts\build.bat linux/amd64
```

### 方式三：Make

需要 GNU Make（可用 `choco install make` 安装）：

```bat
make build-all
```

## 产物

```
build\
├── cf-proxy-linux           linux/amd64
├── cf-proxy-win.exe         windows/amd64
├── cf-proxy-android         linux/arm64
├── cf-proxy_linux_amd64
├── cf-proxy_windows_amd64.exe
└── ...
dist\
├── cf-proxy_dev_linux_amd64.tar.gz
├── cf-proxy_dev_windows_amd64.zip
└── ...
```

## 手动交叉编译

```bat
set CGO_ENABLED=0
set GOOS=windows& set GOARCH=amd64
go build -ldflags="-s -w" -trimpath -o build\cf-proxy-win.exe .
```

## 常见问题

**Q: `go build` 报 `go.mod file not found`**
A: 请确认在项目根目录（含 `go.mod` 的目录）执行。

**Q: 杀毒软件报毒**
A: UPX 压缩后的二进制常被启发式误报。可先用 `-NoUpx` 构建不压缩版本验证；
正式发布建议同时提供未压缩版本或提交误报申诉。

**Q: UPX 压缩后无法运行**
A: 个别环境对 UPX 壳不友好，去掉压缩即可。`scripts/build.ps1 -NoUpx`。

**Q: 交叉编译到 darwin 报错**
A: `CGO_ENABLED=0` 是必须的，脚本已设置。若仍报错请确认 Go 版本 ≥ 1.22。
