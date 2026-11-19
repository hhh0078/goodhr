<!-- 文件作用说明：记录6.3.4.9智联修复的Mac发布、现有Windows记录保留、下载校验及验收边界。 -->

# 本地程序6.3.4.9发布

本次生产代码只调整智联推荐页经历字段范围，公共流程、Worker、其它平台配置和截图逻辑不变。源码修复提交`5b44c6c32006c64db3801d60b294ed90092e20a0`，已合入main。

## 产物

- Mac正式脚本`package-release.sh 6.3.4.9 build`生成通用包；PKG包含arm64和x86_64的Go与原生入口，以及两种架构原生Worker依赖。
- PKG：`https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.3.4.9-mac-universal-8f5e7d62.pkg`，60876832字节，SHA256 `8f5e7d62aeccffd3ed76b99803bf386f1d702e4710bc9f6dadc5073e988829c7`。
- Mac ZIP：`https://oss2.58it.cn/goodhr-local-agent-v6.3.4.9-darwin-universal-0951db4c.zip`，62717421字节，SHA256 `0951db4c9dc7815cec1a3486918f6c7d60d9b12487b54b9decde7295ff798c86`。
- 保留其它发布任务已上线的Windows6.3.4.9 ZIP：`https://oss2.58it.cn/goodhr-local-agent-v6.3.4.9-windows-x64-c0828add.zip`，31606814字节，SHA256 `c0828addc478b7a153e7d7d7f7ce25cd0e68534f5e393039558b9e314bc96279`。其main源码`8e6c8b04`的本地程序目录与本轮修复提交逐字一致。详情见`windows-6349-release-20261119.md`。
- 本轮额外运行Windows原生打包流水线`44476207944`成功，但检测到上述现有Windows发布后没有替换它，也没有重复公开另一套Windows安装包。

## 验证与边界

- 真实智联推荐页20张卡片只读唯一定位通过，没有发送测试招呼。
- 同一现有Worker的本地合成页重现旧6.3.4.8失败；新配置的普通/块/flex排版、换位、同名不同经历、重复身份拒绝、搜索页兼容通过。
- Go41个有测试包通过；Worker386通过、1跳过；Windows x64交叉编译通过；Mac arm64原生和x86_64 Rosetta命令行启动通过。
- PKG/ZIP/应用内容一致；包内版本6.3.4.9，生产地址正确；全部Worker文件与6.3.4.8相同。PKG未签名、未公证。
- 未自动安装到用户Mac，未重启招聘浏览器；真实岗位连续打招呼、Windows与Intel Mac真机安装、其它平台真实账号长跑未验收。

## 发布保护

只为已有更新配置前插一条Mac6.3.4.9记录，保留现有Windows6.3.4.9及所有历史和其它配置。采用整行加锁比较，事务回滚演练通过后才提交；6.3.4系列第四段补丁可忽略，不改变强制更新规则。

数据库配置快照、精确回退SQL和验收材料保留在服务器`/opt/goodhr-release/local-agent-6.3.4.9-20261119/`。本机完整构建、原生回归与公开下载验证材料在`/Users/Zhuanz/Documents/Codex/2026-11-19/goodhr-zhaopin-6349/`及相邻`goodhr-zhaopin-live/`。

本次只发布客户端文件和更新配置，没有构建或激活云端镜像，也没有新增云端容器；收尾只清理本轮暂存安装包，保留数据库配置备份、SQL、发布记录及公开下载。
