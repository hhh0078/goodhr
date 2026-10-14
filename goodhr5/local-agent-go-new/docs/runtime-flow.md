<!-- 文件作用说明：定义本地程序启动、启动前检查、主动打招呼和自动回复的主流程。 -->

# 本地程序流程

## 支持的平台与运行组件安装

本地程序分别打包为 Windows x64 和 macOS（Apple Silicon、Intel）版本，公共安装流程必须兼容三个目标。

| 能力 | Windows | macOS | 实现位置 |
| --- | --- | --- | --- |
| 下载资源 | `win-x64` 清单 | 按芯片选择 `darwin-arm64` / `darwin-x64` 清单 | `internal/runtime/installer.go` 的 `platformKey` |
| Node 查找与安装 | 查找 `node.exe`，原地解压并检查版本 | 查找 `node`，保留 ZIP / tar.gz 的可执行权限与 `bin` 目录，原地检查版本 | `internal/runtime/installer.go`、`manager.go`、`node_executable.go` |
| Camoufox 查找与安装 | 查找 `camoufox.exe`，原地登记实际组件目录 | 保留 `.app/Contents/MacOS` 与 `.app/Contents/Resources`，检查执行权限 | `internal/runtime/component_paths.go`、`installer.go`、`manager.go` |
| OCR 查找与安装 | 查找 `RapidOCR-json.exe` 并检查 ONNX 模型，原地登记 | 查找 Vision 可执行文件、检查执行权限，不要求 ONNX 模型 | `internal/runtime/component_paths.go`、`manager.go` |
| 短命令执行 | 隐藏临时控制台窗口 | 使用普通可取消子进程，不引用 Windows API | `internal/runtime/command_windows.go` / `command_other.go` |
| 原生运行库 | 检查 VC++ x64 运行库 | 不执行 Windows 运行库检查 | `internal/runtime/system_runtime_windows.go` / `system_runtime_other.go` |

Node、Camoufox 和 OCR 都使用免安装压缩包，不调用系统安装器。安装顺序为：下载、SHA256 校验、解压到独立 `runtime/node-runtime-*`、`runtime/camoufox-runtime-*` 或 `runtime/ocr-runtime-*` 目录、检查实际可执行文件与对应平台要求、原子写入 `installed-components.json` 的相对 `executable_path` 和 `directory_path`。Node 还须实际执行并确认主版本至少为 22。验证及保存成功后直接使用新路径，不再移动、覆盖或删除旧组件目录；重启时重新读取，目录整体迁移后相对路径仍有效。失败时清理本次新目录并保留旧目录与旧安装记录。旧固定 `runtime/node`、`runtime/camoufox`、`runtime/ocr`、系统 Node 和没有路径字段的历史记录继续兼容。已成功安装的目录不能当作临时目录清理；旧版本暂时保留，避免文件占用时清理失败影响本次安装。

Camoufox 的 `version.json` 写在实际组件根目录；Worker 的 `CAMOUFOX_INSTALL_DIR`、`CAMOUFOX_BINARY_PATH`、UBO、语言包、字体与浏览器原生资源补丁统一使用这个目录。Mac 中组件根目录是 `Camoufox.app` 的父目录，不能设成 `Contents/MacOS`。显式 `CAMOUFOX_BINARY_PATH` 仍优先，根目录按对应系统的应用结构解析。浏览器文件检查通过只能说明文件已安装，不能代表系统一定允许启动；真正的系统执行策略问题仍需启动日志确认。

OCR 客户端在 `internal/bootstrap/application.go` 通过 `ocr.NewWithExecutableResolver(runtimeManager.OCRPath, ...)` 组装，每次准备启动时解析当前安装路径，避免界面显示已安装但识别进程仍使用旧固定目录。已运行的旧 Worker 或 OCR 进程保留原目录，重新启动本地程序后切换到最新安装；不为组件更新关闭用户正在用的浏览器。

Windows 的 `rename ... Access is denied` 只能确认目录移动被拒绝，不能单凭报错断言安全中心拦截；文件或目录占用、访问权限和安全软件都可能涉及。新流程减少对目录重命名权限及文件占用时机的依赖。若可执行文件本身仍被禁止启动，安装应在版本检查阶段明确失败，不能登记为可用。安装失败日志保留系统、组件、阶段、路径和原始错误，便于进一步区分原因。

回归要求：Windows 实际复现浏览器子文件被占用导致的 `Access is denied`，验证无需移动的新目录登记、旧目录被占用时的完整安装、版本文件被占用时保留旧记录；公共测试验证重启、目录整体迁移、ZIP / tar.gz、缺文件与模型、路径安全及实际 Node 子进程收到的浏览器环境。Windows x64、macOS arm64、macOS amd64 都需编译，Mac 可执行权限、双架构依赖与真机安装需在 Mac 验证。交叉编译通过不代表 Mac 真机验证完成。

2026-10-12 验证记录：Windows `go test ./... -count=1` 通过；Node 安装回归覆盖文件占用、状态文件占用、并发状态读取、失败保留旧版本、ZIP / tar.gz、重启和相对路径迁移，真实 Node v22.14.0 原地安装与执行冒烟通过。Windows x64 主程序及 macOS arm64 / amd64 主程序与运行组件测试程序编译通过。macOS 真机运行、原生依赖和安装包验证尚未完成；本次未发布新版安装包。

2026-10-13 追加验证：Windows 实际复现 Camoufox 子文件占用时 `rename ... Access is denied`，原地登记通过；浏览器与 OCR 的旧目录占用、损坏包、缺可执行文件/模型、版本记录占用、重启与路径迁移回归通过。真实 Node 子进程确认浏览器根目录、版本和 UBO 环境正确，OCR 客户端确认安装后使用新路径。将本机 Camoufox 152.0.4-beta.30 样本打成 ZIP 后，完整经过下载、校验、解压、记录路径、资源检查、Go → Worker → 无界面浏览器启动/关闭，测试通过；使用隔离 Profile，没有访问招聘页面。macOS arm64 / amd64 主程序和运行组件测试程序交叉编译通过，Mac 真机安装、执行权限与浏览器运行尚未验证。独立 Windows 测试包使用正式站点地址，服务器仅增加下载文件，不修改应用配置、组件清单或自动更新配置。

## 1. 程序启动流程

```text
main
  1. 解析启动参数
  2. 加载本地配置
  3. 创建必要目录
  4. 检查固定端口是否已有健康的 GoodHR 实例
  5. 启用 10MB、3 份备份的本地文件日志
  6. 清理过期临时截图、组件压缩包和更新包
  7. 打开 SQLite、执行迁移、恢复异常中断任务并清理过期摘要
  8. 创建 Runtime Manager
  9. 创建 Browser Worker Manager 和 Client
  10. 创建 Cloud、AI、OCR Client
  11. 创建 Task Flow
  12. 注册本地 HTTP API
  13. 启动下载同步、服务和云端控制台
  14. 监听退出信号并按任务、服务、浏览器、下载同步、Worker、OCR、SQLite 顺序优雅清理
```

程序启动只保证本地服务可用，不应自动开始岗位任务。

固定端口已有实例时，必须先读取健康接口并核对状态、版本、端口和数据目录。确认是 GoodHR 后只打开现有控制台并退出；无法确认时不得结束占用端口的其他程序。

## 2. 任务启动入口

所有任务统一进入：

```text
StartTask
  1. 解析强类型请求
  2. RunPreflightChecks
  3. 获取任务和 Profile 运行锁
  4. 保存本地 running 状态
  5. 同步云端 running 状态
  6. 启用系统防睡眠
  7. DispatchTaskFlow
  8. 返回已开始
```

后台流程必须持有可取消的 Context。

## 3. 启动前检查

`RunPreflightChecks` 使用顺序步骤列表：

```text
checks = [
  ValidateRequest,
  CheckLocalProgram,
  CheckCloudSession,
  LoadPositionSnapshot,
  CheckSubscriptionWhenRequired,
  LoadUserPreferences,
  LoadLocalPlatformConfig,
  CheckProfile,
  CheckTaskConflict,
  CheckNodeRuntime,
  CheckWorkerBuild,
  CheckWorker,
  CheckCamoufox,
  CheckLocalStorage,
  CheckRequiredAI,
  CheckRequiredOCR,
  CheckPowerGuard,
]
```

执行规则：

- 按顺序执行。
- 每步记录开始、成功、失败和耗时。
- 必需检查失败立即停止。
- 可选检查失败记录 warning，并明确降级结果。
- 返回完整的 `PreflightResult`，后续流程直接使用，不重复读取相同配置。
- 免费关键词和 OCR 打招呼任务不检查 AI 或会员；自动回复和明确要求 AI 的岗位才检查会员。
- AI 岗位要求云端会员状态同时满足 `active=true` 和 `allow_ai=true`；自动回复还必须满足 `allow_auto_reply=true`，当前只有 Max 全能版开放。
- 本地检查是云端启动检查后的第二道保护，不能根据 `plus`、`max` 字符串自行推断权限。
- 平台 URL、行为和选择器只加载本地平台目录的 `config.json`，不请求云端平台配置；自动回复缺少消息地址或有效目标选择器时立即拦截。

## 4. 流程分发

```text
DispatchTaskFlow
  task_type=greeting   -> GreetingFlow
  task_type=auto_reply -> AutoReplyFlow
  其他                 -> UNSUPPORTED_TASK_TYPE
```

不同主流程不互相调用。

## 5. 主动打招呼流程

```text
GreetingFlow
  1. 启动或复用浏览器
  2. 打开对应 Profile
  3. 获取平台 Runtime
  4. 准备平台入口页
  5. 选择岗位并应用筛选
  6. 扫描当前可见候选人
  7. 检查运行中的云端登录态
  8. 后台并发执行候选人基础信息 AI 预判断
  9. 按页面顺序每 150ms 检查下一位预判断结果，完成一位立即处理一位
  10. 执行基础过滤
  11. 按个人配置等待并读取候选人详情
  12. 执行关键词或 AI 最终判断
  13. AI 流式 score、reason 完整后立即继续页面流程
  14. 收藏阈值大于 0 且详情 AI 分数严格大于阈值时，调用平台收藏能力
  15. 关闭详情并等待
  16. 调用平台打招呼能力
  17. 按最终 AI 分数决定是否索要电话、微信或简历
  18. 需要索要资料或发送首次招呼语时，优先复用聊天框或点击卡片“继续沟通”；首次消息才打开右侧联系人侧边栏
  19. 完成聊天动作后关闭聊天框和联系人侧边栏
  20. 岗位开启结构化简历时，后台接收 AI 剩余字段并异步同步云端
  21. 保存候选人结果
  22. 按个人配置休息、翻页或真实滚动
  23. 同步候选人发现数量、累计统计和最终状态
  24. 完成、停止或失败统一收尾
```

主流程只决定顺序。Boss、智联、猎聘页面差异由平台 Runtime 实现。

岗位 `ai_options.favorite_score_threshold` 默认 `0`，表示不开启收藏。公共流程仅使用已经打开详情后的最终 AI 分数，严格大于收藏阈值才在关闭详情前调用 `FavoriteCandidate`；等于阈值不收藏。收藏独立于“打招呼”开关和打招呼分数：只检查模式、未达到招呼分数时仍按收藏分数执行。关键词模式、没有详情 AI 分数或未打开详情时不收藏。Boss 和猎聘猎头端执行收藏，智联和猎聘企业端直接返回并继续流程；公共流程不按平台名分支。

收藏失败不记为成功，错误交给统一候选人错误策略；退出详情步骤时使用独立 45 秒预算补关详情，避免候选人取消或总时限已到导致弹窗残留。补关失败与原错误同时保留，会话失效和无法确认关闭的致命错误仍立即停止岗位。

收藏判断和详情清理共用一份跨平台 Go 流程，没有 Windows 专属路径或系统调用；Windows x64、macOS arm64、macOS amd64 都复用这里的规则。收藏页面差异只在 `internal/platform/boss` 和 `internal/platform/hliepin`，浏览器动作由共享 Worker 执行。`greeting/favorite_test.go` 通过合成详情和 AI 响应验证分数边界、招呼独立性、空操作及失败清理；它不代替 macOS 真机浏览器和安装包验证。

本次流程验证（2026-10-15）：`go test ./internal/flow/greeting ./internal/integration/cloud` 通过，`go test -c ./internal/flow/greeting` 分别以 Windows amd64、macOS arm64 和 macOS amd64 目标编译通过。macOS 仅完成交叉编译，真机运行尚未验证。

主动打招呼后的聊天动作必须留在推荐页：当前聊天框属于候选人时直接复用；不是首次消息且卡片已有“继续沟通”时直接点击打开聊天框，不打开联系人侧边栏；只有首次消息、卡片还没有“继续沟通”时，才点击推荐页右侧联系人入口并从侧边栏查找当前候选人。全部路径都不得跳转独立沟通页面。索要资料和发送消息共用同一聊天框，结束后统一关闭聊天框与可能打开的联系人侧边栏。

岗位选择的公共顺序为：读取当前岗位、必要时打开岗位列表、输入精简搜索词、读取结构化岗位项、点击匹配项、再次读取当前岗位复核。智联按旧版规则每次直接通过职位弹层选择搜索后的第一项，不读取不稳定的当前岗位文字，并验证弹层已经关闭；猎聘猎头端按旧版当前生效规则跳过页面切岗，只保存岗位上下文供开聊弹框使用。

需要 OCR 时，Go 调用通用长截图能力。Worker 只用真实鼠标滚轮生成分段，Go 逐段 OCR 并合并文本；任何详情读取或 OCR 失败都会执行详情补关。智联和猎聘猎头端在 AI 判断前还会调用平台详情浏览能力，用真实滚轮模拟查看，不使用 JS 滚动。

每批候选人开始前重新检查云端登录态。401/403 会停止任务；临时网络错误只记录 warning，避免短暂断网误停。个人配置中的列表停留、详情前后等待、打招呼前等待、滚动等待和模拟休息都使用可取消定时器；模拟休息只写本地日志，不修改招聘页面。

主动打招呼默认不限制扫描批数，不得再使用固定三批作为结束条件。每批完成后调用平台的统一候选人列表推进能力，持续加载下一批；只有达到岗位本次打招呼上限、平台确认没有更多候选人、用户手动停止、云端状态不允许继续或统一错误策略触发时才结束。若云端以后明确下发 `max_batches` 或 `scan_rounds`，才按该值限制批数。结束前必须写一条用户可见的原因说明。

候选人没有稳定编号时直接跳过，避免同一个人被重复打招呼。关键词岗位需要详情时，先读取详情再做最终关键词判断；OCR 和多模态 AI 使用的临时截图在读取后立即删除。

基础信息 AI 预判断最多并发处理 5 位，但主流程始终按页面原顺序消费；第一位结果没回来时每 150ms 检查一次，第一位完成后立即处理，不等待整批。最终 AI 判断采用两段式流式消费：`score` 和 `reason` 完整后即可关闭详情、打招呼和处理下一位；岗位开启 `output_structured_resume` 时，剩余结构化简历字段继续在后台接收并异步同步云端，失败只记 warning，不回滚页面动作。

猎聘猎头端分页不依赖可能隐藏的“下一页”。平台 Runtime 从第 2 页开始，使用公共精确文本选择器依次点击 `2、3、4、5...`；每次点击后仍通过公共列表变化检测确认新候选人已出现。

## 6. 自动回复流程

```text
AutoReplyFlow
  1. 启动或复用浏览器
  2. 打开对应 Profile
  3. 获取平台 Runtime
  4. 打开消息页面
  5. 每轮检查云端登录态
  6. 扫描未读会话
  7. 读取会话上下文
  8. 判断是否需要人工接管
  9. 调用 AI 生成回复
  10. 执行发送前安全检查
  11. 调用平台回复能力
  12. 保存会话和回复结果
  13. 继续下一条或按间隔等待
  14. 完成、停止或失败收尾
```

自动回复不得复用主动打招呼的完整主流程，只复用明确的公共能力。

## 7. 平铺步骤规范

每个主流程文件必须能从一个顶层方法看清主要顺序。

允许：

```text
Run
  -> PrepareBrowser
  -> PreparePlatform
  -> Scan
  -> Decide
  -> Act
  -> Persist
```

不允许：

```text
Run -> PrepareBrowser -> PreparePage -> SelectPosition -> Scan -> Decide
```

后者把流程藏进多个方法深处，日志和维护都会变困难。

步骤方法可以调用下一层基础能力，但不能偷偷接管后续主流程。

## 8. 任务停止

```text
StopTask
  1. 标记用户主动停止
  2. 通知当前步骤尽快结束
  3. 等待安全点
  4. 超时后取消 Context
  5. 关闭当前详情
  6. 保存 stopped 状态
  7. 同步云端摘要
  8. 按产品规则决定是否保留浏览器窗口
```

停止任务不得直接杀死整个本地程序。

## 9. 失败处理

- 步骤失败必须带任务 ID、步骤名、平台、错误码和 trace ID。
- 找不到选择器时保留 Worker 详细诊断。
- 可重试错误按配置重试，记录当前次数。
- 连续相同错误达到阈值后停止当前任务，避免无限点击。
- 当前主动打招呼流程连续 3 个候选人出现同类错误时停止任务。
- 单个候选人最多处理 180 秒；浏览器被关闭或 OCR 组件不可用时立即停止，单张图片没有识别到文字只跳过当前候选人。
- 用户关闭浏览器与程序异常必须使用不同状态。
- 失败收尾不能覆盖用户主动停止状态。
- 未捕获 panic 也必须进入同一套失败收尾，释放 Profile、停止防睡眠并保存明确错误。
- 任务每 30 秒检查一次时间断层；休眠恢复断层超过 2 分钟时按失败停止，避免继续操作过期页面。
- 岗位开启提示音时，打招呼成功播放成功音，任务失败播放失败音。
- 任务失败通过 `/api/fail-notice` 请求云端发送邮件；正常停止只同步 stopped，避免重复邮件。
- 任务完成同步要求云端返回 `notice_sent=true`，否则最多重试三次；云端通知失败只记 warning，不把已经完成的本地任务改成失败。

## 10. 状态归属

- 真实运行状态归属 Go 本地程序。
- 网页只是控制台，刷新网页不能终止任务。
- Worker 只保存浏览器会话状态，不保存业务任务状态。
- 进程重启后根据 SQLite 恢复可展示状态；不能假装中断任务仍在运行。

## 11. 下载监听流程

```text
页面触发下载
  -> Firefox 原生下载管理
  -> pending
  -> 浏览器保存文件、同名处理并写入原生历史
  -> saved 或 failed
  -> 浏览器观察器写入本地原子快照
  -> Worker 校验并同步状态
  -> Go 每秒主动同步终态
  -> SQLite 去重保存
  -> 首次 saved 显示十秒提示
  -> 用户可打开文件、在 Finder 中定位或暂不处理
```

下载同步是平台无关流程。Worker 不反向调用 Go，也不写 SQLite；Go 不参与浏览器下载文件的实际保存。

切换下载目录时，Go 先校验绝对路径，再请求 Worker 配置；Worker 等待浏览器确认新目录后返回成功，Go 才登记为允许打开文件的目录。打开或 Finder 定位下载文件时，会解析软链接后的真实路径并再次检查目录边界。旧目录中的历史下载仍可打开，清空 GoodHR 下载记录不会删除实际文件或浏览器原生历史。

程序退出时，浏览器在关闭前将原生下载的最终状态落盘；Worker 关闭浏览器后读取最后一份快照，再删除本次临时状态目录。Go 执行最后一次同步后才停止 Worker。浏览器负责原生下载保存与取消，Worker 不再等待或另存 Playwright 管理的临时文件。

## 12. 运行组件安装流程

```text
控制台提交强类型 manifest
  -> 校验当前系统资源、HTTPS 地址和完整 SHA256
  -> 按 Node、Camoufox、OCR 顺序处理
  -> 下载到临时文件并报告进度
  -> 校验 SHA256
  -> 安全解压到临时目录
  -> 原子替换目标目录，失败时恢复旧目录
  -> 保存组件版本
  -> 更新 Worker 的 Node 和 Camoufox 路径
```

Node 必须为 22 或更高。Worker 就绪检查同时验证编译入口和 `cloakbrowser` Node 依赖。OCR 可选，但岗位要求 OCR 时启动前检查必须通过；安装包有额外单层目录时，运行管理器和 OCR 客户端都会递归查找可执行文件。

## 13. 本地程序更新流程

```text
控制台提交下载地址和目标版本
  -> 比较当前版本
  -> 校验 HTTPS 地址和完整 SHA256
  -> 下载更新包并报告进度
  -> 校验下载文件 SHA256
  -> 安全准备 pkg 或 zip 安装器
  -> 用 macOS 默认安装方式打开
```

本地程序更新与 Node/Camoufox/OCR 运行组件更新分开，不能放进同一个管理器。

## 14. 日志和本地数据保留

- Go 和 Worker 的结构化步骤日志统一按任务编号写入岗位日志，每个岗位只保留最近 1000 条。
- 主程序和 Worker 文件日志单个最多 10MB，各保留 3 份历史文件。
- 本地任务、候选人、会话、下载和步骤摘要保留 90 天。
- 临时截图和组件下载压缩包保留最多 24 小时，程序更新包保留最多 7 天。
- 用户下载目录、浏览器 Profile、Cookie 和书签不参与自动清理。

## 15. 首条真实筛选结果与教程事实

关键词拒绝和纯关键词最终判断属于筛选终态。关键词通过后仍需要 AI 最终评分时，仅发布 `preview`、`terminal=false`，不能提前完成教程；AI 最终评分返回后才发布终态。

教程事实通过已有云端接口在后台同步，每次任务最多一个请求在途。成功后不重复，失败只记录日志，后续真实终态可重试；同步有独立的 3 秒预算，不阻塞候选人流程。任务统计只持有原子同步状态的指针，该状态不进入 JSON 统计，也不改变扫描、成功、失败或跳过计数。上传证据仅有 `kind`、`phase`、`terminal`，不包含候选人资料。

此能力位于公共 `internal/flow/greeting` 和 `internal/flow/shared`，Windows、macOS Apple Silicon 与 Intel 复用相同代码，不调用系统专属能力。2026-10-15 已通过流程与云端单元测试，以及 `windows/amd64`、`darwin/arm64`、`darwin/amd64` 编译；macOS 真机运行尚未验证。定向 `go test -race` 已尝试，但当前 Windows 环境没有 GCC，无法构建 race 所需的 CGo。
