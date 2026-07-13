-- 本迁移用于给岗位模板补充“是否输出简历结构化信息”开关，历史岗位默认关闭。
UPDATE positions
SET common_config = jsonb_set(
  COALESCE(common_config, '{}'::jsonb),
  '{output_structured_resume}',
  'false'::jsonb,
  true
)
WHERE NOT COALESCE(common_config, '{}'::jsonb) ? 'output_structured_resume';
