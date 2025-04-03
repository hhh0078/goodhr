@echo off
chcp 65001

echo 正在检查Python环境...
python --version >nul 2>&1
if errorlevel 1 (
    echo 未检测到Python，请安装Python 3.7或更高版本
    pause
    exit /b 1
)

echo 正在安装依赖包...
python -m pip install -r requirements.txt

echo 正在启动程序...
python mouse_control.py
pause 