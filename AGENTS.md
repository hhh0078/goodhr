# AGENTS.md

此文件为 Codex (Codex.ai/code) 在此代码库中工作时提供指导。

涉及代码改动时，必须自动遵守 /Users/Zhuanz/.codex/skills/yunchuan-dev-standards/SKILL.md；完成验证后只提交本次相关改动，并执行 git push。除非用户明确说不要提交或不要推送。

## 项目概述

GoodHR 助手是一个 Chrome 浏览器扩展，旨在帮助 HR 专业人员在中国招聘平台上自动化候选人筛选和沟通。该扩展支持传统的基于关键词的筛选（免费版）和 AI 驱动的智能筛选（AI高级版）。

## 架构

### 核心组件

**Chrome 扩展结构：**

- `manifest.json` - 扩展清单文件（v3），定义权限和入口点
- `background.js` - Service Worker，处理扩展生命周期和消息传递
- `config.js` - 集中配置文件，包含 API 端点和默认设置
- `popup/` - 扩展弹窗界面（HTML + JavaScript）
- `content_scripts/` - 注入到招聘网站的脚本

**目标平台：**
该扩展适用于多个中国招聘平台：

- Boss直聘 (zhipin.com)
- 拉勾网 (lagou.com)
- 智联招聘 (zhaopin.com)
- 猎聘网 (liepin.com)
- 草浪网 (grasswave.cn)

### 数据流

1. **用户配置**：设置存储在 Chrome 存储中，可选择通过手机绑定同步到服务器
2. **网站检测**：内容脚本检测当前招聘平台并加载相应解析器
3. **候选人处理**：滚动候选人列表，提取信息，应用筛选条件
4. **AI 集成**：高级版本将候选人数据发送到轨迹流动 AI API 进行智能筛选
5. **操作执行**：自动向匹配的候选人发送问候或下载简历

### 关键文件

- `content_scripts/index.js` - 主要内容脚本协调器
- `content_scripts/sites/common.js` - 基础解析器类，包含共享功能
- `content_scripts/sites/boss.js` - Boss直聘平台特定解析器
- `content_scripts/sites/lagou.js` - 拉勾网平台特定解析器
- `content_scripts/resume_downloader.js` - 简历下载功能
- `popup/index.js` - 弹窗界面逻辑，支持双模式

## 开发任务

### 测试扩展

由于这是一个 Chrome 扩展，测试需要：

1. **在 Chrome 中加载**：使用 `chrome://extensions/` 开发者模式加载未打包的扩展
2. **在目标网站测试**：导航到支持的招聘平台验证功能
3. **检查控制台日志**：监控浏览器控制台的错误和调试输出
4. **验证存储**：使用 Chrome 开发者工具的应用程序选项卡检查存储的设置

### 配置管理

- 设置存储在 Chrome 的本地存储 API 中
- 通过 `config.js` 中的 API 端点提供服务器同步
- 用户数据包括岗位、关键词、AI 令牌和筛选偏好
- 配置格式在 `user.json` 结构中定义

### AI 集成

高级版本与轨迹流动 AI 服务集成：

- API 端点：`https://api.siliconflow.cn/v1/chat/completions`
- 需要用户提供的 API 令牌
- 两阶段筛选：基础筛选和详细评估
- 在弹窗界面中可配置提示模板

### 平台解析器

每个招聘平台都有一个继承自 `BaseParser` 的专用解析器：

- 实现网站特定的候选人提取逻辑
- 处理平台特定的 DOM 结构和分页
- 管理反机器人检测和速率限制
- 支持基于关键词和 AI 的筛选模式

## 重要说明

### 安全考虑

- API 令牌在本地存储并传输到外部服务
- 内容脚本可以访问敏感的招聘数据
- 扩展需要对目标网站的广泛主机权限

### 速率限制

- 操作之间内置延迟（3-5秒可配置）
- 随机间隔模拟人类行为
- 候选人互动的频率控制

### 本地化

- 界面主要使用简体中文
- 注释和文档混合使用中英文
- 目标用户是中国 HR 专业人员

## 文件结构概要

```
goodHR_2.0.3/
├── manifest.json              # Chrome 扩展清单
├── background.js              # Service Worker
├── config.js                  # 配置常量
├── popup/
│   ├── index.html            # 弹窗界面，支持双模式
│   └── index.js              # 弹窗逻辑和 AI 配置
├── content_scripts/
│   ├── index.js              # 主要内容脚本
│   ├── resume_downloader.js  # 简历下载功能
│   └── sites/
│       ├── common.js         # 基础解析器类
│       ├── boss.js           # Boss直聘解析器
│       ├── lagou.js          # 拉勾网解析器
│       ├── liepin.js         # 猎聘网解析器
│       ├── zhilian.js        # 智联招聘解析器
│       └── grasswave.js      # 草浪网解析器
├── sounds/                   # 音频文件和库
├── icons/                    # 扩展图标
└── user.json                # 示例用户配置
```
