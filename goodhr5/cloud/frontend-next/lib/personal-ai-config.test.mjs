/** 本文件验证个人 AI 配置的同域限制、外部模型保留及内置模型价格。 */
import assert from "node:assert/strict";
import test from "node:test";
import {
  formatBuiltinAIPrice,
  normalizeAIBaseURL,
  normalizeBuiltinAIModels,
  resolvePersonalAIModel,
  usesBuiltinAIModels,
} from "./personal-ai-config.ts";

const hostname = "goodhr.58it.cn";
const builtinURL = "https://GOODHR.58IT.CN/api/ai-compatible/v1/chat/completions";
const models = normalizeBuiltinAIModels([
  null,
  { id: " " },
  {
    id: " qwen3.8-flash ",
    name: " 通义千问3.8-flash-默认 ",
    description: " 非常推荐 ",
    input_price_per_1m_cents: 200,
    output_price_per_1m_cents: 540,
  },
  { id: "qwen3.8-flash", name: "重复项" },
  { id: "vision-plus", name: "", description: null },
]);

test("接口真实主机等于当前域名时才限制模型，忽略大小写", () => {
  assert.equal(usesBuiltinAIModels(builtinURL, hostname), true);
  assert.equal(usesBuiltinAIModels("https://goodhr.58it.cn:8443/api/v1", hostname), true);
  assert.equal(usesBuiltinAIModels("http://GOODHR.58IT.CN/api/v1", hostname), true);
  assert.equal(usesBuiltinAIModels("https://third-party.invalid/v1", hostname), false);
  assert.equal(usesBuiltinAIModels(builtinURL, ""), false);
});

test("拒绝把当前域名放进路径查询用户信息或相似子域名的伪装地址", () => {
  for (const value of [
    "https://external.invalid/goodhr.58it.cn", "https://external.invalid/?next=goodhr.58it.cn",
    "https://external.invalid/#goodhr.58it.cn", "https://goodhr.58it.cn.external.invalid/v1",
    "https://sub.goodhr.58it.cn/v1", "https://notgoodhr.58it.cn/v1",
    "https://goodhr.58it.cn@external.invalid/v1", "https://user:secret@goodhr.58it.cn/v1",
    "ftp://goodhr.58it.cn/v1", "javascript://goodhr.58it.cn", "/api/goodhr.58it.cn", "not a url",
  ]) assert.equal(usesBuiltinAIModels(value, hostname), false, value);
});

test("同域的旧模型和空模型回到首个有效项，有效模型继续保留", () => {
  assert.equal(resolvePersonalAIModel(builtinURL, "legacy-model", hostname, models), "qwen3.8-flash");
  assert.equal(resolvePersonalAIModel(builtinURL, "", hostname, models), "qwen3.8-flash");
  assert.equal(resolvePersonalAIModel(builtinURL, " vision-plus ", hostname, models), "vision-plus");
});

test("外部模型自由填写，列表读取失败或空列表不会覆盖已有模型", () => {
  assert.equal(resolvePersonalAIModel("https://third-party.invalid/v1", "my/private-model", hostname, models), "my/private-model");
  assert.equal(resolvePersonalAIModel(builtinURL, "keep-existing", hostname, null), "keep-existing");
  assert.equal(resolvePersonalAIModel(builtinURL, "keep-existing", hostname, []), "keep-existing");
});

test("清理空项和重复项时保持顺序，并保留模型提示", () => {
  assert.deepEqual(models.map((item) => item.id), ["qwen3.8-flash", "vision-plus"]);
  assert.equal(models[0].name, "通义千问3.8-flash-默认");
  assert.equal(models[0].description, "非常推荐");
  assert.equal(models[1].name, "vision-plus");
  assert.deepEqual(normalizeBuiltinAIModels({ models: [] }), []);
});

test("模型价格按分转元，缺失和错误价格不会显示为免费", () => {
  assert.equal(formatBuiltinAIPrice(models[0].input_price_per_1m_cents), "¥2.00");
  assert.equal(formatBuiltinAIPrice(models[0].output_price_per_1m_cents), "¥5.40");
  assert.equal(formatBuiltinAIPrice(0), "¥0.00");
  assert.equal(formatBuiltinAIPrice(undefined), "未配置");
  assert.equal(formatBuiltinAIPrice(-1), "未配置");
  assert.equal(formatBuiltinAIPrice(Number.NaN), "未配置");
});

test("补全接口地址时保留现有兼容路由，不重复追加路径", () => {
  assert.equal(normalizeAIBaseURL(" https://custom.invalid/v1/ "), "https://custom.invalid/v1/chat/completions");
  assert.equal(normalizeAIBaseURL(builtinURL), builtinURL);
  assert.equal(normalizeAIBaseURL(""), "");
});
