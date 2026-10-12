@echo off
chcp 65001 >nul
setlocal enabledelayedexpansion
rem cf-proxy Windows 构建脚本 (cmd)
rem 用法: scripts\build.bat [平台]   例如: scripts\build.bat linux/amd64

cd /d "%~dp0.."

set APP_NAME=cf-proxy
set BUILD_DIR=build
set DIST_DIR=dist

for /f "delims=" %%i in ('git describe --tags --always --dirty 2^>nul') do set VERSION=%%i
if "%VERSION%"=="" set VERSION=dev
for /f "delims=" %%i in ('git rev-parse --short HEAD 2^>nul') do set GIT_COMMIT=%%i
if "%GIT_COMMIT%"=="" set GIT_COMMIT=unknown

set LDFLAGS=-s -w -X main.Version=%VERSION% -X main.BuildTime=%DATE%_%TIME% -X main.GitCommit=%GIT_COMMIT%

if "%1"=="" goto all
goto one

:one
set PLATFORM=%1
for /f "tokens=1,2 delims=/" %%a in ("%PLATFORM%") do (
    set GOOS=%%a
    set GOARCH=%%b
)
call :buildone
goto end

:all
for %%p in (linux/amd64 linux/arm64 linux/386 windows/amd64 windows/386 darwin/amd64 darwin/arm64) do (
    for /f "tokens=1,2 delims=/" %%a in ("%%p") do (
        set GOOS=%%a
        set GOARCH=%%b
    )
    call :buildone
)
goto market

:buildone
set OUT=%BUILD_DIR%\%APP_NAME%_%GOOS%_%GOARCH%
if "%GOOS%"=="windows" set OUT=%OUT%.exe
if not exist %BUILD_DIR% mkdir %BUILD_DIR%
echo 构建 %GOOS%/%GOARCH% ...
set CGO_ENABLED=0
go build -ldflags="%LDFLAGS%" -trimpath -o "%OUT%" .
if errorlevel 1 (
    echo   失败: %GOOS%/%GOARCH%
) else (
    echo   完成: %OUT%
)
goto :eof

:market
echo 生成插件市场二进制 ...
set CGO_ENABLED=0
set GOOS=linux& set GOARCH=amd64
go build -ldflags="%LDFLAGS%" -trimpath -o "%BUILD_DIR%\%APP_NAME%-linux" .
set GOOS=windows& set GOARCH=amd64
go build -ldflags="%LDFLAGS%" -trimpath -o "%BUILD_DIR%\%APP_NAME%-win.exe" .
set GOOS=linux& set GOARCH=arm64
go build -ldflags="%LDFLAGS%" -trimpath -o "%BUILD_DIR%\%APP_NAME%-android" .
echo 插件市场二进制完成，见 %BUILD_DIR%\

:end
echo.
echo 构建结束。
endlocal
