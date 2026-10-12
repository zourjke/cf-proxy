<#
.SYNOPSIS
    cf-proxy 跨平台构建脚本 (PowerShell)
.DESCRIPTION
    产出与 scripts/build.sh 一致的目录结构：
      build/  —— 插件市场固定命名二进制 (cf-proxy-linux / cf-proxy-win.exe / cf-proxy-android)
      dist/   —— 各平台发布包 (zip)
.EXAMPLE
    powershell -ExecutionPolicy Bypass -File scripts\build.ps1
    powershell -ExecutionPolicy Bypass -File scripts\build.ps1 -Platform linux/amd64
    powershell -ExecutionPolicy Bypass -File scripts\build.ps1 -MarketOnly
#>
param(
    [string]$Platform = "",
    [switch]$MarketOnly,
    [switch]$NoUpx
)

$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")

$AppName   = "cf-proxy"
$BuildDir  = "build"
$DistDir   = "dist"

$Version = (git describe --tags --always --dirty 2>$null); if (-not $Version) { $Version = "dev" }
$GitCommit = (git rev-parse --short HEAD 2>$null); if (-not $GitCommit) { $GitCommit = "unknown" }
$BuildTime = (Get-Date).ToUniversalTime().ToString("yyyy-MM-dd_HH:mm:ss") + "_UTC"

$LdFlags = "-s -w -X main.Version=$Version -X main.BuildTime=$BuildTime -X main.GitCommit=$GitCommit"

$Platforms = @(
    "linux/amd64", "linux/arm64", "linux/386",
    "windows/amd64", "windows/386",
    "darwin/amd64", "darwin/arm64",
    "freebsd/amd64"
)

# 插件市场固定命名：后缀 -> 平台
$MarketTargets = [ordered]@{
    "linux"   = "linux/amd64"
    "win.exe" = "windows/amd64"
    "android" = "linux/arm64"
}

function Invoke-GoBuild([string]$Goos, [string]$Goarch, [string]$Out) {
    $env:GOOS = $Goos
    $env:GOARCH = $Goarch
    $env:CGO_ENABLED = "0"
    Write-Host "构建 $Goos/$Goarch -> $Out"
    & go build -ldflags="$LdFlags" -trimpath -o $Out .
    if ($LASTEXITCODE -ne 0) { throw "构建失败: $Goos/$Goarch" }
}

function Invoke-Upx([string]$File) {
    if ($NoUpx) { return }
    if (Get-Command upx -ErrorAction SilentlyContinue) {
        & upx --best --lzma $File 2>$null | Out-Null
    }
}

function New-ReleasePackage([string]$Goos, [string]$Goarch, [string]$Binary) {
    $name = "$AppName`_$Version`_$Goos`_$Goarch"
    $dir = Join-Path $DistDir $name
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    Copy-Item $Binary $dir
    if (Test-Path "README.md") { Copy-Item "README.md" $dir }

    if ($Goos -eq "windows") {
        Set-Content -Path (Join-Path $dir "start.bat") -Encoding ASCII -Value @"
@echo off
echo Starting cf-proxy on port 12525...
cf-proxy.exe -port 12525
pause
"@
    } else {
        $sh = Join-Path $dir "start.sh"
        Set-Content -Path $sh -Encoding ASCII -Value @"
#!/bin/sh
echo "Starting cf-proxy on port 12525..."
exec ./cf-proxy -port 12525
"@
    }

    $zip = Join-Path $DistDir "$name.zip"
    if (Test-Path $zip) { Remove-Item $zip }
    Compress-Archive -Path $dir -DestinationPath $zip
    Remove-Item -Recurse -Force $dir
    Write-Host "发布包: $zip" -ForegroundColor Green
}

# ---- 主流程 ----
Write-Host "cf-proxy 构建 v$Version ($GitCommit)" -ForegroundColor Cyan

if (Test-Path $BuildDir) { Remove-Item -Recurse -Force $BuildDir }
if (Test-Path $DistDir)  { Remove-Item -Recurse -Force $DistDir }
New-Item -ItemType Directory -Force -Path $BuildDir, $DistDir | Out-Null

& go mod tidy | Out-Null

if ($MarketOnly) {
    foreach ($suffix in $MarketTargets.Keys) {
        $p = $MarketTargets[$suffix] -split "/"
        $out = Join-Path $BuildDir "$AppName-$suffix"
        Invoke-GoBuild $p[0] $p[1] $out
        Invoke-Upx $out
        Write-Host "  build/$AppName-$suffix" -ForegroundColor Green
    }
} elseif ($Platform) {
    $p = $Platform -split "/"
    $ext = if ($p[0] -eq "windows") { ".exe" } else { "" }
    $out = Join-Path $BuildDir "$AppName`_$($p[0])_$($p[1])$ext"
    Invoke-GoBuild $p[0] $p[1] $out
    Invoke-Upx $out
    New-ReleasePackage $p[0] $p[1] $out
} else {
    foreach ($pf in $Platforms) {
        $p = $pf -split "/"
        $ext = if ($p[0] -eq "windows") { ".exe" } else { "" }
        $out = Join-Path $BuildDir "$AppName`_$($p[0])_$($p[1])$ext"
        Invoke-GoBuild $p[0] $p[1] $out
        Invoke-Upx $out
        New-ReleasePackage $p[0] $p[1] $out
    }
    foreach ($suffix in $MarketTargets.Keys) {
        $p = $MarketTargets[$suffix] -split "/"
        $out = Join-Path $BuildDir "$AppName-$suffix"
        Invoke-GoBuild $p[0] $p[1] $out
        Invoke-Upx $out
        Write-Host "  build/$AppName-$suffix" -ForegroundColor Green
    }
}

Write-Host "构建完成。" -ForegroundColor Green
