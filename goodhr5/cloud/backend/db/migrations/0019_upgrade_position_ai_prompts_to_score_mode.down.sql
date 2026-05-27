-- 回滚岗位模板提示词升级：删除新增 greet_prompt，并保留当前 filter/open 文本。
UPDATE positions
SET ai_config = ai_config - 'greet_prompt'
WHERE ai_config ? 'greet_prompt';
