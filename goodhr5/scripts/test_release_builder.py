"""文件作用：验证独立构建器身份漂移时停止、缓存回收只指向GoodHR且不删除业务镜像。"""

import hashlib
import json
from pathlib import Path
import socket
import tempfile
import unittest
from unittest.mock import Mock, patch

import release_builder as builder
from release_cutover import CutoverError


class BuilderTests(unittest.TestCase):
    """只模拟构建工具，不连接Docker或下载插件。"""

    def test_changed_identity_prevents_cache_cleanup(self):
        """同名构建器被替换时不能修剪或停止其他人的对象。"""
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            conf, record = root / "config", root / "record.json"
            conf.write_bytes(b"fixed configuration")
            record.write_text(json.dumps({"host": socket.gethostname(), "config_sha256": hashlib.sha256(conf.read_bytes()).hexdigest(),
                                          "container_id": "expected", "image_id": "fixed-image", "builder_config_sha256": hashlib.sha256(b"").hexdigest()}))
            engine = Mock()
            engine.inspect.return_value = {"Id": "foreign", "Image": "fixed-image"}
            value = {"Name": builder.NAME, "Driver": "docker-container", "Nodes": [{"DriverOpts": builder.OPTIONS, "Endpoint": "unix:///var/run/docker.sock"}]}
            with patch.object(builder, "TOOLS", root), patch.object(builder, "RECORD", record), patch.object(builder, "CONFIG", conf), \
                    patch.object(builder, "metadata", return_value=value), patch.object(builder, "call") as call, self.assertRaises(CutoverError):
                builder.trim(engine)
            call.assert_not_called()
            engine.request.assert_not_called()

    def test_trim_is_limited_to_registered_builder(self):
        """回收必须指定完整构建器名称、有限空间目标，不使用全局或强制删除。"""
        engine = Mock(prefix="/v1.45")
        with patch.object(builder, "verify", return_value={"Id": "registered-id"}), patch.object(builder, "call") as call:
            builder.trim(engine)
        arguments = call.call_args.args[0]
        self.assertEqual(arguments[:3], ["prune", "--builder", "goodhr-release"])
        self.assertEqual(arguments[arguments.index("--max-used-space") + 1], "4GiB")
        self.assertEqual(builder.CONFIG.read_text().count('maxUsedSpace = "4GiB"'), 2)
        self.assertIn('mirrors = ["docker.m.daocloud.io", "docker.1ms.run", "docker.1panel.live"]', builder.CONFIG.read_text())
        self.assertNotIn("--force", arguments)
        self.assertEqual(engine.request.call_count, 1)
        self.assertTrue(engine.request.call_args.args[1].endswith("/containers/registered-id/stop?t=30"))


if __name__ == "__main__":
    unittest.main()
