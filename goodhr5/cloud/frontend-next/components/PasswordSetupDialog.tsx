/** 本文件负责强制历史账号设置登录密码，设置完成前不允许关闭弹框。 */
"use client";

import { LockOutlined } from "@ant-design/icons";
import { Alert, Button, Form, Input, Modal } from "antd";
import { useState } from "react";

type PasswordSetupDialogProps = {
  open: boolean;
  email?: string;
  onSubmit: (password: string) => Promise<void>;
};

/** PasswordSetupDialog 收集并确认历史账号的新密码。 */
export default function PasswordSetupDialog({
  open,
  email,
  onSubmit,
}: PasswordSetupDialogProps) {
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  /** submit 校验两次输入并保存密码。 */
  async function submit() {
    if (loading) return;
    setError("");
    if ([...password].length < 2) return setError("密码至少需要 2 个字符");
    if (password !== confirmation)
      return setError("两次密码不一样，我先不敢保存");
    setLoading(true);
    try {
      await onSubmit(password);
      setPassword("");
      setConfirmation("");
    } catch (submitError) {
      setError(
        submitError instanceof Error
          ? submitError.message
          : "密码没保存成功，请再试一次",
      );
    } finally {
      setLoading(false);
    }
  }

  return (
    <Modal
      open={open}
      title="给老账号补一个密码"
      width={440}
      closable={false}
      mask={{ closable: false }}
      keyboard={false}
      footer={
        <Button
          type="primary"
          loading={loading}
          onClick={() => void submit()}
          disabled={loading || [...password].length < 2 || !confirmation}
        >
          设置密码并继续
        </Button>
      }
    >
      <p
        style={{
          color: "var(--goodhr-text-secondary, #637069)",
          lineHeight: 1.7,
        }}
      >
        我小声提醒一下：{email || "这个账号"}{" "}
        以前只用验证码，还没有密码。设置好以后，登录会快很多。
      </p>
      <Form layout="vertical" onFinish={() => void submit()}>
        <Form.Item
          label="新密码"
          htmlFor="account-new-password"
          extra="至少 2 个字符"
        >
          <Input.Password
            id="account-new-password"
            autoComplete="new-password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            disabled={loading}
            prefix={<LockOutlined aria-hidden="true" />}
            onPressEnter={(event) => {
              event.preventDefault();
              void submit();
            }}
          />
        </Form.Item>
        <Form.Item label="再输入一次" htmlFor="account-confirm-password">
          <Input.Password
            id="account-confirm-password"
            autoComplete="new-password"
            value={confirmation}
            onChange={(event) => setConfirmation(event.target.value)}
            disabled={loading}
            onPressEnter={(event) => {
              event.preventDefault();
              void submit();
            }}
          />
        </Form.Item>
        {error ? <Alert type="error" title={error} showIcon /> : null}
      </Form>
    </Modal>
  );
}
