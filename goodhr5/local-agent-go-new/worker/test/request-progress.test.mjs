// 文件作用：用真实 HTTP 路由和列表执行流程验证请求编号、并发槽和超时现场，页面读取使用可控制的替身。
import test from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import { randomUUID } from "node:crypto";
import { ActionService } from "../dist/browser/actions/action-service.js";
import { WorkerRouter } from "../dist/http/router.js";

/** deferred 提供可控的浏览器读取结束点。 */
function deferred() { let resolve; const promise = new Promise(done => { resolve = done; }); return { promise, resolve }; }

for (const timedOut of [false, true]) {
  test(timedOut ? "超时冻结仍待返回的字段，后续排空不能覆盖现场" : "同批其他字段成功后移除其槽，全部成功时没有在途读取", { timeout: 5000 }, async t => {
    const actions = new ActionService(), entered = deferred(), release = deferred();
    const page = { isClosed: () => false, frames: () => [], context: () => ({ browser: () => ({ isConnected: () => true, version: () => "135.0.3" }) }) };
    const row = {}, blocked = {}, free = {};
    const action = actions.findAction;
    action.session.requirePage = async () => page;
    action.session.elements.remember = () => "fake-ref";
    action.primitive.resolveAll = async () => [{ list_index: 42, locator: row, page }];
    action.primitive.resolveRelative = async (_p, _row, spec) => ({
      locator: spec.description === "name" ? blocked : free, view: { visible: true }, attempts: [],
    });
    action.readPrimitive.text = async locator => {
      if (locator === blocked) { entered.resolve(); await release.promise; }
      return "candidate-private-value";
    };
    const router = new WorkerRouter(actions);
    const server = http.createServer((req, res) => void router.handle(req, res));
    await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
    t.after(async () => { release.resolve(); server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); });
    const base = `http://127.0.0.1:${server.address().port}`, id = randomUUID();
    const selector = { target: { selectors: [{ type: "css", value: ".row" }] }, description: "row" };
    const response = fetch(base + "/api/v1/element/find-all", { method: "POST", headers: {
      "content-type": "application/json", "x-request-id": id, "x-worker-timeout-ms": timedOut ? "400" : "2500" },
      body: JSON.stringify({ selector, expected_missing: true, fields: { name: { ...selector, description: "name" }, private_field: selector } }),
    }).then(res => res.json());
    /** inspect 通过现有健康入口读取指定请求的内存快照。 */
    const inspect = async requestID => (await (await fetch(base + "/health", { headers: { "x-inspect-request-id": requestID } })).json()).data.request_progress;
    await entered.promise;
    assert.deepEqual(await inspect(randomUUID()), {});
    const running = await inspect(id);
    assert.equal(running.request_id, id);
    assert.equal(running.pending_reads.length, 1);
    assert.equal(running.pending_reads[0].item_index, 42);
    assert.equal(running.pending_reads[0].worker_phase, "read_field");
    assert.equal(running.pending_reads[0].field_key, "name");
    assert.equal(running.browser_connected, true);
    assert.equal(running.browser_version, "135.0.3");
    assert.equal(running.node_version, process.version);
    assert.doesNotMatch(JSON.stringify(running), /candidate-private-value|private_field/);
    if (!timedOut) release.resolve();
    const result = await response;
    if (timedOut) {
      assert.equal(result.error.code, "WORKER_UNRESPONSIVE");
      const snapshot = result.error.details.request_progress.pending_reads;
      assert.equal(snapshot[0].elapsed_ms > 0, true);
      release.resolve();
      await new Promise(resolve => setImmediate(resolve));
      assert.deepEqual((await inspect(id)).pending_reads, snapshot);
    } else {
      assert.equal(result.ok, true);
      assert.deepEqual((await inspect(id)).pending_reads, []);
    }
  });
}
