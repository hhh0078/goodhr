"""文件作用：安全演练并发布 AI 充值赠送后端与 0092 钱包迁移，保留停机备份及不可逆切换证据。"""

import argparse
import copy
import fcntl
import hashlib
import importlib.util
import json
import os
import pathlib
import re
import shutil
import signal
import socket
import subprocess
import time
import urllib.error
import urllib.request


ROOT = pathlib.Path("/opt/goodhr-release/ai-recharge-backend-20261012-c9d4316a")
LAUNCHER = pathlib.Path("/opt/goodhr-release/hr-interview-20261010-63ad61cd/hr-release.py")
LAUNCHER_SHA256 = "f23375cf68107eb2ed21637d38b2462f7346c14b82ab9a3d069893ea700fef0c"
DATABASE = "goodhr_ai_recharge_verify"
CLONE = "goodhr-hr-db-ai-recharge-20261012"
CANDIDATE = "goodhr-ai-recharge-backend-candidate-20261012"
STANDBY = "goodhr-ai-recharge-backend-standby-20261012"
FORMAL = "goodhr-production-backend-1"
BACKUP = FORMAL + "-backup-ai-recharge-backend-20261012"
MIGRATION = "0092_ai_recharge_promotions.sql"
URL_MIGRATION = "0093_repair_builtin_ai_public_url.sql"
PENDING_MIGRATIONS = (MIGRATION, URL_MIGRATION)
UPDATE_URL = "https://goodhr.58it.cn/api/system/browser-extension"
UPDATE_SHA256 = "53d83f2e1f9cf859a1010990a714bf7d142e02f4eb46a486e0174b064d8c9653"
BASELINE = {
    FORMAL: ("e52f9d5750da4582f5279d8913397eceabe0a452b5656ff0c6b1fabd3b6695c2", "sha256:5257e7034c7b5f9ecd0877dfd06de80aa3d03ebcc14fe303af3ea2be054b954c", "2026-10-10T16:35:25.040978232Z"),
    "goodhr-production-frontend-1": ("0dbf4eb66895e56f563bc1e2b5f60cd90eb1fd91b70ef8f34173c9b7d403a766", "sha256:a5208e3e904a3acca657162f96c4dab49fab114f7ceb2208e0e69e9c186767ed", "2026-10-11T18:11:46.784509816Z"),
    "postgres": ("7ae7305d3a222272d23cf4a68ae302cf877fc71d2ce1fab9ad3a97b3fe02d89d", "sha256:413d57cb67bb3585acae8769a48661aa1dcac66076e3a5f88ea6c5be55f8916e", "2026-09-20T12:25:51.09539213Z"),
    "redis": ("78a704cd2eb89019c62bfb6ab2ceef603bf2cd293fe3d522ca7c132a1da72025", "sha256:646a47c903c5c45f99081094d8c3ba51ec2a1091babf42dc5a928cef024e6fcd", "2026-09-20T12:28:59.032970342Z"),
}
ALTERED_COLUMNS = {
    "payment_orders": ["bonus_amount_cents"],
    "payment_refunds": ["revoked_bonus_units", "rights_reserved"],
    "users": ["ai_bonus_balance_units", "ai_wallet_refund_block_reason", "ai_wallet_ledger_version"],
    "ai_balance_records": ["bonus_change_units", "bonus_balance_after_units", "wallet_ledger_version"],
}


def safe_existing(name):
    """按完整容器名查询唯一对象，Docker 错误不能伪装成对象不存在。"""
    try:
        result = subprocess.run(["docker", "inspect", name], stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30)
    except (OSError, subprocess.TimeoutExpired):
        raise RuntimeError("容器查询没有得到确定结果") from None
    if result.returncode:
        expected = ("Error: No such object: " + name).encode()
        if result.stdout.strip() == b"[]" and result.stderr.strip() == expected:
            return None
        raise RuntimeError("容器查询失败，不能按不存在处理")
    try:
        values = json.loads(result.stdout)
        if len(values) != 1 or values[0].get("Name") != "/" + name:
            raise ValueError
        return values[0]
    except (ValueError, TypeError, KeyError, json.JSONDecodeError):
        raise RuntimeError("容器查询结果身份不明确") from None


def launcher():
    """核对已审查启动器原文字节后加载，并限定本轮隔离库及精确容器查询。"""
    if LAUNCHER.is_symlink() or hashlib.sha256(LAUNCHER.read_bytes()).hexdigest() != LAUNCHER_SHA256:
        raise RuntimeError("复用启动器摘要不一致")
    spec = importlib.util.spec_from_file_location("reviewed_ai_recharge_launcher", LAUNCHER)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    module.VERIFY_DATABASE = DATABASE
    module.existing = safe_existing
    return module


def sql(hr, container, database, query, readonly=True, timeout=960):
    """只允许正式库或本轮隔离库执行 SQL，并为历史重放提供十五分钟上限。"""
    if (container, database) not in (("postgres", "goodhr5"), (CLONE, DATABASE)):
        raise RuntimeError("SQL 数据库目标不在本轮白名单")
    options = "-c statement_timeout=900000 -c lock_timeout=30000"
    if readonly:
        options += " -c default_transaction_read_only=on"
    command = ["docker", "exec", "-i", "-e", "PGOPTIONS=" + options, container, "sh", "-c",
               'exec psql -X -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$1" -t -A', "ai-recharge-release-sql", database]
    return hr.run(command, data=query.encode("utf-8"), timeout=timeout).decode("utf-8").strip()


def parse_json_output(value):
    """从事务命令输出中提取唯一 JSON 行，避免把 BEGIN 等状态文本当成业务结果。"""
    rows = [line for line in value.splitlines() if line.startswith("{") or line.startswith("[")]
    if len(rows) != 1:
        raise RuntimeError("数据库校验没有返回唯一 JSON")
    return json.loads(rows[0])


def ledger(hr, container, database):
    """读取完整迁移文件名顺序，发布判断始终使用文件名而非数字前缀。"""
    return sql(hr, container, database, "SELECT filename FROM schema_migrations ORDER BY filename;").splitlines()


def tables(hr, container, database):
    """读取 public 表清单，用于确认迁移只增加钱包分笔表。"""
    value = sql(hr, container, database, "SELECT COALESCE(json_agg(tablename ORDER BY tablename),'[]'::json) FROM pg_tables WHERE schemaname='public';")
    return json.loads(value)


def quote(value):
    """把受控文本转换为 PostgreSQL 字符串常量。"""
    return "'" + str(value).replace("'", "''") + "'"


def identifier(value):
    """只引用由数据库表清单返回的普通标识符。"""
    if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", value):
        raise RuntimeError("表名不符合受控标识符规则")
    return '"' + value + '"'


def fingerprint(hr, container, database, names, strip_new=False):
    """在一个只读快照内计算表行数与确定性摘要，可忽略 0092 新列比较全部旧字段。"""
    queries = []
    for name in sorted(names):
        expression = "to_jsonb(t)"
        if strip_new and name in ALTERED_COLUMNS:
            columns = ",".join(quote(column) for column in ALTERED_COLUMNS[name])
            expression += "-ARRAY[" + columns + "]::text[]"
        if strip_new and name == "user_ai_configs":
            expression = "to_jsonb(t)||jsonb_build_object('base_url',CASE WHEN lower(trim(t.base_url))='https://goodhr5.58it.cn/api/ai-compatible/v1/chat/completions' AND left(t.api_key_encrypted,5)='ghai_' THEN 'https://goodhr.58it.cn/api/ai-compatible/v1/chat/completions' ELSE t.base_url END)"
        if strip_new and name == "system_configs":
            condition = "t.config_key='system.app_config' AND lower(trim(COALESCE(t.config_value#>>'{builtin_ai,public_base_url}',''))) IN ('','https://goodhr5.58it.cn/api/ai-compatible/v1/chat/completions','https://goodhr.58it.cn/api/ai-compatible/v1/chat/completions')"
            repaired = "jsonb_set(COALESCE(t.config_value,'{}'::jsonb),'{builtin_ai}',COALESCE(t.config_value->'builtin_ai','{}'::jsonb)||jsonb_build_object('public_base_url','https://goodhr.58it.cn/api/ai-compatible/v1/chat/completions'),true)"
            expression = "CASE WHEN " + condition + " THEN to_jsonb(t)||jsonb_build_object('config_value'," + repaired + ") ELSE to_jsonb(t) END"
        hashed = "md5((" + expression + ")::text)"
        value = "jsonb_build_object('rows',count(*),'hash',md5(COALESCE(string_agg(%s,'' ORDER BY %s),'')))" % (hashed, hashed)
        queries.append("SELECT %s AS name,%s AS value FROM %s t" % (quote(name), value, identifier(name)))
    if not queries:
        return {}
    query = "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SELECT jsonb_object_agg(name,value) FROM (" + " UNION ALL ".join(queries) + ") q; COMMIT;"
    return parse_json_output(sql(hr, container, database, query))


def schema_signature(hr, container, database):
    """计算四张改表及钱包表的列、约束、索引和注释摘要，证明重复迁移不会改结构。"""
    query = r"""
WITH wanted(name) AS (VALUES ('payment_orders'),('payment_refunds'),('users'),('ai_balance_records'),('ai_wallet_lots')),
parts AS (
 SELECT 'column:'||c.table_name||':'||c.ordinal_position||':'||c.column_name||':'||c.data_type||':'||c.is_nullable||':'||COALESCE(c.column_default,'') AS value FROM information_schema.columns c JOIN wanted w ON w.name=c.table_name WHERE c.table_schema='public'
 UNION ALL SELECT 'index:'||tablename||':'||indexname||':'||indexdef FROM pg_indexes i JOIN wanted w ON w.name=i.tablename WHERE schemaname='public'
 UNION ALL SELECT 'constraint:'||t.relname||':'||c.conname||':'||pg_get_constraintdef(c.oid) FROM pg_constraint c JOIN pg_class t ON t.oid=c.conrelid JOIN pg_namespace n ON n.oid=t.relnamespace JOIN wanted w ON w.name=t.relname WHERE n.nspname='public'
 UNION ALL SELECT 'comment:'||c.relname||':'||COALESCE(a.attname,'')||':'||d.description FROM pg_description d JOIN pg_class c ON c.oid=d.objoid JOIN pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_attribute a ON a.attrelid=c.oid AND a.attnum=d.objsubid JOIN wanted w ON w.name=c.relname WHERE n.nspname='public'
)
SELECT json_build_object('count',count(*),'hash',md5(COALESCE(string_agg(value,'' ORDER BY value),''))) FROM parts;
"""
    return json.loads(sql(hr, container, database, query))


def verify_host(hr):
    """核对固定生产主机和公网地址，避免在错误服务器执行容器或数据库动作。"""
    if socket.gethostname() != "ldclouda31539" or "43.228.79.108" not in hr.run(["hostname", "-I"]).decode().split():
        raise RuntimeError("生产服务器身份不一致")


def verify_baseline(hr, backend=True):
    """核对正式容器完整 ID、镜像、启动时间与运行状态。"""
    verify_host(hr)
    for name, expected in BASELINE.items():
        if not backend and name == FORMAL:
            continue
        item = hr.inspect(name)
        if (item["Id"], item["Image"], item["State"]["StartedAt"]) != expected or not item["State"]["Running"]:
            raise RuntimeError("生产容器基线变化：" + name)


def verify_supporting(hr):
    """切换期间只核对前端、PostgreSQL 与 Redis 未发生替换或重启。"""
    verify_baseline(hr, backend=False)


def fetch_update_hash():
    """读取公开客户端更新配置并返回完整响应摘要，不打印配置正文。"""
    with urllib.request.urlopen(UPDATE_URL, timeout=20) as response:
        if response.status != 200:
            raise RuntimeError("客户端更新配置 HTTP 状态异常")
        return hashlib.sha256(response.read()).hexdigest()


def material_proof(hr, commit=None):
    """核对构建证明、新旧迁移正文和正式 ledger，确保唯一待执行项为 0092。"""
    build = hr.load(ROOT, "backend-build-result.private.json")
    expected_commit = commit or build.get("commit")
    if not re.fullmatch(r"[0-9a-f]{40}", expected_commit or "") or build.get("commit") != expected_commit:
        raise RuntimeError("构建提交不符合本轮目标")
    image_id = build.get("image_id", "")
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", image_id):
        raise RuntimeError("候选后端不是完整不可变镜像 ID")
    image = hr.inspect(image_id, image=True)
    if image["Id"] != image_id or (image["Config"].get("Labels") or {}).get("org.opencontainers.image.revision") != expected_commit:
        raise RuntimeError("候选镜像身份或提交标签不一致")
    old = hr.image_migrations(BASELINE[FORMAL][1])
    new = hr.image_migrations(image_id)
    if set(new) != set(old) | set(PENDING_MIGRATIONS) or any(new.get(name) != text for name, text in old.items()):
        raise RuntimeError("候选镜像除本轮 0092、0093 外改变了历史迁移")
    source = hr.migrations_from_tar((ROOT / "source.tar").read_bytes(), "goodhr5/cloud/backend/db/migrations/")
    extracted_migrations = ROOT / "source/goodhr5/cloud/backend/db/migrations"
    if (source != new or hr.digest(extracted_migrations / MIGRATION) != build.get("migration_0092_sha256")
            or hr.digest(extracted_migrations / URL_MIGRATION) != build.get("migration_0093_sha256")):
        raise RuntimeError("源码、镜像和构建证明中的迁移正文不一致")
    applied = ledger(hr, "postgres", "goodhr5")
    if len(applied) != 98 or applied != sorted(old):
        raise RuntimeError("正式库不是已审定的 98 项迁移基线")
    return {"commit": expected_commit, "image_id": image_id, "image_tag": build.get("image_tag"),
            "old_migrations": old, "new_migrations": new, "ledger": applied}


def check_materials(hr):
    """在演练和切换前重新核对镜像、迁移、备份与准备清单没有漂移。"""
    manifest = hr.load(ROOT, "manifest.private.json")
    proof = material_proof(hr, manifest["commit"])
    if proof["image_id"] != manifest["backend_image"] or proof["ledger"] != manifest["ledger"]:
        raise RuntimeError("准备后的镜像或 ledger 证明发生变化")
    if hr.digest(ROOT / "database.dump") != manifest["dump"]["sha256"]:
        raise RuntimeError("演练数据库备份摘要发生变化")
    for name in PENDING_MIGRATIONS:
        path = ROOT / ("migration-" + name.split("_", 1)[0] + ".private.sql")
        if hashlib.sha256(path.read_bytes()).hexdigest() != manifest["migration_sha256"][name]:
            raise RuntimeError("冻结的迁移正文摘要发生变化：" + name)
    return manifest


def migration_sql(manifest):
    """把精确 ledger 守卫、0092 正文及 ledger 追加组成单一事务。"""
    expected = quote(json.dumps(sorted(manifest["ledger"]))) + "::jsonb"
    texts = [(ROOT / ("migration-" + name.split("_", 1)[0] + ".private.sql")).read_text(encoding="utf-8") for name in PENDING_MIGRATIONS]
    statements = [
        "BEGIN;",
        "SELECT pg_advisory_xact_lock(hashtext('goodhr-ai-recharge-0092-release'));",
        "LOCK TABLE schema_migrations IN EXCLUSIVE MODE;",
        "DO $guard$ BEGIN IF (SELECT COALESCE(jsonb_agg(filename ORDER BY filename),'[]'::jsonb) FROM schema_migrations) <> " + expected + " THEN RAISE EXCEPTION 'migration ledger changed'; END IF; IF to_regclass('public.ai_wallet_lots') IS NOT NULL OR EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND ((table_name='users' AND column_name='ai_wallet_ledger_version') OR (table_name='payment_orders' AND column_name='bonus_amount_cents'))) THEN RAISE EXCEPTION '0092 schema already exists without ledger'; END IF; END; $guard$;",
    ]
    statements.extend(texts)
    statements.append("INSERT INTO schema_migrations(filename) VALUES " + ",".join("(" + quote(name) + ")" for name in PENDING_MIGRATIONS) + ";")
    statements.append("COMMIT;")
    return "\n".join(statements)


def invariant_report(hr, container, database):
    """核对钱包版本、余额、赠送缓存、充值分笔和活跃退款的资金不变量。"""
    query = r"""
WITH lots AS (
 SELECT user_id,COALESCE(sum(remaining_principal_units),0) principal,COALESCE(sum(remaining_bonus_units),0) bonus FROM ai_wallet_lots GROUP BY user_id
), bad_recharge AS (
 SELECT count(*) value FROM ai_wallet_lots l LEFT JOIN payment_orders o ON o.order_no=l.order_no
 WHERE l.source_category='recharge' AND (l.order_no='' OR o.order_no IS NULL OR o.user_id<>l.user_id OR o.order_type<>'ai_balance' OR o.status<>'paid' OR l.principal_units<>o.amount_cents::bigint*100 OR l.bonus_units<>o.bonus_amount_cents::bigint*100)
), blocked AS (
 SELECT ai_wallet_refund_block_reason reason,count(*) value FROM users WHERE ai_wallet_refund_block_reason<>'' GROUP BY ai_wallet_refund_block_reason
)
SELECT json_build_object(
 'users_not_version_1',(SELECT count(*) FROM users WHERE ai_wallet_ledger_version<>1),
 'bonus_cache_mismatches',(SELECT count(*) FROM users u LEFT JOIN lots l ON l.user_id=u.id WHERE u.ai_bonus_balance_units<>COALESCE(l.bonus,0)),
 'wallet_total_mismatches',(SELECT count(*) FROM users u LEFT JOIN lots l ON l.user_id=u.id WHERE COALESCE(l.principal,0)+COALESCE(l.bonus,0)<>GREATEST(u.ai_balance_units,0)),
 'bad_lots',(SELECT count(*) FROM ai_wallet_lots WHERE principal_units<0 OR bonus_units<0 OR remaining_principal_units<0 OR remaining_bonus_units<0 OR remaining_principal_units>principal_units OR remaining_bonus_units>bonus_units),
 'bad_recharge_lots',(SELECT value FROM bad_recharge),
 'unsafe_open_refunds',(SELECT count(*) FROM payment_refunds WHERE refund_type='ai_balance' AND status IN('pending','unknown','processing','action_required','success') AND NOT rights_reserved),
 'legacy_builtin_ai_urls',(SELECT count(*) FROM user_ai_configs WHERE lower(trim(base_url))='https://goodhr5.58it.cn/api/ai-compatible/v1/chat/completions' AND left(api_key_encrypted,5)='ghai_'),
 'invalid_system_builtin_url',(SELECT CASE WHEN COALESCE(trim(config_value#>>'{builtin_ai,public_base_url}'),'')='' OR lower(trim(config_value#>>'{builtin_ai,public_base_url}'))='https://goodhr5.58it.cn/api/ai-compatible/v1/chat/completions' THEN 1 ELSE 0 END FROM system_configs WHERE config_key='system.app_config'),
 'wallet_lots',(SELECT count(*) FROM ai_wallet_lots),
 'blocked_users',(SELECT count(*) FROM users WHERE ai_wallet_refund_block_reason<>''),
 'blocked_reasons',COALESCE((SELECT json_object_agg(reason,value) FROM blocked),'{}'::json)
);
"""
    report = json.loads(sql(hr, container, database, query))
    for key in ("users_not_version_1", "bonus_cache_mismatches", "wallet_total_mismatches", "bad_lots", "bad_recharge_lots", "unsafe_open_refunds", "legacy_builtin_ai_urls", "invalid_system_builtin_url"):
        if report[key] != 0:
            raise RuntimeError("钱包迁移不变量未通过：" + key)
    return report


def verify_migrated(hr, container, database, manifest, before):
    """验证 0092 唯一追加、旧字段及其他表不变，并检查全部钱包资金不变量。"""
    expected_ledger = sorted(manifest["ledger"] + list(PENDING_MIGRATIONS))
    if ledger(hr, container, database) != expected_ledger:
        raise RuntimeError("迁移 ledger 没有恰好追加 0092")
    if set(tables(hr, container, database)) != set(manifest["tables"]) | {"ai_wallet_lots"}:
        raise RuntimeError("迁移后的 public 表范围不一致")
    after = fingerprint(hr, container, database, set(manifest["tables"]) - {"schema_migrations"}, strip_new=True)
    if after != before:
        raise RuntimeError("0092 改变了旧字段或非受影响表")
    schema = schema_signature(hr, container, database)
    columns = int(sql(hr, container, database, "SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND ((table_name='payment_orders' AND column_name IN('bonus_amount_cents')) OR (table_name='payment_refunds' AND column_name IN('revoked_bonus_units','rights_reserved')) OR (table_name='users' AND column_name IN('ai_bonus_balance_units','ai_wallet_refund_block_reason','ai_wallet_ledger_version')) OR (table_name='ai_balance_records' AND column_name IN('bonus_change_units','bonus_balance_after_units','wallet_ledger_version')));"))
    if columns != 9:
        raise RuntimeError("0092 的九个新增字段不完整")
    return {"old_tables_verified": len(before), "old_columns_unchanged": True,
            "ledger_count": len(expected_ledger), "schema": schema,
            "invariants": invariant_report(hr, container, database)}


def dump_database(hr, target):
    """生成自包含 custom 格式全库备份，校验 PGDMP 头、目录及摘要后原子就位。"""
    partial = target.with_suffix(target.suffix + ".partial")
    if partial.exists() or target.exists():
        raise RuntimeError("数据库备份目标已存在，禁止覆盖")
    with partial.open("xb") as output:
        os.chmod(partial, 0o600)
        hr.run(["docker", "exec", "-i", "postgres", "sh", "-c", 'exec pg_dump -Fc --no-owner --no-acl -U "$POSTGRES_USER" -d goodhr5'], output=output, timeout=1200)
        output.flush()
        os.fsync(output.fileno())
    with partial.open("rb") as stream:
        if stream.read(5) != b"PGDMP":
            raise RuntimeError("数据库备份不是 PGDMP 格式")
    with partial.open("rb") as stream:
        catalog = hr.run(["docker", "exec", "-i", "postgres", "pg_restore", "--list"], input_file=stream, timeout=300)
    if not catalog:
        raise RuntimeError("数据库备份目录为空")
    partial.replace(target)
    return {"bytes": target.stat().st_size, "sha256": hr.digest(target), "catalog_lines": len(catalog.splitlines())}


def prepare(hr, args):
    """只读核对材料与生产基线，生成新鲜全库备份并冻结发布清单。"""
    verify_baseline(hr)
    if any((ROOT / name).exists() for name in ("manifest.private.json", "database.dump", "before-inspect.private.json")):
        raise RuntimeError("已有准备材料，禁止覆盖")
    proof = material_proof(hr, args.commit)
    if fetch_update_hash() != UPDATE_SHA256:
        raise RuntimeError("客户端更新配置在发布前发生变化")
    old_tables = tables(hr, "postgres", "goodhr5")
    if "ai_wallet_lots" in old_tables:
        raise RuntimeError("正式库已出现 0092 表但 ledger 尚未记录")
    database_bytes = int(sql(hr, "postgres", "goodhr5", "SELECT pg_database_size(current_database());"))
    if shutil.disk_usage(ROOT).free < database_bytes + 1024 ** 3:
        raise RuntimeError("根盘空间不足以备份并恢复演练库")
    backup = dump_database(hr, ROOT / "database.dump")
    migration_hashes = {}
    for name in PENDING_MIGRATIONS:
        migration_text = proof["new_migrations"][name]
        migration_path = ROOT / ("migration-" + name.split("_", 1)[0] + ".private.sql")
        migration_path.write_text(migration_text, encoding="utf-8")
        migration_path.chmod(0o600)
        migration_hashes[name] = hashlib.sha256(migration_text.encode()).hexdigest()
    current = [hr.inspect(name) for name in BASELINE]
    manifest = {"commit": proof["commit"], "backend_image": proof["image_id"], "backend_tag": proof["image_tag"],
                "ledger": proof["ledger"], "tables": old_tables, "database_bytes": database_bytes,
                "migration_sha256": migration_hashes, "dump": backup,
                "update_sha256": UPDATE_SHA256, "baseline": BASELINE}
    hr.save(ROOT, "before-inspect.private.json", current)
    hr.save(ROOT, "manifest.private.json", manifest)
    # 在停止正式服务前验证完整 Config/HostConfig 能创建新后端载荷。
    hr.application_payload(ROOT, "backend", proof["image_id"], candidate=False)
    verify_baseline(hr)
    print(json.dumps({"prepared": True, "commit": proof["commit"], "pending": list(PENDING_MIGRATIONS), "dump": backup}, ensure_ascii=False))


def verify_ports():
    """确认隔离 PostgreSQL 和候选后端端口没有被占用。"""
    for port in (55441, 8089):
        with socket.socket() as probe:
            try:
                probe.bind(("127.0.0.1", port))
            except OSError:
                raise RuntimeError("隔离演练端口已占用：" + str(port)) from None


def wait_postgres(hr, container_id):
    """有限等待隔离 PostgreSQL 就绪。"""
    for _ in range(45):
        result = subprocess.run(["docker", "exec", container_id, "pg_isready", "-U", "postgres"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5)
        if result.returncode == 0:
            return
        time.sleep(1)
    raise RuntimeError("隔离 PostgreSQL 未在限定时间内就绪")


def verify_candidate_http(hr):
    """只做候选健康和匿名权限检查，不登录、不创建订单、不修改余额。"""
    hr.wait_http("http://127.0.0.1:8089/health")
    for path in ("/api/ai-wallet", "/api/ai-wallet/records", "/api/payment/orders", "/api/payment/refund"):
        hr.wait_http("http://127.0.0.1:8089" + path, 401)


def rehearse(hr):
    """恢复完整备份到隔离 PG，执行迁移、幂等复跑及只读候选后端验证。"""
    verify_baseline(hr)
    manifest = check_materials(hr)
    if (ROOT / "rehearsal.private.json").exists() or (ROOT / "rehearsal-pgdata").exists():
        raise RuntimeError("已有隔离演练现场，禁止覆盖")
    verify_ports()
    if shutil.disk_usage(ROOT).free < manifest["database_bytes"] + 512 * 1024 ** 2:
        raise RuntimeError("根盘空间不足以恢复隔离数据库")
    data = ROOT / "rehearsal-pgdata"
    data.mkdir(mode=0o755)
    data.chmod(0o755)
    state = {"database_container": CLONE, "database_id": None, "database": DATABASE,
             "candidate_name": CANDIDATE, "candidate_id": None, "cleaned": False, "last_operation": "create_clone"}
    hr.save(ROOT, "rehearsal.private.json", state)
    if safe_existing(CLONE) or safe_existing(CANDIDATE):
        raise RuntimeError("隔离容器名称已被占用")
    pg_id = hr.run(["docker", "create", "--name", CLONE, "--restart", "no", "--label", "goodhr.release=" + ROOT.name,
                    "--label", "goodhr.release_kind=database", "-p", "127.0.0.1:55441:5432", "-e", "POSTGRES_USER=postgres",
                    "-e", "POSTGRES_HOST_AUTH_METHOD=trust", "-e", "PGDATA=/var/lib/postgresql/isolated", "--mount",
                    "type=bind,src=" + str(data) + ",dst=/var/lib/postgresql", BASELINE["postgres"][1]]).decode().strip()
    state["database_id"] = pg_id
    state["last_operation"] = "start_clone"
    hr.save(ROOT, "rehearsal.private.json", state)
    hr.start_container(pg_id)
    wait_postgres(hr, pg_id)
    hr.run(["docker", "exec", pg_id, "createdb", "-U", "postgres", DATABASE])
    state["last_operation"] = "restore_clone"
    hr.save(ROOT, "rehearsal.private.json", state)
    with (ROOT / "database.dump").open("rb") as stream:
        hr.run(["docker", "exec", "-i", pg_id, "pg_restore", "--exit-on-error", "--no-owner", "--no-acl", "-U", "postgres", "-d", DATABASE], input_file=stream, timeout=1200)
    if ledger(hr, CLONE, DATABASE) != manifest["ledger"] or tables(hr, CLONE, DATABASE) != manifest["tables"]:
        raise RuntimeError("隔离恢复库与备份基线不一致")
    old_names = set(manifest["tables"]) - {"schema_migrations"}
    before = fingerprint(hr, CLONE, DATABASE, old_names, strip_new=True)
    state["last_operation"] = "migrate_clone"
    hr.save(ROOT, "rehearsal.private.json", state)
    sql(hr, CLONE, DATABASE, migration_sql(manifest), readonly=False)
    report = verify_migrated(hr, CLONE, DATABASE, manifest, before)
    all_names = set(manifest["tables"]) | {"ai_wallet_lots"}
    repeat_before = fingerprint(hr, CLONE, DATABASE, all_names)
    repeat_schema = schema_signature(hr, CLONE, DATABASE)
    state["last_operation"] = "repeat_0092_clone"
    hr.save(ROOT, "rehearsal.private.json", state)
    repeat_sql = "BEGIN;\n" + "\n".join((ROOT / ("migration-" + name.split("_", 1)[0] + ".private.sql")).read_text(encoding="utf-8") for name in PENDING_MIGRATIONS) + "\nCOMMIT;"
    sql(hr, CLONE, DATABASE, repeat_sql, readonly=False)
    if fingerprint(hr, CLONE, DATABASE, all_names) != repeat_before or schema_signature(hr, CLONE, DATABASE) != repeat_schema:
        raise RuntimeError("0092 重复执行改变了隔离库数据或结构")
    invariant_report(hr, CLONE, DATABASE)
    state["last_operation"] = "create_candidate"
    hr.save(ROOT, "rehearsal.private.json", state)
    candidate_id = hr.create_application(ROOT, "backend", CANDIDATE, manifest["backend_image"], candidate=True)
    state["candidate_id"] = candidate_id
    state["last_operation"] = "start_candidate"
    hr.save(ROOT, "rehearsal.private.json", state)
    hr.start_container(candidate_id)
    verify_candidate_http(hr)
    candidate = hr.inspect(CANDIDATE)
    if candidate["Id"] != candidate_id or not candidate["State"]["Running"] or candidate["RestartCount"] != 0:
        raise RuntimeError("候选后端运行身份或重启次数异常")
    if fingerprint(hr, CLONE, DATABASE, all_names) != repeat_before:
        raise RuntimeError("候选后端只读检查期间隔离库发生写入")
    verify_baseline(hr)
    state["last_operation"] = "verified"
    hr.save(ROOT, "rehearsal.private.json", state)
    hr.save(ROOT, "rehearsal-result.private.json", {"ready": True, "commit": manifest["commit"], "report": report,
                                                        "repeat_idempotent": True, "candidate_readonly": True})
    print(json.dumps({"rehearsal": "passed", "commit": manifest["commit"], "report": report}, ensure_ascii=False))


def migration_state(hr, manifest):
    """根据正式 ledger 分类迁移状态；查询失败或其他组合一律视为不确定。"""
    try:
        value = ledger(hr, "postgres", "goodhr5")
    except BaseException:
        return "unknown"
    if value == manifest["ledger"]:
        return "absent"
    if value == sorted(manifest["ledger"] + list(PENDING_MIGRATIONS)):
        return "committed"
    return "unknown"


def recognize_release_container(hr, name, container_id, image_id, kind):
    """核对本轮容器 ID、镜像与发布标签，避免误停误删其他容器。"""
    item = hr.inspect(name)
    labels = item["Config"].get("Labels") or {}
    if item["Id"] != container_id or item["Image"] != image_id or labels.get("goodhr.release") != ROOT.name or labels.get("goodhr.release_kind") != kind:
        raise RuntimeError("本轮容器身份不一致：" + name)
    return item


def verify_production_http(hr):
    """核对新后端健康、匿名权限边界及正式订阅相关页面。"""
    hr.wait_http("http://127.0.0.1:8086/health")
    for path in ("/api/ai-wallet", "/api/ai-wallet/records", "/api/payment/orders", "/api/payment/refund"):
        hr.wait_http("http://127.0.0.1:8086" + path, 401)
    for path in ("/login", "/admin/subscription", "/download"):
        hr.wait_http("https://goodhr.58it.cn" + path)


def restore_before_migration(hr, state, manifest):
    """仅在正式 ledger 明确未提交 0092 时删除待命容器并恢复旧后端。"""
    if migration_state(hr, manifest) != "absent":
        raise RuntimeError("0092 状态不是明确未提交，禁止启动旧后端")
    standby = safe_existing(STANDBY)
    if standby:
        recognize_release_container(hr, STANDBY, state.get("standby_id"), manifest["backend_image"], "backend")
        if standby["State"]["Running"]:
            hr.run(["docker", "stop", "-t", "20", standby["Id"]], timeout=45)
        hr.run(["docker", "rm", standby["Id"]])
    old = safe_existing(FORMAL)
    if not old:
        saved = safe_existing(BACKUP)
        if not saved or saved["Id"] != BASELINE[FORMAL][0]:
            raise RuntimeError("旧后端备份身份不一致")
        hr.run(["docker", "rename", saved["Id"], FORMAL])
        old = hr.inspect(FORMAL)
    if old["Id"] != BASELINE[FORMAL][0] or old["Image"] != BASELINE[FORMAL][1]:
        raise RuntimeError("旧后端身份不一致")
    if not old["State"]["Running"]:
        hr.start_container(old["Id"])
    hr.wait_http("http://127.0.0.1:8086/health")
    state["rolled_back_pre_migration"] = True
    state["last_operation"] = "pre_migration_restore_complete"
    hr.save(ROOT, "cutover-progress.private.json", state)


def forward_recovery(hr, state, manifest):
    """迁移已提交且验证通过时只恢复新后端服务，绝不启动旧镜像。"""
    if migration_state(hr, manifest) != "committed" or not state.get("migration_verified"):
        raise RuntimeError("迁移尚未完成验证，保留后端停机现场")
    current = safe_existing(FORMAL)
    if current and current["Id"] == BASELINE[FORMAL][0]:
        if current["State"]["Running"]:
            hr.run(["docker", "stop", "-t", "20", current["Id"]], timeout=45)
        if safe_existing(BACKUP):
            raise RuntimeError("旧后端备份名称已占用")
        hr.run(["docker", "rename", current["Id"], BACKUP])
        current = None
    standby = safe_existing(STANDBY)
    if current is None and standby:
        recognize_release_container(hr, STANDBY, state["standby_id"], manifest["backend_image"], "backend")
        hr.run(["docker", "rename", standby["Id"], FORMAL])
        current = hr.inspect(FORMAL)
    if not current:
        current = hr.inspect(FORMAL)
    recognize_release_container(hr, FORMAL, state["standby_id"], manifest["backend_image"], "backend")
    if not current["State"]["Running"]:
        hr.start_container(current["Id"])
    verify_production_http(hr)
    state["forward_recovered"] = True
    state["last_operation"] = "forward_recovery_complete"
    hr.save(ROOT, "cutover-progress.private.json", state)


def cutover(hr):
    """先创建待命新后端，再停旧后端备份、迁移并切换；提交后只允许向前恢复。"""
    verify_baseline(hr)
    manifest = check_materials(hr)
    rehearsal = hr.load(ROOT, "rehearsal-result.private.json")
    if not rehearsal.get("ready") or rehearsal.get("commit") != manifest["commit"]:
        raise RuntimeError("缺少当前提交的完整隔离演练")
    if (ROOT / "cutover-progress.private.json").exists():
        raise RuntimeError("已有正式切换进度，禁止盲目重跑")
    if fetch_update_hash() != manifest["update_sha256"]:
        raise RuntimeError("客户端更新配置在切换前发生变化")
    if ledger(hr, "postgres", "goodhr5") != manifest["ledger"] or tables(hr, "postgres", "goodhr5") != manifest["tables"]:
        raise RuntimeError("正式数据库基线在切换前发生变化")
    if safe_existing(STANDBY) or safe_existing(BACKUP):
        raise RuntimeError("待命或备份容器名称已占用")
    state = {"standby_id": None, "old_stopped": False, "migration_state": "not_started",
             "migration_verified": False, "complete": False, "last_operation": "create_standby"}
    hr.save(ROOT, "cutover-progress.private.json", state)
    try:
        standby_id = hr.create_application(ROOT, "backend", STANDBY, manifest["backend_image"], candidate=False)
        state["standby_id"] = standby_id
        state["last_operation"] = "standby_created"
        hr.save(ROOT, "cutover-progress.private.json", state)
        recognize_release_container(hr, STANDBY, standby_id, manifest["backend_image"], "backend")
        state["last_operation"] = "stop_old_backend"
        hr.save(ROOT, "cutover-progress.private.json", state)
        hr.run(["docker", "stop", "-t", "20", BASELINE[FORMAL][0]], timeout=45)
        state["old_stopped"] = True
        state["last_operation"] = "backup_stopped_database"
        hr.save(ROOT, "cutover-progress.private.json", state)
        cutover_dump = dump_database(hr, ROOT / "database-cutover.dump")
        state["cutover_dump"] = cutover_dump
        hr.save(ROOT, "cutover-progress.private.json", state)
        if ledger(hr, "postgres", "goodhr5") != manifest["ledger"] or tables(hr, "postgres", "goodhr5") != manifest["tables"]:
            raise RuntimeError("停机备份后正式数据库基线变化")
        before = fingerprint(hr, "postgres", "goodhr5", set(manifest["tables"]) - {"schema_migrations"}, strip_new=True)
        hr.save(ROOT, "production-before-migration-fingerprint.private.json", before)
        state["migration_state"] = "in_progress"
        state["last_operation"] = "migrate_production"
        hr.save(ROOT, "cutover-progress.private.json", state)
        sql(hr, "postgres", "goodhr5", migration_sql(manifest), readonly=False)
        state["migration_state"] = "committed"
        state["last_operation"] = "verify_production_migration"
        hr.save(ROOT, "cutover-progress.private.json", state)
        report = verify_migrated(hr, "postgres", "goodhr5", manifest, before)
        state["migration_verified"] = True
        hr.save(ROOT, "cutover-progress.private.json", state)
        state["last_operation"] = "preserve_old_backend"
        hr.save(ROOT, "cutover-progress.private.json", state)
        hr.run(["docker", "rename", BASELINE[FORMAL][0], BACKUP])
        state["last_operation"] = "promote_standby"
        hr.save(ROOT, "cutover-progress.private.json", state)
        hr.run(["docker", "rename", standby_id, FORMAL])
        state["last_operation"] = "start_new_backend"
        hr.save(ROOT, "cutover-progress.private.json", state)
        hr.start_container(standby_id)
        verify_production_http(hr)
        verify_supporting(hr)
        current = recognize_release_container(hr, FORMAL, standby_id, manifest["backend_image"], "backend")
        if current["RestartCount"] != 0 or not current["State"]["Running"]:
            raise RuntimeError("新正式后端发生重启或没有运行")
        report["post_start_invariants"] = invariant_report(hr, "postgres", "goodhr5")
        if fetch_update_hash() != manifest["update_sha256"]:
            raise RuntimeError("客户端更新配置在发布后发生变化")
        report["cutover_dump"] = cutover_dump
        state["complete"] = True
        state["last_operation"] = "complete"
        hr.save(ROOT, "cutover-progress.private.json", state)
        hr.save(ROOT, "deployment-result.private.json", {"complete": True, "commit": manifest["commit"],
                                                           "backend_container": standby_id, "backend_image": manifest["backend_image"],
                                                           "old_backend": BASELINE[FORMAL][0], "report": report})
        print(json.dumps({"cutover": "complete", "commit": manifest["commit"], "backend_container": standby_id, "report": report}, ensure_ascii=False))
    except BaseException as cause:
        committed = migration_state(hr, manifest)
        state["observed_migration_state"] = committed
        state["failure"] = type(cause).__name__
        hr.save(ROOT, "cutover-progress.private.json", state)
        if committed == "absent":
            restore_before_migration(hr, state, manifest)
            raise RuntimeError("正式切换未完成，0092 未提交，旧后端已安全恢复") from cause
        if committed == "committed" and state.get("migration_verified"):
            try:
                forward_recovery(hr, state, manifest)
            except BaseException as recovery:
                raise RuntimeError("0092 已提交，向前恢复未完成；旧后端保持停止，禁止启动") from recovery
            raise RuntimeError("切换检查未全部通过，但 0092 已提交且新后端已向前恢复；禁止旧后端回退") from cause
        raise RuntimeError("0092 已提交或状态不确定；旧后端保持停止，禁止启动和数据库自动恢复") from cause


def rollback(hr):
    """只允许在正式 ledger 明确没有 0092 时恢复旧后端；提交后永远拒绝回退旧代码。"""
    verify_supporting(hr)
    manifest = check_materials(hr)
    state = hr.load(ROOT, "cutover-progress.private.json")
    if migration_state(hr, manifest) != "absent":
        raise RuntimeError("0092 已提交或状态不确定，禁止启动旧后端")
    restore_before_migration(hr, state, manifest)
    print(json.dumps({"rollback": "complete", "migration_committed": False, "old_backend_restored": True}))


def cleanup(hr):
    """仅删除本轮隔离候选与恢复库，保留演练报告、正式备份和旧后端容器。"""
    state = hr.load(ROOT, "rehearsal.private.json")
    manifest = hr.load(ROOT, "manifest.private.json")
    candidate = safe_existing(CANDIDATE)
    if candidate:
        recognize_release_container(hr, CANDIDATE, state.get("candidate_id"), manifest["backend_image"], "backend")
        if candidate["State"]["Running"]:
            hr.run(["docker", "stop", "-t", "20", candidate["Id"]], timeout=45)
        hr.run(["docker", "rm", candidate["Id"]])
    clone = safe_existing(CLONE)
    if clone:
        labels = clone["Config"].get("Labels") or {}
        if clone["Id"] != state.get("database_id") or clone["Image"] != BASELINE["postgres"][1] or labels.get("goodhr.release") != ROOT.name:
            raise RuntimeError("隔离数据库身份不一致，拒绝清理")
        if clone["State"]["Running"]:
            hr.run(["docker", "stop", "-t", "20", clone["Id"]], timeout=45)
        hr.run(["docker", "rm", clone["Id"]])
    data = (ROOT / "rehearsal-pgdata").resolve()
    if data.exists():
        if data.parent != ROOT.resolve() or data.name != "rehearsal-pgdata":
            raise RuntimeError("隔离数据目录不在固定发布目录")
        shutil.rmtree(data)
    state["cleaned"] = True
    state["last_operation"] = "cleaned"
    hr.save(ROOT, "rehearsal.private.json", state)
    print(json.dumps({"cleanup": "complete", "candidate_removed": True, "clone_removed": True}))


def interrupted(signum, frame):
    """把终止信号转为普通失败，让切换保护逻辑判断迁移是否已经提交。"""
    raise RuntimeError("收到终止信号，进入受保护状态核对")


def main():
    """锁定固定根盘发布目录，并按 prepare、rehearse、cutover、rollback、cleanup 执行。"""
    parser = argparse.ArgumentParser(description="AI 充值赠送后端生产发布")
    parser.add_argument("operation", choices=["prepare", "rehearse", "cutover", "rollback", "cleanup"])
    parser.add_argument("--commit")
    args = parser.parse_args()
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    os.umask(0o077)
    ROOT.mkdir(parents=True, mode=0o700, exist_ok=True)
    ROOT.chmod(0o700)
    if ROOT.is_symlink() or ROOT.resolve() != ROOT or ROOT.parent != pathlib.Path("/opt/goodhr-release") or os.stat(ROOT).st_dev != os.stat("/").st_dev:
        raise RuntimeError("发布目录必须是根盘固定目录")
    with (ROOT / "release.lock").open("a+b") as lock:
        fcntl.flock(lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        hr = launcher()
        if args.operation == "prepare":
            if not args.commit:
                raise RuntimeError("prepare 必须提供完整目标提交")
            prepare(hr, args)
        elif args.operation == "rehearse":
            rehearse(hr)
        elif args.operation == "cutover":
            cutover(hr)
        elif args.operation == "rollback":
            rollback(hr)
        else:
            cleanup(hr)


if __name__ == "__main__":
    try:
        main()
    except Exception as cause:
        print(json.dumps({"success": False, "error": str(cause)}, ensure_ascii=False))
        raise SystemExit(1)
