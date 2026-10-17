-- 文件作用说明：为个人操作偏好增加鼠标移动速度档位，旧账号默认使用快速档。
ALTER TABLE user_preferences
    ADD COLUMN IF NOT EXISTS mouse_speed TEXT NOT NULL DEFAULT 'fast'
    CHECK (mouse_speed IN ('ultra_fast', 'fast', 'standard', 'relaxed', 'slow'));

COMMENT ON COLUMN user_preferences.mouse_speed IS '鼠标移动速度档位：ultra_fast超快90至210毫秒、fast快速180至360毫秒（默认）、standard标准350至650毫秒、relaxed舒缓650至1100毫秒、slow慢速1100至1800毫秒';
