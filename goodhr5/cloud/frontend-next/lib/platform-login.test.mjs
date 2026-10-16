/** 本文件验证新旧平台登录配置解析、登录页排除、实际轮询恢复和暂停边界，全程隔离本地程序及真实招聘网站。 */
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
const legacyPlatforms = [
  { id: "zhaopin", entry: "https://rd6.zhaopin.com/app/recommend", domain: "https://rd6.zhaopin.com", login: "https://rd6.zhaopin.com/login", passport: "https://passport.zhaopin.com", extra: "https://www.zhaopin.com" },
  { id: "hliepin", entry: "https://h.liepin.com/search/getConditionItem", domain: "https://h.liepin.com", login: "https://h.liepin.com/login", passport: "https://passport.liepin.com" },
  { id: "liepin", entry: "https://lpt.liepin.com/recommend", domain: "https://lpt.liepin.com", login: "https://lpt.liepin.com/login", passport: "https://passport.liepin.com" },
];

/** legacyPlatformConfig 复现现有三种旧平台配置的认证字段，仅作为纯数据，不访问其招聘网站。 */
function legacyPlatformConfig(platform) {
  return {
    id: platform.id,
    auth: {
      pages: [
        { url: platform.entry, match: "contains", entry: true },
        { url: platform.domain, match: "contains" },
        ...(platform.extra ? [{ url: platform.extra, match: "contains" }] : []),
      ],
      entry_url: platform.entry,
      login_url_prefixes: [platform.passport, platform.login],
      logged_in_url_contains: [new URL(platform.domain).hostname],
    },
  };
}

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

for (const platform of legacyPlatforms) {
  test(`${platform.id}旧认证配置沿用原入口，只用明确登录前缀补公开规则`, async () => {
    await withLoginAgent([], async (api, calls) => {
      const fixture = legacyPlatformConfig(platform);
      const parsed = api.parsePlatformAuthConfig(platform.id === "zhaopin" ? JSON.stringify(fixture) : fixture, platform.id);
      assert.deepEqual(parsed.pages, fixture.auth.pages, "不能把旧入口或域名字段扩张成新的登录成功规则");
      assert.equal(api.pickAuthEntryURL(parsed), platform.entry);
      assert.equal(api.pickLoginEntryURL(parsed), platform.passport);
      assert.deepEqual(parsed.public_pages, [platform.passport, platform.login].map((url) => ({ url, match: "prefix", code: "login" })));
      assert.equal(api.isLoggedInURL(platform.entry, parsed), true);
      for (const prefix of [platform.passport, platform.login]) {
        assert.equal(api.isLoginURL(`${prefix}/verify`, parsed), true);
        assert.equal(api.isLoggedInURL(`${prefix}/verify`, parsed), false, "登录地址不能被原auth泛域名规则认成成功");
      }
      assert.equal(api.isLoggedInURL("https://unrelated.example.invalid/", parsed), false);
      assert.equal(calls.length, 0, "配置解析不能请求浏览器或招聘页面");
    });
  });
}

test("现代公开登录规则继续优先，明确登录页即使命中泛域名也不算成功", async () => {
  await withLoginAgent([], async (api) => {
    const configured = { auth: { pages: [{ url: "https://recruiting.example.invalid", match: "contains", entry: true }], login_url_prefixes: ["https://legacy.example.invalid/login"] }, public: { pages: [{ url: "https://recruiting.example.invalid/login", match: "prefix", code: "login" }] } };
    const parsed = api.parsePlatformAuthConfig(configured, "modern");
    assert.deepEqual(parsed.public_pages, configured.public.pages);
    assert.equal(api.pickLoginEntryURL(parsed), configured.public.pages[0].url);
    assert.equal(api.isLoggedInURL("https://recruiting.example.invalid/login?from=recommend", parsed), false);
    assert.equal(api.isLoggedInURL("https://recruiting.example.invalid/recommend", parsed), true);
  });
});

test("标准public.pages省略可选code时继续可用，保留首条公开页面作为登录入口", async () => {
  await withLoginAgent([], async (api) => {
    const configured = { auth: { pages: [{ url: "https://recruiting.example.invalid", match: "prefix", entry: true }] }, public: { pages: [{ url: "https://recruiting.example.invalid/login", match: "prefix" }] } };
    const parsed = api.parsePlatformAuthConfig(configured, "modern");
    assert.deepEqual(parsed.public_pages, configured.public.pages);
    assert.equal(api.pickLoginEntryURL(parsed), "https://recruiting.example.invalid/login");
    assert.equal(api.isLoginURL("https://recruiting.example.invalid/login/verify", parsed), true);
    assert.equal(api.isLoggedInURL("https://recruiting.example.invalid/login/verify", parsed), false);
    assert.equal(api.isLoggedInURL("https://recruiting.example.invalid/recommend", parsed), true);
  });
});

test("缺失或畸形登录规则继续报错，不能依靠entry_url或logged_in_url_contains猜登录", async () => {
  await withLoginAgent([], async (api) => {
    for (const missing of [undefined, [], ["", " ", null, {}, "javascript:alert(1)", "rd6.zhaopin.com/login"]]) {
      const fixture = legacyPlatformConfig(legacyPlatforms[0]);
      fixture.auth.login_url_prefixes = missing;
      assert.throws(() => api.parsePlatformAuthConfig(fixture, "zhaopin"), /缺少 public\.pages 登录规则或 auth\.login_url_prefixes/);
    }
    assert.throws(() => api.parsePlatformAuthConfig({ auth: { pages: auth.pages }, public: { pages: [] } }, "missing"), /缺少 public\.pages/);
    assert.throws(() => api.parsePlatformAuthConfig({ auth: { pages: auth.pages }, public: { pages: [{ url: "", code: "login" }, null, {}] } }, "missing"), /缺少 public\.pages/);
    assert.throws(() => api.parsePlatformAuthConfig({ auth: { entry_url: legacyPlatforms[0].entry, login_url_prefixes: [legacyPlatforms[0].login], logged_in_url_contains: ["zhaopin.com"] } }, "missing"), /缺少 auth\.pages/);
  });
});

test("旧配置登录页连续三次也不通过，离开登录页后重新确认三次才继续", async () => {
  const platform = legacyPlatforms[0];
  await withLoginAgent([platform.login, platform.login, platform.login, platform.entry, platform.entry, platform.entry], async (api, calls) => {
    const statuses = [];
    const parsed = api.parsePlatformAuthConfig(legacyPlatformConfig(platform), platform.id);
    await api.waitForPlatformLoggedIn("http://isolated-agent.invalid", parsed, (message) => statuses.push(message));
    assert.equal(calls.length, 6);
    assert.equal(statuses.filter((message) => message === "请在招聘浏览器中完成扫码或验证码登录").length, 3);
    assert.equal(statuses.at(-1), "正在确认登录状态 3/3");
  });
});

test("启动前快速检查也不能把旧配置的同域登录页认成已登录", async () => {
  const platform = legacyPlatforms[1];
  await withLoginAgent([platform.login], async (api, calls) => {
    const parsed = api.parsePlatformAuthConfig(legacyPlatformConfig(platform), platform.id);
    await assert.rejects(api.confirmPlatformLoggedInForPosition("http://isolated-agent.invalid", parsed, () => {}), /招聘平台还没登录/);
    assert.equal(calls.length, 3);
  });
});

test("真实猎聘account/login即使回跳参数命中岗位入口和泛域名也不能算已登录", async () => {
  const platform = legacyPlatforms[1];
  const loginURL = "https://h.liepin.com/account/login?backurl=https%3A%2F%2Fh.liepin.com%2Fsearch%2FgetConditionItem";
  await withLoginAgent([loginURL], async (api, calls) => {
    const parsed = api.parsePlatformAuthConfig(legacyPlatformConfig(platform), platform.id);
    assert.equal(api.isLoginURL(loginURL, parsed), true);
    assert.equal(api.isLoggedInURL(loginURL, parsed), false);
    await assert.rejects(api.confirmPlatformLoggedInForPosition("http://isolated-agent.invalid", parsed, () => {}), /招聘平台还没登录/);
    assert.equal(calls.length, 3, "开始前的三次检查都不能放行这个真实登录页");
  });
  const businessURL = `${platform.entry}?source=login&backurl=https%3A%2F%2Fh.liepin.com%2Faccount%2Flogin`;
  await withLoginAgent([loginURL, loginURL, loginURL, businessURL, businessURL, businessURL], async (api, calls) => {
    const parsed = api.parsePlatformAuthConfig(legacyPlatformConfig(platform), platform.id);
    const statuses = [];
    assert.equal(api.isLoginURL(businessURL, parsed), false, "业务页查询参数提到login不能误判为登录页");
    assert.equal(api.isLoggedInURL(businessURL, parsed), true, "正常业务URL仍按原auth规则判断，不剥离或重写query");
    await api.waitForPlatformLoggedIn("http://isolated-agent.invalid", parsed, (message) => statuses.push(message));
    assert.equal(calls.length, 6, "离开真实登录页后才开始连续三次确认");
    assert.equal(statuses.filter((message) => message === "请在招聘浏览器中完成扫码或验证码登录").length, 3);
    assert.equal(statuses.at(-1), "正在确认登录状态 3/3");
  });
});

test("通用登录排除仅识别完整pathname段，不扫描query或把相似业务路径当登录页", async () => {
  const platform = legacyPlatforms[1];
  await withLoginAgent([], async (api) => {
    const parsed = api.parsePlatformAuthConfig(legacyPlatformConfig(platform), platform.id);
    for (const path of ["/account/login", "/account/signin", "/sso/sign-in/verify", "/user/log-in", "/user/log_in", "/user/sign_in", "/sso/logon", "/sso/signon", "/ACCOUNT/LOGIN.html", "/account/%6cogin", "/account/%6cogin/%broken"]) {
      const url = `${platform.domain}${path}?next=${encodeURIComponent(platform.entry)}`;
      assert.equal(api.isLoginURL(url, parsed), true, path);
      assert.equal(api.isLoggedInURL(url, parsed), false, path);
    }
    for (const path of ["/search/login-history", "/search/mylogin", "/search/signin-settings", "/search/getConditionItem?login=true&return_to=/account/login"]) {
      const url = `${platform.domain}${path}`;
      assert.equal(api.isLoginURL(url, parsed), false, path);
      assert.equal(api.isLoggedInURL(url, parsed), true, path);
    }
    const exactURL = `${platform.entry}?tab=candidates&source=login`;
    const exactConfig = { pages: [{ url: exactURL, match: "exact" }], public_pages: parsed.public_pages };
    assert.equal(api.isLoggedInURL(exactURL, exactConfig), true, "带query的显式exact成功规则保持原义");
    assert.equal(api.isLoggedInURL(platform.entry, exactConfig), false, "不能扩大原exact成功规则");
  });
});

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
