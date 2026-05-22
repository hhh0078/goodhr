<template>
  <section class="panel">
    <div class="panel-header">
      <div>
        <h2>平台账号</h2>
        <div class="top-info">
          > 该功能有助您的团队在不同电脑上切换多个账号，可关闭该功能。
        </div>

        <div class="top-info">
          > cookie 为非对称加密存储在服务器。有且仅有您的账号和团队成员可访问,
        </div>
        <div class="top-info">
          > 即使服务器被黑客攻击,也无法获取到您的cookie。除非黑客攻击了您的电脑
        </div>
        <div class="top-info">
          > 如果您还想使用该功能，且依旧担心安全问题，可考虑联系作者私有化部署。
        </div>
      </div>
      <div style="display: flex; gap: 8px">
        <button v-if="!showForm" class="ghost" @click="showForm = true">
          + 新增</button
        ><button v-else class="ghost" @click="showForm = false">收起</button
        ><button class="ghost" @click="load">刷新</button>
      </div>
    </div>
    <p v-if="!token" class="hint">需要登录</p>
    <template v-if="showForm && token"
      ><div class="form-grid">
        <label
          >平台<select v-model="form.platformId">
            <option value="boss">Boss直聘</option>
            <option value="zhaopin">智联招聘</option>
            <option value="liepin">猎聘</option>
          </select></label
        >
        <label v-if="pendingCookies"
          >名称<input v-model="form.displayName" placeholder="我的Boss"
        /></label>
      </div>
      <p v-if="msg" :class="msgType">{{ msg }}</p>
      <div class="actions">
        <button
          :disabled="loading || (pendingCookies && !form.displayName)"
          @click="create"
        >
          {{
            loading
              ? "处理中..."
              : pendingCookies
                ? "保存账号"
                : "登录并获取Cookie"
          }}
        </button>
      </div></template
    >
    <p v-if="accounts.length === 0" class="hint">暂无账号</p>
    <div v-else class="card-list" style="margin-top: 8px">
      <article v-for="a in accounts" :key="a.id" class="card">
        <div>
          <strong>{{ a.display_name || a.id }}</strong>
          <p class="card-meta">
            {{ a.platform_id }} | cookie:{{
              a.status === "available" ? "已登录" : "未登录"
            }}
            | 最近时间:{{ formatLocalTime(a.updated_at) }}
          </p>
        </div>
        <div class="account-actions">
          <button
            class="ghost"
            :disabled="loading || openingAccountId === a.id"
            @click="openWithCookie(a)"
          >
            {{ openingAccountId === a.id ? "打开中..." : "打开" }}
          </button>
          <button
            class="ghost"
            :disabled="loading || refreshingAccountId === a.id"
            @click="refreshCookie(a)"
          >
            {{ refreshingAccountId === a.id ? "登录中..." : "重新登录" }}
          </button>
          <button class="ghost danger" @click="del(a)">删除</button>
        </div>
      </article>
    </div>
  </section>
</template>
<script setup lang="ts">
import { onMounted, ref, watch } from "vue";
import {
  claimCookie,
  createCookie,
  deletePlatformAccount,
  listPlatformConfigs,
  listPlatformAccounts,
  releaseCookie,
  updateCookie,
} from "../services/cloudApi";
import { cloudApiBase } from "../services/apiClient";
import { getLocalHealth, openPage } from "../services/localAgentApi";
import {
  decryptCookieByAgent,
  pickDecryptPayload,
} from "../services/cookieCrypto";
import { runPlatformLoginFlow } from "../services/platformLoginFlow";

const props = defineProps<{ token: string; agentBaseUrl: string }>();
const accounts = ref<any[]>([]);
const loading = ref(false);
const msg = ref("");
const msgType = ref("error");
const form = ref({ platformId: "boss", displayName: "" });
const showForm = ref(false);
const platformConfigs = ref<any[]>([]);
const pendingCookies = ref<any[] | null>(null);
const refreshingAccountId = ref("");
const openingAccountId = ref("");

/**
 * 将后端时间转换为当前电脑本地时间显示。
 * @param {string} value - 后端返回的时间字符串。
 * @returns {string} 本地时间文本。
 */
function formatLocalTime(value: string) {
  if (!value) return "未更新";
  const source = String(value);
  const normalized = /(?:Z|[+-]\d{2}:?\d{2})$/.test(source)
    ? source
    : `${source}Z`;
  const date = new Date(normalized);
  if (Number.isNaN(date.getTime())) return source.slice(0, 16) || "未更新";
  const pad = (num: number) => String(num).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

async function load() {
  try {
    platformConfigs.value = await listPlatformConfigs();
    const list: any[] = await listPlatformAccounts();
    accounts.value = list;
  } catch {}
}
async function create() {
  loading.value = true;
  msg.value = "";
  try {
    if (!pendingCookies.value) {
      msg.value = "正在检查平台登录状态";
      msgType.value = "success";
      pendingCookies.value = await runPlatformLoginFlow(
        props.agentBaseUrl,
        form.value.platformId,
        platformAuthConfig(form.value.platformId),
        (message) => {
          msg.value = message;
          msgType.value = "success";
        },
      );
      msg.value = "已获取 cookie，请输入账号名称";
      msgType.value = "success";
      return;
    }
    await createCookie({
      platform_id: form.value.platformId,
      display_name: form.value.displayName,
      cookies: pendingCookies.value,
    });
    form.value.displayName = "";
    pendingCookies.value = null;
    msg.value = "创建成功";
    msgType.value = "success";
    await load();
  } catch (e: any) {
    msg.value = e.message;
    msgType.value = "error";
  } finally {
    loading.value = false;
  }
}

/**
 * 为已有平台账号重新执行扫码登录流程，并保存新的 cookie。
 * @param {any} account - 平台账号记录。
 * @returns {Promise<void>} 无返回值。
 */
async function refreshCookie(account: any) {
  if (!account?.id) return;
  refreshingAccountId.value = account.id;
  loading.value = true;
  msg.value = "";
  try {
    msg.value = `正在为 ${account.display_name || account.id} 重新登录`;
    msgType.value = "success";
    const cookies = await runPlatformLoginFlow(
      props.agentBaseUrl,
      account.platform_id,
      platformAuthConfig(account.platform_id),
      (message) => {
        msg.value = message;
        msgType.value = "success";
      },
      {
        userDataDir: account.local_profile_id || account.id,
        cookieSync: {
          cookie_id: account.id,
          platform_id: account.platform_id,
          display_name: account.display_name,
          cloud_api_base: cloudApiBase(),
        },
      },
    );
    msg.value = `已导出 ${cookies.length} 条 cookie，正在更新云端`;
    msgType.value = "success";
    const updated = await updateCookie(account.id, {
      platform_id: account.platform_id,
      display_name: account.display_name,
      cookies,
    });
    msg.value = `cookie 已更新，最近时间 ${formatLocalTime(updated?.updated_at)}`;
    msgType.value = "success";
    await load();
  } catch (e: any) {
    msg.value = e.message;
    msgType.value = "error";
  } finally {
    refreshingAccountId.value = "";
    loading.value = false;
  }
}

/**
 * 使用指定 cookie 账号直接打开平台推荐页。
 * @param {any} account - cookie 账号记录。
 * @returns {Promise<void>} 无返回值。
 */
async function openWithCookie(account: any) {
  if (!account?.id) return;
  if (!props.agentBaseUrl) {
    msg.value = "未检测到本地程序";
    msgType.value = "error";
    return;
  }
  openingAccountId.value = account.id;
  loading.value = true;
  msg.value = "";
  try {
    const authConfig = platformAuthConfig(account.platform_id);
    const targetURL = authConfig.entry_url || authConfig.logged_in_url_prefix;
    if (!targetURL) throw new Error("平台配置缺少入口地址");
    const openPayload: any = {
      url: targetURL,
      persistent: true,
      user_data_dir: account.local_profile_id || account.id,
      headless: false,
      humanize: true,
    };

    let claimed = false;
    try {
      const health = await getLocalHealth(props.agentBaseUrl);
      const machineID = String(health.machine_id || "").trim();
      if (machineID) {
        const claimedPayload = await claimCookie(account.id, {});
        claimed = true;
        const decryptPayload = pickDecryptPayload(claimedPayload, machineID);
        const cookies = await decryptCookieByAgent(
          props.agentBaseUrl,
          decryptPayload,
        );
        if (Array.isArray(cookies) && cookies.length > 0) {
          openPayload.cookies = cookies;
        }
      }
    } catch (e: any) {
      throw new Error(`cookie 解密失败，无法打开账号：${e?.message || e}`);
    } finally {
      if (claimed) {
        try {
          await releaseCookie(account.id);
        } catch (e) {
          console.warn("release cookie claim failed", e);
        }
      }
    }

    await openPage(props.agentBaseUrl, openPayload);
    msg.value = "已打开推荐页";
    msgType.value = "success";
  } catch (e: any) {
    msg.value = e.message;
    msgType.value = "error";
  } finally {
    openingAccountId.value = "";
    loading.value = false;
  }
}

function platformAuthConfig(platformId: string) {
  const item = platformConfigs.value.find(
    (config: any) => config.config_key === `platform.${platformId}`,
  );
  if (!item?.config_value) return {};
  try {
    const parsed = JSON.parse(item.config_value);
    return (
      parsed.auth || {
        entry_url: parsed.pages?.[0]?.url,
        logged_in_url_prefix: parsed.pages?.[0]?.url,
      }
    );
  } catch {
    return {};
  }
}
async function del(a: any) {
  try {
    // 确认删除
    if (!confirm(`确定删除账号 ${a.display_name || a.id} 吗？`)) return;
    // 删除账号
    await deletePlatformAccount(a.id);
  } catch {}
  await load();
}
onMounted(load);
watch(
  () => form.value.platformId,
  () => {
    pendingCookies.value = null;
    msg.value = "";
  },
);
</script>
<style scoped>
.top-info {
  color: #0a0;
  font-size: 12px;
}
.top-info.success {
  color: #0f0;
}
.top-info.warn {
  color: #fa0;
}
.top-info.error {
  color: #f33;
}
.account-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: flex-end;
}
</style>
