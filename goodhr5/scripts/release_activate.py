"""文件作用：在正式发布既有锁和验收门禁内切换两个应用镜像，保留有序环境及宿主配置，失败恢复原容器；不操作数据库或删除对象。"""

import argparse
import copy
import json
import os
from pathlib import Path
import re
import signal
import socket
import stat
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

from release_cutover import CutoverError, DockerEngine as BaseEngine, require


ROLES = ("backend", "frontend")
IMAGE = re.compile(r"sha256:[0-9a-f]{64}\Z")
COMMIT = re.compile(r"[0-9a-f]{40}\Z")
HEX = re.compile(r"[0-9a-f]{64}\Z")
TAG = re.compile(r"[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}\Z")
ROOT = Path("/opt/goodhr-release")
OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def save_json(path, value, exclusive=False):
    """save_json 以0600写入本次私密快照或安全日志，拒绝覆盖原配置及跟随链接。"""
    path = Path(path)
    require(not path.is_symlink() and path.parent.resolve() == path.parent, "发布记录路径不安全")
    temporary = path if exclusive else path.with_name(path.name + ".tmp")
    descriptor = os.open(str(temporary), os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_NOFOLLOW", 0), 0o600)
    with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
        json.dump(value, stream, ensure_ascii=False, indent=2)
        stream.flush()
        os.fsync(stream.fileno())
    if not exclusive:
        temporary.replace(path)


def environment(old, image):
    """environment 保留原数组全部顺序和重复项，仅按新镜像顺序追加原来没有的默认键。"""
    result = copy.deepcopy(old or [])
    defaults = image or []
    require(all(isinstance(row, str) and "=" in row and "\x00" not in row for row in result + defaults), "环境配置格式不明确")
    keys = {row.split("=", 1)[0] for row in result}
    added = []
    for row in defaults:
        key = row.split("=", 1)[0]
        if key not in keys:
            result.append(row)
            keys.add(key)
            added.append(key)
    for row in result + defaults:
        key, value = row.split("=", 1)
        require(key != "NEXT_PUBLIC_ONBOARDING_PREVIEW_MODE" or value == "", "正式候选禁止启用教程模拟模式")
        require(key != "NEXT_PUBLIC_LOCAL_AGENT_PORT" or value not in ("43130", "43131"), "正式候选禁止指定本地测试端口")
    return result, added


def endpoints(container):
    """endpoints 保留显式网络设置和别名，剔除绑定旧对象ID的自动别名及动态地址。"""
    result = {}
    for name, details in (container.get("NetworkSettings", {}).get("Networks") or {}).items():
        entry = {key: copy.deepcopy(details[key]) for key in ("IPAMConfig", "Links", "Aliases", "DriverOpts") if details.get(key)}
        if entry.get("Aliases"):
            entry["Aliases"] = [row for row in entry["Aliases"] if row not in (container["Id"], container["Id"][:12])]
        result[name] = entry
    return result


def mounts(container):
    """mounts 比较真实挂载源、目标和权限，防止换工作区时误挂空目录。"""
    return sorted([{key: row.get(key) for key in ("Type", "Source", "Destination", "RW", "Propagation")} for row in container.get("Mounts", [])], key=lambda row: (row["Destination"], row["Source"]))


def payload_for(old, image):
    """payload_for 深复制原运行参数，仅替换镜像、正式审计标签和明确新增的镜像环境默认键。"""
    host = old["HostConfig"]
    require(not host.get("AutoRemove") and not host.get("ContainerIDFile") and not host.get("VolumesFrom"), "原服务包含不支持的自动删除或外部容器依赖")
    require(all(row.get("Type") == "bind" for row in old.get("Mounts", [])), "正式切换仅支持已核对的绑定挂载")
    require(not old.get("State", {}).get("Paused"), "原正式容器处于暂停状态")
    require(not old["Config"].get("MacAddress") and not any(details.get("IPAMConfig") for details in (old.get("NetworkSettings", {}).get("Networks") or {}).values()), "静态IP或MAC需要单独核对停止和恢复策略")
    value = copy.deepcopy(old["Config"])
    # Docker会用镜像默认值补入旧Config的空字段；有差异必须在停止前审阅，而不是重建后才发现。
    defaults = image["Config"]
    for key in ("User", "WorkingDir", "StopSignal", "Entrypoint"):
        require(value.get(key) or not defaults.get(key), "候选将补入未核对的默认运行字段：" + key)
    if not value.get("Entrypoint"):
        require(value.get("Cmd") or not defaults.get("Cmd"), "候选将补入未核对的默认命令")
    if defaults.get("Healthcheck"):
        current_check = value.get("Healthcheck") or {}
        require(all(current_check.get(key) or not content for key, content in defaults["Healthcheck"].items()), "候选将补入未核对的默认健康检查字段")
    value["Image"] = image["Id"]
    value["Env"], added = environment(value.get("Env"), image["Config"].get("Env"))
    labels = value.get("Labels") or {}
    labels.update({key: content for key, content in (image["Config"].get("Labels") or {}).items() if key.startswith("goodhr.")})
    if "org.opencontainers.image.revision" in labels:
        labels["org.opencontainers.image.revision"] = labels["goodhr.source_commit"]
    if "com.docker.compose.image" in labels:
        labels["com.docker.compose.image"] = image["Id"]
    value["Labels"] = labels
    # Docker会补入镜像默认标签、挂载与暴露端口；提前拒绝未授权的功能参数增量。
    require(set(image["Config"].get("Labels") or {}) <= set(labels), "候选带有未核对的新默认标签")
    for key in ("Volumes", "ExposedPorts"):
        require(set(image["Config"].get(key) or {}) <= set(value.get(key) or {}), "候选带有未核对的新默认挂载或端口")
    require(not image["Config"].get("Healthcheck") or value.get("Healthcheck"), "候选新增健康检查需要先核对")
    value["HostConfig"] = copy.deepcopy(host)
    if host.get("NetworkMode") not in ("host", "none"):
        value["NetworkingConfig"] = {"EndpointsConfig": endpoints(old)}
    return value, added


def verify_gate_result(value, commit, candidates):
    """verify_gate_result 将实时验收返回绑定到本次主机、提交和两个候选，不能用其他成功报告替代。"""
    expected = {"success": True, "host": socket.gethostname(), "source_commit": commit, "backend_image": candidates["backend"]["image"], "frontend_image": candidates["frontend"]["image"]}
    require(isinstance(value, dict) and all(value.get(key) == content for key, content in expected.items()), "实时验收结果没有绑定本次候选")
    require(all(HEX.fullmatch(value.get(key, "")) for key in ("before_migration_fingerprint", "after_migration_fingerprint")), "实时验收迁移指纹不完整")


def verify_runtime(actual, old, payload, running):
    """verify_runtime 在创建和启动后核对完整配置、环境顺序、挂载、端口及显式网络。"""
    expected = {key: value for key, value in payload.items() if key not in ("HostConfig", "NetworkingConfig")}
    require(actual is not None and actual["Image"] == payload["Image"] and actual["State"]["Running"] is running, "容器镜像或运行状态不符")
    require(actual["Config"] == expected, "新容器功能配置或环境顺序未完整保留")
    require(actual["HostConfig"] == payload["HostConfig"], "新容器宿主参数未完整保留")
    require(mounts(actual) == mounts(old) and endpoints(actual) == endpoints(old), "新容器挂载或网络参数未完整保留")


class DockerEngine(BaseEngine):
    """复用已验证的本机Docker读写能力，仅增加通用镜像读取和精确命名。"""

    def image(self, identifier):
        """image 按不可变ID读取候选元数据，不使用可能漂移的标签。"""
        require(IMAGE.fullmatch(identifier), "候选镜像ID不完整")
        return self.request("GET", self.prefix + "/images/" + urllib.parse.quote(identifier, safe="") + "/json")

    def rename(self, identifier, name):
        """rename 只允许两个正式名称及本次格式的备份名称，不提供删除接口。"""
        require(HEX.fullmatch(identifier) and re.fullmatch(r"goodhr-production-(backend|frontend)-1(?:-(?:backup|failed)-[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})?", name), "容器改名超出正式发布范围")
        try:
            self.request("POST", self.prefix + "/containers/" + identifier + "/rename?name=" + urllib.parse.quote(name))
        except CutoverError:
            require(self.inspect(identifier)["Name"].lstrip("/") == name, "容器改名结果无法确认")


def wait_health(url):
    """wait_health 仅访问已核对的本机HTTP入口，有限等待，失败由调用者恢复原双服务。"""
    require(re.fullmatch(r"http://127\.0\.0\.1:[0-9]+/(?:health)?", url), "健康检查只能访问本机明确端口")
    for _ in range(24):
        try:
            with OPENER.open(url, timeout=3) as response:
                if response.status == 200:
                    return
        except (OSError, urllib.error.HTTPError):
            pass
        time.sleep(2)
    raise CutoverError("正式服务健康检查未通过")


class Activation:
    """在调用方已持发布锁后执行可回退的双服务切换，依赖可注入以测试故障边界。"""

    def __init__(self, release_dir, release_id, commit, candidates, engine, health, gate, save=save_json):
        """__init__ 绑定本次目录、提交和不可变镜像，所有私密配置只写受限快照。"""
        self.root, self.release_id, self.commit = Path(release_dir), release_id, commit
        self.candidates, self.engine, self.health, self.gate, self.save = candidates, engine, health, gate, save
        require(TAG.fullmatch(release_id) and COMMIT.fullmatch(commit) and set(candidates) == set(ROLES), "正式发布身份不完整")
        self.originals, self.payloads = {}, {}
        self.journal = {"schema_version": 1, "host": socket.gethostname(), "source_commit": commit, "release_id": release_id, "created": {}, "added_environment_keys": {}, "events": []}

    def record(self, phase, role=None):
        """record 在动作前后保存非敏感阶段，日志绝不包含环境值和daemon错误正文。"""
        self.journal["events"].append({"phase": phase, "role": role})
        self.save(self.root / "activation-journal.json", self.journal)

    def note(self, phase, role=None):
        """note 在故障恢复中尽力写日志，磁盘错误不能中断另一服务的恢复。"""
        try:
            self.record(phase, role)
        except Exception:
            pass

    def name(self, role, suffix=""):
        """name 生成本次唯一正式、回退或失败名称，不接受外部容器名。"""
        return "goodhr-production-" + role + "-1" + ("-" + suffix + "-" + self.release_id if suffix else "")

    def preserved(self):
        """preserved 比较全部非目标容器身份与状态，禁止顺带改变其他应用。"""
        original_ids = {row["Id"] for row in self.originals.values()}
        expected = {row["Id"]: row for row in self.baseline if row["Id"] not in original_ids}
        actual = {row["Id"]: row for row in self.engine.snapshot()}
        require(all(actual.get(identifier) == row for identifier, row in expected.items()), "其他容器状态发生变化")
        require(set(actual) == set(expected) | original_ids | set(self.journal["created"].values()), "出现计划外容器对象")

    def verify_old(self, role, running=True):
        """verify_old 每次停止前按ID、镜像、名字、完整配置和网络执行比较后切换。"""
        old = self.originals[role]
        current = self.engine.inspect(self.name(role))
        require(current["Id"] == old["Id"] and current["Image"] == old["Image"], "正式原容器身份已经改变")
        require(current["State"]["Running"] is running and current["Config"] == old["Config"] and current["HostConfig"] == old["HostConfig"] and mounts(current) == mounts(old) and endpoints(current) == endpoints(old), "正式原容器配置已经改变")

    def prepare(self):
        """prepare 在停止前核对镜像标签、原参数、发布门禁，保存0600原配置且拒绝覆盖旧现场。"""
        require(not any((self.root / name).exists() for name in ("activation-originals.private.json", "activation-journal.json", "production-result.json")), "本次切换已有现场，禁止重复激活")
        self.baseline = self.engine.snapshot()
        for role in ROLES:
            image = self.engine.image(self.candidates[role]["image"])
            labels = image.get("Config", {}).get("Labels") or {}
            require(image["Id"] == self.candidates[role]["image"] and [labels.get(key) for key in ("goodhr.source_commit", "goodhr.release.id", "goodhr.release_kind", "goodhr.dockerfile_sha256")] == [self.commit, self.release_id, role, self.candidates[role]["dockerfile_sha256"]], "实际候选镜像元数据与正式发布不一致")
            old = self.engine.inspect(self.name(role))
            require(old is not None and HEX.fullmatch(old["Id"]) and old["Name"].lstrip("/") == self.name(role) and old["State"]["Running"], "正式原服务身份或状态异常")
            require(all(self.engine.inspect(self.name(role, suffix), missing=True) is None for suffix in ("backup", "failed")), "本次备份或失败名称已经占用")
            self.originals[role] = old
            self.payloads[role], self.journal["added_environment_keys"][role] = payload_for(old, image)
        self.gate()
        require(self.engine.snapshot() == self.baseline, "准备期间容器基线发生变化")
        for role in ROLES:
            self.verify_old(role)
        self.save(self.root / "activation-originals.private.json", {"host": socket.gethostname(), "source_commit": self.commit, "originals": self.originals}, exclusive=True)
        self.record("prepared")

    def create(self, role):
        """create 处理响应丢失时核对精确新名称，先登记ID再核验配置与启动。"""
        self.record("creating", role)
        try:
            identifier = self.engine.create(self.name(role), self.payloads[role])
        except Exception:
            actual = self.engine.inspect(self.name(role), missing=True)
            labels = (actual or {}).get("Config", {}).get("Labels") or {}
            if actual and actual["Image"] == self.candidates[role]["image"] and actual["Id"] not in {row["Id"] for row in self.baseline} and labels.get("goodhr.release.id") == self.release_id and labels.get("goodhr.source_commit") == self.commit and labels.get("goodhr.release_kind") == role:
                self.journal["created"][role] = actual["Id"]
            raise
        require(HEX.fullmatch(identifier) and identifier not in {row["Id"] for row in self.baseline}, "新容器ID不明确")
        self.journal["created"][role] = identifier
        self.record("created", role)
        verify_runtime(self.engine.inspect(identifier), self.originals[role], self.payloads[role], False)
        self.preserved()
        self.engine.start(identifier)
        self.record("started", role)
        verify_runtime(self.engine.inspect(identifier), self.originals[role], self.payloads[role], True)
        self.health(role)

    def restore(self):
        """restore 尽力恢复原两个ID，失败新容器停止改名保留，不删除数据、镜像或容器。"""
        errors = []
        for role in ("frontend", "backend"):
            try:
                old = self.originals[role]
                original = self.engine.inspect(old["Id"])
                require(original["Image"] == old["Image"] and original["Config"] == old["Config"] and original["HostConfig"] == old["HostConfig"] and mounts(original) == mounts(old) and endpoints(original) == endpoints(old), "原回退对象配置发生变化")
                require(original["Name"].lstrip("/") in (self.name(role), self.name(role, "backup")), "原回退对象名称发生变化")
                current = self.engine.inspect(self.name(role), missing=True)
                if current and current["Id"] != old["Id"]:
                    require(current["Id"] == self.journal["created"].get(role) and current["Image"] == self.candidates[role]["image"], "正式名称已被其他容器占用")
                    require(self.engine.inspect(self.name(role, "failed"), missing=True) is None, "失败保留名称已占用")
                    if current["State"]["Running"]:
                        self.engine.stop(current["Id"])
                    self.engine.rename(current["Id"], self.name(role, "failed"))
                if original["Name"].lstrip("/") != self.name(role):
                    self.engine.rename(old["Id"], self.name(role))
                if not original["State"]["Running"]:
                    self.engine.start(old["Id"])
                self.verify_old(role)
                self.note("old_restored", role)
            except Exception:
                errors.append(role)
        for role in ROLES:
            try:
                self.health(role)
            except Exception:
                errors.append(role + "_health")
        try:
            self.preserved()
        except Exception:
            errors.append("other_containers")
        require(not errors, "原双服务恢复仍有未通过条件，请按私密快照和日志核对")

    def activate(self):
        """activate 串行停止并保留原服务，启动新镜像；任一故障自动尝试恢复两端。"""
        self.prepare()
        try:
            for role in ("frontend", "backend"):
                self.preserved()
                self.verify_old(role)
                self.record("stopping_old", role)
                self.engine.stop(self.originals[role]["Id"])
                self.record("renaming_old", role)
                self.engine.rename(self.originals[role]["Id"], self.name(role, "backup"))
                self.record("old_retained", role)
            for role in ROLES:
                self.create(role)
            for role in ROLES:
                actual = self.engine.inspect(self.name(role))
                require(actual["Id"] == self.journal["created"][role], "新正式容器ID发生漂移")
                verify_runtime(actual, self.originals[role], self.payloads[role], True)
            self.preserved()
            result = {"success": True, "stage": "production_activation", "host": socket.gethostname(), "source_commit": self.commit, "release_id": self.release_id, "containers": dict(self.journal["created"]), "images": {role: self.candidates[role]["image"] for role in ROLES}, "added_environment_keys": self.journal["added_environment_keys"], "other_containers_unchanged": True, "database_restored": False}
            self.record("activation_succeeded")
            self.save(self.root / "production-result.json", result)
            return result
        except BaseException as error:
            self.note("activation_failed")
            restored = False
            try:
                self.restore()
                restored = True
            except BaseException:
                pass
            self.note("automatic_rollback_succeeded" if restored else "automatic_rollback_incomplete")
            try:
                self.save(self.root / "production-result.json", {"success": False, "stage": "production_activation", "automatic_rollback_succeeded": restored, "error_type": type(error).__name__, "database_restored": False})
            except Exception:
                pass
            raise CutoverError("正式切换未完成；" + ("原双服务已恢复" if restored else "自动回退未完整通过，请核对持久日志")) from None


def inherited_locks(source_root):
    """inherited_locks 确认正式入口已持有全部锁，禁止直接调用helper绕过统一发布流程。"""
    require(os.name == "posix", "正式激活仅允许Linux主机执行")
    import fcntl
    expected = {7: ROOT / ".release-operation.lock", 8: ROOT / ".backend-deploy.lock", 9: ROOT / ".frontend-deploy.lock", 10: Path("/www/wwwroot/goodhr-production/backups/.goodhr-container-deploy.lock"), 11: source_root / ".release-source.lock"}
    for descriptor, path in expected.items():
        info = os.fstat(descriptor)
        require(path.resolve() == path and not path.is_symlink() and stat.S_ISREG(info.st_mode) and info.st_uid == 0 and (info.st_dev, info.st_ino) == (path.stat().st_dev, path.stat().st_ino), "正式入口发布锁身份不匹配")
        probe = os.open(str(path), os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            try:
                fcntl.flock(probe, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                continue
            raise CutoverError("正式入口尚未持有发布锁")
        finally:
            os.close(probe)


def main():
    """main 仅接受统一发布入口传入的不可变候选，复核源码和验收证据后切换。"""
    parser = argparse.ArgumentParser(description="保留原运行参数的GoodHR正式镜像激活")
    for argument in ("release-dir", "release-id", "source-commit", "backend-image", "frontend-image", "backend-dockerfile-sha", "frontend-dockerfile-sha", "backend-url", "frontend-url", "evidence", "registry"):
        parser.add_argument("--" + argument, required=True)
    args = parser.parse_args()

    def interrupted(signum, frame):
        """interrupted 把常见退出信号转成受控异常，让已开始的切换先恢复原两端。"""
        raise CutoverError("正式激活收到中断信号")

    for signum in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
        signal.signal(signum, interrupted)
    source_root = Path(__file__).resolve().parents[1]
    inherited_locks(source_root)
    release = Path(args.release_dir)
    require(release == ROOT / args.release_id and release.is_dir() and release.resolve() == release, "本次发布目录不匹配")
    require(COMMIT.fullmatch(args.source_commit), "源码提交不完整")
    for role in ROLES:
        require(IMAGE.fullmatch(getattr(args, role + "_image")) and HEX.fullmatch(getattr(args, role + "_dockerfile_sha")), "候选镜像或构建文件摘要不完整")

    def gate():
        """gate 在配置备份后再次检查干净源码和实时账本，拒绝验收后漂移。"""
        commands = (["git", "rev-parse", "HEAD"], ["git", "status", "--porcelain", "--untracked-files=normal"])
        results = [subprocess.run(command, cwd=source_root, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30) for command in commands]
        require(all(result.returncode == 0 for result in results) and results[0].stdout.decode().strip() == args.source_commit and not results[1].stdout.strip(), "激活前源码提交或工作区发生变化")
        evidence_path = Path(args.evidence)
        require(evidence_path.is_file() and not evidence_path.is_symlink() and evidence_path.resolve() == evidence_path, "隔离验收证据路径不安全")
        evidence_bytes = evidence_path.read_bytes()
        evidence = json.loads(evidence_bytes)
        expected = {"stage": "isolated_validation", "success": True, "legacy_data_preserved": True, "rollback_verified": True, "host": socket.gethostname(), "source_commit": args.source_commit, "backend_image": args.backend_image, "frontend_image": args.frontend_image}
        require(isinstance(evidence, dict) and all(evidence.get(key) == value for key, value in expected.items()), "隔离验收证据已经改变或不属于本次候选")
        result = subprocess.run([sys.executable, str(source_root / "scripts/release_retention.py"), "check", "--registry", args.registry, "--evidence", args.evidence], stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=120)
        require(result.returncode == 0, "激活前实时迁移或兼容证据检查未通过")
        verify_gate_result(json.loads(result.stdout), args.source_commit, candidates)
        require(evidence_path.read_bytes() == evidence_bytes, "实时验收期间证据发生变化")

    candidates = {role: {"image": getattr(args, role + "_image"), "dockerfile_sha256": getattr(args, role + "_dockerfile_sha")} for role in ROLES}
    result = Activation(release, args.release_id, args.source_commit, candidates, DockerEngine(), lambda role: wait_health(getattr(args, role + "_url")), gate).activate()
    print(json.dumps(result, ensure_ascii=False))


if __name__ == "__main__":
    try:
        main()
    except (CutoverError, OSError, ValueError, KeyError, TypeError, subprocess.TimeoutExpired):
        print("正式激活未完成，请核对本次受限配置快照和 activation-journal.json；未操作数据库或删除镜像。", file=sys.stderr)
        sys.exit(1)
