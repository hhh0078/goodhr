<!-- 文件作用：说明后台控件与间距统一的前端发布范围、备份验收、精确切换及回退办法。 -->
# 后台尺寸统一：仅前端发布

本次仅更新 Next.js 前端。后端、PostgreSQL、Redis 不停止、不重启；没有 SQL 写入、数据库恢复、schema migration 或插件配置更新。发布器固定复用已审查的 `hr-release.py` 原语，不调用其双应用切换或迁移阶段。

脚本：`scripts/releases/admin-uniform-20261015.py`。固定启动器为 `/opt/goodhr-release/hr-interview-20261010-63ad61cd/hr-release.py`，SHA256 必须是 `f23375cf68107eb2ed21637d38b2462f7346c14b82ab9a3d069893ea700fef0c`。

## 范围和基线

- 主机必须为 `ldclouda31539` 且拥有 `43.228.79.108`。
- 后端容器 ID 为 `f308ae790c1e6daf43e3453dd27504f7871b2cf6f1da564d95d368836646130a`，镜像为 `sha256:31df9fe839f4375e0bada14816c695662029e6575d44cbac01bc2d728ce4014b`，启动时间为 `2026-10-14T17:51:50.637741708Z`。
- 旧前端容器 ID 为 `38d24aea6bc9f2542db1ef25b0de469e5e6f85310fde8d90c2ca497c498ad2ff`，镜像为 `sha256:83ee8d4cbbe2a1a8ad850a8c5f165097986df63e7a23383c642e798fe6facbf0`，启动时间为 `2026-10-14T17:51:54.11472544Z`。
- PG 和 Redis 沿用脚本中固定的精确容器 ID、镜像和启动时间，任何变化均停止。切换与回退后的后端、PG、Redis 三项身份及启动时间必须仍符合基线。
- 103 项既有迁移：源码归档与现有后端镜像正文一致，正式 `schema_migrations` 文件名与全字段摘要不变。比较沿用已审查启动器的 LF 行尾标准化。
- Chrome 保留 `1.0.9`，公开 ZIP SHA256 为 `fefe405c45bc61d6c4b5b656554375c085c1a895c0a0c8089c509afc463107bd`。Firefox 保留已签名 `1.0.7`；完整系统配置行和 Agent 兼容快照以只读摘要校验，不更新版本或地址。

## 私有材料和构建

`/opt/goodhr-release/admin-uniform-20261015` 存放完整 PG custom-format dump、完整旧前端 inspect/Env、阶段状态、摘要、构建日志；目录 700，文件 600。根盘约 1.3 GiB，原备份约 104 MiB；脚本要求复制源码后保留至少 256 MiB、准备备份时至少 256 MiB。实际 dump 大小及 SHA256 保存到 manifest，磁盘不足或备份目录检查失败不会切换应用。

`/www/wwwroot/goodhr-production/releases/admin-uniform-20261015` 存放解包源码和独立 runtime 构建目录。要求至少 3 GiB 可用空间。源码必须来自完整 Git 提交的 tar 归档，核对 SHA256 与 pax `comment`，拒绝越界、链接或重复条目，不接受工作目录直接打包。

前端在固定旧前端镜像内运行 `npm ci --include=dev --registry=https://registry.npmmirror.com --no-audit --no-fund && npm run build`。资源上限：2 GiB 内存、3 GiB 内存加 swap、2 CPU、Node heap 1536 MiB，关闭 Next.js 构建遥测。应用统计逻辑不改变。生成的 standalone、static、public 分别复制至新的 runtime 目录；该目录没有源码 `.dockerignore`，避免遗漏 `.next`。

构建只生成前端镜像，manifest 的 `images` 只能包含 `frontend`。候选前端仅在 `127.0.0.1:5182` 运行，服务端 `CLOUD_API_BASE=http://127.0.0.1:8086` 只读取现有生产后端的公开接口。匿名 HTTP GET 检查登录、各后台路由、下载页、API 401 边界、插件版本和 Chrome ZIP，不打开带真实账号的浏览器。

## 分阶段执行

在审查通过、主分支提交和归档上传后，由负责人逐阶段运行；每条命令必须使用同一完整 40 位提交。下面的占位符需替换为已核对值。

```sh
python3 /opt/goodhr-release/admin-uniform-20261015/admin-uniform.py build --commit COMMIT40 --archive /opt/goodhr-release/admin-uniform-20261015/uploaded-source.tar --archive-sha256 ARCHIVE_SHA256
python3 /opt/goodhr-release/admin-uniform-20261015/admin-uniform.py prepare --commit COMMIT40
python3 /opt/goodhr-release/admin-uniform-20261015/admin-uniform.py rehearse --commit COMMIT40
python3 /opt/goodhr-release/admin-uniform-20261015/admin-uniform.py cutover --commit COMMIT40
python3 /opt/goodhr-release/admin-uniform-20261015/admin-uniform.py cleanup --commit COMMIT40
```

`prepare` 制作完整 `pg_dump -Fc`，检查 `PGDMP` 头并使用 `pg_restore --list` 检查目录。只检查备份目录，不运行 restore，不创建隔离 PG。它保存全部四容器原 inspect、前端完整 Env、迁移与插件配置摘要。只读验证完成且候选 HTTP 验收成功，才允许切换。

跨发布锁固定为 `/www/wwwroot/goodhr-production/backups/.goodhr-container-deploy.lock`，不允许两个发布同时改变容器。正式创建完整复用旧前端 Config、HostConfig、网络及卷，仅更新镜像和本轮发布标签。

## 切换、回退和清理

切换只对旧前端执行 stop → rename 为备份 → create 新前端 → start。每一步先记录私有意图。检查本机和公网后，再核对后端/PG/Redis 身份、103 项迁移行及插件配置摘要。新前端不符合预期或收到 SIGTERM 时自动恢复精确旧前端；恢复期间屏蔽重复终止信号。SIGKILL/断电无法自动处理，必须依据持久意图执行下面的精确回退。

```sh
python3 /opt/goodhr-release/admin-uniform-20261015/admin-uniform.py rollback --commit COMMIT40
```

若 Docker create 超时、ID 尚未写入进度，回退仅识别正式名称下具有本轮完整镜像 ID、release/commit/frontend 标签的容器，并持久记录恢复到的 ID。外来容器、未知查询错误和身份漂移均保留现场。失败的新前端改名保留；旧前端 ID 与镜像必须符合固定基线。回退不会恢复数据库，也不改变后端、PG或Redis。

`cleanup` 只允许移除本轮精确 `goodhr-admin-uniform-frontend-candidate-20261015` 候选容器，复核镜像/ID/标签及 host 网络、5182 环回绑定。不会删除正式容器、旧前端备份、失败新前端、dump、私有日志、源码或镜像。构建命令失败时如残留构建容器，应先按固定名称、本轮标签及固定旧镜像核对，由负责人单独处理；不能扩大候选清理范围。

脚本 stdout 只显示提交、镜像或容器 ID、阶段状态、数量、摘要和布尔结果，不显示 Env、SQL正文、账号、候选人或邮件内容。异常输出固定提示，详细现场留在私有材料。

## 实际验证与发布进度

发布源码提交 `9374fd0a8332d6d07273d622816b4f0979be516f` 已合并主分支。本地验证结果由负责人汇总确认：本次新增 17 项 UI 回归、既有 9 项 UI 回归、37 项单元测试以及 TypeScript 类型检查均通过。

发布器的 Python 3.8 AST、21 个函数中文注释及禁止写 SQL / 原双应用切换调用的静态检查通过。3 项纯内存模拟通过：停止前配置漂移不触发 stop，create 成功但结果未知时按标签找回 ID 并恢复旧前端，成功切换只操作前端。模拟未执行子进程、HTTP或Docker，受保护三容器保持原状态；双人只读审查未发现阻塞。

负责人已执行并确认 `build`、`prepare`、`rehearse`、`cutover` 四阶段均成功，退出码均为 0。正式前端已切换为本次镜像，实际材料如下。

| 材料 | 实际值 |
| --- | --- |
| 源码提交 | `9374fd0a8332d6d07273d622816b4f0979be516f` |
| Git 归档 SHA256 | `30b0ee1b054ab8d15f985ea72ce166815c6b18f42a4b5735e3851eb6be582ee1` |
| 前端镜像 ID | `sha256:1243f405e9a4ccbc110d8a55f2ab5dd0863dd6ad175f7784849fee88b20cf59a` |
| 前端镜像 tag | `goodhr-production-frontend:admin-uniform-9374fd0a` |
| 正式新前端容器 ID | `8d291e59f0f9d6ba01783a492efa71bda17323954a13a462b5eba1eea1eee617` |
| 正式前端启动时间（UTC） | `2026-10-14T18:47:26.95653031Z` |
| 已清理的 5182 候选前端容器 ID | `8a4c48f06b077c5efa3c0c31249a7a3c1cd3334c34e8e52a14ad19d0d1480e82` |
| 完整 PG dump 大小 | `104402130` bytes |
| 完整 PG dump SHA256 | `00d55e12a32a782c84646012a2db1141123734b9c69d3c1462f43124707daf30` |

候选与公网各 25 条页面匿名 GET 均返回 200；5 条私有 API 匿名检查均返回 401。Chrome ZIP 摘要、Chrome `1.0.9` 和 Firefox 已签名 `1.0.7` 的发布配置均保持不变；103 项迁移正文与正式 ledger 的只读检查通过。后端、PG、Redis 的精确容器 ID、镜像和启动时间保持原基线，未执行 SQL 写入、数据库恢复或后端重启。

负责人已完成 `cleanup`，退出码为 0，仅移除本轮 5182 候选容器；完整 dump、旧前端备份容器及全部镜像保留。清理后后端、PG、Redis 的完整 ID、镜像和启动时间仍与固定基线一致。另从 Windows 复核公网 6 条页面，均返回 200；Chrome `1.0.9` ZIP 摘要、Firefox 已签名 `1.0.7` 均通过，`Go-http-client/1.1` 旧 Agent 默认响应仍为兼容版本 `1.0.7`。

生产环境最终 17 项 UI 回归全部通过，日志记录 `17 passed (2.4m)`。浏览器 API 审计共 1430 条记录，其中 118 条非 GET；每条均为 `mocked=true`、`real_service_called=false`，UI 浏览器未调用真实业务 API、未写真实业务数据。发布器和负责人前述匿名 GET、只读 SQL 是另外执行的授权只读验收。

负责人核看正式页面截图：岗位管理 1366 / 390 宽、猎头推荐 1366 第 2 页 / 390 宽，以及个人配置正常配置状态 1366 宽，均符合尺寸、工具栏对齐、内容间距及分页边距要求。首轮测试定位调整为完整 Tabs 可点击控件，并校正个人配置模拟数据的真实接口形状；31–33px 高度断言保持，修正后重跑 17 项全部通过。这些修正只涉及测试，业务源码和已上线镜像保持本次提交，不需要重新构建。

实际证据（相对仓库根目录）：

- UI 最终日志：[output/admin-uniform-20261015/prod-ui-final.log](../output/admin-uniform-20261015/prod-ui-final.log)。
- 浏览器 API 审计：[output/admin-uniform-20261015/prod-api-fixture-audit-final.jsonl](../output/admin-uniform-20261015/prod-api-fixture-audit-final.jsonl)。
- 正式页面截图：[output/admin-uniform-20261015/prod/output/admin-uniform-20261015/](../output/admin-uniform-20261015/prod/output/admin-uniform-20261015/)。

构建、备份、候选验收、正式切换、候选清理和生产 UI 最终验收均已完成。线上各阶段由负责人执行，本文档编辑未操作生产环境。
