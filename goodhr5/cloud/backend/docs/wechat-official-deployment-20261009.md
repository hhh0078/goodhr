<!-- 本文件记录公众号关注奖励的生产发布、测试依据及回退入口；不保存公众号凭据和数据库密码。 -->
# 公众号关注奖励上线记录（2026-10-09）

## 已发布的功能

未认证个人订阅号使用「扫码关注 → 发送一次绑定码」关联网站账号，服务端从微信签名回调取得 OpenID。首次完成关注绑定赠送 5 天会员；有效 Plus / Max 保留类型与剩余天数，再增加 5 天。网站账号和公众号微信用户均只能领取一次。

未绑定用户可关闭提示，控制台保留「关注领会员」按钮重新打开。已领取过的用户显示「关注公众号」。用户管理通过「OpenID 有 / 无」展示绑定状态。公众号配置集中在「系统配置 → 其他配置」，AES 密钥只保存于服务端，Token 只向超级管理员展示。

## 发布目标与构建

- 服务器：`ldclouda31539`，`43.228.79.108`。
- 站点：`https://goodhr.58it.cn`。
- 产品镜像源提交：`3a8de3b34ca0f8a4cc1fd7ae6e4b86fd31a11d32`。
- 后端镜像：`goodhr-production-backend:wechat-follow-20261009-3a8de3b3`，镜像 ID `sha256:622cc06b73ef2b086a5654033194a5b03b7301decb6b0da4c56d8bcff3532600`。
- 前端镜像：`goodhr-production-frontend:wechat-follow-20261009-3a8de3b3`，镜像 ID `sha256:e30411a4ab65bd98d66dbf0bbe7e0ef3e259228db900bb807510d55efadeac82`。
- 前端生产 BUILD_ID：`kBTC5wAVioDkoEl_66ozH`。
- 正式后端容器 ID：`403eeebffe00f46164a2a7c5ed81585203bc07487ad81f3d9c02dbc7fb468964`。
- 正式前端容器 ID：`d0637495399633cc283b076a11b5398bd1d15d38dbae280544b7fdadf3db5ca7`。

先在隔离端口 8087 / 5176 验证候选容器，再切换正式 8086 / 5175。保留原挂载、网络、启动命令、环境变量、日志与重启策略。候选后端关闭邮件调度，正式后端沿用原调度设置。

数据库启动迁移已执行 `0084_wechat_follow_gift.sql`，迁移记录共 88 条；3 张新表和用户的 3 个新字段中文注释完整。独立核验时用户数 882，新关注状态、绑定码、奖励及原始 OpenID 绑定均为 0，没有向生产添加模拟关注或模拟会员记录。

## 测试依据

- 隔离 PostgreSQL 16 数据库执行全部 88 条迁移后，后端测试输出 221 个通过事件（143 个顶层测试、78 个子测试），0 失败、1 个已有测试跳过。包含并发领取仅入账一次、取消重关、过期码、JSON 订阅字段保留、事务失败回退和删除账号后保留防重复账本。
- `go vet ./...` 通过。环境没有 CGO / C 编译器，未执行 race detector；并发行为通过 PostgreSQL 集成测试验证。
- 前端 TypeScript、15 个单元测试、正式 Next.js 构建通过，32 个静态页面生成成功。
- UI 覆盖 73 个用例；完整首轮 72 通过、1 个开发提示层遮挡按钮导致超时。修正测试点击位置后，9 个相关用例复测全部通过，保留两轮报告，不把复测描述为单轮 73 个全通过。验证包括弹框关闭、遮罩、取消、叠加优先级、退出登录时的延迟响应、移动端、二维码失败重试、绑定码过期及重新生成。
- 生产公网：首页、登录页、robots、sitemap 返回 200，首页服务端 HTML 保留 title / description；匿名状态和生成绑定码返回 401。
- 公网回调：正确签名 GET 和 AES GET 回显均通过；安全模式加密 POST 返回成功，错误收件人返回 400，明文降级返回 403。探针使用忽略的图片消息，前后关注状态 / 码 / 奖励表均为 0。
- 默认微信二维码 URL 返回完整 430×430 JPEG，HTTP 200。尚未进行真实微信扫码和事件验收。
- `system.onboarding_config` 与发布前备份逐字段及 SHA256 一致：`bf5fbb9d5b071c1ab075bd441dcb639f04744c7e7d564f6bd0b27909e26a5914`。本地 Agent 健康接口返回 200，版本 6.2.10.1；本功能未修改 Agent、Worker 或下载更新信息。
- PostgreSQL 和 Redis 容器 ID / 启动时间与发布前相同，未重启。

本地详细报告位于前端忽略目录 `artifacts/wechat-official-qa/` 和 `artifacts/wechat-follow-qa/`。服务器只输出不含凭据的验证摘要；完整容器环境、数据库备份和配置快照仅保存在受限备份目录。

## 管理员待完成的微信操作

已在生产预填 AppID、原始 ID、用户提供的 AES 密钥和随机 Token。功能开关保持关闭，避免微信尚未接入时向用户承诺可领取。

1. 打开「系统配置 → 其他配置」，复制 Token。
2. 微信服务器 URL 填 `https://goodhr.58it.cn/api/wechat-official/callback`；Token 与系统相同，AES 使用原密钥，消息模式选「安全模式」，数据格式 XML。
3. 微信验证并启用服务器配置后，打开系统的公众号绑定开关并保存。
4. 在控制台复制绑定码，扫码关注，向公众号发送绑定码，检查会员增加 5 天、用户管理显示「OpenID 有」。此前已关注但没有回调记录的账号需取消后重新关注一次。

操作细节及微信原有自动回复 / 菜单切换说明见 [公众号配置指南](wechat-official-follow.md)。本功能不依赖 AppSecret，也不承诺未认证账号支持官方带参二维码或主动通知 API。

## 备份与回退

备份目录：`/www/wwwroot/goodhr-production/backups/wechat-follow-20261009-221635`，目录权限 700，私有文件权限 600。

数据库备份 `goodhr5-before.dump`：87,449,342 字节，SHA256 `c56fb4f3ccd0664ba2d465d3c7c601cd49661ca49651361b0235c282a5cadfe8`。`pg_restore -l` 可读取 458 条 TOC；未声称完成整库恢复演练。

旧容器已停止并保留：

- `goodhr-production-backend-backup-20261009-221635`
- `goodhr-production-frontend-backup-20261009-221635`

已独立检查发布脚本的身份校验和失败回退路径。必要时在目标服务器执行：

```sh
python3 /www/wwwroot/goodhr-production/backups/wechat-follow-20261009-221635/deploy-containers.py rollback /www/wwwroot/goodhr-production/backups/wechat-follow-20261009-221635 3a8de3b34ca0f8a4cc1fd7ae6e4b86fd31a11d32
```

回退先核对容器身份，隐藏私有配置后恢复旧容器，不恢复旧数据库覆盖上线后的业务，也不删除奖励账本。此前已有的其他备份容器保留。
