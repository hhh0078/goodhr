-- 本迁移为运行组件清单增加 Camoufox 反检测浏览器分组和 GeoIP 数据库分组。
-- Camoufox 镜像包上传 OSS 后由管理员填入下载地址；填入前本地程序自动回退使用旧 cloakbrowser 配置，不影响存量安装。
-- GeoIP 数据库（GeoLite2-City，win/mac 同一文件）由我们自主分发，避免用户端访问 GitHub 下载失败。
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

UPDATE system_configs
SET config_value = jsonb_set(
    config_value,
    '{runtime_components,geoip}',
    COALESCE(
        config_value #> '{runtime_components,geoip}',
        '{
          "win": {
            "version": "",
            "url": "",
            "sha256": "",
            "note": "GeoLite2-City GeoIP 数据库（win/mac 为同一文件），代理场景时区语言匹配使用，避免用户访问 GitHub 下载"
          },
          "mac": {
            "version": "",
            "url": "",
            "sha256": "",
            "note": "GeoLite2-City GeoIP 数据库（与 win 相同文件），镜像上传 OSS 后填入"
          }
        }'::jsonb
    ),
    true
)
WHERE config_key = 'system.onboarding_config';
