@echo off
REM 文件作用说明：从 Windows 命令行调用统一 PowerShell 打包脚本。
setlocal EnableExtensions

set "SCRIPT_DIR=%~dp0"
set "VERSION=%~1"
if "%VERSION%"=="" set "VERSION=6"
set "BUILD_MODE=%~2"
if "%BUILD_MODE%"=="" (
  echo [GoodHR] 请在版本号后填写 dev 或 build，例如 package-windows.bat 6.1.1 build
  exit /b 1
)

where powershell.exe >nul 2>nul
if errorlevel 1 (
  echo [GoodHR] 没找到 PowerShell，Windows 安装包暂时没法生成。
  exit /b 1
)

powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%SCRIPT_DIR%package-windows.ps1" -Version "%VERSION%" -Mode "%BUILD_MODE%"
exit /b %ERRORLEVEL%

