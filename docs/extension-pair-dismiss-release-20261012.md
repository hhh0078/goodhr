<!-- 文件作用：记录浏览器插件配对弹框关闭修复的代码范围、验证结果及仅前端正式发布证据。 -->

# 插件配对弹框关闭修复发布记录（2026-10-12）

2026-10-12 北京时间 02:11 已发布到 https://goodhr.58it.cn ，本地配对 UI 验收、生产切换和 20 项生产页面回归均已完成。

## 修复与授权边界

插件确认连接时，原组件把请求 `busy` 传给通用 `AdminDialog.loading`，导致关闭按钮、取消按钮、遮罩和 Esc 全部失效。本次仅将配对组件改用已有的 `confirmLoading`，等待时禁用确认按钮，保留四种关闭入口；通用弹框的表单提交行为保持原样。

- 关闭立即隐藏弹框，并清除 URL 的 `extension_pair`、`agent_port`，保留简历库的其他筛选参数。
- 状态按配对 ID、本机端口和当前账号隔离，邮箱优先，缺少邮箱时使用用户 ID。
- 关闭、卸载或更换配对后忽略旧请求结果；登录 token 变化后不更新弹框或弹出旧账号提示。
- 请求期间及授权成功后不重复批准；确认按钮与同步请求标记共同阻止连续点击。

关闭弹框不撤回已经发给本机 Agent 的授权请求。等待时文案为：“连接正在处理，关掉弹框后也可能完成，可回插件查看结果。”等待时取消按钮显示“关闭”，不把关闭描述为撤销授权。

## 代码与验证

代码从 `b60ea204` 基线开发，该基线包含生产 `f6b346ef` 及后续文档、测试补充。修复已合入 `main`。本次最终部署源码采用 `fb7c2e43`，相对先行已上线的 `0dfaf44a` 恰好只有配对组件、UI 测试和本文档三个文件变化，保留先行前端改动。

最初按 `b60ea204` / `f6b346ef` 准备发布时，被全局发布锁阻止，没有保存 `prepare` 材料，也没有操作容器。等待 `subscription-cards` 的 `0dfaf44ae1f91562ccb359684642a5e980a10e32` 上线后，重新核对旧生产基线，以最终源码成功完成本轮 `prepare` 并启动构建。

| 项目 | 当前记录 |
| --- | --- |
| Node 测试 | 26/26 通过 |
| 配对组件 TypeScript 检查 | `tsc` 通过 |
| 差异格式检查 | `diffcheck` 通过 |
| 配对 UI 回归 | 12 项不同用例全部通过：首轮 9 项、`selector-retry` 定向复测 2 项 pending、`esc-motion-retry` 定向复测 1 项 Esc |
| 账号与 token 变化 | 仅代码守卫审查，未作为 UI 已测结果 |
| 修复提交 | `7b66758b60a1902a83f305d0c25f003488ac5ee0` |
| 主分支合并提交 | `fb7c2e432da97abcdce1920e15cdb4c5dc7a1362` |
| 部署源码提交 | `fb7c2e432da97abcdce1920e15cdb4c5dc7a1362` |
| 前端源码归档 | `frontend-source-fb7c2e43.tar`，4188160 字节 |
| 前端源码归档 SHA256 | `d59571903d4233675f426640054883bb2987f740b3f57197fc57374148d646c3` |

配对 UI 实际覆盖四种关闭入口、筛选参数保留、pending 成功和失败的迟到结果隔离、配对 ID 与端口切换、重复批准、成功完成、失败重试、过期后新配对及无效端口。首轮剩余三项因关闭文案选择器重复和 Modal 入场动画时机未通过；调整选择器及等待真实动画完成后定向复测通过，仍保留原业务断言。

证据：`D:/work/goodhr-release-artifacts/20261012-extension-pair-dismiss/local-ui/pair-ui-evidence.json`。页面 HTML 与静态资源来自本地预览，云端业务接口及本机配对接口均使用测试替身，不代表真实插件授权验收。

本轮仅更新 Next.js 前端，不更新后端、本地客户端或浏览器插件包，不操作 PostgreSQL、Redis，不执行数据库迁移；迁移数量为 **0**。

## 发布基线与脚本

| 项目 | 记录 |
| --- | --- |
| 服务器 | `ldclouda31539` / `43.228.79.108`，准备及正式切换均重新核对 |
| 发布目录 | `/opt/goodhr-release/extension-pair-dismiss-20261012` |
| 旧生产源码提交 | `0dfaf44ae1f91562ccb359684642a5e980a10e32` |
| 旧生产 Build ID | `3GdLzYvRTpQqo_hyWmmDc` |
| 正式前端容器名 | `goodhr-production-frontend-1` |
| 旧前端容器 ID | `3d606b4e453a12a0d2d412ae9e11da20189fbd4356ab0c89811e38885a6ac74c` |
| 旧前端镜像 ID | `sha256:43dd0b08e31e35a2c85d1e53673cab7e57a581409b74328c8b5aff65e8bf06b3` |
| 旧前端启动时间 | `2026-10-11T18:05:17.822169047Z` |
| 候选容器名 | `goodhr-extension-pair-dismiss-20261012-candidate` |
| 旧前端备份名 | `goodhr-production-frontend-1-backup-extension-pair-dismiss-20261012` |
| 发布 helper | `frontend-pair-dismiss-release.py` |
| helper SHA256 | `1d038d8bdaf64edf1f17824ef5d545a754432a40f8095af29e1e152ca2e9f012` |
| helper 本地检查 | `py_compile`、`--help` 通过，29655 字节 |

helper 本地文件位于 `D:\work\goodhr-release-artifacts\20261012-extension-pair-dismiss\frontend-pair-dismiss-release.py`。复用已审查的前端发布流程，保存原容器完整配置，候选只监听环回 `5178`，正式前端继承原配置。各阶段核对固定容器身份、源码和镜像摘要；后端、PG、Redis仅核验，更新接口完整 JSON 摘要前后必须一致。切换失败只恢复保存的旧前端。

最终 helper 只替换正式前端 `BASELINE` 的容器 ID、镜像 ID、启动时间三元组。原主题基线版本保留在本地同目录的 `frontend-pair-dismiss-release-theme-baseline.py`，SHA256 为 `5eaeb460f51ee9e5c6a86684fd415f01cedc557ecb62ac6a95df28f95eda6146`。

已复核先行发布保存的数据库备份 `/opt/goodhr-release/subscription-cards-20261012/database.dump` 存在，93762669 字节，SHA256 为 `74fa827b693bab309c1db7d97e3f913d5ed0da68b2b9865a3465c3ad3374cd27`。这是先行发布的备份记录；本轮仍为仅前端发布、0 迁移，不写 PostgreSQL。

## 正式发布结果

| 项目 | 结果 |
| --- | --- |
| 生产配对 UI 验收与证据路径 | 20/20 首轮通过：配对 12 项、默认绿色和自由主题 6 项、简历保护及搜索 2 项；本地证据目录 `D:/work/goodhr-release-artifacts/20261012-extension-pair-dismiss/production-ui` |
| 构建日志、新镜像 ID、源码标签、新 Build ID | 构建 287 秒完成，编译、类型检查及 34 个页面生成通过；镜像 `sha256:a5208e3e904a3acca657162f96c4dab49fab114f7ceb2208e0e69e9c186767ed`，revision 为 `fb7c2e432da97abcdce1920e15cdb4c5dc7a1362`，Build ID `XGP7jQTBuMoTcQIyZaIpW` |
| 候选 ID、页面及静态资源检查 | 候选 `ed650f42f4ffe9fbad3d6770a77984783ec06b40395a249122f84bf7a2d2b0da`；三个页面及 12 份静态资源核对通过，检查后停止并删除本次候选 |
| 正式新容器 ID、启动时间、切换时间 | `0dbf4eb66895e56f563bc1e2b5f60cd90eb1fd91b70ef8f34173c9b7d403a766`；`2026-10-11T18:11:46.784509816Z`，北京时间 2026-10-12 02:11，正式切换检查约 5 秒完成 |
| 本机与公网 `/login`、`/admin/resumes`、`/download` | 全部 HTTP 200，运行 Build ID 符合本次镜像；12 份静态资源 SHA256 本机、公网与镜像一致 |
| 后端、PG、Redis身份与运行配置保持一致 | ID、镜像、启动时间、完整 Config/HostConfig/挂载与准备时一致，运行正常；四个正式容器重启次数均为 0；本机后端 `/health` HTTP 200，未登录消息摘要和简历库设置均为 401 |
| 客户端更新接口前后摘要 | 完整 JSON SHA256 均为 `f071fd60aa79a7859fd6eb3ce32b5b9d6a5aba3bc4a21fa6cd9fa5b14e696dad`，客户端更新配置未变化 |
| 旧前端保留、私有快照和最终发布报告 | 上表旧前端已停止并保存为备份名；发布目录保留完整配置、源码归档、前后快照、构建/候选/切换结果和 `production-final-verification.json` |

本次正式前端完整运行配置与旧前端一致，仅按构建结果更新镜像和源码标签。依赖 lock 的 LF SHA256 仍为 `60731c62cf4b2fb8516e035301fdca1c2889fea6ca1b709d2eec077266db98c7`，使用已有 Node 与依赖缓存镜像，未下载或安装依赖。后续发布记录提交不改变运行镜像的源码标签。

生产交互回归使用真实生产 HTML 与静态资源，全部云端业务及本机接口使用测试替身；使用合成账号、凭证与简历，没有登录或写入真实账号，也没有执行真实插件批准。匿名读取简历页包含预期 Build ID。页面回归首轮 20/20 通过，保留全部关闭、迟到回包、重复批准、配对切换及筛选参数断言。

回退使用本轮 helper 的 `rollback`，仅恢复上述旧前端，不回退数据库：

```sh
python3 /opt/goodhr-release/extension-pair-dismiss-20261012/frontend-pair-dismiss-release.py rollback --root /opt/goodhr-release/extension-pair-dismiss-20261012 --commit fb7c2e432da97abcdce1920e15cdb4c5dc7a1362
```
