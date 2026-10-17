/* 文件作用：验证托管Firefox临时发布清单绑定固定身份、版本和已构建XPI摘要，独立于正式签名渠道。 */
"use strict";
const test = require("node:test"), assert = require("node:assert/strict");
const { version } = require("../package.json");
const { firefoxTemporaryRelease, FIREFOX_TEMPORARY_RELEASE_FILE } = require("../scripts/build.js");

test("临时发布清单只引用固定ID的当前Firefox未签名XPI，不借用Chrome摘要", () => {
  const sha = "a".repeat(64), packages = [{ browser: "chrome", file: `goodhr-resume-chrome-${version}.zip`, sha256: "b".repeat(64) }, { browser: "firefox", file: `goodhr-resume-firefox-unsigned-${version}.xpi`, sha256: sha, signed: false, installation: "temporary-only" }];
  assert.equal(FIREFOX_TEMPORARY_RELEASE_FILE, "goodhr-resume-firefox-latest.json");
  assert.deepEqual(firefoxTemporaryRelease(packages), { version, url: `/zip/goodhr-resume-firefox-unsigned-${version}.xpi`, sha256: sha, extension_id: "resume-assistant@goodhr.58it.cn", channel: "temporary" });
  for (const patch of [{ signed: true }, { installation: "permanent" }, { sha256: "broken" }, { file: "other.xpi" }, { browser: "chrome" }]) assert.throws(() => firefoxTemporaryRelease([{ ...packages[1], ...patch }]), /缺少/);
  assert.throws(() => firefoxTemporaryRelease([]), /缺少/);
});
