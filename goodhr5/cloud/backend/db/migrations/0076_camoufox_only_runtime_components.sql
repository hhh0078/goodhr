-- 文件作用说明：为已有系统配置补齐 Node、Camoufox 和 Windows OCR 下载资源，并移除旧浏览器下载配置。
-- win/mac 的 version、url、sha256、note 分别表示版本、下载地址、校验值和中文说明。
-- 已配置自建 Node 或 Camoufox 镜像的系统保留原配置；旧 OSS 地址切换到可用的 OSS2 镜像。
UPDATE system_configs
SET config_value = jsonb_set(
  jsonb_set(
    jsonb_set(
      config_value #- '{runtime_components,cloakbrowser}',
      '{runtime_components,camoufox}',
      jsonb_build_object(
        'win', CASE
            WHEN NULLIF(BTRIM(config_value #>> '{runtime_components,camoufox,win,url}'), '') IS NOT NULL
                THEN config_value #> '{runtime_components,camoufox,win}'
            ELSE '{"version":"152.0.4-beta.30","url":"https://oss2.58it.cn/camoufox-152.0.4-beta.30-win.x86_64.zip","sha256":"ea52a02fb1cfb1813ef6a326bea03fb2b650c9774143d953a94a27bfc8f10072","note":"Camoufox Windows x64"}'::jsonb
        END,
        'mac', CASE
            WHEN NULLIF(BTRIM(config_value #>> '{runtime_components,camoufox,mac,url}'), '') IS NOT NULL
                THEN config_value #> '{runtime_components,camoufox,mac}'
            ELSE '{"version":"152.0.4-beta.30","url":"https://oss2.58it.cn/camoufox-152.0.4-beta.30-mac.arm64.zip","sha256":"3b43e766574f286a6a63296cf58b660b7a3120952086c869b4df4c9a71604bc3","note":"Camoufox macOS Apple Silicon"}'::jsonb
        END
      ),
      true
    ),
    '{runtime_components,ocr,win}',
    '{"version":"rapidocr-json-v0.2.0","url":"https://oss2.58it.cn/goodhr-rapidocr-json-win-x64-v0.2.0.zip","sha256":"4db6867818002f194f79d1edda291efb438ab6f371d4b307bae990232b917a3d","note":"RapidOCR-json Windows x64，包含中英文 V3/V4 模型"}'::jsonb,
    true
  ),
  '{runtime_components,node_runtime}',
  jsonb_build_object(
    'win', CASE
      WHEN NULLIF(BTRIM(config_value #>> '{runtime_components,node_runtime,win,url}'), '') IS NOT NULL
        AND config_value #>> '{runtime_components,node_runtime,win,url}' NOT LIKE 'https://oss.58it.cn/%'
        THEN config_value #> '{runtime_components,node_runtime,win}'
      ELSE '{"version":"22.19.0","url":"https://oss2.58it.cn/goodhr-node-runtime-win-x64.zip","sha256":"ea3fad0e67a991d8477d8c01344b56e69c676ccb733f065b22436994b1253f86","note":"GoodHR Node 运行环境 Windows x64"}'::jsonb
    END,
    'mac', CASE
      WHEN NULLIF(BTRIM(config_value #>> '{runtime_components,node_runtime,mac,url}'), '') IS NOT NULL
        AND config_value #>> '{runtime_components,node_runtime,mac,url}' NOT LIKE 'https://oss.58it.cn/%'
        THEN config_value #> '{runtime_components,node_runtime,mac}'
      ELSE '{"version":"22.19.0","url":"https://oss2.58it.cn/goodhr-node-runtime-darwin-arm64.tar.gz","sha256":"c59006db713c770d6ec63ae16cb3edc11f49ee093b5c415d667bb4f436c6526d","note":"GoodHR Node 运行环境 macOS Apple Silicon"}'::jsonb
    END
  ),
  true
)
WHERE config_key = 'system.onboarding_config'
  AND jsonb_typeof(config_value #> '{runtime_components}') = 'object';
