// 本文件负责 GoodHR 后台菜单页面路由配置。
import { createRouter, createWebHistory, type RouteLocationNormalized } from "vue-router";
import DashboardView from "../views/DashboardView.vue";
import AccountView from "../views/AccountView.vue";
import PositionView from "../views/PositionView.vue";
import TaskListView from "../views/TaskListView.vue";
import ResumeLibraryView from "../views/ResumeLibraryView.vue";
import ResumeDetailView from "../views/ResumeDetailView.vue";
import TenantView from "../views/TenantView.vue";
import InvitationView from "../views/InvitationView.vue";
import PersonalConfigView from "../views/PersonalConfigView.vue";
import SubscriptionView from "../views/SubscriptionView.vue";
import HelpView from "../views/HelpView.vue";
import UserManagementView from "../views/admin/UserManagementView.vue";
import ActivationCodeView from "../views/admin/ActivationCodeView.vue";
import PaymentRecordsView from "../views/admin/PaymentRecordsView.vue";
import SystemConfigView from "../views/admin/SystemConfigView.vue";

export const MENU_CACHE_KEY = "goodhr5_active_menu";

export const menuRouteMap: Record<string, string> = {
  agent: "dashboard",
  account: "accounts",
  position: "positions",
  "task-list": "tasks",
  "resume-library": "resumes",
  tenant: "team",
  invitation: "invitations",
  "personal-config": "personal-config",
  subscription: "subscription",
  help: "help",
  "user-management": "users",
  "activation-codes": "activation-codes",
  "payment-records": "payment-records",
  "system-config": "system-config",
};

export const router = createRouter({
  history: createWebHistory("/admin/"),
  routes: [
    { path: "/", name: "dashboard", component: DashboardView, meta: { menuId: "agent" } },
    { path: "/accounts", name: "accounts", component: AccountView, meta: { menuId: "account" } },
    { path: "/positions", name: "positions", component: PositionView, meta: { menuId: "position" } },
    { path: "/tasks", name: "tasks", component: TaskListView, meta: { menuId: "task-list" } },
    { path: "/resumes", name: "resumes", component: ResumeLibraryView, meta: { menuId: "resume-library" } },
    { path: "/resumes/detail", name: "resume-detail", component: ResumeDetailView, meta: { menuId: "resume-library" } },
    { path: "/team", name: "team", component: TenantView, meta: { menuId: "tenant" } },
    { path: "/invitations", name: "invitations", component: InvitationView, meta: { menuId: "invitation" } },
    { path: "/personal-config", name: "personal-config", component: PersonalConfigView, meta: { menuId: "personal-config" } },
    { path: "/subscription", name: "subscription", component: SubscriptionView, meta: { menuId: "subscription" } },
    { path: "/help", name: "help", component: HelpView, meta: { menuId: "help" } },
    { path: "/users", name: "users", component: UserManagementView, meta: { menuId: "user-management", superAdmin: true } },
    { path: "/activation-codes", name: "activation-codes", component: ActivationCodeView, meta: { menuId: "activation-codes", superAdmin: true } },
    { path: "/payment-records", name: "payment-records", component: PaymentRecordsView, meta: { menuId: "payment-records", superAdmin: true } },
    { path: "/system-config", name: "system-config", component: SystemConfigView, meta: { menuId: "system-config", superAdmin: true } },
  ],
});

let savedMenuApplied = false;

router.beforeEach((to) => normalizeLegacyRoute(to));

/**
 * 把旧的 menu 查询参数和旧缓存转换为新路由。
 * @param {RouteLocationNormalized} to - 即将进入的路由。
 * @returns {any} 返回重定向位置或 undefined。
 */
function normalizeLegacyRoute(to: RouteLocationNormalized) {
  if (to.query.candidate_id && to.name !== "resume-detail") {
    return { name: "resume-detail", query: { candidate_id: to.query.candidate_id } };
  }
  if (to.query.menu === "resume-detail") {
    return { name: "resume-detail", query: pickQuery(to.query, ["candidate_id"]) };
  }
  if ((to.query.menu === "resume-library" || to.query.task_id) && to.name !== "resumes") {
    return { name: "resumes", query: pickQuery(to.query, ["task_id"]) };
  }
  const menu = typeof to.query.menu === "string" ? to.query.menu : "";
  if (menu && menuRouteMap[menu]) {
    return { name: menuRouteMap[menu] };
  }
  if (!savedMenuApplied && to.path === "/" && Object.keys(to.query).length === 0) {
    savedMenuApplied = true;
    const savedMenu = localStorage.getItem(MENU_CACHE_KEY) || "";
    if (savedMenu && savedMenu !== "agent" && menuRouteMap[savedMenu]) {
      return { name: menuRouteMap[savedMenu] };
    }
  }
  return undefined;
}

/**
 * 从旧链接中保留指定查询参数。
 * @param {Record<string, any>} query - 原始查询参数。
 * @param {string[]} keys - 需要保留的参数名。
 * @returns {Record<string, any>} 返回新的查询参数。
 */
function pickQuery(query: Record<string, any>, keys: string[]) {
  return keys.reduce<Record<string, any>>((nextQuery, key) => {
    if (query[key]) nextQuery[key] = query[key];
    return nextQuery;
  }, {});
}
