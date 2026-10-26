"""文件作用：验证宝塔备份的一份保留、写入保护、路径限制和容器挂载保护。"""

import json
import os
import pathlib
import subprocess
import tempfile
import time
import unittest
from unittest import mock

import panel_backup_retention as retention


class PanelBackupRetentionTests(unittest.TestCase):
    """在临时目录模拟清理，避免接触真实备份或Docker服务。"""

    def setUp(self):
        """为每个场景建立独立目录并替换固定备份根目录。"""
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name).resolve()
        self.patcher = mock.patch.object(retention, 'BACKUP_ROOT', self.root)
        self.patcher.start()
        self.addCleanup(self.patcher.stop)

    def backup(self, name, age=7200):
        """生成指定修改时间的测试备份文件。"""
        path = self.root / name
        path.write_bytes(b'backup')
        timestamp = time.time() - age
        os.utime(path, (timestamp, timestamp))
        return path

    def test_keep_one_and_preview_does_not_delete(self):
        """预览只给出计划，执行只保留日期最新的一份。"""
        files = [self.backup('2026-10-{}.zip'.format(day)) for day in (24, 25, 26)]
        with mock.patch.object(retention, 'container_mount_sources', return_value=set()):
            plan = retention.prune()
            self.assertEqual(plan['keep'], 1)
            self.assertEqual(plan['retained'], ['2026-10-26.zip'])
            self.assertTrue(all(path.exists() for path in files))
            result = retention.prune(True)
        self.assertEqual(result['removed'], ['2026-10-25.zip', '2026-10-24.zip'])
        self.assertEqual([path.name for path in self.root.iterdir()], ['2026-10-26.zip'])

    def test_recent_invalid_and_symlink_files_remain(self):
        """一小时内写入、非法日期、非日期命名和符号链接均不删除。"""
        self.backup('2026-10-26.zip')
        recent = self.backup('2026-10-25.zip', 60)
        invalid = self.backup('2026-02-30.zip')
        other = self.backup('manual.zip')
        link = self.root / '2026-10-24.zip'
        link.symlink_to(other)
        with mock.patch.object(retention, 'container_mount_sources', return_value=set()):
            result = retention.prune(True)
        self.assertEqual(result['removed'], [])
        self.assertTrue(all(path.exists() for path in (recent, invalid, other, link)))

    def test_mount_equal_ancestor_and_descendant_remain(self):
        """目标与挂载源相等或互为上下级时，即使容器停止也保留目标。"""
        self.backup('2026-10-26.zip')
        old = self.backup('2026-10-25.zip')
        for source in (old, self.root, old / 'child'):
            with self.subTest(source=source), mock.patch.object(
                    retention, 'container_mount_sources', return_value={source}):
                result = retention.prune(True)
                self.assertEqual(result['skipped_mounted'], [old.name])
                self.assertTrue(old.exists())

    def test_docker_sources_include_all_containers_and_resolved_paths(self):
        """查询包含停止容器，并同时保护符号链接源及其真实路径。"""
        alias = self.root / 'alias'
        alias.symlink_to(self.root, target_is_directory=True)
        results = [subprocess.CompletedProcess([], 0, 'a' * 64 + '\n'),
                   subprocess.CompletedProcess([], 0, json.dumps([{'Type': 'bind', 'Source': str(alias)}]))]
        with mock.patch.object(retention.subprocess, 'run', side_effect=results) as run:
            sources = retention.container_mount_sources()
        self.assertIn('--all', run.call_args_list[0].args[0])
        self.assertEqual(sources, {alias, self.root})

    def test_docker_failure_never_deletes(self):
        """Docker服务不可访问时必须失败关闭，不能当作没有挂载。"""
        self.backup('2026-10-26.zip')
        old = self.backup('2026-10-25.zip')
        with mock.patch.object(retention.subprocess, 'run', side_effect=subprocess.CalledProcessError(1, 'docker')):
            with self.assertRaises(subprocess.CalledProcessError):
                retention.prune(True)
        self.assertTrue(old.exists())

    def test_incomplete_mount_results_never_delete(self):
        """容器检查缺失或格式异常时，旧备份全部保留。"""
        self.backup('2026-10-26.zip')
        old = self.backup('2026-10-25.zip')
        for output in ('', '{}', '[{"Type":"bind"}]'):
            results = [subprocess.CompletedProcess([], 0, 'a' * 64 + '\n'),
                       subprocess.CompletedProcess([], 0, output)]
            with self.subTest(output=output), mock.patch.object(retention.subprocess, 'run', side_effect=results):
                with self.assertRaises(RuntimeError):
                    retention.prune(True)
                self.assertTrue(old.exists())


if __name__ == '__main__':
    unittest.main()
