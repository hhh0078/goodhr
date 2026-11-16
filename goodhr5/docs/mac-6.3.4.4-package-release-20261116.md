<!-- 文件作用说明：记录Mac6.3.4.4通用包的源码、完整性、可选更新发布与条件回退依据。 -->

# Mac 6.3.4.4 发布记录

2026-11-16。按用户要求从已推送至 Gitee/GitHub `main` 的干净源码 `c350cc5d2bc5de9f6f3a786809428e489aa7e38f` 打包发布，使用 `scripts/package-release.sh 6.3.4.4 build`。独立 Git 归档构建，不包含原工作区未提交改动；Go/npm 使用国内镜像。

## 修复与更新方式

- 修复前程无忧打招呼后，邀投递结果验证失败会跳过首次招呼语的问题。确认聊天对象后先发送招呼语，再索要资料；聊天身份及发送结果检查继续保留。
- 本次仅发布 Mac 通用包，支持 Apple Silicon 与 Intel；保留现有 Windows 6.3.4.2 及全部历史下载记录。
- 版本使用第四段补丁号，不修改强制升级判断。已用正式前端函数确认 6.3.4、6.3.4.1、6.3.4.2、6.3.4.3 到 6.3.4.4 都是可忽略更新，正常余额下不会因版本拦住岗位启动。更早基础版本的既有升级要求不变。

## 固定产物

| 格式 | 字节数 | SHA256 | 下载 |
| --- | ---: | --- | --- |
| PKG | 60863116 | `b42a2743dcab9f5c46d9d8480e1ce9942cd643ffeed6d61872d76e9c979804f1` | [Mac安装器](https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.3.4.4-mac-universal-b42a2743.pkg) |
| ZIP | 62702933 | `cc23d4ca3a1aaedafba233fb636e1f86c8cabdb6c3236b803b4dc744eaaf2c02` | [Mac应用压缩包](https://oss2.58it.cn/goodhr-local-agent-v6.3.4.4-darwin-universal-cc23d4ca.zip) |

两份产物使用独立摘要文件名，不覆盖旧包。通过宝塔 Upload 返回的固定公钥上传命令传输，服务器复核 SHA256 后移入既有下载目录，权限 0644。

## 验证范围

- App、PKG 和 ZIP 内 2452 项文件、目录、链接及权限清单相同；清单 SHA256 为 `d1c3ebb299de40671f8094e0a01cf67ee31cd57272b9877a6dd279a646cb1940`。
- 两个 Info.plist 版本字段、build-info.json、原生 Go 程序 `--help` 均为 6.3.4.4；正式域名为 `https://goodhr.58it.cn`。
- Go 主程序及 AppKit 启动器均含 arm64/x86_64；两个 impit 原生模块分别核对 Apple/Intel 架构；Worker TypeScript 构建及入口语法检查通过。
- 同源码的 greeting/job51/common 回归及 Windows amd64、macOS arm64/amd64 完整程序编译已通过；本轮另通过 config、updater、system 子目录、主程序相关 Go 测试及 14 项前端启动版本检查测试。
- 未覆盖安装当前运行客户端，未向真实候选人发送测试消息，未执行 Intel Mac/Windows 真机安装运行。PKG 沿用未签名、未公证形式，不能把上述编译及合成回归当作真机实发通过。

## 发布保护与回退

服务器 `/opt/goodhr-release/mac-6.3.4.4-20261116/` 保留发布前整行配置、预期/发布后快照、`update.sql`、`rollback.sql`、源码归档、构建日志、产物与清单摘要、容器和磁盘基线。目录 0700，文件 0600。

配置更新锁定 `system_configs` 的 `system.onboarding_config` 整行，完整比对预期快照；任何并发变化均中止。只在 `local_agent` 首位新增 Mac 6.3.4.4，一条 Windows 下载地址为空；旧六条记录及全部其他配置、行字段保留。回退也必须核对整行仍等于本次发布结果，不能覆盖后续修改。

## 公开发布结果

- PKG 与 ZIP 均从本机经公网 HTTPS 完整读取，HTTP 200；长度及 SHA256 与上表一致，没有用 HEAD 或部分下载代替。
- 公开 `/api/system/local-agent-updates` 与 `/download` 均 HTTP 200，Mac 入口指向本次 6.3.4.4 PKG。复用正式平台选择函数，Windows 仍选到原 6.3.4.2；移除本次首条 Mac 记录后，原六条公开记录逐项一致。
- 数据库发布后整行与预期一致，规范化快照 SHA256 为 `6b1c065c92e4bf3d91ca9d39fb1374713ea05e72d5eca973a54a422efc9e8ca8`。没有修改强制升级算法、Windows 版本或其他配置。
- 发布前后 14 个运行容器的 ID、镜像 ID 和启动时间相同，镜像记录增减均为零，没有构建或切换云端镜像。
- 收尾时根分区可用 21434253312 字节，`/www` 可用 6070759424 字节。相对基线占用分别增加 51314688 和 121966592 字节，包含源码与证据留存、安装包及同期正常写入，不能全部归到构建缓存。
- 打包脚本的临时 stage 已自动清理；PKG/ZIP 验证用的本次解包目录在保留清单后删除。源码归档、成品、日志、配置快照及回退材料保留。
