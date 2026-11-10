// 文件作用：用独立本地静态页面与真实 Camoufox 复现元素引用积累、HTTP 取消后读取叠加；不访问招聘账号、不注入脚本。
import assert from 'node:assert/strict';
import http from 'node:http';
import { firefox } from 'playwright-core';
import { ElementRegistry } from '../dist/browser/session/element-registry.js';
import { ActionService } from '../dist/browser/actions/action-service.js';
import { WorkerRouter } from '../dist/http/router.js';
import { actionContextStorage } from '../dist/logging/operation.js';

const binary = process.env.CAMOUFOX_BINARY_PATH;
const expectFixed = process.argv.includes("--assert-fixed");
if (!binary || !global.gc) throw new Error('请指定 CAMOUFOX_BINARY_PATH，并使用 node --expose-gc 运行');
const rows = Array.from({ length: 150 }, (_, index) => `<article class="card">Synthetic ${index}</article>`).join('');
const web = http.createServer((_request, response) => response.end(`<title>isolated reproduction</title>${rows}`));
await listen(web);
let browser;
try {
  browser = await firefox.launch({ executablePath: binary, headless: true });
  const page = await browser.newPage();
  await page.goto(`http://127.0.0.1:${web.address().port}`);
  await retention(page, browser);
  await cancellation(page, browser, "deadline");
  await cancellation(page, browser, "disconnect");
} finally {
  await browser?.close();
  await close(web);
}

/** listen 在随机本地端口监听，避免碰到用户正在运行的 Agent。 */
async function listen(server) { await new Promise(resolve => server.listen(0, '127.0.0.1', resolve)); }

/** close 只关闭本次测试创建的 HTTP 连接。 */
async function close(server) { server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); }

/** retention 保持十万个真实 Locator，真实等待超过 TTL 后测量引用数与垃圾回收后的内存。 */
async function retention(page, browser) {
  const registry = new ElementRegistry();
  global.gc();
  const baseline = process.memoryUsage().heapUsed;
  for (let round = 1; round <= 4; round++) {
    for (let index = 0; index < 25000; index++) registry.remember(page, page.locator('.card').nth(index % 150));
    global.gc();
    const readStarted = performance.now();
    await page.locator('.card').first().innerText();
    console.log(JSON.stringify({ read_ms: Math.round(performance.now() - readStarted), phase: 'retention', references: registry.entries.size,
      heap_growth_mb: Math.round((process.memoryUsage().heapUsed - baseline) / 1048576), connected: browser.isConnected() }));
  }
  await new Promise(resolve => setTimeout(resolve, 31000));
  global.gc();
  const retained = registry.entries.size;
  const heapGrowth = Math.round((process.memoryUsage().heapUsed - baseline) / 1048576);
  console.log(JSON.stringify({ phase: 'after_real_ttl', references: retained, heap_growth_mb: heapGrowth,
    expired: [...registry.entries.values()].filter(entry => Date.now() - entry.created_at > 30000).length }));
  if (expectFixed) { assert.equal(retained, 0); assert.ok(heapGrowth < 16, `过期后堆内存仍比基线高 ${heapGrowth} MiB`); }
  registry.clearPage(page);
  global.gc();
  console.log(JSON.stringify({ phase: 'after_clear', references: registry.entries.size,
    heap_growth_mb: Math.round((process.memoryUsage().heapUsed - baseline) / 1048576), readable: Boolean(await page.locator('.card').first().innerText()) }));
}

/** cancellation 通过正式 Worker HTTP 路由取消请求并再次读取，观测旧读取是否结束及总并发；30ms 延迟只加在测试替身中。 */
async function cancellation(page, browser, mode) {
  const actions = new ActionService();
  actions.session.requirePage = async () => page;
  let active = 0, peak = 0, afterAbort = 0, oldFinished = false;
  const inflight = new Set();
  const primitive = actions.findAction.readPrimitive;
  const read = primitive.text.bind(primitive);
  /** instrumentRead 在原始标准 Locator 读取前加入固定延迟，统计请求取消后的新增调用。 */
  primitive.text = async locator => {
    if (actionContextStorage.getStore()?.signal?.aborted) afterAbort++;
    active++; peak = Math.max(peak, active);
    try { await new Promise(resolve => setTimeout(resolve, 30)); return await read(locator); }
    finally { active--; }
  };
  const find = actions.findElements.bind(actions);
  /** trackRead 跟踪实际读取退出，防止把 HTTP 客户端退出误认为底层操作退出。 */
  actions.findElements = (request, context) => {
    const promise = find(request, context);
    inflight.add(promise);
    void promise.then(() => {}, () => {}).finally(() => {
      if (context.trace_id === 'cancelled-read') oldFinished = true;
      inflight.delete(promise);
    });
    return promise;
  };
  const router = new WorkerRouter(actions);
  const api = http.createServer((request, response) => void router.handle(request, response));
  await listen(api);
  try {
    const endpoint = `http://127.0.0.1:${api.address().port}/api/v1/element/find-all`;
    const body = JSON.stringify({ selector: { target: { selectors: [{ type: 'css', value: '.card' }] }, description: 'synthetic cards', timeout_ms: 1000 }, max_items: 100, read_concurrency: 100 });
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 150);
    let firstResponse;
    try { const response = await fetch(endpoint, { method: 'POST', body, signal: controller.signal,
      headers: { 'Content-Type': 'application/json', 'X-Trace-ID': 'cancelled-read', 'X-Worker-Timeout-Ms': mode === 'deadline' ? '100' : '40000' } }); firstResponse = await response.json(); }
    catch (error) { if (!controller.signal.aborted) throw error; }
    finally { clearTimeout(timer); }
    await new Promise(resolve => setTimeout(resolve, 150));
    console.log(JSON.stringify({ mode, phase: 'after_http_cancel', old_finished: oldFinished, active_reads: active, health: actions.healthStatus(), connected: browser.isConnected() }));
    if (expectFixed) {
      assert.equal(oldFinished, true); assert.equal(active, 0);
      if (mode === 'deadline') assert.equal(firstResponse?.error?.code, 'WORKER_UNRESPONSIVE');
      else assert.equal(controller.signal.aborted, true);
    }
    const response = await fetch(endpoint, { method: 'POST', body, headers: { 'Content-Type': 'application/json', 'X-Trace-ID': 'new-read' } });
    assert.equal(response.status, 200);
    await response.json();
    await Promise.allSettled([...inflight]);
    console.log(JSON.stringify({ mode, phase: 'after_second_read', old_finished: oldFinished, peak_reads: peak, reads_started_after_cancel: afterAbort, connected: browser.isConnected() }));
    if (expectFixed) { assert.equal(afterAbort, 0); assert.ok(peak <= 4); assert.equal(active, 0); }
  } finally {
    await Promise.allSettled([...inflight]);
    await close(api);
  }
}
