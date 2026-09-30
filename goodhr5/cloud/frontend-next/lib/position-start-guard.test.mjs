/** 本文件负责验证岗位启动余额检查，以及必要版本升级和可忽略小补丁的判断。 */

import assert from "node:assert/strict";
import test from "node:test";
import { evaluatePositionStartGuard, isMandatoryLocalAgentUpdate, isVersionLower, positionUsesAI } from "./position-start-guard.ts";

test("非 AI 岗位跳过余额检查", () => {
  const failure = evaluatePositionStartGuard(null, "5.3.5", "5.3.5", false);
  assert.equal(failure, null);
});

test("AI 岗位余额不足时禁止启动", () => {
  const failure = evaluatePositionStartGuard({ balance: "0" }, "5.3.5", "5.3.5", true);
  assert.equal(failure?.code, "ai_balance_insufficient");
});

test("本地程序版本过低时提示刷新并触发更新提醒", () => {
  const failure = evaluatePositionStartGuard(null, "5.3.91", "5.3.94", false);
  assert.equal(failure?.code, "agent_version_outdated");
  assert.match(failure?.message || "", /请立即刷新浏览器或重启浏览器/);
  assert.match(failure?.message || "", /自动弹出更新提醒/);
});

test("基础筛选或详情筛选启用 AI 时识别为 AI 岗位", () => {
  assert.equal(positionUsesAI({ common_config: { mode_default: "ai", detail_mode: "ocr" } }), true);
  assert.equal(positionUsesAI({ common_config: { mode_default: "keyword", detail_mode: "ai" } }), true);
  assert.equal(positionUsesAI({ common_config: { mode_default: "keyword", detail_mode: "ocr" } }), false);
});

test("第四段及之后的小补丁可以忽略，忽略后允许开始岗位任务", () => {
  for (const [current, target] of [["6.1.9", "6.1.9.1"], ["6.1.9.1", "6.1.9.2"], ["6.1.9.99", "6.1.9.100"], ["6.1.9.1", "6.1.9.1.1"], ["6.2", "6.2.0.1"], ["v6.2.0", "v6.2.0.2"], ["6.2.4.2", "6.2.4.3"]]) {
    assert.equal(isVersionLower(current, target), true, `${current} 应能发现 ${target}`);
    assert.equal(isMandatoryLocalAgentUpdate(current, target), false, `${current} 到 ${target} 可忽略`);
    assert.equal(evaluatePositionStartGuard(null, current, target, false), null);
    assert.equal(evaluatePositionStartGuard({ balance: "1" }, current, target, true), null);
  }
});

test("前三段升高必须更新，已有小补丁不能绕过必要升级", () => {
  for (const [current, target] of [["6.1.9", "6.1.10"], ["6.1.9.99", "6.1.10.1"], ["6.1.9", "6.2"], ["6.1.99", "6.2.0.1"], ["6.2", "6.2.1"], ["6.9.99.9", "7"]]) {
    assert.equal(isMandatoryLocalAgentUpdate(current, target), true, `${current} 到 ${target} 必须更新`);
    assert.equal(evaluatePositionStartGuard(null, current, target, false)?.code, "agent_version_outdated");
  }
});

test("版本补零相等、目标更旧时不要求更新", () => {
  for (const [current, target] of [["6.2", "6.2.0"], ["6.2.0.0", "6.2"], ["6.1.10", "6.1.9.100"], ["6.2", "6.1.99"], ["6.1.9.2", "6.1.9.1"]]) {
    assert.equal(isVersionLower(current, target), false);
    assert.equal(isMandatoryLocalAgentUpdate(current, target), false);
    assert.equal(evaluatePositionStartGuard(null, current, target, false), null);
  }
});

test("小补丁仍需通过 AI 余额检查，缺失版本仍禁止开始任务", () => {
  assert.equal(evaluatePositionStartGuard({ balance: "0" }, "6.2", "6.2.0.1", true)?.code, "ai_balance_insufficient");
  assert.equal(evaluatePositionStartGuard(null, "", "6.2", false)?.code, "agent_version_unavailable");
  assert.equal(evaluatePositionStartGuard(null, "6.2", "", false)?.code, "agent_version_unavailable");
  assert.equal(isMandatoryLocalAgentUpdate("", "6.2"), false);
});
