<!-- 文件作用：记录用户已构建的Mac 6.3.4.3安装包发布、下载配置及校验依据。 -->
# Mac 6.3.4.3 发布记录

用户已构建安装包，本次仅检查并通过宝塔MCP发布，不重建、不覆盖历史包、不修改Windows及运行组件配置。

| 文件 | 字节数 | SHA256 |
| --- | ---: | --- |
| GoodHR-Local-Agent-Setup-v6.3.4.3-mac-universal-3bf42b72.pkg | 60863157 | 3bf42b72e739af9752d6a7bde2caedb04602ae6f901d063e143b7ae2bcb5d113 |
| goodhr-local-agent-v6.3.4.3-darwin-universal-4fe11748.zip | 62702932 | 4fe1174895879ae76b0e2f79cf9b5aef36ad06d4487d7fb39c0323e5067699c5 |

文件发布在 https://oss2.58it.cn/ 根目录。本地、服务器、Mac公网完整下载与服务器公网完整下载的两组大小及SHA256均一致，HTTPS返回200。

`system.onboarding_config.local_agent`首位新增Mac 6.3.4.3记录，URL和SHA256使用PKG；原5条历史版本、Windows 6.3.4.2及其他字段不变。整行锁和快照比对防止覆盖并发修改，临时表演练更新及完全回退通过。备份、更新及条件回退SQL位于服务器私有目录 `/www/backup/goodhr-mac-6.3.4.3-20261113/`。

公网 `/api/system/local-agent-updates` 已返回新版；真实Chrome检查官网下载页版本、下载按钮和安装提示弹窗链接均为新PKG。后台共用下载配置；调用实际`latestLocalAgentRelease`方法验证Mac选择6.3.4.3的PKG及其SHA256，Windows继续选择6.3.4.2。未使用登录后台的真实会话进行安装或自动更新操作。

静态检查：PKG版本与生产域名正确，主程序及启动器含arm64和x86_64，两架构impit存在，Worker为camoufox-js 0.12.0；ZIP完整性通过，PKG与ZIP的2250项应用内容一致。安装包未签名；本次没有实际安装、运行程序或Intel真机验收，不据下载成功推断运行功能通过。

同轮公众号检查：用户使用已关注微信发送纯4位数字，确认公众号有回复且网页自动登录成功。后端相关回归通过，Redis路径通过；未设置独立PG连接的集成用例跳过。没有发现需要新增后端修复的问题。微信说明按用户要求更新为“使用微信扫码关注公众号，将下面的数字发送给公众号。”；桌面、390px和320px真实Chrome隔离接口交互检查通过，保留协议前置、缓存、红色数字和紧凑操作间距。
