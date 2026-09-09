-- 本迁移修复 Node 和 Windows OCR 运行组件的失效下载地址，并保留管理员自行配置的其他地址。

UPDATE system_configs
SET config_value = jsonb_set(
        config_value,
        '{runtime_components,node_runtime,win,url}',
        to_jsonb('https://oss2.58it.cn/goodhr-node-runtime-win-x64.zip'::text),
        false
    )
WHERE config_key = 'system.onboarding_config'
  AND config_value #>> '{runtime_components,node_runtime,win,url}' = 'https://oss.58it.cn/goodhr-node-runtime-win-x64.zip';

UPDATE system_configs
SET config_value = jsonb_set(
        config_value,
        '{runtime_components,node_runtime,mac,url}',
        to_jsonb('https://oss2.58it.cn/goodhr-node-runtime-darwin-arm64.tar.gz'::text),
        false
    )
WHERE config_key = 'system.onboarding_config'
  AND config_value #>> '{runtime_components,node_runtime,mac,url}' = 'https://oss.58it.cn/goodhr-node-runtime-darwin-arm64.tar.gz';

UPDATE system_configs
SET config_value = jsonb_set(
        jsonb_set(
            jsonb_set(
                config_value,
                '{runtime_components,ocr,win,url}',
                to_jsonb('https://oss2.58it.cn/goodhr-rapidocr-json-win-x64-v0.2.0.zip'::text),
                false
            ),
            '{runtime_components,ocr,win,version}',
            to_jsonb('goodhr-rapidocr-json-0.2.0'::text),
            false
        ),
        '{runtime_components,ocr,win,sha256}',
        to_jsonb('4db6867818002f194f79d1edda291efb438ab6f371d4b307bae990232b917a3d'::text),
        false
    )
WHERE config_key = 'system.onboarding_config'
  AND config_value #>> '{runtime_components,ocr,win,url}' IN (
      'https://oss.58it.cn/goodhr-ocr-win-x64.zip',
      'https://oss2.58it.cn/goodhr-ocr-win-x64.zip'
  );
