with frontend as (
  select config_value
  from system_configs
  where config_key = 'frontend'
)
insert into system_configs (config_key, config_value, description)
values
  (
    'frontend.website_url',
    coalesce((select to_jsonb(config_value ->> 'website_url') from frontend), to_jsonb('http://goodhr.58it.cn'::text)),
    '前端官网地址'
  ),
  (
    'frontend.contact_url',
    coalesce((select to_jsonb(config_value ->> 'contact_url') from frontend), to_jsonb('http://58it.cn'::text)),
    '前端联系地址'
  ),
  (
    'frontend.donate_url',
    coalesce((select to_jsonb(config_value ->> 'donate_url') from frontend), to_jsonb('http://58it.cn'::text)),
    '前端打赏地址'
  ),
  (
    'frontend.share_url',
    coalesce((select to_jsonb(config_value ->> 'share_url') from frontend), to_jsonb('http://goodhr.58it.cn'::text)),
    '前端分享地址'
  ),
  (
    'frontend.announcement',
    coalesce((select to_jsonb(config_value ->> 'announcement') from frontend), to_jsonb('免费版用于关键词筛选，AI版用于岗位说明智能判断。'::text)),
    '前端公告'
  ),
  (
    'frontend.default_click_prompt',
    coalesce((select to_jsonb(config_value ->> 'default_click_prompt') from frontend), to_jsonb(''::text)),
    '默认查看详情 Prompt'
  ),
  (
    'frontend.ads',
    coalesce(
      (select config_value -> 'ads' from frontend),
      jsonb_build_array(
        jsonb_build_object(
          'title', 'GoodHR 官网',
          'subtitle', '查看插件说明与最新版本',
          'url', 'http://goodhr.58it.cn',
          'background_color', '#1d4ed8',
          'text_color', '#ffffff'
        ),
        jsonb_build_object(
          'title', '联系作者',
          'subtitle', '插件、后台、自动化工具定制',
          'url', 'http://58it.cn',
          'background_color', '#eff6ff',
          'text_color', '#1e3a8a'
        ),
        jsonb_build_object(
          'title', '分享给另一个HR',
          'subtitle', '把插件发给同事一起使用',
          'url', 'http://goodhr.58it.cn',
          'background_color', '#ecfeff',
          'text_color', '#155e75'
        )
      )
    ),
    '前端广告配置'
  )
on conflict (config_key) do update set
  config_value = excluded.config_value,
  description = excluded.description,
  updated_at = now();
