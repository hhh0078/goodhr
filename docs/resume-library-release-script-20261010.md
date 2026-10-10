<!-- 文件作用：说明本次简历库发布脚本的固定基线、验证流程与回退保护，实际执行证据见上线记录。 -->
# 简历库生产发布脚本审查说明

脚本见 [resume-library-20261010.py](../scripts/releases/resume-library-20261010.py)，实际执行结果见 [上线记录](resume-library-production-20261010.md)。目标固定为 `ldclouda31539 / 43.228.79.108 / https://goodhr.58it.cn`。材料必须位于根盘 `/opt/goodhr-release/resume-library-20261010-*`；发布脚本不修改服务器旧 Git checkout，不构建镜像，不重启 PostgreSQL 或 Redis。

## 已核对的基线

- 另一项独立发布已将生产更新至 `805fce9f8b848f1f5f46e09befa5a53c4959db50`。本脚本固定核对新后端 ID `cd42e5abc3ac68ab33ec1e7822a695396ddb165666e8433e5eadeba7cc146a43`、镜像 `sha256:7c506d2925a6e7e2c73d0b1eaf1a19ea41161ef21e808fe1e325d2db964ffb42`，新前端 ID `acb4627d7aaf02ed014ac9ce0890733c194f043107afd827e66b7e06dd790708`、镜像 `sha256:c813ad4f08100dc2ca4d1ed599689c4aa47d5e34193c044de30f9ff7ba86c846`；PostgreSQL、Redis 原基线不变。
- Nginx API 到 `127.0.0.1:8086`，前端到 `127.0.0.1:5175`；应用目录 `/www/wwwroot/goodhr-production/good-hr-ai-js/goodhr5`。
- Docker 根 `/www/docker/engine` 在独立 `/www` 数据盘。最初侦察时数据盘约 3.6GB、根盘约 26.6GB 空闲，后续独立发布已改变现场；上传和导入前必须刷新实际空间，不删除回退镜像。
- 最初侦察时库 `goodhr5` 约 591MB、候选 34,609、触达 18,037、事件 13,386。该数量仅为旧快照；本次 `prepare` 保存实时聚合量与完整备份。
- 六个审定迁移已由独立发布执行：`0084_candidate_library_upgrade.sql`、`0085_browser_extension_release_config.sql`、`0086_candidate_notes.sql`、`0087_user_messages.sql`、`0088_resume_library_settings.sql`、`0089_candidate_shares.sql`。相同编号的微信迁移仍按完整文件名区分。`prepare` 严格核对历史正文，允许 pending 为空，并保存全部六份 SQL 供独立库演练使用；不得把已执行迁移重复写入 ledger。

新增主分支 `805fce9f` 的 0087 仅创建 `user_message_state/messages/user_message_receipts` 和索引，0088 仅创建 `resume_library_settings`，0089 仅创建 `candidate_shares` 和索引；均有中文备注，使用 `CREATE IF NOT EXISTS`，不改写原候选、触达或事件资料。

## 执行顺序

先在本地从最终合并提交创建云端源码 `git archive`，构建生产地址镜像并添加 `org.opencontainers.image.revision=<完整提交>` 标签。上传源码归档、镜像归档、脚本和最终 `release.json`，核验传输 SHA，再导入镜像。以下命令中的占位值必须由最终产物替换：

```sh
python3 release.py prepare --release-dir /opt/goodhr-release/resume-library-20261010-唯一编号 \
  --commit 完整提交 --backend-image 后端标签 --frontend-image 前端标签 \
  --archive 源码归档绝对路径 --archive-sha256 源码归档SHA256 --release-json 最终公开配置绝对路径
python3 release.py rehearse --release-dir /opt/goodhr-release/resume-library-20261010-唯一编号
```

`prepare` 创建 700 目录、600 私有文件，保存原容器完整配置、私有环境、插件旧行、完整自定义格式数据库备份及摘要。数据库备份必须通过 `pg_restore --list`，然后实际恢复演练，不能把 `.partial` 视为备份成功。

`rehearse` 建专属 PostgreSQL 容器，数据挂在根盘，数据库名只能是 `goodhr_resume_verify_*`。恢复完整 dump，再在一个事务中只执行尚缺少的审定迁移、对应 ledger 和插件公开配置；当前基线通常没有待执行迁移。它比较原候选/触达/事件数量及全部业务字段的行聚合指纹，只统一薪资数值的文本格式。备注数应为“原已有独立备注 + 尚未迁入的 legacy 备注”，不会要求独立备注数量等于旧事件数量。随后核对旧备注正文和时间，重跑 0086 核验幂等。

随后仅在独立库的新表写入合成消息、幂等凭据、账号设置和分享记录；引用克隆父表的键以满足外键，不修改原候选、触达或事件。保存这五张新表的数量和聚合指纹，再实际执行整数薪资及旧公开配置回退，验证原三表指纹未变、两个薪资列回到 integer，0084 记录撤销而 0086—0089 保留。新表与合成资料必须完整保留。再根据当时完整 ledger 只执行尚未标记的迁移，恢复 numeric 和最终插件配置，并再次验证原三表和新增五表的指纹。原没有插件配置时重跑 0084、0085；原存在配置时仅重跑 0084，其余 ledger 不重复插入。生产库不参与该回退演练。

如果恢复库已有小数或整数越界薪资，整数转换应被拒绝；此时核对 numeric、全部原资料和 ledger 均未改变，验证的是回退保护，绝不截断已有小数。实际成功转换与拒绝转换在演练报告中分别记录。

候选后端使用该独立库、独立 uploads/logs、空 Redis/SMTP 配置、禁邮件调度，只执行健康与匿名 401 检查；不借真实用户 Token。不挂生产 uploads/logs，不访问真实招聘页面。候选前端只验证 `/login`。端口是仅本机的 `55439`、`8089`、`5178`；保留挂载父目录 `pgdata.chmod(755)` 以便官方 PostgreSQL 入口切换用户，外层发布目录仍为 700。

审查通过并保存演练结果后，部署负责人执行：

```sh
python3 release.py cutover --release-dir /opt/goodhr-release/resume-library-20261010-唯一编号
```

切换前再次核对容器和 ledger；暂停原后端，迁移与公开配置更新同一事务提交，再换后端、前端，原容器改名保留。正式镜像仍采用原私有环境、网络、挂载和端口。最终核对本机健康、官网登录、匿名候选 401、插件 release API 和官网 ZIP 实际 SHA。需要另外由负责人核对真实账号页面和下载入口。

停机前将插件当前整行与私有备份比较，包括值、启用状态、更新时间；任何漂移都会在停机前拒绝发布。迁移事务还持有配置表写锁并复核同一整行，关闭停机检查与迁移之间的竞态窗口。

## 插件公开配置

`release.json` 恰好包含 `version/note/url_chrome/sha256_chrome/url_firefox/sha256_firefox/firefox_signed`。Chrome 文件必须已烘焙进前端 `/app/public/downloads/`，配置 SHA 与镜像文件一致。Firefox 本轮暂不发布时地址和摘要都为 `""`，`firefox_signed=false`。旧 0085 迁移不修改；事务内立即用最终配置覆盖公开字段，保留未知字段，`enabled=true`。旧行有私有备份。

## 回退与限制

```sh
python3 release.py rollback --release-dir /opt/goodhr-release/resume-library-20261010-唯一编号
python3 release.py cleanup-rehearsal --release-dir /opt/goodhr-release/resume-library-20261010-唯一编号
```

805fce9f 原容器已经支持 numeric，生产 `rollback` 必须记录并核对该模式，只恢复原应用和插件公开配置，保留 numeric、全部 ledger 以及所有消息、设置、分享、备注、推荐资料。生产路径不调用整数转换，也不删除 0084/0085 ledger。整数转换函数只供独立恢复演练使用；旧 eda74de5 的发布材料不能用于本脚本回退。

回退公开配置先检查当前发布字段仍等于本次 release.json；管理员已经改过时拒绝覆盖并恢复确认过的 numeric 后端。若停机后事务尚未提交，整行仍等于旧快照时直接恢复805原容器，不执行多余配置写入。任何身份、私有环境或 schema 不确定都保留现场并报阻塞。当前基线不需要额外兼容桥接镜像。

公开配置恢复只改变公开字段，保留未知字段；原没有插件配置时可删除本次新增行，但生产原有完整 ledger 始终不改动。

脚本不自动恢复全库，避免覆盖发布后正常业务数据。断电、SIGKILL、命令超时或 Docker 返回不确定结果时，先人工核对 `cutover-progress.private.json`、实际容器 ID 与迁移 ledger，再使用回退入口；不要猜测成功。后续再次发布必须重新核对基线与 pending migration 集合，不能原样重用本轮固定基线。

清理仅删除记录 ID/标签均匹配的独立候选容器，保留根盘恢复数据目录，不执行 `DROP DATABASE goodhr5`、不递归删除生产目录。未通过的演练可保留现场后人工处理，原样重跑会被端口和目录存在检查阻止。
