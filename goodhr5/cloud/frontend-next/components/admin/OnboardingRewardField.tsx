/** 本文件提供关注赠送天数的紧凑配置输入，合并草稿时保留注册试用及本地组件等全部字段。 */
"use client";
import { Flex, Form, InputNumber, Typography } from "antd";

/** parseOnboardingConfig 安全读取对象草稿，格式不正确时禁止快捷输入覆盖原始内容。 */
function parseOnboardingConfig(value: string): Record<string, unknown> | null {
  try {
    const parsed = JSON.parse(value);
    return parsed && typeof parsed === "object" && !Array.isArray(parsed)
      ? parsed
      : null;
  } catch {
    return null;
  }
}

/** validateOnboardingReward 与云端保持整数 1 至 365 的校验，字段缺失时使用服务端缺省值。 */
export function validateOnboardingReward(value: string) {
  const config = parseOnboardingConfig(value);
  if (!config) return "本地程序与组件配置需要使用 JSON 对象";
  if (!("wechat_follow_reward_days" in config)) return "";
  const days = config.wechat_follow_reward_days;
  return typeof days === "number" &&
    Number.isInteger(days) &&
    days >= 1 &&
    days <= 365
    ? ""
    : "关注赠送请填写 1 至 365 的整数天数";
}

/** OnboardingRewardField 只调整关注奖励，不改变原注册赠送字段或本地程序配置。 */
export default function OnboardingRewardField({
  value,
  onChange,
}: {
  value: string;
  onChange: (value: string) => void;
}) {
  const config = parseOnboardingConfig(value);
  const current = config?.wechat_follow_reward_days;
  /** changeReward 合并关注天数，并保留 JSON 中所有未知字段及嵌套内容。 */
  function changeReward(days: number | null) {
    if (!config) return;
    onChange(
      JSON.stringify({ ...config, wechat_follow_reward_days: days }, null, 2),
    );
  }
  return (
    <Flex vertical gap={4} style={{ marginBottom: 16 }}>
      <Form layout="inline">
        <Form.Item
          label="关注赠送"
          htmlFor="wechat-follow-reward-days"
          style={{ margin: 0 }}
        >
          <InputNumber
            id="wechat-follow-reward-days"
            value={
              current === undefined
                ? 2
                : typeof current === "number"
                  ? current
                  : null
            }
            disabled={!config}
            min={1}
            max={365}
            precision={0}
            onChange={changeReward}
            style={{ width: 100 }}
            suffix="天"
          />
        </Form.Item>
      </Form>
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        1–365 天，每个账号仅领一次。与原注册赠送字段 trial_days
        同组，下面的其他配置保持原样。
      </Typography.Text>
    </Flex>
  );
}
