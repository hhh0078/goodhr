// 本文件负责云端登录、验证码和当前用户相关接口。
import { api } from "../apiClient";

/**
 * 发送邮箱登录验证码。
 * @param {string} email - 用户邮箱。
 * @returns {Promise<any>} 返回验证码发送结果。
 */
export async function sendLoginCode(email: string) {
  return api("/api/auth/send-code", { method: "POST", auth: false, body: { email } });
}

/**
 * 使用邮箱验证码登录。
 * @param {string} email - 用户邮箱。
 * @param {string} code - 邮箱验证码。
 * @param {string} inviterID - 邀请人 ID。
 * @returns {Promise<any>} 返回登录 token 和用户信息。
 */
export async function loginByCode(email: string, code: string, inviterID = "") {
  return api("/api/auth/login", {
    method: "POST",
    auth: false,
    body: { email, code, inviter_id: inviterID },
  });
}

/**
 * 读取当前登录用户。
 * @param {string} token - 指定 access_token；为空时使用本地保存的 token。
 * @returns {Promise<any>} 返回当前用户信息。
 */
export async function currentUser(token = "") {
  const data = await api(
    "/api/auth/me",
    token ? { headers: { Authorization: `Bearer ${token}` } } : {},
  );
  return data.user;
}
