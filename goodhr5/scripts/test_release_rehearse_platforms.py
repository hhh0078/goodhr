"""文件作用：离线验证固定0106平台迁移的精确JSON保护、权限窗口、镜像绑定及回退证据，不连接数据库或服务器。"""

import copy
import hashlib
import io
import tarfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

import release_rehearse_platforms as platforms
from release_cutover import CutoverError


class PlatformMigrationTests(unittest.TestCase):
    """以合成完整配置和账本验证唯一平台数据迁移，禁止把其他改动包装成本次验收。"""

    def setUp(self):
        """setUp 创建带自定义入口、关闭状态和旧选择器的完整旧行。"""
        self.start, self.now, self.end = ["2026-11-02T00:00:" + value + "+00:00" for value in ("00", "10", "20")]
        self.sql = b"approved fixed SQL"
        self.approved = platforms.approval(hashlib.sha256(self.sql).hexdigest())
        self.zhaopin = self.row("platform.zhaopin", {
            "id": "zhaopin", "open": False, "selector": {"old": "preserved"},
            "auth": {"entry_url": "https://rd6.zhaopin.com/custom", "match": "custom", "pages": [
                {"url": platforms.SUPPORTED["platform.zhaopin"][0], "custom": "rule"}]},
            "public": {"pages": [{"url": "custom login"}]},
        }, False)
        self.other = self.row("system.secret", {"value": "synthetic private field"})
        self.before_configs = [self.zhaopin, self.other]
        self.after_configs = copy.deepcopy(self.before_configs)
        self.after_configs[0]["config_value"] = platforms.merged_value(self.zhaopin["config_value"], "platform.zhaopin")
        self.after_configs[0]["updated_at"] = self.now
        new = self.row("platform.51job", platforms.merged_value(platforms.NEW_PLATFORM, "platform.51job"))
        new.update({"id": "00000000-0000-4000-8000-000000000002", "description": platforms.NEW_DESCRIPTION,
                    "created_at": self.now, "updated_at": self.now})
        self.after_configs.append(new)
        self.ledger = [{"filename": name, "applied_at": "2026-10-01T00:00:00+00:00"}
                       for name in ("0001_init.sql", "0103_position_screening_records.sql")]
        self.new_ledger = self.ledger + [{"filename": platforms.MIGRATION, "applied_at": self.now}]
        self.before = {"public\tusers": {"rows": 5, "sha256": "a" * 64},
                       platforms.LEDGER: {"rows": 2, "sha256": "b" * 64},
                       platforms.CONFIGS: {"rows": 2, "sha256": "c" * 64}}
        self.after = copy.deepcopy(self.before)
        self.after[platforms.LEDGER] = {"rows": 3, "sha256": "d" * 64}
        self.after[platforms.CONFIGS] = {"rows": 3, "sha256": "e" * 64}
        self.schema = {"tables": {"users": {"owner": "postgres"}}, "functions": {}}

    def row(self, key, value, enabled=True):
        """row 构造与真实配置表相同列集合的合成行。"""
        return {"id": "00000000-0000-4000-8000-000000000001", "config_key": key, "config_value": value,
                "description": "保留原说明", "enabled": enabled,
                "created_at": "2026-10-01T00:00:00+00:00", "updated_at": "2026-10-01T00:00:00+00:00"}

    def verify(self, **overrides):
        """verify 对实际审阅函数传入完整快照，允许单独制造越界变化。"""
        values = dict(before=self.before, after=self.after, old_ledger=self.ledger, new_ledger=self.new_ledger,
                      old_configs=self.before_configs, new_configs=self.after_configs,
                      schema_before=self.schema, schema_after=self.schema, start=self.start, end=self.end)
        values.update(overrides)
        return platforms.assert_upgrade(**values)

    def test_explicit_sha_and_exact_archive_reject_auto_reply_migrations(self):
        """只有完整显式SHA和旧集合加0106可通过，0104/0105、旧SQL变化和不同字节必须拒绝。"""
        for digest in (None, "a" * 63, "z" * 64):
            with self.subTest(digest=digest), self.assertRaises(CutoverError):
                platforms.approval(digest)
        old = {row["filename"]: b"old SQL" for row in self.ledger}
        new = dict(old, **{platforms.MIGRATION: self.sql})
        platforms.assert_archives(new, old, list(old), self.approved, self.sql)
        bad = dict(new, **{"0104_auto_reply_configuration.sql": b"not allowed"})
        changed_old = dict(new); changed_old[next(iter(old))] = b"changed"
        for packaged in (bad, changed_old, old, dict(new, **{platforms.MIGRATION: b"different"})):
            with self.subTest(packaged=list(packaged)), self.assertRaises(CutoverError):
                platforms.assert_archives(packaged, old, list(old), self.approved, self.sql)

    def test_exact_merge_preserves_existing_settings_and_is_idempotent(self):
        """默认入口、关闭开关、旧选择器和其他认证字段不能被平台新增覆盖。"""
        before = copy.deepcopy(self.zhaopin["config_value"])
        value = platforms.merged_value(before, "platform.zhaopin")
        self.assertEqual(before, self.zhaopin["config_value"])
        self.assertFalse(value["open"])
        self.assertEqual(value["auth"]["entry_url"], before["auth"]["entry_url"])
        self.assertEqual(value["auth"]["pages"][0], before["auth"]["pages"][0])
        self.assertEqual(platforms.merged_value(value, "platform.zhaopin"), value)
        self.assertTrue(self.verify())

    def test_old_duplicate_numeric_prefixes_are_distinct_filenames(self):
        """历史同编号前缀的不同文件都须保留，不能按编号去重或要求迁移连续。"""
        old = {name: b"old SQL" for name in ("0100_old.sql", "0100_other.sql", "0103_records.sql")}
        new = dict(old, **{platforms.MIGRATION: self.sql})
        platforms.assert_archives(new, old, list(old), self.approved, self.sql)
        with self.assertRaises(CutoverError):
            platforms.assert_archives(new, old, list(old) + ["0100_old.sql"], self.approved, self.sql)

    def test_existing_disabled_51job_or_nonobject_config_is_preserved(self):
        """已有前程的启用和开放开关不能强开，非对象配置按固定SQL原样保留。"""
        custom = self.row("platform.51job", {"open": False, "auth": {"entry_url": "custom", "pages": []}}, False)
        before = self.before_configs + [custom]
        after = copy.deepcopy(before)
        for row in after:
            if row["config_key"] in platforms.SUPPORTED:
                row["config_value"] = platforms.merged_value(row["config_value"], row["config_key"])
                row["updated_at"] = self.now
        self.assertTrue(platforms.assert_config_upgrade(before, after, self.start, self.end))
        for value in (None, [], "invalid old object"):
            before = [self.row("platform.51job", value), self.other]
            self.assertTrue(platforms.assert_config_upgrade(before, copy.deepcopy(before), self.start, self.end))

    def test_config_row_drift_and_extra_row_changes_are_rejected(self):
        """逐行比较全部配置，其他秘密配置、旧身份、默认入口和开关变化都不能被整表例外吞掉。"""
        changes = []
        for index, key, value in [(0, "enabled", True), (0, "id", "changed"), (0, "description", "changed"),
                                  (1, "config_value", {"value": "changed"}), (2, "description", "wrong")]:
            changed = copy.deepcopy(self.after_configs); changed[index][key] = value; changes.append(changed)
        changed = copy.deepcopy(self.after_configs); changed[0]["config_value"]["auth"]["entry_url"] = "wrong"; changes.append(changed)
        changes.extend([self.after_configs[1:], self.after_configs + [self.other]])
        for changed in changes:
            with self.subTest(changed=changed), self.assertRaises(CutoverError):
                self.verify(new_configs=changed)

    def test_business_rows_ddl_ledger_and_execution_time_are_all_protected(self):
        """只批准两配置和一条账本；任何业务行、DDL、旧执行时间或窗口外时间都失败。"""
        changed = copy.deepcopy(self.after); changed["public\tusers"]["sha256"] = "f" * 64
        with self.assertRaises(CutoverError): self.verify(after=changed)
        with self.assertRaises(CutoverError): self.verify(schema_after={"tables": {}, "functions": {}})
        with self.assertRaises(CutoverError): self.verify(schema_before={}, schema_after={})
        rows = copy.deepcopy(self.new_ledger); rows[0]["applied_at"] = self.now
        with self.assertRaises(CutoverError): self.verify(new_ledger=rows)
        for time in (self.start.replace("00+", "00"), "2026-10-01T00:00:00+00:00", "2026-11-02T00:01:00+00:00", None):
            rows = copy.deepcopy(self.new_ledger); rows[-1]["applied_at"] = time
            with self.subTest(time=time), self.assertRaises(CutoverError): self.verify(new_ledger=rows)
        rows = self.new_ledger + [{"filename": "0105.sql", "applied_at": self.now}]
        with self.assertRaises(CutoverError): self.verify(new_ledger=rows)

    def runner(self):
        """runner 创建只在内存运行的真实调度对象，不连接Docker或数据库。"""
        args = SimpleNamespace(release_dir="synthetic", release_id="synthetic", source_commit="f" * 40,
                               backend_image="sha256:" + "a" * 64, frontend_image="sha256:" + "b" * 64,
                               database="synthetic", platform_migration_sha256=self.approved["sha256"])
        runner = platforms.PlatformRehearsal(args, Mock())
        runner.engine.prefix = "/v1.47"
        runner.before_filenames = [row["filename"] for row in self.ledger]
        runner.old_sql = {name: b"old SQL" for name in runner.before_filenames}
        runner.sql, runner.fingerprint = self.sql, "e" * 64
        return runner

    def test_startup_permissions_are_column_limited_and_rollback_has_no_dml(self):
        """候选仅获得固定INSERT/UPDATE列，旧回退不获得任何配置或账本写权限。"""
        runner = self.runner(); runner.psql = Mock(side_effect=[self.start.encode(), b"", b""])
        runner.grant_startup("candidate")
        statements = [call.args[0] for call in runner.psql.call_args_list]
        self.assertIn("INSERT(config_key,config_value,description,enabled)", statements[2])
        self.assertIn("UPDATE(config_value,updated_at)", statements[2])
        self.assertNotIn("ALL TABLES", statements[2])
        runner.psql = Mock(); runner.grant_startup("rollback")
        self.assertEqual([call.args[0] for call in runner.psql.call_args_list], [statements[1]])

    def test_restriction_checks_column_writes_not_just_table_writes(self):
        """撤权后连残余列级写入也必须拒绝，而不只检查整表权限。"""
        runner = self.runner(); runner.psql = Mock(side_effect=[b"", b"0", b"f"])
        runner.restrict()
        sql = [call.args[0] for call in runner.psql.call_args_list]
        self.assertIn("REVOKE INSERT(config_key", sql[0])
        self.assertIn("has_any_column_privilege", sql[1])
        for writes, create in [(b"1", b"f"), (b"0", b"t")]:
            runner.psql = Mock(side_effect=[b"", writes, create])
            with self.assertRaises(CutoverError): runner.restrict()

    def archive(self, runner):
        """archive 在内存构造与固定候选相符的迁移文件包。"""
        result = io.BytesIO()
        with tarfile.open(fileobj=result, mode="w") as files:
            for name, raw in dict(runner.old_sql, **{platforms.MIGRATION: self.sql}).items():
                member = tarfile.TarInfo("migrations/" + name); member.size = len(raw)
                files.addfile(member, io.BytesIO(raw))
        return result.getvalue()

    def prepare_pair(self, runner, events, fail=False):
        """prepare_pair 用真实pair控制流记录权限、health和停止次序，不接触外部服务。"""
        def own(name, payload):
            """own 仅在内存保存对象名字，覆盖真实改名路径。"""
            runner.created[name] = name; return name
        def probe(frontend, url, expected, wait=False):
            """probe 记录health边界，必要时模拟启动失败。"""
            if url.endswith("/health"):
                events.append("health")
                if fail: raise CutoverError("synthetic health failure")
            else: events.append("business")
            return expected
        runner.own, runner.probe = own, probe
        runner.grant_startup = Mock(side_effect=lambda label: events.append("grant"))
        runner.restrict = Mock(side_effect=lambda: events.append("restrict"))
        runner.engine.start = Mock(side_effect=lambda identifier: events.append("start"))
        runner.stop = Mock(side_effect=lambda identifier: events.append("stop"))
        runner.psql = Mock(return_value=self.end.encode())
        runner.database_snapshot = Mock(return_value=self.after)
        runner.verify_pair = Mock()

    def test_pair_restricts_before_business_and_also_on_health_failure(self):
        """health之后先撤权再做业务探测；health失败仍撤权并停止两容器，不能签发报告。"""
        for fail in (False, True):
            runner = self.runner(); events = []; self.prepare_pair(runner, events, fail)
            with patch.object(platforms, "command", return_value=self.archive(runner)), patch.object(platforms, "save_json") as save:
                if fail:
                    with self.assertRaises(CutoverError): runner.pair("candidate", runner.candidate)
                    save.assert_not_called()
                else:
                    runner.pair("candidate", runner.candidate)
                    self.assertLess(events.index("health"), events.index("restrict"))
                    self.assertLess(events.index("restrict"), events.index("business"))
            self.assertEqual(events.count("stop"), 2)
            self.assertLess(events.index("restrict"), events.index("stop"))

    def test_rollback_requires_complete_migrated_state(self):
        """回退必须保持迁移后全部数据、完整账本和结构，不能只看旧业务表。"""
        runner = self.runner(); runner.migrated_baseline = self.after
        runner.migrated_ledger, runner.migrated_schema = self.new_ledger, self.schema
        runner.ledger_records = Mock(return_value=self.new_ledger)
        runner.database_schema = Mock(return_value=self.schema); runner.configs = Mock(return_value=self.after_configs)
        runner.verify_pair("rollback", self.after)
        changed = copy.deepcopy(self.after); changed[platforms.CONFIGS]["sha256"] = "f" * 64
        with self.assertRaises(CutoverError): runner.verify_pair("rollback", changed)
        runner.ledger_records = Mock(return_value=self.ledger)
        with self.assertRaises(CutoverError): runner.verify_pair("rollback", self.after)

    def test_evidence_requires_both_real_reports_and_correct_approval(self):
        """只有绑定同一迁移后指纹的两份完整报告才能把默认零迁移证据改为固定0106。"""
        candidate = {"backend": "sha256:" + "a" * 64, "frontend": "sha256:" + "b" * 64}
        rollback = {"backend": "sha256:" + "c" * 64, "frontend": "sha256:" + "d" * 64}
        reports = {label: {"host": "synthetic", "success": True, "legacy_data_preserved": True,
                   "contract_verified": True, "schema_verified": True, "migration_fingerprint": "e" * 64,
                   "backend_image": pair["backend"], "frontend_image": pair["frontend"],
                   "platform_migration": self.approved, "post_startup_write_and_create_permissions": False}
                   for label, pair in [("candidate", candidate), ("rollback", rollback)]}
        args = ["synthetic", "f" * 40, "synthetic", candidate, rollback, "1" * 64, "e" * 64]
        result = platforms.platform_evidence(*args, reports, self.approved)
        self.assertEqual(result["expected_new_migrations"], 1)
        self.assertEqual(result["before_migration_fingerprint"], "1" * 64)
        for field, value in [("schema_verified", False), ("platform_migration", None),
                             ("post_startup_write_and_create_permissions", True), ("migration_fingerprint", "1" * 64)]:
            changed = copy.deepcopy(reports); changed["rollback"][field] = value
            with self.subTest(field=field), self.assertRaises(CutoverError):
                platforms.platform_evidence(*args, changed, self.approved)


if __name__ == "__main__":
    unittest.main()
