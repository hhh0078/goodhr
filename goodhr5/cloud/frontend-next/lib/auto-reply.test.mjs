/** 本文件负责验证自动回复前端默认值和岗位条件去重规则。 */

import assert from "node:assert/strict";
import test from "node:test";
import {
  DEFAULT_RESUME_REQUEST_MESSAGE,
  duplicateConditionContent,
  emptyPositionAutoReplyConfig,
} from "./auto-reply-rules.ts";

test("自动回复新岗位使用安全默认值", () => {
  const config = emptyPositionAutoReplyConfig("position-1");
  assert.equal(config.position_id, "position-1");
  assert.equal(config.enabled, false);
  assert.equal(config.resume_request_message, DEFAULT_RESUME_REQUEST_MESSAGE);
  assert.equal(config.max_threads_per_checkpoint, 3);
});

test("岗位条件忽略空格和标点后仍会识别重复", () => {
  const duplicate = duplicateConditionContent([
    {
      id: "",
      type: "required",
      content: "必须统招本科",
      sort_order: 0,
      enabled: true,
    },
    {
      id: "",
      type: "confirm",
      content: "必须 统招本科。",
      sort_order: 1,
      enabled: true,
    },
  ]);
  assert.equal(duplicate, "必须 统招本科。");
});
