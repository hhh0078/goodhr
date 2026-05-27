"use client";

import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "next/navigation";
import {
  IDENTIFIER_STORAGE_KEY,
  INVITE_STORAGE_KEY,
} from "@/lib/constants";
import { registerSiteUser } from "@/lib/api";

function parseInvite(value: string | null): number | null {
  if (!value) return null;
  const num = Number(value);
  if (!Number.isFinite(num) || num <= 0) return null;
  return Math.trunc(num);
}

export function RegisterCard() {
  const searchParams = useSearchParams();
  const [identifier, setIdentifier] = useState("");
  const [inviteId, setInviteId] = useState<number | null>(null);
  const [loading, setLoading] = useState(false);
  const [message, setMessage] = useState("");

  const inviteFromQuery = useMemo(
    () => parseInvite(searchParams.get("invite")),
    [searchParams],
  );

  useEffect(() => {
    const storedIdentifier = localStorage.getItem(IDENTIFIER_STORAGE_KEY) || "";
    if (storedIdentifier) {
      setIdentifier(storedIdentifier);
    }

    const storedInvite = parseInvite(localStorage.getItem(INVITE_STORAGE_KEY));
    const finalInvite = inviteFromQuery ?? storedInvite;
    setInviteId(finalInvite);

    if (inviteFromQuery) {
      localStorage.setItem(INVITE_STORAGE_KEY, String(inviteFromQuery));
      document.cookie = `goodhr_invite_id=${inviteFromQuery}; Path=/; Max-Age=${60 * 60 * 24 * 30}; SameSite=Lax`;
    }
  }, [inviteFromQuery]);

  async function submit() {
    const trimmed = identifier.trim();
    if (!trimmed) {
      setMessage("请先输入邮箱或手机号");
      return;
    }

    setLoading(true);
    setMessage("");
    try {
      const result = await registerSiteUser({
        identifier: trimmed,
        inviter_id: inviteId ?? undefined,
      });
      localStorage.setItem(IDENTIFIER_STORAGE_KEY, result.user.identifier);
      setIdentifier(result.user.identifier);
      setMessage(result.is_new_user ? "注册成功，信息已保存" : "登录成功，信息已同步");
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "请求失败");
    } finally {
      setLoading(false);
    }
  }

  return (
    <section className="register-card">
      <h2>立即体验</h2>
      <p>
        输入邮箱或手机号即可完成注册/登录。若通过邀请链接进入，会自动绑定邀请关系。
      </p>
      {inviteId ? (
        <p className="invite-tip">当前邀请码：{inviteId}</p>
      ) : (
        <p className="invite-tip">当前未携带邀请码</p>
      )}
      <div className="register-form">
        <input
          type="text"
          value={identifier}
          onChange={(e) => setIdentifier(e.target.value)}
          placeholder="邮箱或手机号"
        />
        <button type="button" onClick={submit} disabled={loading}>
          {loading ? "提交中..." : "注册 / 登录"}
        </button>
      </div>
      {message ? <p className="register-msg">{message}</p> : null}
    </section>
  );
}
