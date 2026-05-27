alter table if exists accounts
  drop column if exists volcengine_api_key,
  drop column if exists volcengine_model;

delete from system_configs
where config_key = 'frontend';
