/** 云端认证逻辑 */
import { ref } from "vue";
import { cloudApiBase, getAccessToken, setAccessToken } from "../services/apiClient";
import { currentUser, loginByCode, sendLoginCode } from "../services/cloudApi";

export function useAuth() {
  const email = ref("");
  const code = ref("");
  const devCode = ref("");
  const token = ref(getAccessToken());
  const user = ref(null);
  const error = ref("");
  const loading = ref(false);

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
      const data = await loginByCode(email.value, code.value);
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
    try {
      user.value = await currentUser();
    } catch {
      logout();
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
    sendCode,
    login,
    loadCurrentUser,
    logout,
    CLOUD_API_BASE: cloudApiBase(),
  };
}
