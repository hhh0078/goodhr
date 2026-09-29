// 文件作用说明：显式运行本地静态页面冒烟，验证真实新标签页、超时后继续操作和无脚本注入。

import assert from "node:assert/strict";
import fs from "node:fs/promises";
import http from "node:http";
import os from "node:os";
import path from "node:path";
import { ActionService } from "../dist/browser/actions/action-service.js";

const root = await fs.mkdtemp(path.join(os.tmpdir(), "goodhr-click-exit-smoke-"));
const html = '<!doctype html><html><meta charset="utf-8"><title>GoodHR 本地验证</title><style>body{padding:40px;font:24px sans-serif}input,button,a{display:block;margin:30px;width:280px;height:45px}#spacer{height:2200px}</style><a id="popup" href="/detail" target="_blank">打开新标签页</a><button id="ordinary">普通按钮</button><input id="input" aria-label="验证输入"><div id="spacer"></div><footer>滚动验证底部</footer></html>';
const server = http.createServer((_request, response) => { response.writeHead(200, { "Content-Type": "text/html; charset=utf-8" }); response.end(html); });
await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
const actions = new ActionService();

/** context 为本地冒烟生成独立操作标识及超时，不连接招聘账号。 */
function context(action) { return { action, trace_id: "real-click-smoke", request_id: action, started_at: Date.now(), deadline_at: Date.now() + 90000 }; }

/** selector 创建通用 CSS 选择器，只使用正式封装能力查找元素。 */
function selector(css) { return { description: "本地验证元素", target: { selectors: [{ type: "css", value: css }] } }; }

try {
  await actions.startBrowser({ headless: true, url: `http://127.0.0.1:${server.address().port}/`, downloads_path: root }, context("browser.start"));
  const result = await actions.clickElement({ selector: selector("#popup"), wait_for_new_page: true, new_page_timeout_ms: 10000 }, context("element.click"));
  assert.equal(result.new_page_opened, true);
  assert.match(result.new_page_url, /\/detail$/);
  await assert.rejects(actions.clickElement({ selector: selector("#ordinary"), wait_for_new_page: true, new_page_timeout_ms: 20 }, context("element.click")), error => error.code === "ACTION_TIMEOUT" && error.step === "wait_new_page");
  const input = await actions.inputElement({ selector: selector("#input"), text: "timeout recovered", verify: true }, context("element.input"));
  assert.equal(input.verified, true);
  assert.equal(actions.healthStatus().status, "ok");
  await actions.scroll({ distance: 500 }, context("element.scroll"));
  const screenshot = await actions.screenshot({ directory: root, filename: "after-scroll.png" }, context("screenshot.page"));
  assert.ok(screenshot.size > 0);
  console.log("SMOKE_OK " + JSON.stringify({ new_page: true, timeout_handled: true, next_input_verified: true, worker_healthy: true, real_wheel: true, screenshot: screenshot.path }));
} finally {
  await actions.stopBrowser(context("browser.stop"));
  await new Promise(resolve => server.close(resolve));
}
