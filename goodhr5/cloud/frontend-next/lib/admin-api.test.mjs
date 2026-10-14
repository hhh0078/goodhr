/** 文件作用说明：执行实际本地Agent探测代码，验证测试构建固定端口及缓存隔离，全程替换fetch避免请求真实程序。 */
import assert from "node:assert/strict";
import test from "node:test";
import { readFile } from "node:fs/promises";
import ts from "typescript";

const source = await readFile(new URL("./admin-api.ts", import.meta.url), "utf8");
const compiled = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } }).outputText.replace('from "./api"', `from ${JSON.stringify(new URL("./api.ts", import.meta.url).href)}`);
let moduleSequence = 0;
const detectCacheKey = "goodhr5_local_agent_detect_cache";
const portCacheKey = "goodhr5_local_agent_port";

/** withFakeAgent 隔离每次模块配置和浏览器缓存，记录实际探测地址，fetch永远不会接触网络。 */
async function withFakeAgent(port, options, run) {
  const original = { port: process.env.NEXT_PUBLIC_LOCAL_AGENT_PORT, window: globalThis.window, storage: globalThis.localStorage, fetch: globalThis.fetch };
  if (port === undefined) delete process.env.NEXT_PUBLIC_LOCAL_AGENT_PORT; else process.env.NEXT_PUBLIC_LOCAL_AGENT_PORT = port;
  const values = new Map(Object.entries(options.storage || {}));
  const calls = [];
  globalThis.localStorage = { getItem: (key) => values.get(key) ?? null, setItem: (key, value) => values.set(key, value), removeItem: (key) => values.delete(key) };
  globalThis.window = { setTimeout, clearTimeout, location: { search: "" }, ...(options.memory || options.pending ? { __goodhrLocalAgentDetectState: { detecting: options.pending || null, cache: options.memory || { baseURL: "", checkedAt: 0 } } } : {}) };
  globalThis.fetch = async (url) => { calls.push(String(url)); if ((options.healthyPorts || []).includes(Number(new URL(url).port))) return { ok: true }; throw new Error("fake offline"); };
  try {
    const api = await import(`data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}#${++moduleSequence}`);
    await run(api, calls, values);
  } finally {
    if (original.port === undefined) delete process.env.NEXT_PUBLIC_LOCAL_AGENT_PORT; else process.env.NEXT_PUBLIC_LOCAL_AGENT_PORT = original.port;
    if (original.window === undefined) delete globalThis.window; else globalThis.window = original.window;
    if (original.storage === undefined) delete globalThis.localStorage; else globalThis.localStorage = original.storage;
    globalThis.fetch = original.fetch;
  }
}

test("固定测试端口离线时忽略preferred和正式缓存，绝不回退43129", async () => {
  for (const cacheKind of ["storage", "memory"]) {
    const cached = { baseURL: "http://127.0.0.1:43129", checkedAt: Date.now() };
    await withFakeAgent("43139", { healthyPorts: [43129], storage: { [portCacheKey]: "43129", ...(cacheKind === "storage" ? { [detectCacheKey]: JSON.stringify(cached) } : {}) }, ...(cacheKind === "memory" ? { memory: cached } : {}) }, async (api, calls) => {
      assert.equal(await api.detectLocalAgent("http://127.0.0.1:43129"), "");
      assert.deepEqual(calls, ["http://127.0.0.1:43139/health"]);
      assert.equal(await api.detectLocalAgent("http://127.0.0.1:43129"), "");
      assert.equal(calls.length, 1);
      assert.equal(api.captureLocalAgentPortFromURL("?local_port=43129"), 0);
    });
  }
});

test("固定端口只复用同端口缓存，preferred不同也不会探测正式Agent", async () => {
  const cached = { baseURL: "http://127.0.0.1:43139", checkedAt: Date.now() };
  await withFakeAgent("43139", { storage: { [detectCacheKey]: JSON.stringify(cached), [portCacheKey]: "43129" } }, async (api, calls) => {
    assert.equal(await api.detectLocalAgent("http://127.0.0.1:43129"), cached.baseURL);
    assert.deepEqual(calls, []);
    assert.equal(api.captureLocalAgentPortFromURL("?local_port=43139"), 43139);
  });
  await withFakeAgent("43139", { healthyPorts: [43129, 43139], storage: { [portCacheKey]: "43129" } }, async (api, calls) => {
    assert.equal(await api.detectLocalAgent("http://127.0.0.1:43129"), cached.baseURL);
    assert.deepEqual(calls, ["http://127.0.0.1:43139/health"]);
  });
  await withFakeAgent("43139", { healthyPorts: [43139], pending: Promise.resolve("http://127.0.0.1:43129") }, async (api, calls) => {
    assert.equal(await api.detectLocalAgent(), cached.baseURL);
    assert.deepEqual(calls, ["http://127.0.0.1:43139/health"]);
  });
});

test("非空无效固定端口禁止探测或复用正式缓存", async () => {
  for (const port of ["0", "65536", "43139.5", "http://127.0.0.1:43139"]) {
    await withFakeAgent(port, { healthyPorts: [43129], memory: { baseURL: "http://127.0.0.1:43129", checkedAt: Date.now() } }, async (api, calls) => {
      assert.equal(await api.detectLocalAgent("http://127.0.0.1:43129"), "");
      assert.deepEqual(calls, []);
    });
  }
});

test("未配置固定端口时保留原默认、preferred和端口缓存顺序", async () => {
  await withFakeAgent(undefined, { healthyPorts: [43129] }, async (api, calls) => {
    assert.equal(await api.detectLocalAgent(), "http://127.0.0.1:43129");
    assert.deepEqual(calls, ["http://127.0.0.1:43129/health"]);
  });
  await withFakeAgent("", { healthyPorts: [43129], storage: { [portCacheKey]: "43140" } }, async (api, calls) => {
    assert.equal(await api.detectLocalAgent("http://127.0.0.1:43139"), "http://127.0.0.1:43129");
    assert.deepEqual(calls, ["http://127.0.0.1:43139/health", "http://127.0.0.1:43140/health", "http://127.0.0.1:43129/health"]);
  });
  const cached = { baseURL: "http://127.0.0.1:43140", checkedAt: Date.now() };
  await withFakeAgent(undefined, { storage: { [detectCacheKey]: JSON.stringify(cached) } }, async (api, calls) => {
    assert.equal(await api.detectLocalAgent(), cached.baseURL);
    assert.deepEqual(calls, []);
  });
});

/** 验证切换账号后才返回的旧请求不会把新账号退出，教程等待期间也保持身份隔离。 */
test("旧账号延迟返回401不会清掉新账号Token或跳转登录页", async () => {
  await withFakeAgent("43139", { storage: { goodhr5_access_token: "old-local-test-token" } }, async (api, _calls, values) => {
    let respond;
    const redirected = [];
    Object.assign(globalThis.window.location, { pathname: "/admin", replace: (url) => redirected.push(url) });
    globalThis.fetch = () => new Promise((resolve) => { respond = resolve; });
    const request = api.cloudRequest("/api/user-flow");
    values.set("goodhr5_access_token", "new-local-test-token");
    respond({ ok: false, status: 401, text: async () => JSON.stringify({ error: "session is invalid or expired" }) });
    await assert.rejects(request, (error) => error.status === 401);
    assert.equal(values.get("goodhr5_access_token"), "new-local-test-token");
    assert.deepEqual(redirected, []);
  });
});

/** 验证当前账号真正失效时仍清理凭证并引导重新登录，不能因修复竞态丢掉原行为。 */
test("当前账号返回401仍清理自身Token并保留原来的返回地址", async () => {
  await withFakeAgent("43139", { storage: { goodhr5_access_token: "current-local-test-token" } }, async (api, _calls, values) => {
    const redirected = [];
    Object.assign(globalThis.window.location, { pathname: "/admin/positions", search: "?local_port=43139", replace: (url) => redirected.push(url) });
    globalThis.fetch = async () => ({ ok: false, status: 401, text: async () => JSON.stringify({ error: "session is invalid or expired" }) });
    await assert.rejects(api.cloudRequest("/api/user-flow"), (error) => error.status === 401);
    assert.equal(values.has("goodhr5_access_token"), false);
    assert.deepEqual(redirected, ["/login?next=%2Fadmin%2Fpositions%3Flocal_port%3D43139"]);
  });
});
