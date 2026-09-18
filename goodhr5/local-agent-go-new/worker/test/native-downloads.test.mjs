// 文件作用说明：验证原生下载记录观察、目录确认、会话隔离与清除记录不影响文件和浏览器历史。
import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { DownloadManager } from "../dist/browser/session/download-manager.js";
import { parseNativeDownloadSnapshot } from "../dist/browser/session/native-download-snapshot.js";

const logger = { info() {}, warn() {}, error() {} };

/** fixture 创建与实际浏览器一致的下载快照。 */
function fixture(sessionID, directory, downloads = [], revision = 0) {
 return { version: 1, session_id: sessionID, directory, revision, downloads };
}

/** entry 创建包含实际文件路径的原生记录。 */
function entry(filePath, status = "saved", createdAt = "2026-09-18T08:00:00.000Z") {
 return { file_path: filePath, url: "https://example.test/resume.pdf", page_url: "https://example.test/", size: 20, status, error: "", created_at: createdAt };
}

/** 验证稳定编号与浏览器实际路径，不另存或更改文件。 */
test("原生记录保留实际路径且重启读取编号稳定", () => {
 const directory = path.join(os.tmpdir(), "下载");
 const snapshot = fixture("session", directory, [entry(path.join(directory, "简历.pdf"))]);
 const first = parseNativeDownloadSnapshot(snapshot, "session").downloads[0];
 const second = parseNativeDownloadSnapshot(snapshot, "session").downloads[0];
 assert.equal(first.id, second.id);
 assert.equal(first.filename, "简历.pdf");
 assert.equal(first.path, snapshot.downloads[0].file_path);
 const repeated = parseNativeDownloadSnapshot(fixture("session", directory, [entry(first.path, "saved", "2026-09-18T09:00:00.000Z")]), "session");
 assert.notEqual(first.id, repeated.downloads[0].id);
});

/** 验证外部快照的会话、路径、状态和字段均受校验。 */
test("拒绝其他会话和不合法原生下载状态", () => {
 const directory = os.tmpdir();
 const valid = fixture("session", directory, [entry(path.join(directory, "resume.pdf"))]);
 assert.throws(() => parseNativeDownloadSnapshot(valid, "other"));
 for (const invalid of [{file_path:"relative.pdf"}, {size:-1}, {status:"unknown"}, {created_at:"broken"}]) {
  assert.throws(() => parseNativeDownloadSnapshot({...valid, downloads:[{...valid.downloads[0], ...invalid}]}, "session"));
 }
});

/** 验证读取进度和终态，不修改文件；清除 GoodHR 记录后不从原生历史重新导入。 */
test("观察终态、清除记录和关闭都不会删除用户下载", async () => {
 const directory = await fs.mkdtemp(path.join(os.tmpdir(), "goodhr-download-test-"));
 const manager = new DownloadManager(logger);
 try {
  await manager.prepare(directory);
  const prefs = await manager.prepareNativeSession();
  const filePath = path.join(directory, "简历.pdf");
  await fs.writeFile(filePath, "%PDF-1.4 test");
  const snapshotPath = prefs["goodhr.downloads.snapshot"];
  const sessionID = prefs["goodhr.downloads.session"];
  await fs.writeFile(snapshotPath, JSON.stringify(fixture(sessionID, directory, [entry(filePath,"pending")])));
  await manager.waitForReady();
  assert.equal((await manager.list()).pending, 1);
  await fs.writeFile(snapshotPath, JSON.stringify(fixture(sessionID, directory, [entry(filePath)])));
  await manager.waitForReady();
  assert.equal((await manager.list()).downloads[0].status, "saved");
  await manager.clear();
  assert.equal((await manager.list()).count, 0);
  assert.equal(JSON.parse(await fs.readFile(snapshotPath,"utf8")).downloads.length, 1);
  assert.equal(await fs.readFile(filePath,"utf8"), "%PDF-1.4 test");
  await manager.reset();
  assert.equal(await fs.readFile(filePath,"utf8"), "%PDF-1.4 test");
 } finally { await manager.reset(); await fs.rm(directory,{recursive:true,force:true}); }
});

/** 验证目录切换需要浏览器回写确认，不影响之前下载的真实路径。 */
test("浏览器确认后才返回新目录", async () => {
 const directory = await fs.mkdtemp(path.join(os.tmpdir(), "goodhr-directory-test-"));
 const manager = new DownloadManager(logger);
 try {
  await manager.prepare(directory);
  const prefs = await manager.prepareNativeSession();
  const snapshotPath = prefs["goodhr.downloads.snapshot"];
  const settingsPath = prefs["goodhr.downloads.settings"];
  const snapshot = fixture(prefs["goodhr.downloads.session"], directory, [entry(path.join(directory,"old.pdf"))]);
  await fs.writeFile(snapshotPath,JSON.stringify(snapshot));
  await manager.waitForReady();
  let complete = false;
  const next = path.join(directory,"next");
  const switching = manager.configure(next,{action:"downloads.configure",trace_id:"test",started_at:Date.now()}).then(() => { complete = true; });
  let settings;
  for (let attempt=0;attempt<50;attempt++) {
   settings = JSON.parse(await fs.readFile(settingsPath,"utf8"));
   if(settings.directory===next) break;
   await new Promise(resolve=>setTimeout(resolve,10));
  }
  assert.equal(complete,false);
  assert.equal(manager.directory(),directory);
  await fs.writeFile(snapshotPath,JSON.stringify({...snapshot,directory:settings.directory,revision:settings.revision}));
  await switching;
  assert.equal(manager.directory(),next);
  assert.equal((await manager.list()).downloads[0].path,path.join(directory,"old.pdf"));
 } finally { await manager.reset(); await fs.rm(directory,{recursive:true,force:true}); }
});

/** 验证首次启动观察器前，Go 轮询不会因快照尚未生成而报错。 */
test("浏览器准备期间下载轮询安静等待", async () => {
 const manager = new DownloadManager(logger);
 try {
  await manager.prepareNativeSession();
  assert.equal((await manager.list()).count,0);
  assert.equal((await manager.list()).pending,0);
 } finally { await manager.reset(); }
});
