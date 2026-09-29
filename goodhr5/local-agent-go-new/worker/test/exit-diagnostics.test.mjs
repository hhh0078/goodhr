// 文件作用说明：验证跨请求操作轨迹、崩溃同步输出、敏感字段过滤和真实启动失败的原始调用栈。

import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createServer } from "node:net";
import test from "node:test";
import { WorkerLogger } from "../dist/logging/logger.js";

test("退出日志包含其他日志器记录的最后操作和原始错误，并限制上传大小", () => {
  const events = [];
  const writer = (line, critical) => events.push({ payload: JSON.parse(line), critical, line });
  const first = new WorkerLogger(writer);
  first.info({ action: "element.click", trace_id: "original-trace", request_id: "request-1", started_at: Date.now() }, "mouse.down", "start", { target_description: "候选人详情" });
  first.error({ action: "element.click", trace_id: "original-trace", started_at: Date.now() }, "mouse.down", "failed", {
    error: "APZ timeout token=private-secret", stack: "Error: APZ timeout\n at Mouse.down", cookie: "private-cookie",
  });
  new WorkerLogger(writer).error({ action: "worker.exit", trace_id: "", started_at: Date.now() }, "exit", "failed", {
    category: "异常退出", exit_code: 1, exit_reason: "未捕获异常",
  });
  const exit = events.at(-1);
  assert.equal(exit.critical, true);
  assert.equal(exit.payload.diagnostics.node_version, process.version);
  assert.ok(exit.payload.diagnostics.recent_operations.some(item => item.trace_id === "original-trace"));
  assert.match(exit.payload.diagnostics.last_error.stack, /Mouse.down/);
  assert.doesNotMatch(exit.line, /private-secret|private-cookie/);
  assert.ok(Buffer.byteLength(exit.line) < 16384);
});

test("真实 Worker 启动端口被占用时，退出前同步留下原始错误、调用栈和退出码", async () => {
  const server = createServer();
  await new Promise((resolve, reject) => { server.once("error", reject); server.listen(0, "127.0.0.1", resolve); });
  try {
    const child = spawnSync(process.execPath, ["dist/main.js"], {
      cwd: new URL("..", import.meta.url), env: { ...process.env, GOODHR_WORKER_PORT: String(server.address().port) },
      encoding: "utf8", timeout: 10000,
    });
    assert.equal(child.status, 1, child.stderr);
    const events = child.stdout.trim().split("\n").map(line => JSON.parse(line));
    const crash = events.find(item => item.action === "worker.crash");
    const exit = events.find(item => item.action === "worker.exit");
    assert.equal(crash.category, "异常退出");
    assert.match(crash.stack, /EADDRINUSE/);
    assert.equal(exit.exit_code, 1);
    assert.match(exit.diagnostics.last_error.stack, /EADDRINUSE/);
    for (const item of events) assert.ok(Buffer.byteLength(JSON.stringify(item)) < 16384);
  } finally {
    await new Promise(resolve => server.close(resolve));
  }
});
