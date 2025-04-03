# GoodHR 鼠标控制工具

一个基于Python的鼠标控制API服务，为GoodHR自动化工具提供精确的鼠标操作支持。

## 功能特点

- 提供基于Flask的HTTP API接口
- 支持精确的鼠标移动和点击操作
- 支持滚动操作
- 带有可视化鼠标指示器
- 内置权限指南和检查工具

## 安装要求

### 系统要求
- Windows/MacOS操作系统
- Python 3.8或更高版本

### 依赖项
所有依赖项都列在`requirements.txt`文件中，主要包括：
- Flask
- PyAutoGUI
- PyQt5
- requests
- psutil

## 安装步骤

1. 克隆或下载本项目
2. 安装依赖：
   ```
   pip install -r requirements.txt
   ```
3. 在Windows系统中，可直接运行`run_mouse_control.bat`

## 使用方法

### 直接运行
双击`run_mouse_control.bat`或执行以下命令：
```
python mouse_control.py
```

### API接口

启动后，服务将在本地5000端口（或自动选择的可用端口）提供以下API：

- **GET /api/mouse/move** - 移动鼠标
  - 参数: 
    - x: X坐标
    - y: Y坐标
    - duration: 移动持续时间(秒)
    - click: 是否点击 (true/false)

- **GET /api/mouse/scroll** - 滚动鼠标
  - 参数:
    - x: X坐标
    - y: Y坐标
    - scroll_amount: 滚动量（正值向上，负值向下）
    - duration: 操作持续时间(秒)

- **GET /api/health** - 健康检查

## 故障排除

- 如果端口被占用，程序会自动尝试其他端口
- 如果权限问题导致无法操作鼠标，程序会显示权限设置指南
- 请确保已授予程序访问屏幕和控制鼠标的权限

## 版本说明

当前版本：2.0.0

## 更多信息

本工具是GoodHR招聘助手系统的一部分，主要用于支持自动化工具(2.0py)的界面操作。 