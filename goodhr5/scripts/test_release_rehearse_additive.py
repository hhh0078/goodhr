"""文件作用：离线验证文章单表迁移的固定批准、旧行与结构保护、旧镜像兼容证据及最小权限时序，不连接服务器。"""

import copy
import hashlib
import io
import tarfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

import release_rehearse as rehearsal
import release_rehearse_additive as additive
from release_cutover import CutoverError


class AdditiveMigrationSafetyTests(unittest.TestCase):
    """使用完整合成旧行和DDL验证单表迁移的所有例外均有明确边界。"""

    def setUp(self):
        """setUp 建立两个旧迁移和一张旧业务表的最小完整合成基线。"""
        self.sql = b"CREATE TABLE public_articles(id uuid);"
        self.options = additive.additive_options(additive.MIGRATION, hashlib.sha256(self.sql).hexdigest())
        self.old_ledger = [{"filename": name, "applied_at": "2026-10-01T00:00:00+00:00"}
                           for name in ["0001_schema.sql", "0096_separate_resume_folders.sql"]]
        self.new_ledger = self.old_ledger + [{"filename": additive.MIGRATION, "applied_at": "2026-10-17T00:00:00+00:00"}]
        self.before = {"public\tusers": {"rows": 2, "sha256": "a" * 64}, additive.SNAPSHOT_LEDGER: {"rows": 2, "sha256": "b" * 64}}
        self.after = copy.deepcopy(self.before)
        self.after[additive.SNAPSHOT_LEDGER] = {"rows": 3, "sha256": "c" * 64}
        self.after[additive.SNAPSHOT_NEW_TABLE] = {"rows": 0, "sha256": additive.EMPTY_SHA256}
        self.schema_before = {name: {} for name in ["tables", "views", "functions", "aggregates", "sequences", "types", "namespaces", "extensions", "other_relations"]}
        self.schema_before["tables"]["public.users"] = {"owner": "postgres", "columns": ["id", "email"], "comment": "旧用户"}
        self.schema_after = copy.deepcopy(self.schema_before)
        self.schema_after["tables"][additive.NEW_TABLE] = {
            "kind": "r", "persistence": "p", "partition_key": None, "row_security": False,
            "force_row_security": False, "columns": [{"name": name} for name in sorted(additive.ARTICLE_COLUMNS)],
            "indexes": {name: "approved definition" for name in additive.ARTICLE_INDEXES}, "triggers": {}, "policies": {},
        }

    def test_opt_in_requires_exact_file_and_sha_without_changing_default(self):
        """不传参数保持零迁移，仅单个精确文件和完整固定SHA可进入新增表模式。"""
        self.assertIsNone(additive.additive_options())
        self.assertEqual(self.options["new_table"], additive.NEW_TABLE)
        for name, digest in [(additive.MIGRATION, None), (None, "a" * 64), ("0098.sql", "a" * 64),
                             ("../" + additive.MIGRATION, "a" * 64), (additive.MIGRATION, "a" * 63)]:
            with self.subTest(name=name), self.assertRaises(CutoverError):
                additive.additive_options(name, digest)

    def test_packaged_candidate_and_rollback_have_distinct_exact_sets(self):
        """候选仅多0097且内容SHA一致，旧镜像必须仍是原集合而不能冒充新镜像。"""
        old = [row["filename"] for row in self.old_ledger]
        additive.assert_packaged_migrations("candidate", old + [additive.MIGRATION], old, self.options, self.sql)
        additive.assert_packaged_migrations("rollback", old, old, self.options)
        for label, names, raw in [("candidate", old, self.sql), ("candidate", old + [additive.MIGRATION, "extra.sql"], self.sql),
                                  ("candidate", old + [additive.MIGRATION], b"wrong bytes"),
                                  ("rollback", old + [additive.MIGRATION], None), ("rollback", old + old, None)]:
            with self.subTest(label=label), self.assertRaises(CutoverError):
                additive.assert_packaged_migrations(label, names, old, self.options, raw)

    def test_only_new_ledger_row_and_empty_article_table_are_allowed(self):
        """账本是单独严格审计的追加，不忽略旧行内容或整表计数。"""
        self.assertTrue(additive.assert_added_tables(self.before, self.after, self.old_ledger, self.new_ledger))
        changes = []
        changed = copy.deepcopy(self.after); changed["public\tusers"]["sha256"] = "d" * 64; changes.append(changed)
        changed = copy.deepcopy(self.after); del changed["public\tusers"]; changes.append(changed)
        changed = copy.deepcopy(self.after); changed["public\textra"] = {"rows": 0, "sha256": additive.EMPTY_SHA256}; changes.append(changed)
        changed = copy.deepcopy(self.after); changed[additive.SNAPSHOT_NEW_TABLE]["rows"] = 1; changes.append(changed)
        changed = copy.deepcopy(self.after); changed[additive.SNAPSHOT_NEW_TABLE]["sha256"] = "f" * 64; changes.append(changed)
        changed = copy.deepcopy(self.after); changed[additive.SNAPSHOT_LEDGER]["rows"] = 4; changes.append(changed)
        for changed in changes:
            with self.subTest(changed=changed), self.assertRaises(CutoverError):
                additive.assert_added_tables(self.before, changed, self.old_ledger, self.new_ledger)

    def test_real_tab_snapshot_rejects_existing_article_and_alias_keys(self):
        """使用真实TAB分隔键，已有文章表、点号别名和额外表均不能混入本轮新增表基线。"""
        original = copy.deepcopy(self.before)
        self.assertTrue(additive.assert_added_tables(self.before, self.after, self.old_ledger, self.new_ledger))
        self.assertEqual(self.before, original)
        before_existing = copy.deepcopy(self.before)
        before_existing[additive.SNAPSHOT_NEW_TABLE] = {"rows": 0, "sha256": additive.EMPTY_SHA256}
        with self.assertRaises(CutoverError):
            additive.assert_added_tables(before_existing, self.after, self.old_ledger, self.new_ledger)
        for name in [additive.NEW_TABLE, "public\tunapproved"]:
            after = copy.deepcopy(self.after)
            after[name] = {"rows": 0, "sha256": additive.EMPTY_SHA256}
            with self.subTest(name=name), self.assertRaises(CutoverError):
                additive.assert_added_tables(self.before, after, self.old_ledger, self.new_ledger)
        dotted = {name.replace("\t", "."): value for name, value in self.before.items()}
        with self.assertRaises(CutoverError):
            additive.assert_added_tables(dotted, self.after, self.old_ledger, self.new_ledger)

    def test_old_ledger_times_and_unexpected_rows_are_not_ignored(self):
        """旧applied_at、其他字段、重复记录或未批准文件变化都不能签发证据。"""
        changes = []
        rows = copy.deepcopy(self.new_ledger); rows[0]["applied_at"] = "changed"; changes.append(rows)
        rows = copy.deepcopy(self.new_ledger); rows[0]["extra"] = "changed"; changes.append(rows)
        changes.extend([self.new_ledger[1:], self.new_ledger + self.new_ledger[:1],
                        self.new_ledger + [{"filename": "0098.sql", "applied_at": "unexpected"}]])
        rows = copy.deepcopy(self.new_ledger); rows[-1]["applied_at"] = None; changes.append(rows)
        for changed in changes:
            with self.subTest(changed=changed), self.assertRaises(CutoverError):
                additive.assert_added_ledger(self.old_ledger, changed)

    def test_ddl_views_functions_sequences_types_and_extensions_cannot_change(self):
        """仅表名相符不够，旧DDL和所有盘点的其他对象也必须保持一致。"""
        self.assertTrue(additive.assert_added_schema(self.schema_before, self.schema_after))
        for category in self.schema_before:
            changed = copy.deepcopy(self.schema_after)
            if category == "tables":
                changed[category]["public.users"]["comment"] = "changed"
            else:
                changed[category]["unapproved object"] = "new"
            with self.subTest(category=category), self.assertRaises(CutoverError):
                additive.assert_added_schema(self.schema_before, changed)
        for field, value in [("indexes", {"extra": "new"}), ("triggers", {"write_old": "new"}),
                             ("policies", {"allow_all": "new"}), ("kind", "p"), ("row_security", True)]:
            changed = copy.deepcopy(self.schema_after); changed["tables"][additive.NEW_TABLE][field] = value
            with self.subTest(field=field), self.assertRaises(CutoverError):
                additive.assert_added_schema(self.schema_before, changed)

    def runner(self):
        """runner 创建不连接Docker的真实调度对象以检验权限和执行时序。"""
        args = SimpleNamespace(release_dir="synthetic-release", release_id="synthetic-release", source_commit="f" * 40,
                               backend_image="sha256:" + "a" * 64, frontend_image="sha256:" + "b" * 64,
                               database="synthetic_test", additive_migration=additive.MIGRATION,
                               additive_migration_sha256=self.options["sha256"])
        runner = rehearsal.Rehearsal(args, Mock())
        runner.engine.prefix = "/v1.47"
        runner.before_ledger, runner.before_schema = self.old_ledger, self.schema_before
        runner.before_filenames = [row["filename"] for row in self.old_ledger]
        runner.fingerprint = "e" * 64
        return runner

    def test_startup_permission_window_never_grants_business_dml(self):
        """CREATE只在启动窗口授予，候选只有账本filename列INSERT，不授旧业务DML。"""
        runner = self.runner(); runner.psql = Mock()
        runner.additive_startup_permissions("candidate")
        sql = [call.args[0] for call in runner.psql.call_args_list]
        self.assertEqual(sql, ["GRANT CREATE ON SCHEMA public TO rehearsal_app;",
                               "GRANT INSERT(filename) ON public.schema_migrations TO rehearsal_app;"])
        runner.psql.reset_mock(); runner.additive_startup_permissions("rollback")
        self.assertEqual([call.args[0] for call in runner.psql.call_args_list], [sql[0]])

    def test_health_boundary_revokes_ddl_and_ledger_writes(self):
        """health后归还新表所有权并只授SELECT，CREATE或写权限未实际撤销必须失败。"""
        runner = self.runner(); runner.ledger_records = Mock(return_value=self.new_ledger)
        runner.database_schema = Mock(return_value=self.schema_after)
        runner.psql = Mock(side_effect=[b"", b"", b"0\n", b"f\n"])
        runner.additive_readonly_permissions("candidate")
        commands = [call.args[0] for call in runner.psql.call_args_list]
        self.assertIn("REVOKE INSERT(filename)", commands[0]); self.assertIn("OWNER TO postgres", commands[0])
        self.assertIn("GRANT SELECT ON public.public_articles", commands[0])
        self.assertEqual(commands[1], "REVOKE CREATE ON SCHEMA public FROM rehearsal_app;")
        for writable, create in [(b"1", b"f"), (b"0", b"t")]:
            runner.psql = Mock(side_effect=[b"", b"", writable, create])
            with self.assertRaises(CutoverError):
                runner.additive_readonly_permissions("candidate")

    def test_pair_revokes_startup_permission_before_any_business_http(self):
        """真实pair控制流必须先health再撤权，然后才探测登录和业务，最终停止本轮两容器。"""
        runner = self.runner(); events = []
        archive = io.BytesIO()
        with tarfile.open(fileobj=archive, mode="w") as files:
            for name in runner.before_filenames + [additive.MIGRATION]:
                raw = self.sql if name == additive.MIGRATION else b"old SQL"
                member = tarfile.TarInfo("migrations/" + name); member.size = len(raw); files.addfile(member, io.BytesIO(raw))
        def own(name, payload):
            """own 仅在内存维护本轮对象命名，帮助真实pair覆盖最终改名分支。"""
            runner.created[name] = name; return name
        def probe(frontend, url, expected, wait=False):
            """probe 记录路径顺序，不访问外部服务。"""
            events.append("health" if url.endswith("/health") else "business"); return expected
        runner.own = own; runner.probe = probe
        runner.engine.start = Mock(side_effect=lambda identifier: events.append("start"))
        runner.additive_startup_permissions = Mock(side_effect=lambda label: events.append("grant"))
        runner.additive_readonly_permissions = Mock(side_effect=lambda label: events.append("revoke"))
        runner.stop = Mock(side_effect=lambda identifier: events.append("stop"))
        runner.database_snapshot = Mock(return_value=self.after); runner.verify_additive_pair = Mock()
        with patch.object(rehearsal, "command", return_value=archive.getvalue()), patch.object(rehearsal, "save_json"):
            runner.pair("candidate", runner.candidate)
        self.assertLess(events.index("grant"), events.index("start"))
        self.assertLess(events.index("health"), events.index("revoke"))
        self.assertLess(events.index("revoke"), events.index("business"))
        self.assertEqual(events.count("stop"), 2)

    def test_rollback_must_keep_full_migrated_tables_ledger_and_schema(self):
        """旧pair必须在迁移后同一库保护完整50表，不能仅证明旧48表可读。"""
        runner = self.runner(); runner.migrated_baseline = self.after; runner.migrated_ledger = self.new_ledger
        runner.migrated_schema = self.schema_after; runner.ledger_records = Mock(return_value=self.new_ledger)
        runner.database_schema = Mock(return_value=self.schema_after)
        self.assertTrue(runner.verify_additive_pair("rollback", copy.deepcopy(self.after)))
        runner.database_schema = Mock(return_value=self.schema_before)
        with self.assertRaises(CutoverError): runner.verify_additive_pair("rollback", self.after)
        runner.database_schema = Mock(return_value=self.schema_after); runner.ledger_records = Mock(return_value=self.old_ledger)
        with self.assertRaises(CutoverError): runner.verify_additive_pair("rollback", self.after)

    def test_success_evidence_binds_both_pairs_to_migrated_schema(self):
        """新增迁移证据必须区分前后账本，并由同一迁移后的两个真实组合报告共同支持。"""
        candidate = {"backend": "sha256:" + "a" * 64, "frontend": "sha256:" + "b" * 64}
        rollback = {"backend": "sha256:" + "c" * 64, "frontend": "sha256:" + "d" * 64}
        reports = {}
        for label, pair in [("candidate", candidate), ("rollback", rollback)]:
            reports[label] = {"host": "synthetic-host", "success": True, "legacy_data_preserved": True,
                              "contract_verified": True, "migration_fingerprint": "e" * 64,
                              "backend_image": pair["backend"], "frontend_image": pair["frontend"],
                              "additive_migration": self.options, "schema_verified": True}
        args = ["synthetic-host", "f" * 40, "synthetic-release", candidate, rollback, "e" * 64, reports]
        evidence = rehearsal.evidence_documents(*args, self.options, "1" * 64)
        self.assertEqual(evidence["expected_new_migrations"], 1)
        self.assertNotEqual(evidence["before_migration_fingerprint"], evidence["after_migration_fingerprint"])
        for field, value in [("schema_verified", False), ("additive_migration", None), ("migration_fingerprint", "1" * 64)]:
            changed = copy.deepcopy(reports); changed["rollback"][field] = value
            with self.subTest(field=field), self.assertRaises(CutoverError):
                rehearsal.evidence_documents(*args[:-1], changed, self.options, "1" * 64)
        with self.assertRaises(CutoverError): rehearsal.evidence_documents(*args, self.options, "e" * 64)


if __name__ == "__main__":
    unittest.main()
