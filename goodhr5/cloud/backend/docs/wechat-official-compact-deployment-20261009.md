<!-- 本文件记录公众号关注弹框、四位数字绑定码和动态赠送天数的生产发布与验证，不保存凭据。 -->
# 公众号关注弹框与动态赠送上线记录（2026-10-09）

## 本次行为

顶部仅保留红色粗体「关注领取 X 天会员」。删除副标题、公众号名称和额外奖励介绍；操作说明「先扫码关注，再把下面的绑定码发给公众号。」放大、加粗、标红。底部同一行靠右保留「暂不关注」「重新生成」，不显示复制按钮。

绑定码使用密码学随机的 4 位数字，Redis 原子占位、数据库事务再次查重，冲突重试；过期码允许安全复用。旧版未过期的 8 位码仍可完成绑定。每个账号十分钟最多生成五次，连续猜错受到限流。奖励继续按网站账号和公众号微信用户各限制一次。

赠送天数来自 `system.onboarding_config.wechat_follow_reward_days`，生产设置为 **2 天**，范围 1–365。管理入口为「系统配置 → 本地组件 → 本地程序与组件 → 关注赠送」，与原 `trial_days` 同组。页面、公众号回复、实际入账和奖励账本读取同一配置，已领取奖励保留原天数；本次未改变注册赠送行为。

## 实际发布

- 服务器 `ldclouda31539` / `43.228.79.108`，站点 `https://goodhr.58it.cn`。
- 镜像源提交 `e70ecccbd8c33ff05e3dc14b471971bddc021bce`，归档 SHA256 `866163dd89e6871a3e02d371169301245d3ab7e85a56fc69d07d7896b6667646`。
- 后端镜像 `goodhr-production-backend:wechat-follow-20261009-e70ecccb`，ID `sha256:6d153d8f15bb8dafc5fea1f4634d334aebb04cc7227c3e26fee7e90aa16e1a39`。
- 前端镜像 `goodhr-production-frontend:wechat-follow-20261009-e70ecccb`，ID `sha256:c98f0215b3a4d56fb46370f1782b05e0143bfbf3faa1b1d8e6c6aed7e9e16fdb`。
- 正式后端容器 ID `8ebf274ca25932aa6fdfa0cf60ff5fe5af1ee758bc61f786aeaaedfabd3efc96`。
- 正式前端容器 ID `168520f93abc3b3bb92e00b7ce909441791d3580c56d2325888cc5f85ed2cb54`，BUILD_ID `bBcaTTbNAmltiEIK9_fV6`。

顺序构建后在 8087 / 5176 验证候选，再切换正式 8086 / 5175。正式容器沿用原环境、挂载、网络、启动命令、日志和重启策略。候选后端关闭邮件调度，正式后端保留原 `GOODHR_DISABLE_EMAIL_SCHEDULER=false`。

`0085_wechat_follow_reward_config.sql` 已执行，迁移记录共 89 条。移除新增关注字段后，`system.onboarding_config` 全部原字段与备份完全相等，公众号私有配置值与备份完全相等，两层开关均开启。未改变本地程序更新配置、Agent、Worker、已有会员或奖励账本。

## 测试与实机核验

- 隔离 PostgreSQL 16 / Redis 7 后端全量测试 286 个通过事件（164 顶层、122 子测试），0 失败，1 个已有租户邀请迁移测试因不同测试环境未配置而跳过。公众号专项 152 个通过事件，0 失败、0 跳过；`go vet ./...` 通过。未运行缺少 CGO 编译器的 race detector。
- 独立复核动态天数、付费会员保持、重复领取、碰撞重试、过期复用、迟到消息、旧码兼容、限流与缓存故障。真实 PostgreSQL 跨账号碰撞及过期码交叉复用各连续 20 轮通过，Redis 并发占位通过。
- 前端 TypeScript、15 个单元测试、正式 Next.js 构建通过，32 页预生成。
- UI 共覆盖 79 组；首轮 74 通过、5 失败，修复共享弹框加载图标可访问名称并让几何断言等待布局稳定后，相关用例定向复测及公众号最终 31/31 全部通过。保留首轮和复测报告，不称为单轮 79 全通过。桌面和手机的真实公开二维码预览另有 2/2 通过，业务账号、绑定码和写请求均模拟。
- 已使用实际登录的生产控制台验证单行红色动态标题、红色粗体操作说明、四位码、两个同排靠右按钮及持久入口；浏览器未记录 error / warn。该次仅生成本人待绑定码，没有伪造关注或赠送。
- 独立生产核验：首页、登录、robots、sitemap 返回 200，首页 HTML 保留 title / description；后端 health 返回 200，匿名关注状态返回 401。正式容器参数、旧容器身份与备份一致，候选容器已清理。
- 公网协议五项通过：签名 GET 与 AES GET 正确回显 200、忽略图片的加密 POST 返回 200 success、错误收件人返回 400、明文降级返回 403。探针前后关注、奖励、绑定、会员及码表摘要一致，仅使用图片消息，未模拟关注或领取。
- 生产页面仍显示本地程序已连接、版本 6.2.10.1。微信真实扫码、发送绑定码和会员到账仍需公众号本人操作验收，不能用自动化模拟结果代替。

本地报告位于前端忽略目录 `artifacts/wechat-compact-qa/` 和 `artifacts/wechat-follow-compact-qa/`。后续测试、文档及主分支合并提交不等同于本次产品镜像源提交；主分支同期个人 AI 配置更新被保留在 Git 合并结果中。

## 备份和回退

备份目录 `/www/wwwroot/goodhr-production/backups/wechat-follow-20261009-231819`，目录权限 700，私有文件权限 600。

数据库备份 87,352,749 字节，SHA256 `37eabd4fb3577226f228890f5d55a79b9685bda7a8479cc6b9d76b72972b4979`。`pg_restore -l` 读取 499 条 TOC；未声称完成整库恢复演练。PostgreSQL / Redis 容器 ID 与启动时间均未改变。

旧容器已停止并保留，候选已清理：

- `goodhr-production-backend-backup-20261009-231819`
- `goodhr-production-frontend-backup-20261009-231819`

必要时在已确认的目标服务器执行：

```sh
python3 /www/wwwroot/goodhr-production/backups/wechat-follow-20261009-231819/deploy-containers.py rollback /www/wwwroot/goodhr-production/backups/wechat-follow-20261009-231819 e70ecccbd8c33ff05e3dc14b471971bddc021bce
```

旧后端仅识别 8 位码，失败回退会临时隐藏公众号功能（仅将私有配置记录的 `enabled` 设为 false），保留配置内容、密钥、业务数据和奖励账本。修复后重新发布再启用；不恢复整库覆盖实时数据，不删除历史备份容器。
