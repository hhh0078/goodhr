/** 云端认证逻辑 */
import { ref } from "vue";
import { ApiError, cloudApiBase, getAccessToken, setAccessToken } from "../services/apiClient";
import { currentUser, loginByCode, sendLoginCode } from "../services/cloudApi";

const INVITE_CACHE_KEY = "goodhr5_invite_id";

export function useAuth() {
  const email = ref("");
  const code = ref("");
  const devCode = ref("");
  const token = ref(getAccessToken());
  const user = ref(null);
  const error = ref("");
  const loading = ref(false);
  const inviterID = ref(readInviteID());

  async function sendCode() {
    loading.value = true;
    error.value = "";
    devCode.value = "";
    try {
      const data = await sendLoginCode(email.value);
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
    try {
      const data = await loginByCode(email.value, code.value, inviterID.value);
      token.value = data.access_token;
      setAccessToken(data.access_token);
      user.value = data.user;
    } catch (e) {
      error.value = e.message;
    } finally {
      loading.value = false;
    }
  }

  async function loadCurrentUser() {
    if (!token.value) return;
    for (let i = 0; i < 3; i += 1) {
      try {
        user.value = await currentUser();
        return;
      } catch (e: any) {
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

  return {
    email,
    code,
    devCode,
    token,
    user,
    error,
    loading,
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
