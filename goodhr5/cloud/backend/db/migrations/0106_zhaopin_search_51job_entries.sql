-- 文件作用：新增前程无忧平台入口，并补充智联搜索人才及两平台已开页面复用规则。
-- 只维护云端展示和登录入口；页面动作、行为和选择器仍由本地平台配置负责。
-- 保留已有平台配置、默认入口、开放开关和历史数据；重复执行不会重复追加地址。
INSERT INTO system_configs (config_key, config_value, description, enabled)
VALUES (
  'platform.51job',
  $config${
    "id": "51job",
    "name": "前程无忧",
    "domain": "mall.51job.com",
    "open": true,
    "auth": {
      "pages": [
        { "url": "https://mall.51job.com/Revision/online/talentRecommend", "entry": true, "match": "prefix", "title": "人才推荐" },
        { "url": "https://mall.51job.com/Revision/online/talent/search", "match": "prefix", "title": "搜索人才" }
      ],
      "entry_url": "https://mall.51job.com/Revision/online/talentRecommend"
    },
    "public": {
      "pages": [
        { "url": "https://mall.51job.com/", "match": "exact", "code": "login", "title": "前程无忧账号入口" }
      ]
    }
  }$config$::jsonb,
  '前程无忧展示、登录入口和已开页面复用配置；页面操作使用本地平台配置',
  true
)
ON CONFLICT (config_key) DO NOTHING;

-- 逐个平台合并明确支持的推荐和搜索地址，不覆盖管理员原先配置的认证规则。
DO $migration$
DECLARE
  platform_key TEXT;
  supported_urls JSONB;
  page_url TEXT;
  current_config JSONB;
  auth_config JSONB;
  auth_pages JSONB;
  reuse_urls JSONB;
BEGIN
  FOR platform_key, supported_urls IN
    SELECT * FROM (VALUES
      ('platform.zhaopin', '["https://rd6.zhaopin.com/app/recommend", "https://rd6.zhaopin.com/app/search"]'::jsonb),
      ('platform.51job', '["https://mall.51job.com/Revision/online/talentRecommend", "https://mall.51job.com/Revision/online/talent/search"]'::jsonb)
    ) AS supported(config_key, urls)
  LOOP
    SELECT config_value INTO current_config FROM system_configs WHERE config_key = platform_key FOR UPDATE;
    IF NOT FOUND OR jsonb_typeof(current_config) <> 'object' THEN
      CONTINUE;
    END IF;
    auth_config := CASE WHEN jsonb_typeof(current_config->'auth') = 'object' THEN current_config->'auth' ELSE '{}'::jsonb END;
    auth_pages := CASE WHEN jsonb_typeof(auth_config->'pages') = 'array' THEN auth_config->'pages' ELSE '[]'::jsonb END;
    reuse_urls := CASE WHEN jsonb_typeof(auth_config->'reuse_urls') = 'array' THEN auth_config->'reuse_urls' ELSE '[]'::jsonb END;
    FOR page_url IN SELECT jsonb_array_elements_text(supported_urls)
    LOOP
      IF NOT EXISTS (SELECT 1 FROM jsonb_array_elements(auth_pages) AS page WHERE page->>'url' = page_url) THEN
        auth_pages := auth_pages || jsonb_build_array(jsonb_build_object('url', page_url, 'match', 'prefix'));
      END IF;
      IF NOT reuse_urls ? page_url THEN
        reuse_urls := reuse_urls || jsonb_build_array(page_url);
      END IF;
    END LOOP;
    UPDATE system_configs
    SET config_value = jsonb_set(current_config, '{auth}', auth_config || jsonb_build_object('pages', auth_pages, 'reuse_urls', reuse_urls)),
        updated_at = now()
    WHERE config_key = platform_key;
  END LOOP;
END;
$migration$;
