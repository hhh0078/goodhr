// 本文件负责 GoodHR 静态官网的邀请码缓存逻辑。
(function cacheInviteID() {
  var params = new URLSearchParams(window.location.search);
  var inviteID = params.get("invite");
  if (!inviteID) return;
  localStorage.setItem("goodhr5_invite_id", inviteID);
})();
