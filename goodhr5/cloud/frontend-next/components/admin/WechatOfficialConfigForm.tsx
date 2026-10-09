/** 本文件使用 Ant Design 表单编辑超管公众号配置，并保留未重新填写的 AES 密钥。 */
"use client";
import { CopyOutlined } from "@ant-design/icons";
import {
  Alert,
  Button,
  Flex,
  Form,
  Image,
  Input,
  Select,
  Space,
  Switch,
  Tag,
  Typography,
} from "antd";
import { officialAccountQRURL, readableImageURL } from "@/lib/wechat-official";

type ConfigValues = Record<string, unknown>;
type Notify = (
  message: string,
  severity?: "success" | "error" | "warning" | "info",
) => void;

/** parseOfficialConfig 安全读取已有 JSON，非法草稿交由系统配置页面统一提示。 */
export function parseOfficialConfig(value: string): ConfigValues {
  try {
    const parsed = JSON.parse(value || "{}");
    return parsed && typeof parsed === "object" && !Array.isArray(parsed)
      ? parsed
      : {};
  } catch {
    return {};
  }
}

/** validateOfficialConfig 只校验已启用公众号的必要配置，AES 留空表示保留服务端密钥。 */
export function validateOfficialConfig(value: string) {
  const config = parseOfficialConfig(value);
  if (config.enabled === true) {
    if (!String(config.app_id || "").trim()) return "请填写公众号 AppID";
    if (!String(config.original_id || "").trim()) return "请填写公众号原始 ID";
    if (!String(config.token || "").trim()) return "请填写服务器配置 Token";
    if (
      !String(config.encoding_aes_key || "").trim() &&
      config.encoding_aes_key_configured !== true
    )
      return "请填写 EncodingAESKey";
  }
  const aes = String(config.encoding_aes_key || "").trim();
  if (aes && !/^[A-Za-z0-9+/]{43}$/.test(aes))
    return "EncodingAESKey 需要 43 位 Base64 字符";
  const qrURL = String(config.qr_image_url || "").trim();
  if (qrURL) {
    try {
      const url = new URL(qrURL);
      if (url.protocol !== "https:" || !url.hostname)
        return "二维码图片请使用 HTTPS 地址";
    } catch {
      return "二维码图片请使用 HTTPS 地址";
    }
  }
  return "";
}

/** officialConfigForSave 移除只读提示字段，空 AES 原样交给后端保留，不展示原始 JSON。 */
export function officialConfigForSave(value: string) {
  const config = { ...parseOfficialConfig(value) };
  delete config.callback_url;
  delete config.encoding_aes_key_configured;
  delete config.reward_days;
  return JSON.stringify(config, null, 2);
}

/** WechatOfficialConfigForm 复用系统配置草稿和保存流程，密钥不通过源码编辑器暴露。 */
export default function WechatOfficialConfigForm({
  value,
  onChange,
  notify,
}: {
  value: string;
  onChange: (value: string) => void;
  notify: Notify;
}) {
  const config = parseOfficialConfig(value);
  const originalID = String(config.original_id || "");
  const qrURL =
    readableImageURL(String(config.qr_image_url || "")) ||
    officialAccountQRURL(originalID);
  const callbackBase =
    process.env.NEXT_PUBLIC_CLOUD_API_BASE ||
    (typeof window === "undefined"
      ? "https://goodhr.58it.cn"
      : window.location.origin);
  const callbackURL = String(
    config.callback_url ||
      `${callbackBase.replace(/\/$/, "")}/api/wechat-official/callback`,
  );
  /** setField 合并单个配置项，不覆盖未编辑字段和服务端密钥保留标记。 */
  function setField(key: string, nextValue: unknown) {
    onChange(JSON.stringify({ ...config, [key]: nextValue }, null, 2));
  }
  /** copyText 复制已显示给超管的配置内容，失败时保留界面并说明手动复制方式。 */
  async function copyText(text: string, label: string) {
    try {
      await navigator.clipboard.writeText(text);
      notify(`${label}已复制`, "success");
    } catch {
      notify("这次没复制成功，可以选中文字手动复制", "warning");
    }
  }
  return (
    <Flex vertical gap={16}>
      <Alert
        type="info"
        showIcon
        title="个人订阅号用绑定码就能接上"
        description="把服务器地址、Token 和消息模式填到公众号后台。用户关注后发送绑定码，验证成功后领取一次会员奖励。安全消息也不需要 AppSecret。"
      />
      <Form layout="vertical" style={{ maxWidth: 720 }}>
        <Form.Item label="启用公众号绑定" htmlFor="wechat-official-enabled">
          <Switch
            id="wechat-official-enabled"
            checked={config.enabled === true}
            onChange={(enabled) => setField("enabled", enabled)}
          />
        </Form.Item>
        <Form.Item label="公众号名称" htmlFor="wechat-official-account-name">
          <Input
            id="wechat-official-account-name"
            value={String(config.account_name || "")}
            placeholder="GoodHR"
            onChange={(event) => setField("account_name", event.target.value)}
          />
        </Form.Item>
        <Form.Item
          label="公众号原始 ID"
          htmlFor="wechat-official-original-id"
          extra="公众号后台的原始 ID，以 gh_ 开头。用于生成官方关注二维码。"
        >
          <Input
            id="wechat-official-original-id"
            value={originalID}
            placeholder="gh_ce9d36869c26"
            onChange={(event) => setField("original_id", event.target.value)}
          />
        </Form.Item>
        <Form.Item
          label="AppID"
          htmlFor="wechat-official-app-id"
          extra="与公众号后台的 AppID 保持一致。"
        >
          <Input
            id="wechat-official-app-id"
            value={String(config.app_id || "")}
            autoComplete="off"
            onChange={(event) => setField("app_id", event.target.value)}
          />
        </Form.Item>
        <Form.Item
          label="服务器地址（只读）"
          htmlFor="wechat-official-callback-url"
        >
          <Space.Compact block>
            <Input
              id="wechat-official-callback-url"
              value={callbackURL}
              readOnly
              style={{ minWidth: 0 }}
            />
            <Button
              icon={<CopyOutlined aria-hidden="true" />}
              onClick={() => void copyText(callbackURL, "服务器地址")}
            >
              复制地址
            </Button>
          </Space.Compact>
        </Form.Item>
        <Form.Item
          label="服务器配置 Token"
          htmlFor="wechat-official-token"
          extra="与公众号后台的 Token 保持一致。只有超级管理员能查看和复制。"
        >
          <Space.Compact block>
            <Input.Password
              id="wechat-official-token"
              autoComplete="new-password"
              value={String(config.token || "")}
              onChange={(event) => setField("token", event.target.value)}
              style={{ minWidth: 0 }}
            />
            <Button
              icon={<CopyOutlined aria-hidden="true" />}
              disabled={!String(config.token || "")}
              onClick={() => void copyText(String(config.token || ""), "Token")}
            >
              复制 Token
            </Button>
          </Space.Compact>
        </Form.Item>
        <Form.Item
          label="EncodingAESKey"
          htmlFor="wechat-official-aes-key"
          extra={
            config.encoding_aes_key_configured
              ? "已配置密钥，留空会保留；重新填写才会覆盖。"
              : "启用前填写 43 位 EncodingAESKey，保存后不再回显。"
          }
        >
          <Input.Password
            id="wechat-official-aes-key"
            autoComplete="new-password"
            value={String(config.encoding_aes_key || "")}
            placeholder={
              config.encoding_aes_key_configured
                ? "已配置，留空保留"
                : "填写 43 位 EncodingAESKey"
            }
            onChange={(event) =>
              setField("encoding_aes_key", event.target.value)
            }
          />
        </Form.Item>
        <Form.Item
          label="消息模式"
          htmlFor="wechat-official-message-mode"
          extra="与微信后台消息加解密方式保持一致，推荐安全模式。"
        >
          <Select
            id="wechat-official-message-mode"
            value={String(config.message_mode || "safe")}
            onChange={(mode) => setField("message_mode", mode)}
            options={[
              { value: "safe", label: "安全模式（推荐）" },
              { value: "compat", label: "兼容模式" },
              { value: "plaintext", label: "明文模式" },
            ]}
          />
        </Form.Item>
        <Form.Item
          label="关注二维码图片地址"
          htmlFor="wechat-official-qr-url"
          extra="留空按原始 ID 使用微信官方图片，也可以填自己的 HTTPS 图片地址。"
        >
          <Input
            id="wechat-official-qr-url"
            value={String(config.qr_image_url || "")}
            onChange={(event) => setField("qr_image_url", event.target.value)}
          />
        </Form.Item>
        <Form.Item label="关注奖励">
          <Tag color="success">每个账号仅领一次</Tag>
          <Typography.Text type="secondary">
            赠送天数在“本地程序与组件”的“关注赠送”中调整。
          </Typography.Text>
        </Form.Item>
      </Form>
      {qrURL ? (
        <Flex vertical gap={8} align="flex-start">
          <Typography.Text type="secondary">关注二维码预览</Typography.Text>
          <Image
            src={qrURL}
            alt="公众号关注二维码预览"
            width={180}
            height={180}
            preview={false}
            style={{ objectFit: "contain" }}
          />
        </Flex>
      ) : null}
    </Flex>
  );
}
