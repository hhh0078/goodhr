// 文件作用说明：读取 Worker 启动参数、启动本机 HTTP 服务并处理退出信号。

import { WorkerLogger } from "./logging/logger.js";
import { WorkerServer } from "./http/server.js";

const logger = new WorkerLogger();
let exitReason = "运行结束";
// 保留 Node 默认的异常退出行为，只补充脱敏诊断，不能吞掉异常继续工作。
process.on("uncaughtExceptionMonitor", (error, origin) => {
  exitReason = `${origin}: ${error.message}`;
  logger.error({ trace_id: "", action: "worker.crash", started_at: Date.now() }, origin, "failed", {
    category: "异常退出", pid: process.pid, error: error.message, stack: error.stack ?? "",
  });
});
process.on("unhandledRejection", (reason) => {
  const error = reason instanceof Error ? reason : new Error(String(reason));
  exitReason = `unhandledRejection: ${error.message}`;
  logger.error({ trace_id: "", action: "worker.crash", started_at: Date.now() }, "unhandled_rejection", "failed", {
    category: "异常退出", error: error.message, stack: error.stack ?? "",
  });
  throw error;
});
process.on("exit", (code) => {
  const context = { trace_id: "", action: "worker.exit", started_at: Date.now() };
  const fields = { pid: process.pid, exit_code: code, exit_reason: exitReason };
  if (code === 0) {
    logger.info(context, "exit", "finished", fields);
  } else {
    logger.error(context, "exit", "failed", { ...fields, category: "异常退出", error_message: exitReason });
  }
});

/** main 启动 Browser Worker 并注册优雅退出。 */
async function main(): Promise<void> {
  const host = "127.0.0.1";
  const port = parsePort(process.env.GOODHR_WORKER_PORT);
  const server = new WorkerServer(host, port);
  await server.start();
  process.stdout.write(
    `${JSON.stringify({
      timestamp: new Date().toISOString(),
      level: "info",
      action: "worker.start",
      status: "success",
      pid: process.pid,
      node_version: process.version,
      host,
      port,
    })}\n`,
  );
  /** shutdown 关闭 Worker HTTP 服务并设置正常退出状态。 */
  const shutdown = async (signal: string): Promise<void> => {
    exitReason = `收到 ${signal} 信号`;
    try {
      await server.stop();
      process.exitCode = 0;
    } catch (error) {
      exitReason = `关闭失败：${error instanceof Error ? error.message : String(error)}`;
      logger.error({ trace_id: "", action: "worker.shutdown", started_at: Date.now() }, "shutdown", "failed", {
        error: exitReason, stack: error instanceof Error ? error.stack ?? "" : "",
      });
      process.exitCode = 1;
    }
  };
  process.once("SIGINT", () => void shutdown("SIGINT"));
  process.once("SIGTERM", () => void shutdown("SIGTERM"));
}

/** parsePort 读取并约束 Worker 监听端口。 */
function parsePort(value: string | undefined): number {
  const port = Number.parseInt(value ?? "39881", 10);
  return Number.isInteger(port) && port > 0 && port <= 65_535
    ? port
    : 39_881;
}

await main();
