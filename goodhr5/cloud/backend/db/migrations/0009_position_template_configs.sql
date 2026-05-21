-- 岗位模板扩展：公共参数、AI 专属参数、关键词专属参数
ALTER TABLE positions
  ADD COLUMN IF NOT EXISTS common_config JSONB NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS ai_config JSONB NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS keyword_config JSONB NOT NULL DEFAULT '{}'::jsonb;

COMMENT ON COLUMN positions.common_config IS '岗位模板公共参数（提示音、各类延迟等）';
COMMENT ON COLUMN positions.ai_config IS '岗位模板 AI 模式专属参数（模型、岗位要求、提示词等）';
COMMENT ON COLUMN positions.keyword_config IS '岗位模板关键词模式专属参数（关键词行为、详情打开概率等）';
