"""文件作用：显式使用本机已有PostgreSQL镜像，验证短行摘要仍保护全部字段、重复次数及旧字段投影；不连接生产。"""

import hashlib
import json
import os
import subprocess
import time
from types import SimpleNamespace
import unittest

from release_rehearse import Rehearsal


@unittest.skipUnless(os.environ.get("GOODHR_SNAPSHOT_TEST_IMAGE"), "需要显式指定本机已有PostgreSQL测试镜像")
class SnapshotPostgresTests(unittest.TestCase):
    """只创建无网络、无端口、临时内存数据目录的本轮测试容器。"""

    def test_row_digest_preserves_content_multiplicity_and_projection(self):
        """真实SQL必须忽略行顺序、保留重复和空值差异，新增列只在完整快照中影响摘要。"""
        image = json.loads(subprocess.check_output(["docker", "image", "inspect", os.environ["GOODHR_SNAPSHOT_TEST_IMAGE"]]))[0]
        volumes = list((image["Config"].get("Volumes") or {}).keys())
        self.assertEqual(len(volumes), 1)
        identifier = subprocess.check_output([
            "docker", "create", "--network", "none", "--memory", "384m", "--pids-limit", "128",
            "--label", "goodhr.project=snapshot-test", "--tmpfs", volumes[0] + ":rw,size=128m",
            "--env", "POSTGRES_HOST_AUTH_METHOD=trust", image["Id"],
        ], text=True).strip()
        try:
            subprocess.run(["docker", "start", identifier], check=True, stdout=subprocess.DEVNULL)
            for attempt in range(60):
                if subprocess.run(["docker", "exec", identifier, "pg_isready", "-U", "postgres"],
                                  stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
                    break
                time.sleep(0.25)
            else:
                self.fail("本轮隔离PostgreSQL未就绪")
            runner = Rehearsal.__new__(Rehearsal)
            runner.pg, runner.args = identifier, SimpleNamespace(database="postgres")
            table = "public\tgoodhr_snapshot_contract"
            columns = {table: ["value", "payload", "nullable"]}
            runner.psql("CREATE TABLE goodhr_snapshot_contract(value text,payload jsonb,nullable text); "
                        "INSERT INTO goodhr_snapshot_contract VALUES "
                        "('重复',jsonb_build_object('正文',repeat('中文',4096)),''),"
                        "('重复',jsonb_build_object('正文',repeat('中文',4096)),''),(NULL,NULL,NULL);")
            baseline = runner.database_snapshot(columns)
            self.assertEqual(baseline[table]["rows"], 3)
            runner.psql("CREATE TEMP TABLE snapshot_copy AS SELECT * FROM goodhr_snapshot_contract; "
                        "TRUNCATE goodhr_snapshot_contract; "
                        "INSERT INTO goodhr_snapshot_contract SELECT * FROM snapshot_copy ORDER BY value NULLS FIRST;")
            self.assertEqual(runner.database_snapshot(columns), baseline)
            runner.psql("ALTER TABLE goodhr_snapshot_contract ADD COLUMN new_field text DEFAULT '新增';")
            self.assertEqual(runner.database_snapshot(columns), baseline)
            complete = runner.database_snapshot()
            runner.psql("UPDATE goodhr_snapshot_contract SET new_field='变化';")
            self.assertEqual(runner.database_snapshot(columns), baseline)
            self.assertNotEqual(runner.database_snapshot(), complete)
            runner.psql("UPDATE goodhr_snapshot_contract SET nullable='' WHERE value IS NULL;")
            self.assertNotEqual(runner.database_snapshot(columns), baseline)
            runner.psql("UPDATE goodhr_snapshot_contract SET nullable=NULL WHERE value IS NULL; "
                        "DELETE FROM goodhr_snapshot_contract WHERE ctid IN "
                        "(SELECT ctid FROM goodhr_snapshot_contract WHERE value='重复' LIMIT 1);")
            after = runner.database_snapshot(columns)
            self.assertEqual(after[table]["rows"], 2)
            self.assertNotEqual(after, baseline)
            runner.psql("TRUNCATE goodhr_snapshot_contract;")
            self.assertEqual(runner.database_snapshot(columns)[table], {"rows": 0, "sha256": hashlib.sha256(b"").hexdigest()})
        finally:
            actual = json.loads(subprocess.check_output(["docker", "container", "inspect", identifier]))[0]
            self.assertEqual(actual["Config"]["Labels"]["goodhr.project"], "snapshot-test")
            subprocess.run(["docker", "stop", "-t", "5", identifier], check=True, stdout=subprocess.DEVNULL)
            subprocess.run(["docker", "container", "rm", identifier], check=True, stdout=subprocess.DEVNULL)


if __name__ == "__main__":
    unittest.main()
