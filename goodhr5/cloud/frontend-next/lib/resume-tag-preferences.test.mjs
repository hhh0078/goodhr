/** 文件作用说明：验证标签缓存账号隔离、损坏恢复、最近点击排序和删除标签清理。 */
import assert from "node:assert/strict";
import test from "node:test";
import { orderResumeTags, parseResumeTagOrder, rememberResumeTag, resumeTagPreferenceKey } from "./resume-tag-preferences.ts";

test("标签缓存按账号隔离且匿名账号不写缓存", () => {
  assert.notEqual(resumeTagPreferenceKey("account-a"), resumeTagPreferenceKey("account-b"));
  assert.equal(resumeTagPreferenceKey("  "), "");
});
test("坏缓存和旧标签不会影响最近点击顺序", () => {
  assert.deepEqual(parseResumeTagOrder("broken-json"), []);
  assert.deepEqual(parseResumeTagOrder('{"id":"one"}'), []);
  assert.deepEqual(parseResumeTagOrder('["one",null,"one",12,"","two"]'), ["one", "two"]);
  const tags = [{ id: "one" }, { id: "two" }, { id: "three" }];
  const order = rememberResumeTag("two", ["deleted", "one", "two"], tags);
  assert.deepEqual(order, ["two", "one"]);
  assert.deepEqual(orderResumeTags(tags, order).map((tag) => tag.id), ["two", "one", "three"]);
  assert.deepEqual(tags.map((tag) => tag.id), ["one", "two", "three"]);
});
