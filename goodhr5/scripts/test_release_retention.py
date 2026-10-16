"""文件作用：离线验证Docker回收的兼容证据、祖先闭包、运行及其他项目保护、删除顺序和状态变化即停止，不调用真实Docker。"""

import copy
from contextlib import nullcontext
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location("release_retention", Path(__file__).with_name("release_retention.py"))
retention = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(retention)


def iid(value):
    """iid 生成固定且结构有效的离线镜像身份。"""
    return "sha256:" + format(value, "064x")


def image(value, role="backend", parent=0, tags=None):
    """image 创建不包含环境变量的安全镜像样本。"""
    return {"Id": iid(value), "Parent": iid(parent) if parent else "", "RepoTags": tags if tags is not None else ["goodhr-production-" + role + ":v" + str(value)], "RepoDigests": [], "Layers": [iid(100 + value)], "Release": "test-release" if role in retention.ROLES else "", "Role": role}


def container(value, name, image_value, running=False, role="backend"):
    """container 创建状态与项目标签均明确的离线容器。"""
    return {"Id": format(value, "064x"), "Name": "/" + name, "Image": iid(image_value), "Running": running, "Status": "running" if running else "exited", "StartedAt": "2026-10-15T01:00:00Z", "RestartCount": 0, "Release": "test-release" if role in retention.ROLES else "", "Role": role}


class FakeDocker:
    """模拟精确Docker删除后的状态，允许注入计划外变化和查询失败。"""

    def __init__(self, snapshot):
        """保存独立快照及实际执行动作列表。"""
        self.state = copy.deepcopy(snapshot)
        self.actions = []
        self.probe_calls = 0
        self.fail_remove = False

    def snapshot(self):
        """返回当前模拟全局状态。"""
        return copy.deepcopy(self.state)

    def migration_fingerprint(self, registry, snapshot):
        """记录只读迁移探针调用。"""
        self.probe_calls += 1
        return registry["current_migration_fingerprint"]

    def remove_container(self, key):
        """只移除明确容器，失败时模拟Docker保持现场。"""
        if self.fail_remove:
            raise retention.GuardError("模拟删除失败")
        self.actions.append(("container", key))
        self.state["containers"] = [row for row in self.state["containers"] if row["Id"] != key]

    def remove_image_ref(self, reference, approved_legacy_refs=()):
        """只移除明确标签或无标签镜像，与真实执行的预期状态保持一致。"""
        key = next(key for key, row in self.state["images"].items() if reference == key or reference in row["RepoTags"])
        self.actions.append(("image", reference))
        retention.expected_after_ref(self.state, key, reference)


class RetentionTests(unittest.TestCase):
    """验证持久回收计划的关键保护边界。"""

    def setUp(self):
        """建立当前双服务、一个证据回退及两个待删除旧版本。"""
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.evidence = self.root / "verified.json"
        self.evidence.write_bytes(b'{"compatibility":"verified by release operator"}')
        self.fingerprint = "f" * 64
        self.policy = {"schema_version": 1, "rollback_keep_per_service": 2, "minimum_compatible_rollback_per_service": 1, "delete_original_database_backups": False, "trusted_runtime_images": {"backend": [iid(1)], "frontend": [iid(2)]}}
        values = [image(1, role="base", tags=["alpine:3.22"]), image(2, role="base", tags=["node:22-alpine"]), image(3, parent=1), image(4, role="frontend", parent=2), image(5, parent=1), image(6, role="frontend", parent=2), image(7, parent=1), image(8, role="frontend", parent=2)]
        self.snapshot = {"host": "test-production", "containers": sorted([container(1, "goodhr-production-backend-1", 3, True), container(2, "goodhr-production-frontend-1", 4, True, "frontend"), container(3, "goodhr-production-backend-1-backup-obsolete", 7), container(4, "goodhr-production-frontend-1-backup-obsolete", 8, role="frontend"), container(5, "postgres", 1, True, role="database")], key=lambda row: row["Id"]), "images": {row["Id"]: row for row in values}}
        self.registry = {"schema_version": 1, "host": self.snapshot["host"], "current_migration_fingerprint": self.fingerprint, "migration_probe": {"container": "postgres", "database": "goodhr5", "user": "postgres"}, "verified_releases": [self.record("rollback", 5, 6, "2026-10-14T08:00:00+08:00")]}

    def record(self, name, backend, frontend, when):
        """生成包含实际证据文件摘要的完整验证配对。"""
        path = self.root / (name + "-pair.json")
        path.write_text(json.dumps({"success": True, "legacy_data_preserved": True, "contract_verified": True, "host": self.snapshot["host"], "backend_image": iid(backend), "frontend_image": iid(frontend), "migration_fingerprint": self.fingerprint}), encoding="utf-8")
        return {"id": name, "verified_at": when, "migration_fingerprint": self.fingerprint, "backend_image": iid(backend), "frontend_image": iid(frontend), "compatible": True, "evidence_path": str(path), "evidence_sha256": hashlib.sha256(path.read_bytes()).hexdigest()}

    def plan(self):
        """使用纯数据计划器生成待测白名单。"""
        return retention.make_plan(self.snapshot, self.registry, self.policy)

    def test_verified_fallback_and_current_services_are_kept(self):
        """当前服务和各端已验证历史回退均保留，只列明确停止的旧应用容器。"""
        plan = self.plan()
        self.assertEqual({row["Image"] for row in plan["containers_to_remove"]}, {iid(7), iid(8)})
        self.assertEqual({row["id"] for row in plan["images_to_remove"]}, {iid(7), iid(8)})
        self.assertTrue({iid(1), iid(2), iid(3), iid(4), iid(5), iid(6)} <= set(plan["protected_image_ids"]))

    def test_all_running_and_other_project_stopped_containers_protect_images(self):
        """即使使用本项目旧镜像，运行容器及其他项目停止容器也保护镜像及祖先。"""
        self.snapshot["containers"].extend([container(10, "unrelated-stopped-job", 7, role="foreign"), container(11, "goodhr-production-frontend-1-backup-running", 8, True, "frontend")])
        plan = self.plan()
        self.assertEqual(plan["images_to_remove"], [])
        self.assertNotIn(format(11, "064x"), {row["Id"] for row in plan["containers_to_remove"]})
        self.assertTrue({format(10, "064x"), format(11, "064x")} <= set(plan["protected_container_ids"]))

    def test_foreign_tag_keeps_image_and_full_ancestor_chain(self):
        """任意外部标签会保护完整父链，不能因同时存在业务标签而删除。"""
        self.snapshot["images"][iid(7)]["Parent"] = iid(8)
        self.snapshot["images"][iid(7)]["RepoTags"].append("other-project:archive")
        self.assertEqual(self.plan()["images_to_remove"], [])

    def test_unverified_or_stale_or_corrupt_evidence_blocks_plan(self):
        """只有当前版本、旧迁移指纹、虚假兼容或损坏证据均不能充当历史回退。"""
        variants = [dict(self.registry["verified_releases"][0], compatible=False), dict(self.registry["verified_releases"][0], migration_fingerprint="a" * 64), dict(self.registry["verified_releases"][0], evidence_sha256="0" * 64), self.record("current-only", 3, 4, "2026-10-15T08:00:00+08:00")]
        for row in variants:
            self.registry["verified_releases"] = [row]
            with self.subTest(row=row["id"]), self.assertRaises(retention.GuardError):
                self.plan()

    def test_missing_current_service_or_missing_ancestor_blocks_plan(self):
        """正式服务停止或父链不完整时不生成可删除清单。"""
        self.snapshot["containers"][0]["Running"] = False
        with self.assertRaises(retention.GuardError):
            self.plan()
        self.snapshot["containers"][0]["Running"] = True
        self.snapshot["images"][iid(7)]["Parent"] = iid(99)
        with self.assertRaises(retention.GuardError):
            self.plan()

    def test_each_service_keeps_at_most_two_real_compatible_pairings(self):
        """历史数量受限且保留真实验证配对，不把独立前后端记录拼接成新配对。"""
        self.registry["verified_releases"] = [self.record("frontend-only", 3, 6, "2026-10-15T08:00:00+08:00"), self.record("both-older", 5, 8, "2026-10-14T08:00:00+08:00"), self.record("too-old", 7, 8, "2026-10-13T08:00:00+08:00")]
        plan = self.plan()
        self.assertTrue(all(1 <= len(refs) <= 2 for refs in plan["historical_images"].values()))
        actual_pairs = {(row["backend_image"], row["frontend_image"]) for row in self.registry["verified_releases"]}
        self.assertTrue(all((row["backend_image"], row["frontend_image"]) in actual_pairs for row in plan["compatible_rollback_records"]))

    def test_children_are_removed_before_parents(self):
        """镜像操作按孩子到父亲排序，祖先不会在仍有孩子时先被请求删除。"""
        self.snapshot["images"][iid(7)]["Parent"] = iid(8)
        actions = self.plan()["images_to_remove"]
        self.assertEqual([row["id"] for row in actions], [iid(7), iid(8)])
        with self.assertRaises(retention.GuardError):
            retention.ancestor_closure({iid(7)}, {iid(7): dict(self.snapshot["images"][iid(7)], Parent=iid(7))})

    def test_container_name_and_label_whitelist_does_not_expand_to_database(self):
        """goodhr数据库或任意相似前缀不能因名称相近被清理。"""
        for name in ("goodhr-hr-db-old", "goodhr-other-backend-candidate-old", "goodhr-production-backend-1evil-backup-old", "postgres"):
            self.assertFalse(retention.target_container(container(99, name, 7, role="database"), self.snapshot["images"][iid(7)]))
        known = container(99, "exact-approved-candidate", 7)
        self.assertFalse(retention.target_container(known, self.snapshot["images"][iid(7)]))
        self.assertTrue(retention.target_container(known, self.snapshot["images"][iid(7)], ["exact-approved-candidate"]))

    def test_execute_has_exact_order_and_persistent_audit(self):
        """执行先容器后镜像，每步复核并保存可审查审计。"""
        plan = self.plan()
        reader = FakeDocker(self.snapshot)
        audit_path = self.root / "audit.json"
        result = retention.execute_plan(plan, self.registry, self.policy, reader, audit_path)
        self.assertTrue(result["success"])
        self.assertEqual([action[0] for action in reader.actions], ["container", "container", "image", "image"])
        self.assertEqual(reader.probe_calls, 2)
        self.assertTrue(json.loads(audit_path.read_text(encoding="utf-8"))["success"])

    def test_snapshot_change_and_delete_failure_stop_without_remaining_actions(self):
        """批准后状态改变时不执行，删除失败时不继续下一项并记录当前尝试。"""
        plan = self.plan()
        reader = FakeDocker(self.snapshot)
        reader.state["containers"][0]["RestartCount"] = 1
        with self.assertRaises(retention.GuardError):
            retention.execute_plan(plan, self.registry, self.policy, reader, self.root / "changed-audit.json")
        self.assertEqual(reader.actions, [])
        reader = FakeDocker(self.snapshot)
        reader.fail_remove = True
        audit_path = self.root / "failed-audit.json"
        with self.assertRaises(retention.GuardError):
            retention.execute_plan(plan, self.registry, self.policy, reader, audit_path)
        audit = json.loads(audit_path.read_text(encoding="utf-8"))
        self.assertEqual(reader.actions, [])
        self.assertEqual(audit["attempting"], {"container_id": plan["containers_to_remove"][0]["Id"]})

    def test_multi_tag_is_removed_individually_and_foreign_digest_is_protected(self):
        """批准标签逐一移除；外部仓库digest也作为持有引用保护。"""
        self.snapshot["images"][iid(7)]["RepoTags"].append("goodhr-production-backend:second")
        plan = self.plan()
        refs = next(row["refs"] for row in plan["images_to_remove"] if row["id"] == iid(7))
        self.assertEqual(len(refs), 2)
        reader = FakeDocker(self.snapshot)
        retention.execute_plan(plan, self.registry, self.policy, reader, self.root / "tags-audit.json")
        self.assertEqual(len([action for action in reader.actions if action[0] == "image"]), 3)
        self.snapshot["images"][iid(7)]["RepoDigests"].append("external/repository@" + iid(100))
        self.assertNotIn(iid(7), {row["id"] for row in self.plan()["images_to_remove"]})

    def test_live_migration_probe_uses_only_fixed_readonly_sql(self):
        """迁移探针核验运行postgres、简单标识和排序JSON摘要，查询变化立即拒绝。"""
        reader = retention.DockerRetention()
        rows = ["0001_initial.sql", "0103_latest.sql"]
        self.registry["current_migration_fingerprint"] = hashlib.sha256(json.dumps(sorted(rows), ensure_ascii=False, separators=(",", ":")).encode("utf-8")).hexdigest()
        with patch.object(reader, "query", return_value="\n".join(rows) + "\n") as query:
            self.assertEqual(reader.migration_fingerprint(self.registry, self.snapshot), self.registry["current_migration_fingerprint"])
            args = query.call_args.args[0]
            self.assertEqual(args[-1], "SELECT filename FROM schema_migrations ORDER BY filename;")
            self.assertNotIn("password", " ".join(args).lower())
        self.registry["migration_probe"]["database"] = "db;DROP TABLE"
        with self.assertRaises(retention.GuardError):
            reader.migration_fingerprint(self.registry, self.snapshot)

    def test_mutations_never_force_prune_or_delete_volumes(self):
        """删除命令不使用force、prune、卷或网络参数，镜像必须显式no-prune。"""
        reader = retention.DockerRetention()
        with patch.object(reader, "query", return_value="") as query:
            reader.remove_container(format(9, "064x"))
            self.assertEqual(query.call_args.args[0], ["container", "rm", format(9, "064x")])
            reader.remove_image_ref("goodhr-production-backend:obsolete")
            self.assertEqual(query.call_args.args[0], ["image", "rm", "--no-prune", "goodhr-production-backend:obsolete"])

    def test_change_during_apply_stops_after_first_precise_operation(self):
        """删除第一项后出现其他项目的新容器时立即停止，不继续按旧快照回收。"""
        plan = self.plan()
        reader = FakeDocker(self.snapshot)
        remove = reader.remove_container

        def changed_remove(key):
            """模拟另一操作者在批准删除之后新增外部容器。"""
            remove(key)
            reader.state["containers"].append(container(99, "other-project-new", 1, True, role="foreign"))

        reader.remove_container = changed_remove
        with self.assertRaises(retention.GuardError):
            retention.execute_plan(plan, self.registry, self.policy, reader, self.root / "mid-change.json")
        self.assertEqual(len(reader.actions), 1)

    def test_cli_fixed_hash_and_registry_recheck_before_mutation(self):
        """CLI计划有固定SHA；批准SHA错误或登记被修改时不触发任何Docker删除。"""
        policy_path, registry_path, plan_path = self.root / "policy.json", self.root / "registry.json", self.root / "plan.json"
        policy_path.write_text(json.dumps(self.policy), encoding="utf-8")
        registry_path.write_text(json.dumps(self.registry), encoding="utf-8")
        reader = FakeDocker(self.snapshot)
        output = io.StringIO()
        with patch.object(retention, "DockerRetention", return_value=reader), patch.object(retention.sys, "stdout", output):
            self.assertEqual(retention.main(["--policy", str(policy_path), "plan", "--registry", str(registry_path), "--out", str(plan_path)]), 0)
        result = json.loads(output.getvalue())
        self.assertEqual((result["containers"], result["images"], result["targets"]), (2, 2, 4))
        self.assertEqual(result["sha256"], hashlib.sha256(plan_path.read_bytes()).hexdigest())
        arguments = ["--policy", str(policy_path), "apply", "--plan", str(plan_path), "--sha256"]
        with patch.object(retention, "DockerRetention", return_value=reader), self.assertRaises(retention.GuardError):
            retention.main(arguments + ["0" * 64])
        registry_path.write_text(json.dumps(dict(self.registry, current_migration_fingerprint="b" * 64)), encoding="utf-8")
        with patch.object(retention, "DockerRetention", return_value=reader), patch.object(retention.socket, "gethostname", return_value=self.snapshot["host"]), patch.object(retention, "deployment_locks", return_value=nullcontext()), self.assertRaises(retention.GuardError):
            retention.main(arguments + [result["sha256"]])
        self.assertEqual(reader.actions, [])

    def test_builder_cache_keeps_latest_finished_leaf_and_ancestors(self):
        """每角色保留最新已完成缓存链，失败分支和旧完成缓存进入受限候选。"""
        for value, parent, stage, finished, created in ((20, 1, "backend-builder", "false", "2026-10-10T00:00:00Z"), (21, 20, "backend-builder", "true", "2026-10-10T01:00:00Z"), (22, 1, "backend-builder", "false", "2026-10-14T00:00:00Z"), (23, 22, "backend-builder", "true", "2026-10-14T01:00:00Z"), (24, 1, "backend-builder", "false", "2026-10-15T01:00:00Z"), (25, 2, "frontend-dependencies", "true", "2026-10-14T02:00:00Z")):
            row = image(value, role="build", parent=parent, tags=[])
            row.update(Project="goodhr", BuildStage=stage, BuildFinished=finished, Created=created)
            self.snapshot["images"][row["Id"]] = row
        plan = self.plan()
        self.assertEqual(set(plan["retained_build_cache_leaf_ids"]), {iid(23), iid(25)})
        removed = {row["id"] for row in plan["images_to_remove"]}
        self.assertTrue({iid(20), iid(21), iid(24)} <= removed)
        self.assertFalse({iid(22), iid(23), iid(25)} & removed)

    def test_builder_external_reference_and_unknown_project_are_protected(self):
        """构建标签不能越过外部引用保护，其他项目的缓存永不成为候选。"""
        for value, project, tags in ((20, "goodhr", ["other:cache"]), (21, "foreign", []), (22, "goodhr", [])):
            row = image(value, role="build", parent=1, tags=tags)
            row.update(Project=project, BuildStage="backend-builder", BuildFinished="false", Created="2026-10-15T01:00:00Z")
            self.snapshot["images"][row["Id"]] = row
        self.snapshot["containers"].append(container(30, "outside-using-cache", 22, role="foreign"))
        removed = {row["id"] for row in self.plan()["images_to_remove"]}
        self.assertFalse({iid(20), iid(21), iid(22)} & removed)

    def test_docker_created_fraction_precision_and_real_nanosecond_samples(self):
        """Docker三、六、七、九位小数及真实样本均保留完整纳秒，不依赖新版Python的宽松解析。"""
        for value, nanos in (("2026-10-16T14:09:58.851Z", 851000000), ("2026-10-16T14:09:58.851392Z", 851392000), ("2026-10-16T14:09:11.0638735Z", 63873500), ("2026-10-16T14:09:58.851392078Z", 851392078), ("2026-10-16T14:08:42.521710067Z", 521710067), ("2026-10-16T14:09:58Z", 0)):
            with self.subTest(value=value):
                whole, fraction = retention.parse_docker_created(value)
                self.assertEqual(fraction, nanos)
                self.assertEqual(whole.microsecond, 0)
                self.assertEqual(whole.utcoffset(), retention.dt.timedelta(0))
        self.assertEqual(retention.parse_docker_created("2026-10-16T14:09:11.0638735Z"), retention.parse_docker_created("2026-10-16T14:09:11.063873500Z"))

    def test_docker_created_offset_and_adjacent_second_sorting(self):
        """不同时区的同一时刻相等，跨秒比较优先整秒再比较纳秒。"""
        utc = retention.parse_docker_created("2026-10-16T14:09:58.851392078Z")
        self.assertEqual(utc, retention.parse_docker_created("2026-10-16T22:09:58.851392078+08:00"))
        self.assertEqual(utc, retention.parse_docker_created("2026-10-16T09:09:58.851392078-05:00"))
        self.assertLess(retention.parse_docker_created("2026-10-16T14:09:58.999999999Z"), retention.parse_docker_created("2026-10-16T14:09:59Z"))

    def test_builder_same_microsecond_keeps_actual_latest_nanosecond(self):
        """同微秒内用纳秒确定最新完成缓存，不能截断后靠镜像ID误选旧分支。"""
        for value, when in ((20, "2026-10-16T14:09:58.851392078Z"), (21, "2026-10-16T14:09:58.851392001Z")):
            row = image(value, role="build", parent=1, tags=[])
            row.update(Project="goodhr", BuildStage="backend-builder", BuildFinished="true", Created=when)
            self.snapshot["images"][row["Id"]] = row
        plan = self.plan()
        self.assertEqual(plan["retained_build_cache_leaf_ids"], [iid(20)])
        self.assertIn(iid(21), {row["id"] for row in plan["images_to_remove"]})
        self.assertNotIn(iid(20), {row["id"] for row in plan["images_to_remove"]})

    def test_docker_created_invalid_or_missing_timezone_stops_plan(self):
        """缺失时区、超九位小数、非法日期与时区仍停止，不猜测缓存删除顺序。"""
        for value in (None, "", "2026-10-16T14:09:58", "2026-10-16T14:09:58.1234567890Z", "2026-10-16T14:09:58.Z", "2026-02-30T14:09:58Z", "2026-10-16T14:09:58+24:00", "2026-10-16T14:09:58+01:99", "2026-10-16T14:09:58-00:60", "2026-10-16T14:09:60Z"):
            with self.subTest(value=value):
                with self.assertRaises(retention.GuardError):
                    retention.parse_docker_created(value)
                row = image(20, role="build", parent=1, tags=[])
                row.update(Project="goodhr", BuildStage="backend-builder", BuildFinished="true", Created=value)
                self.snapshot["images"][row["Id"]] = row
                with self.assertRaises(retention.GuardError):
                    self.plan()

    def test_evidence_must_bind_exact_pair_host_and_migration(self):
        """即使文件SHA正确，错误配对、主机、账本或失败契约也不能当兼容证明。"""
        row = self.registry["verified_releases"][0]
        path = Path(row["evidence_path"])
        original = json.loads(path.read_text(encoding="utf-8"))
        for field, value in (("backend_image", iid(7)), ("frontend_image", iid(8)), ("host", "other-host"), ("migration_fingerprint", "0" * 64), ("contract_verified", False)):
            path.write_text(json.dumps(dict(original, **{field: value})), encoding="utf-8")
            row["evidence_sha256"] = hashlib.sha256(path.read_bytes()).hexdigest()
            with self.subTest(field=field), self.assertRaises(retention.GuardError):
                self.plan()

    def test_activation_checks_planned_pair_and_live_before_ledger_without_lock(self):
        """激活检查按计划新镜像寻找完整回退配对，读取迁移前账本且不再次获取锁。"""
        evidence = {"success": True, "legacy_data_preserved": True, "rollback_verified": True, "host": self.snapshot["host"], "source_commit": "a" * 40, "backend_image": iid(3), "frontend_image": iid(4), "before_migration_fingerprint": "a" * 64, "after_migration_fingerprint": self.fingerprint}
        reader = FakeDocker(self.snapshot)
        with patch.object(retention, "deployment_locks", side_effect=AssertionError("不应重复锁定")):
            result = retention.check_activation(self.registry, evidence, reader, self.policy)
        self.assertTrue(result["success"])
        self.assertEqual(reader.probe_calls, 1)
        self.assertTrue(result["compatible_rollback_records"])
        for changed in (dict(evidence, rollback_verified=False), dict(evidence, after_migration_fingerprint="0" * 64), dict(evidence, backend_image=iid(99))):
            with self.assertRaises(retention.GuardError):
                retention.check_activation(self.registry, changed, reader, self.policy)
        self.registry["verified_releases"] = []
        with self.assertRaises(retention.GuardError):
            retention.check_activation(self.registry, evidence, reader, self.policy)

    def test_snapshot_label_templates_are_nil_safe_and_never_request_env(self):
        """无标签基础镜像返回null而非模板失败，所有index均被if保护且不请求Env。"""
        reader = retention.DockerRetention()
        pg = container(1, "postgres", 1, True, role="database")
        base = image(1, role="base", tags=["alpine:3.22"])
        base.update(Release=None, Role=None, ReleaseId=None, Project=None, BuildStage=None, BuildFinished=None, Created="2026-10-15T00:00:00Z")
        calls = []

        def query(args):
            """模拟nil标签的真实安全模板输出并记录参数。"""
            calls.append(args)
            if args[:2] == ["container", "ls"]:
                return pg["Id"] + "\n"
            if args[:2] == ["image", "ls"]:
                return base["Id"] + "\n"
            return json.dumps(pg if args[0] == "container" else base)

        with patch.object(reader, "query", side_effect=query):
            self.assertEqual(reader.snapshot()["images"][base["Id"]]["BuildStage"], None)
        templates = [args[3] for args in calls if "--format" in args]
        self.assertTrue(templates)
        for template in templates:
            self.assertEqual(template.count("index .Config.Labels"), template.count("{{if .Config.Labels}}"))
            self.assertNotIn(".Env", template)

    def test_simulated_plan_is_never_executable(self):
        """模拟批准预览即使结构与现状一致，也必须显式拒绝执行。"""
        plan = dict(self.plan(), simulation_only=True)
        reader = FakeDocker(self.snapshot)
        with self.assertRaisesRegex(retention.GuardError, "模拟"):
            retention.execute_plan(plan, self.registry, self.policy, reader, self.root / "simulation-audit.json")
        self.assertEqual(reader.actions, [])

    def legacy_approval(self, image_value=7):
        """创建缺旧角色标签的真实三元来源快照，不修改默认目标规则。"""
        row = next(value for value in self.snapshot["containers"] if value["Image"] == iid(image_value))
        row.update(Name="/goodhr-production-backend-before-20261005", Role=None, Release=None)
        self.snapshot["images"][row["Image"]].update(Role=None, Release=None, RepoTags=["goodhr-release-backup:goodhr-production-backend-1-20261006-223943"])
        source = self.root / "legacy-source-snapshot.json"
        source.write_text(json.dumps(self.snapshot), encoding="utf-8")
        return {"schema_version": 1, "host": self.snapshot["host"], "source_snapshot_path": str(source), "source_snapshot_sha256": hashlib.sha256(source.read_bytes()).hexdigest(), "containers": [{"id": row["Id"], "name": row["Name"], "image": row["Image"], "role": "backend"}]}

    def test_legacy_triple_requires_explicit_approval_and_freezes_exact_refs(self):
        """历史默认仍保护，固定来源三元批准后仅该容器及该镜像精确旧引用可加入。"""
        approval = self.legacy_approval()
        self.assertNotIn(iid(7), {row["id"] for row in self.plan()["images_to_remove"]})
        plan = retention.make_plan(self.snapshot, self.registry, self.policy, legacy_allowlist=approval)
        self.assertIn(iid(7), {row["id"] for row in plan["images_to_remove"]})
        self.assertEqual(plan["legacy_allowlist"], approval)
        self.assertEqual(plan["approved_legacy_image_refs"][iid(7)], self.snapshot["images"][iid(7)]["RepoTags"])
        reader = FakeDocker(self.snapshot)
        result = retention.execute_plan(plan, self.registry, self.policy, reader, self.root / "legacy-audit.json")
        self.assertTrue(result["success"])
        self.assertIn(("image", "goodhr-release-backup:goodhr-production-backend-1-20261006-223943"), reader.actions)

    def test_legacy_triple_rejects_identity_changes_running_foreign_and_duplicates(self):
        """三元改动、运行状态、其他服务标签、外部引用和重复ID均拒绝批准。"""
        approval = self.legacy_approval()
        variants = []
        wrong_id = copy.deepcopy(approval)
        wrong_id["containers"][0]["id"] = "9" * 64
        variants.append(wrong_id)
        wrong_name = copy.deepcopy(approval)
        wrong_name["containers"][0]["name"] = "/foreign-production-backend-before-old"
        variants.append(wrong_name)
        wrong_image = copy.deepcopy(approval)
        wrong_image["containers"][0]["image"] = iid(8)
        variants.append(wrong_image)
        duplicate = copy.deepcopy(approval)
        duplicate["containers"].append(copy.deepcopy(duplicate["containers"][0]))
        variants.append(duplicate)
        for variant in variants:
            with self.subTest(variant=variant["containers"][0]), self.assertRaises(retention.GuardError):
                retention.make_plan(self.snapshot, self.registry, self.policy, legacy_allowlist=variant)
        row = next(value for value in self.snapshot["containers"] if value["Image"] == iid(7))
        row["Running"] = True
        with self.assertRaises(retention.GuardError):
            retention.make_plan(self.snapshot, self.registry, self.policy, legacy_allowlist=approval)
        row["Running"], row["Role"] = False, "database"
        with self.assertRaises(retention.GuardError):
            retention.make_plan(self.snapshot, self.registry, self.policy, legacy_allowlist=approval)
        row["Role"] = None
        self.snapshot["images"][iid(7)]["RepoTags"].append("foreign-service:must-stay")
        with self.assertRaises(retention.GuardError):
            retention.make_plan(self.snapshot, self.registry, self.policy, legacy_allowlist=approval)

    def test_legacy_approval_cannot_remove_compatible_or_other_service_image(self):
        """兼容回退与其他项目容器所用镜像不接受历史三元清除。"""
        approval = self.legacy_approval()
        self.snapshot["containers"].append(container(33, "foreign-using-legacy", 7, role="foreign"))
        with self.assertRaises(retention.GuardError):
            retention.make_plan(self.snapshot, self.registry, self.policy, legacy_allowlist=approval)
        self.snapshot["containers"].pop()
        self.registry["verified_releases"] = [self.record("legacy-now-compatible", 7, 6, "2026-10-15T08:00:00+08:00")]
        with self.assertRaises(retention.GuardError):
            retention.make_plan(self.snapshot, self.registry, self.policy, legacy_allowlist=approval)

    def test_legacy_source_file_change_stops_before_any_apply_action(self):
        """来源快照批准后改变时，apply重新验证失败并保留全部Docker对象。"""
        approval = self.legacy_approval()
        plan = retention.make_plan(self.snapshot, self.registry, self.policy, legacy_allowlist=approval)
        Path(approval["source_snapshot_path"]).write_bytes(b"{}")
        reader = FakeDocker(self.snapshot)
        with self.assertRaises(retention.GuardError):
            retention.execute_plan(plan, self.registry, self.policy, reader, self.root / "legacy-source-changed.json")
        self.assertEqual(reader.actions, [])

    def test_snapshot_batches_inspect_without_reducing_identity_checks(self):
        """全量快照按64批量读取且始终二次枚举身份，规模增长不会导致逐对象CLI调用。"""
        reader = retention.DockerRetention()
        images = {iid(value): image(value, role="base", tags=[]) for value in range(1, 131)}
        containers = {format(value, "064x"): container(value, "foreign-" + str(value), 1, role="foreign") for value in range(1, 71)}
        calls = []

        def query(args):
            """返回批量白名单输出，同时统计每次实际CLI对象数量。"""
            calls.append(args)
            source = containers if args[0] == "container" else images
            if args[1] == "ls":
                return "\n".join(reversed(sorted(source)))
            return "\n".join(json.dumps(source[key]) for key in reversed(args[4:]))

        with patch.object(reader, "query", side_effect=query):
            snapshot = reader.snapshot()
        self.assertEqual((len(snapshot["containers"]), len(snapshot["images"])), (70, 130))
        batches = [args for args in calls if args[1] == "inspect"]
        self.assertEqual(len(batches), 5)
        self.assertTrue(all(len(args[4:]) <= 64 for args in batches))
        self.assertEqual(len([args for args in calls if args[1] == "ls"]), 4)

    def test_batch_inspect_rejects_missing_extra_and_duplicate_ids(self):
        """批量返回少项、多项或重复身份均停止，不能因批量化降低状态保护。"""
        reader = retention.DockerRetention()
        first, second, extra = image(1), image(2), image(3)
        for rows in ([first], [first, second, extra], [first, first]):
            with self.subTest(ids=[row["Id"] for row in rows]), patch.object(reader, "query", return_value="\n".join(json.dumps(row) for row in rows)), self.assertRaises(retention.GuardError):
                reader.inspect_batch("image", [iid(1), iid(2)], "safe-template")

    def test_linux_evidence_path_can_be_audited_offline_on_windows(self):
        """本机审阅Linux绝对路径只使用已核验注入源，原始server路径不会被改成虚假本机证明。"""
        approval = self.legacy_approval()
        source = json.loads(Path(approval["source_snapshot_path"]).read_text(encoding="utf-8"))
        approval["source_snapshot_path"] = "/opt/goodhr-release/verified-safe-snapshot.json"
        plan = retention.make_plan(self.snapshot, self.registry, self.policy, legacy_allowlist=approval, legacy_evidence_hash=lambda path: approval["source_snapshot_sha256"], legacy_evidence_reader=lambda path: source)
        self.assertEqual(plan["legacy_allowlist"]["source_snapshot_path"], approval["source_snapshot_path"])
        self.assertFalse(retention.absolute_path("relative/snapshot.json"))


if __name__ == "__main__":
    unittest.main()
