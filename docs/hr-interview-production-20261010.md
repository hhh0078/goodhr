<!-- 文件作用说明：记录企业HR面试流程的正式部署版本、备份、迁移、线上验证及容器回退边界。 -->
# 企业 HR 面试流程上线记录（2026-10-10）

部署目标为 `ldclouda31539 / 43.228.79.108`，正式入口为 https://goodhr.58it.cn。前后端于北京时间 2026-10-10 23:30 完成切换，运行源版本为 `63ad61cd766938f47a2f10c455b7ebac196e5387`。后续上线记录提交不包含运行代码修改，不需要重复构建。

使用入口：[企业招聘](https://goodhr.58it.cn/admin/recruitment)、[招聘设置](https://goodhr.58it.cn/admin/recruitment/settings)。管理员先填写公司名称、招聘联系人和联系邮箱，再配置流程及邮件模板；从简历详情或企业招聘页面选择岗位，启动招聘流程。

## 发布产物

- 后端镜像：`goodhr-production-backend:hr-interview-20261010-63ad61cd`，ID `sha256:f82fa9573811363b3bbeeab428e776371f3830ed9fa32bcd43815b3044750477`。
- 前端镜像：`goodhr-production-frontend:hr-interview-20261010-63ad61cd`，ID `sha256:069c89ff154a0c7cbb35c190b6ede92d50d292626f464ccd0c05fad886b28e0c`。
- Linux 后端二进制 SHA256：`c5f078e63a90f0b42d541074d83e05483745f199ae29ee0a42f607efb68d8f70`，Go 1.27、linux/amd64、CGO=0。
- 云端源码归档 SHA256：`3096083ec0d94e6d89fa7134f310bd332367c78d77f361719c64d8343d6a7bf8`。
- 前端使用项目 Dockerfile 和生产域名构建，限制构建 CPU 1 核、内存 1 GiB、内存加交换 2 GiB、Node 堆 768 MiB；构建成功。

正式容器 ID：后端 `66dd55bb25c7f35be2ad8affedc6b495027502b019f950551e02dcb787e34e60`，前端 `88399efbe2afee578a3c8d810d72b41b1393f656192c69a05a40f106f6eaa7ce`。两者发布标签均指向上述源版本，验收时运行正常、重启次数为 0。

## 备份与迁移

私有发布目录为 `/opt/goodhr-release/hr-interview-20261010-63ad61cd`，权限 700；容器环境与配置材料权限 600，不进入 Git。完整数据库备份为其中的 `database.dump`（93,028,562 字节），SHA256 为 `80fd586acc12776cd0ae719f16e66df4ac0a8053c5c62393a7d66788a469b887`，已检查归档格式并实际恢复成功。

先将完整备份恢复到独立 PostgreSQL，再验证新增迁移和候选镜像。演练通过后，暂停旧后端，在一个事务中追加 `0090_recruitment_workflows.sql`、`0091_recruitment_mail.sql` 及迁移记录。原 96 项迁移记录保持不变，总数为 98。

新增六张表：`recruitment_workflows`、`recruitment_applications`、`recruitment_invitations`、`recruitment_company_settings`、`recruitment_mail_templates`、`recruitment_mail_deliveries`。全部表及字段有中文备注。

独立演练和正式迁移均比较了 39 张旧业务表的行数、逐行内容摘要及原迁移记录摘要，结果一致。迁移前核对的原数据包括 34,609 份简历、13,386 条候选人事件、10 条备注、18,037 条跟进记录，推荐及推荐事件表均为 0 条。

正式前后端保留原环境变量、运行命令、工作目录、用户、端口、挂载和完整 HostConfig。PostgreSQL、Redis 的原容器 ID、镜像及启动时间保持不变。此次仅发布云端功能，没有本地 Agent 升级。

## 验证结果

- 本地：后端完整测试及 PostgreSQL 测试、前端 25 项单元测试、类型检查、构建、新招聘流程 7 项和原简历库 24 项 UI 回归通过；另有公开邀请页隐私回归通过。
- 候选镜像：宿主独立端口后端健康为 200；招聘私有接口未登录返回 401，无效邀请接口返回 404，候选前端页面为 200。
- 正式：宿主 `127.0.0.1:8086/health` 为 200；公网首页、登录、企业招聘、招聘设置、公开邀请页面为 200；五类私有招聘接口匿名访问为 401，无效邀请接口为 404。
- 公网浏览器：无效邀请展示可理解的失效提示，不显示确认参加按钮；robots/googlebot 为 noindex、nofollow，referrer 为 no-referrer；没有统计脚本、第三方请求或内部简历接口请求，没有页面异常或静态资源加载失败。
- 公开邀请接口响应含 `Cache-Control: no-store, max-age=0, must-revalidate` 和 `Referrer-Policy: no-referrer`。前后端启动日志未发现 panic、fatal 或 OOM 特征。

生产平台 SMTP 已配置，实际邮件标题按公司名称添加前缀，Reply-To 使用团队联系邮箱。测试与上线验收未向真实候选人发送邮件，未验证真实收件箱投递。

隔离演练前后端及 PostgreSQL 容器已清理。备份、隔离数据库目录和私有验证报告仍保留。

## 回退材料

旧前后端容器已停止并保留为：

- `goodhr-production-backend-1-backup-hr-interview-20261010-63ad61cd`
- `goodhr-production-frontend-1-backup-hr-interview-20261010-63ad61cd`

人工发布脚本见 [`scripts/releases/hr-interview-20261010.py`](../scripts/releases/hr-interview-20261010.py)，与正式执行文件完全一致（SHA256 `f23375cf68107eb2ed21637d38b2462f7346c14b82ab9a3d069893ea700fef0c`）。`prepare`、`rehearse`、`cutover` 不能在当前已完成目录盲目重跑。后续确需回退时，先核对当前服务和私有进度，再执行：

```sh
python3 /opt/goodhr-release/hr-interview-20261010-63ad61cd/hr-release.py rollback --root /opt/goodhr-release/hr-interview-20261010-63ad61cd
```

回退只恢复本次保留的原应用容器；保留新增表、招聘资料和迁移记录，不恢复全库、不删除新业务数据。若后续发布已改变容器或镜像身份，脚本会停止，需先核对，不能直接套用。
