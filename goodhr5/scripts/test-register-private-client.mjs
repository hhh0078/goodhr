/** 文件作用：使用临时目录验证私有安装器登记、构建身份校验、Windows BOM 与双平台清单保留，不接触业务目录。 */
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import crypto from "node:crypto";
import { fileURLToPath, pathToFileURL } from "node:url";

test("私有安装器按地址和版本校验，Windows BOM可读取且登记Mac不会移除Windows", async (t) => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "goodhr-private-register-"));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  const scripts = path.join(directory, "scripts");
  fs.mkdirSync(scripts);
  const source = path.join(path.dirname(fileURLToPath(import.meta.url)), "register-private-client.mjs");
  const isolated = path.join(scripts, "register-private-client.mjs");
  fs.copyFileSync(source, isolated);
  const { registerClient } = await import(pathToFileURL(isolated).href);
  const environment = path.join(directory, "private.env"), info = path.join(directory, "build-info.json");
  fs.writeFileSync(environment, "GOODHR_PUBLIC_BASE_URL=https://192.168.1.20:8443\n");
  fs.writeFileSync(info, "\uFEFF"+JSON.stringify({ version: "6.4.0", environment: "private", public_base_url: "https://other.invalid" }));
  const windows = path.join(directory, "synthetic.exe"), mac = path.join(directory, "synthetic.pkg");
  fs.writeFileSync(windows, "synthetic-windows-installer");
  fs.writeFileSync(mac, "synthetic-mac-installer");
  assert.throws(() => registerClient(environment, windows, info, "win", "6.4.0"), /构建身份/);
  assert.equal(fs.existsSync(path.join(directory, "private-data")), false);
  fs.writeFileSync(info, "\uFEFF"+JSON.stringify({ version: "6.4.0", environment: "private", public_base_url: "https://192.168.1.20:8443" }));
  const win = registerClient(environment, windows, info, "win", "6.4.0");
  assert.equal(win.sha256, crypto.createHash("sha256").update(fs.readFileSync(windows)).digest("hex"));
  assert.ok(win.url_win.startsWith("https://192.168.1.20:8443/downloads/private/"));
  registerClient(environment, mac, info, "mac", "6.4.0");
  const releases = JSON.parse(fs.readFileSync(path.join(directory, "private-data", "assets", "clients.json"), "utf8"));
  assert.equal(releases.length, 2);
  assert.equal(releases.filter(release => release.url_win).length, 1);
  assert.equal(releases.filter(release => release.url_mac).length, 1);
});
