/** 文件作用说明：验证收藏夹缓存账号隔离、损坏恢复、最近点击排序和删除收藏夹清理。 */
import assert from "node:assert/strict";
import test from "node:test";
import { orderResumeFolders, parseResumeFolderOrder, rememberResumeFolder, resumeFolderPreferenceKey } from "./resume-folder-preferences.ts";

test("收藏夹缓存按账号隔离且匿名账号不写缓存", () => {
  assert.notEqual(resumeFolderPreferenceKey("account-a"), resumeFolderPreferenceKey("account-b"));
  assert.equal(resumeFolderPreferenceKey("  "), "");
  assert.equal(resumeFolderPreferenceKey("account-a"), "goodhr5_resume_folder_order:account-a");
  assert.notEqual(resumeFolderPreferenceKey("account-a"), "goodhr5_resume_tag_order:account-a");
});
test("坏缓存和旧收藏夹不会影响最近点击顺序", () => {
  assert.deepEqual(parseResumeFolderOrder("broken-json"), []);
  assert.deepEqual(parseResumeFolderOrder('{"id":"one"}'), []);
  assert.deepEqual(parseResumeFolderOrder('["one",null,"one",12,"","two"]'), ["one", "two"]);
  const folders = [{ id: "one" }, { id: "two" }, { id: "three" }];
  const order = rememberResumeFolder("two", ["deleted", "one", "two"], folders);
  assert.deepEqual(order, ["two", "one"]);
  assert.deepEqual(orderResumeFolders(folders, order).map((folder) => folder.id), ["two", "one", "three"]);
  assert.deepEqual(folders.map((folder) => folder.id), ["one", "two", "three"]);
});
