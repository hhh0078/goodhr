/** 本文件展示公众号免密登录二维码和一次性验证码，并串行等待当前页面的微信确认结果。 */
"use client";

import { Alert, Button, Flex, Image, Spin, Typography } from "antd";
import { useEffect, useRef, useState } from "react";
import { apiRequest, INVITE_CACHE_KEY } from "@/lib/api";
import styles from "./WechatLogin.module.css";

type Challenge = { ticket: string; login_code: string; qr_image_url: string; expires_at: string };
type Props = { agreementAccepted: boolean; onAgreement: () => void; onSuccess: (data: Record<string, unknown>) => void };

/** WechatLogin 保持验证码只属于当前页面，切换入口或重新生成后停止旧轮询，收到登录态才跳转。 */
export default function WechatLogin({ agreementAccepted, onAgreement, onSuccess }: Props) {
  const [challenge, setChallenge] = useState<Challenge | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [status, setStatus] = useState("waiting");
  const [remaining, setRemaining] = useState(0);
  const [copyText, setCopyText] = useState("复制验证码");
  const [generation, setGeneration] = useState(0);
  const agreement = useRef(agreementAccepted);
  const success = useRef(onSuccess);
  const previousTicket = useRef("");
  agreement.current = agreementAccepted;
  success.current = onSuccess;

  useEffect(() => {
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), 10000);
    let active = true;
    setLoading(true); setError(""); setChallenge(null); setStatus("waiting");
    apiRequest("/api/auth/wechat/create", { method: "POST", body: JSON.stringify({ previous_ticket: previousTicket.current }), signal: controller.signal })
      .then((data: Challenge) => {
        if (!active) return;
        if (!/^[a-f0-9]{64}$/.test(String(data.ticket || "")) || !/^\d{8}$/.test(String(data.login_code || "")) || !Number.isFinite(Date.parse(data.expires_at))) throw new Error("登录码没读完整，请重新生成");
        previousTicket.current = data.ticket;
        setChallenge(data);
      })
      .catch((cause: unknown) => { if (active) setError(cause instanceof Error ? cause.message : "登录码没准备好，可以重试或先用邮箱登录"); })
      .finally(() => { window.clearTimeout(timeout); if (active) setLoading(false); });
    return () => { active = false; window.clearTimeout(timeout); controller.abort(); };
  }, [generation]);

  useEffect(() => {
    if (!challenge) return;
    /** updateRemaining 以服务端截止时间计算剩余秒数，后台标签页恢复后不沿用过期倒计时。 */
    const updateRemaining = () => setRemaining(Math.max(0, Math.ceil((Date.parse(challenge.expires_at) - Date.now()) / 1000)));
    updateRemaining();
    const timer = window.setInterval(updateRemaining, 1000);
    return () => window.clearInterval(timer);
  }, [challenge]);

  useEffect(() => {
    if (!challenge) return;
    let controller: AbortController | undefined;
    let active = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let failures = 0;
    /** poll 等待本次请求完成后再安排下一次，防止慢请求叠加或离开页面后恢复旧登录。 */
    async function poll() {
      if (!active) return;
      if (Date.now() >= Date.parse(challenge!.expires_at)) { setStatus("expired"); return; }
      controller = new AbortController();
      const requestTimeout = window.setTimeout(() => controller?.abort(), 10000);
      try {
        const result = await apiRequest("/api/auth/wechat/poll", {
          method: "POST",
          body: JSON.stringify({ ticket: challenge!.ticket, agreement_accepted: agreement.current, inviter_id: localStorage.getItem(INVITE_CACHE_KEY) || "" }),
          signal: controller.signal,
        });
        if (!active) return;
        failures = 0; setError("");
        if (result.access_token) { active = false; success.current(result); return; }
        setStatus(String(result.status || "waiting"));
        if (result.status === "expired") return;
      } catch (cause) {
        if (!active) return;
        failures++;
        setError(cause instanceof Error ? cause.message : "连接有点慢，我再等等。也可以重新生成登录码。");
        if (failures >= 3) return;
      } finally {
        window.clearTimeout(requestTimeout);
      }
      if (active) timer = setTimeout(() => void poll(), 1500);
    }
    void poll();
    return () => { active = false; controller?.abort(); clearTimeout(timer); };
  }, [challenge]);

  /** copyCode 复制公众号可直接识别的完整指令，不让用户手工记忆或转录数字。 */
  async function copyCode() {
    if (!challenge) return;
    try { await navigator.clipboard.writeText(`验证码 ${challenge.login_code}`); setCopyText("已复制，发给公众号就行"); }
    catch { setCopyText("没复制成功，可以长按上面的文字复制"); }
  }

  return (
    <Flex vertical align="center" gap={16} className={styles.login}>
      <Typography.Text strong style={{ fontSize: 18 }}>微信扫码登录</Typography.Text>
      {loading ? <div className={styles.qr}><Spin description="正在准备登录码" /></div> : challenge ? (
        <div className={styles.qr}>
          <Image width={208} height={208} src={challenge.qr_image_url} alt="GoodHR 公众号二维码，请用微信扫码" preview={false} />
          {status === "expired" || remaining === 0 ? <div className={styles.expired}><Typography.Text strong>登录码过期了</Typography.Text><Button type="primary" onClick={() => setGeneration(value => value + 1)}>重新生成</Button></div> : null}
        </div>
      ) : null}
      {challenge ? <Flex vertical align="center" gap={8}>
        <Typography.Text>扫码关注后，给公众号发送</Typography.Text>
        <Typography.Text className={styles.code} style={{ fontSize: "clamp(20px, 5vw, 24px)", fontWeight: 700 }}>验证码 {challenge.login_code}</Typography.Text>
        <Button onClick={() => void copyCode()} disabled={status === "expired" || remaining === 0}>{copyText}</Button>
      </Flex> : null}
      <div role="status" aria-live="polite" className={styles.status}>
        {status === "agreement_required" ? <Button type="link" onClick={onAgreement}>微信已确认，读完并同意协议就能进入</Button> : challenge && status !== "expired" && remaining > 0 ? <><Spin size="small" /> 正在等你发验证码 · {Math.floor(remaining / 60)}:{String(remaining % 60).padStart(2, "0")}</> : null}
      </div>
      {error ? <Alert type="warning" showIcon title={error} action={<Button size="small" onClick={() => setGeneration(value => value + 1)}>重试</Button>} /> : null}
      {challenge && status !== "expired" ? <Button type="text" onClick={() => { setCopyText("复制验证码"); setGeneration(value => value + 1); }}>重新生成登录码</Button> : null}
      <Typography.Paragraph type="secondary" className={styles.hint}>首次使用会自动注册，无需邮箱和密码。已绑定公众号的邮箱账号会继续使用原账号；还没绑定的，请先用邮箱登录。</Typography.Paragraph>
    </Flex>
  );
}
