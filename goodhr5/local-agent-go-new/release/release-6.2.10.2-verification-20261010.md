<!-- 文件作用说明：记录岗位停止404修复、云端部署和Windows 6.2.10.2可选发布的产物、验证及回滚证据。 -->

# Windows 6.2.10.2 与岗位停止修复

源码提交：`eda74de5088c2de5b18fceeff4851c5d3f8002c0`，已推送功能分支并合并到 `main`。

## 修复范围

- 本地没有岗位任务记录时，不再将 SQLite 的 `sql.ErrNoRows` 当成停止失败；使用本次登录凭证请求云端专用停止接口，并检查保存结果，不创建虚假任务或改写历史统计。
- 运行中的任务先检查岗位归属，再等待安全停止点；保留浏览器。停止请求与启动检查使用岗位停止代数，防止停止完成后旧启动继续运行。
- 云端停止接口检查状态写入错误，只有保存成功才返回确认。旧版确认结构保持兼容。
- 网页只在旧本地程序明确返回 `404 / TASK_NOT_FOUND` 时请求云端收尾。登录失效、网络超时和其他错误不会进入此兼容分支。

## Windows 产物

- 正式下载：[GoodHR 6.2.10.2 Windows ZIP](https://oss2.58it.cn/GoodHR-Local-Agent-v6.2.10.2-windows-x64-b8423be4.zip)
- ZIP：27,768,895 字节。
- ZIP SHA256：`b8423be4d3252f64637c6f8fca0a764c6e4757d205c505918cdc6cc762cfb607`
- 安装器 SHA256：`c3ca5b472d0bca5faa701f71e63c89dd92628a4b38b35912bad2d147e850f9bc`
- Agent SHA256：`79ed91f39636251d2f4dc96d5096b579584cf66f48748802c5a55776f7903c4f`
- 原安装教程 SHA256：`a1f82022a6d79f6de675b41695223ae8dc6df47fb8843ed5755e331f58319db8`
- 构建目录：`D:\gh62102-release`。发布清单：`goodhr5/local-agent-go-new/release/release-6.2.10.2-manifest.json`。
- 包内安装器、ZIP CRC、安装教程、生产域名和内置版本均已核验；从公网下载的ZIP与本地文件哈希相同。

## 可选更新验证

只增加第四段版本，`6.2.10.1 → 6.2.10.2` 不触发必要升级判断。真实登录页面显示“这次是小修补，晚点更新也行”，有“暂时忽略”按钮；点击后关闭弹窗并继续使用原 `6.2.10.1`。

只在 `system.onboarding_config.local_agent` 最前面增加新版本：38条历史全部保留，总数39。Mac入口、运行组件及其他引导字段保留，`system.app_config` 没有修改。正式写入前，在同一事务中执行发布与真实撤回SQL并确认原完整配置，然后回滚演练。提交后更新API、下载页、公开ZIP、内置安装器和教程哈希均通过。

本机没有安装新版。原Agent、Worker、浏览器进程保持运行；发布安装包与用户实际选择安装为两个步骤。

## 测试

- 最终归档Go：580项通过、29个包通过，0失败。6项需要显式浏览器环境的测试未启用，保留现有浏览器窗口。
- Worker：199项通过，0失败，0跳过。生产依赖为 `camoufox-js 0.12.0`、`playwright-core 1.60.0`。
- 云端Go全套通过。
- 前端21项单元测试及TypeScript检查通过；本机依赖Junction无法完成Next生产构建，服务器真实Docker生产构建成功。
- 新停止回归覆盖无历史、旧历史、重复停止、当前任务安全点、其他任务不受影响、账号/登录错误、云端写入失败、空确认、SQLite真实错误及慢启动/停止竞态。

## 服务器与恢复材料

目标主机 `ldclouda31539 / 43.228.79.108`，正式站点 `goodhr.58it.cn`，项目目录 `/www/wwwroot/goodhr-production`。

本次备份：`/www/wwwroot/goodhr-production/backups/stop-fix-20261010-193811`。目录权限700，私密文件600。

- PostgreSQL custom备份：`database.dump`，90,559,041字节。
- 数据库备份 SHA256：`94b8c7bb3f432a8e4c44fc20b708a22307b228feb218a7ec9b4ee2702c30fd5f`。
- `pg_restore --list` 独立验证通过，507行，包含岗位与系统配置；不执行数据库恢复。
- 102个迁移文件与生产镜像正文一致，原始哈希差异仅CRLF/LF；规范化清单SHA256：`0891ed41f0dc7ee095294b7f89d8a62b60d336c3b179d1cbf47223d51c949cbc`。
- 数据库90条正向迁移均已执行，待执行0条。
- 容器参数与环境：`before-inspect.private.json`、`backend.private.env`、`frontend.private.env`。
- 更新配置备份：`local-agent-config-before.private.json`。
- 可选版本回滚：`rollback-local-agent-6.2.10.2.sql`，只撤回版本数组，带并发检查，保留后续修改的其他字段。
- 容器切换及回滚：`deploy-containers.py`，依据容器ID和源码标记核对目标，切换失败自动恢复旧容器；不会恢复业务数据库或重启PostgreSQL/Redis。

## 正式服务器验收

前后端分别在隔离端口8087/5176完成候选验证，然后切换正式8086/5175。后端保留原邮件调度设置；正式环境变量、挂载、网络、端口、日志及重启参数完整保留。PostgreSQL/Redis容器ID、镜像和启动时间没有变化。

| 服务 | 镜像 | 镜像ID |
| --- | --- | --- |
| 后端 | `goodhr-production-backend:stop-fix-20261010-eda74de5` | `sha256:ffee191a25c85ee8749d3a920ce0e3bd8349b47372630bf9a4f48ac4b174a5a1` |
| 前端 | `goodhr-production-frontend:stop-fix-20261010-eda74de5` | `sha256:f75818fd3c79bfed739903cdfdc78dccaf13073ee8cd2e4be4acae945adf1a0b` |

- 当前后端容器：`d8d88934a863ef682fa0fd18d78bfe4a953bb9e2192e1ddae031562db79cbe73`。
- 当前前端容器：`f80e95b8164f19e90ebca7cfd67c32d4fdc31ef99f421f9af90fd61e23977e08`。
- 前端BUILD_ID：`lG98M4Go-My_RndROhxXt`，候选与正式相同。
- 主机本地后端 `/health` 返回200；公网 `/login`、`/admin/positions` 及实际静态JS返回200，静态文件字节/哈希与新镜像一致；匿名配置访问返回401。
- 旧容器保留：`goodhr-production-backend-backup-20261010-193811` 和 `goodhr-production-frontend-backup-20261010-193811`。候选已清理。
- 部署证据：同备份目录下 `build-result.json`、`candidate-verification.json`、`deployment-result.json`、`production-verification.json`。

## 实际账号停止验收

在已登录的正式岗位页面刷新加载新前端，点击“金融渠道经理”的“停止”。仍连接本机6.2.10.1；旧本地程序返回无记录404后，新网页通过专用云端接口停止岗位。

- 岗位：`8fe9d74d-94e4-449c-bdca-9fe9a2941d93`。
- UI按钮从“停止”变回“开始”；PostgreSQL独立确认 `status=stopped`。
- 停止时间：`2026-10-10T12:01:02.879911Z`。
- 原统计保留：今日打招呼100、扫描223、跳过122、失败1；启动时间没有改写。
- 原本机Agent/Worker/浏览器PID仍为23812/35256/53660，health版本仍6.2.10.1。
- 截图：`C:\Users\Administrator\.codex\deployments\stop-fix-20261010-193811\position-stopped-verified.png`。
- 可忽略更新截图：同目录 `optional-update-6.2.10.2.png`。

本机旧版原始停止接口仍是旧行为；直接调用本地接口的修复随6.2.10.2提供。网页已兼容旧版，因此用户暂不更新也能停止此次无记录岗位。
