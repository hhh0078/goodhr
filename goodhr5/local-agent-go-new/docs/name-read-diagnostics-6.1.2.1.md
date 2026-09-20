<!-- 文件作用说明：说明姓名读取诊断日志的字段、关联方式和隐私边界。 -->
# 6.1.2.1 姓名读取诊断

本版只补充诊断，保持两次读取、缺少姓名时停止、不点击候选人的规则。没有增加页面脚本注入，也没有调整并发和等待参数。

Windows 日志在安装目录 `data/logs` 下，自定义 `--data-dir` 或 `GOODHR_DATA_DIR` 时跟随自定义目录。

- `agent.log` 的 `event=candidate_name_read`：同一个 `read_id` 下的 `attempt=1/2`，记录返回数量、缺失数量、从零开始的卡片序号和单轮耗时。
- `agent.log` 的 `event=candidate_name_page`：两轮均失败后只读探测页码，`page_known=false` 表示无法确认；这是失败后观察到的页码，不代表读取过程中页面一直没变化。
- `worker.log` 的 `step=list_name_diagnostics`：记录实际匹配规则、匹配数、丢弃序号和返回结果中姓名缺失的序号；通过 `trace_id/request_id` 关联该次浏览器请求。
- `step=read_field:name`：`phase=locate/read` 区分定位和文本读取，记录字段匹配数、可见性、字段耗时、原始错误类别以及配置等待时间和实际等待时间。`match_attempts` 保存定位失败前各层数量。
- `visibility_error_type` 非空：可见性检测本身出错，不能直接认定节点实际隐藏。
- `step=read_list_item,status=read_failed`：卡片正文读取异常，带有 `phase=before/after`。`status=changed` 才表示两次都读到文本且文本不同。

不记录姓名值、简历正文、候选人编号、完整 HTML、截图或原始异常消息/调用栈；Playwright 异常消息可能包含这些资料，因此只保留错误码、类别和安全的定位信息。成功读取也只记录文本长度。日志可判断读取失败阶段，不能凭耗时独自判定网络或电脑性能是根因。

安装器按用户指定版本生成独立下载地址；不修改服务端数据库中的下载版本配置。
