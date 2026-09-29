// 文件作用说明：模拟整个浏览器进程断开，验证异常退出只记录一次且保留最后的手动操作。

import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import test from "node:test";
import { BrowserSession } from "../dist/browser/session/browser-session.js";
import { WorkerLogger } from "../dist/logging/logger.js";

test("手动新建标签后浏览器断开只输出一次异常退出日志", () => {
  const lines = [];
  {
    const session = new BrowserSession(new WorkerLogger(line => lines.push(line)));
    const browser = new EventEmitter();
    const context = new EventEmitter();
    context.pages = () => [];
    session.browser = browser;
    session.registerContext(context);
    const page = new EventEmitter();
    page.url = () => "https://example.test/job?token=private";
    context.emit("page", page);
    browser.emit("disconnected");
    context.emit("close");
    assert.equal(lines.length, 1);
    const event = JSON.parse(lines[0]);
    assert.equal(event.category, "异常退出");
    assert.equal(event.is_error, true);
    assert.equal(event.action, "browser.exit");
    assert.equal(event.last_action, "新建标签页");
    assert.match(event.error_message, /断开/);
    assert.doesNotMatch(lines[0], /private/);
  }
});

test("程序主动关闭浏览器不记录异常退出", () => {
  const lines = [];
  {
    const session = new BrowserSession(new WorkerLogger(line => lines.push(line)));
    const browser = new EventEmitter();
    const context = new EventEmitter();
    context.pages = () => [];
    session.browser = browser;
    session.registerContext(context);
    session.lifecycle.dispose();
    browser.emit("disconnected");
    context.emit("close");
    assert.equal(lines.length, 0);
  }
});

test("标签页崩溃单独记录原始事件，旧浏览器延迟断开不影响新会话", () => {
  const lines = [];
  const session = new BrowserSession(new WorkerLogger(line => lines.push(JSON.parse(line))));
  const oldBrowser = new EventEmitter();
  const oldContext = new EventEmitter();
  const page = new EventEmitter();
  page.url = () => "https://example.test/detail?token=private";
  oldContext.pages = () => [page];
  session.browser = oldBrowser;
  session.registerContext(oldContext);
  page.emit("crash");
  assert.equal(lines[0].action, "browser.crash");
  assert.equal(lines[0].category, "异常退出");
  assert.equal(lines[0].reason_source, "page.crash");
  assert.doesNotMatch(JSON.stringify(lines[0]), /private/);
  session.lifecycle.dispose();
  session.lifecycle.begin();
  const newContext = new EventEmitter();
  newContext.pages = () => [];
  session.browser = new EventEmitter();
  session.registerContext(newContext);
  oldBrowser.emit("disconnected");
  oldContext.emit("close");
  page.emit("crash");
  assert.equal(lines.length, 1);
  newContext.emit("close");
  assert.equal(lines[1].action, "browser.exit");
});
