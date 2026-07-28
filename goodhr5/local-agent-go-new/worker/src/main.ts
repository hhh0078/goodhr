// 文件作用说明：读取 Worker 启动参数、启动本机 HTTP 服务并处理退出信号。

import { WorkerServer } from "./http/server.js";

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
      host,
      port,
    })}\n`,
  );
  const shutdown = async (): Promise<void> => {
    await server.stop();
    process.exitCode = 0;
  };
  process.once("SIGINT", () => void shutdown());
  process.once("SIGTERM", () => void shutdown());
}

/** parsePort 读取并约束 Worker 监听端口。 */
function parsePort(value: string | undefined): number {
  const port = Number.parseInt(value ?? "39881", 10);
  return Number.isInteger(port) && port > 0 && port <= 65_535
    ? port
    : 39_881;
}

await main();
