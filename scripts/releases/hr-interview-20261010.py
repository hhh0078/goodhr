# 文件作用：为HR面试功能提供人工执行的生产备份、独立恢复演练、两项新增迁移、应用切换与保留业务资料的容器回退；本文件不构建、不上传镜像。
import argparse
import copy
import fcntl
import hashlib
import http.client
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
    "goodhr-production-backend-1": ("b80746b6fcd4a314b5dca0e7a927a7775f2918d55f305daebe11755eb2260ff6", "sha256:430a8bd66e2cc0a3956d6da704383aa789cc668b1aa1de7df10b6d2b73312ac4", "2026-10-10T14:58:32.89392656Z"),
    "goodhr-production-frontend-1": ("a915fc4f1d2a9bbd243b7c7085d4614fde529e5fe0a4e3fe2dba2047af2de2d8", "sha256:67c1f83120099061e0c4b7b16ee25335d9bb31c12e59fa81479992c0caeb304f", "2026-10-10T14:58:35.03117983Z"),
    "postgres": ("7ae7305d3a222272d23cf4a68ae302cf877fc71d2ce1fab9ad3a97b3fe02d89d", "sha256:413d57cb67bb3585acae8769a48661aa1dcac66076e3a5f88ea6c5be55f8916e", "2026-09-20T12:25:51.09539213Z"),
    "redis": ("78a704cd2eb89019c62bfb6ab2ceef603bf2cd293fe3d522ca7c132a1da72025", "sha256:646a47c903c5c45f99081094d8c3ba51ec2a1091babf42dc5a928cef024e6fcd", "2026-09-20T12:28:59.032970342Z"),
}
ALLOWED = {"0090_recruitment_workflows.sql", "0091_recruitment_mail.sql"}
NEW_TABLES = {"recruitment_workflows", "recruitment_applications", "recruitment_invitations", "recruitment_company_settings", "recruitment_mail_templates", "recruitment_mail_deliveries"}
VERIFY_DATABASE = "goodhr_hr_verify"
PORTS = (55441, 8089, 5178)
INVALID_TOKEN = "0" * 64


def run(args, data=None, output=None, input_file=None, timeout=180):
    """用参数列表运行命令，超时标为结果待核实，并隐藏凭据、SQL和业务正文。"""
    try:
        result = subprocess.run(args, input=data, stdin=input_file, stdout=output or subprocess.PIPE,
                                stderr=subprocess.PIPE, timeout=timeout)
    except subprocess.TimeoutExpired:
        raise RuntimeError("命令超时，结果需核对私有进度与实际状态；程序=" + args[0])
    if result.returncode:
        raise RuntimeError("命令失败，详细输出未公开；程序=%s exit=%s" % (args[0], result.returncode))
    return result.stdout


def inspect(name, image=False):
    """读取本机完整Docker信息，仅由调用者公开白名单摘要。"""
    return json.loads(run(["docker", "image", "inspect", name] if image else ["docker", "inspect", name]))[0]


def existing(name):
    """按精确容器名称查找，避免部分名称或不同发布的容器被误认。"""
    raw = run(["docker", "ps", "-a", "--filter", "name=^/" + name + "$", "--format", "{{.ID}}"])
    return inspect(name) if raw.strip() else None


def digest(path):
    """流式计算备份或归档SHA256，不将资料加载到输出。"""
    value = hashlib.sha256()
    with pathlib.Path(path).open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            value.update(block)
    return value.hexdigest()


def save(root, name, value):
    """以600权限原子写入并同步私有进度，命令前后的意图均可检查。"""
    target = root / name
    temporary = root / (name + ".tmp")
    with temporary.open("w", encoding="utf-8") as stream:
        os.chmod(str(temporary), 0o600)
        json.dump(value, stream, ensure_ascii=False, indent=2)
        stream.flush()
        os.fsync(stream.fileno())
    temporary.replace(target)
    directory = os.open(str(root), os.O_RDONLY)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)


def load(root, name):
    """从当前发布私有目录读取状态，不接受其他目录的恢复材料。"""
    return json.loads((root / name).read_text(encoding="utf-8"))


def intent(root, state, operation):
    """在可能改变容器或数据库的操作前持久记录当前动作。"""
    state["last_operation"] = operation
    state["updated_at"] = int(time.time())
    save(root, "cutover-progress.private.json", state)


def quote(value):
    """将受控值转换为SQL字符串常量，避免原始文本拼入SQL。"""
    return "'" + str(value).replace("'", "''") + "'"


def identifier(value):
    """仅接受从public目录读取的普通表名，再进行标识符引用。"""
    if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", value):
        raise RuntimeError("数据库表名不符合受控标识符约束")
    return '"' + value + '"'


def sql(container, database, query, readonly=True):
    """通过容器管理角色执行目标库SQL，正式库默认只读且绝不输出错误正文。"""
    if (container == "postgres" and database != "goodhr5") or (container != "postgres" and
            (not container.startswith("goodhr-hr-db-") or database != VERIFY_DATABASE)):
        raise RuntimeError("数据库目标不符合正式库或独立恢复库约束")
    options = "-c statement_timeout=120000 -c lock_timeout=10000"
    if readonly:
        options += " -c default_transaction_read_only=on"
    command = ["docker", "exec", "-i", "-e", "PGOPTIONS=" + options, container, "sh", "-c",
               'exec psql -X -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$1" -t -A', "hr-release-sql", database]
    return run(command, query.encode("utf-8"), timeout=180).decode("utf-8").strip()


def ledger(container, database):
    """读取完整迁移文件名与排序，始终按文件名而非编号判断。"""
    return sql(container, database, "SELECT filename FROM schema_migrations ORDER BY filename;").splitlines()


def tables(container, database):
    """列出public业务表，用于比较新增范围和原业务行。"""
    query = "SELECT COALESCE(json_agg(tablename ORDER BY tablename),'[]'::json) FROM pg_tables WHERE schemaname='public';"
    return json.loads(sql(container, database, query))


def snapshot(container, database, names):
    """对每张指定表读取数量和全部字段的确定性行摘要，不公开候选人或配置正文。"""
    queries = []
    for name in sorted(names):
        queries.append("SELECT %s AS name,jsonb_build_object('rows',count(*),'fingerprint',md5(COALESCE(string_agg(md5(to_jsonb(t)::text),'' ORDER BY md5(to_jsonb(t)::text)),''))) AS value FROM %s t" % (quote(name), identifier(name)))
    if not queries:
        return {}
    query = "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SELECT jsonb_object_agg(name,value) FROM (" + " UNION ALL ".join(queries) + ") s; COMMIT;"
    lines = sql(container, database, query).splitlines()
    return json.loads(next(line for line in lines if line.startswith("{")))


def old_ledger_snapshot(container, database, filenames):
    """保存原迁移行的全字段摘要，证明追加两条记录没有重写旧ledger。"""
    values = ",".join(quote(name) for name in filenames)
    query = "SELECT md5(COALESCE(string_agg(md5(to_jsonb(t)::text),'' ORDER BY filename),'')) FROM schema_migrations t WHERE filename IN (" + values + ");"
    return sql(container, database, query)


def verify_baseline(root=None, apps=True):
    """核对已审定服务器和原容器身份，PG和Redis的任何运行基线变化均拒绝操作。"""
    if socket.gethostname() != "ldclouda31539" or "43.228.79.108" not in run(["hostname", "-I"]).decode().split():
        raise RuntimeError("生产服务器身份不匹配，未进行发布")
    expected = BASELINE if root is None else {item["Name"].lstrip("/"): (item["Id"], item["Image"], item["State"]["StartedAt"]) for item in load(root, "before-inspect.private.json")}
    result = []
    for name, values in expected.items():
        if not apps and name.startswith("goodhr-production-"):
            continue
        item = inspect(name)
        if (item["Id"], item["Image"], item["State"]["StartedAt"]) != tuple(values) or not item["State"]["Running"]:
            raise RuntimeError("正式容器基线发生变化：" + name)
        result.append(item)
    return result


def migrations_from_tar(contents, prefix):
    """仅提取安全归档内的正向SQL，拒绝链接、越界路径与重复迁移名。"""
    result = {}
    with tarfile.open(fileobj=io.BytesIO(contents)) as archive:
        for member in archive.getmembers():
            name = pathlib.PurePosixPath(member.name)
            if name.is_absolute() or ".." in name.parts or member.issym() or member.islnk():
                raise RuntimeError("归档包含不安全路径或链接")
            if member.isfile() and str(name).startswith(prefix) and name.name.endswith(".sql") and not name.name.endswith(".down.sql"):
                if name.name in result:
                    raise RuntimeError("归档包含重复迁移文件名")
                result[name.name] = archive.extractfile(member).read().replace(b"\r\n", b"\n").decode("utf-8")
    return result


def image_migrations(image):
    """运行无网络tar工具读取镜像SQL，绝不启动镜像后端或连接正式数据库。"""
    raw = run(["docker", "run", "--rm", "--network", "none", "--entrypoint", "tar", image, "-cf", "-", "-C", "/app/db/migrations", "."])
    return migrations_from_tar(raw, "")


def validate_new_migrations(texts):
    """限制新增SQL只创建六张招聘表及其注释、索引，拒绝旧表变更和独立事务控制。"""
    if set(texts) != ALLOWED:
        raise RuntimeError("新增迁移必须恰好为0090和0091")
    created = set()
    for text in texts.values():
        clean = re.sub(r"(?m)--[^\n]*", "", text)
        for statement in clean.split(";"):
            statement = statement.strip()
            if not statement:
                continue
            create = re.match(r"(?is)^CREATE TABLE IF NOT EXISTS ([a-z_]+)\s*\(", statement)
            comment = re.match(r"(?is)^COMMENT ON (?:TABLE|COLUMN) ([a-z_]+)(?:\.[a-z_]+)? IS ", statement)
            index = re.match(r"(?is)^CREATE (?:UNIQUE )?INDEX IF NOT EXISTS [a-z_]+ ON ([a-z_]+)\s*\(", statement)
            match = create or comment or index
            if not match or match.group(1) not in NEW_TABLES:
                raise RuntimeError("新增SQL包含六张招聘表之外的操作")
            if create:
                created.add(match.group(1))
    if created != NEW_TABLES:
        raise RuntimeError("新增SQL没有恰好创建六张招聘表")


def check_materials(root):
    """在演练和切换前重新核对备份、归档、镜像提交和全部SQL正文摘要。"""
    manifest = load(root, "manifest.private.json")
    if digest(root / "source.tar") != manifest["source_sha256"] or digest(root / "database.dump") != manifest["dump"]["sha256"]:
        raise RuntimeError("归档或数据库备份摘要发生变化")
    for kind in ("backend", "frontend"):
        image = inspect(manifest["images"][kind]["id"], image=True)
        if (image["Config"].get("Labels") or {}).get("org.opencontainers.image.revision") != manifest["commit"]:
            raise RuntimeError("候选镜像提交与发布材料不一致")
    actual = image_migrations(manifest["images"]["backend"]["id"])
    if {name: hashlib.sha256(text.encode()).hexdigest() for name, text in actual.items()} != manifest["migration_sha256"]:
        raise RuntimeError("候选镜像迁移正文发生变化")
    texts = load(root, "migrations.private.json")
    validate_new_migrations(texts)
    if any(actual.get(name) != text for name, text in texts.items()):
        raise RuntimeError("已审定新增迁移正文发生变化")
    return manifest


def verify_ports():
    """确认三个隔离端口可绑定，任何占用都会停止演练。"""
    for port in PORTS:
        with socket.socket() as probe:
            try:
                probe.bind(("127.0.0.1", port))
            except OSError:
                raise RuntimeError("独立演练端口已占用：" + str(port))


def prepare(root, args):
    """备份完整数据库及原应用私有参数，只审定两项新迁移，不写正式库。"""
    current = verify_baseline()
    if any((root / name).exists() for name in ("manifest.private.json", "before-inspect.private.json", "migrations.private.json", "backend.private.env", "frontend.private.env", "database.dump.partial", "database.dump")):
        raise RuntimeError("已有发布材料，禁止覆盖；请核对私有状态或使用新的发布目录")
    if not re.fullmatch(r"[0-9a-f]{40}", args.commit or ""):
        raise RuntimeError("请提供完整40位最终提交")
    archive = pathlib.Path(args.archive or "")
    if not archive.is_file() or not re.fullmatch(r"[0-9a-f]{64}", args.archive_sha256 or "") or digest(archive) != args.archive_sha256:
        raise RuntimeError("源码归档或SHA256不匹配")
    if archive.stat().st_size > 300 * 1024 ** 2:
        raise RuntimeError("源码归档超过300MB")
    images = {}
    for kind, tag in (("backend", args.backend_image), ("frontend", args.frontend_image)):
        if not tag:
            raise RuntimeError("前后端候选镜像均需提供")
        image = inspect(tag, image=True)
        if (image["Config"].get("Labels") or {}).get("org.opencontainers.image.revision") != args.commit:
            raise RuntimeError("镜像OCI提交标签与最终提交不符")
        images[kind] = {"id": image["Id"], "tag": tag}
    source = migrations_from_tar(archive.read_bytes(), "goodhr5/cloud/backend/db/migrations/")
    previous = image_migrations(BASELINE["goodhr-production-backend-1"][1])
    candidate = image_migrations(images["backend"]["id"])
    applied = ledger("postgres", "goodhr5")
    if len(applied) != 96 or set(applied) != set(previous) or candidate != source or set(candidate) - set(previous) != ALLOWED or any(candidate.get(name) != text for name, text in previous.items()):
        raise RuntimeError("旧96项迁移、正文或新增0090/0091范围与归档镜像不一致")
    texts = {name: candidate[name] for name in sorted(ALLOWED)}
    validate_new_migrations(texts)
    old_tables = tables("postgres", "goodhr5")
    if NEW_TABLES.intersection(old_tables) or any(name.startswith("recruitment_") for name in old_tables):
        raise RuntimeError("正式库已有招聘表，与未发布基线不符")
    database_bytes = int(sql("postgres", "goodhr5", "SELECT pg_database_size(current_database());"))
    if shutil.disk_usage(root).free < max(256 * 1024 ** 2, database_bytes * 2 + 64 * 1024 ** 2):
        raise RuntimeError("根盘不足以同时保留备份与独立恢复库")
    verify_ports()
    save(root, "before-inspect.private.json", current)
    for kind in ("backend", "frontend"):
        old = next(item for item in current if item["Name"] == "/goodhr-production-" + kind + "-1")
        env = old["Config"].get("Env") or []
        if any("\n" in value or "\r" in value for value in env):
            raise RuntimeError("原容器环境包含多行值，需单独审查")
        path = root / (kind + ".private.env")
        path.write_text("\n".join(env) + "\n", encoding="utf-8")
        path.chmod(0o600)
    if archive.resolve() != (root / "source.tar").resolve():
        if (root / "source.tar").exists() and digest(root / "source.tar") != args.archive_sha256:
            raise RuntimeError("发布目录已有不同的source.tar，禁止覆盖构建材料")
        shutil.copyfile(str(archive), str(root / "source.tar"))
    (root / "source.tar").chmod(0o600)
    if digest(root / "source.tar") != args.archive_sha256:
        raise RuntimeError("归档复制后摘要发生变化")
    save(root, "migrations.private.json", texts)
    path = root / "database.dump.partial"
    with path.open("xb") as stream:
        path.chmod(0o600)
        run(["docker", "exec", "postgres", "sh", "-c", 'exec pg_dump -U "$POSTGRES_USER" -d goodhr5 -Fc --no-owner --no-acl'], output=stream, timeout=900)
        stream.flush()
        os.fsync(stream.fileno())
    with path.open("rb") as stream:
        catalog = run(["docker", "exec", "-i", "postgres", "pg_restore", "--list"], input_file=stream)
    with path.open("rb") as stream:
        if stream.read(5) != b"PGDMP" or not catalog:
            raise RuntimeError("完整备份未通过pg_restore目录校验")
    path.replace(root / "database.dump")
    manifest = {"commit": args.commit, "images": images, "ledger": applied, "tables": old_tables,
                "database_bytes": database_bytes, "source_sha256": args.archive_sha256,
                "migration_sha256": {name: hashlib.sha256(text.encode()).hexdigest() for name, text in candidate.items()},
                "dump": {"bytes": (root / "database.dump").stat().st_size, "sha256": digest(root / "database.dump")}}
    save(root, "manifest.private.json", manifest)
    verify_baseline(root)
    print(json.dumps({"prepared": str(root), "commit": args.commit, "dump": manifest["dump"], "pending": sorted(ALLOWED)}, ensure_ascii=False))


def migration_sql(root):
    """将仅新增六表的DDL和追加两条ledger记录组成一个事务，旧ledger行不更新不删除。"""
    manifest = load(root, "manifest.private.json")
    texts = load(root, "migrations.private.json")
    validate_new_migrations(texts)
    expected = quote(json.dumps(sorted(manifest["ledger"]))) + "::jsonb"
    commands = ["BEGIN; LOCK TABLE schema_migrations IN EXCLUSIVE MODE; SELECT pg_advisory_xact_lock(hashtext('goodhr-hr-interview-release'));",
                "DO $ledger_guard$ BEGIN IF (SELECT COALESCE(jsonb_agg(filename ORDER BY filename),'[]'::jsonb) FROM schema_migrations) <> " + expected + " THEN RAISE EXCEPTION 'migration ledger changed'; END IF; END; $ledger_guard$;",
                "DO $table_guard$ BEGIN IF EXISTS(SELECT 1 FROM pg_tables WHERE schemaname='public' AND tablename LIKE 'recruitment_%') THEN RAISE EXCEPTION 'unexpected recruitment tables'; END IF; END; $table_guard$;"]
    names = [identifier(name) for name in manifest["tables"] if name != "schema_migrations"]
    if names:
        commands.append("LOCK TABLE " + ",".join(names) + " IN SHARE MODE;")
    for name in sorted(ALLOWED):
        commands.extend([texts[name], "INSERT INTO schema_migrations(filename) VALUES (" + quote(name) + ");"])
    commands.append("COMMIT;")
    return "\n".join(commands)


def verify_migrated(root, container, database, before, old_ledger_hash):
    """验证新增范围、旧业务全行摘要和旧ledger整行摘要全部保留。"""
    manifest = load(root, "manifest.private.json")
    expected_tables = set(manifest["tables"]) | NEW_TABLES
    if set(tables(container, database)) != expected_tables or ledger(container, database) != sorted(set(manifest["ledger"]) | ALLOWED):
        raise RuntimeError("迁移后表范围或98项ledger不一致")
    after = snapshot(container, database, set(manifest["tables"]) - {"schema_migrations"})
    if after != before or old_ledger_snapshot(container, database, manifest["ledger"]) != old_ledger_hash:
        raise RuntimeError("迁移改变了原业务行或旧ledger记录")
    feature = snapshot(container, database, NEW_TABLES)
    if any(value["rows"] for value in feature.values()):
        raise RuntimeError("DDL迁移后出现未经操作的招聘资料")
    return {"old_tables_verified": len(before), "old_rows_unchanged": True, "old_ledger_unchanged": True, "ledger_count": 98, "new_tables": sorted(NEW_TABLES)}


class LocalDockerConnection(http.client.HTTPConnection):
    """通过本机Unix socket复用完整Docker创建参数，不访问远程Docker或安装依赖。"""

    def connect(self):
        """连接固定本机daemon套接字并设置有限超时。"""
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect("/var/run/docker.sock")


def docker_request(method, path, payload=None, accepted=(200, 201, 204, 304)):
    """向本机Docker API发送私有配置，不把daemon响应正文写入错误信息。"""
    connection = LocalDockerConnection("localhost", timeout=90)
    body = None if payload is None else json.dumps(payload).encode("utf-8")
    try:
        connection.request(method, path, body=body, headers={"Content-Type": "application/json"})
        response = connection.getresponse()
        raw = response.read()
        if response.status not in accepted:
            raise RuntimeError("本机Docker操作失败，需核对私有进度；HTTP=" + str(response.status))
        return json.loads(raw) if raw else None
    except (socket.timeout, TimeoutError):
        raise RuntimeError("本机Docker操作超时，容器状态需按名称和发布标签核实")
    finally:
        connection.close()


def docker_version():
    """读取本机daemon支持的API版本，避免使用固定版本丢失新参数。"""
    value = run(["docker", "version", "--format", "{{.Server.APIVersion}}"]).decode().strip()
    if not re.fullmatch(r"\d+\.\d+", value):
        raise RuntimeError("Docker API版本未识别")
    return "/v" + value


def application_payload(root, kind, image, candidate=False):
    """正式应用完整复用原Config和HostConfig；隔离候选只使用新镜像默认值与独立目录。"""
    manifest = load(root, "manifest.private.json")
    old = next(item for item in load(root, "before-inspect.private.json") if item["Name"] == "/goodhr-production-" + kind + "-1")
    new = inspect(image, image=True)
    if (old["Config"].get("Entrypoint") or []) != (new["Config"].get("Entrypoint") or []):
        raise RuntimeError("新旧镜像Entrypoint不同，需重新审查运行方式")
    if candidate:
        payload = copy.deepcopy(new["Config"])
        env = dict(value.split("=", 1) for value in payload.get("Env") or [] if "=" in value)
        host = {"NetworkMode": "host", "RestartPolicy": {"Name": "no"}, "LogConfig": {"Type": "json-file", "Config": {"max-size": "10m", "max-file": "2"}}}
        if kind == "backend":
            env.update({"GOODHR_PG_DSN": "postgres://postgres@127.0.0.1:55441/" + VERIFY_DATABASE + "?sslmode=disable", "GOODHR_CLOUD_ADDR": "127.0.0.1:8089", "GOODHR_DISABLE_EMAIL_SCHEDULER": "true", "GOODHR_REDIS_ADDR": "", "GOODHR_REDIS_PASSWORD": "", "GOODHR_SMTP_HOST": "", "GOODHR_SMTP_USERNAME": "", "GOODHR_SMTP_PASSWORD": "", "GOODHR_SMTP_FROM": "", "GOODHR_CLOUD_LOG_FILE": "logs/isolated-hr-release.log", "GOODHR_PUBLIC_BASE_URL": "http://127.0.0.1:5178"})
            mounts = []
            for directory in ("uploads", "logs"):
                path = root / ("candidate-" + directory)
                path.mkdir(mode=0o700, exist_ok=True)
                mounts.append({"Type": "bind", "Source": str(path), "Target": "/app/" + directory})
            host["Mounts"] = mounts
        else:
            env.update({"PORT": "5178", "HOSTNAME": "127.0.0.1", "CLOUD_API_BASE": "http://127.0.0.1:8089", "NEXT_PUBLIC_CLOUD_API_BASE": "http://127.0.0.1:8089"})
        payload["Env"] = [key + "=" + value for key, value in env.items()]
        payload["HostConfig"] = host
        payload.pop("Healthcheck", None)
        payload["Labels"] = {}
    else:
        payload = copy.deepcopy(old["Config"])
        payload["HostConfig"] = copy.deepcopy(old["HostConfig"])
        if payload["HostConfig"].get("ContainerIDFile") or old["State"].get("Paused"):
            raise RuntimeError("原应用使用特殊ID文件或暂停状态，需人工审查")
        endpoints = {}
        for name, details in (old.get("NetworkSettings", {}).get("Networks") or {}).items():
            endpoints[name] = {key: copy.deepcopy(details[key]) for key in ("IPAMConfig", "Links", "Aliases", "DriverOpts", "MacAddress") if details.get(key)}
        if endpoints and payload["HostConfig"].get("NetworkMode") not in ("host", "none"):
            payload["NetworkingConfig"] = {"EndpointsConfig": endpoints}
        if "com.docker.compose.image" in (payload.get("Labels") or {}):
            payload["Labels"]["com.docker.compose.image"] = image
    payload["Image"] = image
    labels = payload.get("Labels") or {}
    labels.update({"goodhr.release": root.name, "goodhr.source_commit": manifest["commit"], "goodhr.release_kind": kind, "org.opencontainers.image.revision": manifest["commit"]})
    payload["Labels"] = labels
    return payload


def create_application(root, kind, name, image, candidate=False):
    """仅创建容器并返回ID，由上层先记录ID再启动，以便超时核对和安全恢复。"""
    if existing(name):
        raise RuntimeError("目标应用容器名称已占用：" + name)
    payload = application_payload(root, kind, image, candidate)
    save(root, ("candidate-" if candidate else "new-") + kind + "-create.private.json", {"name": name, "image": image, "payload": payload})
    result = docker_request("POST", docker_version() + "/containers/create?name=" + urllib.parse.quote(name), payload)
    return result["Id"]


def start_container(container_id):
    """通过已持久记录的容器ID启动，不根据模糊名称执行。"""
    docker_request("POST", docker_version() + "/containers/" + container_id + "/start")


def wait_http(url, expected=200):
    """只执行匿名GET健康和页面检查，有限重试且不登录、不发送邮件、不写候选资料。"""
    for _ in range(25):
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
    raise RuntimeError("HTTP检查未通过：" + url)


def verify_http(backend, frontend):
    """检查健康、招聘认证边界、无效邀请及已构建的新页面。"""
    wait_http(backend + "/health")
    wait_http(backend + "/api/recruitment/company", 401)
    wait_http(backend + "/api/recruitment/applications", 401)
    wait_http(backend + "/api/public/recruitment/invitations/" + INVALID_TOKEN, 404)
    wait_http(frontend + "/login")
    wait_http(frontend + "/admin/recruitment")
    wait_http(frontend + "/interview-invitation/" + INVALID_TOKEN)


def rehearse(root):
    """恢复完整备份到根盘专用PG，事务演练新增迁移并启动停发信的隔离候选应用。"""
    verify_baseline(root)
    manifest = check_materials(root)
    if (root / "rehearsal.private.json").exists() or (root / "rehearsal-pgdata").exists():
        raise RuntimeError("已有演练现场；先检查cleanup结果，重新演练需新发布目录")
    verify_ports()
    if shutil.disk_usage(root).free < manifest["database_bytes"] + 96 * 1024 ** 2:
        raise RuntimeError("根盘不足以建立独立恢复库")
    suffix = hashlib.sha256(root.name.encode()).hexdigest()[:12]
    pg_name = "goodhr-hr-db-" + suffix
    path = root / "rehearsal-pgdata"
    path.mkdir(mode=0o755)
    # 官方PG入口切换postgres用户后仍需遍历此挂载父目录；发布目录本身继续保持700。
    path.chmod(0o755)
    state = {"database_container": pg_name, "database_id": None, "database": VERIFY_DATABASE, "candidates": {}, "last_operation": "create_database", "cleaned": False}
    save(root, "rehearsal.private.json", state)
    if existing(pg_name):
        raise RuntimeError("独立恢复容器名称已被占用")
    pg_id = run(["docker", "create", "--name", pg_name, "--restart", "no", "--label", "goodhr.release=" + root.name, "--label", "goodhr.release_kind=database", "-p", "127.0.0.1:55441:5432", "-e", "POSTGRES_USER=postgres", "-e", "POSTGRES_HOST_AUTH_METHOD=trust", "-e", "PGDATA=/var/lib/postgresql/isolated", "--mount", "type=bind,src=" + str(path) + ",dst=/var/lib/postgresql", BASELINE["postgres"][1]]).decode().strip()
    state["database_id"] = pg_id
    state["last_operation"] = "start_database"
    save(root, "rehearsal.private.json", state)
    start_container(pg_id)
    for _ in range(30):
        check = subprocess.run(["docker", "exec", pg_id, "pg_isready", "-U", "postgres"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5)
        if check.returncode == 0:
            break
        time.sleep(1)
    else:
        raise RuntimeError("独立恢复PG未就绪")
    state["last_operation"] = "restore_dump"
    save(root, "rehearsal.private.json", state)
    run(["docker", "exec", pg_id, "createdb", "-U", "postgres", VERIFY_DATABASE])
    with (root / "database.dump").open("rb") as stream:
        run(["docker", "exec", "-i", pg_id, "pg_restore", "--exit-on-error", "--no-owner", "--no-acl", "-U", "postgres", "-d", VERIFY_DATABASE], input_file=stream, timeout=900)
    if ledger(pg_name, VERIFY_DATABASE) != manifest["ledger"] or set(tables(pg_name, VERIFY_DATABASE)) != set(manifest["tables"]):
        raise RuntimeError("独立恢复库的旧表或ledger与备份基线不同")
    before = snapshot(pg_name, VERIFY_DATABASE, set(manifest["tables"]) - {"schema_migrations"})
    ledger_hash = old_ledger_snapshot(pg_name, VERIFY_DATABASE, manifest["ledger"])
    state["last_operation"] = "migrate_isolated_database"
    save(root, "rehearsal.private.json", state)
    sql(pg_name, VERIFY_DATABASE, migration_sql(root), readonly=False)
    report = verify_migrated(root, pg_name, VERIFY_DATABASE, before, ledger_hash)
    for kind in ("backend", "frontend"):
        name = "goodhr-hr-" + kind + "-candidate-" + suffix
        state["candidates"][kind] = {"name": name, "id": None, "image": manifest["images"][kind]["id"]}
        state["last_operation"] = "create_candidate_" + kind
        save(root, "rehearsal.private.json", state)
        container_id = create_application(root, kind, name, manifest["images"][kind]["id"], candidate=True)
        state["candidates"][kind]["id"] = container_id
        state["last_operation"] = "start_candidate_" + kind
        save(root, "rehearsal.private.json", state)
        start_container(container_id)
        wait_http("http://127.0.0.1:8089/health" if kind == "backend" else "http://127.0.0.1:5178/login")
    verify_http("http://127.0.0.1:8089", "http://127.0.0.1:5178")
    verify_baseline(root)
    state["last_operation"] = "verified"
    save(root, "rehearsal.private.json", state)
    save(root, "rehearsal-result.private.json", {"ready": True, "commit": manifest["commit"], "report": report})
    print(json.dumps({"rehearsal": "passed", "database": VERIFY_DATABASE, "report": report}, ensure_ascii=False))


def recognize_new(root, kind, item, expected_id=None):
    """验证新容器ID或不确定创建结果的发布标签与镜像，禁止误操作其他任务容器。"""
    manifest = load(root, "manifest.private.json")
    labels = item["Config"].get("Labels") or {}
    if item["Image"] != manifest["images"][kind]["id"] or labels.get("goodhr.release") != root.name or labels.get("goodhr.source_commit") != manifest["commit"] or labels.get("goodhr.release_kind") != kind or (expected_id and item["Id"] != expected_id):
        raise RuntimeError("新应用容器身份变化，保留现场等待核对")


def rollback(root):
    """恢复原前后端容器，保留全部新增表、招聘资料和完整ledger，绝不恢复或删除数据库。"""
    verify_baseline(root, apps=False)
    state = load(root, "cutover-progress.private.json")
    originals = {item["Name"].lstrip("/"): item for item in load(root, "before-inspect.private.json")}
    for kind in reversed(state["changed"]):
        name = "goodhr-production-" + kind + "-1"
        old = originals[name]
        saved_name = name + "-backup-" + root.name
        current = existing(name)
        if current and current["Id"] != old["Id"]:
            recognize_new(root, kind, current, state["new"].get(kind))
            state["new"][kind] = current["Id"]
            intent(root, state, "rollback_stop_new_" + kind)
            if current["State"]["Running"]:
                run(["docker", "stop", "-t", "20", current["Id"]], timeout=45)
            failed_name = name + "-failed-" + root.name + "-" + current["Id"][:12]
            intent(root, state, "rollback_preserve_failed_" + kind)
            if existing(failed_name):
                raise RuntimeError("失败容器保留名称已占用")
            run(["docker", "rename", current["Id"], failed_name])
            state.setdefault("failed", {})[kind] = {"name": failed_name, "id": current["Id"]}
            intent(root, state, "rollback_restore_name_" + kind)
            current = None
        if current is None:
            backup = existing(saved_name)
            if not backup or backup["Id"] != old["Id"] or backup["Image"] != old["Image"]:
                raise RuntimeError("旧应用备份身份不一致，恢复材料已保留")
            intent(root, state, "rollback_rename_old_" + kind)
            run(["docker", "rename", backup["Id"], name])
            current = inspect(name)
        if current["Id"] != old["Id"] or current["Image"] != old["Image"]:
            raise RuntimeError("原应用身份未确认，停止恢复")
        intent(root, state, "rollback_start_old_" + kind)
        if not current["State"]["Running"]:
            start_container(old["Id"])
        wait_http("http://127.0.0.1:8086/health" if kind == "backend" else "http://127.0.0.1:5175/login")
    verify_baseline(root, apps=False)
    wait_http("https://goodhr.58it.cn/login")
    state["rolled_back"] = True
    intent(root, state, "rollback_complete")
    save(root, "rollback-result.private.json", {"restored": True, "database_restored": False, "new_tables_retained": True, "ledger_retained": True, "old_container_ids": {kind: originals["goodhr-production-" + kind + "-1"]["Id"] for kind in state["changed"]}})
    print(json.dumps({"rollback": "complete", "database_restored": False, "new_tables_retained": True, "ledger_retained": True}))


def cutover(root):
    """演练通过后暂停旧后端、事务追加两项迁移、保留旧容器并切换，失败时自动恢复旧应用。"""
    verify_baseline(root)
    manifest = check_materials(root)
    result = load(root, "rehearsal-result.private.json")
    if not result.get("ready") or result["commit"] != manifest["commit"]:
        raise RuntimeError("缺少当前提交的完整恢复演练")
    if (root / "cutover-progress.private.json").exists():
        raise RuntimeError("已有切换进度，禁止盲目重跑；请核实结果或执行rollback")
    if ledger("postgres", "goodhr5") != manifest["ledger"] or set(tables("postgres", "goodhr5")) != set(manifest["tables"]):
        raise RuntimeError("正式库ledger或表基线变化，未暂停应用")
    for kind in ("backend", "frontend"):
        if existing("goodhr-production-" + kind + "-1-backup-" + root.name):
            raise RuntimeError("旧应用备份容器名称已占用")
    state = {"changed": [], "new": {}, "migrated": False, "rolled_back": False, "last_operation": "begin_cutover"}
    intent(root, state, "begin_cutover")
    try:
        state["changed"].append("backend")
        intent(root, state, "stop_old_backend")
        run(["docker", "stop", "-t", "20", BASELINE["goodhr-production-backend-1"][0]], timeout=45)
        before = snapshot("postgres", "goodhr5", set(manifest["tables"]) - {"schema_migrations"})
        ledger_hash = old_ledger_snapshot("postgres", "goodhr5", manifest["ledger"])
        save(root, "before-migration-snapshot.private.json", before)
        intent(root, state, "migrate_production_database")
        sql("postgres", "goodhr5", migration_sql(root), readonly=False)
        state["migrated"] = True
        intent(root, state, "verify_production_migration")
        report = verify_migrated(root, "postgres", "goodhr5", before, ledger_hash)
        for kind in ("backend", "frontend"):
            name = "goodhr-production-" + kind + "-1"
            if kind == "frontend":
                state["changed"].append(kind)
                intent(root, state, "stop_old_frontend")
                run(["docker", "stop", "-t", "20", BASELINE[name][0]], timeout=45)
            intent(root, state, "preserve_old_" + kind)
            run(["docker", "rename", BASELINE[name][0], name + "-backup-" + root.name])
            intent(root, state, "create_new_" + kind)
            state["new"][kind] = create_application(root, kind, name, manifest["images"][kind]["id"])
            intent(root, state, "start_new_" + kind)
            start_container(state["new"][kind])
            wait_http("http://127.0.0.1:8086/health" if kind == "backend" else "http://127.0.0.1:5175/login")
        verify_http("http://127.0.0.1:8086", "http://127.0.0.1:5175")
        wait_http("https://goodhr.58it.cn/login")
        wait_http("https://goodhr.58it.cn/api/recruitment/company", 401)
        wait_http("https://goodhr.58it.cn/api/public/recruitment/invitations/" + INVALID_TOKEN, 404)
        wait_http("https://goodhr.58it.cn/admin/recruitment")
        wait_http("https://goodhr.58it.cn/interview-invitation/" + INVALID_TOKEN)
        verify_baseline(root, apps=False)
        if ledger("postgres", "goodhr5") != sorted(set(manifest["ledger"]) | ALLOWED):
            raise RuntimeError("新应用启动后的ledger发生变化")
        state["complete"] = True
        intent(root, state, "cutover_complete")
        save(root, "deployment-result.private.json", {"complete": True, "commit": manifest["commit"], "new_container_ids": state["new"], "report": report})
        print(json.dumps({"cutover": "complete", "commit": manifest["commit"], "new_container_ids": state["new"], "report": report}, ensure_ascii=False))
    except BaseException:
        try:
            rollback(root)
        except BaseException:
            raise RuntimeError("发布未完成且自动恢复受阻；私有进度与旧容器保留，请核对容器状态和ledger，禁止全库恢复或删除新增资料")
        raise RuntimeError("发布未完成，旧应用已恢复；新增表、资料和ledger保留，请核对rollback-result.private.json")


def cleanup(root):
    """仅移除本次记录的隔离候选和恢复PG容器，保留dump、私有材料及独立数据目录。"""
    verify_baseline(root, apps=False)
    state = load(root, "rehearsal.private.json")
    if state["database"] != VERIFY_DATABASE or not state["database_container"].startswith("goodhr-hr-db-"):
        raise RuntimeError("隔离恢复库名称不符合约束")
    values = list(reversed(list(state["candidates"].values()))) + [{"name": state["database_container"], "id": state["database_id"], "image": BASELINE["postgres"][1]}]
    removed = []
    for value in values:
        if value["name"] in BASELINE:
            raise RuntimeError("清理目标出现正式容器，拒绝清理")
        item = existing(value["name"])
        if item is None:
            removed.append(value["name"])
            continue
        labels = item["Config"].get("Labels") or {}
        if (value["id"] and item["Id"] != value["id"]) or item["Image"] != value["image"] or labels.get("goodhr.release") != root.name:
            raise RuntimeError("隔离容器身份变化，停止清理")
        value["id"] = item["Id"]
        state["last_operation"] = "cleanup_" + value["name"]
        save(root, "rehearsal.private.json", state)
        if item["State"]["Running"]:
            run(["docker", "stop", "-t", "10", item["Id"]], timeout=30)
        run(["docker", "rm", item["Id"]])
        removed.append(value["name"])
    state["cleaned"] = True
    state["last_operation"] = "cleanup_complete"
    save(root, "rehearsal.private.json", state)
    save(root, "cleanup-result.private.json", {"removed_containers": removed, "isolated_data_retained": True, "backup_retained": True})
    print(json.dumps({"cleanup": "complete", "removed_containers": removed, "isolated_data_retained": True}))


def interrupt_release(signum, frame):
    """将正常终止信号转换为异常，使切换阶段执行已有容器恢复流程。"""
    raise RuntimeError("发布进程收到终止信号，需核对私有进度")


def main():
    """将发布目录限制在根盘指定范围，以跨命令私有锁串行执行明确阶段。"""
    parser = argparse.ArgumentParser()
    parser.add_argument("operation", choices=("prepare", "rehearse", "cutover", "rollback", "cleanup"))
    parser.add_argument("--root", required=True)
    for name in ("commit", "archive", "archive-sha256", "backend-image", "frontend-image"):
        parser.add_argument("--" + name)
    args = parser.parse_args()
    signal.signal(signal.SIGTERM, interrupt_release)
    os.umask(0o077)
    root = pathlib.Path(args.root).resolve()
    if root.parent != pathlib.Path("/opt/goodhr-release") or not re.fullmatch(r"hr-interview-[A-Za-z0-9_-]+", root.name):
        raise RuntimeError("发布目录必须为/opt/goodhr-release/hr-interview-*独立目录")
    root.mkdir(mode=0o700, parents=True, exist_ok=True)
    root.chmod(0o700)
    if os.stat(str(root)).st_dev != os.stat("/").st_dev:
        raise RuntimeError("发布材料必须放在根盘，不能使用/www数据盘")
    with (root.parent / ".hr-interview-release.lock").open("a") as lock:
        os.chmod(lock.name, 0o600)
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        operations = {"prepare": lambda: prepare(root, args), "rehearse": lambda: rehearse(root), "cutover": lambda: cutover(root), "rollback": lambda: rollback(root), "cleanup": lambda: cleanup(root)}
        operations[args.operation]()


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(json.dumps({"success": False, "error": str(error)}, ensure_ascii=False))
        raise SystemExit(1)
