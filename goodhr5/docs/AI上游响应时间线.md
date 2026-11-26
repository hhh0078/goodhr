<!-- 文件作用：说明 AI 响应收尾和阶段日志，便于排查长时间等待。 -->

每次上游尝试的 `[AI请求记录]` 和 `X-GoodHR-AI-Attempt-Details` 新增 timeline。所有时刻为 UTC RFC3339 纳秒格式，北京时间加八小时。

| 字段 | 含义 |
|---|---|
| request_written_at | 请求写完 |
| first_response_at / headers_at | 首响应字节、完整响应头到达 |
| first_data_at / last_data_at | 实际读取首批、末批正文 |
| finish_reason_at | 首次解析到结束原因，继续读取 usage 和余下正文 |
| done_at / eof_at | SSE 完成标志、实际正文 EOF，未经过的阶段留空 |
| error_at | 观察到读写错误、SSE 错误或最终校验失败 |
| end_at / end_reason | 本次尝试最终结束时刻与分类 |
| received_bytes | 实际读取字节数，含缓冲预读，可能多于接纳的正文 |

只对聊天接口声明的 SSE 在 `[DONE]` 收尾。普通 JSON 与没有 DONE 的旧接口继续按 EOF 处理。DONE 不跳过完整 JSON 和工具参数校验，局部评分或 finish_reason 不会提前结束；结束前的完整简历与 usage 保留，结束后的错误不覆盖已完成结果。HTTP200 的错误对象、具体错误、错误编号、截断标记和重试原因沿用既有处理。

首响应到首正文、末正文到结束的间隔可区分等待响应、等待数据和流收尾。时间代表后端观察时刻，不代表模型内部生成时刻，不保存简历正文、图片或认证。

完整时间线保存在后端容器日志。旧桌面版不会自动展示新增 timeline；使用 client_request_id / analysis_group_id 与岗位日志关联，此次无需桌面更新。

验证：`go test ./internal/httpapi` 和 `go test -race ./internal/httpapi -run 'TestAIResponse|TestAITimeline'`，全部使用本地合成上游。
