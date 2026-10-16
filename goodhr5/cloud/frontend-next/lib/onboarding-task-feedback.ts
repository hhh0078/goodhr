/** 本文件统一新手教程对本地任务终态和无筛选结果提示的判断，不代替真实筛选结果或安全停止检查。 */

export type OnboardingTaskFeedback = {
  ended: boolean;
  statusText: string;
  error: string;
};

const endedStatuses = new Set(["failed", "stopped", "completed"]);

/** taskRecord 仅接收任务对象，空值和异常协议结构按尚未确认的状态处理。 */
function taskRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : {};
}

/** taskText 只读取去除首尾空白后的文字，避免把空值或对象显示给用户。 */
function taskText(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

/** isOnboardingTaskEnded 只按本地任务的明确终态判断结束，步骤名 finished 不能充当停止证明。 */
export function isOnboardingTaskEnded(task: unknown): boolean {
  return endedStatuses.has(taskText(taskRecord(task).status).toLowerCase());
}

/** getOnboardingTaskFeedback 在尚无真实筛选结果时区分失败、停止和结束，调用方应先处理真实筛选结果。 */
export function getOnboardingTaskFeedback(task: unknown): OnboardingTaskFeedback {
  const record = taskRecord(task);
  const status = taskText(record.status).toLowerCase();
  if (status === "failed") {
    return {
      ended: true,
      statusText: "这轮遇到问题，已经停下来了",
      error: taskText(record.error_message) || taskText(taskRecord(record.error).message) || taskText(record.summary)
        || "这一轮没跑通，还没有真实筛选结果。处理好提示后，可以重新开始。",
    };
  }
  if (status === "stopped") {
    return {
      ended: true,
      statusText: "这一轮已经停止",
      error: "这一轮已经停下，还没有真实筛选结果。准备好后，可以重新开始。",
    };
  }
  if (status === "completed") {
    return {
      ended: true,
      statusText: "这一轮已经结束",
      error: "这一轮已经结束，还没有真实筛选结果。可以调整平台筛选条件后重新开始。",
    };
  }

  const currentStep = taskText(record.current_step);
  if (currentStep.toLowerCase() === "finished" || status === "finished") {
    return { ended: false, statusText: "正在确认本轮状态", error: "" };
  }
  // 本地任务多数步骤为机器标识；只沿用已有中文说明，不把原始状态或标识直接展示为进度。
  const readableStatus = [currentStep, taskText(record.message), taskText(record.summary)]
    .find((value) => /[\u3400-\u9fff]/u.test(value));
  return { ended: false, statusText: readableStatus || "正在等待第一条真实筛选结果", error: "" };
}
