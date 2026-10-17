/** 本文件验证官网 Firefox 下载仅使用正确身份、版本和同站地址的临时发布清单。 */
import assert from "node:assert/strict";
import test from "node:test";
import { normalizeFirefoxTemporaryRelease } from "./firefox-temporary-release.ts";

const site = "https://goodhr.58it.cn";
const manifest = {
  version: "1.0.16",
  url: "/zip/goodhr-resume-firefox-unsigned-1.0.16.xpi",
  sha256: "A".repeat(64),
  extension_id: "resume-assistant@goodhr.58it.cn",
  channel: "temporary",
};

test("临时清单返回真实版本、同站 XPI 地址和规范化摘要", () => {
  assert.deepEqual(normalizeFirefoxTemporaryRelease(manifest, site), {
    ...manifest,
    url: `${site}${manifest.url}`,
    sha256: "a".repeat(64),
  });
});

test("错误身份、渠道、版本或摘要不能开放下载", () => {
  for (const change of [
    { version: "" }, { version: "01.0.16" }, { version: "1.0.16-beta" },
    { sha256: "" }, { sha256: "x".repeat(64) },
    { channel: "signed" }, { extension_id: "someone-else@example.invalid" },
    { extra: "unexpected" },
  ]) assert.equal(normalizeFirefoxTemporaryRelease({ ...manifest, ...change }, site), null);
  for (const value of [null, [], "1.0.16", {}, { ...manifest, url: undefined }]) {
    assert.equal(normalizeFirefoxTemporaryRelease(value, site), null);
  }
});

test("站外、凭证、参数、版本不符和 ZIP 地址不能冒充 Firefox 临时 XPI", () => {
  for (const url of [
    `https://other.invalid${manifest.url}`,
    `http://goodhr.58it.cn${manifest.url}`,
    `https://user:pass@goodhr.58it.cn${manifest.url}`,
    `${manifest.url}?redirect=1`, `${manifest.url}#fragment`,
    "/zip/goodhr-resume-firefox-unsigned-1.0.9.xpi",
    "/zip/goodhr-resume-firefox-unsigned-1.0.16.zip",
    "javascript:alert(1)",
  ]) assert.equal(normalizeFirefoxTemporaryRelease({ ...manifest, url }, site), null);
  assert.equal(normalizeFirefoxTemporaryRelease(manifest, "not-a-url"), null);
  assert.equal(normalizeFirefoxTemporaryRelease(manifest, "http://public.invalid"), null);
});
