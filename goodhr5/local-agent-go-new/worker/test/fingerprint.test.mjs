// 文件作用说明：验证同一持久化 Profile 使用稳定指纹，且不覆盖调用方显式配置。

import assert from "node:assert/strict";
import test from "node:test";

import { withStableProfileFingerprint } from "../dist/browser/session/fingerprint.js";

/** 验证同一 Profile 每次生成相同指纹，不同 Profile 使用不同指纹。 */
test("持久化 Profile 使用稳定指纹", () => {
  const first = withStableProfileFingerprint([], "/profiles/account-a");
  const repeated = withStableProfileFingerprint([], "/profiles/account-a");
  const other = withStableProfileFingerprint([], "/profiles/account-b");

  assert.deepEqual(first, repeated);
  assert.match(first[0], /^--fingerprint=\d{5}$/);
  assert.notDeepEqual(first, other);
});

/** 验证调用方显式指纹优先，普通非持久化会话交给 CloakBrowser 自动生成。 */
test("保留显式指纹并跳过普通会话", () => {
  assert.deepEqual(
    withStableProfileFingerprint(["--fingerprint=12345"], "/profiles/account-a"),
    ["--fingerprint=12345"],
  );
  assert.deepEqual(withStableProfileFingerprint([], undefined), []);
});
