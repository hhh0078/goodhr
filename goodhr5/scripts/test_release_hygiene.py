"""文件作用：用隔离临时目录验证发布收尾的越界、链接、业务备份、挂载、变化与正常清理保护，不访问真实Docker或服务器。"""

import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import release_hygiene as hygiene


class FakeReader:
    """提供可控制的容器、挂载和离线TOC结果，所有测试都不调用真实服务。"""

    def __init__(self):
        """__init__ 初始化无引用、无活动任务的隔离状态。"""
        self.rows = []
        self.mount_rows = []
        self.toc_reads = 0

    def containers(self):
        """containers 返回测试容器元数据，不包含环境变量。"""
        return self.rows

    def mounts(self):
        """mounts 返回测试宿主挂载点。"""
        return self.mount_rows

    def dump_toc(self, path):
        """dump_toc 模拟可读的离线备份目录并记录核验次数。"""
        self.toc_reads += 1
        return hashlib.sha256(b"TABLE public example").hexdigest()


class HygieneTests(unittest.TestCase):
    """在独立测试发布根目录运行有实际文件变化的保护场景。"""

    def setUp(self):
        """setUp 创建本机隔离目录并替换发布根，绝不触碰/opt或真实源码。"""
        self.temporary = tempfile.TemporaryDirectory()
        self.base = Path(self.temporary.name).resolve()
        self.release = self.base / "test-release"
        self.release.mkdir()
        self.cache = self.release / "frontend-build" / "node_modules"
        self.cache.mkdir(parents=True)
        (self.cache / "cache.bin").write_bytes(b"rebuildable")
        self.reader = FakeReader()
        self.manifest = {"schema_version": 1, "release_dir": str(self.release), "host": hygiene.socket.gethostname(),
                         "status": "succeeded", "completed_workdirs": ["frontend-build/node_modules"]}
        self.root_patch = patch.object(hygiene, "RELEASE_ROOT", self.base)
        self.root_patch.start()
        self.write_manifest()

    def tearDown(self):
        """tearDown 仅由TemporaryDirectory回收本机测试夹具。"""
        self.root_patch.stop()
        self.temporary.cleanup()

    def write_manifest(self):
        """write_manifest 保存测试完成证据，供状态和摘要变化场景复用。"""
        (self.release / hygiene.MANIFEST_NAME).write_text(json.dumps(self.manifest), encoding="utf-8")

    def plan_file(self):
        """plan_file 将只读计划保存到目标树之外。"""
        plan = hygiene.create_plan(self.release, self.reader)
        path = self.base / "plan.json"
        hygiene.write_json(path, plan)
        return path, hygiene.file_sha(path)

    def test_release_scope_and_relative_traversal_rejected(self):
        """test_release_scope_and_relative_traversal_rejected 拒绝发布根本身、嵌套发布目录和相对路径回退。"""
        for path in (self.base, self.cache, self.base.parent):
            with self.assertRaises(hygiene.HygieneError):
                hygiene.create_plan(path, self.reader)
        self.manifest["completed_workdirs"] = ["../other/node_modules"]
        self.write_manifest()
        with self.assertRaises(hygiene.HygieneError):
            hygiene.create_plan(self.release, self.reader)

    def test_running_or_unrecorded_build_keeps_files(self):
        """test_running_or_unrecorded_build_keeps_files 发布未结束或缺少manifest时不能清理。"""
        self.manifest["status"] = "running"
        self.write_manifest()
        with self.assertRaises(hygiene.HygieneError):
            hygiene.create_plan(self.release, self.reader)
        (self.release / hygiene.MANIFEST_NAME).unlink()
        with self.assertRaises(hygiene.HygieneError):
            hygiene.create_plan(self.release, self.reader)
        self.assertTrue(self.cache.exists())

    def test_failed_release_cleans_completed_cache_but_keeps_pg(self):
        """test_failed_release_cleans_completed_cache_but_keeps_pg 失败构建只回收明确结束的缓存，未验证PG现场继续保留。"""
        pg = self.release / "rehearsal-pgdata"
        pg.mkdir()
        (pg / "PG_VERSION").write_text("16", encoding="utf-8")
        (pg / "debug-state.bin").write_bytes(b"failed rehearsal evidence")
        self.manifest["status"] = "failed"
        self.manifest["isolated_postgres"] = [{"path": "rehearsal-pgdata", "stopped": False, "rehearsal": {"passed": False}}]
        self.write_manifest()
        path, digest = self.plan_file()
        with patch.object(hygiene.shutil.rmtree, "avoids_symlink_attacks", True, create=True):
            result = hygiene.apply_plan(path, digest, self.reader)
        self.assertEqual(result["deleted"], [str(self.cache)])
        self.assertFalse(self.cache.exists())
        self.assertEqual((pg / "debug-state.bin").read_bytes(), b"failed rehearsal evidence")

    def test_business_backup_blocks_but_package_config_is_normal(self):
        """test_business_backup_blocks_but_package_config_is_normal 业务dump阻止清理，第三方包普通配置允许回收。"""
        package = self.cache / "example"
        package.mkdir()
        (package / "package.json").write_text("{}", encoding="utf-8")
        (package / "config.json").write_text("{}", encoding="utf-8")
        (package / "example.sql").write_text("select 1", encoding="utf-8")
        self.assertEqual(len(hygiene.create_plan(self.release, self.reader)["candidates"]), 1)
        (self.cache / "database.dump").write_bytes(b"PGDMP")
        with self.assertRaises(hygiene.HygieneError):
            hygiene.create_plan(self.release, self.reader)

    def test_source_public_and_rollback_never_targets(self):
        """test_source_public_and_rollback_never_targets 明确拒绝源码、public和回退证据作为目标。"""
        for name in ("source", "frontend-runtime/public", "database.dump", "rollback.sql", hygiene.MANIFEST_NAME):
            self.manifest["completed_workdirs"] = [name]
            self.write_manifest()
            with self.assertRaises(hygiene.HygieneError):
                hygiene.create_plan(self.release, self.reader)

    def test_container_ancestor_mount_and_same_device_bind_block(self):
        """test_container_ancestor_mount_and_same_device_bind_block 停止容器的父目录挂载和同盘bind也必须阻止。"""
        self.reader.rows = [{"id": "a" * 64, "status": "exited", "release_dir": None, "role": None,
                             "mounts": [{"Source": str(self.cache.parent)}]}]
        with self.assertRaises(hygiene.HygieneError):
            hygiene.create_plan(self.release, self.reader)
        self.reader.rows = []
        self.reader.mount_rows = [self.cache / "nested-mount"]
        with self.assertRaises(hygiene.HygieneError):
            hygiene.create_plan(self.release, self.reader)

    def test_active_labelled_build_blocks(self):
        """test_active_labelled_build_blocks 发布标签显示构建仍运行时保留缓存。"""
        self.reader.rows = [{"id": "b" * 64, "status": "running", "release_dir": str(self.release), "role": "build", "mounts": []}]
        with self.assertRaises(hygiene.HygieneError):
            hygiene.create_plan(self.release, self.reader)

    def test_docker_nil_labels_are_guarded_and_remain_null(self):
        """test_docker_nil_labels_are_guarded_and_remain_null 兼容Docker26无Labels容器，模板先判断nil再索引。"""
        reader = hygiene.SafetyReader()
        row = {"id": "d" * 64, "status": "exited", "mounts": [], "release_dir": None, "role": None}
        with patch.object(reader, "query", side_effect=[row["id"] + "\n", json.dumps(row)]) as query:
            self.assertEqual(reader.containers(), [row])
        template = query.call_args_list[1].args[0][2]
        self.assertEqual(template.count("{{if .Config.Labels}}"), 2)
        self.assertEqual(template.count("{{else}}null{{end}}"), 2)

    def test_changed_tree_and_wrong_sha_stop_before_delete(self):
        """test_changed_tree_and_wrong_sha_stop_before_delete 计划摘要错误或文件同尺寸变化均停止。"""
        path, digest = self.plan_file()
        with self.assertRaises(hygiene.HygieneError):
            hygiene.apply_plan(path, "0" * 64, self.reader)
        (self.cache / "cache.bin").write_bytes(b"changed____")
        with self.assertRaises(hygiene.HygieneError):
            hygiene.apply_plan(path, digest, self.reader)
        self.assertTrue(self.cache.exists())

    def test_new_reference_at_apply_blocks(self):
        """test_new_reference_at_apply_blocks 计划后新出现停止容器引用也不能删除。"""
        path, digest = self.plan_file()
        self.reader.rows = [{"id": "a" * 64, "status": "exited", "release_dir": None, "role": None, "mounts": [{"Source": str(self.cache)}]}]
        with self.assertRaises(hygiene.HygieneError):
            hygiene.apply_plan(path, digest, self.reader)

    def test_symlink_target_rejected(self):
        """test_symlink_target_rejected 目标被替换为指向外部的链接时不跟随。"""
        outside = self.base / "outside"
        outside.mkdir()
        hygiene.shutil.rmtree(self.cache)
        try:
            self.cache.symlink_to(outside, target_is_directory=True)
        except OSError:
            self.skipTest("本机未开放创建符号链接权限")
        with self.assertRaises(hygiene.HygieneError):
            hygiene.create_plan(self.release, self.reader)
        self.cache.unlink()
        self.assertTrue(outside.exists())

    def test_normal_cleanup_preserves_source_and_audit(self):
        """test_normal_cleanup_preserves_source_and_audit 精确清理缓存，保留同发布源码及原始备份。"""
        source = self.release / "source.tar"
        source.write_bytes(b"source archive")
        backup = self.release / "database.dump"
        backup.write_bytes(b"PGDMP original")
        path, digest = self.plan_file()
        with patch.object(hygiene.shutil.rmtree, "avoids_symlink_attacks", True, create=True):
            result = hygiene.apply_plan(path, digest, self.reader)
        self.assertEqual(result["deleted"], [str(self.cache)])
        self.assertFalse(self.cache.exists())
        self.assertEqual(source.read_bytes(), b"source archive")
        self.assertEqual(backup.read_bytes(), b"PGDMP original")
        self.assertTrue(Path(result["journal"]).is_file())

    def test_pg_requires_stopped_verified_backup_and_rehearsal(self):
        """test_pg_requires_stopped_verified_backup_and_rehearsal 只接收有原容器身份及完整备份/演练证据的恢复副本。"""
        pg = self.release / "rehearsal-pgdata"
        pg.mkdir()
        (pg / "PG_VERSION").write_text("16", encoding="utf-8")
        backup = self.release / "database.dump"
        backup.write_bytes(b"PGDMP original")
        report = self.release / "rehearsal-result.private.json"
        report.write_text('{"passed":true}', encoding="utf-8")
        resource = {"path": "rehearsal-pgdata", "stopped": True, "container_ids": ["c" * 64],
                    "backup": {"path": backup.name, "sha256": hygiene.file_sha(backup)},
                    "rehearsal": {"passed": True, "report_path": report.name, "report_sha256": hygiene.file_sha(report)}}
        self.manifest["isolated_postgres"] = [resource]
        self.write_manifest()
        self.assertEqual(len(hygiene.create_plan(self.release, self.reader)["candidates"]), 2)
        self.assertEqual(self.reader.toc_reads, 1)
        resource["rehearsal"]["passed"] = False
        self.write_manifest()
        with self.assertRaises(hygiene.HygieneError):
            hygiene.create_plan(self.release, self.reader)
        self.assertTrue(pg.exists())
        resource["rehearsal"]["passed"] = True
        self.write_manifest()
        plan_path, plan_sha = self.plan_file()
        backup.write_bytes(b"PGDMP replaced")
        with self.assertRaises(hygiene.HygieneError):
            hygiene.apply_plan(plan_path, plan_sha, self.reader)
        self.assertTrue(pg.exists())
        self.assertTrue(self.cache.exists())


if __name__ == "__main__":
    unittest.main()
