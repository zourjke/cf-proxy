# cf-proxy drpy-node 插件市场打包脚本（python 3）
#
# 单平台出包：安卓包 id=cf-proxy（bin/ 只含 ELF），win 包 id=cf-proxy-win
# （bin/ 只含 PE）—— 一个条目一个平台，包内不含对方平台死重。
# 市场条目 id 必须逐字等于包内 plugin.json 的 name。
#
# 用法：
#   go 构建 win：CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build \
#     -ldflags="-s -w -X main.Version=$(git describe --tags --always)" \
#     -trimpath -o build/cf-proxy-win.exe .
#   安卓包：python market_zip.py --ver 1.0.0 --platform android --android build/cf-proxy-android
#   win 包：python market_zip.py --ver 1.0.0 --platform win32   --win build/cf-proxy-win.exe
import argparse
import hashlib
import json
import os
import sys
import zipfile

HERE = os.path.dirname(os.path.abspath(__file__))

# name 按平台定死，binaries 只声明当前平台键。
# 包内布局：二进制落 bin/ 目录，binaries 值为纯文件名（不带 bin/ 前缀）——
# 本体 _findEntry 先查 bin/<file> 再查包根，纯文件名对老/新本体都命中 bin/<file>。
PLATFORM_SPEC = {
    'android': {
        'name': 'cf-proxy',
        'bin_arg': '--android',
        'binary': 'cf-proxy-android',
        'platform_key': 'android',
    },
    'win32': {
        'name': 'cf-proxy-win',
        'bin_arg': '--win',
        'binary': 'cf-proxy-win.exe',
        'platform_key': 'win32',
    },
}

PORT = 12525


def plugin_json(ver: str, name: str, platform_key: str, binary: str) -> str:
    return json.dumps({
        'name': name,
        'version': ver,
        'title': 'CF 过盾代理',
        'desc': 'Cloudflare 过盾代理：uTLS 指纹伪装 + HTTP/3 + DNS 优选 + 智能重试',
        'author': 'zourjke',
        'runtime': 'binary',
        'params': f'-port {PORT}',
        'env': {},
        'binaries': {platform_key: binary},
        'homepage': 'https://github.com/zourjke/cf-proxy',
        '_source': 'cf-proxy-release',
        'installMode': 'import',
        'portField': '-port',
        'healthCheck': {
            'type': 'http',
            'path': '/health',
            'port': PORT,
            'timeoutMs': 3000,
        },
    }, ensure_ascii=False, indent=2)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--ver', required=True)
    ap.add_argument('--platform', required=True, choices=sorted(PLATFORM_SPEC))
    ap.add_argument('--android', help='cf-proxy-android ELF 路径')
    ap.add_argument('--win', help='cf-proxy-win.exe 路径')
    args = ap.parse_args()

    spec = PLATFORM_SPEC[args.platform]
    src = getattr(args, spec['bin_arg'].lstrip('-'))
    if not src:
        sys.exit(f"platform={args.platform} 需要 {spec['bin_arg']} 指定二进制")
    if not os.path.isfile(src):
        sys.exit(f'缺二进制: {src}')

    name = spec['name']
    out_dir = os.path.join(HERE, 'dist')
    os.makedirs(out_dir, exist_ok=True)
    out = os.path.join(out_dir, f'{name}-{args.ver}.zip')
    if os.path.isfile(out):
        os.remove(out)
    with zipfile.ZipFile(out, 'w', zipfile.ZIP_DEFLATED, compresslevel=9) as zf:
        zi = zipfile.ZipInfo('plugin.json')
        zi.external_attr = 0o644 << 16
        zi.compress_type = zipfile.ZIP_DEFLATED
        zf.writestr(zi, plugin_json(
            args.ver, name, spec['platform_key'], spec['binary']).encode('utf-8'))
        zi = zipfile.ZipInfo(f"bin/{spec['binary']}")
        zi.external_attr = 0o755 << 16
        zi.compress_type = zipfile.ZIP_DEFLATED
        with open(src, 'rb') as f:
            zf.writestr(zi, f.read())

    md5 = hashlib.md5(open(out, 'rb').read()).hexdigest()
    print(f'产物: {out}（{os.path.getsize(out)} bytes）')
    print(f'md5: {md5}')


if __name__ == '__main__':
    main()
