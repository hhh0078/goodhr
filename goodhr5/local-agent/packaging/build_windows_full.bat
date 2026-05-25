@echo off
REM 本脚本用于在 Windows 上一键打包 GoodHR Local Agent。

setlocal enabledelayedexpansion

cd /d "%~dp0\.."

echo ==^> 当前目录：%CD%

if not exist ".venv\Scripts\python.exe" (
  echo ==^> 创建 Python 虚拟环境 .venv
  python -m venv .venv
  if errorlevel 1 exit /b 1
)

set "PYTHON=.venv\Scripts\python.exe"

echo ==^> 配置 pip 国内镜像
"%PYTHON%" -m pip config set global.index-url https://mirrors.aliyun.com/pypi/simple >nul
"%PYTHON%" -m pip config set install.trusted-host mirrors.aliyun.com >nul

echo ==^> 升级 pip
"%PYTHON%" -m pip install -U pip
if errorlevel 1 exit /b 1

echo ==^> 安装运行和打包依赖
"%PYTHON%" -m pip install -e ".[packaging]"
if errorlevel 1 exit /b 1

echo ==^> 准备 Windows CloakBrowser
"%PYTHON%" packaging\prepare_vendor.py --platform win --no-extract
if errorlevel 1 exit /b 1

echo ==^> 开始 PyInstaller 打包
"%PYTHON%" -m PyInstaller --clean --noconfirm --distpath dist --workpath build packaging\GoodHRLocalAgent.spec
if errorlevel 1 exit /b 1

echo ==^> 打包完成
echo 产物位置：%CD%\dist\GoodHRLocalAgent

endlocal
