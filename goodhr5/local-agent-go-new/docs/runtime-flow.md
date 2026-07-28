<!-- 文件作用说明：定义本地程序启动、启动前检查、主动打招呼和自动回复的主流程。 -->

# 本地程序流程

## 1. 程序启动流程

```text
main
  1. 解析启动参数
  2. 加载本地配置
  3. 创建必要目录
  4. 打开 SQLite 并执行迁移
  5. 创建 Runtime Manager
  6. 创建 Browser Worker Manager 和 Client
  7. 创建 Cloud、AI、OCR Client
  8. 创建 Task Flow
  9. 注册本地 HTTP API
  10. 启动服务和控制台
  11. 监听退出信号并优雅清理
```

程序启动只保证本地服务可用，不应自动开始岗位任务。

## 2. 任务启动入口

所有任务统一进入：

```text
StartTask
  1. 解析强类型请求
  2. RunPreflightChecks
  3. 创建任务上下文和运行锁
  4. 保存本地 running 状态
  5. DispatchTaskFlow
  6. 返回已开始
```

后台流程必须持有可取消的 Context。

## 3. 启动前检查

`RunPreflightChecks` 使用顺序步骤列表：

```text
checks = [
  ValidateRequest,
  CheckCloudSession,
  CheckSubscription,
  LoadPositionSnapshot,
  LoadPlatformConfig,
  CheckProfile,
  CheckTaskConflict,
  CheckRuntimeComponents,
  CheckWorker,
  CheckCloakBrowser,
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
  7. 执行基础过滤
  8. 按配置读取候选人详情
  9. 执行关键词或 AI 判断
  10. 调用平台打招呼能力
  11. 执行索要电话、微信或简历等后续动作
  12. 保存候选人结果
  13. 同步统计
  14. 按配置滚动并继续下一批
  15. 完成、停止或失败收尾
```

主流程只决定顺序。Boss、智联、猎聘页面差异由平台 Runtime 实现。

## 6. 自动回复流程

```text
AutoReplyFlow
  1. 启动或复用浏览器
  2. 打开对应 Profile
  3. 获取平台 Runtime
  4. 打开消息页面
  5. 扫描未读会话
  6. 读取会话上下文
  7. 判断是否需要人工接管
  8. 调用 AI 生成回复
  9. 执行发送前安全检查
  10. 调用平台回复能力
  11. 保存会话和回复结果
  12. 继续下一条或按间隔等待
  13. 完成、停止或失败收尾
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
  5. 关闭当前详情或临时浮层
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
- 用户关闭浏览器与程序异常必须使用不同状态。
- 失败收尾不能覆盖用户主动停止状态。

## 10. 状态归属

- 真实运行状态归属 Go 本地程序。
- 网页只是控制台，刷新网页不能终止任务。
- Worker 只保存浏览器会话状态，不保存业务任务状态。
- 进程重启后根据 SQLite 恢复可展示状态；不能假装中断任务仍在运行。
