// 文件作用说明：验证 GoodHR 签名 XPI 进入 Firefox Profile 时的幂等升级和失败保留行为。
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { installManagedFirefoxAddon } from "../dist/browser/session/managed-firefox-addon.js";

const extensionID = "resume-assistant@goodhr.58it.cn";
const context = { action: "browser.start", trace_id: "managed-firefox-addon-test", started_at: Date.now() };

/** fixture 创建独立的托管插件与浏览器 Profile，测试结束后删除。 */
async function fixture(t) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "goodhr-firefox-profile-"));
  const managed = path.join(root, "goodhr-resume-assistant");
  const profile = path.join(root, "profile");
  await fs.mkdir(managed);
  await fs.mkdir(profile);
  t.after(() => fs.rm(root, { recursive: true, force: true }));
  return { root, managed, profile, installed: path.join(profile, "extensions", `${extensionID}.xpi`) };
}

/** release 写入与 XPI 一致的身份清单和 SHA256 发布收据。 */
async function release(managed, body, version = "1.0.7") {
  const digest = createHash("sha256").update(body).digest("hex");
  await fs.writeFile(path.join(managed, "manifest.json"), JSON.stringify({
    manifest_version: 3, name: "GoodHR 简历助手", version,
    browser_specific_settings: { gecko: { id: extensionID } },
  }));
  await fs.writeFile(path.join(managed, ".goodhr-release.json"), JSON.stringify({ version, sha256: digest }));
  await fs.writeFile(path.join(managed, ".goodhr-signed.xpi"), body);
}

test("签名原包原子进入 Profile，同版本重复启动保持现有文件", async (t) => {
  const { managed, profile, installed } = await fixture(t);
  const original = Buffer.from("signed-xpi-original");
  await release(managed, original);
  assert.equal(await installManagedFirefoxAddon(profile, [managed], context), managed);
  assert.deepEqual(await fs.readFile(installed), original);
  const firstInfo = await fs.stat(installed);
  assert.equal(await installManagedFirefoxAddon(profile, [managed], context), managed);
  const secondInfo = await fs.stat(installed);
  assert.equal(secondInfo.ino, firstInfo.ino);
});

test("签名包升级会替换 Profile 文件，损坏的新包保留旧版", async (t) => {
  const { managed, profile, installed } = await fixture(t);
  const oldXPI = Buffer.from("signed-xpi-old");
  const newXPI = Buffer.from("signed-xpi-new");
  await release(managed, oldXPI, "1.0.7");
  await installManagedFirefoxAddon(profile, [managed], context);
  await release(managed, newXPI, "1.0.8");
  await installManagedFirefoxAddon(profile, [managed], context);
  assert.deepEqual(await fs.readFile(installed), newXPI);
  await fs.writeFile(path.join(managed, ".goodhr-signed.xpi"), "tampered");
  await assert.rejects(installManagedFirefoxAddon(profile, [managed], context), { code: "BROWSER_ADDON_INVALID" });
  assert.deepEqual(await fs.readFile(installed), newXPI);
});

test("无托管插件不改普通扩展，符号链接扩展目录不会写入外部位置", async (t) => {
  const { root, managed, profile } = await fixture(t);
  assert.equal(await installManagedFirefoxAddon(profile, [], context), null);
  await release(managed, Buffer.from("signed-xpi"));
  const outside = path.join(root, "outside");
  await fs.mkdir(outside);
  await fs.symlink(outside, path.join(profile, "extensions"));
  await assert.rejects(installManagedFirefoxAddon(profile, [managed], context), { code: "BROWSER_ADDON_INVALID" });
  assert.deepEqual(await fs.readdir(outside), []);
});
