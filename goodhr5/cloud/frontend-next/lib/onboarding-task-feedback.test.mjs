/** 本文件验证教程任务失败与无结果结束的提示，以及步骤名不能绕过安全停止检查的边界。 */
import assert from "node:assert/strict";
import test from "node:test";
import { getOnboardingTaskFeedback, isOnboardingTaskEnded } from "./onboarding-task-feedback.ts";

test("只用明确任务状态识别终态，finished 步骤不能充当终态", () => {
  for (const status of ["failed", "stopped", "completed", " FAILED "]) {
    assert.equal(isOnboardingTaskEnded({ status }), true);
    assert.equal(getOnboardingTaskFeedback({ status }).ended, true);
  }
  for (const status of ["running", "pending", "finished", "unknown", "", undefined]) {
    assert.equal(isOnboardingTaskEnded({ status, current_step: "finished" }), false);
    assert.equal(getOnboardingTaskFeedback({ status, current_step: "finished" }).ended, false);
  }
});

test("失败优先展示真实 error_message，不让摘要覆盖错误原因", () => {
  const feedback = getOnboardingTaskFeedback({
    status: "failed", current_step: "finished", error_message: "  没有找到对应岗位  ",
    error: { message: "兼容错误" }, summary: "我没处理成功，但问题不大，运行记录已经留好了",
  });
  assert.deepEqual(feedback, { ended: true, statusText: "这轮遇到问题，已经停下来了", error: "没有找到对应岗位" });
  assert.doesNotMatch(feedback.statusText, /finished|failed/);
});

test("缺少直接错误时兼容 error.message，再退回真实任务摘要", () => {
  assert.equal(getOnboardingTaskFeedback({ status: "failed", error_message: " ", error: { message: " 浏览器连接断开 " }, summary: "摘要" }).error, "浏览器连接断开");
  assert.equal(getOnboardingTaskFeedback({ status: "failed", error_message: {}, error: { message: [] }, summary: " 任务流程未完成 " }).error, "任务流程未完成");
  const feedback = getOnboardingTaskFeedback({ status: "failed", error_message: [], error: "原始错误字符串", summary: null });
  assert.match(feedback.error, /没跑通.*重新开始/);
  assert.doesNotMatch(feedback.error, /object Object|undefined|null/);
});

test("主动停止与正常结束无结果分别说明，不误认为教程完成或仍在运行", () => {
  const stopped = getOnboardingTaskFeedback({ status: "stopped", current_step: "finished", summary: "任务已按你的要求停下来了" });
  const completed = getOnboardingTaskFeedback({ status: "completed", current_step: "finished", summary: "任务已经处理完成" });
  assert.equal(stopped.statusText, "这一轮已经停止");
  assert.match(stopped.error, /还没有真实筛选结果.*重新开始/);
  assert.equal(completed.statusText, "这一轮已经结束");
  assert.match(completed.error, /还没有真实筛选结果.*调整平台筛选条件/);
  assert.notEqual(stopped.error, completed.error);
});

test("运行中沿用中文步骤，候选人单条错误不会被当成整轮结束", () => {
  assert.deepEqual(getOnboardingTaskFeedback({ status: "running", current_step: " 正在查看候选人 ", error_message: "上一位候选人读取超时" }), {
    ended: false, statusText: "正在查看候选人", error: "",
  });
  assert.equal(getOnboardingTaskFeedback({ status: "running", current_step: "scan_decide_and_greet", message: " 正在匹配关键词 " }).statusText, "正在匹配关键词");
  assert.equal(getOnboardingTaskFeedback({ status: "running", current_step: "scan_decide_and_greet", summary: " 正在处理候选人 " }).statusText, "正在处理候选人");
});

test("running 伴随 finished 时只确认状态，不暴露英文或允许重跑", () => {
  assert.deepEqual(getOnboardingTaskFeedback({ status: "running", current_step: " finished ", message: "已经结束" }), {
    ended: false, statusText: "正在确认本轮状态", error: "",
  });
  assert.deepEqual(getOnboardingTaskFeedback({ status: "finished" }), {
    ended: false, statusText: "正在确认本轮状态", error: "",
  });
});

test("缺失、空白和异常结构不能生成假终态或原始状态提示", () => {
  for (const task of [undefined, null, [], "failed", 1, {}, { status: {} }, { status: "running", current_step: {} }, { status: "running", current_step: "running", message: "pending" }]) {
    assert.equal(isOnboardingTaskEnded(task), false);
    assert.deepEqual(getOnboardingTaskFeedback(task), { ended: false, statusText: "正在等待第一条真实筛选结果", error: "" });
  }
});
