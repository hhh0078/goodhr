/** 云端认证逻辑 */
import { computed, ref } from "vue";
import { ApiError, cloudApiBase, getAccessToken, setAccessToken } from "../services/apiClient";
import { currentUser, loginByCode, sendLoginCode } from "../services/api/authApi";

const INVITE_CACHE_KEY = "goodhr5_invite_id";

export function useAuth() {
  const email = ref("");
  const code = ref("");
  const devCode = ref("");
  const token = ref(getAccessToken());
  const user = ref(null);
  const error = ref("");
  const message = ref("");
  const loading = ref(false);
  const sendCodeCooldown = ref(0);
  let sendCodeTimer = 0;
  const inviterID = ref(readInviteID());
  const canSendCode = computed(() => !loading.value && !!email.value && sendCodeCooldown.value <= 0);

  async function sendCode() {
    if (!canSendCode.value) return;
    loading.value = true;
    error.value = "";
    message.value = "";
    devCode.value = "";
    try {
      const data = await sendLoginCode(email.value);
      message.value = "验证码已发送，请查收邮箱";
      startSendCodeCooldown();
      if (data.debug_code) {
        devCode.value = data.debug_code;
        code.value = data.debug_code;
      }
    } catch (e) {
      error.value = e.message;
    } finally {
      loading.value = false;
    }
  }

  async function login() {
    loading.value = true;
    error.value = "";
    message.value = "";
    const targetEmail = normalizeEmail(email.value);
    token.value = "";
    user.value = null;
    setAccessToken("");
    try {
      const data = await loginByCode(email.value, code.value, inviterID.value);
      const nextToken = data.access_token || "";
      const loginUser = data.user || null;
      assertSameLoginUser(targetEmail, loginUser);
      token.value = nextToken;
      setAccessToken(nextToken);
      const me = await currentUser();
      assertSameLoginUser(targetEmail, me);
      user.value = me;
    } catch (e) {
      token.value = "";
      user.value = null;
      setAccessToken("");
      error.value = e.message;
    } finally {
      loading.value = false;
    }
  }

  async function loadCurrentUser() {
    if (!token.value) return;
    const requestToken = token.value;
    for (let i = 0; i < 3; i += 1) {
      try {
        const me = await currentUser();
        if (token.value !== requestToken) {
          return;
        }
        if (!me?.email) {
          logout();
          return;
        }
        user.value = me;
        return;
      } catch (e: any) {
        if (token.value !== requestToken) {
          return;
        }
        const status = e instanceof ApiError ? e.status : 0;
        if (status === 401 || status === 403) {
          logout();
          return;
        }
        if (i < 2) {
          await delay(1000);
          continue;
        }
        error.value = e?.message || "云端服务暂不可用，请稍后重试";
      }
    }
  }

  function logout() {
    token.value = "";
    user.value = null;
    setAccessToken("");
  }

  /**
   * 启动发送验证码倒计时。
   * @returns {void} 无返回值。
   */
  function startSendCodeCooldown() {
    sendCodeCooldown.value = 30;
    if (sendCodeTimer) window.clearInterval(sendCodeTimer);
    sendCodeTimer = window.setInterval(() => {
      sendCodeCooldown.value -= 1;
      if (sendCodeCooldown.value <= 0) {
        sendCodeCooldown.value = 0;
        window.clearInterval(sendCodeTimer);
        sendCodeTimer = 0;
      }
    }, 1000);
  }

  return {
    email,
    code,
    devCode,
    token,
    user,
    error,
    message,
    loading,
    sendCodeCooldown,
    canSendCode,
    inviterID,
    sendCode,
    login,
    loadCurrentUser,
    logout,
    CLOUD_API_BASE: cloudApiBase(),
  };
}

/**
 * 从当前链接中读取邀请人 ID。
 * @returns {string} 邀请人 ID。
 */
function readInviteID() {
  const params = new URLSearchParams(window.location.search);
  const inviteID = params.get("invite") || "";
  if (inviteID) {
    localStorage.setItem(INVITE_CACHE_KEY, inviteID);
    return inviteID;
  }
  return localStorage.getItem(INVITE_CACHE_KEY) || "";
}

function delay(ms: number) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

/**
 * 标准化登录邮箱，避免大小写和空格导致前端校验误判。
 * @param {string} value - 用户输入的邮箱。
 * @returns {string} 标准化后的邮箱。
 */
function normalizeEmail(value: string) {
  return String(value || "").trim().toLowerCase();
}

/**
 * 校验登录接口返回的用户是否就是当前输入邮箱。
 * @param {string} expectedEmail - 当前输入的登录邮箱。
 * @param {any} loginUser - 接口返回的用户对象。
 * @returns {void} 无返回值。
 */
function assertSameLoginUser(expectedEmail: string, loginUser: any) {
  const actualEmail = normalizeEmail(loginUser?.email || "");
  if (!expectedEmail || !actualEmail || actualEmail !== expectedEmail) {
    throw new Error("登录状态异常，请退出后重新登录");
  }
}
