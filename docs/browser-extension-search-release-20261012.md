<!-- 文件作用：记录 Chrome 简历插件 1.0.5、简历查重与内置简历排序的实现范围、验证结果和正式发布证据。 -->

# Chrome 简历插件 1.0.5 与查重排序发布记录（2026-10-12）

2026-10-12 北京时间 03:20 已发布到 GoodHR 正式环境。运行后端源码为 `292b04818706e498174f3e808b1bbb26ae7af114`，本轮没有数据库迁移，也没有更换前端、PostgreSQL 或 Redis。

## 本次行为

- 插件浮窗宽度调整为 370px，高度调整为最高 560px，减少横向遮挡并保留纵向信息空间。
- 登录入口统一为“密码登录、验证码登录、本地程序登录”三个选项；云端登录后仍保留紧凑的本地程序连接入口，供 AI 总结使用。
- “推荐到岗位”改为输入岗位、公司或城市后再显示最多 8 条匹配结果；输入为空时不展示岗位列表，清空输入可取消推荐。
- 有效手机号只做手机号和平台候选人编号精确查重，不再混入同名不同手机号结果；没有有效手机号时继续使用姓名、性别、年龄、最早学校和最早公司做结构化模糊查重。
- 内置示例简历“瓜瓜”的创建和更新时间固定为 2020-01-01，并只出现在全部真实简历之后；分页完整时进入下一页，分页不完整时追加在最后一页。
- 插件版本升级为 `1.0.5`。旧版检测到该版本后继续走已有的强制更新提示，并跳转 GoodHR 下载页。

Firefox 本轮没有发布安装包。后台原有 `url_firefox`、`sha256_firefox`、`firefox_signed` 及其他未知配置字段全部保留。

## 本地验证

| 项目 | 结果 |
| --- | --- |
| 插件单元测试 | 84/84 通过 |
| 插件 UI smoke | 通过 |
| Chrome 154 临时加载真实扩展 | 安装、浮窗预览、发送标签页和 frame 检查通过 |
| Chrome 构建 | 通过 |
| 本地联调构建 | 通过 |
| Go 后端测试 | `GOPROXY=off go test ./... -count=1` 通过 |
| Go 静态检查 | `go vet ./...` 通过 |
| 差异检查 | `git diff --check` 通过 |

Chrome 安装包：`GoodHR-Resume-Chrome-v1.0.5-c8880bc1.zip`，83893 字节，SHA256 为 `c8880bc1d757256aa61fa0bd6652471f3ab365f1be8b13378c98744293fd1406`。

## 正式发布证据

发布脚本固定服务器、现有四个正式容器的 ID/镜像/启动时间、源码归档、后端二进制、Chrome ZIP 和已审查启动器的 SHA256。切换前制作完整 PostgreSQL 自定义格式备份，并通过 `PGDMP` 文件头和 `pg_restore --list` 校验。

| 项目 | 结果 |
| --- | --- |
| 发布目录 | `/opt/goodhr-release/extension-search-1.0.5-292b0481` |
| 完整数据库备份 | 94016809 字节，SHA256 `ff5dac23818d25478a231aad19f4dccf84966096d6d67c25083d8b6c06d37347` |
| 隔离恢复 | 独立 PostgreSQL、100 条迁移、47 张 public 表全部一致 |
| 候选后端 | 环回端口 8089 健康；匿名简历库和查重接口均为 401；启动前后全部业务表指纹及迁移清单不变 |
| 正式新后端容器 | `d8720023ce2b95e2601943da0175c47b2e95457f54eb1fb0718739d10ea403fb` |
| 正式新后端镜像 | `sha256:1beeeff7b178ab91dae9a305fb97c371062fb73e5794abb95e4307222e851aca` |
| 旧后端备份 | `goodhr-production-backend-1-backup-extension-search-20261012-292b0481`，原 ID `d13d6895f44ab3b1613f38b0fca542d53497a10b808aea3b3ee2ba43c1ffd2f5` |
| 正式服务检查 | `/health` 200；简历库和查重匿名边界 401；重启次数 0；关键日志无 panic、fatal 或迁移失败 |
| 数据库 | 正式迁移仍为 100 条，本轮迁移数 0 |
| 依赖服务 | 前端、PostgreSQL、Redis 的 ID、镜像和启动时间与发布前一致 |

插件配置使用整行指纹、事务行锁和 CAS 更新，只改动 `version`、`note`、`url_chrome`、`sha256_chrome` 四个字段。发布后再次逐字段比较，Firefox 与未知字段保持不变。

## 公开下载

- 下载地址：<https://oss2.58it.cn/GoodHR-Resume-Chrome-v1.0.5-c8880bc1.zip>
- 官网下载页：<https://goodhr.58it.cn/download#browser-extension>
- 公开插件配置：<https://goodhr.58it.cn/api/system/browser-extension>

公开下载文件为 83893 字节，SHA256 与本地构建完全一致。官网页面已展示 `1.0.5` 和新的 OSS2 地址；公开配置接口本机和公网均为 200，响应继续包含 `Cache-Control: no-store`。

## 回退入口

后端与插件配置分开回退，不回退数据库，也不会启动更早的充值功能前版本：

```sh
python3 /opt/goodhr-release/extension-search-1.0.5-292b0481/production_release.py rollback-backend --root /opt/goodhr-release/extension-search-1.0.5-292b0481
python3 /opt/goodhr-release/extension-search-1.0.5-292b0481/production_release.py rollback-config --root /opt/goodhr-release/extension-search-1.0.5-292b0481
```

隔离候选后端、隔离 PostgreSQL 和隔离数据目录已清理。完整数据库备份、旧后端容器、构建镜像、发布前后配置快照和验收结果仍保留在发布目录中。
