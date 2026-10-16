"""文件作用：离线验证隔离演练的身份绑定、零迁移账本、完整数据保持和生产对象保护；不调用Docker或数据库。"""

import copy
import hashlib
import json
from pathlib import Path
from types import SimpleNamespace
import unittest

import release_rehearse as rehearsal
from release_cutover import CutoverError


class RehearsalSafetyTests(unittest.TestCase):
    """覆盖演练成功证据与越界操作的真实边界，不连接服务器。"""

    def setUp(self):
        """setUp 为每个用例建立完整且独立的合成镜像和实际报告合同。"""
        self.candidate = {"backend": "sha256:" + "a" * 64, "frontend": "sha256:" + "b" * 64}
        self.rollback = {"backend": "sha256:" + "c" * 64, "frontend": "sha256:" + "d" * 64}
        self.fingerprint = "e" * 64
        self.reports = {}
        for label, pair in (("candidate", self.candidate), ("rollback", self.rollback)):
            self.reports[label] = {"host": "synthetic-host", "success": True, "legacy_data_preserved": True, "contract_verified": True,
                                   "migration_fingerprint": self.fingerprint, "backend_image": pair["backend"], "frontend_image": pair["frontend"]}

    def document(self):
        """document 使用固定合成输入调用证据生成，不访问文件和运行环境。"""
        return rehearsal.evidence_documents("synthetic-host", "f" * 40, "synthetic-release", self.candidate, self.rollback, self.fingerprint, self.reports)

    def test_migration_fingerprint_matches_production_algorithm(self):
        """迁移指纹必须和正式登记的排序、JSON编码方式完全相同。"""
        names = ["0002_中文.sql", "0001_schema.sql"]
        expected = hashlib.sha256(json.dumps(sorted(names), ensure_ascii=False, separators=(",", ":")).encode("utf-8")).hexdigest()
        self.assertEqual(rehearsal.migration_fingerprint(names), expected)
        for value in ([], ["a.sql", "a.sql"], [None]):
            with self.assertRaises(CutoverError):
                rehearsal.migration_fingerprint(value)

    def test_no_table_is_ignored_when_rows_or_hash_change(self):
        """退款、已读消息和配置均与候选人一样严格核验，不能以后台任务为由跳过。"""
        baseline = {name: {"rows": 1, "sha256": "a" * 64} for name in ("public.candidates", "public.payment_refunds", "public.messages", "public.system_configs")}
        self.assertTrue(rehearsal.assert_preserved(baseline, copy.deepcopy(baseline)))
        for name in baseline:
            changed = copy.deepcopy(baseline)
            changed[name]["sha256"] = "b" * 64
            with self.assertRaisesRegex(CutoverError, name):
                rehearsal.assert_preserved(baseline, changed)
        with self.assertRaises(CutoverError):
            rehearsal.assert_preserved(baseline, {"public.candidates": baseline["public.candidates"]})

    def test_dsn_formats_bind_exact_database_without_leaking_secrets(self):
        """URL及关键字连接串只能确定同一个库，冲突或无库名时拒绝且错误不含口令。"""
        for dsn in ("postgres://user:sensitive@host:5432/goodhr5?sslmode=disable", "host=localhost password='sensitive with spaces' dbname=goodhr5"):
            self.assertEqual(rehearsal.dsn_database(["GOODHR_PG_DSN=" + dsn]), "goodhr5")
        with self.assertRaises(CutoverError) as failure:
            rehearsal.dsn_database(["GOODHR_PG_DSN=postgres://user:sensitive@host/a", "GOODHR_PG_DSN=postgres://user:sensitive@host/b"])
        self.assertNotIn("sensitive", str(failure.exception))

    def test_isolated_environment_has_only_local_dependencies(self):
        """隔离配置固定内部数据库和Redis并关闭邮件，不继承生产环境或凭据。"""
        rows = rehearsal.backend_environment("goodhr5", "synthetic-password", "goodhr-rehearsal-test")
        values = dict(row.split("=", 1) for row in rows)
        self.assertIn("@goodhr-rehearsal-test-pg:5432/goodhr5?", values["GOODHR_PG_DSN"])
        self.assertEqual(values["GOODHR_REDIS_ADDR"], "goodhr-rehearsal-test-redis:6379")
        self.assertEqual(values["GOODHR_DISABLE_EMAIL_SCHEDULER"], "true")
        self.assertEqual(values["GOODHR_SMTP_PASSWORD"], "")
        self.assertNotIn("GOODHR_WECHAT_PRIVATE_KEY", values)
        with self.assertRaises(CutoverError):
            rehearsal.backend_environment("db;DROP", "test", "goodhr-rehearsal-test")

    def test_identifier_quoting_cannot_escape_sql_identifier(self):
        """实际数据库标识符中的引号被双写，控制字符明确拒绝。"""
        self.assertEqual(rehearsal.quote_identifier('a";DROP TABLE users;--'), '"a"";DROP TABLE users;--"')
        with self.assertRaises(CutoverError):
            rehearsal.quote_identifier("table\nother")

    def test_success_documents_report_limited_contract_scope(self):
        """成功证据绑定完整组合，同时明确只读验收不能冒充业务写入验收。"""
        result = self.document()
        self.assertTrue(result["success"])
        self.assertTrue(result["rollback_verified"])
        self.assertEqual(result["before_migration_fingerprint"], result["after_migration_fingerprint"])
        self.assertEqual(result["expected_new_migrations"], 0)
        self.assertEqual(result["contract_scope"], "read_only_startup_and_anonymous_routes")
        self.assertIn("requires_separate", result["business_write_validation"])

    def test_incomplete_or_mismatched_pair_cannot_issue_evidence(self):
        """任一组合失败、换镜像、换主机或迁移指纹变化都不能签发发布成功证据。"""
        original = copy.deepcopy(self.reports)
        for field, value in (("success", False), ("legacy_data_preserved", False), ("contract_verified", False), ("host", "other-host"), ("frontend_image", self.candidate["frontend"]), ("migration_fingerprint", "0" * 64)):
            self.reports = copy.deepcopy(original)
            self.reports["rollback"][field] = value
            with self.assertRaises(CutoverError):
                self.document()
        self.reports = {"candidate": original["candidate"]}
        with self.assertRaises(CutoverError):
            self.document()

    def test_production_objects_and_host_network_cannot_be_owned(self):
        """不属于本轮的容器以及宿主网络在调用Docker前即拒绝。"""
        arguments = SimpleNamespace(release_dir=str(Path("synthetic-release")), release_id="synthetic-release", source_commit="f" * 40, backend_image=self.candidate["backend"], frontend_image=self.candidate["frontend"])
        runner = rehearsal.Rehearsal(arguments, None)
        with self.assertRaises(CutoverError):
            runner.stop("a" * 64)
        with self.assertRaises(CutoverError):
            runner.own("goodhr-production-backend-1", {"HostConfig": {"NetworkMode": runner.prefix}})
        with self.assertRaises(CutoverError):
            runner.own(runner.prefix + "-backend", {"HostConfig": {"NetworkMode": "host"}})
        with self.assertRaises(CutoverError):
            runner.own(runner.prefix + "-backend", {"HostConfig": {"NetworkMode": runner.prefix, "Binds": ["/production:/data"]}})

    def test_cleanup_manifest_rejects_running_and_unowned_objects(self):
        """收尾计划只含本轮停止对象，不能让生产finalize顺带清理其他容器或数据卷。"""
        network = {"Id": "a" * 64, "Name": "goodhr-rehearsal-test", "Internal": True, "Labels": {"goodhr.rehearsal.id": "synthetic-release"}}
        container = {"Id": "b" * 64, "Name": "/goodhr-rehearsal-test-pg", "Image": self.rollback["backend"], "State": {"Running": False}, "Config": {"Labels": {"goodhr.rehearsal.id": "synthetic-release"}}}
        plan = rehearsal.cleanup_manifest("synthetic-host", "synthetic-release", network["Name"], network, [container], "/synthetic/rehearsal")
        self.assertFalse(plan["delete_files"])
        self.assertFalse(plan["delete_volumes"])
        self.assertEqual(plan["containers"][0]["id"], container["Id"])
        for change in ("running", "name", "label"):
            invalid = copy.deepcopy(container)
            if change == "running":
                invalid["State"]["Running"] = True
            elif change == "name":
                invalid["Name"] = "/goodhr-production-backend-1"
            else:
                invalid["Config"]["Labels"]["goodhr.rehearsal.id"] = "other-release"
            with self.assertRaises(CutoverError):
                rehearsal.cleanup_manifest("synthetic-host", "synthetic-release", network["Name"], network, [invalid], "/synthetic/rehearsal")


if __name__ == "__main__":
    unittest.main()
