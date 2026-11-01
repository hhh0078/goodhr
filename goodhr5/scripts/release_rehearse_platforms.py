"""文件作用：仅隔离验证固定0106平台配置迁移及完整旧应用回退，复用正式演练的备份、内网、锁和精确清理，不切换生产。"""

import copy
import datetime as dt
import hashlib
import json
import re
import subprocess
import sys
import urllib.parse
import uuid

from release_activate import save_json
from release_cutover import CutoverError, require
from release_rehearse import (Rehearsal, ROLES, assert_preserved, backend_environment,
                              command, evidence_documents, main, migration_fingerprint)
from release_rehearse_screening import packaged_migrations
from release_retention import digest_file, verified_records


MIGRATION = "0106_zhaopin_search_51job_entries.sql"
LEDGER = "public\tschema_migrations"
CONFIGS = "public\tsystem_configs"
SUPPORTED = {
    "platform.zhaopin": ["https://rd6.zhaopin.com/app/recommend", "https://rd6.zhaopin.com/app/search"],
    "platform.51job": ["https://mall.51job.com/Revision/online/talentRecommend",
                       "https://mall.51job.com/Revision/online/talent/search"],
}
NEW_PLATFORM = {
    "id": "51job", "name": "前程无忧", "domain": "mall.51job.com", "open": True,
    "auth": {"pages": [
        {"url": SUPPORTED["platform.51job"][0], "entry": True, "match": "prefix", "title": "人才推荐"},
        {"url": SUPPORTED["platform.51job"][1], "match": "prefix", "title": "搜索人才"}],
        "entry_url": SUPPORTED["platform.51job"][0]},
    "public": {"pages": [{"url": "https://mall.51job.com/", "match": "exact", "code": "login",
                           "title": "前程无忧账号入口"}]},
}
NEW_DESCRIPTION = "前程无忧展示、登录入口和已开页面复用配置；页面操作使用本地平台配置"
CONFIG_COLUMNS = {"id", "config_key", "config_value", "description", "enabled", "created_at", "updated_at"}
SCOPE = "fixed_platform_configuration_auto_migration_and_read_only_startup"


def approval(digest):
    """approval 只接受本次固定文件的显式完整摘要，不提供任意迁移文件或零校验模式。"""
    require(isinstance(digest, str) and re.fullmatch(r"[0-9a-f]{64}", digest), "必须显式提供0106文件的完整SHA256")
    return {"filename": MIGRATION, "sha256": digest}


def timestamp(value):
    """timestamp 规范化PG的短时区及可变精度以兼容Python3.8，仍拒绝无时区或无效时间。"""
    require(isinstance(value, str), "配置或迁移时间格式异常")
    match = re.fullmatch(r"(\d{4}-\d{2}-\d{2})[T ](\d{2}:\d{2}:\d{2})(?:\.(\d{1,6}))?"
                         r"(Z|[+-](?:[01]\d|2[0-3])(?::[0-5]\d)?)", value.strip())
    require(match is not None, "配置或迁移时间格式异常")
    date, clock, fraction, offset = match.groups()
    offset = "+00:00" if offset == "Z" else offset + ":00" if len(offset) == 3 else offset
    normalized = date + "T" + clock + ("." + fraction.ljust(6, "0") if fraction else "") + offset
    try:
        result = dt.datetime.fromisoformat(normalized)
    except (TypeError, ValueError):
        raise CutoverError("配置或迁移时间格式异常") from None
    require(result.tzinfo is not None, "配置或迁移时间缺少时区")
    return result


def in_window(value, start, end):
    """in_window 要求新增执行时间确实落在本轮候选启动窗口，而非伪造旧账本。"""
    require(timestamp(start) <= timestamp(value) <= timestamp(end), "配置或迁移执行时间不在本轮启动窗口")


def keyed_rows(rows, key):
    """keyed_rows 按真实唯一键索引完整行，拒绝重复键、空键和不完整快照。"""
    require(isinstance(rows, list) and rows, "完整行快照为空")
    result = {}
    for row in rows:
        require(isinstance(row, dict) and isinstance(row.get(key), str) and row[key] and row[key] not in result,
                "完整行快照存在重复或无效键")
        result[row[key]] = row
    return result


def merged_value(value, key):
    """merged_value 独立计算已批准0106的精确JSON结果，保留默认入口、开放开关和所有自定义字段。"""
    result = copy.deepcopy(value)
    if not isinstance(result, dict):
        return result
    auth = result.get("auth") if isinstance(result.get("auth"), dict) else {}
    pages = auth.get("pages") if isinstance(auth.get("pages"), list) else []
    reuse = auth.get("reuse_urls") if isinstance(auth.get("reuse_urls"), list) else []
    for url in SUPPORTED[key]:
        if not any(isinstance(page, dict) and page.get("url") == url for page in pages):
            pages.append({"url": url, "match": "prefix"})
        if url not in [entry for entry in reuse if isinstance(entry, str)]:
            reuse.append(url)
    auth.update({"pages": pages, "reuse_urls": reuse})
    result["auth"] = auth
    return result


def assert_config_upgrade(before, after, start, end):
    """assert_config_upgrade 逐行保护全部配置，仅允许两个平台的精确合并和缺失51job的默认插入。"""
    old, new = keyed_rows(before, "config_key"), keyed_rows(after, "config_key")
    expected_keys = set(old) | {"platform.51job"}
    require(set(new) == expected_keys, "配置表出现未批准的增删")
    for key, row in old.items():
        if key not in SUPPORTED or not isinstance(row.get("config_value"), dict):
            require(new[key] == row, "不在批准范围内的配置行发生变化")
            continue
        expected = copy.deepcopy(row)
        expected["config_value"] = merged_value(row["config_value"], key)
        in_window(new[key].get("updated_at"), start, end)
        expected["updated_at"] = new[key]["updated_at"]
        require(new[key] == expected, "平台配置覆盖了原入口、开关、其他字段或旧行身份")
    if "platform.51job" not in old:
        row = new["platform.51job"]
        require(set(row) == CONFIG_COLUMNS and row.get("enabled") is True
                and row.get("description") == NEW_DESCRIPTION
                and row.get("config_value") == merged_value(NEW_PLATFORM, "platform.51job"),
                "前程新平台默认配置与批准内容不一致")
        try:
            uuid.UUID(row["id"])
        except (ValueError, TypeError, AttributeError):
            raise CutoverError("前程新平台记录编号无效") from None
        for key in ("created_at", "updated_at"):
            in_window(row[key], start, end)
    return True


def assert_upgrade(before, after, old_ledger, new_ledger, old_configs, new_configs, schema_before, schema_after, start, end):
    """assert_upgrade 核验全部业务行、完整旧账本与DDL不变，只允许固定配置变化及一条账本追加。"""
    require(LEDGER in before and CONFIGS in before and set(before) == set(after), "恢复库表集合或必要表不完整")
    assert_preserved({key: value for key, value in before.items() if key not in (LEDGER, CONFIGS)},
                     {key: value for key, value in after.items() if key not in (LEDGER, CONFIGS)})
    require(isinstance(schema_before, dict) and schema_before and schema_before == schema_after,
            "0106不应改变任何数据库结构、所有者、索引或其他对象")
    old, new = keyed_rows(old_ledger, "filename"), keyed_rows(new_ledger, "filename")
    require(MIGRATION not in old and set(new) == set(old) | {MIGRATION}, "本次并非只追加0106")
    require(all(new[name] == row for name, row in old.items()), "旧迁移内容或执行时间变化")
    require(set(new[MIGRATION]) == {"filename", "applied_at"}, "新增迁移账本结构异常")
    in_window(new[MIGRATION]["applied_at"], start, end)
    assert_config_upgrade(old_configs, new_configs, start, end)
    require(before[LEDGER]["rows"] == len(old_ledger) and after[LEDGER]["rows"] == len(new_ledger)
            and before[CONFIGS]["rows"] == len(old_configs) and after[CONFIGS]["rows"] == len(new_configs),
            "整表快照与逐行审阅数量不一致")
    return True


def assert_archives(candidate, rollback, before_names, approved, source):
    """assert_archives 绑定新旧镜像全部SQL与干净源码，阻止0104/0105或被修改的旧迁移混入。"""
    require(MIGRATION not in before_names and len(before_names) == len(set(before_names)), "0106已经执行或旧账本重复")
    require(set(rollback) == set(before_names) and set(candidate) == set(before_names) | {MIGRATION},
            "候选或回退迁移集合超出旧账本加0106")
    require(all(candidate[name] == raw for name, raw in rollback.items()), "旧迁移SQL字节发生变化")
    require(isinstance(source, bytes) and candidate[MIGRATION] == source
            and hashlib.sha256(source).hexdigest() == approved["sha256"], "0106镜像、源码或批准摘要不一致")


def platform_evidence(host, commit, release_id, candidate, rollback, before, after, reports, approved):
    """platform_evidence 仅用同一迁移后库上实际通过的两份配对报告签发固定迁移证据。"""
    approval(approved.get("sha256"))
    require(approved.get("filename") == MIGRATION and re.fullmatch(r"[0-9a-f]{64}", before or "")
            and before != after, "平台迁移批准或前后账本不完整")
    for report in reports.values():
        require(report.get("platform_migration") == approved and report.get("schema_verified") is True
                and report.get("post_startup_write_and_create_permissions") is False,
                "平台候选或回退报告缺少精确迁移和撤权验证")
    evidence = evidence_documents(host, commit, release_id, candidate, rollback, after, reports)
    evidence.update({"before_migration_fingerprint": before, "expected_new_migrations": 1,
                     "platform_migration": approved, "contract_scope": SCOPE,
                     "business_write_validation": "only_the_two_approved_platform_configuration_rows"})
    return evidence


class PlatformRehearsal(Rehearsal):
    """复用完整备份、内网恢复与回退管理，仅扩展固定0106配置数据迁移的最小权限和严格核验。"""

    def __init__(self, arguments, engine):
        """__init__ 必须显式批准0106摘要，不接受与文章新增表模式混用。"""
        super().__init__(arguments, engine)
        require(self.additive is None, "平台配置演练不能混用文章迁移例外")
        self.approved = approval(getattr(arguments, "platform_migration_sha256", None))

    def validate(self):
        """validate 在创建隔离对象前绑定生产身份、干净源码和固定SQL原始字节。"""
        source = super().validate()
        self.sql = (source / "cloud/backend/db/migrations" / MIGRATION).read_bytes()
        require(hashlib.sha256(self.sql).hexdigest() == self.approved["sha256"], "0106源码与显式批准摘要不一致")
        return source

    def configs(self):
        """configs 只在隔离库内读取完整配置行供内存比较，不输出或另存配置秘密正文。"""
        return [json.loads(line) for line in self.psql(
            "SELECT row_to_json(t)::text FROM public.system_configs t ORDER BY config_key;").decode().splitlines()]

    def restore(self):
        """restore 沿用完整生产备份恢复，随后保存真实旧账本、结构和配置基线并撤销CREATE。"""
        super().restore()
        self.before_filenames, self.before_fingerprint = list(self.filenames), self.fingerprint
        require(MIGRATION not in self.before_filenames, "0106已在生产账本，不能冒充新迁移")
        self.before_ledger, self.before_schema, self.before_configs = self.ledger_records(), self.database_schema(), self.configs()
        self.psql("REVOKE CREATE ON SCHEMA public FROM rehearsal_app;")
        inspector = self.own(self.prefix + "-migration-rollback", {
            "Image": self.rollback["backend"], "HostConfig": {"NetworkMode": self.prefix}})
        self.old_sql = packaged_migrations(command(["docker", "cp", inspector + ":/app/db/migrations", "-"], timeout=30))
        save_json(self.directory / "platform-baseline.json", {"approved": self.approved,
                  "ledger": self.before_ledger, "schema": self.before_schema,
                  "configuration_rows": len(self.before_configs)}, exclusive=True)

    def grant_startup(self, label):
        """grant_startup 仅候选启动窗口授配置所需列及账本filename插入，其他业务表仍只读。"""
        require(label in ("candidate", "rollback"), "平台演练角色不明确")
        if label == "candidate":
            self.window_start = self.psql("SELECT clock_timestamp()::text;").decode().strip()
        self.psql("GRANT CREATE ON SCHEMA public TO rehearsal_app;")
        if label == "candidate":
            self.psql("GRANT INSERT(config_key,config_value,description,enabled), UPDATE(config_value,updated_at) "
                      "ON public.system_configs TO rehearsal_app; "
                      "GRANT INSERT(filename) ON public.schema_migrations TO rehearsal_app;")

    def restrict(self):
        """restrict 在health之后及失败收尾均撤销全部启动权限，连列级写权限也必须实际清零。"""
        self.psql("REVOKE INSERT(config_key,config_value,description,enabled), UPDATE(config_value,updated_at) "
                  "ON public.system_configs FROM rehearsal_app; "
                  "REVOKE INSERT(filename) ON public.schema_migrations FROM rehearsal_app; "
                  "REVOKE CREATE ON SCHEMA public FROM rehearsal_app;")
        writable = self.psql("SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace "
                             "WHERE c.relkind IN ('r','p') AND n.nspname !~ '^pg_' AND n.nspname<>'information_schema' "
                             "AND (has_table_privilege('rehearsal_app',c.oid,'INSERT,UPDATE,DELETE,TRUNCATE') "
                             "OR has_any_column_privilege('rehearsal_app',c.oid,'INSERT,UPDATE,REFERENCES'));").decode().strip()
        create = self.psql("SELECT has_schema_privilege('rehearsal_app','public','CREATE');").decode().strip()
        require(writable == "0" and create == "f", "平台迁移启动窗口结束后仍有创建或列级写入权限")

    def verify_pair(self, label, after):
        """verify_pair 严格核验真实自动迁移结果，旧组合必须在同一个升级后库保持全部数据和结构。"""
        ledger, schema, configs = self.ledger_records(), self.database_schema(), self.configs()
        if label == "candidate":
            assert_upgrade(self.baseline, after, self.before_ledger, ledger, self.before_configs, configs,
                           self.before_schema, schema, self.window_start, self.window_end)
            self.migrated_baseline, self.migrated_ledger, self.migrated_schema = after, ledger, schema
            self.filenames = [row["filename"] for row in ledger]
            self.fingerprint = migration_fingerprint(self.filenames)
            save_json(self.directory / "platform-migration.json", {"approved": self.approved,
                      "before_ledger": self.before_ledger, "after_ledger": ledger,
                      "schema_verified": True, "configuration_rows_before": len(self.before_configs),
                      "configuration_rows_after": len(configs), "configuration_values_logged": False,
                      "automatic_run_migrations": True, "startup_column_write_permission_window": True,
                      "post_startup_write_and_create_permissions": False}, exclusive=True)
        else:
            assert_preserved(self.migrated_baseline, after)
            require(ledger == self.migrated_ledger and schema == self.migrated_schema,
                    "旧应用回退改变了迁移后的完整账本或数据库结构")

    def pair(self, label, images):
        """pair 沿用原配对探测，候选通过真实RunMigrations升级，撤权后才验证匿名路由并运行完整回退。"""
        backend = self.own(self.prefix + "-backend", {"Image": images["backend"],
            "Env": backend_environment(self.args.database, self.app_password, self.prefix),
            "HostConfig": {"NetworkMode": self.prefix, "Memory": 256 * 1024**2}})
        packaged = packaged_migrations(command(["docker", "cp", backend + ":/app/db/migrations", "-"], timeout=30))
        if label == "candidate":
            assert_archives(packaged, self.old_sql, self.before_filenames, self.approved, self.sql)
        else:
            require(packaged == self.old_sql, "回退镜像迁移字节发生变化")
        frontend = self.own(self.prefix + "-frontend", {"Image": images["frontend"],
            "Env": ["PORT=3000", "HOSTNAME=0.0.0.0", "CLOUD_API_BASE=http://" + self.prefix + "-backend:8084",
                    "NEXT_PUBLIC_ONBOARDING_PREVIEW_MODE="],
            "HostConfig": {"NetworkMode": self.prefix, "Memory": 384 * 1024**2}})
        checks = []
        try:
            self.grant_startup(label)
            self.engine.start(backend)
            self.engine.start(frontend)
            base = "http://" + self.prefix + "-backend:8084"
            checks.append({"path": "/health", "status": self.probe(frontend, base + "/health", 200, wait=True)})
            self.restrict()
            if label == "candidate":
                self.window_end = self.psql("SELECT clock_timestamp()::text;").decode().strip()
            for path in ("/api/auth/me", "/api/candidates", "/api/positions", "/api/resume-library/settings"):
                checks.append({"path": path, "status": self.probe(frontend, base + path, 401)})
            checks.append({"path": "/api/platforms/config/", "status": self.probe(frontend, base + "/api/platforms/config/", 200)})
            for path in ("/", "/login", "/download", "/admin/positions"):
                checks.append({"frontend_path": path, "status": self.probe(frontend,
                    "http://" + self.prefix + "-frontend:3000" + path, 200, wait=True)})
        finally:
            try:
                self.restrict()
            finally:
                self.stop(backend)
                self.stop(frontend)
        after = self.database_snapshot()
        self.verify_pair(label, after)
        report = {"host": self.host, "success": True, "legacy_data_preserved": True,
                  "contract_verified": True, "schema_verified": True,
                  "migration_fingerprint": self.fingerprint, "backend_image": images["backend"],
                  "frontend_image": images["frontend"], "contract_scope": SCOPE, "checks": checks,
                  "tables": after, "platform_migration": self.approved,
                  "post_startup_write_and_create_permissions": False}
        save_json(self.directory / (label + "-pair-summary.json"), report, exclusive=True)
        self.reports[label] = report
        for role, identifier in (("backend", backend), ("frontend", frontend)):
            name = self.prefix + "-" + role
            self.engine.request("POST", self.engine.prefix + "/containers/" + identifier + "/rename?name=" + urllib.parse.quote(name + "-" + label))
            del self.created[name]
            self.created[name + "-" + label] = identifier

    def finish(self):
        """finish 仅在两套完整应用均通过且生产未变时写本次专属证据和新兼容登记。"""
        for role in ROLES:
            actual = self.engine.inspect("goodhr-production-" + role + "-1")
            require(actual["Id"] == self.originals[role]["Id"] and actual["Image"] == self.rollback[role]
                    and actual["State"]["Running"], "演练期间生产应用身份发生变化")
        evidence = platform_evidence(self.host, self.args.source_commit, self.args.release_id, self.candidate,
                                    self.rollback, self.before_fingerprint, self.fingerprint, self.reports, self.approved)
        records = []
        for label, images in (("candidate", self.candidate), ("rollback", self.rollback)):
            path = self.directory / (label + "-pair-summary.json")
            records.append({"id": self.args.release_id + "-" + label,
                "verified_at": dt.datetime.now(dt.timezone.utc).isoformat(), "compatible": True,
                "migration_fingerprint": self.fingerprint, "backend_image": images["backend"],
                "frontend_image": images["frontend"], "evidence_path": str(path), "evidence_sha256": digest_file(path)})
        registry = {"schema_version": 1, "host": self.host, "current_migration_fingerprint": self.fingerprint,
                    "migration_probe": {"container": self.args.postgres_container, "database": self.args.database,
                                        "user": self.args.postgres_user}, "verified_releases": records}
        verified_records(registry, self.host)
        save_json(self.root / "retention-registry.json", registry, exclusive=True)
        save_json(self.root / "isolated-validation.json", evidence, exclusive=True)
        self.journal["success"] = True

    def run(self):
        """run 继续使用原完整隔离调度，只把返回范围标明为固定配置自动迁移验收。"""
        result = super().run()
        result["scope"] = SCOPE
        return result


def configure_parser(parser):
    """configure_parser 只为此固定迁移入口增加必须显式提供的文件摘要，不改变通用默认门禁。"""
    parser.description = __doc__
    parser.add_argument("--platform-migration-sha256", required=True)


if __name__ == "__main__":
    try:
        sys.exit(main(PlatformRehearsal, configure_parser))
    except (CutoverError, OSError, ValueError, subprocess.TimeoutExpired) as error:
        print(json.dumps({"success": False, "error": str(error)}, ensure_ascii=False), file=sys.stderr)
        sys.exit(1)
