<!-- 文件作用：说明2026-10-18视频教程播放器的专用前端发布、回退和精确清理，不替代通用双服务发布流程。 -->

# 教程播放器专用前端发布

本次仅替换 `goodhr-production-frontend-1`，限制主机为 `ldclouda31539 / 43.228.79.108`。后端、PostgreSQL、Redis 的容器 ID、启动时间和配置必须保持不变。不执行数据库迁移、全库导出、镜像回收或其他项目操作。

## 本次教程与媒体包

- 源文件：`D:\下载\GoodHR新手教程_中文字幕配音版.mp4`，26343513字节，SHA256 `de8f8bc6ddb25422e7ca635802bcfb630be536a40935d21a438fa082f9734b28`。
- HLS：[安装与首次使用教程](https://oss2.58it.cn/goodhr-tutorial-install-20261018-de8f8bc6.m3u8)。
- 可选封面：[教程封面](https://oss2.58it.cn/goodhr-tutorial-install-20261018-de8f8bc6-poster.jpg)。
- 媒体包：`goodhr-tutorial-install-20261018-de8f8bc6-media.zip`，27346495字节，SHA256 `2b0ff648afb960f79841f8d82c5453ec52b83854af885c73ddaf8d40652f3dda`。
- 本次76个媒体文件含74个TS分片，共28878450字节，平铺在 `oss2.58it.cn` 根目录。实际发布须另外核对播放列表、分片及封面公开响应。
- 配置与媒体部署备份目录：`/opt/goodhr-release/tutorial-media-20261018-de8f8bc6`，保留旧 `system.guide` 完整行、新视频数组、已通过的事务回退演练及修改前Nginx配置。

## 后台配置

超级管理员进入“系统配置 → 邀请帮助 → 帮助中心”，继续编辑原 `system.guide` 的 `videos` 数组；保留 `cards`、`summary` 等其他指南字段。数组支持多条，顺序就是展示顺序，`enabled: false` 隐藏，`poster` 可不填。只修改本次授权的视频数组，原有视频应按实际配置保留。

下例第一条为本次真实教程，第二条说明如何追加视频，地址为待替换的占位值且默认隐藏；不要用示例覆盖其他指南字段：

```json
{
  "videos": [
    {
      "id": "install-and-first-task",
      "title": "GoodHR 安装与首次使用教程",
      "description": "约10分钟，完成注册、安装、创建岗位、登录平台并开始筛选。",
      "src": "https://oss2.58it.cn/goodhr-tutorial-install-20261018-de8f8bc6.m3u8",
      "poster": "https://oss2.58it.cn/goodhr-tutorial-install-20261018-de8f8bc6-poster.jpg",
      "enabled": true
    },
    {
      "id": "another-guide",
      "title": "下一条教程",
      "description": "填写这条教程的简短说明。",
      "src": "https://example.com/another-guide.mp4",
      "enabled": false
    }
  ]
}
```

## 前端镜像发布

先将本次代码提交并冻结为完整提交 ID，使用正式前端 Dockerfile 和原生产构建参数构建干净镜像。镜像需有 `goodhr.source_commit`、`goodhr.release_kind=frontend`、`goodhr.release.id`、`goodhr.dockerfile_sha256` 标签；发布 ID 必须等于发布目录名称，名称以 `tutorial-hls-` 开头。镜像参数必须使用完整 `sha256:` ID，不能使用浮动标签。源码必须无未提交或未跟踪修改。

脚本是本次明确授权的单前端专用入口，不修改通用发布器。三个既有共用锁必须已存在：`/opt/goodhr-release/.release-operation.lock`、`.frontend-deploy.lock` 及 `/www/wwwroot/goodhr-production/backups/.goodhr-container-deploy.lock`；同时持有源码目录已有的 `.release-source.lock`，防止自动快进源码和工具。

依次执行下列阶段，每次均传入相同的三个参数：

```bash
python3 scripts/releases/tutorial-hls-frontend-20261018.py prepare --release-dir /opt/goodhr-release/tutorial-hls-20261018 --image sha256:完整新镜像ID --commit 完整40位提交ID
python3 scripts/releases/tutorial-hls-frontend-20261018.py rehearse --release-dir /opt/goodhr-release/tutorial-hls-20261018 --image sha256:完整新镜像ID --commit 完整40位提交ID
python3 scripts/releases/tutorial-hls-frontend-20261018.py activate --release-dir /opt/goodhr-release/tutorial-hls-20261018 --image sha256:完整新镜像ID --commit 完整40位提交ID
```

`prepare` 拒绝覆盖已有目录，写入0700目录内的0600私有完整快照。不要打印或上传其中的 Env、完整 inspect 或配置。镜像来源仍经过既有 `release_guard.py` 检查；原前端限定 ID `023dd87d…2ddb32`、镜像 `sha256:b37ea472…59647`、源码前缀 `bf751957`；后端限定 ID `88a84e52…d38548`。

`rehearse` 分别在随机 `127.0.0.1` 端口运行候选新前端和原镜像，GET `/videos`、`/login`，并确认公开 `/videos`、`/login` 返回200；后端健康仅读取 `http://127.0.0.1:8086/health`。公开 `/health` 当前会落到前端并返回404，不能把它当发布健康入口。演练候选使用独立网络别名，结束停止，保留到精确清理。演练不写业务数据；管理员配置 `system.guide.videos` 的单行备份及事务回退演练由本次发布操作者另行完成。

`activate` 再次检查源码、镜像和容器漂移，仅停止并改名保留原前端，复制原完整 Config、HostConfig、有序且可重复的 Env 及显式网络参数创建新前端。本次拒绝新增缺省环境键，只更新镜像和本次审计标签。任何创建、启动、健康或首次终止信号故障均尝试恢复原容器 ID；恢复期间屏蔽重复终止信号，避免原前端停在半途。创建前持久保存意图，可恢复响应丢失或日志滞后的本轮精确候选。不要使用通用 `--activate-only`，它会切换两个服务。

需要回退时，把上述阶段改成 `rollback`；需要收尾时改成 `cleanup`，三个参数保持一致。回退保留失败候选；清理只删除本轮精确登记的演练或失败候选，不删除原回退容器、当前正式前端或任何镜像、卷。遇到状态漂移或自动回退未完整通过，保留材料排查，不重复准备或覆盖现场。

本地静态或内存模拟通过不等于生产验证。正式执行后应另行保留播放器桌面与手机实测、公开视频及分片响应、当前前端新 ID，以及受保护服务 ID/StartedAt 未变的证据。
