"""文件作用：仅为2026-10-18教程播放器发布准备、演练、切换和回退前端；私有保留完整运行配置，禁止操作后端、数据库和Redis。"""

import argparse
from contextlib import contextmanager
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import socket
import stat
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

SCRIPTS = Path(__file__).resolve().parents[1]
SOURCE = SCRIPTS.parent
sys.path.insert(0, str(SCRIPTS))
sys.dont_write_bytecode = True
from release_activate import DockerEngine, CutoverError, endpoints, mounts, payload_for, require, verify_runtime

HOST = "ldclouda31539"
ADDRESS = "43.228.79.108"
PUBLIC = "https://goodhr.58it.cn"
BACKEND_HEALTH = "http://127.0.0.1:8086"
NAME = "goodhr-production-frontend-1"
OLD_ID = "023dd87d71446eb1db3c6225abe8e0b76e76450d11be345cf7395e62df2ddb32"
OLD_IMAGE = "sha256:b37ea4727988a455965ac09156f702e5331d94eb727305c9298942b796959647"
OLD_COMMIT = "bf751957"
BACKEND_ID = "88a84e527595cdaba73ebc76e1ba1f0a0ee546f2ce7dff578dbdab95d2d38548"
ROOT = Path("/opt/goodhr-release")
LOCKS = (ROOT / ".release-operation.lock", ROOT / ".frontend-deploy.lock", Path("/www/wwwroot/goodhr-production/backups/.goodhr-container-deploy.lock"), SOURCE / ".release-source.lock")
HEX = re.compile(r"[0-9a-f]{64}\Z")
IMAGE = re.compile(r"sha256:[0-9a-f]{64}\Z")
COMMIT = re.compile(r"[0-9a-f]{40}\Z")
SIGNALS = tuple(getattr(signal, name) for name in ("SIGTERM", "SIGINT", "SIGHUP") if hasattr(signal, name))


class NoRedirect(urllib.request.HTTPRedirectHandler):
    """禁止探测被重定向到未经核对的外部地址。"""

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        """redirect_request 拒绝自动跳转，要求直接入口返回成功。"""
        return None


OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())


def command(arguments):
    """command 捕获只读命令输出，错误时不泄露参数、环境或工具原始正文。"""
    try:
        result = subprocess.run(arguments, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=90)
    except (OSError, subprocess.SubprocessError):
        raise CutoverError("发布只读检查无法完成") from None
    require(result.returncode == 0, "发布只读检查未通过")
    return result.stdout.decode("utf-8").strip()


def host_check():
    """host_check 将实际操作限定在指定Linux主机及其已核对的公网地址。"""
    require(os.name == "posix" and socket.gethostname() == HOST and os.geteuid() == 0, "只允许指定Linux主机的root执行")
    addresses = command(["ip", "-4", "-o", "addr", "show"])
    require(re.search(r"\binet\s+" + re.escape(ADDRESS) + r"/", addresses), "目标主机IP不匹配")


def source_check(commit):
    """source_check 复核实际源码提交及干净状态，拒绝激活期间混入其他修改。"""
    require(command(["git", "-C", str(SOURCE), "rev-parse", "HEAD"]) == commit, "源码提交与本次候选不一致")
    require(not command(["git", "-C", str(SOURCE), "status", "--porcelain", "--untracked-files=normal"]), "源码存在未提交改动，发布停止")


@contextmanager
def locked():
    """locked 持有三把既有共用锁及源码锁，不新建未知锁或跟随符号链接。"""
    import fcntl
    descriptors = []
    try:
        for path in LOCKS:
            require(path.is_file() and path.resolve() == path and not path.is_symlink(), "既有发布锁缺失或路径异常")
            descriptor = os.open(path, os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC)
            descriptors.append(descriptor)
            info = os.fstat(descriptor)
            require(stat.S_ISREG(info.st_mode) and info.st_uid == 0, "发布锁归属不正确")
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        yield
    except BlockingIOError:
        raise CutoverError("另有发布正在进行，本次停止") from None
    finally:
        for descriptor in reversed(descriptors):
            os.close(descriptor)


def private_read(path):
    """private_read 只读本次root所有且权限受限的普通JSON材料。"""
    require(path.is_file() and not path.is_symlink() and path.resolve() == path, "私有发布材料缺失或路径异常")
    info = path.stat()
    require(info.st_uid == 0 and stat.S_IMODE(info.st_mode) == 0o600, "私有发布材料必须为root所有的0600文件")
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        raise CutoverError("私有发布材料无法读取") from None
    require(isinstance(value, dict), "私有发布材料格式不正确")
    return value


def save_json(path, value, exclusive=False):
    """save_json 用本次独占随机临时文件原子写入0600材料，失败只清理自己创建的临时文件。"""
    require(not path.is_symlink() and path.parent.resolve() == path.parent, "发布材料路径不安全")
    descriptor, temporary_name = tempfile.mkstemp(prefix="." + path.name + ".", suffix=".tmp", dir=path.parent)
    temporary = Path(temporary_name)
    try:
        os.chmod(temporary, 0o600)
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            json.dump(value, stream, ensure_ascii=False, indent=2)
            stream.flush()
            os.fsync(stream.fileno())
        if exclusive:
            os.link(temporary, path)
        else:
            os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if temporary.exists():
            temporary.unlink()


@contextmanager
def restoring():
    """restoring 在精确回退期间屏蔽重复终止和键盘中断，结束后恢复原信号处理。"""
    previous = {number: signal.getsignal(number) for number in SIGNALS}
    try:
        for number in SIGNALS:
            signal.signal(number, signal.SIG_IGN)
        yield
    finally:
        for number, handler in previous.items():
            signal.signal(number, handler)


def probe(base, paths, wait=False):
    """probe 只发送固定入口GET请求，要求200且不输出响应正文。"""
    require(base == PUBLIC or re.fullmatch(r"http://127\.0\.0\.1:[0-9]{1,5}", base), "健康探测地址超出范围")
    for attempt in range(20 if wait else 1):
        result = {}
        for path in paths:
            require(path in ("/videos", "/login", "/health"), "健康探测路径超出范围")
            try:
                request = urllib.request.Request(base + path, method="GET", headers={"Cache-Control": "no-cache"})
                with OPENER.open(request, timeout=5) as response:
                    result[path] = response.status
            except (OSError, urllib.error.URLError):
                result[path] = None
        if all(value == 200 for value in result.values()):
            return result
        if wait and attempt < 19:
            time.sleep(2)
    raise CutoverError("前端或公开入口健康检查未通过")


def original_equal(actual, old, running=True, started=False):
    """original_equal 核对原对象完整配置、网络及身份；激活前同时核对启动时间。"""
    require(actual is not None and actual["Id"] == old["Id"] and actual["Image"] == old["Image"], "原容器身份漂移")
    require(actual["Config"] == old["Config"] and actual["HostConfig"] == old["HostConfig"] and mounts(actual) == mounts(old) and endpoints(actual) == endpoints(old), "原容器配置或网络漂移")
    require(actual["State"]["Running"] is running, "原容器运行状态不符合预期")
    if started:
        require(actual["State"].get("StartedAt") == old["State"].get("StartedAt") and actual.get("RestartCount") == old.get("RestartCount"), "原容器在准备后发生重启")


class Release:
    """通过固定身份及私有材料执行本次单前端发布，不提供其他项目操作入口。"""

    def __init__(self, root, image, commit, engine):
        """__init__ 绑定本次目录、不可变镜像、源码及可注入Docker引擎。"""
        self.root, self.image, self.commit, self.engine = Path(root), image, commit, engine
        require(self.root.is_absolute() and self.root.parent == ROOT and self.root.resolve() == self.root, "发布目录必须直接位于指定根目录")
        require(re.fullmatch(r"tutorial-hls-[A-Za-z0-9_.-]{1,80}", self.root.name), "发布目录名称超出本次教程范围")
        require(IMAGE.fullmatch(image) and COMMIT.fullmatch(commit), "必须指定完整镜像ID及40位源码提交")
        self.release_id = self.root.name
        self.backup_name = NAME + "-backup-" + self.release_id
        self.failed_name = NAME + "-failed-" + self.release_id
        self.plan_path = self.root / "prepare.private.json"
        self.state_path = self.root / "state.private.json"
        self.plan, self.state = {}, {"status": "new", "owned": {}, "events": []}

    def record(self, phase):
        """record 原子保存阶段及精确候选ID，材料不向标准输出泄露运行配置。"""
        self.state["events"].append({"phase": phase, "time": int(time.time())})
        save_json(self.state_path, self.state)

    def note(self, phase):
        """note 在回退过程中尽力记账，日志写入失败不能阻止恢复原容器。"""
        try:
            self.record(phase)
        except Exception:
            pass

    def load(self):
        """load 读取本次材料并绑定镜像、提交、主机和目录。"""
        require(self.root.is_dir() and self.root.stat().st_uid == 0 and stat.S_IMODE(self.root.stat().st_mode) == 0o700, "发布目录必须为root所有的0700目录")
        self.plan, self.state = private_read(self.plan_path), private_read(self.state_path)
        require([self.plan.get(key) for key in ("host", "release_id", "image", "commit")] == [HOST, self.release_id, self.image, self.commit], "发布材料与本次参数不一致")
        require(self.plan["original"]["Id"] == OLD_ID and self.plan["original"]["Image"] == OLD_IMAGE, "发布材料的原前端身份不匹配")
        self.recover_creation()
        self.reconcile_owned()

    def reconcile_owned(self):
        """reconcile_owned 仅协调已登记精确候选ID在正式名与本轮失败名间的日志滞后。"""
        for identifier, entry in self.state["owned"].items():
            if entry.get("kind") != "activated":
                continue
            actual = self.engine.inspect(identifier, missing=True)
            if not actual or actual["Name"].lstrip("/") == entry["name"]:
                continue
            name = actual["Name"].lstrip("/")
            require({name, entry["name"]} <= {NAME, self.failed_name} and actual["Image"] == self.image and (actual["Config"].get("Labels") or {}).get("goodhr.tutorial_hls_owner") == self.release_id, "已登记候选名称变化超出本轮回退范围")
            entry["name"] = name
            self.note("candidate_name_reconciled")

    def recover_creation(self):
        """recover_creation 按已落盘的创建意图及完整请求识别响应丢失对象，恢复登记后再校验保护范围。"""
        intent = self.state.get("creating")
        if not intent:
            return
        kind, name, payload = intent.get("kind"), intent.get("name"), intent.get("payload", {})
        expected_name = NAME if kind == "activated" else "goodhr-" + self.release_id + "-" + str(kind)
        require(kind in ("activated", "candidate", "rollback-check") and name == expected_name, "创建意图名称超出本轮范围")
        require(payload.get("Image") == (OLD_IMAGE if kind == "rollback-check" else self.image), "创建意图镜像不匹配")
        actual = self.engine.inspect(name, missing=True)
        if kind == "activated" and (actual is None or actual["Id"] == OLD_ID):
            if actual:
                original_equal(actual, self.plan["original"], running=actual["State"]["Running"])
            actual = self.engine.inspect(self.failed_name, missing=True)
            if actual:
                name = self.failed_name
        if actual:
            expected_config = {key: value for key, value in payload.items() if key not in ("HostConfig", "NetworkingConfig")}
            require(actual["Id"] not in {row["Id"] for row in self.plan["baseline"]} and actual["Image"] == payload["Image"], "待恢复候选身份不匹配")
            require(actual["Name"].lstrip("/") == name and actual["Config"] == expected_config and actual["HostConfig"] == payload["HostConfig"], "待恢复候选完整配置不匹配")
            require((actual["Config"].get("Labels") or {}).get("goodhr.tutorial_hls_owner") == self.release_id, "待恢复候选缺少本轮所有权标签")
            self.state["owned"][actual["Id"]] = {"name": name, "kind": kind, "image": payload["Image"]}
        self.state.pop("creating", None)
        self.note("creation_intent_recovered")

    def image_check(self):
        """image_check 核对本次镜像源码、角色、发布标签和正式Dockerfile来源。"""
        image = self.engine.image(self.image)
        labels = image.get("Config", {}).get("Labels") or {}
        dockerfile_sha = hashlib.sha256((SOURCE / "cloud/frontend-next/Dockerfile").read_bytes()).hexdigest()
        require(image["Id"] == self.image and [labels.get(key) for key in ("goodhr.source_commit", "goodhr.release_kind", "goodhr.release.id", "goodhr.dockerfile_sha256")] == [self.commit, "frontend", self.release_id, dockerfile_sha], "候选镜像审计标签不匹配")
        command([sys.executable, str(SCRIPTS / "release_guard.py"), "image", "--kind", "frontend", "--image", self.image])
        return image

    def protected(self):
        """protected 核对全部非前端原对象及后端、PostgreSQL、Redis完整配置和启动时间。"""
        current = {row["Id"]: row for row in self.engine.snapshot()}
        expected = {row["Id"]: row for row in self.plan["baseline"] if row["Id"] != OLD_ID}
        require(all(current.get(identifier) == value for identifier, value in expected.items()), "受保护容器身份或启动状态发生变化")
        allowed = set(expected) | {OLD_ID} | set(self.state["owned"])
        require(OLD_ID in current and set(current) <= allowed, "发现本次材料之外的容器或原回退对象丢失，发布停止")
        for old in self.plan["protected"].values():
            original_equal(self.engine.inspect(old["Id"]), old, started=True)

    def prepare(self):
        """prepare 只读现场后创建0700目录及0600完整快照，拒绝覆盖已有发布材料。"""
        source_check(self.commit)
        require(not self.root.exists(), "发布目录已存在，禁止覆盖原现场")
        require(ROOT.is_dir() and ROOT.resolve() == ROOT, "发布根目录不存在或为链接")
        self.root.mkdir(mode=0o700)
        os.chmod(self.root, 0o700)
        baseline = self.engine.snapshot()
        old = self.engine.inspect(NAME)
        require(old["Id"] == OLD_ID and old["Image"] == OLD_IMAGE and old["State"]["Running"], "生产原前端身份不匹配")
        require((old["Config"].get("Labels") or {}).get("goodhr.source_commit", "").startswith(OLD_COMMIT), "生产原前端源码标签不匹配")
        require(all(self.engine.inspect(name, missing=True) is None for name in (self.backup_name, self.failed_name)), "本次备份或失败名称已占用")
        image = self.image_check()
        payload, added = payload_for(old, image)
        require(not added, "本次只更新前端，候选新增默认环境键需要另行核对")
        protected = {"backend": self.engine.inspect(BACKEND_ID)}
        require(protected["backend"]["Name"].lstrip("/") == "goodhr-production-backend-1", "受保护后端名称不匹配")
        for role in ("postgres", "redis"):
            matches = [row for row in baseline if re.search(r"(?:^|[-_])" + role + r"(?:[-_]|$)", row["Name"].lstrip("/"))]
            require(len(matches) == 1, "受保护" + role + "容器必须唯一")
            protected[role] = self.engine.inspect(matches[0]["Id"])
        require(all(row["State"]["Running"] for row in protected.values()), "受保护服务未运行")
        require(self.engine.snapshot() == baseline, "准备期间容器现场已改变")
        source_check(self.commit)
        self.plan = {"host": HOST, "address": ADDRESS, "release_id": self.release_id, "image": self.image, "commit": self.commit, "original": old, "protected": protected, "baseline": baseline, "payload": payload, "added_environment_keys": added}
        save_json(self.plan_path, self.plan, exclusive=True)
        self.state["status"] = "prepared"
        self.record("prepared")

    def create_owned(self, name, payload, kind):
        """create_owned 在创建响应不明时按唯一名称识别本次候选，并及时登记精确ID。"""
        require(self.engine.inspect(name, missing=True) is None, "本次候选名称已占用")
        self.state["creating"] = {"name": name, "kind": kind, "payload": payload}
        self.record("creating_" + kind)
        try:
            identifier = self.engine.create(name, payload)
        except BaseException:
            actual = self.engine.inspect(name, missing=True)
            if actual and actual["Image"] == payload["Image"] and (actual["Config"].get("Labels") or {}).get("goodhr.tutorial_hls_owner") == self.release_id:
                self.state["owned"][actual["Id"]] = {"name": name, "kind": kind, "image": payload["Image"]}
                self.state.pop("creating", None)
                self.note("creation_response_uncertain_" + kind)
            raise
        require(HEX.fullmatch(identifier) and identifier != OLD_ID and identifier not in {row["Id"] for row in self.plan["baseline"]}, "候选ID与原现场冲突")
        self.state["owned"][identifier] = {"name": name, "kind": kind, "image": payload["Image"]}
        self.state.pop("creating", None)
        self.record("created_" + kind)
        return identifier

    def trial(self, image_id, kind):
        """trial 在127.0.0.1随机端口运行指定前端镜像，避免占用正式端口及网络别名。"""
        name = "goodhr-" + self.release_id + "-" + kind
        payload, _ = payload_for(self.plan["original"], self.engine.image(image_id))
        with socket.socket() as reservation:
            reservation.bind(("127.0.0.1", 0))
            port = reservation.getsockname()[1]
        require(payload["HostConfig"].get("NetworkMode") != "host", "环回演练不支持原前端使用host网络")
        payload["Labels"]["goodhr.tutorial_hls_owner"] = self.release_id
        payload["Labels"]["goodhr.tutorial_hls_trial"] = kind
        payload["HostConfig"]["PortBindings"] = {"3000/tcp": [{"HostIp": "127.0.0.1", "HostPort": str(port)}]}
        payload["HostConfig"]["RestartPolicy"] = {"Name": "no", "MaximumRetryCount": 0}
        payload["MacAddress"] = ""
        mode = payload["HostConfig"].get("NetworkMode", "default")
        payload["NetworkingConfig"] = {"EndpointsConfig": {mode: {"Aliases": [name]}}} if mode not in ("default", "bridge", "none") else {}
        identifier = self.create_owned(name, payload, kind)
        try:
            self.engine.start(identifier)
            actual = self.engine.inspect(identifier)
            bindings = actual["HostConfig"].get("PortBindings")
            require(actual["State"]["Running"] and bindings == payload["HostConfig"]["PortBindings"], "演练候选未限制在预期环回端口")
            result = probe("http://127.0.0.1:" + str(port), ("/videos", "/login"), wait=True)
            self.protected()
            return {"image": image_id, "id": identifier, "loopback_get": result}
        finally:
            actual = self.engine.inspect(identifier, missing=True)
            if actual and actual["State"]["Running"]:
                self.engine.stop(identifier)

    def rehearse(self):
        """rehearse 分别运行新前端与原镜像回退组合，公开GET检查不写业务数据。"""
        self.load()
        require(self.state["status"] == "prepared", "演练必须从本次准备状态开始")
        source_check(self.commit)
        self.image_check()
        self.protected()
        original_equal(self.engine.inspect(NAME), self.plan["original"], started=True)
        candidate = self.trial(self.image, "candidate")
        rollback = self.trial(OLD_IMAGE, "rollback-check")
        public = probe(PUBLIC, ("/videos", "/login"))
        backend_health = probe(BACKEND_HEALTH, ("/health",))
        self.protected()
        original_equal(self.engine.inspect(NAME), self.plan["original"], started=True)
        self.state["rehearsal"] = {"success": True, "commit": self.commit, "candidate": candidate, "rollback": rollback, "public_get": public, "protected_backend_health_get": backend_health}
        self.state["status"] = "rehearsed"
        self.record("rehearsed")

    def health(self):
        """health 检查前端环回和公开页面，后端健康只读探测已有本机端口。"""
        bindings = self.plan["original"]["HostConfig"].get("PortBindings", {}).get("3000/tcp") or []
        require(len(bindings) == 1 and str(bindings[0].get("HostPort", "")).isdigit(), "正式前端端口不明确")
        probe("http://127.0.0.1:" + str(bindings[0]["HostPort"]), ("/videos", "/login"), wait=True)
        probe(PUBLIC, ("/videos", "/login"), wait=True)
        probe(BACKEND_HEALTH, ("/health",))

    def restore(self):
        """restore 仅保留本轮失败前端并恢复精确原容器，不重新创建原前端或操作其他服务。"""
        with restoring():
            self.restore_exact()

    def restore_exact(self):
        """restore_exact 在重复信号被屏蔽后完成精确原前端恢复，日志故障不阻断恢复。"""
        old = self.engine.inspect(OLD_ID)
        original_equal(old, self.plan["original"], running=old["State"]["Running"])
        require(old["Name"].lstrip("/") in (NAME, self.backup_name), "原回退对象名称已改变")
        current = self.engine.inspect(NAME, missing=True)
        if current and current["Id"] != OLD_ID:
            identifier = current["Id"]
            entry = self.state["owned"].get(identifier) or {}
            if not entry:
                payload = copy.deepcopy(self.plan["payload"])
                payload["Labels"]["goodhr.tutorial_hls_owner"] = self.release_id
                require(identifier not in {row["Id"] for row in self.plan["baseline"]}, "正式名称被原现场其他容器占用")
                verify_runtime(current, self.plan["original"], payload, current["State"]["Running"])
                entry = {"name": NAME, "kind": "activated", "image": self.image}
                self.state["owned"][identifier] = entry
                self.note("exact_candidate_recovered")
            require(entry.get("kind") == "activated" and current["Image"] == self.image, "正式名称不是本轮候选，禁止停止")
            require(self.engine.inspect(self.failed_name, missing=True) is None, "失败保留名称已占用")
            if current["State"]["Running"]:
                self.engine.stop(identifier)
            self.engine.rename(identifier, self.failed_name)
            entry["name"] = self.failed_name
            self.note("failed_candidate_retained")
        old = self.engine.inspect(OLD_ID)
        if old["Name"].lstrip("/") != NAME:
            self.engine.rename(OLD_ID, NAME)
        if not old["State"]["Running"]:
            self.engine.start(OLD_ID)
        original_equal(self.engine.inspect(NAME), self.plan["original"])
        self.health()
        self.protected()
        self.state.pop("creating", None)
        self.state["status"] = "rolled_back"
        self.note("original_container_restored")

    def activate(self):
        """activate 持锁复核源码及现场，只替换前端，任一故障自动恢复精确原容器。"""
        self.load()
        require(self.state["status"] == "rehearsed" and self.state.get("rehearsal", {}).get("success") is True, "缺少本次完整新旧前端演练")
        source_check(self.commit)
        self.image_check()
        self.protected()
        original_equal(self.engine.inspect(NAME), self.plan["original"], started=True)
        require(self.engine.inspect(self.backup_name, missing=True) is None and self.engine.inspect(self.failed_name, missing=True) is None, "回退名称在验收后被占用")
        payload = copy.deepcopy(self.plan["payload"])
        payload["Labels"]["goodhr.tutorial_hls_owner"] = self.release_id
        self.state["status"] = "activating"
        self.record("activation_started")
        try:
            source_check(self.commit)
            self.protected()
            original_equal(self.engine.inspect(NAME), self.plan["original"], started=True)
            self.engine.stop(OLD_ID)
            self.record("original_stopped")
            self.engine.rename(OLD_ID, self.backup_name)
            self.record("original_retained")
            identifier = self.create_owned(NAME, payload, "activated")
            verify_runtime(self.engine.inspect(identifier), self.plan["original"], payload, False)
            self.engine.start(identifier)
            verify_runtime(self.engine.inspect(identifier), self.plan["original"], payload, True)
            self.health()
            self.protected()
            self.state["status"] = "activated"
            self.record("activation_succeeded")
        except BaseException:
            try:
                self.restore()
            except BaseException:
                self.state["status"] = "rollback_incomplete"
                self.record("automatic_rollback_incomplete")
                raise CutoverError("前端切换失败且自动回退未完整通过，请核对私有记录") from None
            raise CutoverError("前端切换失败，精确原容器已恢复") from None

    def rollback(self):
        """rollback 在持锁状态下恢复本次保存的原前端，始终保留数据库及其他服务。"""
        self.load()
        require(self.state["status"] in ("activated", "activating", "rolled_back", "rollback_incomplete"), "当前阶段不允许执行回退")
        self.protected()
        self.restore()

    def cleanup(self):
        """cleanup 仅删除登记的本轮演练或失败候选，永不删除运行中的正式前端、原回退容器或镜像。"""
        self.load()
        self.protected()
        for identifier, entry in self.state["owned"].items():
            actual = self.engine.inspect(identifier, missing=True)
            if not actual or actual["Name"].lstrip("/") == NAME:
                continue
            require(identifier != OLD_ID and identifier not in {row["Id"] for row in self.plan["baseline"]}, "清理对象属于原现场")
            require(actual["Name"].lstrip("/") == entry["name"] and actual["Image"] == entry["image"] and (actual["Config"].get("Labels") or {}).get("goodhr.tutorial_hls_owner") == self.release_id, "候选身份发生漂移，清理停止")
            require(entry["kind"] in ("candidate", "rollback-check", "activated") and (entry["kind"] != "activated" or entry["name"] == self.failed_name), "清理对象不在本次临时范围")
            if actual["State"]["Running"]:
                self.engine.stop(identifier)
            self.engine.request("DELETE", self.engine.prefix + "/containers/" + identifier + "?v=0&force=0")
            require(self.engine.inspect(identifier, missing=True) is None, "候选删除结果不明确")
            entry["removed"] = True
            self.record("exact_candidate_removed")
        self.protected()
        self.record("cleanup_completed")


def main():
    """main 按阶段执行本次固定主机的前端操作，仅输出非敏感完成摘要。"""
    parser = argparse.ArgumentParser(description="2026-10-18教程播放器专用前端发布，不操作后端、数据库和Redis")
    parser.add_argument("stage", choices=("prepare", "rehearse", "activate", "rollback", "cleanup"))
    parser.add_argument("--release-dir", required=True)
    parser.add_argument("--image", required=True)
    parser.add_argument("--commit", required=True)
    args = parser.parse_args()

    def interrupted(signum, frame):
        """interrupted 将终止信号转为受控异常，使已经开始的前端切换执行自动回退。"""
        raise CutoverError("本次前端发布收到中断信号")

    for signum in SIGNALS:
        signal.signal(signum, interrupted)
    os.umask(0o077)
    host_check()
    with locked():
        release = Release(args.release_dir, args.image, args.commit, DockerEngine())
        getattr(release, args.stage)()
    print(json.dumps({"success": True, "stage": args.stage, "release_id": Path(args.release_dir).name}, ensure_ascii=False))


if __name__ == "__main__":
    try:
        main()
    except (Exception, KeyboardInterrupt) as error:
        print("本次前端发布未完成：" + (str(error) if isinstance(error, CutoverError) else "请核对私有阶段材料"), file=sys.stderr)
        sys.exit(1)
