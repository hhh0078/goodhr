/* 文件作用：从合成采集输入运行真实插件字段整理器，输出可由 Go httptest 消费的完整 CREATE 请求。 */
"use strict";
const fs = require("node:fs"), path = require("node:path"), shared = require("../src/shared.js");
const directory = path.resolve(__dirname, "..", "tests", "fixtures");

/** exportCreateFixture 生成无真实个人资料的跨模块联调 JSON，并保留稳定幂等键。 */
function exportCreateFixture() {
  const input = JSON.parse(fs.readFileSync(path.join(directory, "create-input.json"), "utf8"));
  const payload = { ...shared.sanitizeResume(input), idempotency_key: "plugin-cross-module-fixture-1" };
  fs.writeFileSync(path.join(directory, "plugin-candidate-create.json"), JSON.stringify(payload, null, 2) + "\n");
  return payload;
}

if (require.main === module) { exportCreateFixture(); console.log("合成 CREATE 请求已写入 tests/fixtures/plugin-candidate-create.json"); }
module.exports = { exportCreateFixture };
