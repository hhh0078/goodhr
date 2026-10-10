<!-- 文件作用说明：记录招聘下载自动导入的账号边界、恢复行为、本地格式支持和客户端发布要求。 -->

# 下载简历自动导入

GoodHR 托管 Firefox 的原生下载观察器提供来源与文件路径，Go 下载导入队列复用现有 AI 批量导入管理器。云端只收到结构化简历和站内消息，原件、截图和本地路径不会上传。

- 只导入本次程序启动并绑定当前账号后开始的新 PDF、DOC、DOCX 下载。普通独立 Firefox、历史文件、取消下载、未知 CDN 来源不会自动导入。
- 平台域名在各平台目录的 `download_sources.json` 随程序嵌入；按真实主机识别，上传来源地址前删除参数和碎片。
- 账号开关来自 `/api/resume-library/settings`，首次必须收到明确布尔值才能使用 AI；每 30 秒刷新。网页保存后可请求 `POST /api/v1/resume-library/settings/refresh` 立即应用。
- 同账号、同文件 SHA-256 使用固定任务号；重下、重启和消息网络重试均不重复扣费。AI 结果不明确、缺手机号、查重失败或重复均保留本地草稿，发送幂等站内消息。非简历跳过入库。
- 完整姓名与有效手机号、明确 `is_resume:true`、没有重复时自动确认入库。入库响应不明确先读本地收据，随后交由人工核对，不自动再次写入。
- Token 和账号代次校验闭包只存在内存，切号取消旧流程。关闭开关停止自动批次；关闭期间的下载重新开启后不会补扫。
- DOC 是真实 OLE Word 二进制解析，支持中文 UTF-16 和压缩字符，不执行宏和外链。加密、损坏、超限或非 OLE 的伪装 DOC 提示重新导出 DOCX/PDF。单文件 10 MB，提取 5 分钟，正文 20 万字。

`go test ./...` 验证本地逻辑；真实下载和 AI 费用另按发布验收检查。正式客户端版本由 `scripts/package-windows.ps1 -Version <版本> build` 或 `scripts/package-release.sh` 注入 `internal/version.Value`，只部署云端不会更新旧客户端。

新增容器解析依赖 `github.com/richardlehane/mscfb v1.0.4` 及其 `msoleps v1.0.1` 为 Apache-2.0，许可证及版权说明随安装包的 `licenses` 目录交付。
