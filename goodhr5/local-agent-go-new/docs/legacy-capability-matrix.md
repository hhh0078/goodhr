<!-- 文件作用说明：逐文件记录旧本地程序能力在新版中的归属、迁移状态和仍需真实页面确认的配置。 -->

# 旧版能力逐文件核对表

状态说明：

- `已迁移`：新版已有明确代码归属。
- `公共替代`：旧版平台重复代码已由强类型公共能力替代。
- `旧版空实现`：旧文件本身没有页面操作，新版只保留扩展入口。
- `明确不迁移`：旧能力当前已停用或只服务非 macOS，已核实后不带入新版。
- `待云端配置`：执行代码已存在，但旧版没有可确认的选择器或页面地址，不能伪造。
- `待真实回归`：代码和配置模板已准备，本轮按要求没有启动真实账号验证。

## 平台公共层

| 旧文件 | 旧能力 | 新归属 | 状态 |
|---|---|---|---|
| `internal/platformcore/runtime.go` | 平台统一接口、候选人和详情模型 | `internal/platform/model/runtime.go` | 已迁移，改为强类型细粒度接口 |

## Boss 直聘

| 旧文件 | 旧能力 | 新归属 | 状态 |
|---|---|---|---|
| `boss/runtime.go` | 平台身份、候选人辅助数据 | `boss/runtime.go`、`common/runtime.go` | 公共替代 |
| `boss/entry.go` | 打开推荐页、关闭确认弹框、入口判断 | `boss/entry.go`、`boss/config.json`、`common.OpenVerifiedPage` | 已迁移，弹框为初始化动作列表；最终地址跳到登录页会明确失败 |
| `boss/navigation.go` | URL、岗位名和选择器辅助 | `model.Config`、`common/runtime.go` | 公共替代 |
| `boss/position.go` | 当前岗位读取、搜索、列表选择 | `boss/position.go`、`common.SelectPosition` | 已迁移，增加点击后复核 |
| `boss/candidate.go` | 候选人列表、定位、滚动、姓名加年龄稳定指纹 | `boss/candidate.go`、`common.FindCandidates` | 已迁移；缺少年龄时不生成稳定指纹 |
| `boss/detail.go` | 打开、读取、清理牛人分析器内容、关闭详情 | `boss/detail.go`、通用点击/读取 | 已迁移，DOM 和 OCR 合并后统一执行平台清理 |
| `boss/screenshot.go` | 详情真实滚轮分段截图和 OCR 输入 | `element.screenshot-long`、`greeting.readDetailWithOCR` | 已迁移为平台无关分段 OCR，待真实回归 |
| `boss/greet.go` | 卡片内打招呼 | `boss/greet.go`、通用候选人作用域点击 | 已迁移 |
| `boss/followup.go` | 基础筛选、索要信息 | `boss/position.go`、`boss/followup.go` | 旧版空实现；新版支持配置驱动，选择器待云端配置 |
| `boss/helpers.go` | `map[string]any` 解析和 Worker 响应拆包 | 强类型协议和 `common/runtime.go` | 公共替代 |
| `boss/runtime_test.go` | Boss 平台身份、入口和候选人规则回归 | `common/runtime_test.go`、`config_template_test.go` | 旧测试逐项核对，规则由公共测试覆盖 |

## 智联招聘

| 旧文件 | 旧能力 | 新归属 | 状态 |
|---|---|---|---|
| `zhaopin/runtime.go` | 平台身份、候选人辅助数据 | `zhaopin/runtime.go`、`common/runtime.go` | 公共替代 |
| `zhaopin/entry.go` | 打开推荐页和入口判断 | `zhaopin/entry.go` | 已迁移 |
| `zhaopin/navigation.go` | 岗位名、搜索词和选择器辅助 | `common.PositionSearchQuery`、`common.SelectPosition` | 公共替代 |
| `zhaopin/position.go` | 每次直接打开职位弹层、搜索、点击第一项并确认弹层关闭 | `zhaopin/position.go`、`zhaopin/config.json` | 已迁移，保留不读取当前岗位文字的旧版规则 |
| `zhaopin/candidate.go` | 候选人列表、定位、滚动、指纹 | `zhaopin/candidate.go`、通用候选人能力 | 已迁移 |
| `zhaopin/detail.go` | 打开、读取、AI 前真实滚轮浏览、关闭并复核详情 | `zhaopin/detail.go`、通用详情和真实滚轮 | 已迁移，详情未关闭时会再按一次 Escape，待真实回归 |
| `zhaopin/greet.go` | 卡片内打招呼 | `zhaopin/greet.go` | 已迁移 |
| `zhaopin/followup.go` | 继续沟通、索要电话二次确认、微信、附件简历、消息、关闭聊天框 | `zhaopin/followup.go`、`zhaopin/config.json` | 已迁移，待真实回归 |
| `zhaopin/helpers.go` | 动态 map 和 Worker 辅助 | 强类型协议和公共能力 | 公共替代 |
| `zhaopin/runtime_test.go` | 智联入口、岗位、详情和沟通规则回归 | `common/runtime_test.go`、`config_template_test.go` | 旧测试逐项核对，规则由公共测试和配置模板覆盖 |

## 猎聘企业端

| 旧文件 | 旧能力 | 新归属 | 状态 |
|---|---|---|---|
| `liepin/runtime.go` | 平台身份和辅助数据 | `liepin/runtime.go`、公共能力 | 公共替代 |
| `liepin/entry.go` | 打开推荐页和入口判断 | `liepin/entry.go` | 已迁移 |
| `liepin/navigation.go` | 页面、岗位和选择器辅助 | 强类型配置和公共能力 | 公共替代 |
| `liepin/position.go` | 当前岗位、岗位列表选择 | `liepin/position.go`、`common.SelectPosition` | 已迁移 |
| `liepin/candidate.go` | 候选人列表、滚动、指纹 | `liepin/candidate.go` | 已迁移 |
| `liepin/detail.go` | 打开、读取、关闭详情 | `liepin/detail.go` | 已迁移 |
| `liepin/greet.go` | 候选人打招呼 | `liepin/greet.go` | 已迁移 |
| `liepin/followup.go` | 基础筛选、索要信息 | `liepin/position.go`、`liepin/followup.go` | 旧版空实现；新版支持配置驱动 |
| `liepin/helpers.go` | 动态 map 和 Worker 辅助 | 强类型协议和公共能力 | 公共替代 |
| `liepin/runtime_test.go` | 猎聘企业端入口和候选人规则回归 | `common/runtime_test.go`、`config_template_test.go` | 旧测试逐项核对，规则由公共测试覆盖 |

## 猎聘猎头端

| 旧文件 | 旧能力 | 新归属 | 状态 |
|---|---|---|---|
| `hliepin/runtime.go` | 当前岗位、开聊方式和候选人辅助 | `hliepin/runtime.go` | 已迁移为单任务状态 |
| `hliepin/entry.go` | 打开找人页和入口判断 | `hliepin/entry.go` | 已迁移 |
| `hliepin/navigation.go` | 页面、岗位、详情辅助 | 强类型配置和公共能力 | 公共替代 |
| `hliepin/position.go` | 保留岗位上下文、跳过自动切岗 | `hliepin/position.go` | 已迁移 |
| `hliepin/search.go` | 发布职位/快捷搜索；后来停用自动改变页面筛选 | `hliepin/position.go`、岗位快照字段 | 已迁移当前生效行为：沿用用户手动结果 |
| `hliepin/candidate.go` | 表格候选人过滤、稳定简历 ID、动态文字清理、翻页 | `hliepin/candidate.go`、`hliepin/config.json` | 已迁移 |
| `hliepin/detail.go` | 点击候选人并等待新标签页、读取、AI 前真实滚轮浏览和关闭 | `hliepin/detail.go`、通用新标签页等待和真实滚轮 | 已迁移，待真实回归 |
| `hliepin/stable_click.go` | 唯一作用域、位置稳定、只点一次 | 通用 `ClickAction`、候选人父级选择器 | 公共替代 |
| `hliepin/greet.go` | 清理弹层、立即沟通、选择职位/不选职位、立即开聊、Esc 收尾 | `hliepin/greet.go`、`hliepin/config.json` | 已迁移，待真实回归 |
| `hliepin/followup.go` | 复用聊天框、姓名校验、索要信息、二次确认、关闭聊天框和联系人抽屉 | `hliepin/followup.go`、`hliepin/config.json` | 已迁移，待真实回归 |
| `hliepin/helpers.go` | 动态 map 和 Worker 辅助 | 强类型协议和公共能力 | 公共替代 |
| `hliepin/runtime_test.go` | 猎聘猎头端开聊、翻页和稳定编号规则 | `hliepin/helpers_test.go`、`config_template_test.go` | 已迁移关键纯规则测试 |

## 平台注册

| 旧文件 | 旧能力 | 新归属 | 状态 |
|---|---|---|---|
| `internal/platforms/registry.go` | 四个平台运行时注册和别名归一化 | `internal/platform/registry.go` | 已迁移 |
| `internal/platforms/registry_test.go` | 平台编号和未知平台回归 | `internal/platform/registry.go`、全量 `go test` | 规则保留，未知平台仍返回明确错误 |

## 旧岗位主流程

| 旧文件 | 旧能力 | 新归属 | 状态 |
|---|---|---|---|
| `positionrunner/runner.go` | 启动参数、任务锁、总流程依赖 | `flow/lifecycle`、`flow/preflight` | 已迁移；个人偏好改为强类型快照 |
| `positionrunner/lifecycle.go` | 启动、停止、浏览器和防睡眠收尾 | `flow/lifecycle/runner.go` | 已迁移；失败邮件和失败提示音在唯一最终状态后执行 |
| `positionrunner/pipeline.go` | 候选人批次编排 | `flow/greeting/flow.go` | 已迁移为同步平铺流程 |
| `positionrunner/scan.go` | 入口、岗位、扫描、滚动和云端状态检查 | `preflight`、`greeting.processBatches`、`auto_reply.processConversations` | 已迁移；每批和每轮都会检查登录，临时网络错误只告警 |
| `positionrunner/candidate.go` | 去重、判断、打招呼、索要信息、拟人等待和休息 | `greeting.processCandidate`、`greeting/pacing.go` | 已迁移；索要信息要求最终 AI 分数严格大于岗位阈值 |
| `positionrunner/detail.go` | 详情生命周期、OCR/AI、退出补关 | `greeting.processCandidate`、长截图 | 已迁移；详情失败也会统一补关 |
| `positionrunner/decision.go` | 关键词、AI 决策和页面浮层 | `greeting.matchesKeyword`、`integration/ai`、`shared/runtime.go` | 已迁移；`enable_thinking` 控制同步通用浮层 |
| `positionrunner/error_policy.go` | 连续相同错误停止、浏览器关闭和 OCR 组件故障立即停止 | `greeting.processBatches`、`integration/ocr` 稳定错误码 | 已迁移；单图无文字只跳过当前候选人，连续 3 个其他同类错误停止任务 |
| `positionrunner/persistence.go` | 本地候选人、统计和日志 | `storage`、`lifecycle/logger.go` | 已迁移为不保存敏感详情的摘要 |
| `positionrunner/notification.go` | 声音、完成邮件重试和失败通知 | `internal/system/notification`、`integration/cloud.SyncCompletedSummary`、`SendFailNotice` | 已迁移为 macOS `afplay` 系统音；完成邮件未确认时最多重试三次，失败邮件使用强类型请求 |
| `positionrunner/command_other.go` | 非 Windows 子进程配置 | 新版目标 macOS，无额外动作 | 公共替代 |
| `positionrunner/command_windows.go` | Windows 隐藏窗口 | 当前 macOS 版本不启用 | 待 Windows 打包阶段 |
| `positionrunner/error_policy_test.go` | 连续同类错误、OCR 错误分类和不可继续错误 | `greeting.processBatches`、`integration/ocr/client_test.go` | 规则已迁移，当前 Go 全量测试通过 |
| `positionrunner/runner_test.go` | 云端快照、偏好、详情、打招呼和通知综合回归 | `cloud/client_test.go`、`greeting/pacing_test.go`、平台公共测试 | 已拆成强类型小测试，不复制动态 map 大测试 |
| `positionrunner/viewport_test.go` | 旧固定视口值 | 无 | 明确不迁移；旧版固定视口调用当前已停用 |

## 旧 Node Worker

| 旧文件 | 旧能力 | 新归属 | 状态 |
|---|---|---|---|
| `worker-node/src/index.js` | 会话、页面、元素、平台专用路由、截图、下载、Cookie、浮层 | `worker/src` 分层目录、`browser/session/download-manager.ts`、`flow/download`、`storage/download.go` | 已拆分；下载监听含处理中、成功、失败、后缀、重名和最近 100 条，Go 主动同步 SQLite 并显示打开提示 |
| `browser-actions.js` | 浏览器基础与高级动作 | `primitives`、`actions` | 公共替代 |
| `human-type.js` | 随机字符输入 | `InputAction`、`KeyboardPrimitive` | 已迁移 |
| `hliepin-stable-click.js` | 猎聘稳定单击 | 通用 `ClickAction.waitForStablePosition` | 公共替代 |
| `detail-ready.js` | 等待详情出现 | `FindAction` 的单元素和列表超时轮询 | 公共替代 |
| `detail-scroll.js` | 详情滚动参数 | 通用 `ScrollAction`、长截图动作 | 公共替代 |
| `boss-scroll-anchor.js` | Boss 列表滚轮落点 | `SelectorSpec.wheel_anchor` | 公共替代 |
| `boss-scroll-diagnostic.js` | Boss 滚动诊断 | 通用滚动结构化日志 | 公共替代 |
| `list-click-scroll-diagnostic.js` | 列表点击滚动诊断 | 通用查找、滚动和点击日志 | 公共替代 |
| `candidate-match.js` | 页面候选人文字匹配 | 平台候选人指纹和候选人作用域选择器 | 公共替代 |
| `greet-policy.js` | 平台打招呼后续策略 | Go 平台 `greet.go` | 已迁移到正确职责层 |
| `navigation-target.js` | URL 目标判断和已有标签页复用 | `browser/session/navigation.ts`、`BrowserSession.open` | 已迁移；命中现有页面时不导航，保留用户手动筛选 |
| `browser-display.js` | 视口和缩放诊断 | `LocatorPrimitive.view` | 只迁移只读视口；旧固定视口校准调用已停用，明确不迁移 |
| `profile-process.js` | 清理占用 Profile 的浏览器进程 | `profile`、Worker 会话复用 | 旧清理逻辑仅 Windows 生效，当前 macOS 阶段明确不迁移 |
| `ai-overlay-policy.js` | 浮层重复和存活时间 | 通用 `OverlayAction` | 已迁移 |
| `test-screenshot.js` | 人工截图调试脚本 | 不进入生产代码 | 不迁移 |

## 旧 Worker 测试文件逐项核对

| 旧测试文件 | 旧验证重点 | 新版覆盖 |
|---|---|---|
| `ai-overlay-policy.test.js` | 浮层存活时间和重复更新 | `OverlayAction` 运行时校验和 TypeScript 严格类型 |
| `boss-scroll-anchor.test.js` | 列表滚轮落点选择 | 通用 `wheel_anchor` 和四个平台配置模板 |
| `boss-scroll-diagnostic.test.js` | 滚动前后诊断 | `ReadPrimitive.scrollState` 读取最近可滚动父级 |
| `browser-actions.test.js` | 基础动作参数和错误 | Worker 请求运行时校验、统一错误和 TypeScript 类型检查 |
| `browser-display.test.js` | 固定视口校准 | 旧生产调用已停用，明确不迁移 |
| `candidate-match.test.js` | 候选人文字匹配 | `common.FindCandidates`、候选人指纹和 Go 公共测试 |
| `detail-ready.test.js` | 详情等待规则 | `LocatorPrimitive.resolve` 和 `resolveAll` 超时轮询 |
| `detail-scroll.test.js` | 详情滚动参数 | 通用真实滚轮和长截图动作 |
| `greet-policy.test.js` | 打招呼后续策略 | 四个平台 `greet.go`、`followup.go` |
| `hliepin-stable-click.test.js` | 猎聘唯一元素和稳定位置 | `ClickAction` 稳定位置、严格选择器和猎聘配置 |
| `human-type.test.js` | 随机逐字符输入 | `InputAction`、`KeyboardPrimitive` |
| `list-click-scroll-diagnostic.test.js` | 列表点击和滚动诊断 | 通用查找、移动、滚动、点击日志 |
| `navigation-target.test.js` | URL 包含匹配 | `worker/test/navigation-target.test.mjs`，已保留 3 个边界用例 |
| `profile-process.test.js` | Windows Profile 进程匹配 | 当前目标 macOS，明确不迁移 |

旧 Worker 测试没有原样复制；对应生产规则迁到公共层后，由 TypeScript 编译、Node 内置测试和 Go 强类型测试共同检查。

## 旧应用与本地 HTTP 层逐文件核对

| 旧文件 | 旧能力 | 新归属 | 状态 |
|---|---|---|---|
| `internal/app/app_update.go` | 程序更新状态、下载、版本比较、安全解压和启动安装器 | `internal/updater/`、`internal/api/update.go` | 已迁移，增加 HTTPS、完整 SHA256 和安全压缩包规则 |
| `internal/app/app_update_test.go` | 版本比较和 zip 越界回归 | `internal/updater/app_test.go` | 已迁移 |
| `internal/app/browser_focus.go` | 浏览器启动或打开页面后尝试把桌面窗口置前 | Worker `page.bringToFront()` | 公共替代；不再额外执行 AppleScript 或 PowerShell 抢占系统前台 |
| `internal/app/command_other.go` | 非 Windows 子进程参数 | Go 标准 `exec.Cmd` | 公共替代 |
| `internal/app/command_windows.go` | Windows 隐藏命令窗口 | 无 | 待 Windows 打包阶段 |
| `internal/app/console_update.go` | 下载和安装本地静态控制台 | 云端控制台和 `internal/system/console/opener.go` | 明确不迁移；新版不再维护第二份本地控制台包 |
| `internal/app/diagnostics.go` | 目录、端口、运行组件和 Profile 锁诊断 | `internal/api/diagnostics.go` | 已迁移 |
| `internal/app/files.go` | 下载文件路径校验、打开、定位和桌面提示 | `internal/api/downloads.go`、`internal/system/files/`、`internal/system/notification/download.go` | 已迁移；文件接口允许默认目录和 Worker 已成功使用过的下载目录，并阻止软链接越界 |
| `internal/app/files_notwindows.go` | 非 Windows 的提示兼容入口 | `internal/system/notification/download.go` | 已迁移为 macOS 十秒操作提示 |
| `internal/app/files_test.go` | 下载目录越界保护 | `internal/api/server_test.go` | 已迁移 |
| `internal/app/files_windows.go` | Windows 原生下载提示窗口 | 无 | 待 Windows 打包阶段 |
| `internal/app/opener.go` | 等待服务、复用已有实例、解析控制台地址并附加本地端口 | `internal/system/console/opener.go` | 已迁移；确认健康身份后复用实例并直接打开云端控制台 |
| `internal/app/opener_other.go` | 非 Windows 默认浏览器打开 | `open` 系统命令 | 已迁移为当前 macOS 实现 |
| `internal/app/opener_test.go` | 控制台 URL 参数回归 | `internal/system/console/opener_test.go` | 已迁移 |
| `internal/app/opener_windows.go` | Windows 默认浏览器打开 | 无 | 待 Windows 打包阶段 |
| `internal/app/server.go` | 旧路由集中注册、Worker 转发和静态控制台托管 | `internal/api/`、`internal/bootstrap/`、强类型 Browser Client | 已拆分迁移；平台专用 Worker 转发和本地静态控制台明确取消 |
| `internal/app/server_values_test.go` | 宽松动态参数和固定视口兼容 | `internal/api/server_test.go`、Worker 请求校验测试 | 公共替代；核心请求不再使用动态 map |

## 旧浏览器 Go 层逐文件核对

| 旧文件 | 旧能力 | 新归属 | 状态 |
|---|---|---|---|
| `internal/browser/CLOAKBROWSER.md` | CloakBrowser 运行说明 | `README.md`、`docs/architecture.md`、`docs/browser-worker-design.md` | 已迁移并统一说明唯一正式链路 |
| `internal/browser/go_actions.go` | Go 组合浏览器动作和平台兼容动作 | Go 平台层、Worker `actions` | 明确不迁移 Go 实现；能力已放回正确层 |
| `internal/browser/go_cdp.go` | 手写 CDP 和 WebSocket 客户端 | TypeScript Worker + CloakBrowser 官方 SDK | 明确不迁移 |
| `internal/browser/go_controller.go` | Go 直连浏览器总控制器 | TypeScript Worker 会话、动作和原子能力 | 明确不迁移第二条浏览器链路 |
| `internal/browser/go_controller_test.go` | Go CDP 分类和控制器测试 | Go 协议测试、Worker 测试 | 公共替代 |
| `internal/browser/go_download.go` | Go 下载行为 | Worker 下载监听、`flow/download` | 公共替代 |
| `internal/browser/go_element.go` | Go 元素查找、引用和读取 | Worker `locator`、`read`、`element-registry` | 公共替代 |
| `internal/browser/go_helpers.go` | Go 动态参数和 JS 字符串辅助 | 强类型 Go/TS 协议和运行时校验 | 公共替代 |
| `internal/browser/go_input.go` | Go 点击、输入和按键 | Worker `ClickAction`、`InputAction`、`KeyboardPrimitive` | 公共替代 |
| `internal/browser/go_screenshot.go` | Go 页面和元素截图 | Worker 通用截图与真实滚轮长截图 | 公共替代 |
| `internal/browser/go_scroll.go` | Go 滚动和视口判断 | Worker `ScrollAction`、只读滚动状态 | 公共替代，禁止 JS 推动滚动 |
| `internal/browser/go_session.go` | Go 浏览器进程、页面和 Cookie 会话 | Worker `BrowserSession` | 公共替代 |
| `internal/browser/viewport.go` | 固定视口配置 | 强类型浏览器启动参数 | 已迁移可选视口和 User-Agent；不再强制固定尺寸 |
| `internal/browser/viewport_test.go` | 固定视口默认值测试 | `worker/test/browser-start-request.test.mjs` | 公共替代 |
| `internal/browser/worker.go` | Node Worker 调用、启动、重启和固定端口清理 | `internal/browser/client/`、`internal/browser/process/`、`internal/runtime/` | 已拆分迁移 |
| `internal/browser/worker_other.go` | 非 Windows Worker 进程结束 | `internal/browser/process/manager.go` | 已迁移，并在程序退出前先关闭浏览器 |
| `internal/browser/worker_test.go` | Worker 调用和状态测试 | Worker Node 测试、Go Client/Runtime 全量测试 | 公共替代 |
| `internal/browser/worker_windows.go` | Windows Worker 进程树和隐藏窗口 | 无 | 待 Windows 打包阶段 |

## 旧 Profile 层逐文件核对

| 旧文件 | 旧能力 | 新归属 | 状态 |
|---|---|---|---|
| `internal/browserprofile/defaults.go` | Profile 默认书签、必应搜索和 Chromium 保护校验 | `internal/profile/bookmarks.go`、`manager.go` | 已迁移五个默认书签、用户书签保留和书签栏显示；默认搜索引擎、受保护偏好和 Web Data 修改明确不迁移 |
| `internal/browserprofile/defaults_test.go` | 书签和搜索引擎文件测试 | `internal/profile/bookmarks_test.go`、`manager_test.go` | 已迁移书签顺序、保留和幂等测试；不保留已取消的搜索引擎测试 |
| `internal/browserprofile/command_other.go` | 非 Windows Profile 命令 | 无 | 随旧搜索引擎保护校验一起取消 |
| `internal/browserprofile/command_windows.go` | Windows 隐藏 Profile 初始化命令 | 无 | 随旧搜索引擎保护校验一起取消 |

## 旧云端、AI 与本地数据层逐文件核对

| 旧文件 | 旧能力 | 新归属 | 状态 |
|---|---|---|---|
| `internal/cloudapi/client.go` | 登录、会员、岗位、偏好、平台配置、统计、完成/失败通知 | `internal/integration/cloud/client.go`、`platform_config.go` | 已强类型迁移；完整候选人详情上传云端按新数据边界明确取消 |
| `internal/cloudapi/client_test.go` | 云端接口和完成通知测试 | `internal/integration/cloud/client_test.go` | 已迁移关键强类型、登录失效和邮件确认测试 |
| `internal/config/config.go` | 路径、端口、Worker、运行组件和控制台配置 | `internal/config/config.go` | 已迁移并以 macOS 用户配置目录为默认数据目录 |
| `internal/localai/client.go` | 文本/图片评分、SSE、重试、回复和结构化简历 | `internal/integration/ai/client.go` | 已迁移评分、图片、SSE、重试和回复；完整结构化简历入库/上传按新数据边界取消 |
| `internal/localai/client_test.go` | AI 解析、流式、重试和错误分类测试 | `internal/integration/ai/client_test.go` | 已迁移关键流式、重试和致命错误测试 |
| `internal/localdb/ai_types.go` | 本地 AI 配置模型 | 云端强类型 AI 配置 | 明确不迁移本地重复配置 |
| `internal/localdb/db.go` | 旧 SQLite 初始化和大表迁移 | `internal/storage/store.go`、`migrations/` | 已重建为最小本地摘要库 |
| `internal/localdb/positions.go` | 岗位、日志和完整候选人库 | `task_runs`、`task_logs`、`candidate_records` | 状态和日志已迁移；敏感完整候选人库明确不迁移 |
| `internal/localdb/positions_test.go` | 岗位、日志、设置和候选人数据库测试 | `internal/storage/*_test.go` | 已拆分迁移当前保留的数据规则 |
| `internal/localdb/records.go` | 本地设置、下载和截图记录 | `download_records`、云端配置、截图临时文件 | 下载已迁移；重复设置库和敏感截图索引明确取消 |

## 旧 OCR、系统和运行组件逐文件核对

| 旧文件 | 旧能力 | 新归属 | 状态 |
|---|---|---|---|
| `internal/ocr/engine.go` | RapidOCR 常驻进程、JSON 行协议、模型和日志 | `internal/integration/ocr/client.go` | 已迁移，支持嵌套结果、取消、统一关闭和递归查找安装路径 |
| `internal/ocr/command_other.go` | 非 Windows OCR 进程配置 | Go 标准 `exec.Cmd` | 公共替代 |
| `internal/ocr/command_windows.go` | Windows 隐藏 OCR 窗口 | 无 | 待 Windows 打包阶段 |
| `internal/power/inhibitor.go` | 防睡眠统一接口 | `internal/system/power/guard.go` | 已迁移 |
| `internal/power/inhibitor_darwin.go` | macOS `caffeinate` | `internal/system/power/guard.go` | 已迁移 |
| `internal/power/inhibitor_other.go` | 其他系统降级 | 无 | 当前只交付 macOS |
| `internal/power/inhibitor_windows.go` | Windows 电源 API | 无 | 待 Windows 打包阶段 |
| `internal/process/port.go` | 监听端口探测 | API 监听和诊断端口检查 | 公共替代；当前产品仍固定使用配置端口 |
| `internal/process/restart_other.go` | 非 Windows 旧实例空实现 | 无 | 明确不迁移空实现 |
| `internal/process/restart_windows.go` | Windows 旧实例和端口占用清理 | 无 | 待 Windows 打包阶段 |
| `internal/process/restart_windows_test.go` | Windows 端口和进程解析测试 | 无 | 待 Windows 打包阶段 |
| `internal/process/terminate_other.go` | 非 Windows 进程结束 | Worker 优雅停止和浏览器先行关闭 | 已迁移当前实际用途 |
| `internal/process/terminate_windows.go` | Windows 进程树结束 | 无 | 待 Windows 打包阶段 |
| `internal/response/json.go` | 动态统一 JSON 响应 | `internal/api/server.go` 的强类型响应 | 已迁移 |
| `internal/runtime/installer.go` | Node、CloakBrowser、OCR 下载、SHA256 和解压 | `internal/runtime/installer.go`、`archive.go` | 已迁移，增加失败回滚和安全链接检查 |
| `internal/runtime/installer_test.go` | SHA256、路径和组件版本测试 | `internal/runtime/installer_test.go` | 已迁移并补充 Worker 依赖路径测试 |
| `internal/runtime/manager.go` | 运行组件状态、Node/Worker/CloakBrowser/OCR 路径 | `internal/runtime/manager.go`、`types.go` | 已迁移；Worker 状态同时检查 CloakBrowser Node 依赖 |
| `internal/version/version.go` | 程序版本 | `internal/version/version.go` | 已迁移，当前默认版本为 `6` |

## 旧入口、文档、脚本与打包文件逐文件核对

| 旧文件 | 旧能力 | 新归属 | 状态 |
|---|---|---|---|
| `README.md` | 旧程序能力、运行和打包说明 | 新 `README.md` | 已重写 |
| `build_windows_installer.bat` | Windows 安装器入口 | 无 | 待 Windows 打包阶段 |
| `cmd/goodhr-local-agent/main.go` | 参数、文件日志、旧实例和服务启动 | 新同路径、`internal/bootstrap` | 已迁移 macOS 启动、健康实例复用、信号和文件日志；Windows 强制 `--restart` 待打包阶段 |
| `docs/REFACTOR_PLAN.md` | 第一版重构计划 | `docs/migration-plan.md`、`architecture.md` | 已替代 |
| `docs/开发.md` | 临时 Windows 开发命令 | `scripts/run-dev.sh` | 已替代 |
| `packaging/ChineseSimplified.isl` | Inno Setup 中文语言 | 无 | 待 Windows 打包阶段 |
| `packaging/GoodHRLocalAgentGo.iss` | Windows Inno Setup 定义 | 无 | 待 Windows 打包阶段 |
| `packaging/build_windows_installer.ps1` | Windows 安装器构建 | 无 | 待 Windows 打包阶段 |
| `scripts/build_go_binary.ps1` | Windows Go 构建 | 无 | 待 Windows 打包阶段 |
| `scripts/build_go_binary.sh` | Go 构建 | `scripts/build.sh` | 已迁移当前 macOS 构建 |
| `scripts/install_local_worker_dev.sh` | 开发安装旧 Worker 源码 | `scripts/run-dev.sh`、`npm run build` | 公共替代；新版直接运行同仓库编译产物 |
| `scripts/package_node_runtime.sh` | Node Runtime 打包 | `scripts/prepare-runtime.sh`、运行组件在线安装和 `scripts/package-release.sh` | 已由当前 macOS 流程替代；正式包包含 Go、Worker 编译产物和 Worker 生产依赖 |
| `scripts/package_worker.sh` | Worker 发布包 | `scripts/build.sh` | 已迁移为统一编译 |
| `scripts/windows_smoke_test.ps1` | Windows 接口冒烟 | 无 | 待 Windows 打包阶段 |

## 旧 Worker 包清单

| 旧文件 | 旧能力 | 新归属 | 状态 |
|---|---|---|---|
| `worker-node/package.json` | JavaScript Worker 依赖和命令 | `worker/package.json` | 已迁移为锁定版本的 TypeScript 严格模式项目 |
| `worker-node/package-lock.json` | Node 依赖锁 | `worker/package-lock.json` | 已迁移 |

## 当前不能伪装完成的配置

- 自动回复主流程和四个平台 `reply.go` 已实现，但旧版没有消息页地址、未读会话、上下文、输入框和发送按钮配置；除 Boss 消息页地址外均标记 `pending_selectors`。
- 收藏和不合适接口已实现，旧版没有四个平台的稳定选择器；模板中明确标记待云端配置。
- Boss、猎聘企业端旧版 `followup.go` 本身为空；新版保留配置驱动能力，不伪造按钮。
- 本地四份 `config.json` 已准备并带中文属性说明，现作为 `go:embed` 内置兜底；云端配置优先，内置模板只补齐缺失字段，标记待配置的选择器不会被伪造。
- 所有标记“待真实回归”的能力，本轮均未启动真实招聘账号任务。
