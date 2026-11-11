"""文件作用：验证GoodHR缓存归属、共享依赖保护和精确ID删除的状态门禁。"""

import copy
from pathlib import Path
import tempfile
import unittest

from release_build_cache import normalize, targets, execute, GuardError


def cache(key, description, parents=(), **values):
    """创建不连接Docker的缓存记录，覆盖Docker 26带空格的父链字段。"""
    return {"ID": key * 25, "Description": description, " Parents": [parent * 25 for parent in parents],
            "InUse": False, "Shared": False, "Size": 10, **values}


class CacheTests(unittest.TestCase):
    """共享Go基础、其他项目和全部运行对象不得被精确缓存清理影响。"""

    def setUp(self):
        """构造共享基础及两个独立项目，不依赖实现内部常量。"""
        self.rows = normalize([cache("a", "golang base"), cache("b", "COPY source", ("a",)),
                               cache("c", 'mount / from exec /bin/sh -c CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /goodhr-server ./cmd/server', ("b",)),
                               cache("d", '[stage-1 5/6] COPY --from=builder /src/db ./db', ("c",)), cache("e", "RUN jijie-api", ("a",))])

    def test_only_goodhr_branch_is_removed(self):
        """共享基础及其他项目保留，GoodHR从孩子到父亲逐项回收。"""
        self.assertEqual(targets(self.rows), ["d" * 25, "c" * 25, "b" * 25])
        self.rows["c" * 25]["InUse"] = True
        self.assertEqual(targets(self.rows), ["d" * 25])
        self.rows["d" * 25]["Shared"] = True
        self.assertEqual(targets(self.rows), [])

    def test_missing_parents_and_unknown_records_fail_closed(self):
        """父链缺项停止，无明确GoodHR标识的缓存不被误删。"""
        with self.assertRaises(GuardError):
            normalize([cache("b", "RUN goodhr-server", ("a",))])
        self.assertEqual(targets(normalize([cache("z", "npm run build")])), [])

    def test_foreign_child_and_state_change_are_protected(self):
        """其他项目依赖GoodHR缓存时保护整条父链，外部状态变化时不执行删除。"""
        self.rows.update(normalize([cache("z", "foreign app")]))
        self.rows["z" * 25]["Parents"] = ["d" * 25]
        self.assertEqual(targets(self.rows), [])
        from unittest.mock import Mock
        reader = Mock()
        reader.caches.return_value = {}
        plan = {"caches": self.rows, "targets": [], "docker": {}}
        # 使用原独占分支，确保进入删除前的状态核验。
        del self.rows["z" * 25]
        plan["targets"] = targets(self.rows)
        with tempfile.TemporaryDirectory() as directory, self.assertRaises(GuardError):
            execute(plan, reader, Path(directory) / "changed.json")
        reader.remove.assert_not_called()

    def test_api_requires_exact_id_filter(self):
        """回收请求必须携带唯一完整ID，不能退化为全局清理。"""
        from types import SimpleNamespace
        from unittest.mock import Mock
        import json
        import urllib.parse
        from release_build_cache import CacheReader
        reader = CacheReader.__new__(CacheReader)
        reader.engine = SimpleNamespace(prefix="/v1.45", request=Mock())
        reader.remove("a" * 25)
        method, url = reader.engine.request.call_args.args
        self.assertEqual(method, "POST")
        query = urllib.parse.parse_qs(urllib.parse.urlsplit(url).query)
        self.assertEqual(json.loads(query["filters"][0]), {"id": ["a" * 25]})
        with self.assertRaises(GuardError):
            reader.remove("")

    def test_exact_delete_preserves_all_other_state(self):
        """逐项回收必须只删除该ID，外部状态变化时在下一步之前停止。"""
        rows = copy.deepcopy(self.rows)

        class Reader:
            """模拟缓存API，同时保持容器与镜像快照完全不变。"""
            def __init__(self):
                """绑定自身作为Docker安全快照读取器。"""
                self.docker = self
            def caches(self):
                """返回完整独立快照。"""
                return copy.deepcopy(rows)
            def snapshot(self):
                """业务容器和镜像始终固定。"""
                return {"running": "unchanged"}
            def remove(self, key):
                """精确模拟单个缓存ID删除。"""
                del rows[key]
                return {"CachesDeleted": [key], "SpaceReclaimed": 10}

        reader = Reader()
        plan = {"caches": copy.deepcopy(rows), "targets": targets(rows), "docker": reader.snapshot()}
        with tempfile.TemporaryDirectory() as directory:
            result = execute(plan, reader, Path(directory) / "audit.json")
        self.assertTrue(result["success"])
        self.assertEqual(set(rows), {"a" * 25, "e" * 25})


if __name__ == "__main__":
    unittest.main()
