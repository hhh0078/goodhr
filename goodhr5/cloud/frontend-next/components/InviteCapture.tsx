/** 本文件负责缓存官网 URL 中的邀请码，不发起任何网络请求。 */
"use client";

import { useEffect } from "react";
import { INVITE_CACHE_KEY } from "@/lib/api";

/** InviteCapture 将当前链接中的 invite 参数保存到浏览器本地。 */
export default function InviteCapture() {
  useEffect(() => {
    const inviteID = new URLSearchParams(window.location.search).get("invite")?.trim() || "";
    if (inviteID) localStorage.setItem(INVITE_CACHE_KEY, inviteID);
  }, []);
  return null;
}
