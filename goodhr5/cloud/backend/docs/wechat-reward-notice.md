<!-- 本文件说明公众号关注赠送会员邮件的事务、发送结果、上线兼容和人工核查边界，不包含凭据或真实用户信息。 -->
# 公众号关注赠送会员邮件

公众号关注绑定首次发奖后，后台调用原有 `sendSubscriptionRewardNotice`，继续使用 `subscription_reward.html` 和“GoodHR 会员时间变动提醒”主题。原因显示“公众号关注赠送会员”；赠送天数、会员类型、到期时间取永久奖励账本入账时的快照，不受后续修改奖励天数影响。

发奖、会员更新、绑定码消费和待发通知在同一个 PostgreSQL 事务中提交。微信回调不等待 SMTP，也不另一次调用会员发放接口。重复回调、取消重关和重新绑定不会重置奖励或邮件状态。

## 邮件状态

- `pending`：奖励已提交，通知尚未领取。
- `sending`：一个工作进程独占发送，随机领取令牌和两分钟租约保护结果写回。
- `sent`：SMTP 已接受邮件，记录接受时间；不表示收件箱已经显示。
- `retry`：邮件明确未被接受且为临时失败，按 1 分钟、5 分钟、15 分钟、1 小时退避，最多尝试五次。
- `failed`：明确永久拒绝、非传输错误或已达到重试上限，需要人工核查。
- `uncertain`：DATA 后确认丢失，或发送进程退出后领取租约过期。服务器可能已接受，禁止盲目重发。
- `skipped`：迁移前的历史记录，或用户已删除、邮箱无法解析。

后台每五秒最多领取一封，读取同一持久账本，服务重启后可继续处理 `pending/retry`。过期 `sending` 仅转为 `uncertain`，避免“发送成功后写回失败”造成自动重复通知；SMTP 和数据库之间无法保证绝对恰好发送一次。

SMTP 建连、TLS 握手和协议读写使用同一连接截止，最多 30 秒。DATA 返回最终成功后，QUIT 失败仅作清理，不再视为发送失败。明确 DATA 4xx 可重试，5xx 停止；缺少最终确认保留 `uncertain`。

## 发布与核查

新增迁移 `0086_wechat_follow_reward_notice.sql`，为奖励账本增加六个有中文备注的邮件字段及索引。先用 `skipped` 填充已有奖励，最后设置新行默认 `pending` 及发送时间，保证候选迁移到正式切换期间，旧后端新增的奖励也不会漏掉通知。迁移可重复执行，不重置已有发送状态。

`GOODHR_DISABLE_EMAIL_SCHEDULER=true` 禁止启动关注通知后台任务，候选容器必须保留此开关。无 SMTP 的开发模式不会把日志邮件标成真实发送。本功能不受营销挽回邮件开关影响。

上线前备份数据库和容器参数，验证真实隔离 PostgreSQL、并发领取、事务回滚、状态防重放、动态 2/7 天、Plus/Max、SMTP 总超时与确认丢失。生产只更新后端容器，保留前端、数据库及缓存进程。回退只恢复原后端，保留新增兼容字段及账本，不用整库恢复覆盖实时业务。

历史通知默认不群发补发。确需补通知时，先确认目标奖励未发过、`mail_status='skipped'` 且 `mail_attempts=0`，以明确邮箱和该奖励入账时间为条件仅改邮件状态及下次发送时间。不得再次延长会员，不应自动重试 `sent/sending/uncertain`。

2026-10-10 本地验证：真实隔离 PostgreSQL 的通知存储 10 组通过；连同工作进程共 16 组、35 个含子场景通过，零失败和跳过。SMTP 新增 9 组本地网络测试通过。配置隔离 PostgreSQL 的 `go test ./... -count=1` 共 400 项通过，4 项因缺少租户/真实 Redis 的对应环境跳过；`go vet ./...` 通过。测试不会使用正式凭据或向真实用户发送邮件。

## 2026-10-10 生产发布记录

- 目标服务器 `ldclouda31539 / 43.228.79.108`，站点 `https://goodhr.58it.cn`。
- 镜像源码提交 `7ba817bb0da72f7f676bb6928a66c65bfe10d956`；源归档 SHA256 `7f337bf3a9b68c44d5d73b28eb778c0e0d4534744016a9f46d8ca75b04db3854`。
- 镜像 `goodhr-production-backend:wechat-reward-mail-20261010-7ba817bb`，ID `sha256:9f8f624a324e2ee6bb51c05b68f529f7c1b571634bb5b833e14e5f14afea0557`。
- 正式后端容器 `59d18e0aa4c65e4755b473bfc2964cd3dbfdb231d239addbace8c374d03282dc`；候选端口 8087 禁发验证通过，再切正式 8086。正式邮件调度沿用原 `false` 禁用值，关注通知任务启动一次。
- 只切后端，前端、PostgreSQL、Redis 的 ID 和启动时间保持原值，官网、登录、robots、sitemap 均 200，匿名关注状态 401。
- `0086` 迁移登记一次，总迁移数由 89 到 90，六个邮件列均有中文备注。候选阶段历史奖励仍为 `skipped`，公众号与 onboarding 完整私有配置一致，原奖励字段和全部用户会员摘要保留。
- 为反馈问题的用户本人既有唯一 2 天奖励，以邮箱、入账时刻、`skipped` 和零尝试为条件只改通知状态。北京时间 2026-10-10 00:59:26，SMTP 已接受，账本 `sent / attempts=1`；未再次赠送会员，全部会员摘要仍为 `f889505da5f79c551e30e568ae9e066a`。
- 公网五个协议探针全部通过：普通签名 GET 和 AES GET 正确回显，忽略图片的加密 POST 返回 `success`，错误收件人 400，明文降级 403。探针未发送关注或绑定消息，前后关注、奖励、绑定、会员和码表数量与摘要一致。
- 后续发布文档提交不改变镜像产品代码；本地测试容器已清理。安全证据在前端忽略目录 `artifacts/wechat-reward-mail-qa/`。

备份目录 `/www/wwwroot/goodhr-production/backups/wechat-reward-mail-20261010-004441`。目录权限 700，私有参数和数据库备份 600。数据库备份 87,356,181 字节，SHA256 `b100914f36b0f44cb2242b3c56f48f5913758a0e2bf493fcb9734aa694f69cba`，`pg_restore -l` 验证可读，未做整库恢复演练。旧后端保留为 `goodhr-production-backend-backup-20261010-004441`。

已确认目标服务器后，可按原参数回退后端；保留邮件状态、会员及实时业务数据：

```sh
python3 /www/wwwroot/goodhr-production/backups/wechat-reward-mail-20261010-004441/deploy-backend.py rollback /www/wwwroot/goodhr-production/backups/wechat-reward-mail-20261010-004441 7ba817bb0da72f7f676bb6928a66c65bfe10d956
```
