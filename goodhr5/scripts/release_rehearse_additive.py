"""文件作用：为唯一获准的0097文章新增表迁移提供显式隔离验收规则；默认发布流程不启用这些例外。"""

import hashlib
import re

from release_cutover import require


MIGRATION = "0097_public_articles.sql"
NEW_TABLE = "public.public_articles"
LEDGER = "public.schema_migrations"
SNAPSHOT_LEDGER = "public\tschema_migrations"
SNAPSHOT_NEW_TABLE = "public\tpublic_articles"
EMPTY_SHA256 = hashlib.sha256(b"").hexdigest()
ARTICLE_COLUMNS = {"id", "title", "content", "published_at", "is_published", "created_at", "updated_at"}
ARTICLE_INDEXES = {"public_articles_pkey", "idx_public_articles_published_time", "idx_public_articles_admin_time"}


def additive_options(filename=None, digest=None):
    """additive_options 仅在文件名与固定摘要同时提供时允许这一个已审阅迁移。"""
    if filename is None and digest is None:
        return None
    require(filename == MIGRATION and isinstance(digest, str) and re.fullmatch(r"[0-9a-f]{64}", digest),
            "新增表演练仅允许明确指定0097_public_articles.sql及其完整SHA256")
    return {"filename": MIGRATION, "sha256": digest, "new_table": NEW_TABLE}


def assert_packaged_migrations(label, names, before_names, options, migration_bytes=None):
    """assert_packaged_migrations 分别绑定新镜像104文件与旧镜像原103文件，拒绝任何额外迁移或错内容。"""
    require(label in {"candidate", "rollback"} and isinstance(names, list) and len(names) == len(set(names)),
            "迁移镜像角色或文件列表不明确")
    require(before_names and len(before_names) == len(set(before_names)) and MIGRATION not in before_names,
            "新增表迁移已存在或原迁移基线不明确")
    expected = list(before_names) + ([MIGRATION] if label == "candidate" else [])
    require(sorted(names) == sorted(expected), "候选或回退镜像迁移集合与本轮明确基线不符")
    if label == "candidate":
        require(isinstance(migration_bytes, bytes) and hashlib.sha256(migration_bytes).hexdigest() == options["sha256"],
                "候选镜像内0097迁移内容与批准SHA256不符")


def ledger_map(rows):
    """ledger_map 保留每条账本的全部字段，拒绝重复文件名或缺失执行时间。"""
    require(isinstance(rows, list) and rows, "完整迁移账本基线为空")
    result = {}
    for row in rows:
        require(isinstance(row, dict) and isinstance(row.get("filename"), str)
                and isinstance(row.get("applied_at"), str) and row["applied_at"]
                and row["filename"] not in result, "迁移账本记录重复或不完整")
        result[row["filename"]] = row
    return result


def assert_added_ledger(before, after):
    """assert_added_ledger 精确保留旧103行及时间戳，仅允许新增指定0097一行。"""
    old, new = ledger_map(before), ledger_map(after)
    require(MIGRATION not in old and set(new) == set(old) | {MIGRATION}, "迁移账本并非仅追加指定0097一行")
    require(all(new[name] == row for name, row in old.items()), "旧迁移账本内容或执行时间发生变化")
    require(set(new[MIGRATION]) == set(next(iter(old.values()))), "新增账本行出现未批准字段")
    return True


def assert_added_tables(before, after, before_ledger, after_ledger):
    """assert_added_tables 精确保护48张旧业务表，单独审计账本追加，并要求唯一新表为空。"""
    require(isinstance(before, dict) and before and isinstance(after, dict) and SNAPSHOT_LEDGER in before,
            "新增表演练缺少完整旧表或账本基线")
    require(SNAPSHOT_NEW_TABLE not in before and set(after) == set(before) | {SNAPSHOT_NEW_TABLE}, "恢复库新增或缺失了未批准的表")
    changed = sorted(name for name in before if name != SNAPSHOT_LEDGER and before[name] != after[name])
    require(not changed, "恢复库旧业务表数据发生变化：" + ", ".join(changed))
    assert_added_ledger(before_ledger, after_ledger)
    require(before[SNAPSHOT_LEDGER]["rows"] == len(before_ledger) and after[SNAPSHOT_LEDGER]["rows"] == len(after_ledger),
            "账本整表行数与完整账本记录不一致")
    require(after[SNAPSHOT_NEW_TABLE] == {"rows": 0, "sha256": EMPTY_SHA256}, "新文章表不是空表")
    return True


def assert_added_schema(before, after):
    """assert_added_schema 拒绝旧DDL改变、额外表或视图函数序列等副作用，仅允许指定文章表及必要索引。"""
    require(isinstance(before, dict) and isinstance(after, dict) and set(before) == set(after), "数据库结构盘点范围变化")
    require("tables" in before and NEW_TABLE not in before["tables"]
            and set(after["tables"]) == set(before["tables"]) | {NEW_TABLE}, "结构盘点发现未批准的新表")
    require(all(after["tables"][name] == value for name, value in before["tables"].items()),
            "旧表字段、索引、约束、触发器、策略、所有者或注释发生变化")
    require(all(after[name] == value for name, value in before.items() if name != "tables"),
            "视图、函数、序列、类型、命名空间或扩展发生变化")
    article = after["tables"][NEW_TABLE]
    require(article.get("kind") == "r" and article.get("persistence") == "p"
            and article.get("partition_key") is None and not article.get("row_security")
            and not article.get("force_row_security"), "新增文章表类型或策略不符合批准范围")
    require({row.get("name") for row in article.get("columns", [])} == ARTICLE_COLUMNS
            and len(article["columns"]) == len(ARTICLE_COLUMNS), "文章表字段不符合批准范围")
    require(set(article.get("indexes", {})) == ARTICLE_INDEXES and not article.get("triggers")
            and not article.get("policies"), "文章表出现未批准索引、触发器或策略")
    return True


# 表结构不包含ACL：只读角色的临时授权单独审计；保留旧对象所有者，防止通过改所有权获得写权限。
SCHEMA_SQL = r"""
WITH app_namespaces AS (
 SELECT oid,nspname,nspowner FROM pg_namespace WHERE nspname !~ '^pg_' AND nspname<>'information_schema'
), app_relations AS (
 SELECT c.*,n.nspname FROM pg_class c JOIN app_namespaces n ON n.oid=c.relnamespace
), table_objects AS (
 SELECT r.nspname||'.'||r.relname AS name,jsonb_build_object(
 'kind',r.relkind,'persistence',r.relpersistence,'owner',pg_get_userbyid(r.relowner),
 'partition_key',pg_get_partkeydef(r.oid),'row_security',r.relrowsecurity,'force_row_security',r.relforcerowsecurity,
 'options',r.reloptions,'tablespace',r.reltablespace,'replica_identity',r.relreplident,
 'comment',obj_description(r.oid,'pg_class'),
 'columns',COALESCE((SELECT jsonb_agg(jsonb_build_object('name',a.attname,'type',format_type(a.atttypid,a.atttypmod),
   'not_null',a.attnotnull,'identity',a.attidentity,'generated',a.attgenerated,
   'default',pg_get_expr(d.adbin,d.adrelid),'comment',col_description(a.attrelid,a.attnum)) ORDER BY a.attnum)
   FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
   WHERE a.attrelid=r.oid AND a.attnum>0 AND NOT a.attisdropped),'[]'::jsonb),
 'indexes',COALESCE((SELECT jsonb_object_agg(i.relname,pg_get_indexdef(i.oid)) FROM pg_index x JOIN pg_class i ON i.oid=x.indexrelid WHERE x.indrelid=r.oid),'{}'::jsonb),
 'constraints',COALESCE((SELECT jsonb_object_agg(c.conname,pg_get_constraintdef(c.oid,true)) FROM pg_constraint c WHERE c.conrelid=r.oid),'{}'::jsonb),
 'triggers',COALESCE((SELECT jsonb_object_agg(t.tgname,jsonb_build_object('definition',pg_get_triggerdef(t.oid,true),'enabled',t.tgenabled)) FROM pg_trigger t WHERE t.tgrelid=r.oid AND NOT t.tgisinternal),'{}'::jsonb),
 'policies',COALESCE((SELECT jsonb_object_agg(p.polname,to_jsonb(p)-'oid'-'polrelid') FROM pg_policy p WHERE p.polrelid=r.oid),'{}'::jsonb)) AS value
 FROM app_relations r WHERE r.relkind IN ('r','p')
)
SELECT jsonb_build_object(
 'tables',COALESCE((SELECT jsonb_object_agg(name,value) FROM table_objects),'{}'::jsonb),
 'views',COALESCE((SELECT jsonb_object_agg(nspname||'.'||relname,jsonb_build_object('kind',relkind,'definition',pg_get_viewdef(oid,true),'owner',pg_get_userbyid(relowner),'comment',obj_description(oid,'pg_class'))) FROM app_relations WHERE relkind IN ('v','m')),'{}'::jsonb),
 'functions',COALESCE((SELECT jsonb_object_agg(n.nspname||'.'||p.proname||'('||pg_get_function_identity_arguments(p.oid)||')',jsonb_build_object('definition',pg_get_functiondef(p.oid),'owner',pg_get_userbyid(p.proowner),'comment',obj_description(p.oid,'pg_proc'))) FROM pg_proc p JOIN app_namespaces n ON n.oid=p.pronamespace WHERE p.prokind IN ('f','p','w')),'{}'::jsonb),
 'aggregates',COALESCE((SELECT jsonb_object_agg(n.nspname||'.'||p.proname||'('||pg_get_function_identity_arguments(p.oid)||')',jsonb_build_object('definition',to_jsonb(a)-'aggfnoid','owner',pg_get_userbyid(p.proowner))) FROM pg_proc p JOIN app_namespaces n ON n.oid=p.pronamespace JOIN pg_aggregate a ON a.aggfnoid=p.oid),'{}'::jsonb),
 'sequences',COALESCE((SELECT jsonb_object_agg(r.nspname||'.'||r.relname,jsonb_build_object('definition',to_jsonb(s)-'seqrelid','owner',pg_get_userbyid(r.relowner),'comment',obj_description(r.oid,'pg_class'))) FROM app_relations r JOIN pg_sequence s ON s.seqrelid=r.oid),'{}'::jsonb),
 'types',COALESCE((SELECT jsonb_object_agg(n.nspname||'.'||t.typname,jsonb_build_object('definition',to_jsonb(t)-'oid'-'typarray',
   'enum_labels',COALESCE((SELECT jsonb_agg(e.enumlabel ORDER BY e.enumsortorder) FROM pg_enum e WHERE e.enumtypid=t.oid),'[]'::jsonb),
   'constraints',COALESCE((SELECT jsonb_object_agg(c.conname,pg_get_constraintdef(c.oid,true)) FROM pg_constraint c WHERE c.contypid=t.oid),'{}'::jsonb)))
   FROM pg_type t JOIN app_namespaces n ON n.oid=t.typnamespace LEFT JOIN pg_type element ON element.oid=t.typelem WHERE t.typrelid=0 AND COALESCE(element.typrelid,0)=0),'{}'::jsonb),
 'namespaces',COALESCE((SELECT jsonb_object_agg(nspname,pg_get_userbyid(nspowner)) FROM app_namespaces),'{}'::jsonb),
 'extensions',COALESCE((SELECT jsonb_object_agg(extname,jsonb_build_object('version',extversion,'schema',n.nspname)) FROM pg_extension e JOIN pg_namespace n ON n.oid=e.extnamespace),'{}'::jsonb)
 , 'other_relations',COALESCE((SELECT jsonb_object_agg(nspname||'.'||relname,jsonb_build_object('kind',relkind,'owner',pg_get_userbyid(relowner))) FROM app_relations WHERE relkind NOT IN ('r','p','i','I','S','v','m')),'{}'::jsonb)
);
"""
