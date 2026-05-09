import { getApiBase, getApiRequest, storageGet, storageSet } from "../lib/chrome.js";

export async function fetchRankingData() {
  const response = await fetch(`${getApiBase()}/dashang.json?t=${Date.now()}`);
  return response.json();
}

export async function fetchServerSettings(phone) {
  const response = await fetch(
    `${getApiBase()}/getjson.php?phone=${encodeURIComponent(phone)}`,
  );
  if (!response.ok) {
    throw new Error("获取配置失败");
  }
  return response.json();
}

export async function updateServerSettings(phone, payload) {
  const response = await fetch(
    `${getApiBase()}/updatejson.php?phone=${encodeURIComponent(phone)}`,
    {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(payload, null, 2),
    },
  );

  if (!response.ok) {
    throw new Error(`服务器响应错误: ${response.status}`);
  }

  const result = await response.json();
  if (result.status !== "success" && result.message !== "配置已保存") {
    throw new Error(result.message || "更新服务器数据失败");
  }
  return result;
}

export async function createAiAccount(phone) {
  const apiRequest = getApiRequest();
  if (!apiRequest) {
    throw new Error("API工具未加载");
  }
  return apiRequest.get("https://siliconflow.a.58it.cn/api/register.php", {
    phone,
  });
}

export async function checkAiBalance(phone) {
  const apiRequest = getApiRequest();
  if (!apiRequest) {
    throw new Error("API工具未加载");
  }
  return apiRequest.get("https://siliconflow.a.58it.cn/api/register.php", {
    phone,
  });
}

export async function fetchAiModels(phone) {
  const response = await fetch(
    `https://siliconflow.a.58it.cn/api/user.php?action=setModel&phone=${encodeURIComponent(phone)}`,
  );
  return response.json();
}

export async function saveAiModel(phone, model) {
  const response = await fetch(
    `https://siliconflow.a.58it.cn/api/user.php?action=setModel&phone=${encodeURIComponent(phone)}&model=${encodeURIComponent(model)}`,
  );
  return response;
}

export async function saveBoundPhone(phone) {
  await storageSet({ hr_assistant_phone: phone });
}

export async function loadBootstrapStorage(storageKey) {
  return storageGet([
    storageKey,
    "hr_assistant_phone",
    "hr_assistant_settings",
    "ai_expire_time",
    "ai_config",
    "selected_tab",
  ]);
}

