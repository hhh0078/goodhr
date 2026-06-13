@echo off
REM 文件作用：Windows 一键检查环境并打包 GoodHR Go 本地程序安装包。
setlocal EnableExtensions

set "ROOT_DIR=%~dp0"
set "VERSION=%~1"
if "%VERSION%"=="" set "VERSION=0.1.0"

echo [GoodHR] 开始打包 Windows 安装包，版本：%VERSION%
echo [GoodHR] 项目目录：%ROOT_DIR%
echo.

REM 检查 Go 是否已安装。
where go >nul 2>nul
if errorlevel 1 (
  echo [GoodHR][错误] 未检测到 Go。
  echo 请先安装 Go，然后重新打开命令行再运行本脚本。
  echo 下载地址：https://go.dev/dl/
  pause
  exit /b 1
)

for /f "tokens=*" %%i in ('go version') do set "GO_VERSION=%%i"
echo [GoodHR] 已检测到 Go：%GO_VERSION%

REM 检查 Inno Setup 6 是否已安装。
set "ISCC_PATH="
where ISCC.exe >nul 2>nul
if not errorlevel 1 (
  for /f "tokens=*" %%i in ('where ISCC.exe') do (
    if not defined ISCC_PATH set "ISCC_PATH=%%i"
  )
)
if not defined ISCC_PATH if exist "%ProgramFiles(x86)%\Inno Setup 6\ISCC.exe" set "ISCC_PATH=%ProgramFiles(x86)%\Inno Setup 6\ISCC.exe"
if not defined ISCC_PATH if exist "%ProgramFiles%\Inno Setup 6\ISCC.exe" set "ISCC_PATH=%ProgramFiles%\Inno Setup 6\ISCC.exe"

if not defined ISCC_PATH (
  echo [GoodHR][错误] 未检测到 Inno Setup 6。
  echo 请先安装 Inno Setup 6，再运行本脚本。
  echo 下载地址：https://jrsoftware.org/isdl.php
  pause
  exit /b 1
)
echo [GoodHR] 已检测到 Inno Setup：%ISCC_PATH%

REM 检查 Worker 目录和依赖。
if not exist "%ROOT_DIR%worker-node\package.json" (
  echo [GoodHR][错误] 未找到 worker-node\package.json。
  echo 请确认当前目录是 goodhr5\local-agent-go。
  pause
  exit /b 1
)

if not exist "%ROOT_DIR%worker-node\node_modules" (
  echo [GoodHR][错误] 未检测到 worker-node\node_modules。
  echo 请先执行下面命令安装 Node Worker 依赖：
  echo.
  echo   cd /d "%ROOT_DIR%worker-node"
  echo   npm install
  echo.
  pause
  exit /b 1
)
echo [GoodHR] Worker 依赖已存在。

REM 检查 PowerShell 是否可用。
where powershell >nul 2>nul
if errorlevel 1 (
  echo [GoodHR][错误] 未检测到 PowerShell，无法调用打包脚本。
  pause
  exit /b 1
)

echo.
echo [GoodHR] 开始调用安装器打包脚本...
powershell -NoProfile -ExecutionPolicy Bypass -File "%ROOT_DIR%packaging\build_windows_installer.ps1" -Version "%VERSION%"
if errorlevel 1 (
  echo.
  echo [GoodHR][错误] 打包失败，请查看上面的错误信息。
  pause
  exit /b 1
)

echo.
echo [GoodHR] 打包完成。
echo [GoodHR] 安装包目录：%ROOT_DIR%dist-installer
echo.
pause
exit /b 0
