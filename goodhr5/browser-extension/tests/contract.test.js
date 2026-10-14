/* 文件作用：用真实插件整理器生成请求，并与仓库云端字段及结构模型核对；Go 路由测试另直接消费同一 JSON。 */
"use strict";
const test = require("node:test"), assert = require("node:assert/strict"), fs = require("node:fs"), path = require("node:path");
const shared = require("../src/shared.js"), fixtureInput = require("./fixtures/create-input.json"), fixtureOutput = require("./fixtures/plugin-candidate-create.json");
const backend = path.resolve(__dirname, "..", "..", "cloud", "backend", "internal", "httpapi");
const libraryCode = fs.readFileSync(path.join(backend, "candidate_library.go"), "utf8");

/** cloudFieldNames 从当前 Go 源码的字段白名单提取实际合同，新增输出必须被云端识别。 */
function cloudFieldNames(variable) {
  const declaration = libraryCode.match(new RegExp(`var ${variable} = \\[\\]string\\{([^}]+)\\}`));
  assert.ok(declaration, `${variable} 云端白名单未找到`);
  return [...declaration[1].matchAll(/"([a-z_]+)"/g)].map((match) => match[1]);
}

test("实际插件 CREATE fixture 与当前整理器一致，所有字段属于当前云端白名单", () => {
  const payload = { ...shared.candidateWriteResume(fixtureInput), idempotency_key: "plugin-cross-module-fixture-1" };
  assert.deepEqual(payload, fixtureOutput);
  const text = cloudFieldNames("candidateTextColumns"), arrays = cloudFieldNames("candidateArrayColumns"), extras = cloudFieldNames("candidateExtraFields");
  const allowed = new Set([...text, ...arrays, ...extras, "platform_id", "platform_candidate_id", "source_url", "expected_salary_min", "expected_salary_max", "idempotency_key", "tag_ids"]);
  for (const [key, value] of Object.entries(payload)) {
    assert.ok(allowed.has(key), `${key} 会被云端忽略`);
    if ([...text, ...extras].includes(key) && !["tags", "age"].includes(key)) assert.equal(typeof value, "string", `${key} 后端要求文字`);
    if (arrays.includes(key)) assert.ok(Array.isArray(value) && value.every((item) => typeof item === "object" && item !== null), `${key} 后端要求对象数组`);
  }
  assert.deepEqual(payload.tag_ids, []); assert.equal(Object.hasOwn(payload, "tags"), false); assert.match(libraryCode, /"tag_ids"/);
  assert.equal(payload.expected_salary_min, 20); assert.equal(payload.expected_salary_max, 25); assert.equal(payload.expected_salary_text, "20-25K/月");
  assert.equal(payload.current_region, fixtureInput.current_region); assert.equal(payload.expected_region, fixtureInput.expected_region); assert.equal(payload.expected_city, undefined);
  assert.match(payload.birth_ym, /^\d{4}-(?:0[1-9]|1[0-2])$/);
});

test("经历子字段与当前 Go 模型一致，标签严格限制且薪资不将文字或人民币数字误转 K", () => {
  const modelCode = fs.readFileSync(path.join(backend, "candidate.go"), "utf8");
  const models = { work_experiences: "CandidateWorkExperience", educations: "CandidateEducation", project_experiences: "CandidateProjectExperience", certificates: "CandidateCertificate", honors: "CandidateHonor" };
  for (const [field, name] of Object.entries(models)) {
    const declaration = modelCode.match(new RegExp(`type ${name} struct \\{([\\s\\S]+?)\\n\\}`)); assert.ok(declaration);
    const keys = [...declaration[1].matchAll(/json:"([a-z_]+)"/g)].map((match) => match[1]);
    assert.deepEqual(Object.keys(fixtureOutput[field][0]).sort(), keys.sort(), field);
  }
  const result = shared.sanitizeResume({ tags: [...Array(35)].map((_, index) => `${index}${"字".repeat(80)}`), expected_salary_min: "20000", expected_salary_max: NaN, birth_ym: "1990-19", expected_city: "兼容旧输入城市" });
  assert.equal(result.tags.length, 30); assert.ok(result.tags.every((tag) => Array.from(tag).length <= 50)); assert.equal(result.expected_salary_min, undefined); assert.equal(result.expected_salary_max, undefined);
  assert.equal(result.birth_ym, ""); assert.equal(result.expected_region, "兼容旧输入城市"); assert.equal(result.expected_city, undefined);
  assert.deepEqual(shared.sanitizeResume({ tags: " 测试，测试;技能 " }).tags, ["测试", "技能"]);
});
