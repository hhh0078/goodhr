-- 新增公告独立配置行
-- config_value 存储 JSON 数组，每项为一个公告对象 { title, content }
-- null 或 [] 时不显示公告，多条则依次弹出多个弹框

insert into system_configs (config_key, config_value, description)
values (
  'frontend.announcements',
  '[]'::jsonb,
  '插件公告列表（数组，null/[] 不显示，每项 { title, content }）'
)
on conflict (config_key) do update set
  config_value = excluded.config_value,
  description = excluded.description,
  updated_at = now();
