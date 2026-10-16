/** 本文件负责验证岗位启动余额检查，以及必要版本升级和可忽略小补丁的判断。 */

import assert from "node:assert/strict";
import test from "node:test";
import { evaluatePositionStartGuard, isMandatoryLocalAgentUpdate, isVersionLower, latestLocalAgentRelease, positionUsesAI } from "./position-start-guard.ts";

/** withUserAgent 临时模拟操作系统，只影响纯函数测试，结束后恢复原始运行环境。 */
function withUserAgent(userAgent, callback) {
  const descriptor = Object.getOwnPropertyDescriptor(globalThis, "navigator");
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: { userAgent } });
  try { return callback(); }
  finally {
    if (descriptor) Object.defineProperty(globalThis, "navigator", descriptor);
    else delete globalThis.navigator;
  }
}

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

test("小补丁仍需通过 AI 余额检查", () => {
  assert.equal(evaluatePositionStartGuard({ balance: "0" }, "6.2", "6.2.0.1", true)?.code, "ai_balance_insufficient");
  assert.equal(isMandatoryLocalAgentUpdate("", "6.2"), false);
});

test("本地版本缺失时禁止启动，并指明连接问题", () => {
  for (const current of [undefined, null, "", "   "]) {
    const failure = evaluatePositionStartGuard(null, current, "6.2.11.0", false);
    assert.equal(failure?.code, "agent_version_unavailable");
    assert.match(failure?.message || "", /本地程序.*版本/);
    assert.match(failure?.message || "", /连接|重新检查/);
  }
});

test("后台发布版本缺失时禁止启动，不误导重连健康的本地程序", () => {
  for (const required of [undefined, null, "", "   "]) {
    const failure = evaluatePositionStartGuard(null, "6.2.11.0", required, false);
    assert.equal(failure?.code, "agent_release_unavailable");
    assert.match(failure?.message || "", /后台|发布配置/);
    assert.doesNotMatch(failure?.message || "", /重新连接本地程序|重启本地程序/);
  }
});

test("Windows发布缺失不能借用Mac发布版本通过启动检查", () => {
  withUserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64)", () => {
    const release = latestLocalAgentRelease({ local_agent: [{ version: "6.2.11.0", url_mac: "https://example.invalid/agent.pkg" }] });
    assert.equal(release.version, "");
    assert.equal(release.url, "");
    assert.equal(evaluatePositionStartGuard(null, "6.2.11.0", release.version, false)?.code, "agent_release_unavailable");
  });
});

test("Mac发布缺失不能借用Windows发布版本通过启动检查", () => {
  withUserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)", () => {
    const release = latestLocalAgentRelease({ local_agent: [{ version: "6.2.11.0", url_win: "https://example.invalid/agent.exe" }] });
    assert.equal(release.version, "");
    assert.equal(release.url, "");
    assert.equal(evaluatePositionStartGuard(null, "6.2.11.0", release.version, false)?.code, "agent_release_unavailable");
  });
});

test("对应系统首条发布版本为空时跳过，使用后续有版本和下载地址的发布", () => {
  for (const [userAgent, urlField, wrongField] of [
    ["Mozilla/5.0 (Windows NT 10.0; Win64; x64)", "url_win", "url_mac"],
    ["Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)", "url_mac", "url_win"],
  ]) {
    withUserAgent(userAgent, () => {
      const release = latestLocalAgentRelease({ local_agent: [null, { version: " ", [urlField]: "https://example.invalid/empty-version" }, { version: "6.2.12.0", [wrongField]: "https://example.invalid/other-os" }, { version: "6.2.11.0", [urlField]: "https://example.invalid/valid" }] });
      assert.equal(release.version, "6.2.11.0");
      assert.equal(release.url, "https://example.invalid/valid");
      assert.equal(evaluatePositionStartGuard(null, "6.2.11.0", release.version, false), null);
    });
  }
});
