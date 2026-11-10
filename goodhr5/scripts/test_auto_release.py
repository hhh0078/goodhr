"""文件作用：回归自动部署的真实版本判断、失败重试、迁移门禁及同HEAD仍执行发布检查的故障。"""

import copy
import hashlib
import json
import os
from pathlib import Path
import shlex
import shutil
import socket
import signal
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from auto_release import AutomaticRelease, cleanup_rehearsal, cleanup_restore_cache, current_release, pending_migrations
from release_cutover import CutoverError
from release_rehearse_auto import APPROVED, check_archives, check_ledger, check_library


ROOT = Path(__file__).resolve().parents[1]
COMMIT = "a" * 40


def live_pair(commit):
    """live_pair 构造两个运行中的独立正式角色，供版本判断与失败保留测试使用。"""
    return {role: {"Id": role, "Image": "sha256:" + "1" * 64, "State": {"Running": True},
                   "Config": {"Labels": {"goodhr.source_commit": commit, "goodhr.release.id": "fixture",
                                         "goodhr.release_kind": role}}} for role in ("backend", "frontend")}


class Engine:
    """提供只读的正式容器快照，任何测试都不会连接本机或远端Docker。"""

    def __init__(self):
        """__init__ 保存独立副本，便于确认失败没有偷偷更换正式版本。"""
        self.values = live_pair("b" * 40)

    def inspect(self, name):
        """inspect 仅按正式角色名称返回可比较副本。"""
        return copy.deepcopy(self.values["backend" if "backend" in name else "frontend"])


class AutomaticReleaseTests(unittest.TestCase):
    """覆盖线上版本、重试和恢复行为，不以Git本地HEAD作为已发布依据。"""

    def test_both_roles_must_run_the_target_commit(self):
        """一个角色仍是旧版或停止时，不能误报发布完成。"""
        pair = live_pair(COMMIT)
        self.assertTrue(current_release(pair, COMMIT))
        pair["backend"]["Config"]["Labels"]["goodhr.source_commit"] = "b" * 40
        self.assertFalse(current_release(pair, COMMIT))
        pair = live_pair(COMMIT)
        pair["frontend"]["State"]["Running"] = False
        self.assertFalse(current_release(pair, COMMIT))
        pair = live_pair(COMMIT)
        pair["frontend"]["Config"]["Labels"]["goodhr.release.id"] = "another-pair"
        self.assertFalse(current_release(pair, COMMIT))

    def test_same_head_retries_failed_deployment_after_backoff(self):
        """发布失败后同一个提交仍会重试；等待期间不会每分钟重复构建，旧服务不受改动。"""
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory).resolve() / "goodhr5"
            source.mkdir()
            engine = Engine()
            before = copy.deepcopy(engine.values)
            job = AutomaticRelease(source, engine, Path(directory).resolve() / "state")
            with patch("auto_release.command", side_effect=lambda args: b"" if "status" in args else COMMIT.encode()), \
                    patch.object(job, "check_target"), patch.object(job, "execute", side_effect=CutoverError("构建测试失败")) as execute:
                with patch("auto_release.time.time", return_value=100), self.assertRaises(CutoverError):
                    job.run()
                self.assertEqual(engine.values, before)
                self.assertEqual(json.loads(job.state_path.read_text())["retry_after"], 400)
                with patch("auto_release.time.time", return_value=101):
                    job.run()
                self.assertEqual(execute.call_count, 1)
                with patch("auto_release.time.time", return_value=401), self.assertRaises(CutoverError):
                    job.run()
                self.assertEqual(execute.call_count, 2)

    def test_live_target_does_not_rebuild(self):
        """实际前后端均已更新时，重复定时检查不触发构建或切换。"""
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory).resolve() / "goodhr5"
            source.mkdir()
            engine = Engine()
            engine.values = live_pair(COMMIT)
            job = AutomaticRelease(source, engine, Path(directory).resolve() / "state")
            with patch("auto_release.command", side_effect=lambda args: b"" if "status" in args else COMMIT.encode()), \
                    patch.object(job, "check_target"), patch.object(job, "check_health"), patch.object(job, "execute") as execute:
                job.run()
                job.run()
                execute.assert_not_called()
            self.assertEqual(json.loads(job.state_path.read_text())["deployed_commit"], COMMIT)

    def test_failed_rehearsal_never_activates(self):
        """候选构建完成但隔离验收失败时，仍精确收尾本轮对象，绝不调用生产激活。"""
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory).resolve()
            source = base / "goodhr5"
            source.mkdir()
            job = AutomaticRelease(source, Engine(), base / "state")
            job.state_root.mkdir()
            preflight = ("fixture", {"Id": "p" * 64}, "postgres", {"Image": "sha256:" + "3" * 64}, [])
            with patch.object(job, "preflight", return_value=preflight), \
                    patch.object(job, "invoke", side_effect=[None, CutoverError("隔离验收未通过")]) as invoke, \
                    patch("auto_release.command", return_value=("sha256:" + "2" * 64).encode()), \
                    patch("auto_release.cleanup_rehearsal") as cleanup:
                with self.assertRaises(CutoverError):
                    job.execute(COMMIT, job.live())
                self.assertEqual(invoke.call_count, 2)
                self.assertFalse(any("--activate-only" in call.args[0] for call in invoke.call_args_list))
                cleanup.assert_called_once_with(job.engine, job.release)

    def test_cancellation_stops_the_owned_subprocess(self):
        """取消编排器时先通知本轮命令退出并等其收尾，不能让构建脱离定时任务继续跑。"""
        from unittest.mock import Mock
        process = Mock()
        process.wait.side_effect = [CutoverError("取消本轮"), -15]
        process.poll.return_value = None
        job = AutomaticRelease(Path("/fixture/goodhr5"), Engine())
        with patch("auto_release.subprocess.Popen", return_value=process), self.assertRaises(CutoverError):
            job.invoke(["bash", "scripts/deploy-build.sh", "--build-only"], {})
        process.send_signal.assert_called_once_with(signal.SIGTERM)
        self.assertEqual(process.wait.call_args_list[-1].kwargs, {"timeout": 330})

    def test_activation_uses_same_tag_only_after_rehearsal_and_cleanup(self):
        """完整编排实际验证阶段顺序、同标签及候选身份，不靠一次成功返回绕过验收。"""
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory).resolve()
            source = base / "goodhr5"
            source.mkdir()
            job = AutomaticRelease(source, Engine(), base / "state")
            job.state_root.mkdir()
            preflight = ("fixture", {"Id": "p" * 64}, "postgres", {"Image": "sha256:" + "3" * 64}, [])
            stages, tags = [], []

            def invoke(arguments, environment):
                """invoke 仅模拟明确阶段结果，验收证据必须先落盘，清理完成后才允许激活。"""
                tags.append(environment["GOODHR_RELEASE_IMAGE_TAG"])
                if "--build-only" in arguments:
                    stages.append("build")
                    job.release.mkdir(parents=True)
                elif "scripts/release_rehearse_auto.py" in arguments:
                    stages.append("rehearse")
                    (job.release / "isolated-validation.json").write_text(json.dumps({
                        "success": True, "source_commit": COMMIT,
                        "backend_image": "sha256:" + "2" * 64, "frontend_image": "sha256:" + "2" * 64}))
                else:
                    self.assertTrue(cleanup.called)
                    self.assertEqual(arguments[-1], "--activate-only")
                    stages.append("activate")
                    job.engine.values = live_pair(COMMIT)

            with patch("auto_release.RELEASE_ROOT", base / "releases"), \
                    patch.object(job, "preflight", return_value=preflight), patch.object(job, "check_target"), \
                    patch.object(job, "reclaim_build_cache"), \
                    patch.object(job, "invoke", side_effect=invoke), patch.object(job, "check_health"), \
                    patch("auto_release.command", return_value=("sha256:" + "2" * 64).encode()), \
                    patch("auto_release.cleanup_rehearsal") as cleanup:
                job.execute(COMMIT, job.live())
            self.assertEqual(stages, ["build", "rehearse", "activate"])
            self.assertEqual(len(set(tags)), 1)
            self.assertEqual(job.state["stage"], "已上线")

    def test_shell_checks_deployment_when_git_is_already_current(self):
        """实际运行入口和临时Git仓库，复现两次HEAD相同仍须检查真实发布状态的旧故障。"""
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            repo, source, tools = base / "repo", base / "repo/goodhr5", base / "bin"
            source.mkdir(parents=True)
            tools.mkdir()
            (source / "scripts").mkdir()
            counter = base / "calls"
            (repo / ".gitignore").write_text("/goodhr5/.release-source.lock\n/goodhr5/deploy.log\n")
            script = (ROOT / "auto_deploy.sh").read_text().replace(
                "STATE_DIR=/opt/goodhr-release/auto-deploy", "STATE_DIR=" + shlex.quote(str(base / "state")))
            (source / "auto_deploy.sh").write_text(script)
            (source / "scripts/auto_release.py").write_text(
                '"""文件作用：仅记录临时入口调用，不触发构建。"""\nfrom pathlib import Path\n'
                + "p=Path(" + repr(str(counter)) + ")\np.write_text(p.read_text()+'x' if p.exists() else 'x')\n")
            (tools / "flock").write_text("#!/bin/sh\n# 文件作用：本地入口测试不使用服务器全局锁。\nexit 0\n")
            (tools / "flock").chmod(0o755)
            git = shutil.which("git")
            for args in (["init"], ["symbolic-ref", "HEAD", "refs/heads/main"], ["add", "."],
                         ["-c", "user.name=自动部署测试", "-c", "user.email=auto-test@example.invalid", "commit", "-m", "测试初始提交"],
                         ["remote", "add", "origin", str(repo)]):
                subprocess.run([git] + args, cwd=repo, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
            environment = dict(os.environ, PATH=str(tools) + os.pathsep + os.environ["PATH"])
            for _ in range(2):
                subprocess.run(["sh", str(source / "auto_deploy.sh")], env=environment, check=True, stderr=subprocess.PIPE)
            self.assertEqual(counter.read_text(), "xx")

    def test_cleanup_refuses_a_running_rehearsal_container(self):
        """即使计划SHA正确，只要本轮对象重新运行，也不能按旧计划删除。"""
        from unittest.mock import Mock
        with tempfile.TemporaryDirectory() as directory:
            release = Path(directory).resolve() / "fixture-release"
            rehearsal = release / "rehearsal"
            rehearsal.mkdir(parents=True)
            row = {"id": "1" * 64, "name": "/goodhr-rehearsal-fixture-pg", "image": "sha256:" + "2" * 64}
            plan = {"host": socket.gethostname(), "release_id": release.name, "delete_files": False,
                    "delete_volumes": False, "containers": [row]}
            path = rehearsal / "cleanup-plan.json"
            path.write_text(json.dumps(plan))
            (rehearsal / "journal.json").write_text(json.dumps({"network": "3" * 64, "cleanup_plan": {
                "path": str(path), "sha256": hashlib.sha256(path.read_bytes()).hexdigest()}}))
            engine = Mock()
            engine.inspect.return_value = {"Id": row["id"], "Name": row["name"], "Image": row["image"],
                "State": {"Running": True}, "Config": {"Labels": {"goodhr.rehearsal.id": release.name}}}
            with patch("auto_release.command") as command, self.assertRaises(CutoverError):
                cleanup_rehearsal(engine, release)
            command.assert_not_called()

    def test_failed_restore_data_is_never_deleted(self):
        """只有真实验收和临时对象收尾都通过才可回收PG副本，失败现场继续保留。"""
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory).resolve()
            release = base / ("auto-" + COMMIT[:12] + "-fixture")
            pg = release / "rehearsal/pgdata"
            pg.mkdir(parents=True)
            (pg / "failed-state").write_text("保留排错")
            (release / "isolated-validation.json").write_text(json.dumps({"stage": "isolated_validation",
                "host": socket.gethostname(), "source_commit": COMMIT, "success": True,
                "legacy_data_preserved": True, "rollback_verified": True}))
            (release / "rehearsal/journal.json").write_text('{"success":false}')
            with patch("auto_release.RELEASE_ROOT", base), patch("auto_release.hygiene.apply_plan") as apply, \
                    self.assertRaises(CutoverError):
                cleanup_restore_cache(release)
            apply.assert_not_called()
            self.assertTrue((pg / "failed-state").exists())

    def test_cache_gc_requires_candidate_image_protection(self):
        """当前旧版账本的回收计划也必须保护本轮候选，缺失保护就不能执行任何删除。"""
        from types import SimpleNamespace
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory).resolve()
            registry = base / "fixture/retention-registry.json"
            registry.parent.mkdir()
            registry.write_text("{}")
            job = AutomaticRelease(base / "goodhr5", Engine(), base / "state")
            job.state_root.mkdir()
            job.release = base / "candidate"
            job.release.mkdir()

            def invoke(arguments, environment):
                """invoke 仅提供不保护候选的错误计划，测试必须在apply之前阻断。"""
                (job.release / "preactivation-retention-plan.json").write_text('{"protected_image_ids":[]}')

            with patch("auto_release.RELEASE_ROOT", base), patch("auto_release.command", return_value=str(base).encode()), \
                    patch("auto_release.shutil.disk_usage", return_value=SimpleNamespace(free=0)), \
                    patch.object(job, "invoke", side_effect=invoke) as invoke_mock, self.assertRaises(CutoverError):
                job.reclaim_build_cache(job.live(), {"backend": "sha256:" + "2" * 64, "frontend": "sha256:" + "3" * 64}, {})
            self.assertEqual(invoke_mock.call_count, 1)
            self.assertIn("plan", invoke_mock.call_args.args[0])

    def test_cache_gc_uses_cache_only_plan_and_the_real_apply_arguments(self):
        """空间维护只清构建缓存，apply使用计划已绑定的登记，不传不存在的registry选项。"""
        from types import SimpleNamespace
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory).resolve()
            registry = base / "fixture/retention-registry.json"
            registry.parent.mkdir()
            registry.write_text("{}")
            job = AutomaticRelease(base / "goodhr5", Engine(), base / "state")
            job.state_root.mkdir()
            job.release = base / "candidate"
            job.release.mkdir()
            images = {"backend": "sha256:" + "2" * 64, "frontend": "sha256:" + "3" * 64}

            def invoke(arguments, environment):
                """invoke 提供有明确候选保护且不删容器的缓存计划，记录最终CLI契约。"""
                if "plan" in arguments:
                    (job.release / "preactivation-retention-plan.json").write_text(json.dumps({
                        "cache_only": True, "protected_image_ids": list(images.values()), "containers_to_remove": []}))

            with patch("auto_release.RELEASE_ROOT", base), patch("auto_release.command", return_value=str(base).encode()), \
                    patch("auto_release.shutil.disk_usage", side_effect=[SimpleNamespace(free=0), SimpleNamespace(free=4 * 1024**3)]), \
                    patch.object(job, "invoke", side_effect=invoke) as invoke_mock:
                job.reclaim_build_cache(job.live(), images, {})
            self.assertIn("--cache-only", invoke_mock.call_args_list[0].args[0])
            self.assertNotIn("--registry", invoke_mock.call_args_list[1].args[0])
            self.assertIn("apply", invoke_mock.call_args_list[1].args[0])

    def test_low_space_releases_completed_cache_without_deleting_applications(self):
        """保留一条缓存后仍不足时，第二份独立计划只释放已完成缓存，继续保护所有应用。"""
        from types import SimpleNamespace
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory).resolve()
            registry = base / "fixture/retention-registry.json"
            registry.parent.mkdir()
            registry.write_text("{}")
            job = AutomaticRelease(base / "goodhr5", Engine(), base / "state")
            job.state_root.mkdir()
            job.release = base / "candidate"
            job.release.mkdir()
            images = {"backend": "sha256:" + "2" * 64, "frontend": "sha256:" + "3" * 64}

            def invoke(arguments, environment):
                """invoke 记录两份独立只缓存计划，不执行实际删除。"""
                if "plan" in arguments:
                    Path(arguments[arguments.index("--out") + 1]).write_text(json.dumps({
                        "cache_only": True, "protected_image_ids": list(images.values()), "containers_to_remove": []}))

            free = [SimpleNamespace(free=value * 1024**3) for value in (0, 2, 4)]
            with patch("auto_release.RELEASE_ROOT", base), patch("auto_release.command", return_value=str(base).encode()), \
                    patch("auto_release.shutil.disk_usage", side_effect=free), patch.object(job, "invoke", side_effect=invoke) as invoke_mock:
                job.reclaim_build_cache(job.live(), images, {})
            self.assertEqual(invoke_mock.call_count, 4)
            self.assertNotIn("--drop-build-cache", invoke_mock.call_args_list[0].args[0])
            self.assertIn("--drop-build-cache", invoke_mock.call_args_list[2].args[0])


class MigrationGateTests(unittest.TestCase):
    """覆盖冻结SQL、旧账本完整性和自定义话术的边界，避免为自动化放宽数据检查。"""

    def source(self):
        """source 读取实际冻结SQL，测试不会通过伪造迁移证据绕开摘要门禁。"""
        return {name: (ROOT / "cloud/backend/db/migrations" / name).read_bytes() for name in APPROVED}

    def test_unknown_or_changed_migration_is_rejected(self):
        """只允许已审阅整组迁移，任意新增文件或一字节改动均应在构建前拒绝。"""
        source = self.source()
        source["0001_base.sql"] = b"base"
        self.assertEqual(pending_migrations(source, ["0001_base.sql"]), sorted(APPROVED))
        source["9999_unknown.sql"] = b"unknown"
        with self.assertRaises(CutoverError):
            pending_migrations(source, ["0001_base.sql"])
        del source["9999_unknown.sql"]
        source[next(iter(APPROVED))] += b" changed"
        with self.assertRaises(CutoverError):
            pending_migrations(source, ["0001_base.sql"])

    def test_existing_sql_cannot_change(self):
        """候选不允许修改回退镜像已经携带的旧SQL。"""
        candidate = self.source()
        candidate["0001_base.sql"] = b"changed"
        with self.assertRaises(CutoverError):
            check_archives(candidate, {"0001_base.sql": b"original"}, ["0001_base.sql"], ROOT / "cloud/backend/db/migrations")

    def test_old_ledger_timestamps_are_preserved(self):
        """追加新账本并不能掩盖原账本执行时间被修改。"""
        before = [{"filename": "0001_base.sql", "applied_at": "2020-01-01T00:00:00Z"}]
        after = before + [{"filename": name, "applied_at": "2026-11-11T00:00:01Z"} for name in APPROVED]
        check_ledger(before, after, "2026-11-11T00:00:00Z", "2026-11-11T00:00:02Z")
        after = copy.deepcopy(after)
        after[0]["applied_at"] = "2026-11-11T00:00:01Z"
        with self.assertRaises(CutoverError):
            check_ledger(before, after, "2026-11-11T00:00:00Z", "2026-11-11T00:00:02Z")

    def test_custom_or_empty_library_must_remain_exact(self):
        """用户清空的话术和所有原字段必须原样保留，不能用默认升级覆盖。"""
        row = {"id": "custom", "config_key": "system.auto_reply_library", "config_value": {"phrases": []}}
        sql = (ROOT / "cloud/backend/db/migrations/0121_auto_reply_wechat_defaults.sql").read_text()
        check_library([row], [copy.deepcopy(row)], sql, "2026-11-11T00:00:00Z", "2026-11-11T00:00:02Z")
        with self.assertRaises(CutoverError):
            check_library([row], [dict(row, id="changed")], sql, "2026-11-11T00:00:00Z", "2026-11-11T00:00:02Z")


if __name__ == "__main__":
    unittest.main()
