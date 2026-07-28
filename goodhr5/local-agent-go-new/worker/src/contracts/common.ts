// 文件作用说明：定义 Browser Worker 全部接口共享的 JSON、请求上下文和响应类型。

/** JsonPrimitive 表示可以直接出现在 JSON 中的基础值。 */
export type JsonPrimitive = string | number | boolean | null;

/** JsonValue 表示 Worker 协议允许传输的 JSON 值。 */
export type JsonValue = JsonPrimitive | JsonObject | JsonValue[];

/** JsonObject 表示 Worker 协议允许传输的 JSON 对象。 */
export interface JsonObject {
  [key: string]: JsonValue;
}

/** ActionContext 保存一次 Worker 调用的追踪信息。 */
export interface ActionContext {
  trace_id: string;
  action: string;
  started_at: number;
}

/** SuccessResponse 表示 Worker 接口成功响应。 */
export interface SuccessResponse<T extends JsonValue> {
  ok: true;
  data: T;
  trace_id: string;
}

/** ErrorBody 表示可安全返回给 Go 的错误内容。 */
export interface ErrorBody extends JsonObject {
  code: string;
  message: string;
  action: string;
  step: string;
  trace_id: string;
  retryable: boolean;
  details: JsonObject;
}

/** ErrorResponse 表示 Worker 接口失败响应。 */
export interface ErrorResponse {
  ok: false;
  error: ErrorBody;
  trace_id: string;
}

/** WorkerResponse 表示 Worker 接口统一响应。 */
export type WorkerResponse<T extends JsonValue> =
  | SuccessResponse<T>
  | ErrorResponse;
