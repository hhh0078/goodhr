# 文件作用：为简历库发布提供可审查的备份、独立恢复演练、事务迁移、应用切换和受保护回退；本文件需人工执行，不自行构建或上传镜像。
import argparse
import fcntl
import hashlib
import io
import json
import os
import pathlib
import re
import shutil
import signal
import socket
import subprocess
import tarfile
import time
import urllib.error
import urllib.parse
import urllib.request

BASELINE = {
    "goodhr-production-backend-1": ("cd42e5abc3ac68ab33ec1e7822a695396ddb165666e8433e5eadeba7cc146a43", "sha256:7c506d2925a6e7e2c73d0b1eaf1a19ea41161ef21e808fe1e325d2db964ffb42", "2026-10-10T14:41:06.989789389Z"),
    "goodhr-production-frontend-1": ("acb4627d7aaf02ed014ac9ce0890733c194f043107afd827e66b7e06dd790708", "sha256:c813ad4f08100dc2ca4d1ed599689c4aa47d5e34193c044de30f9ff7ba86c846", "2026-10-10T14:41:17.060158067Z"),
    "postgres": ("7ae7305d3a222272d23cf4a68ae302cf877fc71d2ce1fab9ad3a97b3fe02d89d", "sha256:413d57cb67bb3585acae8769a48661aa1dcac66076e3a5f88ea6c5be55f8916e", "2026-09-20T12:25:51.09539213Z"),
    "redis": ("78a704cd2eb89019c62bfb6ab2ceef603bf2cd293fe3d522ca7c132a1da72025", "sha256:646a47c903c5c45f99081094d8c3ba51ec2a1091babf42dc5a928cef024e6fcd", "2026-09-20T12:28:59.032970342Z"),
}
ALLOWED = {"0084_candidate_library_upgrade.sql", "0085_browser_extension_release_config.sql", "0086_candidate_notes.sql",
           "0087_user_messages.sql", "0088_resume_library_settings.sql", "0089_candidate_shares.sql"}
NEW_FEATURE_TABLES = ("user_message_state", "messages", "user_message_receipts", "resume_library_settings", "candidate_shares")
PUBLIC_FIELDS = {"version", "note", "url_chrome", "sha256_chrome", "url_firefox", "sha256_firefox", "firefox_signed"}
STATS = """SELECT json_build_object('database',current_database(),'database_bytes',pg_database_size(current_database()),
 'profiles',(SELECT count(*) FROM candidate_profiles),'events',(SELECT count(*) FROM candidate_events),
 'engagements',(SELECT count(*) FROM candidate_engagements),
 'legacy_notes',(SELECT count(*) FROM candidate_events WHERE event_type='manual_note'),
 'notes',(SELECT count(*) FROM candidate_notes),
 'missing_legacy_notes',(SELECT count(*) FROM candidate_events e WHERE e.event_type='manual_note'
  AND NOT EXISTS(SELECT 1 FROM candidate_notes n WHERE n.id=e.id)));
"""
FINGERPRINTS = """SELECT json_build_object(
 'profiles', (SELECT md5(COALESCE(string_agg(md5(((to_jsonb(t)-ARRAY['expected_salary_min','expected_salary_max']) ||
 jsonb_build_object('expected_salary_min',trim_scale(expected_salary_min::numeric),'expected_salary_max',trim_scale(expected_salary_max::numeric)))::text),'' ORDER BY id),'')) FROM candidate_profiles t),
 'events', (SELECT md5(COALESCE(string_agg(md5(to_jsonb(t)::text),'' ORDER BY id),'')) FROM candidate_events t),
 'engagements', (SELECT md5(COALESCE(string_agg(md5(to_jsonb(t)::text),'' ORDER BY id),'')) FROM candidate_engagements t));
"""


def run(args, data=None, output=None, timeout=180):
    """使用参数列表执行命令并隐藏异常输出，避免泄露环境、业务正文和数据库凭证。"""
    result = subprocess.run(args, input=data, stdout=output or subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)
    if result.returncode:
        raise RuntimeError("命令执行失败，详细输出未公开；程序=%s exit=%s" % (args[0], result.returncode))
    return result.stdout


def inspect(name, image=False):
    """在内存读取完整容器或镜像信息，调用者只公开白名单字段。"""
    return json.loads(run(["docker", "image", "inspect", name] if image else ["docker", "inspect", name]))[0]


def digest(path):
    """流式计算文件摘要，用于验证归档、dump和恢复材料。"""
    result = hashlib.sha256()
    with pathlib.Path(path).open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            result.update(block)
    return result.hexdigest()


def save(root, name, value):
    """以私有权限原子保存发布状态，不输出私有文件内容。"""
    target = root / name
    temporary = root / (name + ".tmp")
    temporary.write_text(json.dumps(value, ensure_ascii=False, indent=2), encoding="utf-8")
    temporary.chmod(0o600)
    temporary.replace(target)


def load(root, name):
    """读取当前发布目录的结构化恢复材料。"""
    return json.loads((root / name).read_text(encoding="utf-8"))


def quote(value):
    """将受控值转为SQL文本常量；不拼接未经验证的SQL标识符。"""
    return "'" + str(value).replace("'", "''") + "'"


def sql(container, database, query, readonly=True):
    """通过容器本地管理角色执行指定库SQL，生产默认强制只读；SQL和错误正文均不打印。"""
    if container == "postgres" and database != "goodhr5":
        raise RuntimeError("生产容器只允许目标goodhr5")
    if container != "postgres" and not (container.startswith("goodhr-resume-db-") and database.startswith("goodhr_resume_verify_")):
        raise RuntimeError("独立恢复库名称不符合约束")
    flags = "-c statement_timeout=120000 -c lock_timeout=10000"
    if readonly:
        flags += " -c default_transaction_read_only=on"
    command = ['docker', 'exec', '-i', '-e', 'PGOPTIONS=' + flags, container, 'sh', '-c',
               'exec psql -X -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$1" -t -A', 'goodhr-release-sql', database]
    return run(command, query.encode(), timeout=180).decode().strip()


def ledger(container, database):
    """读取完整迁移文件名，避免同编号的不同业务迁移混淆。"""
    return sql(container, database, "SELECT filename FROM schema_migrations ORDER BY filename;").splitlines()


def fingerprints(container, database):
    """聚合现有业务行的确定性摘要，保留全部人工字段，仅统一薪资数值文本格式，不输出个体正文。"""
    return json.loads(sql(container, database, FINGERPRINTS))


def extension_snapshot(container, database):
    """读取插件整行快照用于比较，包含更新时刻；结果仅保存在私有材料或内存。"""
    return json.loads(sql(container, database, "SELECT COALESCE((SELECT row_to_json(s) FROM system_configs s WHERE config_key='system.browser_extension'),'null'::json);"))


def new_feature_snapshot(container, database):
    """只返回审定新增表的数量与确定性行摘要，用于验证回退及重迁移没有删除新增业务资料。"""
    expressions = []
    for table in NEW_FEATURE_TABLES:
        expressions += [quote(table), "(SELECT json_build_object('rows',count(*),'fingerprint',md5(COALESCE(string_agg(md5(to_jsonb(t)::text),'' ORDER BY md5(to_jsonb(t)::text)),''))) FROM " + table + " t)"]
    return json.loads(sql(container, database, "SELECT json_build_object(" + ",".join(expressions) + ");"))


def seed_isolated_new_features(root, container, database):
    """仅在独立恢复库新增合成消息、设置和分享，用真实克隆父键满足外键，不触碰原候选、事件和触达表。"""
    if container == "postgres" or not container.startswith("goodhr-resume-db-") or not database.startswith("goodhr_resume_verify_") or database == "goodhr5":
        raise RuntimeError("新增表合成资料只允许独立恢复库")
    suffix = hashlib.sha256(root.name.encode()).hexdigest()
    message_id = "00000000-0000-4000-8000-" + suffix[:12]
    key = "release-rehearsal:" + root.name
    commands = """BEGIN;
DO $fixture_guard$ BEGIN IF NOT EXISTS(SELECT 1 FROM users) OR NOT EXISTS(SELECT 1 FROM candidate_profiles)
 THEN RAISE EXCEPTION 'isolated fixture parents missing'; END IF; END; $fixture_guard$;
INSERT INTO messages(id,user_id,title,body,message_type,action_url)
 SELECT {message_id}::uuid,id,'简历库发布隔离演练','合成内容，仅用于验证新增表在回退后保留','release_test','' FROM users ORDER BY id LIMIT 1;
INSERT INTO user_message_state(user_id,unread_count,version)
 SELECT user_id,1,1 FROM messages WHERE id={message_id}::uuid
 ON CONFLICT(user_id) DO UPDATE SET unread_count=user_message_state.unread_count+1,version=user_message_state.version+1,updated_at=now();
INSERT INTO user_message_receipts(user_id,idempotency_key,fingerprint,message_id)
 SELECT user_id,{fixture_key},{fixture_hash},id FROM messages WHERE id={message_id}::uuid;
INSERT INTO resume_library_settings(user_id,auto_import_downloads)
 SELECT user_id,false FROM messages WHERE id={message_id}::uuid
 ON CONFLICT(user_id) DO UPDATE SET auto_import_downloads=false,updated_at=now();
INSERT INTO candidate_shares(token_hash,tenant_id,candidate_id,user_email)
 SELECT {fixture_hash},tenant_id,id,'release-test@example.invalid' FROM candidate_profiles ORDER BY id LIMIT 1;
COMMIT;
""".format(message_id=quote(message_id), fixture_key=quote(key), fixture_hash=quote(suffix))
    sql(container, database, commands, readonly=False)
    result = new_feature_snapshot(container, database)
    if any(item["rows"] < 1 for item in result.values()):
        raise RuntimeError("独立新增表合成资料未建立")
    return result


def migrations_from_tar(contents, prefix):
    """只提取归档内的正向SQL正文，拒绝链接、越界路径和重复文件名。"""
    result = {}
    with tarfile.open(fileobj=io.BytesIO(contents)) as archive:
        for member in archive.getmembers():
            name = pathlib.PurePosixPath(member.name)
            if name.is_absolute() or ".." in name.parts or member.issym() or member.islnk():
                raise RuntimeError("归档含不安全路径")
            if member.isfile() and str(name).startswith(prefix) and name.name.endswith(".sql") and not name.name.endswith(".down.sql"):
                if name.name in result:
                    raise RuntimeError("归档含重复迁移名")
                result[name.name] = archive.extractfile(member).read().replace(b"\r\n", b"\n").decode("utf-8")
    return result


def image_migrations(image):
    """只执行镜像中的tar命令读取SQL，不启动后端，也不接触生产数据库。"""
    contents = run(["docker", "run", "--rm", "--network", "none", "--entrypoint", "tar", image, "-cf", "-", "-C", "/app/db/migrations", "."])
    return migrations_from_tar(contents, "")


def approved_pending(previous, candidate, source, applied):
    """按当前镜像完整文件名核对历史正文，允许六个迁移已全部执行，拒绝额外迁移或改写历史。"""
    if set(applied) != set(previous) or any(candidate.get(name) != text for name, text in previous.items()) or candidate != source:
        raise RuntimeError("已执行迁移正文、完整文件名集合或镜像与源码归档不一致")
    pending = set(candidate) - set(previous)
    if pending != ALLOWED - set(previous) or not ALLOWED.issubset(set(candidate)):
        raise RuntimeError("待执行迁移不是审定六文件中尚未部署的部分")
    return sorted(pending)


def verify_baseline(root=None, apps=True):
    """核对服务器身份及原容器ID、镜像、启动时间；数据库和Redis任何漂移均阻止操作。"""
    if socket.gethostname() != "ldclouda31539" or "43.228.79.108" not in run(["hostname", "-I"]).decode().split():
        raise RuntimeError("服务器身份不匹配")
    expected = BASELINE if root is None else {item['Name'].lstrip('/'): (item['Id'], item['Image'], item['State']['StartedAt']) for item in load(root, "before-inspect.private.json")}
    result = []
    for name, values in expected.items():
        if not apps and name.startswith("goodhr-production-"):
            continue
        item = inspect(name)
        if (item["Id"], item["Image"], item["State"]["StartedAt"]) != tuple(values) or not item["State"]["Running"]:
            raise RuntimeError("正式容器基线发生变化：" + name)
        result.append(item)
    return result


def verify_release_config(config, frontend_image):
    """核对公开配置和镜像内最终下载文件的SHA256，允许Firefox暂不公开但必须同步清空地址与摘要。"""
    if set(config) != PUBLIC_FIELDS or not re.fullmatch(r"\d+\.\d+\.\d+", config["version"]):
        raise RuntimeError("release.json字段或统一版本号无效")
    if not isinstance(config["note"], str) or not isinstance(config["firefox_signed"], bool):
        raise RuntimeError("插件公开配置类型无效")
    for browser in ["chrome", "firefox"]:
        url, checksum = config["url_" + browser], config["sha256_" + browser]
        if not url and not checksum and browser == "firefox" and not config["firefox_signed"]:
            continue
        parsed = urllib.parse.urlsplit(url)
        if parsed.scheme and (parsed.scheme != "https" or parsed.netloc != "goodhr.58it.cn"):
            raise RuntimeError("插件地址不属于正式官网")
        if parsed.query or parsed.fragment or not re.fullmatch(r"/downloads/[A-Za-z0-9._-]+", parsed.path) or not re.fullmatch(r"[0-9a-f]{64}", checksum):
            raise RuntimeError("插件下载地址或摘要无效")
        contents = run(["docker", "run", "--rm", "--network", "none", "--entrypoint", "cat", frontend_image, "/app/public" + parsed.path])
        if hashlib.sha256(contents).hexdigest() != checksum:
            raise RuntimeError("镜像内插件下载文件摘要不匹配")


def prepare(root, args):
    """备份实时容器、旧公开配置和数据库，核对最终源码与镜像，仅新增私有发布材料。"""
    current = verify_baseline()
    if (root / "manifest.json").exists():
        raise RuntimeError("发布材料已存在，禁止覆盖；后续使用rehearse/cutover/rollback")
    if shutil.disk_usage(root).free < 2 * 1024 ** 3:
        raise RuntimeError("根盘空间不足2GB")
    if not re.fullmatch(r"[0-9a-f]{40}", args.commit or ""):
        raise RuntimeError("必须提供完整最终提交")
    if not args.archive or not args.archive_sha256 or digest(args.archive) != args.archive_sha256:
        raise RuntimeError("源码归档摘要不匹配")
    archive = pathlib.Path(args.archive)
    if archive.stat().st_size > 300 * 1024 ** 2:
        raise RuntimeError("源码归档超过300MB")
    source = migrations_from_tar(archive.read_bytes(), "goodhr5/cloud/backend/db/migrations/")
    images = {}
    for kind, tag in [("backend", args.backend_image), ("frontend", args.frontend_image)]:
        item = inspect(tag, image=True)
        if (item["Config"].get("Labels") or {}).get("org.opencontainers.image.revision") != args.commit:
            raise RuntimeError("镜像OCI提交与发布提交不一致")
        images[kind] = {"id": item["Id"], "tag": tag}
    candidate = image_migrations(images["backend"]["id"])
    previous = image_migrations(BASELINE["goodhr-production-backend-1"][1])
    applied = ledger("postgres", "goodhr5")
    pending = approved_pending(previous, candidate, source, applied)
    numeric_columns = sql("postgres", "goodhr5", "SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='candidate_profiles' AND column_name IN('expected_salary_min','expected_salary_max') AND data_type='numeric';")
    baseline_salary_numeric = "0084_candidate_library_upgrade.sql" in applied
    if not baseline_salary_numeric or numeric_columns != "2":
        raise RuntimeError("805fce9f生产基线必须已执行0084且两个薪资列均为numeric")
    config = json.loads(pathlib.Path(args.release_json).read_text(encoding="utf-8-sig"))
    verify_release_config(config, images["frontend"]["id"])
    old_config = extension_snapshot("postgres", "goodhr5")
    save(root, "before-inspect.private.json", current)
    save(root, "before-extension-config.private.json", old_config)
    for kind in ["backend", "frontend"]:
        item = next(value for value in current if value["Name"] == "/goodhr-production-" + kind + "-1")
        values = item["Config"].get("Env") or []
        if any("\n" in value or "\r" in value for value in values):
            raise RuntimeError("容器环境含多行值，需审查")
        path = root / (kind + ".private.env")
        path.write_text("\n".join(values) + "\n", encoding="utf-8")
        path.chmod(0o600)
    shutil.copyfile(archive, root / "source.tar")
    (root / "source.tar").chmod(0o600)
    if digest(root / "source.tar") != args.archive_sha256:
        raise RuntimeError("源码归档复制后摘要变化")
    save(root, "migrations.private.json", {name: candidate[name] for name in sorted(ALLOWED)})
    save(root, "release.json", config)
    manifest = {"commit": args.commit, "images": images, "ledger": applied, "pending": pending, "source_sha256": args.archive_sha256,
                "stats": json.loads(sql("postgres", "goodhr5", STATS)), "rollback_backend": None,
                "baseline_salary_numeric": baseline_salary_numeric, "baseline_revision": "805fce9f8b848f1f5f46e09befa5a53c4959db50"}
    if args.rollback_backend_image:
        bridge = inspect(args.rollback_backend_image, image=True)
        labels = bridge["Config"].get("Labels") or {}
        if labels.get("goodhr.numeric_salary_compatible") != "true" or not re.fullmatch(r"[0-9a-f]{40}", labels.get("org.opencontainers.image.revision", "")):
            raise RuntimeError("兼容回退镜像缺少已审定的小数薪资兼容标签和提交")
        manifest["rollback_backend"] = {"id": bridge["Id"], "revision": labels["org.opencontainers.image.revision"]}
    dump = root / "database.dump.partial"
    if dump.exists() or (root / "database.dump").exists():
        raise RuntimeError("数据库备份已存在，禁止覆盖")
    with dump.open("xb") as output:
        run(['docker', 'exec', 'postgres', 'sh', '-c', 'exec pg_dump -U "$POSTGRES_USER" -d goodhr5 -Fc --no-owner --no-acl'], output=output, timeout=900)
    dump.chmod(0o600)
    catalog = run(["docker", "exec", "-i", "postgres", "pg_restore", "--list"], dump.read_bytes())
    with dump.open("rb") as header_stream:
        header = header_stream.read(5)
    if header != b"PGDMP" or not catalog:
        raise RuntimeError("数据库备份未通过目录校验，禁止发布")
    dump.replace(root / "database.dump")
    manifest["dump"] = {"bytes": (root / "database.dump").stat().st_size, "sha256": digest(root / "database.dump")}
    save(root, "manifest.json", manifest)
    verify_baseline(root)
    print(json.dumps({"prepared": str(root), "commit": args.commit, "dump": manifest["dump"], "stats": manifest["stats"], "pending": pending}))


def migration_sql(root, expected_ledger, expected_config=None):
    """将全部增量DDL、对应完整文件名记录和插件公开配置合为同一事务，未知配置字段保留。"""
    texts = load(root, "migrations.private.json")
    desired = load(root, "release.json")
    manifest = load(root, "manifest.json")
    if not set(expected_ledger).issubset(set(manifest["ledger"]) | ALLOWED):
        raise RuntimeError("迁移重跑基线包含未经审定的记录")
    if expected_config is None:
        expected_config = load(root, "before-extension-config.private.json")
    expected = quote(json.dumps(sorted(expected_ledger))) + "::jsonb"
    expected_config_json = quote(json.dumps(expected_config, ensure_ascii=False)) + "::jsonb"
    commands = ["BEGIN; LOCK TABLE schema_migrations IN EXCLUSIVE MODE; LOCK TABLE system_configs IN SHARE ROW EXCLUSIVE MODE; SELECT pg_advisory_xact_lock(hashtext('goodhr-resume-release'));",
                "DO $ledger_guard$ BEGIN IF (SELECT COALESCE(jsonb_agg(filename ORDER BY filename),'[]'::jsonb) FROM schema_migrations) <> " + expected + " THEN RAISE EXCEPTION 'migration ledger changed'; END IF; END; $ledger_guard$;",
                "DO $old_config_guard$ BEGIN IF COALESCE((SELECT to_jsonb(s) FROM system_configs s WHERE config_key='system.browser_extension'),'null'::jsonb) IS DISTINCT FROM " + expected_config_json + " THEN RAISE EXCEPTION 'extension config snapshot changed'; END IF; END; $old_config_guard$;"]
    for name in sorted(texts):
        if name in expected_ledger:
            continue
        text = texts[name]
        if re.search(r"(?im)^\s*(BEGIN|COMMIT|ROLLBACK)\s*;", text):
            raise RuntimeError("迁移自身包含事务控制，无法安全组合")
        commands += [text, "INSERT INTO schema_migrations(filename) VALUES (" + quote(name) + ");"]
    commands += ["INSERT INTO system_configs(config_key,config_value,description,enabled) VALUES ('system.browser_extension','{}'::jsonb,'浏览器插件下载公开配置',true) ON CONFLICT(config_key) DO NOTHING;",
                 "UPDATE system_configs SET config_value=COALESCE(config_value,'{}'::jsonb)||" + quote(json.dumps(desired, ensure_ascii=False)) + "::jsonb, enabled=true,updated_at=now() WHERE config_key='system.browser_extension';",
                 "DO $config_guard$ BEGIN IF NOT EXISTS(SELECT 1 FROM system_configs WHERE config_key='system.browser_extension') THEN RAISE EXCEPTION 'extension config missing'; END IF; END; $config_guard$; COMMIT;"]
    return "\n".join(commands)


def verify_migrated(root, container, database, before, before_fingerprints):
    """只验证聚合数量、原备注完整性、字段类型和公开配置，不公开任何候选人内容。"""
    after = json.loads(sql(container, database, STATS))
    if any(after[key] != before[key] for key in ["profiles", "events", "engagements", "legacy_notes"]):
        raise RuntimeError("迁移改变原候选人或事件总量")
    if fingerprints(container, database) != before_fingerprints:
        raise RuntimeError("迁移改变原候选人、事件或触达正文摘要")
    missing = int(sql(container, database, "SELECT count(*) FROM candidate_events e LEFT JOIN candidate_notes n ON n.id=e.id WHERE e.event_type='manual_note' AND (n.id IS NULL OR n.content IS DISTINCT FROM e.message_text OR n.created_at IS DISTINCT FROM e.created_at OR n.occurred_at IS DISTINCT FROM e.created_at);"))
    if missing:
        raise RuntimeError("旧人工备注正文或时间未完整保留")
    types = sql(container, database, "SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='candidate_profiles' AND column_name IN('expected_salary_min','expected_salary_max') AND data_type='numeric';")
    if types != "2" or set(ledger(container, database)) != set(load(root, "manifest.json")["ledger"]) | ALLOWED:
        raise RuntimeError("迁移字段类型或ledger未完整提交")
    config = json.loads(sql(container, database, "SELECT json_build_object('enabled',enabled,'value',config_value) FROM system_configs WHERE config_key='system.browser_extension';"))
    if not config["enabled"] or any(config["value"].get(key) != value for key, value in load(root, "release.json").items()):
        raise RuntimeError("公开插件配置未按最终release.json更新")
    old = load(root, "before-extension-config.private.json")
    if old and any(config["value"].get(key) != value for key, value in old["config_value"].items() if key not in PUBLIC_FIELDS):
        raise RuntimeError("插件原未知配置字段被改变")
    notes = int(sql(container, database, "SELECT count(*) FROM candidate_notes;"))
    expected_notes = before["notes"] + before["missing_legacy_notes"]
    if notes != expected_notes:
        raise RuntimeError("独立备注总量与0086实际迁入条件不一致")
    return {"stats": after, "fingerprints": before_fingerprints, "legacy_notes_missing": missing, "notes": notes, "numeric_columns": 2, "release_config": "verified", "new_feature_tables": new_feature_snapshot(container, database)}


def wait_http(url, expected=200):
    """在短超时内验证服务状态，不发送登录、候选人写入或真实用户操作。"""
    for _ in range(30):
        try:
            with urllib.request.urlopen(url, timeout=2) as response:
                if response.status == expected:
                    return
        except urllib.error.HTTPError as error:
            if error.code == expected:
                return
        except (OSError, TimeoutError):
            pass
        time.sleep(1)
    raise RuntimeError("HTTP状态验证未通过：" + url)


def launch(root, kind, name, image, candidate=False, database=None):
    """正式服务沿用原网络、私有环境、挂载和端口；候选后端仅连接独立恢复库且不挂生产目录。"""
    originals = load(root, "before-inspect.private.json")
    old = next(item for item in originals if item["Name"] == "/goodhr-production-" + kind + "-1")
    host = old["HostConfig"]
    args = ["docker", "run", "-d", "--name", name, "--network", host["NetworkMode"], "--restart", "no" if candidate else host["RestartPolicy"]["Name"]]
    if candidate and kind == "backend":
        if not database or not database.startswith("goodhr_resume_verify_"):
            raise RuntimeError("候选后端必须连接独立库")
        for key, value in {"GOODHR_PG_DSN": "host=127.0.0.1 port=55439 user=postgres dbname=" + database + " sslmode=disable", "GOODHR_CLOUD_ADDR": "127.0.0.1:8089", "GOODHR_PUBLIC_BASE_URL": "https://goodhr.58it.cn", "GOODHR_DISABLE_EMAIL_SCHEDULER": "true", "GOODHR_REDIS_ADDR": "", "GOODHR_REDIS_PASSWORD": "", "GOODHR_SMTP_HOST": "", "GOODHR_SMTP_USERNAME": "", "GOODHR_SMTP_PASSWORD": "", "GOODHR_CLOUD_LOG_FILE": "logs/isolated-release.log"}.items():
            args += ["-e", key + "=" + value]
        for directory in ["uploads", "logs"]:
            path = root / ("candidate-" + directory)
            path.mkdir(mode=0o700, exist_ok=True)
            args += ["--mount", "type=bind,src=" + str(path) + ",dst=/app/" + directory]
    else:
        args += ["--env-file", str(root / (kind + ".private.env"))]
        for mount in old["Mounts"]:
            if mount["Type"] != "bind":
                raise RuntimeError("原应用出现未经审查的非bind挂载")
            args += ["--mount", "type=bind,src=" + mount["Source"] + ",dst=" + mount["Destination"] + (",readonly" if not mount["RW"] else "")]
    for port, bindings in (host.get("PortBindings") or {}).items():
        for binding in bindings:
            published = "5178" if candidate and kind == "frontend" else binding["HostPort"]
            args += ["-p", binding.get("HostIp", "") + ":" + published + ":" + port]
    for key, value in host["LogConfig"].get("Config", {}).items():
        args += ["--log-opt", key + "=" + value]
    args += ["--log-driver", host["LogConfig"]["Type"]]
    for flag, value in [("--user", old["Config"].get("User")), ("--workdir", old["Config"].get("WorkingDir"))]:
        if value:
            args += [flag, value]
    new = inspect(image, image=True)
    if (old["Config"].get("Entrypoint") or []) != (new["Config"].get("Entrypoint") or []):
        raise RuntimeError("新旧应用镜像Entrypoint不同")
    labels = dict(old["Config"].get("Labels") or {})
    if candidate:
        labels = {key: value for key, value in labels.items() if not key.startswith("com.docker.compose.")}
    elif "com.docker.compose.image" in labels:
        labels["com.docker.compose.image"] = new["Id"]
    labels.update({"goodhr.release": root.name, "goodhr.source_commit": load(root, "manifest.json")["commit"],
                   "org.opencontainers.image.revision": (new["Config"].get("Labels") or {}).get("org.opencontainers.image.revision", "")})
    for key, value in labels.items():
        args += ["--label", key + "=" + value]
    for flag, value in [("--memory", host.get("Memory")), ("--memory-reservation", host.get("MemoryReservation")), ("--memory-swap", host.get("MemorySwap")),
                        ("--cpu-shares", host.get("CpuShares")), ("--cpu-period", host.get("CpuPeriod")), ("--cpu-quota", host.get("CpuQuota")),
                        ("--cpuset-cpus", host.get("CpusetCpus")), ("--cpuset-mems", host.get("CpusetMems")), ("--pids-limit", host.get("PidsLimit"))]:
        if value:
            args += [flag, str(value)]
    if host.get("NanoCpus"):
        args += ["--cpus", str(host["NanoCpus"] / 1e9)]
    for option in host.get("SecurityOpt") or []:
        args += ["--security-opt", option]
    for flag, enabled in [("--read-only", host.get("ReadonlyRootfs")), ("--privileged", host.get("Privileged"))]:
        if enabled:
            args += [flag]
    for flag, key in [("--cap-add", "CapAdd"), ("--cap-drop", "CapDrop")]:
        for value in host.get(key) or []:
            args += [flag, value]
    for limit in host.get("Ulimits") or []:
        args += ["--ulimit", limit["Name"] + "=" + str(limit["Soft"]) + ":" + str(limit["Hard"])]
    if host.get("ShmSize"):
        args += ["--shm-size", str(host["ShmSize"])]
    for flag, key in [("--ipc", "IpcMode"), ("--pid", "PidMode")]:
        if host.get(key) and host[key] not in ["private", ""]:
            args += [flag, host[key]]
    args += [image] + (old["Config"].get("Cmd") or [])
    return run(args).decode().strip()


def rehearse(root):
    """将完整备份恢复到根盘专属PostgreSQL容器，事务执行迁移后验证候选服务；生产库不写入。"""
    verify_baseline(root)
    manifest = load(root, "manifest.json")
    if digest(root / "database.dump") != manifest["dump"]["sha256"]:
        raise RuntimeError("数据库备份摘要变化")
    suffix = hashlib.sha256(root.name.encode()).hexdigest()[:12]
    name, database = "goodhr-resume-db-" + suffix, "goodhr_resume_verify_" + suffix
    for port in [55439, 8089, 5178]:
        with socket.socket() as probe:
            if probe.connect_ex(("127.0.0.1", port)) == 0:
                raise RuntimeError("独立验证端口已被占用：" + str(port))
    pgdata = root / "rehearsal-pgdata"
    pgdata.mkdir(mode=0o700, exist_ok=False)
    # 官方入口会切换到postgres用户，挂载父目录须可遍历；外层发布目录仍为700，数据库子目录由入口设置为700。
    pgdata.chmod(0o755)
    pgid = run(["docker", "run", "-d", "--name", name, "--restart", "no", "--label", "goodhr.release=" + root.name,
                "-p", "127.0.0.1:55439:5432", "-e", "POSTGRES_USER=postgres", "-e", "POSTGRES_HOST_AUTH_METHOD=trust", "-e", "PGDATA=/var/lib/postgresql/isolated",
                "--mount", "type=bind,src=" + str(pgdata) + ",dst=/var/lib/postgresql", BASELINE["postgres"][1]]).decode().strip()
    state = {"database_container": name, "database_id": pgid, "database": database, "candidates": {}}
    save(root, "rehearsal.private.json", state)
    for _ in range(30):
        ready = subprocess.run(["docker", "exec", name, "pg_isready", "-U", "postgres"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if not ready.returncode:
            break
        time.sleep(1)
    else:
        raise RuntimeError("独立恢复库未就绪")
    run(["docker", "exec", name, "createdb", "-U", "postgres", database])
    run(["docker", "exec", "-i", name, "pg_restore", "--exit-on-error", "--no-owner", "--no-acl", "-U", "postgres", "-d", database], (root / "database.dump").read_bytes(), timeout=900)
    before = json.loads(sql(name, database, STATS))
    before_fingerprints = fingerprints(name, database)
    if ledger(name, database) != manifest["ledger"]:
        raise RuntimeError("恢复库迁移基线不同")
    sql(name, database, migration_sql(root, manifest["ledger"]), readonly=False)
    report = verify_migrated(root, name, database, before, before_fingerprints)
    new_feature_data = seed_isolated_new_features(root, name, database)
    note_fingerprint_sql = "SELECT md5(COALESCE(string_agg(md5(to_jsonb(n)::text),'' ORDER BY id),'')) FROM candidate_notes n;"
    note_fingerprint = sql(name, database, note_fingerprint_sql)
    sql(name, database, "BEGIN;\n" + load(root, "migrations.private.json")["0086_candidate_notes.sql"] + "\nCOMMIT;", readonly=False)
    if sql(name, database, note_fingerprint_sql) != note_fingerprint:
        raise RuntimeError("独立备注0086重跑未保持幂等")
    report["notes_idempotent"] = True
    unsafe_salary = unsafe_integer_salary_count(name, database)
    if unsafe_salary:
        try:
            sql(name, database, integral_rollback_sql(root), readonly=False)
        except RuntimeError:
            pass
        else:
            raise RuntimeError("独立库的小数或越界薪资未被回退保护拒绝")
        preserved_numeric = sql(name, database, "SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='candidate_profiles' AND column_name IN('expected_salary_min','expected_salary_max') AND data_type='numeric';")
        if preserved_numeric != "2" or fingerprints(name, database) != before_fingerprints:
            raise RuntimeError("独立库拒绝整数回退后未保留原numeric资料")
    else:
        sql(name, database, integral_rollback_sql(root), readonly=False)
        integer_columns = sql(name, database, "SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='candidate_profiles' AND column_name IN('expected_salary_min','expected_salary_max') AND data_type='integer';")
        if integer_columns != "2" or fingerprints(name, database) != before_fingerprints:
            raise RuntimeError("独立库整数薪资回退未保持原业务资料")
    rollback_ledger = ledger(name, database)
    retained_migrations = ALLOWED - {"0084_candidate_library_upgrade.sql", "0085_browser_extension_release_config.sql"}
    if ("0084_candidate_library_upgrade.sql" in rollback_ledger) != bool(unsafe_salary) or not retained_migrations.issubset(set(rollback_ledger)):
        raise RuntimeError("独立回退迁移记录不正确")
    if new_feature_snapshot(name, database) != new_feature_data:
        raise RuntimeError("整数薪资回退删除或修改新增消息、设置或分享资料")
    sql(name, database, migration_sql(root, rollback_ledger, extension_snapshot(name, database)), readonly=False)
    report = verify_migrated(root, name, database, before, before_fingerprints)
    if new_feature_snapshot(name, database) != new_feature_data:
        raise RuntimeError("重迁移删除或修改新增消息、设置或分享资料")
    report.update({"notes_idempotent": True, "integral_salary_rollback_rehearsed": not bool(unsafe_salary), "fractional_salary_guard_rehearsed": bool(unsafe_salary), "migration_reapply_after_rollback": True, "new_feature_data_retained": True})
    for kind in ["backend", "frontend"]:
        candidate = "goodhr-resume-" + kind + "-candidate-" + suffix
        state["candidates"][kind] = {"name": candidate, "id": launch(root, kind, candidate, manifest["images"][kind]["id"], True, database)}
        save(root, "rehearsal.private.json", state)
        wait_http("http://127.0.0.1:8089/health" if kind == "backend" else "http://127.0.0.1:5178/login")
    wait_http("http://127.0.0.1:8089/api/candidates", 401)
    verify_baseline(root)
    save(root, "rehearsal-result.json", {"ready": True, "commit": manifest["commit"], "report": report})
    print(json.dumps({"isolated_rehearsal": "passed", "database": database, "report": report}))


def cleanup_rehearsal(root):
    """仅停止并删除本次记录ID和标签的候选容器，保留独立数据目录，绝不删除或DROP生产库。"""
    state = load(root, "rehearsal.private.json")
    if not state["database"].startswith("goodhr_resume_verify_") or state["database"] == "goodhr5":
        raise RuntimeError("独立库名称不安全")
    values = list(reversed(list(state["candidates"].values()))) + [{"name": state["database_container"], "id": state["database_id"]}]
    for value in values:
        item = inspect(value["name"])
        if item["Id"] != value["id"] or (item["Config"].get("Labels") or {}).get("goodhr.release") != root.name or value["name"] in BASELINE:
            raise RuntimeError("候选容器身份变化，停止清理")
        run(["docker", "stop", "-t", "10", value["name"]])
        run(["docker", "rm", value["name"]])
    save(root, "rehearsal-cleaned.json", {"removed_containers": [value["name"] for value in values], "data_directory_retained": True})


def integral_rollback_sql(root):
    """独立库演练用：独占锁内检查小数和整数越界，仅安全时恢复整数类型并撤销0084，保留新增表。"""
    return """BEGIN; LOCK TABLE candidate_profiles IN ACCESS EXCLUSIVE MODE; LOCK TABLE schema_migrations IN EXCLUSIVE MODE;
DO $salary_guard$ BEGIN IF EXISTS(SELECT 1 FROM candidate_profiles WHERE
 (expected_salary_min IS NOT NULL AND (expected_salary_min<>trunc(expected_salary_min) OR expected_salary_min NOT BETWEEN -2147483648 AND 2147483647)) OR
 (expected_salary_max IS NOT NULL AND (expected_salary_max<>trunc(expected_salary_max) OR expected_salary_max NOT BETWEEN -2147483648 AND 2147483647)))
 THEN RAISE EXCEPTION 'fractional or out-of-range salary; numeric-compatible rollback image required'; END IF; END; $salary_guard$;
ALTER TABLE candidate_profiles ALTER COLUMN expected_salary_min TYPE INTEGER USING expected_salary_min::integer,
 ALTER COLUMN expected_salary_max TYPE INTEGER USING expected_salary_max::integer;
DELETE FROM schema_migrations WHERE filename='0084_candidate_library_upgrade.sql';
""" + restore_config_sql(root) + "\nCOMMIT;"


def unsafe_integer_salary_count(container, database):
    """只统计不能无损转为整数的薪资行，让独立恢复库有小数时验证拒绝回退而不舍弃数据。"""
    return int(sql(container, database, """SELECT count(*) FROM candidate_profiles WHERE
 (expected_salary_min IS NOT NULL AND (expected_salary_min<>trunc(expected_salary_min) OR expected_salary_min NOT BETWEEN -2147483648 AND 2147483647)) OR
 (expected_salary_max IS NOT NULL AND (expected_salary_max<>trunc(expected_salary_max) OR expected_salary_max NOT BETWEEN -2147483648 AND 2147483647));"""))


def restore_config_sql(root, preserve_ledger=False):
    """按原公开字段恢复插件配置，保留当前未知字段；有人改过发布字段时拒绝覆盖。"""
    expected = quote(json.dumps(load(root, "release.json"), ensure_ascii=False)) + "::jsonb"
    keys = "ARRAY[" + ",".join(quote(key) for key in sorted(PUBLIC_FIELDS)) + "]"
    guard = "SELECT config_key FROM system_configs WHERE config_key='system.browser_extension' FOR UPDATE;\n" + \
        "DO $restore_config_guard$ BEGIN IF NOT EXISTS(SELECT 1 FROM system_configs WHERE config_key='system.browser_extension' AND config_value @> " + expected + ") THEN RAISE EXCEPTION 'extension release config changed'; END IF; END; $restore_config_guard$;\n"
    old = load(root, "before-extension-config.private.json")
    if old:
        public = {key: value for key, value in old["config_value"].items() if key in PUBLIC_FIELDS}
        return guard + "UPDATE system_configs SET config_value=(config_value-" + keys + ")||" + quote(json.dumps(public, ensure_ascii=False)) + "::jsonb,enabled=" + ("true" if old["enabled"] else "false") + ",updated_at=now() WHERE config_key='system.browser_extension';"
    result = guard + "DELETE FROM system_configs WHERE config_key='system.browser_extension' AND (config_value-" + keys + ")='{}'::jsonb;\n" + \
        "UPDATE system_configs SET config_value=config_value-" + keys + ",enabled=false,updated_at=now() WHERE config_key='system.browser_extension';\n"
    if not preserve_ledger:
        result += "DELETE FROM schema_migrations WHERE filename='0085_browser_extension_release_config.sql';"
    return result


def recover_backend_after_guard_rejection(root, manifest, progress, original, active):
    """回退被拒绝后只恢复已确认的新小数兼容服务；原整数镜像需列已整数，或保留原容器后启动本次已演练的新镜像。"""
    name = "goodhr-production-backend-1"
    expected_env = original["Config"].get("Env") or []
    if (root / "backend.private.env").read_text(encoding="utf-8") != "\n".join(expected_env) + "\n":
        raise RuntimeError("后端私有恢复环境发生变化，停止自动恢复")
    if active:
        now = inspect(name)
        if now["Id"] != active["Id"] or now["Image"] != active["Image"]:
            raise RuntimeError("后端恢复期间容器身份变化")
        actual_env = dict(value.split("=", 1) for value in now["Config"].get("Env") or [] if "=" in value)
        if any(actual_env.get(key) != value for key, value in (item.split("=", 1) for item in expected_env)):
            raise RuntimeError("现后端环境与私有快照不一致，停止自动恢复")
        numeric_images = {manifest["images"]["backend"]["id"]}
        if manifest["rollback_backend"]:
            numeric_images.add(manifest["rollback_backend"]["id"])
        is_new = now["Id"] == progress["new"].get("backend") and now["Image"] in numeric_images
        labels = now["Config"].get("Labels") or {}
        if is_new and labels.get("goodhr.release") == root.name and labels.get("goodhr.source_commit") == manifest["commit"]:
            run(["docker", "start", name])
            wait_http("http://127.0.0.1:8086/health")
            return "已恢复本次已确认的小数薪资后端"
        if now["Id"] != original["Id"] or now["Image"] != original["Image"]:
            raise RuntimeError("非已确认的本次新镜像或原容器，禁止启动")
        if manifest.get("baseline_salary_numeric"):
            numeric_columns = sql("postgres", "goodhr5", "SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='candidate_profiles' AND column_name IN('expected_salary_min','expected_salary_max') AND data_type='numeric';")
            if numeric_columns != "2":
                raise RuntimeError("805fce9f基线薪资列发生漂移，原numeric后端保持停机")
            run(["docker", "start", name])
            wait_http("http://127.0.0.1:8086/health")
            return "原805fce9f后端已确认支持numeric并恢复，配置或数据未被覆盖"
        integer_columns = sql("postgres", "goodhr5", "SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='candidate_profiles' AND column_name IN('expected_salary_min','expected_salary_max') AND data_type='integer';")
        if integer_columns == "2":
            run(["docker", "start", name])
            wait_http("http://127.0.0.1:8086/health")
            return "薪资列已确认整数，原后端恢复"
    else:
        saved = name + "-backup-" + root.name
        if inspect(saved)["Id"] != original["Id"]:
            raise RuntimeError("原后端恢复材料缺失，停止自动恢复")
    if set(ledger("postgres", "goodhr5")) != set(manifest["ledger"]) | ALLOWED:
        raise RuntimeError("迁移未确认全部提交，保留原后端停机，禁止旧镜像读取numeric")
    if active:
        saved = name + "-backup-" + root.name
        occupied = run(["docker", "ps", "-a", "--filter", "name=^/" + saved + "$", "--format", "{{.ID}}"])
        if occupied.strip():
            raise RuntimeError("原后端备份名被占用，保留停机现场")
        run(["docker", "rename", name, saved])
    new_id = launch(root, "backend", name, manifest["images"]["backend"]["id"])
    recovered = inspect(name)
    if recovered["Id"] != new_id or recovered["Image"] != manifest["images"]["backend"]["id"]:
        raise RuntimeError("应急新后端身份未确认")
    progress["new"]["backend"] = new_id
    progress["numeric_backend_recovered_after_guard_rejection"] = True
    save(root, "cutover-progress.private.json", progress)
    wait_http("http://127.0.0.1:8086/health")
    return "旧整数后端保持备份停机，已启动本次已演练的小数薪资后端"


def rollback(root):
    """按已记录ID恢复805fce9f应用，只回退公开插件配置，保留原numeric、完整ledger和全部新业务资料。"""
    verify_baseline(root, apps=False)
    manifest = load(root, "manifest.json")
    if not manifest.get("baseline_salary_numeric"):
        raise RuntimeError("恢复材料不是已审定的805fce9f numeric基线，拒绝生产回退")
    progress = load(root, "cutover-progress.private.json")
    live_ledger = ledger("postgres", "goodhr5")
    progress["migrated"] = "0084_candidate_library_upgrade.sql" in live_ledger
    originals = {item["Name"].lstrip("/"): item for item in load(root, "before-inspect.private.json")}
    if "backend" in progress["changed"] and progress["migrated"]:
        backend_name = "goodhr-production-backend-1"
        present = run(["docker", "ps", "-a", "--filter", "name=^/" + backend_name + "$", "--format", "{{.ID}}"])
        active = inspect(backend_name) if present.strip() else None
        if active and active["Id"] not in [originals[backend_name]["Id"], progress["new"].get("backend")]:
            raise RuntimeError("后端身份变化，拒绝薪资或配置回退")
        if active and active["State"]["Running"]:
            run(["docker", "stop", "-t", "10", backend_name])
        try:
            if set(live_ledger) != set(manifest["ledger"]) | ALLOWED:
                raise RuntimeError("805fce9f基线完整迁移记录发生漂移")
            if extension_snapshot("postgres", "goodhr5") != load(root, "before-extension-config.private.json"):
                sql("postgres", "goodhr5", "BEGIN;\n" + restore_config_sql(root, preserve_ledger=True) + "\nCOMMIT;", readonly=False)
        except BaseException:
            try:
                recovery = recover_backend_after_guard_rejection(root, manifest, progress, originals[backend_name], active)
            except BaseException:
                raise RuntimeError("薪资或公开配置回退被拒绝且安全恢复受阻；原数据保留，旧整数镜像禁止在numeric列上启动，请核对容器、环境和ledger")
            raise RuntimeError("薪资或公开配置回退被拒绝；" + recovery + "，原业务资料和管理员变更均保留")
    for kind in reversed(progress["changed"]):
        name = "goodhr-production-" + kind + "-1"
        old = originals[name]
        saved = name + "-backup-" + root.name
        ids = run(["docker", "ps", "-a", "--filter", "name=^/" + name + "$", "--format", "{{.ID}}"])
        current = inspect(name) if ids.strip() else None
        if current and current["Id"] != old["Id"] and current["Id"] != progress["new"].get(kind):
            raise RuntimeError("现役应用容器ID变化，禁止回退")
        if current and current["State"]["Running"]:
            run(["docker", "stop", "-t", "10", name])
        if current and current["Id"] != old["Id"]:
            run(["docker", "rename", name, name + "-failed-" + root.name + "-" + str(int(time.time()))])
        if current and current["Id"] == old["Id"]:
            pass
        elif inspect(saved)["Id"] == old["Id"]:
            run(["docker", "rename", saved, name])
        else:
            raise RuntimeError("原应用备份ID不匹配")
        run(["docker", "start", name])
        wait_http("http://127.0.0.1:8086/health" if kind == "backend" else "http://127.0.0.1:5175/login")
    save(root, "rollback-result.json", {"restored": True, "numeric_compatible_bridge": False, "baseline_numeric_and_ledger_retained": True, "new_tables_retained": True, "database_restored": False})
    print(json.dumps({"rollback": "complete", "database_restored": False, "new_tables_retained": True}))


def cutover(root):
    """演练通过后暂停旧后端，在同一事务迁移与更新插件配置，再切换应用；任何失败执行受保护回退。"""
    verify_baseline(root)
    manifest = load(root, "manifest.json")
    result = load(root, "rehearsal-result.json")
    if not result["ready"] or result["commit"] != manifest["commit"]:
        raise RuntimeError("缺少当前提交的完整恢复演练")
    verify_release_config(load(root, "release.json"), manifest["images"]["frontend"]["id"])
    if ledger("postgres", "goodhr5") != manifest["ledger"]:
        raise RuntimeError("生产迁移基线发生变化")
    if extension_snapshot("postgres", "goodhr5") != load(root, "before-extension-config.private.json"):
        raise RuntimeError("插件配置整行已被管理员修改，尚未停机，发布安全退出；请重新审定备份和公开配置")
    progress = {"changed": [], "new": {}, "migrated": False}
    save(root, "cutover-progress.private.json", progress)
    try:
        name = "goodhr-production-backend-1"
        progress["changed"].append("backend")
        save(root, "cutover-progress.private.json", progress)
        run(["docker", "stop", "-t", "20", name])
        before = json.loads(sql("postgres", "goodhr5", STATS))
        before_fingerprints = fingerprints("postgres", "goodhr5")
        save(root, "before-migration-stats.json", before)
        save(root, "before-migration-fingerprints.json", before_fingerprints)
        sql("postgres", "goodhr5", migration_sql(root, manifest["ledger"]), readonly=False)
        progress["migrated"] = True
        save(root, "cutover-progress.private.json", progress)
        report = verify_migrated(root, "postgres", "goodhr5", before, before_fingerprints)
        for kind in ["backend", "frontend"]:
            name = "goodhr-production-" + kind + "-1"
            if kind == "frontend":
                progress["changed"].append(kind)
                save(root, "cutover-progress.private.json", progress)
                run(["docker", "stop", "-t", "20", name])
            run(["docker", "rename", name, name + "-backup-" + root.name])
            progress["new"][kind] = launch(root, kind, name, manifest["images"][kind]["id"])
            save(root, "cutover-progress.private.json", progress)
            wait_http("http://127.0.0.1:8086/health" if kind == "backend" else "http://127.0.0.1:5175/login")
        wait_http("https://goodhr.58it.cn/login")
        wait_http("https://goodhr.58it.cn/api/candidates", 401)
        wait_http("https://goodhr.58it.cn/api/system/browser-extension")
        for browser in ["chrome", "firefox"]:
            config = load(root, "release.json")
            if config["url_" + browser]:
                url = urllib.parse.urljoin("https://goodhr.58it.cn", config["url_" + browser])
                with urllib.request.urlopen(url, timeout=20) as response:
                    value = hashlib.sha256(response.read()).hexdigest()
                if value != config["sha256_" + browser]:
                    raise RuntimeError("官网插件实际文件摘要不匹配")
        verify_baseline(root, apps=False)
        save(root, "deployment-result.json", {"complete": True, "commit": manifest["commit"], "new_container_ids": progress["new"], "report": report})
        print(json.dumps({"cutover": "complete", "commit": manifest["commit"], "new_container_ids": progress["new"], "report": report}))
    except BaseException:
        try:
            rollback(root)
        except BaseException:
            raise RuntimeError("发布未完成且自动回退未完成；恢复材料已保留，须核对容器与ledger，禁止直接启动旧整数薪资镜像")
        raise RuntimeError("发布未完成，受保护回退完成，请核对rollback-result.json")


def main():
    """限制发布目录到根盘指定范围，串行锁定本次发布，并按明确子命令执行。"""
    parser = argparse.ArgumentParser()
    parser.add_argument("operation", choices=["prepare", "rehearse", "cutover", "rollback", "cleanup-rehearsal"])
    parser.add_argument("--release-dir", required=True)
    parser.add_argument("--commit")
    parser.add_argument("--backend-image")
    parser.add_argument("--frontend-image")
    parser.add_argument("--archive")
    parser.add_argument("--archive-sha256")
    parser.add_argument("--release-json")
    parser.add_argument("--rollback-backend-image")
    args = parser.parse_args()
    signal.signal(signal.SIGTERM, interrupt_release)
    os.umask(0o077)
    root = pathlib.Path(args.release_dir).resolve()
    if root.parent != pathlib.Path("/opt/goodhr-release") or not re.fullmatch(r"resume-library-20261010-[A-Za-z0-9_-]+", root.name):
        raise RuntimeError("发布目录必须是/opt/goodhr-release/resume-library-20261010-*独立目录")
    root.mkdir(mode=0o700, parents=True, exist_ok=True)
    if os.stat(root).st_dev != os.stat("/").st_dev:
        raise RuntimeError("发布备份必须放在根盘，不能占用/www数据盘")
    root.chmod(0o700)
    with (root.parent / ".resume-release.lock").open("a") as lock:
        os.chmod(lock.name, 0o600)
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if args.operation == "prepare":
            prepare(root, args)
        elif args.operation == "rehearse":
            rehearse(root)
        elif args.operation == "cutover":
            cutover(root)
        elif args.operation == "rollback":
            rollback(root)
        else:
            cleanup_rehearsal(root)


def interrupt_release(signum, frame):
    """将正常终止信号转成异常，让切换过程进入已有受保护回退流程。"""
    raise RuntimeError("发布进程收到终止信号")


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(json.dumps({"success": False, "error": str(error)}, ensure_ascii=False))
        raise SystemExit(1)
