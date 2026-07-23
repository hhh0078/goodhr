-- 本文件用于一次性修复岗位累计扫描使用“已保存候选人数”造成的历史漏计。

UPDATE positions
SET scanned_count = scanned_count + skipped_count + failed_count,
    updated_at = NOW()
WHERE skipped_count > 0 OR failed_count > 0;

COMMENT ON COLUMN positions.scanned_count IS '岗位累计读取到的去重候选人数，包含后续打招呼、跳过和失败的候选人';
