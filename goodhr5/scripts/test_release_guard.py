"""文件作用：离线验证发布Dockerfile白名单、旧业务镜像祖先、BuildKit歧义及查询失败保护，不连接真实Docker。"""

import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location("release_guard", Path(__file__).with_name("release_guard.py"))
guard = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(guard)
BACKEND_RUNTIME = "sha256:" + "a" * 64
FRONTEND_RUNTIME = "sha256:" + "b" * 64
POLICY = {"backend": {BACKEND_RUNTIME}, "frontend": {FRONTEND_RUNTIME}}


def image_id(character):
    """image_id 生成结构正确且易区分的离线镜像摘要。"""
    return "sha256:" + character * 64


def image(character, layers, parent="", business=False, label="", release_id=""):
    """image 创建只包含被测白名单字段的镜像，不包含环境或完整历史指令。"""
    return {"Id": image_id(character), "Parent": parent, "RepoTags": ["goodhr-production-backend:old"] if business else ["test:clean"], "Layers": [image_id(x) for x in layers], "DockerfileSHA": label, "Release": "", "ReleaseID": release_id}


class FakeDocker:
    """提供确定的镜像父链与history，确保测试不会调用Docker。"""

    def __init__(self, values, candidate, business=(), history=None):
        """保存固定镜像集合和候选别名。"""
        self.values = {value["Id"]: value for value in values}
        self.candidate = candidate
        self.business = set(business)
        self.history_ids = history or [candidate, "<missing>"]

    def inspect(self, reference):
        """返回指定离线镜像，不存在的父镜像按查询失败拒绝。"""
        try:
            return self.values[self.candidate if reference == "candidate" else reference]
        except KeyError:
            raise guard.GuardError("查询失败") from None

    def business_images(self):
        """返回预设本机业务成品ID。"""
        return self.business

    def history(self, reference):
        """返回预设历史镜像身份。"""
        return self.history_ids


class DockerfileTests(unittest.TestCase):
    """验证正式构建文件及可绕过的FROM边界。"""

    def test_current_production_dockerfiles_pass(self):
        """仓库当前官方多阶段构建可直接通过。"""
        for kind, path in guard.DOCKERFILES.items():
            with self.subTest(kind=kind):
                self.assertEqual(guard.check_dockerfile_path(path, kind, POLICY)["kind"], kind)

    def test_official_digest_stage_alias_and_continuation_pass(self):
        """官方固定digest及同文件先前stage引用均可通过。"""
        text = "FROM golang:1.24-alpine AS build\nFROM docker.io/library/alpine:3.22@" + image_id("1") + " AS runtime\nFROM \\\n runtime\n"
        self.assertEqual(guard.check_dockerfile(text, "backend", POLICY)["stages"], 3)

    def test_role_specific_verified_runtime_passes(self):
        """只允许配置中该角色的精确离线运行镜像。"""
        for kind, reference in (("backend", BACKEND_RUNTIME), ("frontend", FRONTEND_RUNTIME)):
            self.assertEqual(guard.check_dockerfile("FROM " + reference, kind, POLICY)["runner_base"], reference)
        with self.assertRaises(guard.GuardError):
            guard.check_dockerfile("FROM " + FRONTEND_RUNTIME, "backend", POLICY)

    def test_business_variable_unknown_and_wrong_runner_fail(self):
        """成品镜像、变量、裸sha、未知官方版本和Go构建器作为runner全部拒绝。"""
        sources = ["FROM goodhr-production-backend:old", "ARG BASE=alpine:3.22\nFROM ${BASE}", "FROM " + image_id("c"), "FROM alpine:latest", "FROM node:22-alpine", "FROM golang:1.24-alpine AS build\nFROM build", "FROM missing", "FROM --platform=$TARGETPLATFORM alpine:3.22"]
        for text in sources:
            with self.subTest(text=text), self.assertRaises(guard.GuardError):
                guard.check_dockerfile(text, "backend", POLICY)

    def test_unsupported_or_malformed_syntax_fails_closed(self):
        """不完整续行、自定义转义、heredoc与重复stage不能绕过检查。"""
        for text in ("FROM alpine:3.22 \\", "# escape=`\nFROM alpine:3.22", "# syntax=other/parser\nFROM alpine:3.22", "FROM alpine:3.22 AS same\nFROM same AS same", "FROM alpine:3.22\nRUN <<EOF\nFROM bad\nEOF", "FROM alpine:3.22 # trailing", "RUN true"):
            with self.subTest(text=text), self.assertRaises(guard.GuardError):
                guard.check_dockerfile(text, "backend", POLICY)

    def test_policy_missing_or_malformed_fails(self):
        """缺失策略、缺失角色、未知字段类型和角色冲突均阻止发布。"""
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "policy.json"
            with self.assertRaises(guard.GuardError):
                guard.load_policy(path)
            for value in ({}, {"trusted_runtime_images": []}, {"trusted_runtime_images": {"backend": []}}, {"trusted_runtime_images": {"backend": [BACKEND_RUNTIME], "frontend": [BACKEND_RUNTIME]}}):
                path.write_text(json.dumps(value), encoding="utf-8")
                with self.subTest(value=value), self.assertRaises(guard.GuardError):
                    guard.load_policy(path)
            path.write_text(json.dumps({"trusted_runtime_images": {kind: sorted(refs) for kind, refs in POLICY.items()}}), encoding="utf-8")
            self.assertEqual(guard.load_policy(path), POLICY)


class ImageTests(unittest.TestCase):
    """验证经典Docker与BuildKit缺少父链时的镜像来源保护。"""

    def setUp(self):
        """创建真实临时Dockerfile供摘要验证，其他数据全部离线提供。"""
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.dockerfile = Path(self.directory.name) / "Dockerfile"
        self.dockerfile.write_bytes(b"FROM alpine:3.22\nCOPY server /app/server\n")
        self.digest = hashlib.sha256(self.dockerfile.read_bytes()).hexdigest()

    def check(self, values, candidate, business=(), history=None):
        """使用离线Docker适配器运行完整候选镜像检查。"""
        reader = FakeDocker(values, candidate["Id"], business, history)
        return guard.check_image("candidate", "backend", self.dockerfile, POLICY, reader)

    def test_clean_shared_base_and_identical_application_layers_pass(self):
        """共享官方基础、甚至完全相同的应用文件层都不会被当成继承业务镜像。"""
        old = image("d", "12", business=True)
        for layers in ("13", "12"):
            candidate = image("e", layers, business=True, label=self.digest)
            result = self.check([old, candidate], candidate, [old["Id"], candidate["Id"]])
            self.assertEqual(result["ambiguous_prefixes_with_build_proof"], 0)

    def test_explicit_parent_or_history_business_ancestor_fails_even_with_label(self):
        """Parent或history出现旧业务身份时，摘要标签不能为错误继承放行。"""
        old = image("d", "12", business=True)
        for parent, history in ((old["Id"], None), ("", [image_id("e"), old["Id"], "<missing>"])):
            candidate = image("e", "123", parent=parent, label=self.digest)
            with self.subTest(parent=parent), self.assertRaises(guard.GuardError):
                self.check([old, candidate], candidate, [old["Id"]], history)

    def test_untagged_business_parent_is_rejected(self):
        """父镜像只有发布标签、仓库标签已移除时仍识别为业务成品。"""
        old = image("d", "12")
        old["Release"] = "old-release"
        candidate = image("e", "123", parent=old["Id"], label=self.digest)
        with self.assertRaises(guard.GuardError):
            self.check([old, candidate], candidate)

    def test_exact_verified_runtime_is_allowed_despite_historical_tag(self):
        """已精确核验的干净运行镜像优先于旧仓库名称，避免离线基础被历史标签误伤。"""
        base = image("a", "1", business=True)
        candidate = image("e", "123", parent=base["Id"], label=self.digest)
        result = self.check([base, candidate], candidate, [base["Id"]], [candidate["Id"], base["Id"]])
        self.assertEqual(result["checked_business_images"], 0)

    def test_buildkit_strict_prefix_requires_actual_dockerfile_digest(self):
        """缺Parent且完整旧层为前缀时，只有实际受检文件摘要可消除干净构建歧义。"""
        old = image("d", "12", business=True)
        for label in ("", "wrong", self.digest):
            candidate = image("e", "123", label=label)
            if label == self.digest:
                self.assertEqual(self.check([old, candidate], candidate, [old["Id"]])["ambiguous_prefixes_with_build_proof"], 1)
            else:
                with self.subTest(label=label), self.assertRaises(guard.GuardError):
                    self.check([old, candidate], candidate, [old["Id"]])

    def test_windows_dockerfile_label_uses_original_bytes(self):
        """Windows换行不会导致实际构建摘要与镜像标签不一致。"""
        self.dockerfile.write_bytes(b"FROM alpine:3.22\r\nCOPY server /app/server\r\n")
        old = image("d", "12", business=True)
        candidate = image("e", "123", label=hashlib.sha256(self.dockerfile.read_bytes()).hexdigest())
        self.assertEqual(self.check([old, candidate], candidate, [old["Id"]])["ambiguous_prefixes_with_build_proof"], 1)

    def test_missing_parent_and_parent_cycle_fail_closed(self):
        """父链查询失败或循环不能默认通过。"""
        for parent in (image_id("f"), image_id("e")):
            candidate = image("e", "123", parent=parent, label=self.digest)
            with self.subTest(parent=parent), self.assertRaises(guard.GuardError):
                self.check([candidate], candidate)

    def test_same_release_metadata_parent_chain_passes(self):
        """同次发布的无业务tag元数据Parent与候选文件层完全相同才可豁免history。"""
        runtime = image("a", "1")
        first = image("c", "123", parent=runtime["Id"], release_id="release-current")
        second = image("d", "123", parent=first["Id"], release_id="release-current")
        first["RepoTags"], second["RepoTags"] = [], []
        candidate = image("e", "123", parent=second["Id"], business=True, label=self.digest, release_id="release-current")
        result = self.check([runtime, first, second, candidate], candidate,
                            [first["Id"], second["Id"], candidate["Id"]],
                            [candidate["Id"], second["Id"], first["Id"], runtime["Id"]])
        self.assertEqual(result["same_build_metadata_parents"], 2)

    def test_different_release_metadata_parent_rejected(self):
        """文件层相同也不能将其他发布的无tag业务镜像当成纯元数据父节点。"""
        old = image("d", "123", release_id="release-before")
        old["RepoTags"] = []
        candidate = image("e", "123", parent=old["Id"], label=self.digest, release_id="release-current")
        with self.assertRaises(guard.GuardError):
            self.check([old, candidate], candidate, [old["Id"]])

    def test_same_release_business_tag_parent_still_rejected(self):
        """同次发布身份和相同RootFS不豁免带GoodHR成品tag的业务父镜像。"""
        old = image("d", "123", business=True, release_id="release-current")
        candidate = image("e", "123", parent=old["Id"], label=self.digest, release_id="release-current")
        with self.assertRaises(guard.GuardError):
            self.check([old, candidate], candidate, [old["Id"]])

    def test_legacy_release_label_never_gets_metadata_exception(self):
        """旧goodhr.release即便同RootFS且同时带新ID，也不能享受同次元数据豁免。"""
        old = image("d", "123", release_id="release-current")
        old["Release"], old["RepoTags"] = "legacy-release", []
        candidate = image("e", "123", parent=old["Id"], label=self.digest, release_id="release-current")
        with self.assertRaises(guard.GuardError):
            self.check([old, candidate], candidate, [old["Id"]])

    def test_rootfs_different_never_gets_metadata_exception(self):
        """同发布的更短文件层或同长度但内容不同的RootFS均按业务继承拒绝。"""
        for layers in ("12", "124"):
            old = image("d", layers, release_id="release-current")
            old["RepoTags"] = []
            candidate = image("e", "123", parent=old["Id"], label=self.digest, release_id="release-current")
            with self.subTest(layers=layers), self.assertRaises(guard.GuardError):
                self.check([old, candidate], candidate, [old["Id"]])

    def test_history_only_metadata_not_proven_as_parent_rejected(self):
        """未沿实际Parent追溯确认的相同发布元数据ID，不能从history拒绝集合中扣除。"""
        old = image("d", "123", release_id="release-current")
        old["RepoTags"] = []
        candidate = image("e", "123", label=self.digest, release_id="release-current")
        with self.assertRaises(guard.GuardError):
            self.check([old, candidate], candidate, [old["Id"]], [candidate["Id"], old["Id"]])

    def test_dockerfile_attestation_required_without_ambiguous_layers(self):
        """即便无旧业务层前缀，候选也必须无条件声明匹配实际Dockerfile的摘要。"""
        for label in (None, "", "0" * 64):
            candidate = image("e", "123", label=label, release_id="release-current")
            with self.subTest(label=label), self.assertRaisesRegex(guard.GuardError, "Dockerfile"):
                self.check([candidate], candidate)

    def test_business_discovery_combines_legacy_new_release_and_repository(self):
        """同时发现旧发布标签、新release.id和业务仓库tag，防止tag丢失后漏掉业务身份。"""
        reader = guard.DockerReader()
        repository = json.dumps({"Id": image_id("c"), "Repository": "goodhr-production-backend"})
        with patch.object(reader, "query", side_effect=[repository, image_id("d"), image_id("e")]) as query:
            self.assertEqual(reader.business_images(), {image_id("c"), image_id("d"), image_id("e")})
        self.assertIn("label=goodhr.release", query.call_args_list[1].args[0])
        self.assertIn("label=goodhr.release.id", query.call_args_list[2].args[0])

    def test_real_reader_failures_and_bad_json_are_closed(self):
        """Docker失败、超时和异常JSON均返回清晰失败，不泄露stderr环境内容。"""
        reader = guard.DockerReader()
        for failure in (OSError("hidden secret"), subprocess.TimeoutExpired("docker", 30)):
            with patch.object(guard.subprocess, "run", side_effect=failure), self.assertRaisesRegex(guard.GuardError, "只读查询"):
                reader.inspect("candidate")
        with patch.object(guard.subprocess, "run", return_value=subprocess.CompletedProcess([], 1, b"", b"SECRET_ENV=bad")):
            with self.assertRaises(guard.GuardError) as captured:
                reader.inspect("candidate")
            self.assertNotIn("SECRET_ENV", str(captured.exception))
        for method in (reader.inspect, reader.history):
            with patch.object(reader, "query", return_value="not-json"), self.assertRaises(guard.GuardError):
                method("candidate")
        with patch.object(reader, "query", side_effect=guard.GuardError("查询失败")), self.assertRaises(guard.GuardError):
            reader.business_images()
        for data in ("null", '{"Id":null}', '{"Id":"not-a-sha","Repository":"goodhr-production-backend"}'):
            with patch.object(reader, "query", return_value=data), self.subTest(data=data), self.assertRaises(guard.GuardError):
                reader.business_images()

    def test_query_formats_never_request_env_or_created_by(self):
        """Docker查询参数仅请求白名单字段，防止日志意外泄漏环境值。"""
        reader = guard.DockerReader()
        candidate = image("e", "12")
        candidate.update(DockerfileSHA=None, Release=None, ReleaseID=None)
        with patch.object(reader, "query", return_value=json.dumps(candidate)) as query:
            self.assertIsNone(reader.inspect("candidate")["ReleaseID"])
            self.assertNotIn(".Env", str(query.call_args))
            self.assertEqual(str(query.call_args).count("{{if .Config.Labels}}"), 3)
            self.assertEqual(str(query.call_args).count("{{else}}null{{end}}"), 3)
        with patch.object(reader, "query", return_value=json.dumps(candidate["Id"])) as query:
            reader.history(candidate["Id"])
            self.assertNotIn("CreatedBy", str(query.call_args))


if __name__ == "__main__":
    unittest.main()
