#!/bin/bash
# cf-proxy 跨平台构建脚本
# 同时产出两类产物：
#   1) dist/  —— 各平台独立发布包（tar.gz / zip）
#   2) build/ —— 插件市场用的固定命名二进制（cf-proxy-linux / cf-proxy-win.exe / cf-proxy-android）
set -e

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'

cd "$(dirname "$0")/.."

APP_NAME="cf-proxy"
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S_UTC')
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

BUILD_DIR="build"
DIST_DIR="dist"

PLATFORMS=(
    "linux/amd64"
    "linux/arm64"
    "linux/386"
    "windows/amd64"
    "windows/386"
    "darwin/amd64"
    "darwin/arm64"
    "freebsd/amd64"
)

# 插件市场固定命名映射：市场名 -> GOOS/GOARCH
# 与 plugin.json 的 binaries 字段一一对应
MARKET_TARGETS=(
    "linux:linux/amd64"
    "win.exe:windows/amd64"
    "android:linux/arm64"
)

LDFLAGS="-s -w -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.GitCommit=${GIT_COMMIT}"

cleanup() {
    echo -e "${YELLOW}清理构建目录...${NC}"
    rm -rf "$BUILD_DIR" "$DIST_DIR"
}

setup_dirs() {
    mkdir -p "$BUILD_DIR" "$DIST_DIR"
}

compress() {
    local f=$1
    if [ "${USE_UPX}" = "1" ] && command -v upx >/dev/null 2>&1; then
        upx --best --lzma "$f" >/dev/null 2>&1 || true
    fi
}

# 通用构建：$1=goos $2=goarch $3=输出路径
go_build() {
    local goos=$1 goarch=$2 out=$3
    GOOS=$goos GOARCH=$goarch CGO_ENABLED=0 \
        go build -ldflags="$LDFLAGS" -trimpath -o "$out" .
}

build_platform() {
    local platform=$1
    local goos=${platform%%/*}
    local goarch=${platform##*/}
    local output_path="$BUILD_DIR/${APP_NAME}_${goos}_${goarch}"
    [ "$goos" = "windows" ] && output_path="${output_path}.exe"

    echo -e "${BLUE}构建 $goos/$goarch...${NC}"
    if go_build "$goos" "$goarch" "$output_path"; then
        echo -e "${GREEN}✓ $goos/$goarch ($(du -h "$output_path" | cut -f1))${NC}"
        compress "$output_path"
        echo -e "${GREEN}  → 压缩后 $(du -h "$output_path" | cut -f1)${NC}"
        create_release_package "$goos" "$goarch" "$output_path"
    else
        echo -e "${RED}✗ $goos/$goarch 构建失败${NC}"
        return 1
    fi
}

create_release_package() {
    local goos=$1 goarch=$2 binary_path=$3
    local package_name="${APP_NAME}_${VERSION}_${goos}_${goarch}"
    local package_dir="$DIST_DIR/$package_name"
    mkdir -p "$package_dir"

    cp "$binary_path" "$package_dir/"
    cp README.md "$package_dir/" 2>/dev/null || true

    if [ "$goos" = "windows" ]; then
        cat > "$package_dir/start.bat" <<'EOF'
@echo off
echo Starting cf-proxy on port 12525...
cf-proxy.exe -port 12525
pause
EOF
    else
        cat > "$package_dir/start.sh" <<'EOF'
#!/bin/sh
echo "Starting cf-proxy on port 12525..."
exec ./cf-proxy -port 12525
EOF
        chmod +x "$package_dir/start.sh"
    fi

    cat > "$package_dir/config.example" <<'EOF'
# cf-proxy 配置示例
# 用法: ./cf-proxy -port 12525 -proxy http://127.0.0.1:7890 -dns 8.8.8.8
# 请求: GET http://127.0.0.1:12525?url=<urlencode(目标URL)>

PORT=12525
PROXY=
DNS=
EOF

    ( cd "$DIST_DIR"
      if [ "$goos" = "windows" ]; then
          zip -qr "${package_name}.zip" "$package_name"
          echo -e "${GREEN}✓ 发布包: dist/${package_name}.zip${NC}"
      else
          tar -czf "${package_name}.tar.gz" "$package_name"
          echo -e "${GREEN}✓ 发布包: dist/${package_name}.tar.gz${NC}"
      fi )
    rm -rf "$package_dir"
}

# 生成插件市场固定命名二进制（build/ 目录）
build_market() {
    echo -e "${BLUE}生成插件市场二进制...${NC}"
    for entry in "${MARKET_TARGETS[@]}"; do
        local suffix=${entry%%:*}
        local platform=${entry#*:}
        local goos=${platform%%/*}
        local goarch=${platform##*/}
        local out="$BUILD_DIR/${APP_NAME}-${suffix}"
        go_build "$goos" "$goarch" "$out"
        compress "$out"
        echo -e "${GREEN}✓ build/${APP_NAME}-${suffix}  ($goos/$goarch, $(du -h "$out" | cut -f1))${NC}"
    done
    # 校验和
    ( cd "$BUILD_DIR" && sha256sum ${APP_NAME}-* > SHA256SUMS 2>/dev/null || true )
}

show_help() {
    cat <<EOF
cf-proxy 构建脚本

用法: $0 [选项]

选项:
  -h, --help          显示帮助
  -c, --clean         仅清理构建目录
  -a, --all           构建所有平台 + 插件市场二进制 (默认)
  -m, --market        只构建插件市场二进制
  -p, --platform X/Y  只构建指定平台 (例如 linux/amd64)
  --no-upx            禁用 UPX 压缩

支持平台:
$(for p in "${PLATFORMS[@]}"; do echo "  $p"; done)
EOF
}

main() {
    local mode="all" target="" USE_UPX=1
    while [ $# -gt 0 ]; do
        case $1 in
            -h|--help) show_help; exit 0 ;;
            -c|--clean) cleanup; echo -e "${GREEN}清理完成${NC}"; exit 0 ;;
            -a|--all) mode="all" ;;
            -m|--market) mode="market" ;;
            -p|--platform) mode="one"; target=$2; shift ;;
            --no-upx) USE_UPX=0 ;;
            *) echo -e "${RED}未知参数: $1${NC}"; show_help; exit 1 ;;
        esac
        shift
    done

    echo -e "${BLUE}cf-proxy 构建 v${VERSION} (${GIT_COMMIT})${NC}"
    cleanup
    setup_dirs

    case $mode in
        market) build_market ;;
        one) build_platform "$target" ;;
        all)
            for p in "${PLATFORMS[@]}"; do build_platform "$p"; done
            build_market
            ;;
    esac

    echo
    echo -e "${GREEN}构建完成${NC}"
    ls -1 "$BUILD_DIR" 2>/dev/null | sed 's/^/  build\//'
    ls -1 "$DIST_DIR" 2>/dev/null | sed 's/^/  dist\//'
}

main "$@"
