<!-- 文件作用说明：记录 AI 助手、猎头客户岗位、简历向量化及消费保留的交付范围、复验命令、部署回滚方法与尚未通过的真实环境验收；不把模拟测试当作生产验收。 -->
# AI 助手与猎头客户验收

记录日期：2026-10-22。以下状态区分代码验证、独立数据库验证与真实服务验收。向量和消费清理的详细合同见 [AI向量与消费保留.md](AI向量与消费保留.md)。

## 已实现范围

- 侧栏把企业招聘归入 **HR**，客户管理、客户岗位管理和猎头推荐归入 **猎头**；原平台岗位与简历库继续共用。顶栏 AI 入口仅显示图标，带中文提示和无障碍名称。
- 全局对话弹框可关闭，关闭和页面断线不取消后台任务。每条用户消息创建独立任务；同一会话串行执行，使用 `request_id` 幂等，刷新主动按任务游标重连。启动前先等待本地完整会话快照保存成功，内容包括用户消息、原请求编号和待确认助手条目；缓存失败不启动后台任务，重试沿用原编号。创建回包丢失时，先按原请求编号查询任务元信息，再接回原任务。
- Redis 会话在最后一条**新用户消息**后保留 2 小时；读取、心跳、模型输出、重连及同编号重试均不续期。本地 IndexedDB 按团队和账号隔离，最多保存 10 个会话，不设应用过期时间；新建第 11 个时移除最早结束的会话，不淘汰运行中或发送结果未确认的会话。浏览器主动清理存储后无法保证本地历史仍在。
- 模型必须输出完整 JSON 事件；代码围栏、普通文字和不合规结构均拒绝。前后端之间使用带 Bearer 鉴权的 fetch SSE，事件先保存再推送，按任务内递增编号补发去重。思考区展示可公开的进度摘要，正文出现后自动折叠；每次工具调用保留名称、入参、出参、耗时、尝试次数和安全错误。
- 仅开放两个**只读工具**：`search_positions` 查找并获取完整岗位，`search_candidates` 根据已读取的岗位及当前偏好搜索简历。没有自动联系、自动推荐写入、自动面试或录用决策工具。格式或工具失败后把历史与错误交回模型，同一步最多尝试 5 次，包含首次；余额、权限和未确认付费状态不盲目重试。
- 岗位推荐包含需求解析、可靠条件预过滤、BM25 与职业概况/经历向量三路检索、应用层 RRF、精排、逐项证据分析及复核。用户改条件会重新检索其有权访问的全库；“优先”不变成必须，未知不冒充符合，年龄、性别、婚育不替代能力。
- 结果分为 `recommendations` 和 `pending_verification` 两个区域，后者始终标为待确认。关键条件或相关经历没有正向证据时不进入符合名单。`partial_failure=true` 显示“部分分析未完成，已核实结果见下”；即使没有正文或正式推荐，待确认池仍显示。证据展开先展示逐字引文和来源路径，再提供逐项 JSON 详情及原简历入口。
- 恢复本地历史和每次打开弹框时，对已结束任务的最终结果及工具结果中的两个候选池统一复核引用，每批最多 200 条，不收费、不续会话有效期。任一简历删除、版本或权限失效，清空该轮正文、工具和结果并提示重新检索；网络未确认时保留本地历史及用户消息，隐藏旧候选资料、已核实标题和原简历入口，联网重开可继续复核。
- 客户保存甲方公司、联系人、合同链接和负责人；甲方岗位保存完整职责、要求、年薪万元及与现有平台岗位的绑定。服务端检查团队、管理权限及编辑版本；联系人和合同按权限脱敏。绑定不启动平台招聘任务，不改动原有简历、推荐和企业招聘历史。
- 简历库右上设置弹框展示当前权限内真实简历的总数、当前版本已完成数和待处理数。向量化独立提交并立即返回 **202**，提示后台执行、稍后刷新；不等待整库完成，也不依附下载导入开关。资格由服务端检查内置 AI 配置、余额及持久化/搜索条件，不新增会员或额度门槛。

## 接口与配置

除内部消费清理接口、内置向量 Key 接口外，以下业务接口均使用当前登录 Bearer，服务端自行确定团队与账号。

| 接口 | 用途 |
| --- | --- |
| `POST /api/assistant/sessions` | 创建/幂等恢复会话；可传本地 UUID `id`，`history` 只接受普通 user/assistant 文字，不恢复工具权限 |
| `GET /api/assistant/sessions/{id}` | 查询 `active_run` 与各任务的 `id/request_id/status`，恢复丢失回包 |
| `POST /api/assistant/sessions/{id}/messages` | 输入 `content/request_id`，返回 `session_id/run_id` |
| `GET /api/assistant/runs/{id}/events?after=N` | SSE 事件回放与后续推送；JSON 含 `id/session_id/run_id/type/data/created_at` |
| `POST /api/assistant/runs/{id}/cancel` | 停止指定任务 |
| `POST /api/assistant/references/validate` | 按 `candidate_id/resume_version` 免费核对最多 200 个历史引用，返回 `invalid`；不依赖 Redis 会话是否过期 |
| `GET/POST /api/resume-library/vectors` | 读取数量与资格；提交长期向量化任务，POST 返回 202 |
| `GET /api/headhunter/metadata` | 有权访问的有效成员与现有平台岗位选项 |
| `GET/POST /api/headhunter/customers`、`GET/PUT/DELETE /api/headhunter/customers/{id}` | 客户查询和维护；更新携带 `revision`，删除有关系检查 |
| `GET/POST /api/headhunter/jobs`、`GET/PUT/DELETE /api/headhunter/jobs/{id}` | 甲方岗位与 `position_ids` 关系维护；更新携带 `revision`，不级联删除平台岗位 |
| `POST /api/ai-compatible/v1/embeddings` | 固定模型向量接口，使用本人内置 AI Key；网页不直接处理密钥 |
| `POST /api/internal/ai-usage/cleanup` | 内部清理接口；只接受专用请求头密钥，普通登录态不能调用 |

部署沿用已有 `GOODHR_PG_DSN`、`GOODHR_REDIS_ADDR`、Redis 密码/DB 与加密配置。生产缺少 Redis 时禁止创建这些付费后台任务，不以进程内缓存冒充可恢复存储。

- `GOODHR_SEARCH_URL`、`GOODHR_SEARCH_API_KEY`：管理员固定配置的 Elasticsearch 地址与可选 ApiKey。索引为 `goodhr_resume_qwen37_v1`，校验方案 `qwen37-flash-1024-chunks-v1` 和 1024 维；不接受模型提交的 SQL、搜索 DSL、地址或密钥。实际 Elasticsearch 版本、功能和许可仍须部署验收。
- `GOODHR_RERANK_URL`、`GOODHR_RERANK_MODEL`、`GOODHR_RERANK_API_KEY`：可选专用精排服务。未配置时明确标记为有界聊天模型精排 `llm_reranker`，不是已经接通专用模型。
- 在原系统配置中设置内置 AI 上游地址、上游 Key 与聊天模型；用户在个人配置选择本站内置接口。只接受 HTTP(S) 并精确匹配主机，拒绝把路径、查询参数里的本站域名或相似域名当成本站，拒绝 userinfo 伪装；余额使用原钱包。
- 向量固定使用 `qwen3.7-text-embedding-flash`、1024 维，每批最多 20 段。按本次确认的中国北京**同步接口**价格：输入 **0.125 元/百万 Token**，输出不收费，钱包最小单位为 0.0001 元并向上取整。后台异步不等于官方 Batch API，不能套用 Batch 输入价。官方依据：[Qwen3.7-Text-Embedding-Flash 模型页](https://help.aliyun.com/en/model-studio/qwen3-7-text-embedding-flash)。
- `GOODHR_AI_USAGE_CLEANUP_TOKEN`：至少 32 位随机密钥，供后端及受限环境文件中的清理脚本使用，不放进 URL、页面、任务标题或公开日志。

## 迁移与实际限制

后端启动时按 `db/migrations` 顺序执行未登记迁移；部署包必须包含迁移目录，并从后端工作目录启动。升级前备份业务库、原构建和配置，核对迁移记录及实际表/触发器；旧库若没有迁移记录，既有引导逻辑可能只登记文件，不能仅以启动成功判断新结构已建立。

| 迁移 | 内容 |
| --- | --- |
| `0099_headhunter_customers_jobs.sql` | 猎头客户、甲方岗位及平台岗位绑定数据 |
| `0100_ai_usage_daily_retention.sql` | 七日前消费明细日汇总与清理索引，不改变余额或资金批次 |
| `0101_resume_vector_receipts.sql` | 按团队、候选人和简历版本保存向量分片与索引回执 |
| `0102_resume_search_invalidation.sql` | 归属变更递增版本；更新/删除在原业务事务中登记旧搜索副本清理待办 |

当前执行边界：

- 一轮后台任务最长 25 分钟；单实例最多同时执行 4 个助手任务，工具循环最多 16 步。Redis 会话最多 40 轮任务、单任务最多 4000 条事件、整个会话最多约 4 MiB；达到边界提示新开会话，不代表本地历史按时间删除。
- 模型使用最近最多 60 条对话上下文，恢复入口最多接收 100 条普通历史，每条历史最多 20000 字；本地保留历史不等于模型可无限读取全文。
- 三路检索每路最多 500 份命中，RRF 最多保留 1000 份；当前实现只取经权限和版本核对的前 100 人进入精排，最多对前 50 人做深入分析。符合池和待确认池分别最多 10 人。这些是第一版调用边界，不是千万级性能承诺。
- 推荐内部模型预算累计最多 120 次调用、300000 输入 Token、100000 输出 Token；修复也计入。达到预算保留已经核实的人选并明确部分失败，不能把初排当成完成分析；外层助手编排另受 16 步限制。
- 向量任务保留 7 天，每账号只有一个活动任务；正式环境 Redis 租约限制全局最多两个向量执行者。成功分片先持久保存，再写索引；索引失败可复用已付费向量，避免重复收费。付费调用回执不明或重启恢复为 `uncertain` 时停止并提示核对，不自动重复请求。
- 简历变更会使旧版本完成数失效，旧副本按持久待办异步清理；后续向量化任务补齐缺失/更新版本。没有索引或索引延迟时不能据此声称已搜索完整真实库。服务重启可回放保存的事件，不承诺自动续跑进程中断时未确认的付费模型调用。
- 本轮服务器只读侦察：4 GB 内存、约 1.2 GB 可用，Docker 所在分区约余 4.4 GB；业务库有 35332 份简历、167 个团队，约 0.99 GB。未发现现成 Elasticsearch 服务或配置，生产尚未配置搜索地址、专用精排和消费清理密钥。已提出独立搜索服务、扩容或先交付源码的选择，等待用户决定，尚未安装、扩容或修改生产。

## 已做验证与复验

- [x] 前端最终 `npm test`：133/133 通过；缓存引用与即时刷新补丁后，TypeScript 检查及正式构建再次通过。
- [x] `tests/ui/assistant-headhunter.spec.ts` 的原 10 个新增 UI 场景已分轮验证：流式 JSON、工具日志、两种刷新恢复、坏 JSON、十会话、团队/账号隔离、撤权清理、双池/部分失败/原文证据、向量后台提交、客户与岗位绑定，以及四个旧页面的桌面手机入口。随后新增已完成缓存撤权、版本变化、简历删除、网络失联与重开、失败任务工具内引用 5 项；这 5 项加正常流回归共 6/6 通过（30.8 秒）。最后新增“请求刚到后端便刷新且丢回包”，与既有丢回包、正常流共 3/3 通过（11.3 秒），确认无需等待 150 毫秒仍恢复原请求且只提交一次。当前文件共 16 项；最新 TypeScript 及差异格式检查通过，测试预览已停止。
- [x] 原个人配置 UI 7/7 通过；原岗位工具栏、猎头推荐维护、简历设置保存定向 UI 3/3 通过。以上均使用合成账号和模拟 API，没有真实服务写入。
- [x] 新 Go 定向测试、独立 PostgreSQL 迁移/事务/隔离测试、独立 Redis 幂等/TTL/回放/租约测试通过；模型和搜索响应使用受控模拟服务，不能等同真实 Qwen 或 Elasticsearch。
- [x] 同时配置两项隔离 PostgreSQL DSN 的全量 `go test -race ./... -count=1 -json`：退出码 0，耗时 249.428 秒；顶层 444 项通过、19 项跳过，包含子测试合计 845 次通过、28 次跳过、0 失败。跳过项没有计为通过。`go vet ./...` 退出码 0。
- [x] 缓存引用复核补丁后，`go test -race ./internal/httpapi -run '^TestAssistant' -count=1` 再次通过（7.644 秒），包含新增引用核对测试；`go vet ./...` 通过。上述全量数量属于补丁前的完整记录，没有把定向测试充作全量重跑。
- [x] 独立环境从 `0001` 到 `0102` 的完整迁移及后端启动、健康接口验证通过；这项结果不代表生产已迁移或功能已完成真实联调。
- [x] 云端后端 `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build` 通过，确认产物为 Linux x86-64 静态二进制。所有本轮临时 Redis、PostgreSQL 和后端预览进程均已精确结束；没有清理其他容器。

从仓库根目录进入对应目录复验；先使用已安装依赖，若要安装则先检查国内镜像。

```bash
cd goodhr5/cloud/frontend-next
npm test
./node_modules/.bin/tsc --noEmit --incremental false
```

UI 使用两个终端，均在 `goodhr5/cloud/frontend-next`。终端 A 启动独立预览：

```bash
NEXT_PUBLIC_CLOUD_API_BASE=http://127.0.0.1:8084 NEXT_PUBLIC_LOCAL_AGENT_PORT=43139 ./node_modules/.bin/next dev --hostname 127.0.0.1 --port 5196
```

终端 B 只运行模拟业务接口，不连接真实招聘平台或 Local Agent：

```bash
UI_TEST_BASE_URL=http://127.0.0.1:5196 UI_UNIFORM_AGENT_PORT=43139 ./node_modules/.bin/playwright test tests/ui/assistant-headhunter.spec.ts tests/ui/personal-ai-config.spec.ts
UI_TEST_BASE_URL=http://127.0.0.1:5196 UI_UNIFORM_AGENT_PORT=43139 ./node_modules/.bin/playwright test tests/ui/resume-library.spec.ts --grep '岗位与猎头推荐工具单行|猎头推荐独立菜单|简历库下载导入默认'
UI_TEST_BASE_URL=http://127.0.0.1:5196 UI_UNIFORM_AGENT_PORT=43139 ./node_modules/.bin/playwright test tests/ui/assistant-headhunter.spec.ts --grep '顶部只有AI图标|已完成推荐刷新|历史引用核对网络|工具已有待确认'
UI_TEST_BASE_URL=http://127.0.0.1:5196 UI_UNIFORM_AGENT_PORT=43139 ./node_modules/.bin/playwright test tests/ui/assistant-headhunter.spec.ts --grep '顶部只有AI图标|任务创建回包丢失后|发送请求刚到后端'
```

测试复跑时截图写入仓库根目录 `output/ai-headhunter-20261022/`；本轮23个验收文件已单独归档至 `/Users/Zhuanz/Documents/Codex/2026-10-22/new-chat/outputs/GoodHR-AI-验收-20261022/`，未混入源码提交。已目视核查 AI、待确认结果、向量设置、客户岗位与四个既有页面的桌面/手机截图。构建前停止开发预览，避免共用 `.next`：

```bash
npm run build
```

Go 在 `goodhr5/cloud/backend` 运行。PostgreSQL 用例须显式配置回环地址、库名以 `_test` 结尾的 `GOODHR_CANDIDATE_TEST_DSN` 和 `GOODHR_RECHARGE_TEST_DSN`；Redis 用例启动独立临时 `redis-server`，不清空现有实例。环境缺失导致的 skip 不能记为通过。

```bash
go test -count=1 ./internal/httpapi -run 'Test(Assistant|ResumeVector|ResumeSearch|Embedd|Headhunter|UsageRetention)'
go test -race ./... -count=1 -json
go vet ./...
```

## 启动、人工验收与回滚

1. 在目标环境完成 PostgreSQL、Redis、受信 Elasticsearch 和内置 AI 配置，应用上述迁移后，从 `goodhr5/cloud/backend` 启动：`GOODHR_CLOUD_ADDR=127.0.0.1:8084 CGO_ENABLED=0 go run ./cmd/server`。生产使用已编译二进制及现有进程管理；密钥从受限环境文件加载。
2. 前端构建时设置实际 `NEXT_PUBLIC_CLOUD_API_BASE`，运行既有 Next.js 启动方式。反向代理需支持 SSE，关闭事件流缓冲，并确保空闲超时覆盖 15 秒心跳；不能只验证 HTTP 200。
3. 用验收账号建立甲方客户、完整岗位并绑定平台岗位；配置本人内置 AI。在简历设置检查数量，点击向量化确认立即受理，关闭弹框、刷新后核对真实完成数。
4. 通过 AI 图标输入“按某岗位找候选人”，核查先查完整岗位再查简历、调用入出参、可读原文证据、待确认分区。处理中刷新核对仍是同一任务；同请求编号重试不重复调用或扣款。修改偏好后核查重新检索，撤权/更新后旧结果不可继续使用。
5. 消费清理先核对账本迁移与金额一致，再按引用文档配置宝塔每日任务。MCP 已恢复只读连接，但**生产计划任务尚未创建**。

回滚先停止新任务入口及消费清理计划，保存数据库、Redis 和向量回执；不要为回滚直接删客户岗位表、日汇总或原始简历。只回退前后端程序时保留新增数据及迁移记录，重新部署再核对当前版本和权限。索引可重建，但重算向量可能收费；更换模型/维度/分片方案时用新索引回填后切换，禁止新旧空间混用。

**消费清理一旦启用，旧程序必须保留读取 `ai_usage_daily` 的兼容实现。** 旧消费明细已经汇总删除，不能直接换回只统计明细的版本，否则历史消费少计；充值、退款、余额和资金批次始终保留。具体操作见引用文档。

## 尚待验收

- [ ] 生产发布、0099–0102 实际迁移及部署后联调。
- [ ] 真实 Elasticsearch 的版本/许可/索引、三路召回、增量失效清理、规模与并发性能。
- [ ] 真实 Qwen 向量、聊天及可选专用精排渠道的合同、Token、钱包扣款和异常回执。
- [ ] 30～50 个真实岗位的人工标注及独立测试集；比较关键词、混合检索、精排与证据分析，记录 Recall@K、Precision@10、nDCG@10、未知/证据错误、延迟与费用。
- [ ] 宝塔每日消费清理任务实际创建、受限密钥配置、手工运行及汇总金额核对。

以上未验收项保持未勾选，模拟 API、启动成功、截图或单测都不能替代。
