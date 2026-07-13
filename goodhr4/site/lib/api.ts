import { API_BASE } from "@/lib/constants";

export type SiteConfig = {
  announcement?: string;
  website_url?: string;
  contact_url?: string;
  donate_url?: string;
  share_url?: string;
  [key: string]: unknown;
};

export type UpdateRecord = {
  id: number;
  version: string;
  title: string;
  content: string;
  force_update: boolean;
  published_at: string;
};

export type SiteBootstrap = {
  config: SiteConfig;
  updates: UpdateRecord[];
};

export type RegisterPayload = {
  identifier?: string;
  email?: string;
  phone?: string;
  inviter_id?: number;
};

export type RegisterResult = {
  user: {
    id: number;
    email?: string;
    phone?: string;
    identifier: string;
    role: string;
    inviter_id?: number;
    balance?: number;
  };
  is_new_user: boolean;
};

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers || {}),
    },
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error((data as { error?: string }).error || `HTTP ${res.status}`);
  }
  return data as T;
}

export async function fetchBootstrap(): Promise<SiteBootstrap> {
  const data = await request<{ code: number; data: SiteBootstrap }>(
    "/api/v1/site/bootstrap",
    { method: "GET" },
  );
  return data.data;
}

export async function fetchUpdates(): Promise<UpdateRecord[]> {
  const data = await request<{ code: number; data: { items: UpdateRecord[] } }>(
    "/api/v1/site/updates",
    { method: "GET" },
  );
  return data.data.items || [];
}

export async function registerSiteUser(
  payload: RegisterPayload,
): Promise<RegisterResult> {
  const data = await request<{ code: number; data: RegisterResult }>(
    "/api/v1/site/register",
    {
      method: "POST",
      body: JSON.stringify(payload),
      cache: "no-store",
    },
  );
  return data.data;
}
