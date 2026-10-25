"""文件作用：仅为2026-10-26筛选记录发布验证0099至0103五份固定迁移、旧业务数据保持及完整回退；不切换生产或删除对象。"""

import copy
import datetime as dt
import hashlib
import io
import json
from pathlib import Path
import subprocess
import sys
import tarfile
import urllib.parse

from release_activate import save_json
from release_cutover import CutoverError, require
from release_rehearse import (Rehearsal, ROLES, SCHEMA_SQL, assert_preserved, backend_environment,
                              command, evidence_documents, main, migration_fingerprint)
from release_retention import digest_file, verified_records


MIGRATIONS = (
    "0099_headhunter_customers_jobs.sql",
    "0100_ai_usage_daily_retention.sql",
    "0101_resume_vector_receipts.sql",
    "0102_resume_search_invalidation.sql",
    "0103_position_screening_records.sql",
)
NEW_TABLES = {
    "public.headhunter_customers", "public.headhunter_jobs", "public.headhunter_position_bindings",
    "public.ai_usage_daily", "public.candidate_resume_vectors", "public.resume_search_invalidations",
    "public.position_screening_records",
}
NEW_FUNCTIONS = {"public.version_resume_ownership_change()", "public.enqueue_resume_search_invalidation()"}
NEW_TRIGGERS = {"candidate_resume_ownership_version", "candidate_resume_search_invalidation"}
NEW_USAGE_INDEX = "idx_ai_balance_records_usage_retention"
EMPTY_SHA256 = hashlib.sha256(b"").hexdigest()


def packaged_migrations(archive):
    """packaged_migrations 只读取镜像tar内有限的SQL文本，拒绝重复文件名、链接及过大文件，不解包到磁盘。"""
    result = {}
    require(isinstance(archive, bytes) and len(archive) <= 16 * 1024**2, "迁移归档不完整或过大")
    with tarfile.open(fileobj=io.BytesIO(archive)) as files:
        for entry in files.getmembers():
            if not entry.name.endswith(".sql") or entry.name.endswith(".down.sql"):
                continue
            name = Path(entry.name).name
            require(entry.isfile() and name not in result and entry.size <= 4 * 1024**2,
                    "迁移文件类型、大小或名称重复")
            result[name] = files.extractfile(entry).read()
    require(result and len(result) <= 300, "镜像迁移集合为空或异常")
    return result


def assert_ledger_upgrade(before, after):
    """assert_ledger_upgrade 精确保留全部旧迁移行及时间，只允许本次五个文件名追加。"""
    def rows(values):
        """rows 将完整账本按文件名索引，拒绝重复或缺少真实执行时间。"""
        result = {}
        for row in values:
            require(isinstance(row, dict) and row.get("filename") and row.get("applied_at")
                    and row["filename"] not in result, "迁移账本不完整或重复")
            result[row["filename"]] = row
        require(result, "原迁移账本为空")
        return result
    old, new = rows(before), rows(after)
    require(not set(MIGRATIONS) & set(old) and set(new) == set(old) | set(MIGRATIONS),
            "本次并非只追加五份明确迁移")
    require(all(new[name] == row for name, row in old.items()), "旧迁移记录或执行时间变化")


def assert_business_preserved(before, after):
    """assert_business_preserved 对旧业务整表逐行摘要核对，仅审阅账本追加并要求七个新表为空。"""
    new_keys = {name.replace(".", "\t", 1) for name in NEW_TABLES}
    ledger = "public\tschema_migrations"
    require(set(after) == set(before) | new_keys and not set(before) & new_keys,
            "恢复库出现未批准的表增删")
    assert_preserved({k: v for k, v in before.items() if k != ledger},
                     {k: v for k, v in after.items() if k in before and k != ledger})
    require(all(after[name] == {"rows": 0, "sha256": EMPTY_SHA256} for name in new_keys),
            "新表不是空表")


def assert_schema_upgrade(before, after, expected):
    """assert_schema_upgrade 精确匹配独立参考库执行已绑定SQL得到的全部结构，再核对明确允许的对象增量。"""
    require(isinstance(expected, dict) and after == expected,
            "实际结构与独立参考库的固定SQL结构不同，字段、约束、索引或定义发生变化")
    require(isinstance(before, dict) and set(before) == set(after), "结构盘点范围不一致")
    require(not set(before["tables"]) & NEW_TABLES and set(after["tables"]) == set(before["tables"]) | NEW_TABLES,
            "表结构增删超出本次范围")
    old_tables = copy.deepcopy(after["tables"])
    for name in NEW_TABLES:
        table = old_tables.pop(name)
        require(table.get("kind") == "r" and table.get("persistence") == "p" and table.get("comment")
                and all(column.get("comment") for column in table.get("columns", []))
                and not table.get("triggers") and not table.get("policies"), "新表类型、中文注释或副作用异常")
    triggers = old_tables["public.candidate_profiles"]["triggers"]
    require(not NEW_TRIGGERS & set(before["tables"]["public.candidate_profiles"]["triggers"])
            and set(triggers) == set(before["tables"]["public.candidate_profiles"]["triggers"]) | NEW_TRIGGERS,
            "简历触发器变化超出范围")
    for name in NEW_TRIGGERS:
        definition = triggers.pop(name)
        require(definition.get("enabled") == "O" and "candidate_profiles" in definition.get("definition", ""),
                "新增触发器归属或启用状态异常")
    indexes = old_tables["public.ai_balance_records"]["indexes"]
    require(NEW_USAGE_INDEX not in before["tables"]["public.ai_balance_records"]["indexes"]
            and NEW_USAGE_INDEX in indexes, "消费清理索引未新增或已存在")
    indexes.pop(NEW_USAGE_INDEX)
    require(old_tables == before["tables"], "原表字段、约束、索引、策略、注释或所有者发生变化")
    functions = after["functions"]
    require(not set(before["functions"]) & NEW_FUNCTIONS and set(functions) == set(before["functions"]) | NEW_FUNCTIONS
            and all(functions[name] == row for name, row in before["functions"].items()),
            "函数变化超出本次范围")
    require(all(before[name] == value for name, value in after.items() if name not in {"tables", "functions"}),
            "视图、序列、类型、扩展或其他数据库对象变化")


class ScreeningRehearsal(Rehearsal):
    """复用既有完整备份、内网隔离、只读应用和收尾流程，新增受五份镜像SQL摘要约束的升级步骤。"""

    def migration_archive(self, image, label):
        """migration_archive 创建本次不启动的镜像检查容器，读取嵌入SQL，不访问生产配置或目录。"""
        container = self.own(self.prefix + "-migration-" + label,
                             {"Image": image, "HostConfig": {"NetworkMode": self.prefix}})
        return packaged_migrations(command(["docker", "cp", container + ":/app/db/migrations", "-"], timeout=30))

    def reference_schema(self, statements):
        """reference_schema 仅在本次隔离PG建立无业务数据的结构参考库，实际执行同一固定SQL推导精确DDL基线。"""
        reference = "screening_schema_reference"
        schema_dump = command(["docker", "exec", self.pg, "pg_dump", "-U", "postgres", "-d", self.args.database,
                               "--schema-only", "--no-owner", "--no-acl"], timeout=120)
        self.psql("CREATE DATABASE " + reference + " OWNER postgres;")
        prefix = ["docker", "exec", "-i", self.pg, "psql", "-XqAt", "-v", "ON_ERROR_STOP=1",
                  "-U", "postgres", "-d", reference]
        command(prefix, schema_dump, timeout=120)
        before = json.loads(command(prefix, SCHEMA_SQL.encode(), timeout=60))
        require(before == self.before_schema, "仅结构参考库未完整复现原数据库DDL，不能继续推导")
        command(prefix, "\n".join(statements).encode(), timeout=120)
        return json.loads(command(prefix, SCHEMA_SQL.encode(), timeout=60))

    def restore(self):
        """restore 先恢复完整生产备份，再以镜像内固定SQL在隔离库迁移；应用启动时仅有只读权限。"""
        require(self.additive is None, "本次专用演练不能与文章迁移例外混用")
        super().restore()
        self.before_filenames, self.before_fingerprint = list(self.filenames), self.fingerprint
        self.before_ledger, self.before_schema = self.ledger_records(), self.database_schema()
        legacy = copy.deepcopy(self.baseline)
        packaged = self.migration_archive(self.candidate["backend"], "candidate")
        previous = self.migration_archive(self.rollback["backend"], "rollback")
        require(set(packaged) == set(self.before_filenames) | set(MIGRATIONS)
                and not set(MIGRATIONS) & set(self.before_filenames), "候选迁移集合与生产基线不符")
        require(set(previous) == set(self.before_filenames)
                and all(packaged[name] == data for name, data in previous.items()),
                "旧迁移内容与实际回退镜像不一致")
        source = Path(self.args.source_root) / "cloud/backend/db/migrations"
        self.approved = {name: hashlib.sha256((source / name).read_bytes()).hexdigest() for name in MIGRATIONS}
        require(all(hashlib.sha256(packaged[name]).hexdigest() == digest for name, digest in self.approved.items()),
                "镜像迁移SQL与已绑定干净源码摘要不一致")
        statements = ["BEGIN;"]
        for name in MIGRATIONS:
            statements.extend([packaged[name].decode("utf-8"),
                               "INSERT INTO public.schema_migrations(filename) VALUES ('" + name + "');"])
        statements.append("COMMIT;")
        expected_schema = self.reference_schema(statements)
        self.psql("\n".join(statements))
        ledger, schema, migrated = self.ledger_records(), self.database_schema(), self.database_snapshot()
        assert_ledger_upgrade(self.before_ledger, ledger)
        assert_business_preserved(legacy, migrated)
        assert_schema_upgrade(self.before_schema, schema, expected_schema)
        self.psql("GRANT SELECT ON ALL TABLES IN SCHEMA public TO rehearsal_app; REVOKE CREATE ON SCHEMA public FROM rehearsal_app;")
        writable = self.psql("SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace "
                             "WHERE c.relkind IN ('r','p') AND n.nspname !~ '^pg_' AND n.nspname<>'information_schema' "
                             "AND has_table_privilege('rehearsal_app',c.oid,'INSERT,UPDATE,DELETE,TRUNCATE');").decode().strip()
        require(writable == "0", "迁移后应用角色仍具有写权限")
        self.filenames, self.baseline = self.ledger(), migrated
        self.fingerprint = migration_fingerprint(self.filenames)
        self.migrated_schema, self.migrated_ledger = schema, ledger
        save_json(self.directory / "screening-migration.json", {
            "approved_sha256": self.approved, "before_ledger": self.before_ledger, "after_ledger": ledger,
            "before_schema": self.before_schema, "after_schema": schema,
            "expected_schema": expected_schema, "schema_reference": "empty_schema_clone_with_same_image_sql",
            "old_business_data_preserved": True, "new_tables_empty": True,
            "execution": "fixed_image_sql_in_restored_database", "application_read_only": True,
        }, exclusive=True)

    def pair(self, label, images):
        """pair 在同一已迁移恢复库验证新旧完整组合，严格核对原镜像迁移集合及升级后全部数据和结构。"""
        backend = self.own(self.prefix + "-backend", {"Image": images["backend"],
            "Env": backend_environment(self.args.database, self.app_password, self.prefix),
            "HostConfig": {"NetworkMode": self.prefix, "Memory": 256 * 1024**2}})
        packaged = packaged_migrations(command(["docker", "cp", backend + ":/app/db/migrations", "-"], timeout=30))
        expected = self.filenames if label == "candidate" else self.before_filenames
        require(set(packaged) == set(expected), "候选或回退镜像的迁移集合变化")
        if label == "candidate":
            require(all(hashlib.sha256(packaged[name]).hexdigest() == digest for name, digest in self.approved.items()),
                    "候选内迁移摘要变化")
        frontend = self.own(self.prefix + "-frontend", {"Image": images["frontend"],
            "Env": ["PORT=3000", "HOSTNAME=0.0.0.0", "CLOUD_API_BASE=http://" + self.prefix + "-backend:8084",
                    "NEXT_PUBLIC_ONBOARDING_PREVIEW_MODE="],
            "HostConfig": {"NetworkMode": self.prefix, "Memory": 384 * 1024**2}})
        checks = []
        try:
            # PG18对既有迁移表的CREATE IF NOT EXISTS也检查schema CREATE；仅启动窗口授予，不授业务写权限。
            self.psql("GRANT CREATE ON SCHEMA public TO rehearsal_app;")
            self.engine.start(backend)
            self.engine.start(frontend)
            base = "http://" + self.prefix + "-backend:8084"
            checks.append({"path": "/health", "status": self.probe(frontend, base + "/health", 200, wait=True)})
            self.psql("REVOKE CREATE ON SCHEMA public FROM rehearsal_app;")
            privileges = self.psql("SELECT has_schema_privilege('rehearsal_app','public','CREATE') OR "
                "has_table_privilege('rehearsal_app','public.schema_migrations','INSERT,UPDATE,DELETE,TRUNCATE');").decode().strip()
            require(privileges == "f", "启动结束后仍有创建或迁移账本写入权限")
            paths = ["/api/auth/me", "/api/candidates", "/api/positions", "/api/resume-library/settings"]
            if label == "candidate":
                paths.append("/api/positions/00000000-0000-4000-8000-000000000002/screening-records")
            for path in paths:
                checks.append({"path": path, "status": self.probe(frontend, base + path, 401)})
            for path in ("/", "/login", "/download", "/admin/positions"):
                checks.append({"frontend_path": path, "status": self.probe(frontend,
                    "http://" + self.prefix + "-frontend:3000" + path, 200, wait=True)})
        finally:
            try:
                self.stop(backend)
            finally:
                try:
                    self.stop(frontend)
                finally:
                    self.psql("REVOKE CREATE ON SCHEMA public FROM rehearsal_app;")
        after = self.database_snapshot()
        assert_preserved(self.baseline, after)
        require(self.ledger_records() == self.migrated_ledger and self.database_schema() == self.migrated_schema,
                "应用启动或回退改变了数据库结构和迁移记录")
        report = {"host": self.host, "success": True, "legacy_data_preserved": True, "contract_verified": True,
            "migration_fingerprint": self.fingerprint, "backend_image": images["backend"], "frontend_image": images["frontend"],
            "contract_scope": "fixed_image_sql_upgrade_and_read_only_startup", "checks": checks,
            "startup_create_permission_window": True, "post_startup_write_and_create_permissions": False,
            "schema_verified": True, "approved_sha256": self.approved, "tables": after}
        save_json(self.directory / (label + "-pair-summary.json"), report, exclusive=True)
        self.reports[label] = report
        for role, identifier in (("backend", backend), ("frontend", frontend)):
            name = self.prefix + "-" + role
            self.engine.request("POST", self.engine.prefix + "/containers/" + identifier
                                + "/rename?name=" + urllib.parse.quote(name + "-" + label))
            del self.created[name]
            self.created[name + "-" + label] = identifier

    def finish(self):
        """finish 只有真实两组合及固定迁移均通过后才签发本次专属证据，不覆盖全局回退登记。"""
        for role in ROLES:
            current = self.engine.inspect("goodhr-production-" + role + "-1")
            require(current["Id"] == self.originals[role]["Id"] and current["Image"] == self.rollback[role]
                    and current["State"]["Running"], "演练期间生产应用身份变化")
        evidence = evidence_documents(self.host, self.args.source_commit, self.args.release_id,
            self.candidate, self.rollback, self.fingerprint, self.reports)
        require(all(report.get("schema_verified") is True and report.get("approved_sha256") == self.approved
                    for report in self.reports.values()), "组合缺少固定迁移和结构核对")
        evidence.update({"before_migration_fingerprint": self.before_fingerprint, "expected_new_migrations": len(MIGRATIONS),
                         "approved_sha256": self.approved, "schema_verified": True,
                         "contract_scope": "fixed_image_sql_upgrade_and_read_only_startup"})
        records = []
        for label, images in (("candidate", self.candidate), ("rollback", self.rollback)):
            path = self.directory / (label + "-pair-summary.json")
            records.append({"id": self.args.release_id + "-" + label, "verified_at": dt.datetime.now(dt.timezone.utc).isoformat(),
                "compatible": True, "migration_fingerprint": self.fingerprint, "backend_image": images["backend"],
                "frontend_image": images["frontend"], "evidence_path": str(path), "evidence_sha256": digest_file(path)})
        registry = {"schema_version": 1, "host": self.host, "current_migration_fingerprint": self.fingerprint,
            "migration_probe": {"container": self.args.postgres_container, "database": self.args.database, "user": self.args.postgres_user},
            "verified_releases": records}
        verified_records(registry, self.host)
        save_json(self.root / "retention-registry.json", registry, exclusive=True)
        save_json(self.root / "isolated-validation.json", evidence, exclusive=True)
        self.journal["success"] = True


if __name__ == "__main__":
    try:
        sys.exit(main(ScreeningRehearsal))
    except (CutoverError, OSError, ValueError, subprocess.TimeoutExpired) as error:
        print(json.dumps({"success": False, "error": str(error)}, ensure_ascii=False), file=sys.stderr)
        sys.exit(1)
