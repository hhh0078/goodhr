"""文件作用：让宝塔定时任务准备正式候选及隔离验收；等待显式激活，失败保留旧版并自动重试。"""

import argparse
import datetime as dt
import json
import os
import re
from pathlib import Path
import shutil
import signal
import socket
import subprocess
import sys
import time
import urllib.request

from release_activate import COMMIT, TAG, DockerEngine, save_json
from release_cutover import CutoverError, require
from release_rehearse import IDENTIFIER, command, dsn_database
from release_retention import digest_file
import release_hygiene as hygiene


ROLES = ("backend", "frontend")
RELEASE_ROOT = Path("/opt/goodhr-release")
STATE_ROOT = RELEASE_ROOT / "auto-deploy"
CLOUD_PATHS = ("cloud/backend", "cloud/frontend-next", "docker-compose.build.yml", "environments/production.env")


def cloud_changes(source, containers, commit):
    """只比较实际运行版本与目标的云端输入，文档和备份脚本不触发镜像构建。"""
    labels = [item.get("Config", {}).get("Labels") or {} for item in containers.values()]
    previous = {row.get("goodhr.source_commit") for row in labels}
    require(len(previous) == 1 and COMMIT.fullmatch(next(iter(previous)) or ""), "正式前后端源码身份不一致，停止更新")
    deployed = next(iter(previous))
    require(current_release(containers, deployed), "正式前后端不是完整运行组合，停止更新")
    changed = command(["git", "-C", str(source), "diff", "--name-only", deployed, commit, "--", *CLOUD_PATHS])
    return deployed, bool(changed.strip())


def current_release(containers, commit):
    """current_release 必须两个正式容器均运行同一目标提交，不能把拉取过代码当作发布成功。"""
    labels = {role: (item.get("Config", {}).get("Labels") or {}) for role, item in containers.items()}
    return set(containers) == set(ROLES) and bool(labels["backend"].get("goodhr.release.id")) and \
        labels["backend"].get("goodhr.release.id") == labels["frontend"].get("goodhr.release.id") and all(
        item.get("State", {}).get("Running") is True
        and labels[role].get("goodhr.source_commit") == commit
        and labels[role].get("goodhr.release_kind") == role for role, item in containers.items())


def pending_migrations(source, ledger):
    """pending_migrations 自动发现新迁移，不要求人工登记；缺失账本和异常文件名仍停止，内容留给镜像隔离验收。"""
    require(ledger and len(ledger) == len(set(ledger)) and set(ledger) <= set(source), "源码与生产迁移账本不一致")
    pending = set(source) - set(ledger)
    require(all(re.fullmatch(r"[0-9]{4}_[a-z0-9_]+\.sql", name) for name in pending), "新增迁移文件名不规范")
    return sorted(pending)


def cleanup_rehearsal(engine, release):
    """cleanup_rehearsal 核对本轮计划SHA和实际停止对象后精确删除临时容器、空网络，保留所有备份与恢复数据。"""
    directory = release / "rehearsal"
    journal_path = directory / "journal.json"
    if not journal_path.exists():
        return
    journal = json.loads(journal_path.read_text())
    if "network" not in journal:
        return
    record = journal.get("cleanup_plan") or {}
    plan_path = directory / "cleanup-plan.json"
    require(record.get("path") == str(plan_path) and record.get("sha256") == digest_file(plan_path),
            "本轮隔离对象清理计划缺失或SHA不符")
    plan = json.loads(plan_path.read_text())
    require(plan.get("host") == socket.gethostname() and plan.get("release_id") == release.name
            and plan.get("delete_files") is False and plan.get("delete_volumes") is False,
            "隔离清理计划身份或边界不符")
    items = []
    for row in plan["containers"]:
        value = engine.inspect(row["id"])
        require(value["Id"] == row["id"] and value["Name"] == row["name"] and value["Image"] == row["image"]
                and value["State"]["Running"] is False
                and (value["Config"].get("Labels") or {}).get("goodhr.rehearsal.id") == release.name,
                "本轮隔离对象状态发生变化，停止清理")
        items.append(row["id"])
    network = engine.network(plan["network"]["name"])
    require(network["Id"] == plan["network"]["id"] and network.get("Internal") is True
            and (network.get("Labels") or {}).get("goodhr.rehearsal.id") == release.name
            and set(network.get("Containers") or {}) <= set(items), "隔离网络身份或端点不符")
    for identifier in items:
        command(["docker", "container", "rm", identifier])
    require(not engine.network(plan["network"]["name"]).get("Containers"), "本轮网络仍有连接，停止删除")
    command(["docker", "network", "rm", network["Id"]])
    save_json(directory / "automatic-cleanup.json", {"success": True, "plan_sha256": record["sha256"],
              "containers": items, "network": network["Id"], "files_preserved": True})


def cleanup_restore_cache(release):
    """cleanup_restore_cache 仅回收本自动发布已验收且已移除容器的恢复副本，经原manifest和固定SHA门禁保留原始dump。"""
    require(release.parent == RELEASE_ROOT and release.resolve() == release and TAG.fullmatch(release.name),
            "自动验收缓存路径越界")
    pg = release / "rehearsal/pgdata"
    evidence_path = release / "isolated-validation.json"
    if not pg.exists() or not evidence_path.exists():
        return
    evidence = json.loads(evidence_path.read_text())
    journal = json.loads((release / "rehearsal/journal.json").read_text())
    require(evidence.get("stage") == "isolated_validation" and evidence.get("host") == socket.gethostname()
            and all(evidence.get(key) is True for key in ("success", "legacy_data_preserved", "rollback_verified"))
            and journal.get("success") is True and evidence.get("source_commit") == journal.get("source_commit")
            and evidence.get("release_id") == journal.get("release_id") == release.name
            and release.name.startswith("auto-" + evidence["source_commit"][:12] + "-"),
            "恢复副本未完成本轮验收，保留排错现场")
    cleanup = json.loads((release / "rehearsal/automatic-cleanup.json").read_text())
    require(cleanup.get("success") is True and cleanup.get("plan_sha256") == journal["cleanup_plan"]["sha256"],
            "本轮临时容器与网络尚未完成精确收尾")
    pg_ids = [value for name, value in journal["created"].items() if name.endswith("-pg")]
    require(len(pg_ids) == 1 and pg_ids[0] in cleanup.get("containers", []), "本轮恢复容器身份或收尾记录不明确")
    dump = release / "rehearsal/database.dump"
    resource = {"path": "rehearsal/pgdata", "stopped": True, "container_ids": pg_ids,
                "backup": {"path": "rehearsal/database.dump", "sha256": digest_file(dump)},
                "rehearsal": {"passed": True, "report_path": "isolated-validation.json",
                              "report_sha256": digest_file(evidence_path)}}
    manifest_path = release / hygiene.MANIFEST_NAME
    manifest = {"schema_version": 1, "host": socket.gethostname(), "release_dir": str(release),
                "status": "succeeded", "completed_stage": "isolated_validation", "production_activation_claimed": False,
                "completed_workdirs": [], "isolated_postgres": [resource]}
    if manifest_path.exists():
        old = json.loads(manifest_path.read_text())
        require(old.get("host") == socket.gethostname() and old.get("release_dir") == str(release),
                "既有缓存manifest身份不符")
        save_json(release / ("cache-manifest-before-" + str(time.time_ns()) + ".json"), old, exclusive=True)
        manifest["completed_workdirs"] = old.get("completed_workdirs", [])
        require(not old.get("isolated_postgres") or old["isolated_postgres"] == [resource],
                "既有manifest包含其他恢复资源，停止合并")
    save_json(manifest_path, manifest)
    plan_path = release / ("isolated-cache-plan-" + str(time.time_ns()) + ".json")
    hygiene.write_json(plan_path, hygiene.create_plan(release))
    result = hygiene.apply_plan(plan_path, hygiene.file_sha(plan_path))
    print(json.dumps({"已验收恢复缓存收尾": result, "原始备份保留": str(dump)}, ensure_ascii=False), flush=True)


class AutomaticRelease:
    """只编排既有正式发布入口，不新建一套应用切换、数据库备份或镜像回收实现。"""

    def __init__(self, source, engine=None, state_root=STATE_ROOT):
        """__init__ 绑定源码与状态目录，构造时不构建、不接触数据库、不切换容器。"""
        self.source = Path(source)
        self.engine = engine or DockerEngine()
        self.state_root = Path(state_root)
        self.state_path = self.state_root / "status.json"
        self.state = {}
        self.release = None

    def update(self, stage, **values):
        """update 持久化真实阶段和精确版本，日志只包含状态、摘要和路径，不包含环境值。"""
        self.state.update(values, stage=stage, updated_at=dt.datetime.now(dt.timezone.utc).isoformat())
        save_json(self.state_path, self.state)
        print(json.dumps({"自动部署": stage, **values}, ensure_ascii=False), flush=True)

    def live(self):
        """live 只定位正式项目的固定两个名称，不按模糊名称重建或重启其他GoodHR环境。"""
        return {role: self.engine.inspect("goodhr-production-" + role + "-1") for role in ROLES}

    def check_target(self, containers):
        """check_target 交叉检查生效域名、实际5175映射和8086监听，防止部署到另一套项目。"""
        conf = Path("/www/server/panel/vhost/nginx/goodhr.58it.cn.conf").read_text()
        require("proxy_pass http://127.0.0.1:5175;" in conf
                and "proxy_pass http://127.0.0.1:8086/api/;" in conf, "正式域名代理目标发生变化，请先核对站点")
        front, back = containers["frontend"], containers["backend"]
        require(all(item["State"]["Running"] for item in containers.values()), "正式前后端不在运行，停止自动更新")
        require(front["HostConfig"].get("PortBindings", {}).get("3000/tcp") == [{"HostIp": "127.0.0.1", "HostPort": "5175"}],
                "正式前端实际端口不符")
        values = [row.split("=", 1)[1] for row in back["Config"].get("Env", []) if row.startswith("GOODHR_CLOUD_ADDR=")]
        require(back["HostConfig"].get("NetworkMode") == "host" and values and values[-1] == "127.0.0.1:8086",
                "正式后端实际监听不符")

    def invoke(self, arguments, environment):
        """invoke 将构建输出追加到定时任务日志，阶段失败就停止，不执行后面的生产切换。"""
        process = subprocess.Popen(["timeout", "--signal=TERM", "--kill-after=300", "3600"] + arguments,
                                   cwd=str(self.source), env=environment, start_new_session=True)
        try:
            result = process.wait()
        except BaseException:
            if process.poll() is None:
                process.send_signal(signal.SIGTERM)
                try:
                    process.wait(timeout=330)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGKILL)
                    process.wait()
            raise
        require(result == 0, "当前阶段返回失败，详见本轮部署日志")

    def preflight(self, containers):
        """preflight 从实际服务确认库名、PG身份和只读账本，核对预算及冻结迁移后才允许构建。"""
        self.check_target(containers)
        database = dsn_database(containers["backend"]["Config"].get("Env", []))
        postgres, redis = self.engine.inspect("postgres"), self.engine.inspect("redis")
        require(postgres["State"]["Running"] and redis["State"]["Running"], "共享数据库或Redis不在运行")
        users = [row.split("=", 1)[1] for row in postgres["Config"].get("Env", []) if row.startswith("POSTGRES_USER=")]
        user = users[-1] if users else "postgres"
        require(IDENTIFIER.fullmatch(user), "数据库备份角色不明确")
        ledger = command(["docker", "exec", postgres["Id"], "psql", "-XqAt", "-v", "ON_ERROR_STOP=1",
                          "-U", user, "-d", database, "-c", "SELECT filename FROM public.schema_migrations ORDER BY filename;"]).decode().splitlines()
        source = {path.name: path.read_bytes() for path in (self.source / "cloud/backend/db/migrations").glob("*.sql")
                  if not path.name.endswith(".down.sql")}
        pending = pending_migrations(source, ledger)
        require(shutil.disk_usage(RELEASE_ROOT).free >= 6 * 1024**3, "根盘可用空间不足，保留当前线上版本")
        self.reclaim_unused_images(containers, dict(os.environ))
        docker_root = command(["docker", "info", "--format", "{{.DockerRootDir}}"]).decode().strip()
        if shutil.disk_usage(docker_root).free < 3 * 1024**3:
            maintenance = self.state_root / ("cache-maintenance-" + str(time.time_ns()))
            maintenance.mkdir(mode=0o700)
            self.reclaim_build_cache(containers, {}, dict(os.environ), maintenance)
        for path, minimum in ((RELEASE_ROOT, 6), (Path(docker_root), 3)):
            require(shutil.disk_usage(path).free >= minimum * 1024**3,
                    "发布可用空间不足：" + str(path) + "，保留当前线上版本")
        return database, postgres, user, redis, pending

    def reclaim_unused_images(self, containers, environment):
        """新构建前按真实上线组合的兼容登记清理闲置旧候选，全部容器及其镜像始终保留。"""
        labels = [item["Config"].get("Labels") or {} for item in containers.values()]
        release_id = labels[0].get("goodhr.release.id", "")
        require(TAG.fullmatch(release_id) and all(row.get("goodhr.release.id") == release_id for row in labels),
                "当前完整版本编号不一致，停止闲置镜像回收")
        registry = RELEASE_ROOT / release_id / "retention-registry.json"
        require(registry.is_file() and registry.resolve() == registry, "缺少当前真实版本兼容登记，停止回收")
        directory = self.state_root / ("image-maintenance-" + str(time.time_ns()))
        directory.mkdir(mode=0o700)
        plan = directory / "plan.json"
        self.update("检查闲置业务镜像")
        self.invoke(["python3", "scripts/release_retention.py", "plan", "--images-only", "--registry", str(registry), "--out", str(plan)], environment)
        value = json.loads(plan.read_text())
        snapshot = value.get("snapshot", {})
        referenced = {row["Image"] for row in snapshot.get("containers", [])}
        require(value.get("images_only") is True and not value.get("containers_to_remove")
                and {row["Id"] for row in snapshot.get("containers", [])} <= set(value.get("protected_container_ids", []))
                and referenced | {row["Image"] for row in containers.values()} <= set(value.get("protected_image_ids", [])),
                "闲置镜像清理不能影响任何运行或停止容器")
        if value.get("images_to_remove"):
            self.invoke(["python3", "scripts/release_retention.py", "apply", "--plan", str(plan), "--sha256", digest_file(plan)], environment)

    def verify_live(self, containers):
        """verify_live 在隔离验收完成后再次核对原两容器，避免把别的发布结果混进当前切换。"""
        actual = self.live()
        self.check_target(actual)
        require(all(actual[role]["Id"] == containers[role]["Id"]
                    and actual[role]["Image"] == containers[role]["Image"] for role in ROLES), "验收期间正式版本发生变化，重新检查")

    def execute(self, commit, containers):
        """execute 按不可变标签构建、完整隔离与回退验收和精确收尾，等待显式激活。"""
        previous = self.state.get("previous_release_id")
        if previous:
            require(TAG.fullmatch(previous), "上轮自动发布编号异常")
            previous_dir = RELEASE_ROOT / previous
            if (previous_dir / "rehearsal/journal.json").exists():
                if not (previous_dir / "rehearsal/automatic-cleanup.json").exists():
                    cleanup_rehearsal(self.engine, previous_dir)
                cleanup_restore_cache(previous_dir)
        database, postgres, user, redis, pending = self.preflight(containers)
        tag = "auto-" + commit[:12] + "-" + dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
        self.release = RELEASE_ROOT / tag
        environment = dict(os.environ, GOODHR_SOURCE_COMMIT=commit, GOODHR_RELEASE_IMAGE_TAG=tag,
                           GOODHR_DATABASE=database, GOODHR_VERIFIED_RELEASE_EVIDENCE=str(self.release / "isolated-validation.json"),
                           GOODHR_RETENTION_REGISTRY=str(self.release / "retention-registry.json"))
        environment["DOCKER_BUILDKIT"] = "1"
        self.update("构建候选", release_id=tag, pending_migration_count=len(pending))
        self.invoke(["bash", "scripts/deploy-build.sh", "--build-only"], environment)
        images = {role: command(["docker", "image", "inspect", "--format", "{{.Id}}",
                                "goodhr-production-" + role + ":" + tag]).decode().strip() for role in ROLES}
        self.update("隔离验收", candidate_images=images)
        arguments = ["python3", "scripts/release_rehearse_auto.py", "run"]
        options = {"host": socket.gethostname(), "release-dir": str(self.release), "release-id": tag,
                   "source-root": str(self.source), "source-commit": commit, "database": database,
                   "postgres-container": "postgres", "postgres-container-id": postgres["Id"], "postgres-user": user,
                   "redis-image": redis["Image"], **{role + "-container-id": containers[role]["Id"] for role in ROLES},
                   **{role + "-image": images[role] for role in ROLES}}
        for name, value in options.items():
            arguments.extend(["--" + name, value])
        try:
            self.invoke(arguments, environment)
            evidence = json.loads((self.release / "isolated-validation.json").read_text())
            require(evidence.get("success") is True and evidence.get("source_commit") == commit
                    and all(evidence.get(role + "_image") == images[role] for role in ROLES), "隔离证据未绑定本次候选")
            self.reclaim_build_cache(containers, images, environment)
        finally:
            cleanup_rehearsal(self.engine, self.release)
            cleanup_restore_cache(self.release)
        self.verify_live(containers)
        self.check_health()
        self.update("候选已就绪，等待激活", prepared_commit=commit, candidate_images=images, retry_after=0)

    def reclaim_build_cache(self, containers, images, environment, plan_directory=None):
        """reclaim_build_cache 空间低时使用当前真实版本的旧账本兼容登记精确回收，候选由本轮停止容器保护。"""
        docker_root = command(["docker", "info", "--format", "{{.DockerRootDir}}"]).decode().strip()
        if shutil.disk_usage(docker_root).free >= 3 * 1024**3:
            return
        labels = [item["Config"].get("Labels") or {} for item in containers.values()]
        release_id = labels[0].get("goodhr.release.id", "")
        require(TAG.fullmatch(release_id) and all(row.get("goodhr.release.id") == release_id for row in labels),
                "当前完整版本编号不一致，无法自动回收缓存")
        registry = RELEASE_ROOT / release_id / "retention-registry.json"
        require(registry.is_file() and registry.resolve() == registry, "缺少当前真实版本的兼容登记，停止回收")
        for drop in (False, True):
            self.update("回收已过期构建缓存", drop_completed_build_cache=drop)
            plan = (plan_directory or self.release) / ("preactivation-retention-plan-drop.json" if drop else "preactivation-retention-plan.json")
            arguments = ["python3", "scripts/release_retention.py", "plan", "--cache-only", "--registry", str(registry), "--out", str(plan)]
            if drop:
                arguments.append("--drop-build-cache")
            self.invoke(arguments, environment)
            value = json.loads(plan.read_text())
            require(set(images.values()) <= set(value.get("protected_image_ids", [])), "新候选未被本轮隔离容器保护，停止回收")
            require(value.get("cache_only") is True and not value.get("containers_to_remove"), "空间维护不能删除应用容器")
            self.invoke(["python3", "scripts/release_retention.py", "apply", "--plan", str(plan),
                         "--sha256", digest_file(plan)], environment)
            if shutil.disk_usage(docker_root).free >= 3 * 1024**3:
                return
        raise CutoverError("已回收允许删除的构建缓存，但空间仍不足；所有应用镜像和容器继续保留")

    def check_health(self):
        """check_health 实际检查正式前后端及公开域名，仅成功返回才记为已上线。"""
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        for url in ("http://127.0.0.1:8086/health", "http://127.0.0.1:5175/", "https://goodhr.58it.cn/"):
            with opener.open(url, timeout=15) as response:
                require(response.status == 200, "更新后的健康检查尚未通过")

    def run(self):
        """run 比较真实线上提交；失败延迟重试，同一Git提交也会重试，成功后不重复构建。"""
        require(self.source.resolve() == self.source and self.source.name == "goodhr5", "自动发布源码路径不明确")
        self.state_root.mkdir(mode=0o700, parents=True, exist_ok=True)
        require(self.state_root.resolve() == self.state_root and not self.state_path.is_symlink(), "自动发布状态路径不安全")
        if self.state_path.exists():
            self.state = json.loads(self.state_path.read_text())
        review = self.state_root / "pending-review.json"
        if review.exists():
            require(not review.is_symlink(), "验收保留记录路径异常")
            record = json.loads(review.read_text())
            require(record.get("schema_version") == 1 and record.get("host") == socket.gethostname()
                    and record.get("awaiting_user_confirmation") is True, "验收保留记录异常，停止自动发布")
            if self.state.get("stage") != "等待用户确认新容器":
                self.update("等待用户确认新容器", review_record=str(review))
            return
        commit = command(["git", "-C", str(self.source), "rev-parse", "HEAD"]).decode().strip()
        require(COMMIT.fullmatch(commit) and not command(["git", "-C", str(self.source), "status", "--porcelain", "--untracked-files=normal"]).strip(),
                "源码有改动或提交不完整，停止自动更新")
        containers = self.live()
        self.check_target(containers)
        if current_release(containers, commit):
            if self.state.get("stage") == "已上线，收尾待核查":
                self.check_health()
                print("线上版本已更新，原收尾告警仍保留，请查看本轮发布材料。", flush=True)
                return
            if self.state.get("deployed_commit") != commit or self.state.get("stage") != "已上线":
                self.check_health()
                self.update("已上线", source_commit=commit, deployed_commit=commit, retry_after=0)
            return
        deployed, changed = cloud_changes(self.source, containers, commit)
        if not changed:
            self.check_health()
            if self.state.get("source_commit") != commit or self.state.get("stage") != "云端无改动，跳过构建":
                self.update("云端无改动，跳过构建", source_commit=commit, deployed_commit=deployed, retry_after=0)
            return
        prepared = self.state.get("prepared_commit") or ""
        if isinstance(prepared, str) and COMMIT.fullmatch(prepared) and not command([
                "git", "-C", str(self.source), "diff", "--name-only", prepared, commit, "--", *CLOUD_PATHS]).strip():
            require(TAG.fullmatch(self.state.get("release_id") or ""), "既有候选编号缺失，停止重复构建")
            evidence = json.loads((RELEASE_ROOT / self.state["release_id"] / "isolated-validation.json").read_text())
            require(evidence.get("success") is True and evidence.get("source_commit") == prepared
                    and evidence.get("host") == socket.gethostname()
                    and all(evidence.get(role + "_image") == self.state.get("candidate_images", {}).get(role)
                            == command(["docker", "image", "inspect", "--format", "{{.Id}}", "goodhr-production-" + role + ":" + self.state["release_id"]]).decode().strip()
                            for role in ROLES), "既有候选身份或验收证据变化，停止重复构建")
            self.check_health()
            print("已有同一云端内容的验收候选，等待按原标签激活；不重复构建。", flush=True)
            return
        if self.state.get("source_commit") == commit and self.state.get("retry_after", 0) > time.time():
            print("上轮未通过，等待自动重试；正式站点保留旧版。", flush=True)
            return
        previous = self.state.get("release_id") or self.state.get("previous_release_id")
        self.state = {"source_commit": commit, "original_images": {role: containers[role]["Image"] for role in ROLES},
                      "previous_release_id": previous}
        try:
            self.update("发布前检查")
            self.execute(commit, containers)
        except Exception:
            if current_release(self.live(), commit):
                self.check_health()
                self.update("已上线，收尾待核查", deployed_commit=commit, retry_after=0)
            else:
                self.update("本轮未通过，五分钟后重试", retry_after=int(time.time()) + 300,
                            failed_stage=self.state.get("stage"))
            raise


def main():
    """main 由已持定时任务锁的auto_deploy.sh调用，日志保留明确失败阶段，不输出私密配置。"""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-root", required=True)
    args = parser.parse_args()

    def interrupted(signum, frame):
        """interrupted 将退出转成受控取消，先让本轮子进程、隔离对象及激活恢复完成。"""
        for value in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
            signal.signal(value, signal.SIG_IGN)
        raise CutoverError("本轮自动发布收到取消信号")

    for value in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
        signal.signal(value, interrupted)
    try:
        AutomaticRelease(args.source_root).run()
        return 0
    except (CutoverError, OSError, ValueError, subprocess.SubprocessError) as error:
        print(json.dumps({"自动部署失败": str(error)}, ensure_ascii=False), file=sys.stderr, flush=True)
        return 1


if __name__ == "__main__":
    sys.exit(main())
