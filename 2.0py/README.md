# GoodHR 自动化工具

一个基于Python的招聘自动化工具，提供图形用户界面，支持多个招聘平台的自动化操作。

## 功能特点

- 用户友好的GUI界面，基于customtkinter实现
- 支持多招聘平台：BOSS直聘、智联招聘、前程无忧、猎聘
- 自动化候选人筛选和沟通
- 岗位管理功能
- 实时日志记录
- 自动更新检测
- 浏览器自动化集成

## 安装要求

### 系统要求
- Windows/MacOS操作系统
- Python 3.8或更高版本
- Chrome浏览器

### 依赖项
所有依赖项都列在`requirements.txt`文件中，主要包括：
- customtkinter
- selenium
- webdriver_manager
- requests
- pyppeteer
- eel
- keyboard
- pygame

## 安装步骤

1. 克隆或下载本项目
2. 安装依赖：
   ```
   pip install -r requirements.txt
   ```
3. 确保已安装Chrome浏览器

## 配置说明

本工具使用以下配置文件：
- `config.json` - 主配置文件，包含用户设置和岗位信息
- `ai_config.json` - AI功能相关配置
- `platform_config.json` - 各平台特定配置

首次运行时，需要设置：
1. 招聘平台选择
2. 登录手机号
3. Chrome浏览器路径

## 使用方法

1. 运行程序：
   ```
   python main.py
   ```
2. 输入手机号码（GoodHR注册手机号）
3. 选择招聘平台
4. 选择相应的岗位
5. 点击"开始运行"按钮

## 功能模块

- **候选人处理** (`candidate_handler.py`) - 处理候选人数据
- **自动化流程** (`automation.py`) - 实现核心自动化功能
- **平台配置** (`platform_configs.py`) - 管理不同招聘平台的配置信息
- **AI分析器** (`ai_analyzer.py`) - 提供AI驱动的分析功能
- **Web界面** (`eel_app.py`) - 基于Eel的Web界面组件

## 快捷键

- **ESC** - 紧急停止当前操作
- **空格** - 暂停/继续操作

## 故障排除

- 如果浏览器自动化失败，请确保已设置正确的Chrome浏览器路径
- 日志文件保存在`运行日志logs`目录下，用于排查问题
- 浏览器配置文件保存在`browser_profile`目录下

## 版本信息

版本更新信息记录在`version_data.json`文件中。

## 更多信息

本工具是GoodHR招聘助手系统的核心组件，需要配合鼠标控制工具(`2.0/goodhrPython`)使用以实现完整功能。 