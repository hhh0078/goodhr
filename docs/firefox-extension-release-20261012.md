<!-- 本文件记录 2026-10-12 Firefox 简历插件同功能发布、验证结果和生产回滚依据。 -->

# Firefox 简历插件 1.0.7 发布记录

## 发布内容

- Firefox 与 Chrome 共用同一套 `src` 和图标资源，具备自动识别简历、手机号与模糊查重、头像保存、备注、岗位推荐、AI 总结和入库后自动关闭。
- Firefox 清单保留浏览器差异配置，关闭首次安装自动打开侧栏，并声明读取可见简历所需的数据类型。
- 下载页按签名状态展示 Firefox 安装教程。本次没有 Mozilla 签名凭据，因此发布为未签名临时测试包：通过 `about:debugging#/runtime/this-firefox` 载入 ZIP，Firefox 重启后需要重新载入。
- Chrome 1.0.7 安装包及其校验值未改变。

## 代码与安装包

- 代码提交：`149261f0ecce14f0aa2fdb8a54dd0de6268cab32`
- Firefox 下载地址：`https://oss2.58it.cn/GoodHR-Resume-Firefox-v1.0.7-0b6049e0.zip`
- Firefox 文件大小：`80412` 字节
- Firefox SHA-256：`0b6049e02ba317f5606c30a4861e4a668a851a12bcb6e42968b269d6b1435ea1`
- Chrome SHA-256：`e849548a44d9d0f5c29fd6bc4b24730800182953d95a5d4c5230f3cb4340109d`

## 验证结果

- 插件单元测试：89 项全部通过。
- 插件正式构建：通过；Firefox 公共 ZIP 的大小和 SHA-256 与本地构建一致。
- 前端单元测试：32 项全部通过。
- TypeScript 类型检查和 Next.js 正式构建：通过。
- 下载页与简历库定向浏览器测试：4 项全部通过。
- Firefox/Chrome 共享资源逐字节一致，Firefox 后台、侧栏和头像转换兼容测试通过。
- 生产候选容器的 `/login`、`/admin/resumes`、`/download` 均返回 200。
- 公网下载页已展示 Firefox 1.0.7 下载入口、临时测试包说明和安装教程；公网插件包大小与 SHA-256 校验通过。
- 未登录访问 `/api/candidates` 返回 401。
- 当前机器没有可用的 Firefox/Camoufox 运行环境，且官方下载耗时异常，未完成真实 Firefox 浏览器启动验收。此限制不影响已完成的构建、兼容测试和公网下载校验。

## 生产发布与回滚

- 新前端镜像：`sha256:0aad9b469682169bf9ba4c3b46fc6ab6a60e419110172a48b2ea18f1c64b7ef7`
- 发布标签：`firefox-extension-1.0.7-149261f0`
- 服务器证据目录：`/opt/goodhr-release/firefox-extension-1.0.7-149261f0`
- 旧前端容器：`goodhr-production-frontend-1-backup-firefox-extension-20261012-149261f0`
- 旧前端镜像：`sha256:db0164b7771ed048e15efcb8a294544f3948b97bb39f71e31aefa95a0de26282`
- 配置回滚脚本：`/opt/goodhr-release/firefox-extension-1.0.7-149261f0/rollback-browser-extension.sql`
- 后端、PostgreSQL 和 Redis 容器的 ID、镜像、启动时间保持不变。
