/** 文件作用说明：验证两轮独立查询、空分数与真实零值、开始时间边界及真实毫秒展示。 */
import assert from "node:assert/strict";
import test from "node:test";
import { emptyScreeningFilters, screeningDuration, screeningQuery, screeningScore, screeningTime } from "./position-screening.ts";

/** round 构造展示所需的单轮最小样例，不访问后端或模型。 */
function round(overrides = {}) { return { kind: "ai", status: "passed", score: 0, ...overrides }; }

test("固定二十条分页，空条件不作为零分发送", () => {
  const filters = emptyScreeningFilters(); assert.equal(screeningQuery(2, filters).toString(), "page=2&page_size=20");
  assert.equal(screeningQuery(-1, filters).get("page"), "1");
  filters.first.scoreMin = 0; assert.equal(screeningQuery(1, filters).get("first_score_min"), "0");
  assert.equal(filters.second.scoreMin, null); assert.equal(emptyScreeningFilters().first.scoreMin, null);
});

test("两轮独立分数与单侧时间转为UTC，时间上限不包含由服务端执行", () => {
  const filters = emptyScreeningFilters(); filters.first = { scoreMin: 0, scoreMax: 60, startedFrom: "2026-10-26T08:00:00+08:00", startedTo: "" };
  filters.second = { scoreMin: 70, scoreMax: 100, startedFrom: "", startedTo: "2026-10-27T00:00:00+08:00" };
  const query = screeningQuery(3, filters);
  assert.equal(query.get("first_started_from"), "2026-10-26T00:00:00.000Z"); assert.equal(query.get("second_started_to"), "2026-10-26T16:00:00.000Z");
  assert.equal(query.get("first_score_max"), "60"); assert.equal(query.get("second_score_min"), "70");
  assert.equal(query.has("first_started_to"), false); assert.equal(query.has("second_started_from"), false);
});

test("拒绝反向范围、非法时间、越界和非有限分数", () => {
  for (const pair of [[80, 20], [-1, null], [null, 101], [NaN, null], [Infinity, 100]]) {
    const filters = emptyScreeningFilters(); [filters.first.scoreMin, filters.first.scoreMax] = pair; assert.throws(() => screeningQuery(1, filters));
  }
  for (const times of [["bad", ""], ["2026-10-26T00:00:00Z", "2026-10-26T00:00:00Z"], ["2026-10-27T00:00:00Z", "2026-10-26T00:00:00Z"]]) {
    const filters = emptyScreeningFilters(); [filters.second.startedFrom, filters.second.startedTo] = times; assert.throws(() => screeningQuery(1, filters));
  }
});

test("未执行、关键词、无分数和真实零分保持不同", () => {
  assert.equal(screeningScore(round()), "0 分"); assert.equal(screeningScore(round({ score: null })), "未评分");
  assert.equal(screeningScore(round({ status: "not_executed", score: null })), "未评分");
  assert.equal(screeningScore(round({ kind: "keyword", score: null })), "关键词判断，不评分"); assert.equal(screeningScore(round({ score: NaN })), "未评分");
});

test("未知时间耗时不补零，实际零毫秒和一毫秒均保留", () => {
  assert.equal(screeningDuration(null), "未记录"); assert.equal(screeningDuration(0), "0 秒"); assert.equal(screeningDuration(1), "0.001 秒");
  assert.equal(screeningDuration(1250), "1.25 秒"); assert.equal(screeningDuration(-1), "未记录");
  assert.equal(screeningTime(null), "未记录"); assert.equal(screeningTime("bad"), "未记录"); assert.match(screeningTime("2026-10-26T00:00:12Z"), /:12/);
});
