<!-- 文件作用：记录前程无忧交互修复、Mac 6.2.12.4 与岗位本人优先排序的正式发布、验证范围及回退位置。 -->
# 前程无忧、Mac 与岗位排序发布记录

## 发布结果

- Mac：6.2.12.4，可选补丁更新；源码 `258db45502c0a081be0aada33c1ce617c8e9f035`。
- 正式前后端：`positions-own-first-94543893-20261106`，源码 `94543893402855010aabc49a498c07082867a4d2`。
- 岗位列表优先显示当前账号的岗位，各组再按修改时间倒序；缺失或无效时间沿用创建时间与编号兜底。
- 前程无忧“索要简历”对应原生“邀投递”，核对发送结果并避免同岗位重复邀请。
- 前程无忧关闭候选人详情、会话及异常清理后立即移走鼠标，避免关闭位置触发悬浮卡片。平台启用规则位于 `internal/platform/job51/close.go`，通用 Worker 仅提供鼠标操作能力。
- Windows 最新 6.2.12.5 记录、全部 60 条旧更新记录及其他配置保持原样。

## 验证

- 前程无忧已登录页面实测原生邀请；隔离 Camoufox 页面验证旧关闭方式出现悬浮卡、新方式详情和会话关闭后均不出现。
- Go 测试、Worker 回归及 Windows amd64、macOS arm64/amd64 编译通过。
- 岗位本人优先、组内时间排序、刷新和成功启动后重新排序，以及 1366px/390px 后台统一控件测试共 5 项通过。
- 正式源码前端 149 项单测、类型检查和后端 `go test ./...` 通过。
- Mac PKG/ZIP 的 2240 个载荷文件一致；Apple Silicon 原生与 Intel Rosetta 的 Agent、Worker、原生模块和版本健康检查通过，测试进程均退出。
- Mac 安装包仍未签名；没有物理 Intel 机器或真实安装验证，Rosetta 与交叉编译不能代替这些验证。
- 隔离恢复库 57 张表全量行摘要保持一致，新版和完整旧版前后端分别通过启动、页面、匿名鉴权检查；零新增迁移。日志表首次超过 90 秒限制，保留失败现场后，仅将隔离校验超时延长至 600 秒重新验收。
- 正式入口先 `--build-only`，隔离验收后以同一标签 `--activate-only`；未恢复或覆盖生产数据库。公开页面/API 10 项、实际后台静态脚本 27 项通过；18 个其他运行服务保持原容器、镜像及启动时间。

## 安装包与证据

- [Mac 安装包](https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.2.12.4-mac-universal-ab6fde72.pkg)：59339576 字节，SHA256 `ab6fde7246bb57fb29cc4d1a5f07b9f240d188bb05780c1008c5a27024ebe787`。
- [Mac ZIP](https://oss2.58it.cn/goodhr-local-agent-v6.2.12.4-darwin-universal-33f1fb41.zip)：61173638 字节，SHA256 `33f1fb4104676588254f1f4be91b33bd5e4e400c2c733596bad769ec341ce20b`。
- 本机验证：`/tmp/goodhr-release-20261106/`、`/tmp/goodhr-mac-62124-release-20261106-audit/`、`/tmp/goodhr-job51-close-mouse-20261106/`。
- 正式发布与兼容登记：`/opt/goodhr-release/positions-own-first-94543893-20261106/`。
- Mac 配置前后快照及发布证明：`/opt/goodhr-release/mac-6.2.12.4-258db455-20261106-r2/`；私密配置不进入仓库。

## 回退与空间收尾

- 原前后端完整回退镜像：后端 `sha256:2d53bf9e2aef4cad7534da3e77a348b710fcdbfacc30554f9d92b9d8fab6b377`，前端 `sha256:b213e43b6aa09a002e48f17e2b21f21ce289f94b1282cfeb330b51f63db6b84e`；原容器保留为本次发布标签的 backup。
- 本轮成功及失败的临时容器、网络均按精确身份移除；失败恢复目录、原始 dump、SQL、源码归档及审计材料保留。成功恢复副本仅通过固定 SHA 的 hygiene 计划回收。
- 新兼容登记保护当前版本、候选和回退后，按精确计划回收 2 个旧停止容器与 12 个旧镜像/缓存对象；未运行全局 prune、强制删除或数据卷删除。
- 9 份超过七天的安装包上传副本完整归档至 `/opt/goodhr-release/retained-upload-copies-20261106/`，公开原件不变。
- 25 个无容器引用的历史备份完整跨盘保留至 `/opt/goodhr-release/retained-goodhr-production-backups-20261106/`，逐文件核对摘要、权限、属主与修改时间；原 `goodhr5/backups/production` 路径用链接继续访问，未丢弃备份。
- 发布后根盘可用 6509293568 字节（约 6.06GiB），Docker 分区可用 3281379328 字节（约 3.06GiB）；共 27 个容器、105 个镜像，其他项目保持不变。
