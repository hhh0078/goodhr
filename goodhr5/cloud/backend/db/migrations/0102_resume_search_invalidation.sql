-- 文件作用说明：通过持久待办异步删除已删除、更新或转移归属简历的旧搜索副本；不调用付费模型。
CREATE TABLE IF NOT EXISTS resume_search_invalidations (
    tenant_id uuid NOT NULL,
    candidate_id uuid NOT NULL,
    through_version bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id,candidate_id)
);
COMMENT ON TABLE resume_search_invalidations IS '简历旧搜索副本清理待办，无外键以保留删除后的清理意图';
COMMENT ON COLUMN resume_search_invalidations.tenant_id IS '旧副本所属团队，仅用于受控搜索清理';
COMMENT ON COLUMN resume_search_invalidations.candidate_id IS '需要清理旧副本的候选人编号';
COMMENT ON COLUMN resume_search_invalidations.through_version IS '仅删除不高于此版本的旧文档，避免误删新向量';
COMMENT ON COLUMN resume_search_invalidations.created_at IS '待办首次建立时间';

-- version_resume_ownership_change 为归属转移递增资料版本，保证旧租户或创建人的向量不能继续使用。
CREATE OR REPLACE FUNCTION version_resume_ownership_change() RETURNS trigger AS $$
BEGIN
    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id OR NEW.created_by_user_id IS DISTINCT FROM OLD.created_by_user_id THEN
        NEW.row_version := GREATEST(NEW.row_version,OLD.row_version+1);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
COMMENT ON FUNCTION version_resume_ownership_change() IS '转移团队或创建人时递增简历版本，使权限缓存和向量同步失效';
DROP TRIGGER IF EXISTS candidate_resume_ownership_version ON candidate_profiles;
CREATE TRIGGER candidate_resume_ownership_version BEFORE UPDATE OF tenant_id,created_by_user_id ON candidate_profiles
FOR EACH ROW EXECUTE FUNCTION version_resume_ownership_change();

-- enqueue_resume_search_invalidation 在业务事务内保留清理意图，搜索故障或服务重启不会丢失。
CREATE OR REPLACE FUNCTION enqueue_resume_search_invalidation() RETURNS trigger AS $$
BEGIN
    IF TG_OP='DELETE' OR NEW.row_version<>OLD.row_version OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id OR NEW.created_by_user_id IS DISTINCT FROM OLD.created_by_user_id THEN
        INSERT INTO resume_search_invalidations(tenant_id,candidate_id,through_version)
        VALUES(OLD.tenant_id,OLD.id,OLD.row_version)
        ON CONFLICT(tenant_id,candidate_id) DO UPDATE
        SET through_version=GREATEST(resume_search_invalidations.through_version,EXCLUDED.through_version);
        IF TG_OP='UPDATE' THEN
            UPDATE candidate_resume_vectors SET complete=false WHERE tenant_id=OLD.tenant_id AND candidate_id=OLD.id AND resume_version<=OLD.row_version;
        END IF;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
COMMENT ON FUNCTION enqueue_resume_search_invalidation() IS '在简历更新或删除的原事务中登记旧索引失效，后台只删旧版本';
DROP TRIGGER IF EXISTS candidate_resume_search_invalidation ON candidate_profiles;
CREATE TRIGGER candidate_resume_search_invalidation AFTER UPDATE OR DELETE ON candidate_profiles
FOR EACH ROW EXECUTE FUNCTION enqueue_resume_search_invalidation();
