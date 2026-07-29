// 文件作用说明：验证浏览器恢复出的已有标签页也会注册下载监听，避免只监听当前页。

import assert from "node:assert/strict";
import test from "node:test";

import { BrowserSession } from "../dist/browser/session/browser-session.js";
import { DownloadManager } from "../dist/browser/session/download-manager.js";

/** createPage 创建只记录事件名称的最小页面替身。 */
function createPage() {
  const events = [];
  return {
    events,
    on(eventName) {
      events.push(eventName);
    },
  };
}

/** 验证注册上下文时会遍历所有已经存在的页面。 */
test("已有标签页都会注册下载监听", () => {
  const firstPage = createPage();
  const secondPage = createPage();
  const context = {
    pages() {
      return [firstPage, secondPage];
    },
    on() {},
  };
  const logger = { info() {}, warn() {}, error() {} };
  const session = new BrowserSession(logger);

  session.registerContext(context);

  assert.equal(firstPage.events.includes("download"), true);
  assert.equal(secondPage.events.includes("download"), true);
});

/** 验证浏览器退出前等待下载落盘，并保留结果供 Go 最后同步。 */
test("退出前等待下载并保留下载记录", async () => {
  const logger = { info() {}, warn() {}, error() {} };
  const manager = new DownloadManager(logger);
  let finishDownload;
  const pending = new Promise((resolve) => {
    finishDownload = resolve;
  });
  manager.pendingDownloads.add(pending);
  manager.downloads.push({ id: "download-test", status: "saved" });

  let settled = false;
  const waiting = manager.waitForPending().then(() => {
    settled = true;
  });
  await Promise.resolve();
  assert.equal(settled, false);

  manager.pendingDownloads.delete(pending);
  finishDownload();
  await waiting;
  manager.reset();

  assert.equal(manager.list().pending, 0);
  assert.equal(manager.list().downloads.length, 1);
});
