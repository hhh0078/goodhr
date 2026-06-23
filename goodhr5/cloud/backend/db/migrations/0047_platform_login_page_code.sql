-- 为平台公开页面补充用途标识，便于前端区分登录页和其它公开页面。
-- 影响范围：仅更新已有平台配置中的 public.pages JSON，不改动用户业务数据。

UPDATE system_configs
SET config_value = jsonb_set(
    config_value,
    '{public,pages}',
    (
      SELECT jsonb_agg(
        CASE
          WHEN page->>'code' IS NULL
            AND (
              page->>'url' ILIKE '%login%'
              OR page->>'url' ILIKE '%/web/user/%'
              OR page->>'title' ILIKE '%登录%'
            )
          THEN page || '{"code":"login"}'::jsonb
          ELSE page
        END
      )
      FROM jsonb_array_elements(config_value->'public'->'pages') AS page
    ),
    true
)
WHERE config_key LIKE 'platform.%'
  AND enabled = true
  AND jsonb_typeof(config_value->'public'->'pages') = 'array';
