-- 本迁移为岗位模板增加所属招聘平台，并将 Boss 平台详情模式固定为 OCR。
ALTER TABLE positions
  ADD COLUMN IF NOT EXISTS platform_id TEXT NOT NULL DEFAULT 'boss';

COMMENT ON COLUMN positions.platform_id IS '岗位模板所属招聘平台标识，例如 boss、zhaopin、liepin';

UPDATE positions
SET
  platform_id = 'boss',
  common_config = jsonb_set(COALESCE(common_config, '{}'::jsonb), '{detail_mode}', '"ocr"', true)
WHERE platform_id = 'boss';
