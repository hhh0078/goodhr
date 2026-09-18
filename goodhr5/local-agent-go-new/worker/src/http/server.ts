// 文件作用说明：创建只监听本机地址的 Browser Worker HTTP 服务并提供优雅关闭。

import http, { type Server } from "node:http";
import { randomUUID } from "node:crypto";
import { normalizeWorkerError } from "../errors/worker-error.js";
import { WorkerLogger } from "../logging/logger.js";
import { WorkerRouter } from "./router.js";

/** WorkerServer 管理 Worker HTTP 监听生命周期。 */
export class WorkerServer {
  private readonly server: Server;

  /** 创建 Worker HTTP 服务。 */
  constructor(
    private readonly host: string,
    private readonly port: number,
    router = new WorkerRouter(),
  ) {
    this.server = http.createServer((request, response) => {
      void router.handle(request, response).catch((error: unknown) => {
        const context = { trace_id: randomUUID(), action: "http", started_at: Date.now() };
        const normalized = normalizeWorkerError(error, { ...context, step: "request_boundary" });
        new WorkerLogger().failure(context, normalized);
        if (response.destroyed || response.writableEnded) return;
        if (response.headersSent) {
          response.destroy();
          return;
        }
        response.writeHead(500, { "Content-Type": "application/json; charset=utf-8" });
        response.end(JSON.stringify({ ok: false, error: normalized.toBody(), trace_id: context.trace_id }));
      });
    });
  }

  /** start 开始监听本机端口。 */
  async start(): Promise<void> {
    await new Promise<void>((resolve, reject) => {
      this.server.once("error", reject);
      this.server.listen(this.port, this.host, () => {
        this.server.off("error", reject);
        resolve();
      });
    });
  }

  /** stop 停止接收新请求并等待现有连接关闭。 */
  async stop(): Promise<void> {
    if (!this.server.listening) {
      return;
    }
    await new Promise<void>((resolve, reject) => {
      this.server.close((error) => (error ? reject(error) : resolve()));
    });
  }
}
