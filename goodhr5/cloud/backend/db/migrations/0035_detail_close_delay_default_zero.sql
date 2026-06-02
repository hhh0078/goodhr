-- 作用：将关闭详情前延时的默认值调整为 0 秒，避免新老用户默认等待。
ALTER TABLE user_preferences
    ALTER COLUMN detail_close_delay_min SET DEFAULT 0,
    ALTER COLUMN detail_close_delay_max SET DEFAULT 0;

-- 仅把仍停留在旧默认值的用户改为 0，保留用户主动设置过的其它值。
UPDATE user_preferences
SET detail_close_delay_min = 0,
    detail_close_delay_max = 0
WHERE detail_close_delay_min = 1
  AND detail_close_delay_max = 2;
