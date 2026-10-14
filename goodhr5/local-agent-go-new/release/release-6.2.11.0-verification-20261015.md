<!-- 文件作用说明：记录候选人收藏分数功能、Windows 6.2.11.0 强制更新、生产发布与回滚验收证据。 -->

# 收藏分数及 Windows 6.2.11.0 发布验收

- 代码提交：`a6829a4b9d250492aebf1c823f734a04301aa920`，分支 `codex/candidate-favorite-score-20261015` 已推送并快进合入 `origin/main`。
- 岗位配置字段：`ai_config.favorite_score_threshold`，默认 0，范围 0–100。详情 AI 分数严格大于非零阈值时，在关闭详情前调用收藏，独立于打招呼开关和门槛。
- BOSS：当前详情右侧唯一收藏按钮，点击后确认“已收藏”。猎聘猎头：兼容正文/置顶入口，默认未分组单选框→保存→确认“已收藏”和弹层关闭。已收藏不再点击。智联和猎聘企业端直接跳过。
- 浏览器控制使用正式 Worker、标准 Locator、真实鼠标及滚轮；未向招聘页面注入脚本。
- 收藏失败走既有错误策略，详情使用独立 45 秒预算补关；猎聘仅补关本次实际详情，拒绝误关列表或其他候选人。
- 无新增数据库迁移。

## 本地验证

- 云端后端 `go test ./...` 通过；前端 TypeScript 检查通过，34 项单元测试通过，综合交互 66 项检查通过，收藏输入位置与回填截图已检查。
- 本地 Agent `go test ./...` 与 `go vet ./...` 通过；最后修改后的平台和流程测试重新通过。
- Worker 208 项测试：207 通过、1 项按配置跳过、0 失败。
- 真实已登录账户验证：BOSS 首次 1 次点击、猎聘猎头首次 3 次点击；重复调用均 0 次点击。验证后两位测试候选人恢复原未收藏状态。
- 7 个真实 Worker 本机页面场景通过：BOSS 内嵌/根页面、猎聘普通/置顶/长详情；两个可见同名目标均拒绝点击，隐藏浮层及候选人卡片同名操作未误点。原 3 个招聘标签保留并恢复原活动页。
- Windows 发布包隔离启动通过：health 版本 6.2.11.0，Node、Worker 文件和 Windows 原生组件均就绪。未运行安装器或真实招聘任务，只停止独立测试 PID，原 Agent/Worker 保持运行。
- macOS arm64/amd64 编译通过，未做 Mac 真机运行或安装包发布。用户自行打包 Mac。

## Windows 产物

- 安装器 ProductVersion：`6.2.11.0`。
- Go 构建记录：VCS 为上述提交，`vcs.modified=false`，生产地址 `https://goodhr.58it.cn`。
- 安装器在原工作树长路径触及 Windows 260 字符限制；相同程序、静态资产与安装脚本复制到短目录 `D:\gh62110-a6829a4b` 后编译成功，未改变源码或安装数据目录规则。
- 更新 ZIP：`https://oss2.58it.cn/GoodHR-Local-Agent-v6.2.11.0-windows-x64-1c56901e.zip`，30,699,940 字节。
- ZIP SHA256：`1c56901ee13fce07d265a93c66f7390b1d46556aa2c563350e6d8dc85c5bf65b`。
- 安装器：`https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.2.11.0-windows-x64-a8aab6ac.exe`，31,158,450 字节。
- 安装器 SHA256：`a8aab6acaeaab2c72d2a682918745d1a9c3c483f80098c7708dcdb57673b55f3`。
- Agent SHA256：`3f246bd212c57e9ad33286ec982c31d75b70aae3d47e801aa9f4378cd6786868`。
- 本机独立完整公网回读 ZIP/安装器摘要一致；ZIP CRC 通过，内含安装器、安装说明、安装教程图片三份平铺文件，嵌入安装器摘要一致。
- Windows 6.2.10.9→6.2.11.0 的强制更新判断已通过；仅 Windows 更新记录，Mac 仍使用原 6.2.10.7 记录。

## 生产发布

- 主机身份已核对：`ldclouda31539` / `43.228.79.108`。发布前后健康地址从实际容器配置确认，后端 `127.0.0.1:8086/health`、前端 `127.0.0.1:5175`。
- 本次云端发布目录：`/opt/goodhr-release/candidate-favorite-20261015-a6829a4b-r2`。
- 后端镜像：`goodhr-production-backend:candidate-favorite-a6829a4b`，`sha256:b5c137a51bc0ec8fc8586dbd10d72dee47dbec8c4db06102bfe4e0cc37cc90f3`。
- 前端镜像：`goodhr-production-frontend:candidate-favorite-a6829a4b`，`sha256:92c174e899a46404fdb0edc681b0ca0bd5a1a414c02816208d40b7eff7496823`。
- 正式双服务均 Running、RestartCount=0，源码标签为 `a6829a4b9d250492aebf1c823f734a04301aa920`，生产 PostgreSQL 和 Redis 未重启。
- 完整一致快照备份：`/opt/goodhr-release/windows-force-6.2.11.0-a6829a4b/database-before.dump`，104,388,596 字节，SHA256 `59c2c5b175332116ea1c7db1af5209fc6c2239b86c507fd3bf759f6cb4911fd6`。
- 无网络独立 PostgreSQL 18 恢复演练通过：47 张表的结构和行数、100 条迁移、系统配置全部一致；版本配置 CAS 发布及精确回退在演练事务内通过并回滚。仅移除本次演练容器，恢复数据和证据保留。
- 新服务独立验收通过：收藏分数缺省 0→80→0；101 返回 400，原配置未被覆盖；岗位页 200，构建 chunk 包含收藏字段和中文文案，未登录业务接口 401。
- 正式更新配置只前插一条 Windows 6.2.11.0 记录，历史记录 45→46；Mac 原有所有记录及 `system.app_config` 全行保持原样，原 minimum 6.2.10.1 保留。强制更新依当前系统最新记录与前三段版本判断，已用实际公网记录确认 Windows 必须升级、Mac 仍为 6.2.10.7。
- 父代理独立公网验收：`/`、`/login`、`/download`、`/admin/positions` 均 200；`/api/positions` 和 `/api/admin/users` 未登录均 401。更新 API 的版本、URL、SHA 与发布清单匹配，下载页包含新 Windows 链接。
- 原服务容器保留：`goodhr-production-backend-1-backup-candidate-favorite-20261015-a6829a4b-r2` 与 `goodhr-production-frontend-1-backup-candidate-favorite-20261015-a6829a4b-r2`。本轮脚本支持按精确容器身份回退；版本配置另有仅撤回本条记录的回退 SQL，不删除历史版本。
- Windows 发布证据：`/opt/goodhr-release/windows-force-6.2.11.0-a6829a4b/prepared.json`、`published.json`、`public-update-final-verification.json`。云端验收证据位于云端发布目录的 `canary-result.private.json`、`deploy-state.private.json`、`manifest.private.json`；私有文件未纳入仓库。

## 发布过程中已处理的限制

- `/www` 曾不足 1GB，专用上传返回 507。精确核对无容器引用后仅清理两个旧 Next 构建中间镜像；正式镜像和回滚材料保留。用户随后释放空间，发布前重新核对约 11GB 可用。
- 历史备份曾准备完整 `/opt` 副本，3955 个文件的摘要与元数据全部一致。用户释放空间后未切换原备份位置、未移除原目录或建立链接；副本保留在 `/opt/goodhr-release/archive-www-goodhr-backups-20261015`。
- 第一次恢复演练在 PostgreSQL 临时 Unix 服务阶段误判就绪；等待条件改为 TCP 及目标数据库 `SELECT 1`，使用原已验证备份继续演练。生产未受影响，首次日志保留。
- 第一次后端构建受隔离 `/tmp` 128MB 限制；调整至 512MB，使用新的 `-r2` 发布目录，保持源码和原 Go 构建环境。首次目录和日志保留。
