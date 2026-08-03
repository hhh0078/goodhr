/** 本文件负责自动回复前端不依赖浏览器的默认值和岗位条件去重规则。 */

export const DEFAULT_RESUME_REQUEST_MESSAGE = "你好，能发一份简历吗？";

/** emptyPositionAutoReplyConfig 创建岗位自动回复的安全默认配置。 */
export function emptyPositionAutoReplyConfig(positionID: string) {
  return {
    position_id: positionID,
    company_profile_id: "",
    enabled: false,
    position_description: "",
    resume_request_message: DEFAULT_RESUME_REQUEST_MESSAGE,
    poll_interval_seconds: 5,
    max_threads_per_checkpoint: 3,
    version: 0,
    conditions: [],
  };
}

/** duplicateConditionContent 返回忽略空白和标点后的第一条重复条件。 */
export function duplicateConditionContent(
  conditions: Array<{ content: string }>,
) {
  const seen = new Set<string>();
  for (const condition of conditions) {
    const content = condition.content.trim();
    if (!content) continue;
    const key = content
      .toLocaleLowerCase("zh-CN")
      .replace(/[\s\p{P}\p{S}]+/gu, "");
    if (seen.has(key)) return content;
    seen.add(key);
  }
  return "";
}
