/** 文件作用说明：统一自动回复测试开放范围及提示，查看、资料编辑和停止继续使用既有入口。 */
export const AUTO_REPLY_ROLLOUT_CLOSED = false;
export const AUTO_REPLY_ROLLOUT_MESSAGE = "这个平台暂未开放自动回复，目前仅支持 BOSS直聘、猎聘企业端和智联招聘。";
export const AUTO_REPLY_TEST_NOTICE = "自动回复正在测试中，建议先添加作者微信后再使用，遇到问题可以及时联系作者。";

/** isAutoReplyPlatformSupported 判断岗位所属平台是否在本次测试范围内，缺失和未知值均不开放。 */
export function isAutoReplyPlatformSupported(platformID: unknown): boolean {
  return ["boss", "liepin", "zhaopin"].includes(String(platformID || "").trim().toLowerCase());
}
