// 本文件负责 GoodHR 静态官网的邀请码缓存逻辑。
(function initOfficialLayout() {
  cacheInviteID();
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
