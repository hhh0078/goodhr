// 文件作用说明：验证临时插件完整核验且保留 Profile，以及签名 XPI 原有幂等升级和失败保留行为。
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

/** temporaryRelease 写入独立临时插件，用 Go 同合同的 UTF8 路径字节生成目录摘要，不包含真实登录信息。 */
async function temporaryRelease(managed, changes = {}) {
  const manifest = {
    manifest_version: 3, name: "GoodHR 简历助手", version: "1.0.16",
    background: { scripts: ["src/background.js"] },
    browser_specific_settings: { gecko: { id: extensionID } },
    ...changes,
  };
  const files = [
    ["manifest.json", Buffer.from(JSON.stringify(manifest))],
    ["src/background.js", Buffer.from("// 合成后台入口，不访问网络\n")],
    ["src/简体.txt", Buffer.from("合成内容")],
    ["src/\ue000.txt", Buffer.from("UTF8排序在补充平面字符之前")],
    ["src/\u{1f600}.txt", Buffer.from("UTF16默认排序会把本文件提前")],
  ];
  for (const [relative, bytes] of files) {
    await fs.mkdir(path.dirname(path.join(managed, relative)), { recursive: true });
    await fs.writeFile(path.join(managed, relative), bytes);
  }
  const hash = createHash("sha256");
  for (const [relative, bytes] of files.sort((left, right) => Buffer.compare(Buffer.from(left[0]), Buffer.from(right[0])))) {
    hash.update(Buffer.from(`${Buffer.byteLength(relative)}:${relative}:${bytes.length}:`)); hash.update(bytes);
  }
  const receipt = { version: manifest.version, sha256: createHash("sha256").update("合成临时包归档").digest("hex"), tree_sha256: hash.digest("hex"), channel: "temporary" };
  await fs.writeFile(path.join(managed, ".goodhr-release.json"), JSON.stringify(receipt));
  await fs.writeFile(path.join(managed, ".goodhr-load-required"), JSON.stringify(receipt));
  return { manifest, receipt };
}

test("临时包按完整目录收据核验并保留原生加载路径，不写 Profile 或删除旧签名版本", async (t) => {
  const { managed, profile, installed } = await fixture(t);
  await fs.mkdir(path.dirname(installed));
  await fs.writeFile(installed, "原有签名包保持原样");
  await fs.writeFile(path.join(profile, "synthetic-login-marker"), "合成持久标记，不是真实凭证");
  const before = await fs.stat(installed);
  await temporaryRelease(managed);
  assert.equal(await installManagedFirefoxAddon(profile, [managed], context), null);
  assert.equal(await fs.readFile(installed, "utf8"), "原有签名包保持原样");
  assert.equal((await fs.stat(installed)).ino, before.ino);
  assert.equal(await fs.readFile(path.join(profile, "synthetic-login-marker"), "utf8"), "合成持久标记，不是真实凭证");
  assert.deepEqual((await fs.readdir(profile)).sort(), ["extensions", "synthetic-login-marker"]);
  const absentProfile = path.join(profile, "never-created");
  assert.equal(await installManagedFirefoxAddon(absentProfile, [managed], context), null);
  await assert.rejects(fs.stat(absentProfile), { code: "ENOENT" });
});

test("临时渠道不能绕过固定身份、版本、MV3和后台入口校验，即使目录摘要一致也拒绝", async (t) => {
  const invalidManifests = [
    { name: "其它插件" }, { manifest_version: 2 }, { version: "" },
    { browser_specific_settings: { gecko: { id: "other@example.test" } } },
    { background: { scripts: [] } }, { background: { scripts: [""] } },
    { background: { scripts: ["../outside.js"] } }, { background: { scripts: ["/outside.js"] } },
    { background: { scripts: ["src\\background.js"] } }, { background: { scripts: ["src/background.js"], service_worker: "src/background.js" } },
    { background: { scripts: ["src/missing.js"] } }, { background: { scripts: [1] } },
  ];
  for (const changes of invalidManifests) {
    const { managed, profile } = await fixture(t); await temporaryRelease(managed, changes);
    await assert.rejects(installManagedFirefoxAddon(profile, [managed], context), { code: "BROWSER_ADDON_INVALID" }, JSON.stringify(changes));
    assert.deepEqual(await fs.readdir(profile), []);
  }
});

test("临时包拒绝正文篡改、额外文件、收据不匹配和仅修改渠道的签名包", async (t) => {
  for (const change of ["content", "extra", "version", "tree", "archive", "channel", "signed-source"]) {
    const { managed, profile } = await fixture(t), { receipt } = await temporaryRelease(managed);
    if (change === "content") await fs.writeFile(path.join(managed, "src/background.js"), "被替换的内容");
    else if (change === "extra") await fs.writeFile(path.join(managed, "unexpected.js"), "未计入摘要的新入口");
    else if (change === "signed-source") await fs.writeFile(path.join(managed, ".goodhr-signed.xpi"), "不能混用旧签名源");
    else {
      receipt[({ version: "version", tree: "tree_sha256", archive: "sha256", channel: "channel" })[change]] = ({ version: "1.0.15", tree: "f".repeat(64), archive: "invalid", channel: "arbitrary" })[change];
      await fs.writeFile(path.join(managed, ".goodhr-release.json"), JSON.stringify(receipt));
    }
    await assert.rejects(installManagedFirefoxAddon(profile, [managed], context), { code: "BROWSER_ADDON_INVALID" }, change);
    assert.deepEqual(await fs.readdir(profile), []);
  }
  const { managed, profile } = await fixture(t); await release(managed, Buffer.from("合成签名包"));
  const receipt = JSON.parse(await fs.readFile(path.join(managed, ".goodhr-release.json"), "utf8")); receipt.channel = "temporary";
  await fs.writeFile(path.join(managed, ".goodhr-release.json"), JSON.stringify(receipt));
  await assert.rejects(installManagedFirefoxAddon(profile, [managed], context), { code: "BROWSER_ADDON_INVALID" });
});

test("临时包拒绝目录内链接、托管根目录链接和空后台文件，失败不创建 Profile 内容", async (t) => {
  for (const change of ["symlink", "directory-symlink", "empty"]) {
    const { root, managed, profile } = await fixture(t); await temporaryRelease(managed);
    if (change === "empty") await fs.writeFile(path.join(managed, "src/background.js"), "");
    else if (change === "symlink") await fs.symlink(path.join(managed, "src/background.js"), path.join(managed, "linked.js"));
    else { const alternate = path.join(root, "alternate-addon"); await fs.rename(managed, alternate); await fs.symlink(alternate, managed, process.platform === "win32" ? "junction" : "dir"); }
    await assert.rejects(installManagedFirefoxAddon(profile, [managed], context), { code: "BROWSER_ADDON_INVALID" }, change);
    assert.deepEqual(await fs.readdir(profile), []);
  }
});

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
