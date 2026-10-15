/** 本文件验证实际登录轮询的空页恢复、外站提示、连续确认和暂停边界，全程隔离本地程序及真实招聘网站。 */
import assert from "node:assert/strict";
import test from "node:test";
import { readFile } from "node:fs/promises";
import ts from "typescript";

const source = await readFile(new URL("./platform-login.ts", import.meta.url), "utf8");
const compiled = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } }).outputText.replace(
  'import { currentLocalPageURL, openLocalPage } from "./admin-api";',
  "const currentLocalPageURL = (...args) => globalThis.__platformLoginTestAgent.readURL(...args); const openLocalPage = () => { throw new Error('测试不得打开真实浏览器'); };",
);
assert.ok(compiled.includes("__platformLoginTestAgent"), "登录测试必须替换网络依赖，不能请求真实程序");
let moduleSequence = 0;
const auth = {
  pages: [{ url: "https://recruiting.example.invalid/recommend", match: "prefix", entry: true }],
  public_pages: [{ url: "https://recruiting.example.invalid/login", match: "prefix", code: "login" }],
};

/** withLoginAgent 仅加速等待时钟并返回给定页面序列，执行生产轮询且不修改其成功判断。 */
async function withLoginAgent(urls, run, failure) {
  const original = { window: globalThis.window, agent: globalThis.__platformLoginTestAgent };
  const calls = [];
  globalThis.window = { setTimeout: (callback) => setTimeout(callback, 0), clearTimeout };
  globalThis.__platformLoginTestAgent = {
    /** readURL 模拟页面读取，记录轮询次数以核对连续登录确认。 */
    async readURL(base) {
      calls.push(base);
      if (failure) throw failure;
      return urls[Math.min(calls.length - 1, urls.length - 1)] || "";
    },
  };
  try {
    const api = await import(`data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}#${++moduleSequence}`);
    await run(api, calls);
  } finally {
    if (original.window === undefined) delete globalThis.window; else globalThis.window = original.window;
    if (original.agent === undefined) delete globalThis.__platformLoginTestAgent; else globalThis.__platformLoginTestAgent = original.agent;
  }
}

test("空页和其他网站明确提示重开本次登录窗口，平台内跳转也不能冒充登录", async () => {
  const entry = auth.pages[0].url;
  await withLoginAgent(["", "about:blank", "https://www.bing.com/", "https://recruiting.example.invalid/verify", auth.public_pages[0].url, entry, entry, entry], async (api, calls) => {
    const statuses = [];
    await api.waitForPlatformLoggedIn("http://isolated-agent.invalid", auth, (message) => statuses.push(message));
    assert.equal(calls.length, 8);
    assert.match(statuses[0], /没看到窗口.*重新打开本次招聘登录窗口/);
    assert.equal(statuses.filter((message) => message.includes("暂未找到所选招聘平台页面")).length, 3);
    assert.ok(statuses.includes("招聘平台页面正在跳转；请在招聘浏览器中完成登录，我会继续确认。"));
    assert.ok(statuses.includes("请在招聘浏览器中完成扫码或验证码登录"));
    assert.ok(statuses.every((message) => !message.includes("顶部")), "公共账号登录检测不能引导改用其他Profile的顶部浏览器");
    assert.equal(statuses.at(-1), "正在确认登录状态 3/3");
  });
});

test("短暂命中后退回登录页必须重新连续确认三次", async () => {
  const entry = auth.pages[0].url;
  await withLoginAgent([entry, entry, auth.public_pages[0].url, entry, entry, entry], async (api, calls) => {
    const statuses = [];
    await api.waitForPlatformLoggedIn("http://isolated-agent.invalid", auth, (message) => statuses.push(message));
    assert.equal(calls.length, 6);
    assert.equal(statuses.filter((message) => message === "正在确认登录状态 1/3").length, 2);
    assert.equal(statuses.at(-1), "正在确认登录状态 3/3");
  });
});

test("一直停在外站最终给恢复说明，绝不因等待耗尽算作已登录", async () => {
  await withLoginAgent(["https://www.bing.com/"], async (api, calls) => {
    const statuses = [];
    await assert.rejects(api.waitForPlatformLoggedIn("http://isolated-agent.invalid", auth, (message) => statuses.push(message)), /还没确认到平台登录.*重新打开本次招聘登录窗口/);
    assert.equal(calls.length, 180);
    assert.ok(statuses.slice(1).every((message) => message.includes("暂未找到所选招聘平台页面")));
    assert.ok(statuses.every((message) => !message.includes("3/3")));
  });
});

test("页面读取失败立即返回错误，不继续堆叠登录检测", async () => {
  await withLoginAgent([], async (api, calls) => {
    await assert.rejects(api.waitForPlatformLoggedIn("http://isolated-agent.invalid", auth, () => {}), /浏览器窗口已关闭/);
    assert.equal(calls.length, 1);
  }, new Error("浏览器窗口已关闭"));
});

test("登录确认过程中暂停后不能继续推进旧请求", async () => {
  await withLoginAgent([auth.pages[0].url], async (api, calls) => {
    const controller = new AbortController();
    await assert.rejects(api.waitForPlatformLoggedIn("http://isolated-agent.invalid", auth, (message) => {
      if (message === "正在确认登录状态 1/3") controller.abort();
    }, controller.signal), { name: "AbortError" });
    assert.equal(calls.length, 1);
  });
});
