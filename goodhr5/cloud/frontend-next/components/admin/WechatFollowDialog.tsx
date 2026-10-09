/** 本文件展示可取消的公众号关注与绑定码提醒，轮询本人状态并让位于所有业务弹框。 */
"use client";
import { Alert, Flex, Image, Input, Spin, Tag, Typography } from "antd";
import { useCallback, useEffect, useRef, useState } from "react";
import { cloudRequest } from "@/lib/admin-api";
import {
  normalizeWechatStatus,
  officialAccountQRURL,
  readableImageURL,
  type WechatOfficialStatus,
} from "@/lib/wechat-official";
import AdminDialog from "./AdminDialog";
import { useAdminOverlayBusy } from "./AdminOverlay";

type BindingCode = { code: string; expiresAt: number };
type FollowUser = {
  email?: string;
  has_openid?: boolean;
  wechat_follow_gift_granted?: boolean;
} | null;
type FollowProps = {
  user: FollowUser;
  status: WechatOfficialStatus | null;
  autoAllowed: boolean;
  openSignal: number;
  onStatus: (status: WechatOfficialStatus) => void;
  refreshSession: () => Promise<void>;
  notify: (
    message: string,
    severity?: "success" | "error" | "warning" | "info",
  ) => void;
};

/** followDismissKey 按登录邮箱隔离本会话的关闭记录，不影响其他用户或正常招聘。 */
function followDismissKey(email: string) {
  return `goodhr5_wechat_follow_dismissed:${email}`;
}

/** readDismissedFollow 读取本会话关闭记录，浏览器禁用存储时仍可正常打开界面。 */
function readDismissedFollow(email: string) {
  try {
    return Boolean(email && sessionStorage.getItem(followDismissKey(email)));
  } catch {
    return false;
  }
}

/** formatBindingRemaining 将有效期显示为分秒，避免用户心算长串秒数。 */
function formatBindingRemaining(seconds: number) {
  return `${Math.floor(seconds / 60)} 分 ${String(seconds % 60).padStart(2, "0")} 秒`;
}

/** WechatFollowDialog 生成一次性绑定码，关注后的消息由云端验证，前端不直接发放会员。 */
export default function WechatFollowDialog({
  user,
  status,
  autoAllowed,
  openSignal,
  onStatus,
  refreshSession,
  notify,
}: FollowProps) {
  const email = String(user?.email || "");
  const busy = useAdminOverlayBusy();
  const [dismissed, setDismissed] = useState(true);
  const [manualRequested, setManualRequested] = useState(false);
  const [binding, setBinding] = useState<BindingCode | null>(null);
  const [generating, setGenerating] = useState(false);
  const [error, setError] = useState("");
  const [qrFailed, setQRFailed] = useState(false);
  const [now, setNow] = useState(Date.now());
  const requestRef = useRef<AbortController | null>(null);
  const creationAttemptedRef = useRef(false);
  const handledBoundRef = useRef(false);
  const giftBeforeBindingRef = useRef(false);
  const openSignalRef = useRef(openSignal);
  const mountedRef = useRef(false);
  const visibleRef = useRef(false);
  const allowed = Boolean(status?.enabled && status.configured);
  const automaticRequested = Boolean(
    email &&
      user?.has_openid === false &&
      allowed &&
      !status?.bound &&
      !dismissed,
  );
  const visible = Boolean(
    email && !busy && autoAllowed && (manualRequested || automaticRequested),
  );
  visibleRef.current = visible;
  const remaining = Math.max(
    0,
    Math.ceil(((binding?.expiresAt || 0) - now) / 1000),
  );
  const qrURL =
    readableImageURL(status?.qr_image_url || "") ||
    officialAccountQRURL(status?.original_id || "");
  const alreadyGifted = Boolean(
    status?.gift_granted || user?.wechat_follow_gift_granted,
  );
  const rewardDays = status?.reward_days || 2;

  useEffect(() => setQRFailed(false), [qrURL]);
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      requestRef.current?.abort();
      requestRef.current = null;
    };
  }, []);
  useEffect(() => {
    setDismissed(readDismissedFollow(email));
    setManualRequested(false);
    setBinding(null);
    setError("");
    creationAttemptedRef.current = false;
    handledBoundRef.current = false;
  }, [email]);
  useEffect(() => {
    if (openSignalRef.current === openSignal) return;
    openSignalRef.current = openSignal;
    creationAttemptedRef.current = false;
    handledBoundRef.current = false;
    setBinding(null);
    setError("");
    setManualRequested(true);
  }, [openSignal]);
  useEffect(() => {
    if (visible) {
      setNow(Date.now());
      const timer = window.setInterval(() => setNow(Date.now()), 1000);
      return () => window.clearInterval(timer);
    }
    requestRef.current?.abort();
    requestRef.current = null;
    setGenerating(false);
    if (!binding) creationAttemptedRef.current = false;
  }, [visible, binding]);

  /** closeFollow 允许随时暂不关注，并记住本用户本会话的选择。 */
  function closeFollow() {
    requestRef.current?.abort();
    requestRef.current = null;
    creationAttemptedRef.current = false;
    setGenerating(false);
    setManualRequested(false);
    setDismissed(true);
    setBinding(null);
    setError("");
    try {
      sessionStorage.setItem(followDismissKey(email), "1");
    } catch {
      // 本次关闭状态仍保留在内存，浏览器存储不可用也不会阻塞招聘。
    }
  }

  /** finishBinding 只处理一次服务端确认结果，清理轮询后刷新真实会员状态。 */
  const finishBinding = useCallback(
    async (fresh: WechatOfficialStatus) => {
      if (handledBoundRef.current || !mountedRef.current) return;
      handledBoundRef.current = true;
      onStatus(fresh);
      requestRef.current?.abort();
      requestRef.current = null;
      setManualRequested(false);
      setDismissed(true);
      setBinding(null);
      setGenerating(false);
      setError("");
      try {
        await refreshSession();
        if (mountedRef.current)
          notify(
            fresh.gift_granted && !giftBeforeBindingRef.current
              ? `公众号绑定好了，${fresh.gift_days || fresh.reward_days} 天会员已到账。继续搬砖吧。`
              : "公众号绑定好了，可以继续搬砖了。",
            "success",
          );
      } catch {
        if (mountedRef.current)
          notify("绑定成功了，会员状态暂时没同步，稍后刷新一下", "info");
      }
    },
    [notify, onStatus, refreshSession],
  );

  /** generateBinding 串行生成新码；新码会替换旧码，取消后迟到响应不得更新弹框。 */
  const generateBinding = useCallback(async () => {
    if (requestRef.current || !visibleRef.current) return;
    const controller = new AbortController();
    requestRef.current = controller;
    creationAttemptedRef.current = true;
    setGenerating(true);
    setError("");
    const timeout = window.setTimeout(() => controller.abort(), 10000);
    try {
      const current = normalizeWechatStatus(
        await cloudRequest("/api/wechat-official/status", {
          signal: controller.signal,
        }),
      );
      if (
        controller.signal.aborted ||
        !mountedRef.current ||
        !visibleRef.current
      )
        return;
      onStatus(current);
      if (current.bound) {
        giftBeforeBindingRef.current = true;
        await finishBinding(current);
        return;
      }
      if (!current.enabled || !current.configured)
        throw new Error("公众号还没准备好，晚点再来就行");
      giftBeforeBindingRef.current = Boolean(
        current.gift_granted || user?.wechat_follow_gift_granted,
      );
      const result = await cloudRequest("/api/wechat-official/binding", {
        method: "POST",
        signal: controller.signal,
      });
      if (
        controller.signal.aborted ||
        !mountedRef.current ||
        !visibleRef.current
      )
        return;
      if (result.status && typeof result.status === "object") {
        const nextStatus = normalizeWechatStatus(result.status);
        if (nextStatus.bound) {
          await finishBinding(nextStatus);
          return;
        }
        onStatus(nextStatus);
      }
      const code = String(result.binding_code || "");
      const expiresAt = Date.parse(String(result.expires_at || ""));
      if (
        !/^(?:\d{4}|[A-Z0-9]{8})$/.test(code) ||
        !Number.isFinite(expiresAt) ||
        expiresAt <= Date.now()
      )
        throw new Error("绑定码没拿完整，再试一次就好");
      setBinding({ code, expiresAt });
      setNow(Date.now());
    } catch (requestError) {
      if (
        requestRef.current === controller &&
        mountedRef.current &&
        visibleRef.current
      ) {
        setError(
          controller.signal.aborted
            ? "绑定码暂时没拿到，点重新生成再试一次"
            : requestError instanceof Error
              ? requestError.message
              : "绑定码没生成成功，再试一次就好",
        );
      }
    } finally {
      window.clearTimeout(timeout);
      if (requestRef.current === controller) {
        requestRef.current = null;
        if (mountedRef.current) setGenerating(false);
      }
    }
  }, [finishBinding, onStatus, user?.wechat_follow_gift_granted]);

  useEffect(() => {
    if (
      !visible ||
      binding ||
      creationAttemptedRef.current ||
      status?.bound ||
      !allowed
    )
      return;
    // 下一轮再生成，让 React 开发模式的挂载检查先完成，避免创建两次使第一份码立即失效。
    const timer = window.setTimeout(() => void generateBinding(), 0);
    return () => window.clearTimeout(timer);
  }, [allowed, binding, generateBinding, status?.bound, visible]);

  useEffect(() => {
    if (!visible || !allowed || !binding || remaining <= 0 || status?.bound)
      return;
    let active = true;
    let timer: number | undefined;
    let controller: AbortController | undefined;
    /** checkBinding 串行查询本人绑定状态，关闭、过期或绑定成功后不再继续请求。 */
    async function checkBinding() {
      controller = new AbortController();
      const timeout = window.setTimeout(() => controller?.abort(), 10000);
      let continuePolling = true;
      try {
        const fresh = normalizeWechatStatus(
          await cloudRequest("/api/wechat-official/status", {
            signal: controller.signal,
          }),
        );
        if (!active || !mountedRef.current) return;
        onStatus(fresh);
        setError("");
        if (fresh.bound) {
          continuePolling = false;
          await finishBinding(fresh);
        }
      } catch (requestError) {
        if (active && mountedRef.current)
          setError(
            requestError instanceof Error
              ? requestError.message
              : "暂时没查到绑定结果，我再试一次",
          );
      } finally {
        window.clearTimeout(timeout);
        if (active && continuePolling && Date.now() < binding!.expiresAt)
          timer = window.setTimeout(() => void checkBinding(), 2000);
      }
    }
    timer = window.setTimeout(() => void checkBinding(), 1000);
    return () => {
      active = false;
      if (timer !== undefined) window.clearTimeout(timer);
      controller?.abort();
    };
  }, [
    allowed,
    binding,
    finishBinding,
    onStatus,
    remaining > 0,
    status?.bound,
    visible,
  ]);

  return (
    <AdminDialog
      priority="promotion"
      open={visible}
      title={
        <Typography.Text
          style={{ color: "#cf1322", fontSize: 20, fontWeight: 800 }}
        >
          {alreadyGifted ? "关注公众号" : `关注领取 ${rewardDays} 天会员`}
        </Typography.Text>
      }
      cancelText="暂不关注"
      confirmText="重新生成"
      confirmLoading={generating}
      confirmDisabled={!allowed || status?.bound}
      hideClose
      maxWidth="xs"
      onClose={closeFollow}
      onConfirm={() => void generateBinding()}
    >
      <Flex vertical gap={10} align="stretch">
        {qrURL && !qrFailed ? (
          <Flex justify="center">
            <Image
              src={qrURL}
              alt="公众号关注二维码"
              width={180}
              height={180}
              preview={false}
              style={{ objectFit: "contain", maxWidth: "100%" }}
              onError={() => {
                setQRFailed(true);
                return false;
              }}
            />
          </Flex>
        ) : (
          <Alert
            type="info"
            title={
              qrFailed
                ? `二维码图片暂时没加载，可以在微信搜索“${status?.account_name || "GoodHR"}”。`
                : "关注二维码还没配好，晚点再来就行"
            }
          />
        )}
        {!allowed ? (
          <Alert type="info" title="公众号暂未开启，先继续招聘就好" />
        ) : status?.bound ? (
          <Tag color="success">公众号已绑定</Tag>
        ) : (
          <Flex vertical gap={8}>
            <Typography.Text
              style={{
                color: "#cf1322",
                fontSize: 19,
                fontWeight: 800,
                lineHeight: 1.5,
                textAlign: "center",
              }}
            >
              先扫码关注，再把下面的绑定码发给公众号。
            </Typography.Text>
            {generating ? (
              <Flex gap={8} align="center">
                <Spin size="small" />
                <Typography.Text type="secondary">
                  正在准备绑定码，暂不关注也能关掉。
                </Typography.Text>
              </Flex>
            ) : null}
            {binding ? (
              <>
                <Input
                  aria-label="公众号绑定码"
                  value={binding.code}
                  readOnly
                  variant="filled"
                  style={{
                    textAlign: "center",
                    fontFamily: "monospace",
                    fontSize: binding.code.length === 4 ? 42 : 32,
                    fontWeight: 800,
                    letterSpacing: binding.code.length === 4 ? 8 : 3,
                    minWidth: 0,
                    paddingBlock: 4,
                  }}
                />
                <Typography.Text
                  type={remaining > 0 ? "secondary" : "warning"}
                  style={{ textAlign: "center", fontSize: 12 }}
                >
                  {remaining > 0
                    ? `绑定码剩余 ${formatBindingRemaining(remaining)}`
                    : "绑定码已过期，重新生成一份就好。"}
                </Typography.Text>
              </>
            ) : null}
            <Typography.Text
              type="secondary"
              style={{ fontSize: 12, textAlign: "center" }}
            >
              之前已关注？取消后重新关注，再发绑定码。
            </Typography.Text>
          </Flex>
        )}
        {error ? <Alert type="warning" showIcon title={error} /> : null}
      </Flex>
    </AdminDialog>
  );
}
