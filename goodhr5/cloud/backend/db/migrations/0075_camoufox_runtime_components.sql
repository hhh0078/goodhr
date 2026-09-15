-- 本迁移为运行组件清单增加 Camoufox 反检测浏览器分组。
-- 镜像包上传 OSS 后由管理员填入下载地址；填入前本地程序自动回退使用旧 cloakbrowser 配置，不影响存量安装。
-- 说明：geoip 按代理 IP 动态匹配地理位置的功能已停用（用户与招聘平台均在中国大陆，固定 zh-CN 与北京时间），不再需要 GeoLite2 数据库。
-- 每个字段的中文备注：version 组件版本号、url 下载地址、sha256 校验值、note 版本说明。
UPDATE system_configs
SET config_value = jsonb_set(
    config_value,
    '{runtime_components,camoufox}',
    COALESCE(
        config_value #> '{runtime_components,camoufox}',
        '{
          "win": {
            "version": "",
            "url": "",
            "sha256": "",
            "note": "Camoufox 反检测浏览器 Windows x64，镜像包上传 OSS 后填入，本地程序优先使用本组配置"
          },
          "mac": {
            "version": "",
            "url": "",
            "sha256": "",
            "note": "Camoufox 反检测浏览器 macOS Apple Silicon，镜像包上传 OSS 后填入"
          }
        }'::jsonb
    ),
    true
)
WHERE config_key = 'system.onboarding_config';
