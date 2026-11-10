// 本文件限定30天清理只处理有在线临时凭据的资料，并在同来源全部成员中保护已取得手机或近期沟通的人选。
package httpapi

// retentionCloudSourceRelationSQL 只把精确云端编号、链接或平台原生来源相同的成员缓存关联到别名cp，不按姓名扩大清理范围。
const retentionCloudSourceRelationSQL = `(c.payload#>>'{online_profile,candidate_id}'=cp.id::text
 OR EXISTS(SELECT 1 FROM auto_reply_cloud_entities l WHERE l.tenant_id=c.tenant_id AND l.user_id=c.user_id AND l.kind='cloud_links' AND l.id='candidate:'||c.id AND l.payload->>'value'=cp.id::text)
 OR EXISTS(SELECT 1 FROM auto_reply_cloud_entities i WHERE i.tenant_id=c.tenant_id AND i.user_id=c.user_id AND i.kind='identities' AND i.payload->>'candidate_id'=c.id AND coalesce(i.payload->>'platform_candidate_id','')<>'' AND
 ((i.payload->>'platform_id'=cp.source_platform_id AND i.payload->>'platform_candidate_id'=cp.source_platform_candidate_id)
 OR EXISTS(SELECT 1 FROM candidate_profile_sources s WHERE s.tenant_id=cp.tenant_id AND s.candidate_id=cp.id AND s.platform_id=i.payload->>'platform_id' AND s.platform_candidate_id=i.payload->>'platform_candidate_id'))))`

// retentionOnlineTemporarySQL 要求真实在线快照或对应在线缓存；人工或插件缺手机号本身不能成为自动过期凭据。
const retentionOnlineTemporarySQL = `(EXISTS(SELECT 1 FROM candidate_profile_sources s WHERE s.tenant_id=cp.tenant_id AND s.candidate_id=cp.id AND jsonb_typeof(s.online_profile)='object' AND s.online_profile<>'{}'::jsonb)
 OR EXISTS(SELECT 1 FROM auto_reply_cloud_entities c WHERE c.tenant_id=cp.tenant_id AND c.kind='candidates' AND jsonb_typeof(c.payload->'online_profile')='object' AND c.payload->'online_profile'<>'{}'::jsonb AND ` + retentionCloudSourceRelationSQL + `))`

// retentionProtectedActorSQL 使用同一查询的$1固定截止时间，任一精确来源的成员新资料、有效手机或真实沟通都会阻止整份删除。
const retentionProtectedActorSQL = `EXISTS(SELECT 1 FROM auto_reply_cloud_entities c WHERE c.tenant_id=cp.tenant_id AND c.kind='candidates' AND ` + retentionCloudSourceRelationSQL + ` AND (
 regexp_replace(coalesce(nullif(c.payload->>'Phone',''),c.payload->>'phone',''),'[[:space:]()（）-]','','g') ~ '^(1[3-9][0-9]{9}|(\+|00)[1-9][0-9]{7,14}|861[3-9][0-9]{9})$'
 OR coalesce(nullif(c.payload#>>'{online_profile,updated_at}','')::timestamptz,nullif(c.updated_at,'')::timestamptz)>=$1
 OR EXISTS(SELECT 1 FROM auto_reply_cloud_entities cv JOIN auto_reply_cloud_entities m ON m.tenant_id=cv.tenant_id AND m.user_id=cv.user_id AND m.kind='messages' AND m.payload->>'conversation_id'=cv.id
 WHERE cv.tenant_id=c.tenant_id AND cv.user_id=c.user_id AND cv.kind='conversations' AND cv.payload->>'candidate_id'=c.id AND m.payload->>'direction' IN ('self','candidate') AND nullif(m.payload->>'platform_sent_at','')::timestamptz BETWEEN $1 AND now())))`
