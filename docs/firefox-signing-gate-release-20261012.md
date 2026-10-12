<!-- 本文件记录 Firefox 未签名包下线、Mozilla 签名门禁、验证结果和后续正式开放条件。 -->

# Firefox 签名门禁发布记录（2026-10-12）

## 问题与处理结论

标准版 Firefox 会拒绝没有 Mozilla 签名的扩展。此前公开的 Firefox 文件是未签名开发包，即使把 ZIP 改成 XPI，也不能在标准版 Firefox 中长期安装。

本轮先关闭公开下载入口，再准备签名流程。Mozilla 尚未完成签名时，官网、公开接口和后台配置都不能把未签名文件当作正式 Firefox 插件提供。

## 代码基线

- 提交：`042fb663edfa58c3fb6342f1b5b82f8e10d16242`（`拦截未签名火狐插件并准备Mozilla签名`）。
- 插件版本继续使用统一版本 `1.0.7`。如果 Mozilla 提示该版本已存在，需要同时提升 Chrome 和 Firefox 的统一版本号。

### 后端公开门禁

- Firefox 下载信息只有同时满足以下条件才会通过公开接口返回：
  - `firefox_signed=true`；
  - 下载地址是完整的 HTTPS 地址；
  - 文件路径以 `.xpi` 结尾；
  - SHA-256 是 64 位小写十六进制。
- 任一条件不满足时，公开接口会把 Firefox 地址和摘要清空，并返回 `firefox_signed=false`。
- Firefox 门禁不修改 Chrome 下载地址、摘要或其它公开字段。

### 前端下载门禁

- 下载页只在后端返回完整已签名 Firefox 信息时显示 Firefox 下载按钮。
- 未签名或信息不完整时，只显示“正在完成 Mozilla 签名，签名通过后开放下载”，不再提供临时安装入口。
- Firefox 安装教程只说明安装 Mozilla 签名后的 XPI，不再引导普通用户进入 `about:debugging` 临时载入。

### Firefox 独立构建与 AMO 流程

- `npm run build:firefox:unsigned` 只生成待提交 Mozilla 的 Firefox 包，输出目录为 `dist-firefox-unsigned`。
- 待签名文件名明确带 `unsigned`，构建元数据同时标记 `signed=false`、`publishable=false` 和 `temporary-only`，避免被误传到官网下载配置。
- Firefox 独立构建不会改动现有 Chrome 正式包。
- `npm run lint:firefox` 使用固定版本的 Mozilla `web-ext` 检查待签名目录。
- `npm run sign:firefox` 只从运行环境读取 AMO API 信息，仓库、文档、构建产物和发布记录均不得保存任何凭据。
- 详细人工提交和签名后门禁见 `goodhr5/browser-extension/AMO-SUBMISSION.md`。

## 当前生产配置

为避免用户继续下载无法安装的文件，生产配置已先执行以下收口：

- `url_firefox` 已清空；
- `sha256_firefox` 已清空；
- `firefox_signed=false`；
- Chrome 下载配置保持不变。

Mozilla 登录、提交和签名尚未完成，当前不能重新开放 Firefox 下载入口。

## 已完成验证

- 浏览器插件单元测试：`90/90` 通过。
- Firefox 独立待签名构建：通过；生成的未签名 ZIP/XPI 均为 `80412` 字节，SHA-256 为 `0b6049e02ba317f5606c30a4861e4a668a851a12bcb6e42968b269d6b1435ea1`。该摘要只用于确认待签名输入，不得写回正式下载配置。
- Mozilla `web-ext 10.7.0` 校验：`0` 个错误、`0` 个提示、`1` 个警告。唯一警告是 Firefox Android 需要 142 才支持当前数据收集声明；桌面 Firefox 最低版本仍为 140。该警告不影响本轮未签名文件作为 AMO 输入包。
- 后端定向测试通过：已签名 HTTPS XPI 可公开；未签名、HTTP、相对地址、非 XPI 和错误摘要都会被隐藏；Chrome 字段保持不变。
- 前端单元测试：`32/32` 通过。
- 下载页已增加已签名与未签名两种合成配置用例：已签名时展示 XPI 安装入口，未签名时不渲染下载链接或临时载入说明。

## Mozilla 签名后开放步骤

1. 登录 Mozilla Add-ons Developer Hub，选择 **Self-distribution / Unlisted**。
2. 提交 `dist-firefox-unsigned/goodhr-resume-firefox-unsigned-1.0.7.zip`。测试账号只能填写在 AMO 私密审核说明中。
3. 从 Mozilla 下载真正的已签名 XPI，不能把本地 ZIP 改名代替。
4. 使用标准版 Firefox 安装并重启，确认插件仍存在，扩展 ID 和版本正确。
5. 计算签名 XPI 的文件大小和 SHA-256，使用包含摘要的不可覆盖文件名上传 OSS，并确认响应类型为 `application/x-xpinstall`。
6. 从公网重新下载并复算 SHA-256，必须与 Mozilla 签名文件一致。
7. 最后才填写 `url_firefox`、`sha256_firefox` 并设置 `firefox_signed=true`；Chrome 地址和摘要保持不变。

## 生产容器切换

- 新后端镜像：`sha256:460bd02f27019081f14884f3b70a874abf0e9d46f2eaae16ea5543e19f438314`；容器 ID：`d291cb79b6d5759e05dfd5f678d7e7ccc5b3faee0d5722ea8a2e1741b69d95bc`；启动时间：`2026-10-12T08:06:42Z`；重启次数：`0`。
- 新前端镜像：`sha256:3912bb13143df51027d9129155bccc75f5fa57049c569327cb21b6b0b8015af1`；容器 ID：`dd48675a52e1f6e310e9786e34a441bf6b9aa08f3d2f8c736da6870b29f2e0cf`；启动时间：`2026-10-12T08:06:44Z`；重启次数：`0`。
- 两个镜像的发布标签均为 `firefox-signing-gate-042fb663`，源码提交均为 `042fb663edfa58c3fb6342f1b5b82f8e10d16242`。
- 服务器证据目录：`/opt/goodhr-release/firefox-signing-gate-042fb663`，目录内保存发布前后容器检查、构建日志、候选验证、公网响应和回滚脚本，权限已收紧。
- 旧后端和旧前端分别保留为停止状态的回滚容器：`goodhr-production-backend-1-backup-firefox-signing-gate-042fb663`、`goodhr-production-frontend-1-backup-firefox-signing-gate-042fb663`；一键回滚脚本为证据目录内的 `rollback.sh`。
- 首次切换的健康检查端口与生产实际端口不一致，自动回滚正常生效；按生产实际 `127.0.0.1:8086` 修正后完成切换。
- PostgreSQL 和 Redis 的容器 ID、镜像 ID、启动时间与发布前完全一致，本次没有重建或重启依赖服务。

## 最终公网验证

- `/`、`/download`、`/login`、`/admin/resumes` 均返回 `200`。
- `/api/system/browser-extension` 返回 `200`，已隐藏 Firefox 地址和摘要，并返回 `firefox_signed=false`；响应包含 `Cache-Control: no-store`。
- `/download#browser-extension` 不再提供 Firefox 未签名文件或 `about:debugging` 临时安装说明，只显示 Mozilla 签名进度。
- Chrome 下载地址继续指向 `https://oss2.58it.cn/GoodHR-Resume-Chrome-v1.0.7-e849548a.zip`，SHA-256 仍为 `e849548a44d9d0f5c29fd6bc4b24730800182953d95a5d4c5230f3cb4340109d`。
- 未登录访问私有候选人接口仍返回 `401`。
- 正式签名 XPI 的公网响应类型、大小、SHA-256、标准 Firefox 安装及重启保留验证：等待 Mozilla 签名后补充。
