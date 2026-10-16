"""文件作用：用纯内存Docker验证通用正式镜像激活的有序环境、原挂载保留及失败恢复，不连接真实服务。"""

import copy
import json
import os
from pathlib import Path
import stat
import socket
import tempfile
import unittest

import release_activate as activation


SECRET = "synthetic-secret-never-print"
COMMIT = "a" * 40
RELEASE = "unit-test-20261016"


def original(role):
    """original 构造两个带重复环境、假密钥和独立宿主设置的原容器。"""
    identifier = ("1" if role == "backend" else "2") * 64
    name = "goodhr-production-" + role + "-1"
    network = "host" if role == "backend" else "goodhr-production_default"
    return {"Id": identifier, "Name": "/" + name, "Image": "sha256:" + identifier, "RestartCount": 0,
            "State": {"Running": True, "Status": "running", "StartedAt": "old-started"},
            "Config": {"Image": "old-image-reference", "Env": ["SECRET=" + SECRET, "GOODHR_CLOUD_ADDR=127.0.0.1:8086", "GOODHR_CLOUD_ADDR=:8086", "PATH=/usr/bin"], "Labels": {"goodhr.source_commit": "b" * 40, "routing.policy": "keep-me", "org.opencontainers.image.revision": "b" * 40}, "Cmd": ["goodhr-server"] if role == "backend" else ["node", "server.js"], "Entrypoint": None, "Hostname": "original-host", "WorkingDir": "/app", "User": "", "Healthcheck": None, "ExposedPorts": {"3000/tcp": {}} if role == "frontend" else None},
            "HostConfig": {"NetworkMode": network, "PortBindings": {"3000/tcp": [{"HostIp": "127.0.0.1", "HostPort": "5175"}]} if role == "frontend" else {}, "Binds": ["/real-production/uploads:/app/uploads:rw"] if role == "backend" else None, "RestartPolicy": {"Name": "unless-stopped", "MaximumRetryCount": 0}, "LogConfig": {"Type": "json-file", "Config": {"synthetic-credential": SECRET}}, "Memory": 1024, "AutoRemove": False, "ContainerIDFile": "", "VolumesFrom": None},
            "Mounts": [{"Type": "bind", "Source": "/real-production/uploads", "Destination": "/app/uploads", "RW": True, "Propagation": "rprivate"}] if role == "backend" else [],
            "NetworkSettings": {"Networks": {network: {} if role == "backend" else {"Aliases": [identifier[:12], name, "custom-alias"], "IPAMConfig": None, "MacAddress": "aa:bb:cc:dd:ee:ff"}}}}


def image(role):
    """image 构造完整正式标签及新增空预览开关的候选，不引用任何真实镜像。"""
    return {"Id": "sha256:" + ("3" if role == "backend" else "4") * 64,
            "Config": {"Env": ["PATH=/new-default", "NEXT_PUBLIC_ONBOARDING_PREVIEW_MODE=", "NEW_DEFAULT="], "Labels": {"goodhr.release.id": RELEASE, "goodhr.source_commit": COMMIT, "goodhr.release_kind": role, "goodhr.dockerfile_sha256": "d" * 64}}}


class FakeEngine:
    """仅在内存改变两端服务，记录每个动作以断言没有数据库或镜像删除。"""

    def __init__(self):
        """__init__ 建立双端原服务和必须保持原样的其他项目。"""
        self.rows = {row["Id"]: row for row in (original("backend"), original("frontend"))}
        self.other = copy.deepcopy(original("frontend"))
        self.other.update(Id="f" * 64, Name="/other-project", Image="sha256:" + "f" * 64)
        self.rows[self.other["Id"]] = self.other
        self.images = {row["Id"]: row for row in (image("backend"), image("frontend"))}
        self.actions = []
        self.fail = None
        self.uncertain_create = False
        self.corrupt_env = False

    def inspect(self, reference, missing=False):
        """inspect 深复制假容器，防止待测代码直接修改daemon状态。"""
        row = next((row for row in self.rows.values() if reference in (row["Id"], row["Name"].lstrip("/"))), None)
        if row is None and not missing:
            raise activation.CutoverError("合成对象不存在")
        return copy.deepcopy(row)

    def image(self, identifier):
        """image 只读取假候选元数据。"""
        return copy.deepcopy(self.images[identifier])

    def snapshot(self):
        """snapshot 以真实引擎相同字段生成安全身份和运行状态。"""
        return sorted([{"Id": row["Id"], "Name": row["Name"], "Image": row["Image"], "Running": row["State"]["Running"], "Status": row["State"]["Status"], "StartedAt": row["State"]["StartedAt"], "RestartCount": row["RestartCount"]} for row in self.rows.values()], key=lambda row: row["Id"])

    def mutate(self, operation, identifier):
        """mutate 注入一次性失败，并禁止触碰其他项目对象。"""
        if identifier == self.other["Id"]:
            raise AssertionError("操作了其他项目")
        self.actions.append((operation, identifier))
        if self.fail == operation:
            self.fail = None
            raise RuntimeError(SECRET)

    def create(self, name, payload):
        """create 可模拟返回丢失，创建的参数完全来自待测payload。"""
        role = "backend" if "backend" in name else "frontend"
        identifier = ("5" if role == "backend" else "6") * 64
        self.mutate("create_" + role, identifier)
        config = {key: copy.deepcopy(value) for key, value in payload.items() if key not in ("HostConfig", "NetworkingConfig")}
        if self.corrupt_env:
            config["Env"] = list(reversed(config["Env"]))
        self.rows[identifier] = {"Id": identifier, "Name": "/" + name, "Image": payload["Image"], "Config": config, "HostConfig": copy.deepcopy(payload["HostConfig"]), "Mounts": original(role)["Mounts"], "NetworkSettings": {"Networks": copy.deepcopy(payload.get("NetworkingConfig", {}).get("EndpointsConfig", {"host": {}}))}, "State": {"Running": False, "Status": "created", "StartedAt": "never-started"}, "RestartCount": 0}
        if self.uncertain_create:
            self.uncertain_create = False
            raise RuntimeError(SECRET)
        return identifier

    def stop(self, identifier):
        """stop 只改变指定假容器运行状态。"""
        self.mutate("stop", identifier)
        self.rows[identifier]["State"].update(Running=False, Status="exited")

    def start(self, identifier):
        """start 模拟重启后StartedAt发生正常变化。"""
        self.mutate("start", identifier)
        self.rows[identifier]["State"].update(Running=True, Status="running", StartedAt="new-started")

    def rename(self, identifier, name):
        """rename 模拟Docker精确名称冲突保护。"""
        self.mutate("rename", identifier)
        if any(row["Name"] == "/" + name and row["Id"] != identifier for row in self.rows.values()):
            raise activation.CutoverError("合成名称冲突")
        self.rows[identifier]["Name"] = "/" + name


class ActivationTests(unittest.TestCase):
    """覆盖重复环境保留、镜像身份、门禁漂移及双服务故障恢复。"""

    def runner(self, gate=None, health=None, save_fail_phase=None):
        """runner 创建纯假依赖，收集安全日志和受限原配置快照。"""
        engine, audits, private = FakeEngine(), [], []

        def save(path, value, exclusive=False):
            """save 将私密快照与安全日志分开记录，可注入日志磁盘失败。"""
            if value.get("events") and value["events"][-1]["phase"] == save_fail_phase:
                raise OSError(SECRET)
            (private if exclusive else audits).append(copy.deepcopy(value))

        candidates = {role: {"image": image(role)["Id"], "dockerfile_sha256": "d" * 64} for role in activation.ROLES}
        runner = activation.Activation(Path(tempfile.gettempdir()) / "absent-activation-fixture", RELEASE, COMMIT, candidates, engine, health or (lambda role: None), gate or (lambda: None), save)
        return runner, engine, audits, private

    def assert_restored(self, engine):
        """assert_restored 验证原两个ID重回正式运行，其他项目未变化。"""
        for role in activation.ROLES:
            actual = engine.inspect("goodhr-production-" + role + "-1")
            self.assertEqual(actual["Id"], original(role)["Id"])
            self.assertTrue(actual["State"]["Running"])
            self.assertEqual(actual["Config"], original(role)["Config"])
        self.assertEqual(engine.inspect(engine.other["Id"]), engine.other)

    def test_success_preserves_ordered_environment_binds_ports_and_other_labels(self):
        """正常切换保留冲突重复项、真实数据目录和宿主参数，仅追加空默认键并更新审计。"""
        runner, engine, audits, private = self.runner()
        result = runner.activate()
        self.assertTrue(result["success"])
        for role in activation.ROLES:
            actual = engine.inspect(runner.name(role))
            self.assertEqual(actual["Config"]["Env"][:4], original(role)["Config"]["Env"])
            self.assertEqual(actual["Config"]["Env"][4:], ["NEXT_PUBLIC_ONBOARDING_PREVIEW_MODE=", "NEW_DEFAULT="])
            self.assertEqual(actual["HostConfig"], original(role)["HostConfig"])
            self.assertEqual(actual["Mounts"], original(role)["Mounts"])
            self.assertEqual(actual["Config"]["Labels"]["routing.policy"], "keep-me")
            self.assertEqual(actual["Config"]["Labels"]["goodhr.source_commit"], COMMIT)
            self.assertFalse(engine.inspect(runner.name(role, "backup"))["State"]["Running"])
        self.assertIn(SECRET, json.dumps(private))
        self.assertNotIn(SECRET, json.dumps(audits))
        self.assertEqual(engine.inspect(engine.other["Id"]), engine.other)

    def test_bad_image_commit_or_release_never_stops(self):
        """候选标签与冻结提交或发布ID不一致时在任何停止之前拒绝。"""
        for key in ("goodhr.source_commit", "goodhr.release.id", "goodhr.release_kind", "goodhr.dockerfile_sha256"):
            runner, engine, _, _ = self.runner()
            engine.images[image("backend")["Id"]]["Config"]["Labels"][key] = "wrong"
            with self.assertRaises(activation.CutoverError):
                runner.activate()
            self.assertEqual(engine.actions, [])

    def test_existing_compose_image_metadata_updated_without_other_label_changes(self):
        """既有Compose镜像身份同步更新，容器编号等其他标签保留。"""
        old = original("frontend")
        old["Config"]["Labels"].update({"com.docker.compose.image": "sha256:" + "0" * 64, "com.docker.compose.container-number": "1"})
        payload, _ = activation.payload_for(old, image("frontend"))
        self.assertEqual(payload["Labels"]["com.docker.compose.image"], image("frontend")["Id"])
        self.assertEqual(payload["Labels"]["com.docker.compose.container-number"], "1")

    def test_known_compose_builder_label_preserved_from_real_image_shape(self):
        """Compose自动新增的已知构建器审计标签可合并，旧业务标签及运行参数仍保留。"""
        for builder in ("classic", "buildkit"):
            with self.subTest(builder=builder):
                runner, engine, _, _ = self.runner()
                for role in activation.ROLES:
                    engine.images[image(role)["Id"]]["Config"]["Labels"]["com.docker.compose.image.builder"] = builder
                self.assertTrue(runner.activate()["success"])
                for role in activation.ROLES:
                    actual = engine.inspect(runner.name(role))
                    self.assertEqual(actual["Config"]["Labels"]["com.docker.compose.image.builder"], builder)
                    self.assertEqual(actual["Config"]["Labels"]["routing.policy"], "keep-me")
                    self.assertEqual(actual["Config"]["Env"][:4], original(role)["Config"]["Env"])
                    self.assertEqual(actual["HostConfig"], original(role)["HostConfig"])

    def test_unknown_compose_builder_or_other_default_label_never_stops(self):
        """构建器标签只允许明确的两个值，其他未知默认标签仍在停止前拒绝。"""
        for key, content in (("com.docker.compose.image.builder", "unknown"), ("com.docker.compose.image.builder", ""), ("com.docker.compose.image.builder", None), ("unreviewed.default", "classic")):
            with self.subTest(key=key, content=content):
                runner, engine, _, _ = self.runner()
                engine.images[image("backend")["Id"]]["Config"]["Labels"][key] = content
                with self.assertRaises(activation.CutoverError):
                    runner.activate()
                self.assertEqual(engine.actions, [])

    def test_static_ip_or_mac_requires_separate_review_before_stopping(self):
        """静态地址停止后可能被Docker清空，不冒用动态网络恢复策略。"""
        for kind in ("ip", "mac"):
            runner, engine, _, _ = self.runner()
            old = engine.rows[original("frontend")["Id"]]
            if kind == "ip":
                old["NetworkSettings"]["Networks"]["goodhr-production_default"]["IPAMConfig"] = {"IPv4Address": "172.29.0.5"}
            else:
                old["Config"]["MacAddress"] = "aa:bb:cc:dd:ee:ff"
            with self.assertRaises(activation.CutoverError):
                runner.activate()
            self.assertEqual(engine.actions, [])

    def test_new_image_default_runtime_fields_rejected_before_stopping(self):
        """新镜像补入旧空运行字段会改变行为，必须在停止前拒绝。"""
        for key, content in (("User", "1001"), ("WorkingDir", "/new"), ("StopSignal", "SIGUSR1"), ("Entrypoint", ["new-entry"]), ("Healthcheck", {"Test": ["CMD", "true"], "Interval": 10})):
            runner, engine, _, _ = self.runner()
            engine.images[image("backend")["Id"]]["Config"][key] = content
            engine.rows[original("backend")["Id"]]["Config"][key] = None
            with self.assertRaises(activation.CutoverError):
                runner.activate()
            self.assertEqual(engine.actions, [])

    def test_gate_success_for_other_candidate_is_rejected(self):
        """其他发布的成功实时验收也不能替代本次镜像或提交身份。"""
        runner, _, _, _ = self.runner()
        accepted = {"success": True, "host": socket.gethostname(), "source_commit": COMMIT, "backend_image": image("backend")["Id"], "frontend_image": image("frontend")["Id"], "before_migration_fingerprint": "e" * 64, "after_migration_fingerprint": "e" * 64}
        activation.verify_gate_result(accepted, COMMIT, runner.candidates)
        for key in ("host", "source_commit", "backend_image", "frontend_image", "before_migration_fingerprint"):
            wrong = dict(accepted, **{key: "wrong"})
            with self.assertRaises(activation.CutoverError):
                activation.verify_gate_result(wrong, COMMIT, runner.candidates)

    def test_preview_and_fixture_ports_are_rejected_before_stopping(self):
        """生产禁止模拟模式和本地测试Agent端口，无论它来自旧配置还是新默认。"""
        for row in ("NEXT_PUBLIC_ONBOARDING_PREVIEW_MODE=simulation", "NEXT_PUBLIC_LOCAL_AGENT_PORT=43130", "NEXT_PUBLIC_LOCAL_AGENT_PORT=43131"):
            runner, engine, _, _ = self.runner()
            engine.images[image("frontend")["Id"]]["Config"]["Env"].append(row)
            with self.assertRaises(activation.CutoverError):
                runner.activate()
            self.assertEqual(engine.actions, [])

    def test_gate_failure_and_original_drift_never_stops(self):
        """验收门禁或读取后原配置漂移不允许进入停止阶段。"""
        runner, engine, _, _ = self.runner(gate=lambda: (_ for _ in ()).throw(RuntimeError("gate rejected")))
        with self.assertRaises(RuntimeError):
            runner.activate()
        self.assertEqual(engine.actions, [])
        runner, engine, _, _ = self.runner()

        def drift():
            """drift 模拟门禁期间另一操作改变目标环境。"""
            engine.rows[original("backend")["Id"]]["Config"]["Env"].append("OTHER=changed")

        runner.gate = drift
        with self.assertRaises(activation.CutoverError):
            runner.activate()
        self.assertEqual(engine.actions, [])

    def test_each_mutation_failure_restores_both_original_ids(self):
        """停止、改名、任一创建及启动失败都恢复两端，而非只恢复当前端。"""
        for operation in ("stop", "rename", "create_backend", "create_frontend", "start"):
            with self.subTest(operation=operation):
                runner, engine, audits, _ = self.runner()
                engine.fail = operation
                with self.assertRaises(activation.CutoverError):
                    runner.activate()
                self.assert_restored(engine)
                self.assertTrue(audits[-1]["automatic_rollback_succeeded"])
                self.assertNotIn(SECRET, json.dumps(audits))

    def test_lost_create_response_retains_failed_object_and_restores(self):
        """Docker已创建但响应丢失时识别本次对象，停止保留后恢复旧两端。"""
        runner, engine, _, _ = self.runner()
        engine.uncertain_create = True
        with self.assertRaises(activation.CutoverError):
            runner.activate()
        self.assert_restored(engine)
        self.assertIsNotNone(engine.inspect(runner.name("backend", "failed"), missing=True))

    def test_lost_response_with_normalized_config_still_restores(self):
        """创建响应丢失且参数被改变时仍先认领本次对象，避免正式名字阻挡原对象恢复。"""
        runner, engine, _, _ = self.runner()
        engine.uncertain_create = True
        engine.corrupt_env = True
        with self.assertRaises(activation.CutoverError):
            runner.activate()
        self.assert_restored(engine)
        self.assertIsNotNone(engine.inspect(runner.name("backend", "failed"), missing=True))

    def test_daemon_environment_reordering_is_detected_and_rolls_back(self):
        """daemon若重排重复环境，创建后检查会拒绝启动并恢复原服务。"""
        runner, engine, _, _ = self.runner()
        engine.corrupt_env = True
        with self.assertRaises(activation.CutoverError):
            runner.activate()
        self.assert_restored(engine)

    def test_health_failure_and_journal_failure_restore_both(self):
        """健康检查失败或创建后日志写入失败均先恢复，再保存安全失败结果。"""
        state = {"failed": False}

        def health(role):
            """health 只让第一次新后端检查失败，回退健康检查正常。"""
            if not state["failed"]:
                state["failed"] = True
                raise RuntimeError(SECRET)

        for options in ({"health": health}, {"save_fail_phase": "created"}):
            runner, engine, _, _ = self.runner(**options)
            with self.assertRaises(activation.CutoverError):
                runner.activate()
            self.assert_restored(engine)

    def test_unsafe_mount_or_autoremove_never_stops(self):
        """未核对的匿名卷及自动删除设置必须在备份停止之前拒绝。"""
        for unsafe in ("volume", "autoremove"):
            runner, engine, _, _ = self.runner()
            row = engine.rows[original("backend")["Id"]]
            if unsafe == "volume":
                row["Mounts"][0]["Type"] = "volume"
            else:
                row["HostConfig"]["AutoRemove"] = True
            with self.assertRaises(activation.CutoverError):
                runner.activate()
            self.assertEqual(engine.actions, [])

    def test_duplicate_new_default_appended_once_and_existing_empty_kept(self):
        """新默认重复键仅追加第一次，旧空值及重复项不得被镜像默认覆盖。"""
        rows, keys = activation.environment(["A=", "A=second"], ["A=image", "B=first", "B=second"])
        self.assertEqual(rows, ["A=", "A=second", "B=first"])
        self.assertEqual(keys, ["B"])

    def test_compose_build_limits_are_build_arguments_only(self):
        """低内存构建限制只传入前端构建参数，不改变正式运行环境。"""
        compose = (Path(__file__).resolve().parents[1] / "docker-compose.build.yml").read_text(encoding="utf-8")
        self.assertIn("NODE_OPTIONS: ${GOODHR_BUILD_NODE_OPTIONS:---max-old-space-size=1536}", compose)
        self.assertIn("CIRCLE_NODE_TOTAL: ${GOODHR_BUILD_CPU_COUNT:-1}", compose)
        runtime = compose.split("    environment:")[-1]
        self.assertNotIn("NODE_OPTIONS", runtime)
        self.assertNotIn("CIRCLE_NODE_TOTAL", runtime)

    def test_private_snapshot_mode_and_exclusive_write(self):
        """原配置快照是0600且拒绝覆盖，保证失败后的回退材料仍为第一次原值。"""
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory).resolve() / "original.private.json"
            activation.save_json(path, {"fake-secret": SECRET}, exclusive=True)
            if os.name != "nt":
                self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
            with self.assertRaises(FileExistsError):
                activation.save_json(path, {}, exclusive=True)
            self.assertEqual(json.loads(path.read_text())["fake-secret"], SECRET)


if __name__ == "__main__":
    unittest.main()
