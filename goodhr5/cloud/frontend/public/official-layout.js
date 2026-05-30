// 本文件负责 GoodHR 静态官网公共导航、底部和邀请码缓存逻辑。
(function initOfficialLayout() {
  var navItems = [
    { href: "/", label: "首页", match: ["", "index.html"] },
    { href: "/features.html", label: "功能介绍", match: ["features.html"] },
    { href: "/videos.html", label: "安装视频教程", match: ["videos.html"] },
    { href: "/pricing.html", label: "产品定价", match: ["pricing.html"] },
    { href: "/contact.html", label: "联系我们", match: ["contact.html"] },
  ];

  cacheInviteID();
  renderHeader(navItems);
  renderFooter();
})();

/**
 * 缓存邀请人 ID，方便进入后台登录时自动带上。
 * @returns {void} 无返回值。
 */
function cacheInviteID() {
  var params = new URLSearchParams(window.location.search);
  var inviteID = params.get("invite");
  if (!inviteID) return;
  localStorage.setItem("goodhr5_invite_id", inviteID);
}

/**
 * 渲染官网公共导航栏。
 * @param {{ href: string; label: string; match: string[] }[]} navItems - 导航配置。
 * @returns {void} 无返回值。
 */
function renderHeader(navItems) {
  var target = document.querySelector("[data-site-header]");
  if (!target) return;
  var current = currentPageName();
  target.outerHTML =
    '<header class="site-header">' +
    '<a class="brand" href="/">GoodHR</a>' +
    "<nav>" +
    navItems
      .map(function (item) {
        var active = item.match.indexOf(current) >= 0 ? ' class="active"' : "";
        return '<a' + active + ' href="' + item.href + '">' + item.label + "</a>";
      })
      .join("") +
    "</nav>" +
    '<a class="admin-link" href="/admin/">进入后台</a>' +
    "</header>";
}

/**
 * 渲染官网公共底部。
 * @returns {void} 无返回值。
 */
function renderFooter() {
  var target = document.querySelector("[data-site-footer]");
  if (!target) return;
  target.outerHTML =
    '<footer class="site-footer">' +
    "<span>GoodHR 招聘自动化工具</span>" +
    "<span>联系：17607080935</span>" +
    "</footer>";
}

/**
 * 返回当前页面文件名。
 * @returns {string} 当前页面文件名。
 */
function currentPageName() {
  var pathname = window.location.pathname.replace(/\/+$/, "");
  var name = pathname.split("/").pop() || "";
  return name;
}
