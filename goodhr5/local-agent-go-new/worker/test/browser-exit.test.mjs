// 文件作用说明：模拟整个浏览器进程断开，验证异常退出只记录一次且保留最后的手动操作。

import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import test from "node:test";
import { BrowserSession } from "../dist/browser/session/browser-session.js";
import { WorkerLogger } from "../dist/logging/logger.js";

test("手动新建标签后浏览器断开只输出一次异常退出日志", () => {
  const lines = [];
  const originalWrite = process.stdout.write;
  process.stdout.write = (chunk) => { lines.push(String(chunk)); return true; };
  try {
    const session = new BrowserSession(new WorkerLogger());
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
  } finally {
    process.stdout.write = originalWrite;
  }
});

test("程序主动关闭浏览器不记录异常退出", () => {
  const lines = [];
  const originalWrite = process.stdout.write;
  process.stdout.write = (chunk) => { lines.push(String(chunk)); return true; };
  try {
    const session = new BrowserSession(new WorkerLogger());
    const browser = new EventEmitter();
    const context = new EventEmitter();
    context.pages = () => [];
    session.browser = browser;
    session.registerContext(context);
    session.intentionalClose = true;
    browser.emit("disconnected");
    context.emit("close");
    assert.equal(lines.length, 0);
  } finally {
    process.stdout.write = originalWrite;
  }
});
