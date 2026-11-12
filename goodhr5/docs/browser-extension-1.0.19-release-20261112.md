<!-- 文件作用：记录插件 1.0.19 的左上角位置回归修复、双浏览器验证及 OSS2 发布结果。 -->

# 插件 1.0.19 位置修复

## 问题和修复

1.0.18 中，拖动过 logo 后，切换候选人造成资料短暂消失时，会隐藏整个插件容器。隐藏元素的矩形坐标为零，原逻辑仍据此重新计算位置，把坐标写成 `(8, 8)`；下一位有手机号的候选人自动展开时便出现在左上角。

修复仅让可见容器参与位置计算。没有拖动时仍默认右下角；拖动后保留位置，展开时仅把超出窗口的部分移回。登录、手机号条件、候选人范围和收起规则保持原逻辑。

## 验证

- 修复前新增的位置回归检查稳定失败，横坐标实际为 8，而预期保留约 725 的拖动位置。
- 修复后 `npm test` 的 173 项检查全部通过。
- Firefox 150.0.2 测试内核的位置回归、窄窗口拖动、logo 登录与手机号展开 3 项检查全部通过。
- Chrome 157.0.8103.39 实际加载扩展的登录、手机号展开及 BOSS 附件检查通过。
- 三个公开安装包完整下载均为 HTTP 200，大小和摘要一致；包内全部 `src/` 文件与本次源码一致，并确认包含隐藏位置保护条件。
- 使用本地合成候选人和接口验证，未对真实招聘账号执行保存。

## 发布

2026-11-12，通过宝塔 MCP 上传到 `/www/wwwroot/oss2.58it.cn/`，随后更新后台插件配置和托管 Firefox 清单。

| 文件 | 字节数 | SHA256 |
| --- | --- | --- |
| [Chrome ZIP](https://oss2.58it.cn/goodhr-resume-chrome-1.0.19.zip) | 118579 | `1e077e13dd3b5b7539c6a796916c63e5bb73c672e3fb9356ade1116a8c384b8a` |
| [Firefox 临时 XPI](https://oss2.58it.cn/goodhr-resume-firefox-unsigned-1.0.19.xpi) | 113973 | `ca0c77079e714223f12f5ed1d2b56e6d8d81408bc72cb752a0adf2d3ce998ec0` |
| [Firefox 临时 ZIP](https://oss2.58it.cn/goodhr-resume-firefox-unsigned-1.0.19.zip) | 113973 | `ca0c77079e714223f12f5ed1d2b56e6d8d81408bc72cb752a0adf2d3ce998ec0` |

- 后台 `system.browser_extension` 的 Chrome 版本、下载地址、摘要和说明事务更新为 1.0.19，锁定并核对完整旧值以避免覆盖并发修改。
- [Chrome 版本接口](https://goodhr.58it.cn/api/system/browser-extension?browser=chrome) 与[托管 Firefox 临时清单](https://goodhr.58it.cn/zip/goodhr-resume-firefox-latest.json) 均确认返回 1.0.19，下载页为 HTTP 200。
- 托管 Firefox 的同站清单和包入口继续链接到 OSS2 的不可变文件；正式 Firefox 1.0.9 签名渠道及旧本地程序兼容配置保持原样。
- 配置前后快照、带并发保护的更新及回退 SQL、旧 Firefox 清单和链接属性、公网回执及磁盘容器证据保存在服务器 `/opt/goodhr-release/plugin-1.0.19-20261112/`。
- 14 个运行容器及全部镜像清单与发布前一致。根盘剩余 6267957248 字节（约 5.84 GiB），`/www` 剩余 2628554752 字节（约 2.45 GiB）。本次四个已移空上传目录已精确清理。

更新插件后刷新原招聘页面，使页面加载 1.0.19 的内容脚本。
