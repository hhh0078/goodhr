<!-- 文件作用：记录简历插件 1.0.18 的悬浮入口规则、双浏览器验证、OSS2 发布和配置回退位置。 -->

# 简历插件 1.0.18 发布记录

## 使用变化

- 未登录：只显示可拖动的 GoodHR logo，点击进入插件独立登录页。
- 已登录但没有完整手机号：只显示 logo，点击打开原表单，手动补号。
- 已登录且检测到完整手机号：自动展开表单。
- 保留原候选人页面范围、身份隔离、同人手动收起、换人和保存后的处理规则。资料消失时隐藏入口；收起卡片后保留 logo。
- BOSS 附件只在独立号码行或明确联系方式行出现唯一完整号码时触发自动展开，掩码、多号码和业务编号不触发。判断只读取现有可见文字，不调用 AI 或新增主世界读取。
- Chrome、Firefox 共用业务代码，版本从 1.0.17 升为 1.0.18。

## 发布结果

2026-11-12 通过现有宝塔 MCP 的 `Upload`、`Bash` 上传和发布，文件位于 `/www/wwwroot/oss2.58it.cn/`。本次只发布插件文件及修改插件配置。

| 文件 | 字节数 | SHA256 |
| --- | --- | --- |
| [Chrome ZIP](https://oss2.58it.cn/goodhr-resume-chrome-1.0.18.zip) | 118436 | `2db9f6ed3da6f5b861a2b18d723b5f709093bebed484f43a0aa80781ebd9903f` |
| [Firefox 临时 XPI](https://oss2.58it.cn/goodhr-resume-firefox-unsigned-1.0.18.xpi) | 113830 | `18614d14e79d6d2addf1b88a018f03a6b8ac8dc922291b196299df7998746037` |
| [Firefox 临时 ZIP](https://oss2.58it.cn/goodhr-resume-firefox-unsigned-1.0.18.zip) | 113830 | `18614d14e79d6d2addf1b88a018f03a6b8ac8dc922291b196299df7998746037` |

- `goodhr5.system_configs` 的 `system.browser_extension` 通过锁行、比较完整旧值的事务更新 `version_chrome`、Chrome 地址、摘要和说明。Firefox 1.0.9 签名配置、旧本地程序兼容配置和其他字段保持原样。
- [Chrome 公开版本接口](https://goodhr.58it.cn/api/system/browser-extension?browser=chrome) 返回 1.0.18，下载地址指向 OSS2。
- [托管 Firefox 临时更新清单](https://goodhr.58it.cn/zip/goodhr-resume-firefox-latest.json) 原子切换为 1.0.18。清单及 XPI 的同站入口通过现有 `/zip/` 目录符号链接指向 OSS2 文件，满足本地程序原有同源下载校验。
- Firefox 1.0.18 为未签名临时包，供托管浏览器或 `about:debugging` 加载；普通 Firefox 正式安装仍使用已有 Mozilla 签名的 1.0.9，未把临时包标成签名包。

## 验证

- `npm test`：172 项全部通过，包含消息权限、捕获时序、资料隔离、保存和原有回归场景。
- Chrome 157.0.8103.39 实际加载扩展通过：真实扩展域登录 iframe、登录消息来源、检测号码后展开、成本确认取消及 BOSS 同源附件读取。
- Firefox 150.0.2 测试内核运行同一组悬浮交互的 10 项检查全部通过，使用 `browser` 命名空间；候选人和接口均为本机合成数据。
- 最后调整测试操作后，Chrome 的登录、补号、草稿保留及候选人切换 3 项重点检查再次通过。
- 已查看桌面与 360px 窄窗口截图。测试显式点击备注框后输入，并先展开缺号候选人再等待表单，避免 Firefox 的焦点和隐藏 iframe 动画帧差异干扰验证。
- MCP 上传返回完整摘要匹配。三个 OSS2 安装包公网完整下载均为 HTTP 200，大小、摘要及包内 1.0.18 清单均匹配；托管 Firefox 的同站 XPI 也完整校验通过。
- 包内全部 `src/` 文件与本次源码逐字节一致；官网下载页返回 HTTP 200。
- 本次没有在真实招聘账号上提交简历，没有提交 Mozilla 签名审核。

## 证据、回退与收尾

服务器 `/opt/goodhr-release/plugin-1.0.18-20261112/` 保存配置行前后快照、预期配置、带并发保护的 `update.sql` / `rollback.sql`、Firefox 旧清单及其文件属性、安装包摘要、公网下载回执和容器磁盘快照。目录仅所有者可访问。

回退 Chrome 时使用该目录的条件 `rollback.sql`，配置不再等于本次结果时停止。托管 Firefox 回退时先核对当前仍为本次 1.0.18 清单，再从 `firefox-latest.before.json` 原子恢复旧入口；保留各版本文件和证据。

- 发布后 14 个运行容器及全部镜像清单与发布前一致。
- 根盘剩余 6268968960 字节（约 5.84 GiB）；OSS2 / Docker 所在 `/www` 剩余 2637811712 字节（约 2.46 GiB，使用率 94%）。
- 本次已经移空的四个 MCP 上传目录已精确移除，未清理其他安装包、镜像、数据库或备份。
