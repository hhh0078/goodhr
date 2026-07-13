alter table accounts
  add column if not exists phone text,
  add column if not exists email text,
  add column if not exists balance numeric(12,4) not null default 0,
  add column if not exists status text not null default 'active',
  add column if not exists run_mode text not null default 'ai',
  add column if not exists current_position_name text not null default '',
  add column if not exists ai_expire_time date,
  add column if not exists is_and_mode boolean not null default false,
  add column if not exists match_limit integer not null default 60,
  add column if not exists enable_sound boolean not null default true,
  add column if not exists scroll_delay_min integer not null default 3,
  add column if not exists scroll_delay_max integer not null default 8,
  add column if not exists click_frequency integer not null default 7,
  add column if not exists collect_phone boolean not null default true,
  add column if not exists collect_wechat boolean not null default true,
  add column if not exists collect_resume boolean not null default true,
  add column if not exists communication_enabled boolean not null default true,
  add column if not exists greeting_enabled boolean not null default true,
  add column if not exists company_info_content text not null default '',
  add column if not exists job_extra_info text not null default '',
  add column if not exists ai_platform text not null default 'siliconflow',
  add column if not exists ai_model text not null default 'gpt-5.1-chat',
  add column if not exists ai_token text not null default '',
  add column if not exists ai_click_prompt text not null default '',
  add column if not exists ai_contact_prompt text,
  add column if not exists volcengine_api_key text not null default '',
  add column if not exists volcengine_model text not null default 'doubao-seed-1-6-250615',
  add column if not exists positions jsonb not null default '[]'::jsonb,
  add column if not exists extra_settings jsonb not null default '{}'::jsonb;

update accounts
set
  phone = case when identity_type = 'phone' then identifier else phone end,
  email = case when identity_type = 'email' then identifier else email end,
  run_mode = coalesce(settings->>'runMode', run_mode),
  current_position_name = coalesce(settings->>'currentPositionName', settings->'currentPosition'->>'name', current_position_name),
  ai_expire_time = coalesce(nullif(settings->>'ai_expire_time', '')::date, ai_expire_time),
  is_and_mode = coalesce((settings->>'isAndMode')::boolean, is_and_mode),
  match_limit = coalesce((settings->>'matchLimit')::integer, match_limit),
  enable_sound = coalesce((settings->>'enableSound')::boolean, enable_sound),
  scroll_delay_min = coalesce((settings->>'scrollDelayMin')::integer, scroll_delay_min),
  scroll_delay_max = coalesce((settings->>'scrollDelayMax')::integer, scroll_delay_max),
  click_frequency = coalesce((settings->>'clickFrequency')::integer, click_frequency),
  collect_phone = coalesce((settings->'communicationConfig'->>'collectPhone')::boolean, collect_phone),
  collect_wechat = coalesce((settings->'communicationConfig'->>'collectWechat')::boolean, collect_wechat),
  collect_resume = coalesce((settings->'communicationConfig'->>'collectResume')::boolean, collect_resume),
  communication_enabled = coalesce((settings->'runModeConfig'->>'communicationEnabled')::boolean, communication_enabled),
  greeting_enabled = coalesce((settings->'runModeConfig'->>'greetingEnabled')::boolean, greeting_enabled),
  company_info_content = coalesce(settings->'companyInfo'->>'content', company_info_content),
  job_extra_info = coalesce(settings->'jobInfo'->>'extraInfo', job_extra_info),
  ai_platform = coalesce(settings->'ai_config'->>'platform', ai_platform),
  ai_model = coalesce(settings->'ai_config'->>'model', ai_model),
  ai_token = coalesce(settings->'ai_config'->>'token', ai_token),
  ai_click_prompt = coalesce(settings->'ai_config'->>'clickPrompt', ai_click_prompt),
  ai_contact_prompt = coalesce(settings->'ai_config'->>'contactPrompt', ai_contact_prompt),
  volcengine_api_key = coalesce(settings->'ai_config'->'volcengine'->>'apiKey', volcengine_api_key),
  volcengine_model = coalesce(settings->'ai_config'->'volcengine'->>'model', volcengine_model),
  positions = coalesce(settings->'positions', positions),
  extra_settings = settings;

comment on table accounts is '用户配置主表，基础字段拆列，复杂配置保留在 JSON 字段中';
comment on column accounts.identifier is '用户唯一登录标识，可为手机号或邮箱';
comment on column accounts.identity_type is '标识类型，phone 或 email';
comment on column accounts.phone is '手机号标识，非手机号账号可为空';
comment on column accounts.email is '邮箱标识，非邮箱账号可为空';
comment on column accounts.balance is 'AI 余额';
comment on column accounts.status is '账号状态';
comment on column accounts.run_mode is '当前运行模式，free 或 ai';
comment on column accounts.current_position_name is '当前选中的岗位名称';
comment on column accounts.ai_expire_time is 'AI 权益过期日期';
comment on column accounts.is_and_mode is '关键词是否采用全匹配模式';
comment on column accounts.match_limit is '打招呼数量上限';
comment on column accounts.enable_sound is '是否启用提示音';
comment on column accounts.scroll_delay_min is '滚动最小延迟秒数';
comment on column accounts.scroll_delay_max is '滚动最大延迟秒数';
comment on column accounts.click_frequency is '候选人详情点击频率';
comment on column accounts.collect_phone is '是否收集手机号';
comment on column accounts.collect_wechat is '是否收集微信';
comment on column accounts.collect_resume is '是否收集简历';
comment on column accounts.communication_enabled is '是否启用沟通流程';
comment on column accounts.greeting_enabled is '是否启用打招呼流程';
comment on column accounts.company_info_content is '公司附加信息';
comment on column accounts.job_extra_info is '岗位附加信息';
comment on column accounts.ai_platform is 'AI 平台';
comment on column accounts.ai_model is 'AI 模型';
comment on column accounts.ai_token is 'AI Token';
comment on column accounts.ai_click_prompt is 'AI 查看详情提示词';
comment on column accounts.ai_contact_prompt is 'AI 联系提示词';
comment on column accounts.volcengine_api_key is '火山引擎 API Key';
comment on column accounts.volcengine_model is '火山引擎模型名';
comment on column accounts.positions is '岗位列表及其关键词等复杂结构';
comment on column accounts.extra_settings is '未拆分的扩展配置 JSON';
comment on column accounts.settings is '旧版兼容配置 JSON，保留用于迁移和兜底';

create table if not exists system_configs (
  config_key text primary key,
  config_value jsonb not null default '{}'::jsonb,
  description text not null default '',
  updated_at timestamptz not null default now()
);

comment on table system_configs is '系统配置表，存放官网链接、公告和全局 UI 配置';
comment on column system_configs.config_key is '系统配置键';
comment on column system_configs.config_value is '系统配置值 JSON';
comment on column system_configs.description is '系统配置说明';
comment on column system_configs.updated_at is '最后更新时间';

insert into system_configs (config_key, config_value, description)
values (
  'frontend',
  '{
    "website_url": "http://goodhr.58it.cn",
    "contact_url": "http://58it.cn",
    "donate_url": "http://58it.cn",
    "share_url": "http://goodhr.58it.cn",
    "announcement": "免费版用于关键词筛选，AI版用于岗位说明智能判断。"
  }'::jsonb,
  '前端公共配置'
)
on conflict (config_key) do nothing;
