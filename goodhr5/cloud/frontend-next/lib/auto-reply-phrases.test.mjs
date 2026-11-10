/** 文件作用说明：验证空正文、岗位覆盖、恢复继承及删除场景不会被前端合并成默认文字。 */
import assert from "node:assert/strict";
import test from "node:test";
import { effectiveReplyPhrases, replacePhraseOverride } from "./auto-reply-phrases.ts";

const base = { id: "receive_resume", category: "简历", title: "收到简历", scene: "收到附件时", content: "原默认文字" };

test("显式空覆盖保留条目，恢复继承才重新使用默认文字", () => {
  const override = { ...base, content: "" };
  const rows = replacePhraseOverride([], base.id, override);
  assert.deepEqual(effectiveReplyPhrases([base], rows), [override]);
  assert.deepEqual(effectiveReplyPhrases([base], replacePhraseOverride(rows, base.id)), [base]);
  assert.deepEqual(effectiveReplyPhrases([{ ...base, content: "" }], []), [{ ...base, content: "" }]);
});

test("删除项保留场景数据但不在编辑列表重新出现", () => {
  const removed = { ...base, content: "", deleted: true };
  const custom = { ...base, id: "job_private", title: "自建场景" };
  const rows = [removed, { ...custom, content: "", deleted: true }];
  assert.deepEqual(effectiveReplyPhrases([base], rows), []);
  assert.equal(rows[1].scene, custom.scene);
  assert.equal(rows[1].content, "");
});

test("显式deleted false与未提供标记一样生效，只有true才删除", () => {
  const active = { ...base, content: "保留的正常话术", deleted: false };
  const empty = { ...base, id: "receive_phone", content: "", deleted: false };
  assert.deepEqual(effectiveReplyPhrases([], [active, empty]), [active, empty]);
  assert.deepEqual(effectiveReplyPhrases([base], [active]), [active]);
  assert.deepEqual(effectiveReplyPhrases([], [active, { ...empty, deleted: true }]), [active]);
});
