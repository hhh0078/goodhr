<!-- 文件作用：说明 GoodHR 托管火狐在冷启动前检查、校验并替换临时简历插件的流程和验证边界。 -->
# 托管火狐临时简历插件

GoodHR 打开一个新的火狐进程前检查临时插件版本。已有浏览器窗口保持复用，不更新它正在读取的代码目录，不自动关闭用户的标签页；关闭浏览器后再次通过 GoodHR 打开，才加载新版本。

临时插件发布清单为 `/zip/goodhr-resume-firefox-latest.json`，包含版本、同站下载地址、SHA256、固定扩展ID和明确的 `temporary` 通道。插件构建会同时生成清单；发布时先上传并验证对应的不可变XPI，再原子更新清单。它独立于官网Firefox正式签名包和旧程序兼容配置，不把未签名包冒充Mozilla签名包。

本地程序复用插件下载器，核对摘要、安全解压、身份、后台文件和版本，校验成功后才替换 `extensions/goodhr-resume-assistant`。相同版本和内容不重复下载；失败保留完整的旧缓存并记录原因，不能把失败写成已经更新。下载与启动串行，避免两次打开互相替换目录。

Camoufox使用现有 `addons` 启动配置临时加载这个固定目录。插件ID保持 `resume-assistant@goodhr.58it.cn`，浏览器继续使用原Profile。清理范围仅为托管旧代码及下载临时文件，不执行扩展卸载，不删除Profile、storage或UUID，也不修改全局存储保留或安全偏好。账号是否仍有效还取决于会话到期、用户退出及是否换了Profile，不能承诺永久免登录。

Windows沿用Camoufox原生临时加载。macOS的临时通道会绕过旧的“把签名XPI复制进Profile”步骤，同样交给Camoufox加载；正式签名通道仍保留原处理。公共Go流程支持Windows x64、macOS Apple Silicon和Intel，Mac真机行为需单独验证，交叉编译不代表真机验收。

本轮问题：旧本地程序重启后自动准备1.0.9，覆盖手动加载的新版；1.0.9没有BOSS站点匹配，所以打开附件不弹卡片。新通道以临时清单为准，不再被正式签名版1.0.9拉回。

## 验证记录（2026-10-17）

- Go全量测试通过：33个测试包、7个无测试包，0失败。覆盖下载、摘要、损坏缓存、离线回退、防降级、取消、并发和原生Profile锁。
- Worker全量测试：213项，212通过、0失败、1项Mac真机语言包测试跳过。Windows Camoufox 152.0.4-beta.30实际运行固定ID测试扩展，连续启动和同ID升级后，storage.local测试值及UUID均保留。测试使用独立Profile，不读取真实账号。
- 插件构建相关10项测试通过；Chrome和Firefox 1.0.16包内容与已发布包相同。独立临时通道清单只用于GoodHR托管火狐。
- Windows amd64、macOS arm64、macOS amd64均以CGO_ENABLED=0编译通过。Mac安装、原生扩展和真实登录保留尚未经过真机验收。

## Windows真实安装验收

本机从6.2.11.0通过现有本地更新接口下载、校验并安装6.2.11.1，安装路径为`D:\GoodHRLocalAgent`。安装后主程序摘要与发布包内主程序一致，使用原`data/profiles/default`，没有重新登录或重新配对。

1. 安装完成、尚未打开火狐时，托管插件仍为1.0.9；通过本地`/api/v1/page/open`打开BOSS后自动下载并临时加载1.0.16，日志为`cached=false already_current=false`。旧代码备份目录已清理。
2. 工具栏显示1.0.16且原插件账号仍登录，BOSS账号也保留。打开已取得的BOSS简历附件后，GoodHR卡片自动弹出并读取基本资料，没有点击添加入库。
3. 通过本地接口关闭火狐，再用同一Profile打开；插件仍为1.0.16且账号保持登录，BOSS也保持登录。日志为`cached=false already_current=true`，插件manifest内容及修改时间均未变化，未重复下载。原扩展UUID保持一致。
4. 浏览器保持运行时再次打开同一页面返回`reused=true`，没有再次执行插件准备步骤。

Windows独立安装包：`/zip/GoodHR-Local-Agent-Setup-v6.2.11.1-windows-x64.exe`，31177141字节，SHA256为`3fe17c4b083328f3a93522d868ef6732792fa0ea8fed6138c08683bd30defe45`，主程序构建提交为`7e5efb61a3c3f51b1229b33ee252f41bb74cd568`。安装包与临时插件清单已单独发布，本轮没有修改正式本地程序更新配置，也没有重启云端容器。

## 正式更新入口补充（2026-10-17）

按用户随后要求，已将上述已安装验收的Windows 6.2.11.1 EXE加入`system.onboarding_config.local_agent`正式更新记录首项，官网`/download`和`/api/system/local-agent-updates`均已显示新版本及同一下载地址。更新器原生支持EXE，未使用包含主程序的便携ZIP冒充安装器。

只前插一条Windows记录，保留原47条历史记录和全部Mac下载信息；Mac最新仍为已发布的6.2.11.0。`runtime_components`、应用配置、浏览器插件签名配置和运行容器均保持。变更前备份完整配置行，并在临时表事务内验证更新、回滚后原记录完全一致，再使用原JSON和更新时间进行并发校验后写入一行。备份及回退SQL位于服务器`/opt/goodhr-release/local-agent-6-2-11-1-formal-7b2d471d-20261017`。

托管火狐临时插件清单继续发布1.0.16；普通Firefox的Mozilla签名版仍为1.0.9，未把未签名包标成签名包。Chrome下载包为1.0.16。

另已核对浏览器本体：本机仍为152.0.4-beta.30。官方156.0.1-beta.36的`PageHandler.js`已替换旧鼠标实现，现有GoodHR v11补丁要求的两个锚点不存在，且内置中文语言包仅支持152。因此本次没有直接替换浏览器内核；156需要先完成输入实现和中文语言包适配及真实操作回归。
