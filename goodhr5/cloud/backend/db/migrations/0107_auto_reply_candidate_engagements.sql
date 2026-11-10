-- 文件作用：历史自动回复报告记录了实际关联的岗位，为这些候选人补上简历库岗位筛选使用的触达关系。
-- 同一岗位已有任意账号触达时保持其原状态；不推测没有报告的历史简历归属，不产生招呼、手动推荐或任务统计。
INSERT INTO candidate_engagements (tenant_id, candidate_id, position_id, platform_id, status, created_at, updated_at)
SELECT r.tenant_id, r.candidate_id, r.position_id, p.platform_id, 'auto_reply', MIN(r.created_at), MIN(r.created_at)
FROM candidate_auto_reply_reports r
JOIN candidate_profiles cp ON cp.id = r.candidate_id AND cp.tenant_id = r.tenant_id
JOIN positions p ON p.id = r.position_id
JOIN users u ON u.id = p.user_id AND u.tenant_id = r.tenant_id
WHERE NOT EXISTS (
    SELECT 1 FROM candidate_engagements ce
    WHERE ce.tenant_id = r.tenant_id AND ce.candidate_id = r.candidate_id AND ce.position_id = r.position_id
)
GROUP BY r.tenant_id, r.candidate_id, r.position_id, p.platform_id
ON CONFLICT DO NOTHING;
