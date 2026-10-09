<!-- 本文件记录公众号关注回复追加作者联系方式的生产发布、微信权限限制及回退入口，不保存凭据。 -->
# 公众号关注回复追加作者信息（2026-10-10）

原关注回复的会员与绑定码提示完整保留。后面另起段落补充：

> 本公众号仅用于重要消息通知。使用中有疑问，请联系作者，微信：17607080935。
> 作者微信二维码：
> https://goodhr.58it.cn/assets/contact/wechat-developer.jpg
> 点开图片后长按识别，添加作者微信。

这是同一条被动文本回复中的新段落，不额外发送第二条图片。一次回调返回一个 XML 回复；未认证个人订阅号不能使用当前仅对认证企业主体开放的客服 API 补发第二条。采用裸 HTTPS 地址，避免将客服接口的锚标签示例视为被动回复保证。原图访问返回 200、JPEG、140,637 字节；实际微信客户端点击和长按识别仍需人工验收。

当前官方资料：[被动回复规则](https://developers.weixin.qq.com/doc/subscription/guide/product/message/Passive_user_reply_message.html)、[客服消息权限](https://developers.weixin.qq.com/doc/subscription/api/customer/message/api_sendcustommessage.html)。两页均在本次通过 HTTP 200 读取后核对。

## 测试与部署

- 新增 3 个顶层测试、20 个场景，覆盖明文、兼容明文/密文、安全密文及动态 2/7 天；原提示严格保留，作者说明与原图 URL 恰好一次。重复通知、停用、取消关注、绑定成功/失败/重复领取均验证，原绑定结果回复不追加作者信息。
- 相关 5 个顶层测试、23 个子测试全部通过；`go test ./...` 和 `go vet ./...` 通过。关注回复为合法 UTF-8，425 字节，小于 2048。
- 镜像源提交 `77e6183899be03a60ccbf523b8942000322e2bab`，源归档 SHA256 `5be7ecda51913b246cffe11ed9f4331a626682f20e85976ca7b276a82dd4513f`。后续测试与发布文档提交不改变该镜像的产品代码。
- 服务器 `ldclouda31539` / `43.228.79.108`，后端镜像 `goodhr-production-backend:wechat-author-20261010-77e61838`，ID `sha256:ab298fe6da0aaec25763617a4ea84023649f0e989188eb6e0eb8c52db3a09a54`。
- 正式后端容器 ID `f7e2cb51cd54aa2eecfe4e4d1c6fc8562861597de323773391552465911490e9`。先在 8087 验证候选，再切换 8086；旧容器保留，候选已清理。
- 仅更新后端。前端容器 `48985b9daf310dfb256ce26e5ca9b35ef2ddad634b0a7db21ef180636173378b` 保持原状，继续运行 `personal-ai-20261009-49d84328` 镜像。PostgreSQL / Redis 未重启，无新迁移，也不修改系统配置、会员或奖励账本。
- 正式容器完整环境、挂载、网络、命令、日志与重启策略沿用备份；正式邮件调度保持原 `GOODHR_DISABLE_EMAIL_SCHEDULER=false`，候选关闭调度。
- 独立只读核验正式后端参数、前端 / PostgreSQL / Redis 的 ID 与启动时间均符合预期；公众号私有配置和完整 onboarding JSON 与备份相等，迁移仍为 89 条。后端 health 200，匿名关注状态 401，官网、登录、robots、sitemap 均返回 200。
- 五项公网协议探针全部通过：正常签名 GET、AES GET 正确回显 200；忽略图片的加密 POST 返回 200 success；错误收件人 400，明文降级 403。探针时间为北京时间 2026-10-10 00:19:52–53，前后关注、奖励、绑定、会员和码表摘要一致。

详细安全核验报告保存在前端忽略目录 `artifacts/wechat-author-qa/`。生产测试仅使用忽略的图片消息，不伪造关注或领取；真实关注内容通过本地签名、独立 AES 解密的回调测试验证。

## 备份及回退

备份 `/www/wwwroot/goodhr-production/backups/wechat-author-20261010-001145`，目录 700、私有文件 600。数据库备份 87,356,108 字节，SHA256 `235dbbf57daadefbbcee9b7ffb36f29eec50ec9ee65e5ed19ef8fc9f3a68ed70`；`pg_restore -l` 读取 499 条 TOC，未做整库恢复演练。

原后端 ID `d3836986479b3260046ebdb2b8d55361572732f73a4761c1cf3c1cf495908ca7`，停止后保留为 `goodhr-production-backend-backup-20261010-001145`。发布脚本独立审核通过，先核对候选和镜像身份；失败自动恢复旧后端。新旧绑定协议兼容，本次回退不隐藏公众号配置。

必要时在已确认的目标服务器执行：

```sh
python3 /www/wwwroot/goodhr-production/backups/wechat-author-20261010-001145/deploy-backend.py rollback /www/wwwroot/goodhr-production/backups/wechat-author-20261010-001145 77e6183899be03a60ccbf523b8942000322e2bab
```

回退仅恢复后端，不还原整库覆盖实时业务、不删除奖励或其他历史备份。
