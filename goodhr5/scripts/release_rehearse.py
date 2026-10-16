"""文件作用：在专属内部Docker网络恢复完整备份，以只读应用权限核验零迁移发布和完整回退组合；不切换生产、不覆盖全局登记、不删除Docker对象。"""

import argparse
from contextlib import contextmanager
import datetime as dt
import hashlib
import io
import json
import os
from pathlib import Path
import re
import secrets
import shlex
import socket
import stat
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.parse

from release_activate import COMMIT, HEX, IMAGE, TAG, DockerEngine, save_json
from release_cutover import CutoverError, require
from release_retention import deployment_locks, digest_file, verified_records


ROOT = Path("/opt/goodhr-release")
IDENTIFIER = re.compile(r"[A-Za-z_][A-Za-z0-9_]*\Z")
ROLES = ("backend", "frontend")
NODE_PROBE = """const u=process.argv[1];fetch(u,{redirect:'manual',signal:AbortSignal.timeout(5000)}).then(r=>console.log(r.status)).catch(()=>process.exit(2));"""


def command(arguments, data=None, output=None, timeout=120, source=None):
    """command 执行固定参数数组，不在失败日志输出命令、数据库正文或私密环境。"""
    try:
        result = subprocess.run(arguments, input=data, stdin=source, stdout=output or subprocess.PIPE,
                                stderr=subprocess.PIPE, timeout=timeout, check=False)
    except (OSError, subprocess.TimeoutExpired):
        raise CutoverError("隔离检查命令不可用或超时") from None
    require(result.returncode == 0, "隔离检查命令失败，未签发成功证据")
    return result.stdout


def migration_fingerprint(filenames):
    """migration_fingerprint 与正式登记使用相同的排序JSON和UTF8摘要算法。"""
    require(isinstance(filenames, list) and filenames and all(isinstance(row, str) for row in filenames), "迁移账本为空或格式异常")
    require(len(filenames) == len(set(filenames)), "迁移账本存在重复项")
    return hashlib.sha256(json.dumps(sorted(filenames), ensure_ascii=False, separators=(",", ":")).encode()).hexdigest()


def quote_identifier(value):
    """quote_identifier 对数据库实际表名做SQL标识符转义，不拼入未经转义的名称。"""
    require(isinstance(value, str) and value and not any(char in value for char in "\x00\r\n\t"), "数据库对象名称不明确")
    return '"' + value.replace('"', '""') + '"'


def assert_preserved(before, after):
    """assert_preserved 严格比较全部旧表的行数和完整行摘要，不忽略消息、退款或配置表。"""
    require(isinstance(before, dict) and before and isinstance(after, dict), "数据库基线不完整")
    changed = sorted(key for key in set(before) | set(after) if before.get(key) != after.get(key))
    require(not changed, "恢复库表数据发生变化：" + ", ".join(changed))
    return True


def backend_environment(database, password, network_prefix):
    """backend_environment 仅创建隔离必需配置，不复制生产密码、支付证书、SMTP或外部Redis设置。"""
    require(IDENTIFIER.fullmatch(database) and re.fullmatch(r"[a-z0-9-]+", network_prefix), "隔离数据库或网络名称异常")
    dsn = "postgresql://rehearsal_app:" + urllib.parse.quote(password, safe="") + "@" + network_prefix + "-pg:5432/" + database + "?sslmode=disable"
    return ["GOODHR_PG_DSN=" + dsn, "GOODHR_REDIS_ADDR=" + network_prefix + "-redis:6379",
            "GOODHR_REDIS_PASSWORD=", "GOODHR_REDIS_DB=0", "GOODHR_CLOUD_ADDR=0.0.0.0:8084",
            "GOODHR_DISABLE_EMAIL_SCHEDULER=true", "GOODHR_SMTP_HOST=", "GOODHR_SMTP_USERNAME=",
            "GOODHR_SMTP_PASSWORD=", "GOODHR_PUBLIC_BASE_URL=http://" + network_prefix + "-frontend:3000"]


def dsn_database(environment):
    """dsn_database 仅在内存校验实际服务连接库名，冲突DSN或无法解析时禁止备份错库。"""
    values = [row.split("=", 1)[1] for row in environment if row.startswith("GOODHR_PG_DSN=")]
    require(values and len(set(values)) == 1, "原服务数据库DSN缺失或存在冲突")
    dsn = values[0]
    if dsn.startswith(("postgres://", "postgresql://")):
        result = urllib.parse.unquote(urllib.parse.urlsplit(dsn).path.lstrip("/"))
    else:
        fields = dict(row.split("=", 1) for row in shlex.split(dsn) if "=" in row)
        result = fields.get("dbname", "")
    require(IDENTIFIER.fullmatch(result), "原服务数据库名称无法确认")
    return result


def evidence_documents(host, commit, release_id, candidate, rollback, fingerprint, reports):
    """evidence_documents 只有两个完整组合均实际通过后才生成绑定主机、提交、镜像及账本的证据。"""
    require(COMMIT.fullmatch(commit) and TAG.fullmatch(release_id) and HEX.fullmatch(fingerprint), "发布证据身份不完整")
    require(set(candidate) == set(rollback) == set(ROLES) and all(IMAGE.fullmatch(value) for pair in (candidate, rollback) for value in pair.values()), "前后端镜像组合不完整")
    require(all(candidate[role] != rollback[role] for role in ROLES), "每端必须有独立的完整历史回退组合")
    require(set(reports) == {"candidate", "rollback"}, "缺少实际候选或回退报告")
    for name, images in (("candidate", candidate), ("rollback", rollback)):
        report = reports[name]
        require(all(report.get(key) is True for key in ("success", "legacy_data_preserved", "contract_verified")), "组合检查尚未全部成功")
        require(report.get("host") == host and report.get("migration_fingerprint") == fingerprint and all(report.get(role + "_image") == images[role] for role in ROLES), "组合报告与镜像或账本不匹配")
    return {"stage": "isolated_validation", "host": host, "source_commit": commit, "release_id": release_id,
            "success": True, "legacy_data_preserved": True, "rollback_verified": True,
            "backend_image": candidate["backend"], "frontend_image": candidate["frontend"],
            "before_migration_fingerprint": fingerprint, "after_migration_fingerprint": fingerprint,
            "expected_new_migrations": 0, "contract_scope": "read_only_startup_and_anonymous_routes",
            "business_write_validation": "requires_separate_change_specific_tests"}


def cleanup_manifest(host, release_id, prefix, network, containers, directory):
    """cleanup_manifest 只为本轮已停止对象生成精确清单，数据目录和原始备份明确保留，不执行删除。"""
    require(HEX.fullmatch(network.get("Id", "")) and network.get("Name") == prefix and network.get("Internal") is True, "清理网络身份或隔离状态不明确")
    require((network.get("Labels") or {}).get("goodhr.rehearsal.id") == release_id, "清理网络不属于本轮")
    records = []
    for value in containers:
        require(HEX.fullmatch(value.get("Id", "")) and value["Name"].lstrip("/").startswith(prefix + "-"), "清理容器名称或ID越界")
        require(value["State"]["Running"] is False and (value["Config"].get("Labels") or {}).get("goodhr.rehearsal.id") == release_id, "仅能登记本轮已停止的隔离容器")
        records.append({"id": value["Id"], "name": value["Name"], "image": value["Image"], "running": False})
    require(len(records) == len({row["id"] for row in records}), "清理容器ID重复")
    return {"schema_version": 1, "kind": "rehearsal_only_cleanup_plan", "host": host, "release_id": release_id,
            "containers": records, "network": {"id": network["Id"], "name": prefix},
            "preserve_directory": str(directory), "delete_files": False, "delete_volumes": False,
            "procedure": ["重新inspect并逐一核对id/name/image、停止状态和本轮标签", "按精确ID执行docker container rm，不使用force或-v", "核对网络ID、标签且无其他端点后按精确ID执行docker network rm", "保留完整dump、pgdata及全部证据文件"]}


@contextmanager
def rehearsal_locks(source_root):
    """rehearsal_locks 沿用正式入口的源码、总发布及三把应用锁，禁止与切换或源码更新并行。"""
    import fcntl
    handles = []
    try:
        for path in (source_root / ".release-source.lock", ROOT / ".release-operation.lock"):
            require(path.resolve() == path and path.is_file() and not path.is_symlink(), "既有发布锁缺失或路径异常")
            descriptor = os.open(str(path), os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC)
            handles.append(descriptor)
            info = os.fstat(descriptor)
            require(stat.S_ISREG(info.st_mode) and info.st_uid == 0, "发布锁不是root普通文件")
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        with deployment_locks():
            yield
    finally:
        for descriptor in reversed(handles):
            os.close(descriptor)


class Rehearsal:
    """仅拥有本轮创建的隔离对象；任何启动和停止都核对本轮ID，不提供删除或生产激活入口。"""

    def __init__(self, arguments, engine):
        """__init__ 绑定明确输入，构造私密凭据但不立即创建容器、网络或备份。"""
        self.args, self.engine = arguments, engine
        self.root = Path(arguments.release_dir)
        self.directory = self.root / "rehearsal"
        self.prefix = "goodhr-rehearsal-" + hashlib.sha256(arguments.release_id.encode()).hexdigest()[:12]
        self.created = {}
        self.host = socket.gethostname()
        self.candidate = {role: getattr(arguments, role + "_image") for role in ROLES}
        self.password = secrets.token_hex(24)
        self.app_password = secrets.token_hex(24)
        self.reports = {}
        self.journal = {"schema_version": 1, "host": self.host, "release_id": arguments.release_id,
                        "source_commit": arguments.source_commit, "success": False, "created": {}}

    def validate(self):
        """validate 在创建对象前确认本机、干净源码、当前容器身份、候选标签和专属路径。"""
        a = self.args
        require(os.name == "posix" and self.host == a.host, "主机或运行系统不匹配")
        require(TAG.fullmatch(a.release_id) and COMMIT.fullmatch(a.source_commit), "发布编号或源码提交不完整")
        require(self.root.parent == ROOT and self.root.name == a.release_id and self.root.resolve() == self.root and self.root.is_dir(), "必须使用已有的本次正式发布目录")
        require(not self.directory.exists() and not self.directory.is_symlink(), "隔离目录已存在，请先审阅既有材料")
        require(IDENTIFIER.fullmatch(a.database) and IDENTIFIER.fullmatch(a.postgres_user), "数据库或角色名称异常")
        source = Path(a.source_root)
        require(source.resolve() == source and source.name == "goodhr5", "源码根路径不明确")
        require(command(["git", "-C", str(source), "rev-parse", "HEAD"]).decode().strip() == a.source_commit, "源码提交与本次候选不一致")
        require(not command(["git", "-C", str(source), "status", "--porcelain", "--untracked-files=normal"]).strip(), "源码有未提交改动")
        self.originals = {role: self.engine.inspect("goodhr-production-" + role + "-1") for role in ROLES}
        self.rollback = {role: self.originals[role]["Image"] for role in ROLES}
        require(dsn_database(self.originals["backend"]["Config"].get("Env") or []) == a.database, "备份库名与原服务实际数据库不一致")
        for role in ROLES:
            old = self.originals[role]
            require(old["Id"] == getattr(a, role + "_container_id") and old["State"]["Running"], "当前正式应用身份或状态已变化")
            metadata = self.engine.image(self.candidate[role])
            labels = metadata["Config"].get("Labels") or {}
            require(labels.get("goodhr.source_commit") == a.source_commit and labels.get("goodhr.release_kind") == role and labels.get("goodhr.release.id") == a.release_id, "候选镜像来源、角色或发布编号不符")
            require(self.candidate[role] != self.rollback[role], "候选与当前运行镜像相同")
        self.production_pg = self.engine.inspect(a.postgres_container)
        require(self.production_pg["Id"] == a.postgres_container_id and self.production_pg["State"]["Running"], "备份来源PostgreSQL身份已变化")
        self.pg_image = self.engine.image(self.production_pg["Image"])
        self.redis_image = self.engine.image(a.redis_image)
        return source

    def own(self, name, payload):
        """own 只创建本轮独立名称的容器，禁止生产名称、宿主网络、发布端口和外部挂载。"""
        require(name.startswith(self.prefix + "-") and name not in self.created, "隔离容器名称重复或越界")
        payload.setdefault("Labels", {}).update({"goodhr.project": "rehearsal", "goodhr.rehearsal.id": self.args.release_id})
        host = payload.setdefault("HostConfig", {})
        require(host.get("NetworkMode") == self.prefix and not host.get("PortBindings") and not host.get("Privileged"), "隔离容器不允许宿主网络、外部端口或特权")
        for binding in host.get("Binds", []):
            require(binding.split(":", 1)[0].startswith(str(self.directory) + "/"), "隔离挂载不能引用生产或外部目录")
        host.update({"RestartPolicy": {"Name": "no"}, "PidsLimit": 256})
        identifier = self.engine.create(name, payload)
        self.created[name] = identifier
        self.journal["created"][name] = identifier
        save_json(self.directory / "journal.json", self.journal)
        return identifier

    def stop(self, identifier):
        """stop 只停止本轮创建且标签仍匹配的明确ID，不触碰任何原生产容器。"""
        require(identifier in self.created.values(), "停止目标不是本轮隔离对象")
        actual = self.engine.inspect(identifier)
        require((actual["Config"].get("Labels") or {}).get("goodhr.rehearsal.id") == self.args.release_id, "隔离对象标签发生变化")
        if actual["State"]["Running"]:
            self.engine.stop(identifier)

    def psql(self, sql):
        """psql 仅在本轮恢复库执行SQL，通过标准输入传递，查询正文和密码不进入日志。"""
        return command(["docker", "exec", "-i", self.pg, "psql", "-XqAt", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", self.args.database], sql.encode())

    def database_snapshot(self):
        """database_snapshot 对所有非系统表流式计算排序完整行SHA和行数，仅保留不可逆摘要。"""
        tables = self.psql("SELECT n.nspname||E'\\t'||c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.relkind IN ('r','p') AND n.nspname !~ '^pg_' AND n.nspname<>'information_schema' ORDER BY 1;").decode().splitlines()
        result = {}
        for table in tables:
            schema, name = table.split("\t")
            target = quote_identifier(schema) + "." + quote_identifier(name)
            sql = "SET statement_timeout='90s'; COPY (SELECT row_to_json(t)::text FROM " + target + " t ORDER BY row_to_json(t)::text COLLATE \"C\") TO STDOUT;"
            digest, rows = hashlib.sha256(), 0
            with tempfile.TemporaryFile() as errors:
                process = subprocess.Popen(["docker", "exec", "-i", self.pg, "psql", "-XqAt", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", self.args.database], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=errors)
                try:
                    process.stdin.write(sql.encode())
                    process.stdin.close()
                    for line in process.stdout:
                        digest.update(line)
                        rows += 1
                    require(process.wait(timeout=120) == 0, "恢复库表摘要计算失败：" + table)
                finally:
                    process.stdout.close()
                    if process.poll() is None:
                        process.kill()
                        process.wait()
            result[table] = {"rows": rows, "sha256": digest.hexdigest()}
        require(result, "恢复库没有业务表")
        return result

    def ledger(self):
        """ledger 单独读取原始迁移文件名，零迁移发布必须与恢复基线完全一致。"""
        return self.psql("SELECT filename FROM public.schema_migrations ORDER BY filename;").decode().splitlines()

    def restore(self):
        """restore 从明确生产库只读导出完整dump并在无外网网络的新PostgreSQL中恢复。"""
        self.directory.mkdir(mode=0o700)
        network = self.engine.request("POST", self.engine.prefix + "/networks/create", {"Name": self.prefix, "Driver": "bridge", "Internal": True, "CheckDuplicate": True, "Labels": {"goodhr.rehearsal.id": self.args.release_id}})
        self.journal["network"] = network["Id"]
        require(self.engine.network(self.prefix).get("Internal") is True, "隔离网络没有禁用外部路由")
        dump = self.directory / "database.dump"
        with dump.open("xb") as destination:
            os.chmod(dump, 0o600)
            command(["docker", "exec", self.production_pg["Id"], "pg_dump", "-U", self.args.postgres_user, "-Fc", self.args.database], output=destination, timeout=600)
        require(dump.stat().st_size > 0, "完整备份为空")
        with dump.open("rb") as source:
            toc = command(["docker", "exec", "-i", self.production_pg["Id"], "pg_restore", "--list"], source=source)
        (self.directory / "database-toc.txt").write_bytes(toc)
        os.chmod(self.directory / "database-toc.txt", 0o600)
        volumes = list((self.pg_image["Config"].get("Volumes") or {}).keys())
        require(len(volumes) == 1 and volumes[0].startswith("/var/lib/postgresql"), "PostgreSQL镜像挂载形态需单独审阅")
        data_path = self.directory / "pgdata"
        data_path.mkdir(mode=0o700)
        self.pg = self.own(self.prefix + "-pg", {"Image": self.pg_image["Id"], "Env": ["POSTGRES_USER=postgres", "POSTGRES_DB=" + self.args.database, "POSTGRES_PASSWORD=" + self.password], "HostConfig": {"NetworkMode": self.prefix, "Memory": 384 * 1024**2, "Binds": [str(data_path) + ":" + volumes[0]]}})
        self.engine.start(self.pg)
        for attempt in range(40):
            try:
                command(["docker", "exec", self.pg, "pg_isready", "-U", "postgres", "-d", self.args.database], timeout=5)
                break
            except CutoverError:
                if attempt == 39:
                    raise
                time.sleep(1)
        with dump.open("rb") as source:
            command(["docker", "exec", "-i", self.pg, "pg_restore", "-U", "postgres", "-d", self.args.database, "--no-owner", "--no-acl", "--exit-on-error"], source=source, timeout=600)
        self.baseline = self.database_snapshot()
        self.filenames = self.ledger()
        self.fingerprint = migration_fingerprint(self.filenames)
        save_json(self.directory / "baseline.json", {"tables": self.baseline, "migration_filenames": self.filenames, "migration_fingerprint": self.fingerprint, "dump_sha256": digest_file(dump)}, exclusive=True)
        self.psql("CREATE ROLE rehearsal_app LOGIN PASSWORD '" + self.app_password + "'; GRANT CONNECT ON DATABASE " + quote_identifier(self.args.database) + " TO rehearsal_app; GRANT USAGE, CREATE ON SCHEMA public TO rehearsal_app; GRANT SELECT ON ALL TABLES IN SCHEMA public TO rehearsal_app;")
        writable = self.psql("SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.relkind IN ('r','p') AND n.nspname !~ '^pg_' AND n.nspname<>'information_schema' AND has_table_privilege('rehearsal_app',c.oid,'INSERT,UPDATE,DELETE,TRUNCATE');").decode().strip()
        require(writable == "0", "隔离应用角色意外具有业务表写入权限")
        redis_path = self.directory / "redis-data"
        redis_path.mkdir(mode=0o700)
        redis = self.own(self.prefix + "-redis", {"Image": self.redis_image["Id"], "Cmd": ["redis-server", "--save", "", "--appendonly", "no"], "HostConfig": {"NetworkMode": self.prefix, "Memory": 64 * 1024**2, "Binds": [str(redis_path) + ":/data"]}})
        self.engine.start(redis)

    def probe(self, frontend, url, expected, wait=False):
        """probe 仅从本轮前端访问内部服务，返回状态码，不读取候选人或账户数据正文。"""
        require(url.startswith("http://" + self.prefix + "-") and expected in (200, 401, 404), "隔离HTTP目标或预期状态异常")
        for attempt in range(30 if wait else 1):
            try:
                code = command(["docker", "exec", frontend, "node", "-e", NODE_PROBE, url], timeout=8).decode().strip()
                require(code == str(expected), "隔离接口状态未符合预期：" + url.split("/", 3)[-1])
                return int(code)
            except CutoverError:
                if not wait or attempt == 29:
                    raise
                time.sleep(1)

    def pair(self, label, images):
        """pair 在同一恢复库依次验证候选及回退，停止应用后严格比较全部旧数据和迁移账本。"""
        backend = self.own(self.prefix + "-backend", {"Image": images["backend"], "Env": backend_environment(self.args.database, self.app_password, self.prefix), "HostConfig": {"NetworkMode": self.prefix, "Memory": 256 * 1024**2}})
        archive = command(["docker", "cp", backend + ":/app/db/migrations", "-"], timeout=30)
        with tarfile.open(fileobj=io.BytesIO(archive)) as files:
            packaged = sorted(Path(member.name).name for member in files.getmembers() if member.isfile() and member.name.endswith(".sql") and not member.name.endswith(".down.sql"))
        require(packaged == sorted(self.filenames), "镜像迁移集合与恢复账本不一致，本工具只接受零迁移发布")
        frontend = self.own(self.prefix + "-frontend", {"Image": images["frontend"], "Env": ["PORT=3000", "HOSTNAME=0.0.0.0", "CLOUD_API_BASE=http://" + self.prefix + "-backend:8084", "NEXT_PUBLIC_ONBOARDING_PREVIEW_MODE="], "HostConfig": {"NetworkMode": self.prefix, "Memory": 384 * 1024**2}})
        checks = []
        try:
            self.engine.start(backend)
            self.engine.start(frontend)
            base = "http://" + self.prefix + "-backend:8084"
            checks.append({"path": "/health", "status": self.probe(frontend, base + "/health", 200, wait=True)})
            for path in ("/api/auth/me", "/api/candidates", "/api/positions", "/api/resume-library/settings"):
                checks.append({"path": path, "status": self.probe(frontend, base + path, 401)})
            for path in ("/", "/login", "/download", "/admin/resumes"):
                checks.append({"frontend_path": path, "status": self.probe(frontend, "http://" + self.prefix + "-frontend:3000" + path, 200, wait=True)})
        finally:
            self.stop(backend)
            self.stop(frontend)
        after = self.database_snapshot()
        assert_preserved(self.baseline, after)
        require(self.ledger() == self.filenames, "迁移账本发生变化")
        report = {"host": self.host, "success": True, "legacy_data_preserved": True, "contract_verified": True,
                  "migration_fingerprint": self.fingerprint, "backend_image": images["backend"], "frontend_image": images["frontend"],
                  "contract_scope": "read_only_startup_and_anonymous_routes", "checks": checks, "tables": after}
        save_json(self.directory / (label + "-pair-summary.json"), report, exclusive=True)
        self.reports[label] = report
        # 两轮使用相同明确DNS名；旧容器保留，改成本轮已绑定的独立归档名称，不删除对象。
        for role, identifier in (("backend", backend), ("frontend", frontend)):
            name = self.prefix + "-" + role
            self.engine.request("POST", self.engine.prefix + "/containers/" + identifier + "/rename?name=" + urllib.parse.quote(name + "-" + label))
            del self.created[name]
            self.created[name + "-" + label] = identifier

    def finish(self):
        """finish 再核对生产未切换，只写本次专属登记和真实成功证据，不覆盖全局登记。"""
        for role in ROLES:
            actual = self.engine.inspect("goodhr-production-" + role + "-1")
            require(actual["Id"] == self.originals[role]["Id"] and actual["Image"] == self.rollback[role] and actual["State"]["Running"], "演练期间生产应用身份发生变化")
        evidence = evidence_documents(self.host, self.args.source_commit, self.args.release_id, self.candidate, self.rollback, self.fingerprint, self.reports)
        records = []
        for label, images in (("candidate", self.candidate), ("rollback", self.rollback)):
            path = self.directory / (label + "-pair-summary.json")
            records.append({"id": self.args.release_id + "-" + label, "verified_at": dt.datetime.now(dt.timezone.utc).isoformat(), "compatible": True,
                            "migration_fingerprint": self.fingerprint, "backend_image": images["backend"], "frontend_image": images["frontend"],
                            "evidence_path": str(path), "evidence_sha256": digest_file(path)})
        registry = {"schema_version": 1, "host": self.host, "current_migration_fingerprint": self.fingerprint,
                    "migration_probe": {"container": self.args.postgres_container, "database": self.args.database, "user": self.args.postgres_user}, "verified_releases": records}
        verified_records(registry, self.host)
        save_json(self.root / "retention-registry.json", registry, exclusive=True)
        save_json(self.root / "isolated-validation.json", evidence, exclusive=True)
        self.journal["success"] = True

    def record_cleanup(self):
        """record_cleanup 保存可复核的本轮对象删除计划，不把广域清理交给正式发布finalize。"""
        if "network" not in self.journal:
            return
        network = self.engine.network(self.prefix)
        require(network["Id"] == self.journal["network"], "本轮网络ID已变化")
        values = [self.engine.inspect(identifier) for identifier in self.created.values()]
        plan = cleanup_manifest(self.host, self.args.release_id, self.prefix, network, values, self.directory)
        path = self.directory / "cleanup-plan.json"
        save_json(path, plan)
        self.journal["cleanup_plan"] = {"path": str(path), "sha256": digest_file(path)}

    def run(self):
        """run 仅由显式命令执行隔离演练，失败保留原始dump、数据目录和已创建对象用于排错。"""
        source = self.validate()
        with rehearsal_locks(source):
            self.validate()
            try:
                self.restore()
                self.pair("candidate", self.candidate)
                self.pair("rollback", self.rollback)
            finally:
                for identifier in reversed(list(self.created.values())):
                    self.stop(identifier)
                if self.directory.exists():
                    self.record_cleanup()
                    save_json(self.directory / "journal.json", self.journal)
            self.finish()
            save_json(self.directory / "journal.json", self.journal)
        return {"success": True, "evidence": str(self.root / "isolated-validation.json"), "registry": str(self.root / "retention-registry.json"), "scope": "read_only_startup_and_anonymous_routes"}


def main():
    """main 必须显式提供run、生产身份和不可变候选，帮助命令不会接触Docker或生产。"""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=("run",))
    for flag in ("host", "release-dir", "release-id", "source-root", "source-commit", "database", "postgres-container", "postgres-container-id", "postgres-user", "redis-image", "backend-container-id", "frontend-container-id", "backend-image", "frontend-image"):
        parser.add_argument("--" + flag, required=True)
    arguments = parser.parse_args()
    require(all(HEX.fullmatch(getattr(arguments, role + "_container_id")) and IMAGE.fullmatch(getattr(arguments, role + "_image")) for role in ROLES), "应用容器ID或候选镜像ID不完整")
    require(HEX.fullmatch(arguments.postgres_container_id) and IMAGE.fullmatch(arguments.redis_image), "PostgreSQL或Redis身份不完整")
    print(json.dumps(Rehearsal(arguments, DockerEngine()).run(), ensure_ascii=False))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (CutoverError, OSError, ValueError, subprocess.TimeoutExpired) as error:
        print(json.dumps({"success": False, "error": str(error)}, ensure_ascii=False), file=sys.stderr)
        sys.exit(1)
