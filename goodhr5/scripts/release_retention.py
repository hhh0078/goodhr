"""文件作用：登记兼容回退证据并生成、复核、执行精确Docker回收清单；保护运行服务、其他项目和原始备份，不执行全局prune。"""

import argparse
from contextlib import contextmanager
import datetime as dt
import hashlib
import importlib.util
import json
import os
from pathlib import Path, PurePosixPath
import re
import socket
import stat
import sys


SPEC = importlib.util.spec_from_file_location("goodhr_release_guard", Path(__file__).with_name("release_guard.py"))
guard = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(guard)
require = guard.require
GuardError = guard.GuardError
POLICY_PATH = Path(__file__).with_name("release-policy.json")
REGISTRY_PATH = "/opt/goodhr-release/retention-registry.json"
ROLES = ("backend", "frontend")
BUILD_STAGES = ("backend-builder", "frontend-builder", "frontend-dependencies")
SHA = re.compile(r"[0-9a-f]{64}\Z")
CONTAINER_ID = re.compile(r"[0-9a-f]{64}\Z")
IMAGE_ID = guard.DIGEST
IDENTIFIER = re.compile(r"[A-Za-z_][A-Za-z0-9_]*\Z")
LOCK_PATHS = (
    Path("/opt/goodhr-release/.backend-deploy.lock"),
    Path("/opt/goodhr-release/.frontend-deploy.lock"),
    Path("/www/wwwroot/goodhr-production/backups/.goodhr-container-deploy.lock"),
)


def digest_bytes(value):
    """digest_bytes 计算原始字节摘要，避免换行或JSON重排改变批准对象。"""
    return hashlib.sha256(value).hexdigest()


def absolute_path(value):
    """absolute_path 同时识别本机及Linux绝对路径，供Windows离线审阅服务器证据，实际读取仍核对原文件SHA。"""
    return Path(value).is_absolute() or PurePosixPath(str(value)).is_absolute()


def digest_file(path):
    """digest_file 分块核对证据文件，不读取或打印其中的业务正文。"""
    result = hashlib.sha256()
    with Path(path).open("rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            result.update(block)
    return result.hexdigest()


def read_json(path):
    """read_json 读取固定JSON输入并返回其字节摘要，输入异常时明确停止。"""
    try:
        data = Path(path).read_bytes()
        value = json.loads(data.decode("utf-8-sig"))
        require(isinstance(value, dict), "JSON输入必须是对象")
        return value, digest_bytes(data)
    except (OSError, UnicodeError, ValueError):
        raise GuardError("JSON输入无法读取或格式异常：" + str(path)) from None


def save_json(path, value):
    """save_json 原子保存权限0600的计划和进度，不覆盖已有临时文件。"""
    path = Path(path)
    temporary = path.with_name(path.name + ".tmp")
    with temporary.open("x", encoding="utf-8") as output:
        os.chmod(temporary, 0o600)
        json.dump(value, output, ensure_ascii=False, indent=2)
        output.flush()
        os.fsync(output.fileno())
    temporary.replace(path)


def policy_values(policy):
    """policy_values 限定每端最多两个历史回退且至少保留一个，运行基础始终保留。"""
    require(policy.get("schema_version") == 1 and policy.get("rollback_keep_per_service") == 2 and policy.get("minimum_compatible_rollback_per_service") == 1, "回收策略必须明确每端最多2个、至少1个兼容历史镜像")
    require(policy.get("delete_original_database_backups") is False, "策略必须禁止删除原始数据库备份")
    runtimes = policy.get("trusted_runtime_images")
    require(isinstance(runtimes, dict) and all(isinstance(runtimes.get(role), list) and runtimes[role] and all(isinstance(ref, str) and IMAGE_ID.fullmatch(ref) for ref in runtimes[role]) for role in ROLES), "策略缺少可信运行基础")
    return set(runtimes["backend"] + runtimes["frontend"])


def business_ref(reference):
    """business_ref 只认本项目两个精确仓库，外部仓库或其他项目标签一律保护。"""
    return bool(re.fullmatch(r"goodhr-production-(backend|frontend)(?::[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}|@sha256:[0-9a-f]{64})", reference))


def legacy_source_ref(reference, role):
    """legacy_source_ref 仅接受已调查到的本项目角色仓库格式，实际例外仍须逐image逐ref批准。"""
    standard = r"goodhr-production-" + role + r":[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}"
    backup = r"goodhr-release-backup:goodhr-production-" + role + r"-1-[0-9]{8}-[0-9]{6}"
    resume = r"goodhr-resume-20261010-" + role + r":latest"
    return bool(re.fullmatch("(?:" + standard + "|" + backup + "|" + resume + ")", reference))


def legacy_entries(approval, snapshot, evidence_hash=digest_file, evidence_reader=None):
    """legacy_entries 用固定来源快照核验历史容器精确三元，不补写标签、不放宽默认目标范围。"""
    if approval is None:
        return [], {}
    require(isinstance(approval, dict) and approval.get("schema_version") == 1 and approval.get("host") == snapshot["host"], "历史批准清单版本或服务器不匹配")
    path = approval.get("source_snapshot_path")
    expected_sha = approval.get("source_snapshot_sha256")
    require(isinstance(path, str) and absolute_path(path) and isinstance(expected_sha, str) and SHA.fullmatch(expected_sha), "历史来源快照路径或摘要异常")
    require(evidence_hash(Path(path)) == expected_sha, "历史来源快照SHA不一致")
    source = evidence_reader(Path(path)) if evidence_reader else read_json(path)[0]
    require(isinstance(source, dict) and source.get("host") == snapshot["host"] and isinstance(source.get("containers"), list) and isinstance(source.get("images"), dict), "历史来源快照缺少安全对象身份")
    rows = approval.get("containers")
    require(isinstance(rows, list) and rows, "历史批准清单必须逐项列出容器三元")
    actual = {row["Id"]: row for row in snapshot["containers"]}
    recorded = {row["Id"]: row for row in source["containers"]}
    selected, approved_refs, ids = [], {}, set()
    for row in rows:
        require(isinstance(row, dict) and set(row) == {"id", "name", "image", "role"}, "历史批准条目必须仅含id/name/image/role")
        key, name, image_id, role = row["id"], row["name"], row["image"], row["role"]
        require(isinstance(key, str) and CONTAINER_ID.fullmatch(key) and key not in ids and isinstance(image_id, str) and IMAGE_ID.fullmatch(image_id) and role in ROLES, "历史批准三元身份异常或重复")
        ids.add(key)
        require(isinstance(name, str) and re.fullmatch(r"/goodhr-production-" + role + r"(?:-1)?-(?:backup|before|failed)-[A-Za-z0-9_.-]+", name), "历史批准对象不是已调查的GoodHR应用备份名称")
        require(key in actual and key in recorded and image_id in snapshot["images"] and image_id in source["images"], "历史批准对象不存在于来源或实际快照")
        container, original = actual[key], recorded[key]
        for value in (container, original):
            require((value["Id"], value["Name"], value["Image"]) == (key, name, image_id), "历史容器ID/name/image三元变化")
            require(value["Running"] is False and value["Status"] in ("exited", "created", "dead"), "历史批准不能删除运行或状态不明容器")
            require(value.get("Role") in (None, "", role), "历史容器实际角色与批准角色冲突")
        image, source_image = snapshot["images"][image_id], source["images"][image_id]
        require(image.get("Role") in (None, "", role) and source_image.get("Role") in (None, "", role), "历史镜像角色属于其他服务")
        refs = image["RepoTags"] + image["RepoDigests"]
        original_refs = (source_image.get("RepoTags") or []) + (source_image.get("RepoDigests") or [])
        require(refs and sorted(refs) == sorted(original_refs) and all(legacy_source_ref(ref, role) for ref in refs), "历史镜像缺少精确GoodHR角色来源或存在外部引用")
        approved_refs[image_id] = sorted(refs)
        selected.append(container)
    return selected, approved_refs


def business_image(image):
    """business_image 以明确业务标签或本项目仓库判断旧应用成品。"""
    return bool((image.get("Release") or image.get("ReleaseId")) and image.get("Role") in ROLES) or any(business_ref(ref) for ref in image["RepoTags"] + image["RepoDigests"])


def build_image(image):
    """build_image 只识别本项目明确标记的三类构建阶段，未知构建缓存不进入回收候选。"""
    return image.get("Project") == "goodhr" and image.get("BuildStage") in BUILD_STAGES


def retained_build_leaves(images):
    """retained_build_leaves 每种阶段仅保留最新已完成缓存叶子，其完整祖先由统一闭包保护。"""
    retained = set()
    for stage in BUILD_STAGES:
        branches = {key for key, row in images.items() if build_image(row) and row["BuildStage"] == stage}
        parents = {images[key]["Parent"] for key in branches}
        finished = []
        for key in branches - parents:
            row = images[key]
            if row.get("BuildFinished") != "true":
                continue
            try:
                created = dt.datetime.fromisoformat(row["Created"].replace("Z", "+00:00"))
                require(created.utcoffset() is not None, "构建阶段创建时间缺少时区")
            except (ValueError, TypeError, KeyError):
                raise GuardError("构建阶段创建时间异常，禁止猜测缓存保留顺序") from None
            finished.append((created, key))
        if finished:
            retained.add(max(finished)[1])
    return retained


def target_container(container, image, known_names=()):
    """target_container 仅选择已停止的本项目应用备份和已知候选，禁止按宽泛goodhr前缀删除。"""
    if container["Running"] or container["Status"] not in ("exited", "created", "dead"):
        return False
    name = container["Name"].lstrip("/")
    for role in ROLES:
        if name == "goodhr-production-" + role + "-1":
            return False
        role_tag = any(ref.startswith("goodhr-production-" + role + ":") for ref in image["RepoTags"])
        labels_match = bool(container.get("Release") or container.get("ReleaseId")) and container.get("Role") == role
        backup = re.fullmatch(r"goodhr-production-" + role + r"-1-backup-[A-Za-z0-9_.-]+", name)
        candidate = re.fullmatch(r"goodhr-(?:hr|resume|finance|ai-recharge|admin-uniform)-" + role + r"-candidate-[A-Za-z0-9_.-]+", name)
        if backup and (labels_match or role_tag):
            return True
        if (candidate or name in known_names) and labels_match:
            return True
    return False


def ancestor_closure(seeds, images):
    """ancestor_closure 计算完整Parent祖先闭包，父项缺失或出现环时阻止回收。"""
    result = set()
    for seed in seeds:
        current, branch = seed, set()
        while current:
            require(current in images, "镜像父链缺项，无法完整保护祖先")
            require(current not in branch, "镜像父链出现循环")
            branch.add(current)
            result.add(current)
            current = images[current]["Parent"]
    return result


def deletion_order(candidates, images):
    """deletion_order 生成孩子先于父亲的顺序，不让Docker隐式决定额外删除范围。"""
    remaining, result = set(candidates), []
    while remaining:
        parents = {images[key]["Parent"] for key in remaining}
        leaves = sorted(remaining - parents)
        require(leaves, "候选镜像父链出现循环")
        result.extend(leaves)
        remaining.difference_update(leaves)
    return result


def verified_records(registry, host, evidence_hash=digest_file, evidence_reader=None):
    """verified_records 只接纳证据SHA、当前迁移指纹和显式兼容结论均一致的真实配对记录。"""
    require(registry.get("schema_version") == 1 and registry.get("host") == host, "发布登记版本或服务器身份不匹配")
    fingerprint = registry.get("current_migration_fingerprint", "")
    require(isinstance(fingerprint, str) and SHA.fullmatch(fingerprint), "当前迁移指纹必须是完整SHA256")
    records = registry.get("verified_releases")
    require(isinstance(records, list), "缺少兼容发布登记数组")
    accepted, excluded, ids = [], [], set()
    for row in records:
        require(isinstance(row, dict) and isinstance(row.get("id"), str) and re.fullmatch(r"[A-Za-z0-9_.-]+", row["id"]) and row["id"] not in ids, "兼容发布记录身份异常或重复")
        ids.add(row["id"])
        if row.get("compatible") is not True or row.get("migration_fingerprint") != fingerprint:
            excluded.append({"id": row["id"], "reason": "未验证兼容或迁移指纹不一致"})
            continue
        try:
            verified_at = dt.datetime.fromisoformat(row["verified_at"].replace("Z", "+00:00"))
            path = Path(row["evidence_path"])
            valid = verified_at.utcoffset() is not None and absolute_path(path) and SHA.fullmatch(row["evidence_sha256"]) and all(IMAGE_ID.fullmatch(row[role + "_image"]) for role in ROLES)
            require(valid, "兼容发布记录格式异常")
            require(evidence_hash(path) == row["evidence_sha256"], "兼容证据摘要不一致")
            proof = evidence_reader(path) if evidence_reader else read_json(path)[0]
            require(isinstance(proof, dict) and all(proof.get(field) is True for field in ("success", "legacy_data_preserved", "contract_verified")), "兼容证据未通过完整数据和接口契约验证")
            expected = {"host": host, "backend_image": row["backend_image"], "frontend_image": row["frontend_image"], "migration_fingerprint": fingerprint}
            require(all(proof.get(key) == value for key, value in expected.items()), "兼容证据没有绑定当前主机、镜像配对和迁移指纹")
        except (OSError, ValueError, TypeError, KeyError):
            excluded.append({"id": row["id"], "reason": "证据缺失、摘要不符或记录格式异常"})
            continue
        accepted.append(dict(row))
    accepted.sort(key=lambda row: (dt.datetime.fromisoformat(row["verified_at"].replace("Z", "+00:00")), row["id"]), reverse=True)
    return accepted, excluded


def select_rollbacks(records, current, images):
    """select_rollbacks 保留经过完整配对验证的历史镜像，每端至少一个、最多两个，绝不拼接虚假配对。"""
    valid = [row for row in records if all(row[role + "_image"] in images for role in ROLES)]
    selected, historical = [], {role: set() for role in ROLES}
    for role in ROLES:
        candidate = next((row for row in valid if row[role + "_image"] != current[role]), None)
        require(candidate is not None, "缺少与当前数据库兼容且有证据的" + role + "历史回退，禁止回收")
        if candidate not in selected:
            selected.append(candidate)
            for kind in ROLES:
                if candidate[kind + "_image"] != current[kind]:
                    historical[kind].add(candidate[kind + "_image"])
    for row in valid:
        additions = {role: historical[role] | ({row[role + "_image"]} if row[role + "_image"] != current[role] else set()) for role in ROLES}
        if row not in selected and all(len(values) <= 2 for values in additions.values()) and additions != historical:
            selected.append(row)
            historical = additions
    return selected, historical


def make_plan(snapshot, registry, policy, known_names=(), evidence_hash=digest_file, evidence_reader=None, legacy_allowlist=None, legacy_evidence_hash=digest_file, legacy_evidence_reader=None):
    """make_plan 由完整只读快照生成精确删除白名单，所有其他容器与外部镜像都进入保护闭包。"""
    runtimes = policy_values(policy)
    require(all(isinstance(name, str) and re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]+", name) for name in known_names), "额外容器白名单必须是精确简单名称")
    host, containers, images = snapshot["host"], snapshot["containers"], snapshot["images"]
    require(registry["host"] == host, "登记与快照服务器身份不一致")
    current = {}
    for role in ROLES:
        matches = [row for row in containers if row["Name"].lstrip("/") == "goodhr-production-" + role + "-1" and row["Running"]]
        require(len(matches) == 1, "当前正式" + role + "运行容器不唯一，禁止回收")
        current[role] = matches[0]["Image"]
    records, excluded = verified_records(registry, host, evidence_hash, evidence_reader)
    selected, historical = select_rollbacks(records, current, images)
    default_targets = [row for row in containers if target_container(row, images[row["Image"]], known_names)]
    legacy_targets, legacy_refs = legacy_entries(legacy_allowlist, snapshot, legacy_evidence_hash, legacy_evidence_reader)
    target_ids = {row["Id"] for row in default_targets + legacy_targets}
    rollback_images = {row[role + "_image"] for row in selected for role in ROLES}
    immutable = ancestor_closure(rollback_images | set(current.values()) | (runtimes & set(images)) | {row["Image"] for row in containers if row["Running"] or row["Id"] not in target_ids}, images)
    require(not any(row["Image"] in immutable for row in legacy_targets), "历史三元批准不能删除受保护回退、运行或其他服务所用镜像的容器")
    remove_containers = [row for row in containers if row["Id"] in target_ids]
    removed_ids = {row["Id"] for row in remove_containers}
    keep_containers = [row for row in containers if row["Id"] not in removed_ids]
    possible = ancestor_closure({row["Image"] for row in remove_containers}, images)
    possible.update(key for key, image in images.items() if business_image(image))
    builders = {key for key, image in images.items() if build_image(image)}
    possible.update(ancestor_closure(builders, images))
    retained_builds = retained_build_leaves(images)
    foreign = {key for key, image in images.items() if any(not business_ref(ref) and ref not in legacy_refs.get(key, []) for ref in image["RepoTags"] + image["RepoDigests"])}
    protected_seeds = {row["Image"] for row in keep_containers} | rollback_images | retained_builds | foreign | (runtimes & set(images)) | (set(images) - possible)
    protected = ancestor_closure(protected_seeds, images)
    removable = possible - protected
    actions = []
    for key in deletion_order(removable, images):
        image = images[key]
        require(all(business_ref(ref) or ref in legacy_refs.get(key, []) for ref in image["RepoTags"] + image["RepoDigests"]), "候选包含外部镜像引用")
        actions.append({"id": key, "refs": sorted(image["RepoTags"]) or [key]})
    return {"schema_version": 1, "host": host, "current_migration_fingerprint": registry["current_migration_fingerprint"], "known_container_names": sorted(set(known_names)), "legacy_allowlist": legacy_allowlist, "approved_legacy_image_refs": legacy_refs, "snapshot": snapshot, "current_images": current, "compatible_rollback_records": selected, "historical_images": {role: sorted(values) for role, values in historical.items()}, "retained_build_cache_leaf_ids": sorted(retained_builds), "excluded_registry_records": excluded, "protected_container_ids": sorted(row["Id"] for row in keep_containers), "protected_image_ids": sorted(protected), "containers_to_remove": sorted(remove_containers, key=lambda row: row["Id"]), "images_to_remove": actions}


def check_activation(registry, evidence, reader=None, policy=None):
    """check_activation 只读核验切换前真实账本和切换后兼容配对，不获取锁，供已持发布锁的调用方使用。"""
    reader = reader or DockerRetention()
    policy_values(policy if policy is not None else read_json(POLICY_PATH)[0])
    snapshot = reader.snapshot()
    require(isinstance(evidence, dict) and all(evidence.get(field) is True for field in ("success", "legacy_data_preserved", "rollback_verified")), "激活证据尚未通过数据保留和回退验证")
    require(evidence.get("host") == snapshot["host"] == registry.get("host"), "激活证据与服务器身份不一致")
    require(isinstance(evidence.get("source_commit"), str) and re.fullmatch(r"[0-9a-f]{40}", evidence["source_commit"]), "激活证据缺少完整源码提交")
    for field in ("before_migration_fingerprint", "after_migration_fingerprint"):
        require(isinstance(evidence.get(field), str) and SHA.fullmatch(evidence[field]), "激活证据迁移指纹异常")
    require(registry.get("current_migration_fingerprint") == evidence["after_migration_fingerprint"], "兼容登记没有对应切换后的迁移指纹")
    current = {role: evidence.get(role + "_image") for role in ROLES}
    require(all(isinstance(value, str) and IMAGE_ID.fullmatch(value) and value in snapshot["images"] for value in current.values()), "待激活镜像缺失或身份异常")
    probe_registry = dict(registry, current_migration_fingerprint=evidence["before_migration_fingerprint"])
    reader.migration_fingerprint(probe_registry, snapshot)
    records, _ = verified_records(registry, snapshot["host"])
    selected, _ = select_rollbacks(records, current, snapshot["images"])
    require(reader.snapshot() == snapshot, "激活检查期间Docker快照发生变化")
    return {"success": True, "host": snapshot["host"], "source_commit": evidence["source_commit"], "backend_image": current["backend"], "frontend_image": current["frontend"], "before_migration_fingerprint": evidence["before_migration_fingerprint"], "after_migration_fingerprint": evidence["after_migration_fingerprint"], "compatible_rollback_records": selected}


def safe_labels(fields):
    """safe_labels 生成nil安全的明确标签模板，正常无标签基础镜像也能完成只读快照。"""
    return ",".join('"' + field + '":{{if .Config.Labels}}{{json (index .Config.Labels "' + label + '")}}{{else}}null{{end}}' for field, label in fields)


class DockerRetention(guard.DockerReader):
    """只读取白名单Docker字段，执行时也只允许无force、无卷参数的精确删除。"""

    def inspect_batch(self, kind, identifiers, template):
        """inspect_batch 每批最多64个对象并核对输出ID恰好等于输入，减少CLI次数而不省略全局检查。"""
        require(kind in ("container", "image"), "批量读取对象类型异常")
        pattern = CONTAINER_ID if kind == "container" else IMAGE_ID
        require(all(isinstance(key, str) and pattern.fullmatch(key) for key in identifiers) and len(identifiers) == len(set(identifiers)), "批量读取输入身份异常或重复")
        result = []
        for start in range(0, len(identifiers), 64):
            batch = identifiers[start:start + 64]
            try:
                rows = [json.loads(line) for line in self.query([kind, "inspect", "--format", template] + batch).splitlines()]
                returned = [row["Id"] for row in rows]
                require(len(returned) == len(batch) and set(returned) == set(batch), "批量读取输出ID缺失、重复或出现额外对象")
            except (ValueError, TypeError, KeyError):
                raise GuardError("批量读取结果异常，已停止回收") from None
            result.extend(rows)
        return sorted(result, key=lambda row: row["Id"])

    def snapshot(self):
        """snapshot 记录全局容器和镜像安全字段，用于删除前及每步之后检查外部变化。"""
        ids = sorted(set(self.query(["container", "ls", "-aq", "--no-trunc"]).split()))
        common_labels = [("Release", "goodhr.release"), ("ReleaseId", "goodhr.release.id"), ("Role", "goodhr.release_kind")]
        container_template = '{"Id":{{json .Id}},"Name":{{json .Name}},"Image":{{json .Image}},"Running":{{json .State.Running}},"Status":{{json .State.Status}},"StartedAt":{{json .State.StartedAt}},"RestartCount":{{json .RestartCount}},' + safe_labels(common_labels) + '}'
        containers = self.inspect_batch("container", ids, container_template)
        image_ids = sorted(set(self.query(["image", "ls", "-aq", "--no-trunc"]).split()))
        image_labels = common_labels + [("Project", "goodhr.project"), ("BuildStage", "goodhr.build.stage"), ("BuildFinished", "goodhr.build.finished")]
        image_template = '{"Id":{{json .Id}},"Parent":{{json .Parent}},"Created":{{json .Created}},"RepoTags":{{json .RepoTags}},"RepoDigests":{{json .RepoDigests}},"Layers":{{json .RootFS.Layers}},' + safe_labels(image_labels) + '}'
        images = {row["Id"]: row for row in self.inspect_batch("image", image_ids, image_template)}
        for row in containers:
            require(CONTAINER_ID.fullmatch(row["Id"]) and IMAGE_ID.fullmatch(row["Image"]) and row["Image"] in images and type(row["Running"]) is bool and isinstance(row["Status"], str), "容器快照缺失或身份异常")
        for key, row in images.items():
            require(row["Id"] == key and IMAGE_ID.fullmatch(key) and (not row["Parent"] or IMAGE_ID.fullmatch(row["Parent"])), "镜像快照父链异常")
            for field in ("RepoTags", "RepoDigests", "Layers"):
                row[field] = row[field] or []
                require(isinstance(row[field], list) and all(isinstance(value, str) for value in row[field]), "镜像快照引用异常")
                if field != "Layers":
                    row[field].sort()
        require(ids == sorted(set(self.query(["container", "ls", "-aq", "--no-trunc"]).split())) and image_ids == sorted(set(self.query(["image", "ls", "-aq", "--no-trunc"]).split())), "读取快照时Docker对象发生变化")
        return {"host": socket.gethostname(), "containers": sorted(containers, key=lambda row: row["Id"]), "images": images}

    def migration_fingerprint(self, registry, snapshot):
        """migration_fingerprint 仅在核验过的运行postgres内读取迁移文件名，绝不读取密码或执行迁移。"""
        probe = registry.get("migration_probe", {})
        require(isinstance(probe, dict) and probe.get("container") == "postgres" and all(isinstance(probe.get(key), str) and IDENTIFIER.fullmatch(probe[key]) for key in ("database", "user")), "迁移探针必须是postgres容器及安全简单库名、用户名")
        pg = [row for row in snapshot["containers"] if row["Name"].lstrip("/") == probe["container"] and row["Running"]]
        require(len(pg) == 1, "迁移探针没有指向唯一运行postgres容器")
        rows = self.query(["exec", probe["container"], "psql", "-X", "-v", "ON_ERROR_STOP=1", "-At", "-U", probe["user"], "-d", probe["database"], "-c", "SELECT filename FROM schema_migrations ORDER BY filename;"]).splitlines()
        require(rows and len(rows) == len(set(rows)) and all(re.fullmatch(r"[A-Za-z0-9_.-]+\.sql", row) for row in rows), "迁移探针结果缺失或格式异常")
        value = digest_bytes(json.dumps(sorted(rows), ensure_ascii=False, separators=(",", ":")).encode("utf-8"))
        require(value == registry["current_migration_fingerprint"], "实际迁移账本已变化，兼容证据必须重新验证")
        return value

    def remove_container(self, key):
        """remove_container 只删除精确已停止容器，不使用force、卷或网络删除参数。"""
        require(CONTAINER_ID.fullmatch(key), "容器删除ID异常")
        self.query(["container", "rm", key])

    def remove_image_ref(self, reference, approved_legacy_refs=()):
        """remove_image_ref 逐个删除批准引用并禁止自动清理父镜像。"""
        require(business_ref(reference) or IMAGE_ID.fullmatch(reference) or reference in approved_legacy_refs, "镜像删除引用越界")
        self.query(["image", "rm", "--no-prune", reference])


@contextmanager
def deployment_locks():
    """deployment_locks 持有已存在的前后端及全局容器部署锁，拒绝创建新锁或链接路径。"""
    require(os.name == "posix", "Docker回收apply仅允许在已核验Linux主机执行")
    import fcntl
    handles = []
    try:
        for path in sorted(LOCK_PATHS):
            require(path.resolve() == path and path.is_file() and not path.is_symlink(), "既有部署锁缺失或路径异常")
            handle = os.open(path, os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC)
            handles.append(handle)
            info = os.fstat(handle)
            require(stat.S_ISREG(info.st_mode) and info.st_uid == 0, "部署锁必须是root普通文件")
            fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
        yield
    finally:
        for handle in reversed(handles):
            os.close(handle)


def expected_after_ref(snapshot, image_id, reference):
    """expected_after_ref 计算单次批准移除后的精确镜像状态，禁止顺带删除其他对象。"""
    row = snapshot["images"][image_id]
    if reference == image_id:
        del snapshot["images"][image_id]
        return
    row["RepoTags"].remove(reference)
    repositories = {tag.rsplit(":", 1)[0] for tag in row["RepoTags"]}
    row["RepoDigests"] = [value for value in row["RepoDigests"] if value.split("@", 1)[0] in repositories]
    if not row["RepoTags"] and not row["RepoDigests"]:
        del snapshot["images"][image_id]


def execute_plan(plan, registry, policy, reader, audit_path):
    """execute_plan 在持锁调用方内逐步重新检查全局状态，任一步失败即停止并保留精确进度。"""
    require(plan.get("simulation_only") is not True, "模拟回收计划不能执行，请按实际快照重新生成")
    audit = {"success": False, "completed": [], "attempting": None}
    expected = json.loads(json.dumps(plan["snapshot"]))
    try:
        require(reader.snapshot() == expected, "全局Docker快照已变化，必须重新生成计划")
        reader.migration_fingerprint(registry, expected)
        rebuilt = make_plan(expected, registry, policy, plan["known_container_names"], legacy_allowlist=plan.get("legacy_allowlist"))
        for key in ("current_images", "compatible_rollback_records", "historical_images", "retained_build_cache_leaf_ids", "approved_legacy_image_refs", "protected_container_ids", "protected_image_ids", "containers_to_remove", "images_to_remove"):
            require(rebuilt[key] == plan[key], "批准计划与当前安全规则或证据不一致")
        for row in plan["containers_to_remove"]:
            require(reader.snapshot() == expected, "删除前全局Docker状态变化，已停止")
            audit["attempting"] = {"container_id": row["Id"]}
            save_json(audit_path, audit)
            reader.remove_container(row["Id"])
            expected["containers"] = [value for value in expected["containers"] if value["Id"] != row["Id"]]
            require(reader.snapshot() == expected, "容器删除后出现计划外状态变化")
            audit["completed"].append(audit["attempting"])
            audit["attempting"] = None
            save_json(audit_path, audit)
        for row in plan["images_to_remove"]:
            for reference in row["refs"]:
                require(reader.snapshot() == expected, "镜像删除前全局Docker状态变化，已停止")
                audit["attempting"] = {"image_id": row["id"], "reference": reference}
                save_json(audit_path, audit)
                approved_refs = plan.get("approved_legacy_image_refs", {}).get(row["id"], [])
                if approved_refs:
                    reader.remove_image_ref(reference, approved_refs)
                else:
                    reader.remove_image_ref(reference)
                expected_after_ref(expected, row["id"], reference)
                require(reader.snapshot() == expected, "镜像删除后出现计划外状态变化")
                audit["completed"].append(audit["attempting"])
                audit["attempting"] = None
                save_json(audit_path, audit)
        reader.migration_fingerprint(registry, expected)
        audit.update(success=True, final_snapshot=expected)
        save_json(audit_path, audit)
        return audit
    except Exception as error:
        audit["error"] = str(error)
        save_json(audit_path, audit)
        raise


def main(argv=None):
    """main 生成可审查且可按固定SHA批准的持久计划，执行入口不自动获得发布或清理授权。"""
    parser = argparse.ArgumentParser(description="GoodHR精确Docker保留与回收")
    parser.add_argument("--policy", default=str(POLICY_PATH))
    commands = parser.add_subparsers(dest="operation", required=True)
    plan_parser = commands.add_parser("plan")
    plan_parser.add_argument("--registry", default=REGISTRY_PATH)
    plan_parser.add_argument("--out", required=True)
    plan_parser.add_argument("--container-name", action="append", default=[])
    plan_parser.add_argument("--legacy-allowlist")
    plan_parser.add_argument("--legacy-sha256")
    check_parser = commands.add_parser("check")
    check_parser.add_argument("--registry", default=REGISTRY_PATH)
    check_parser.add_argument("--evidence", required=True)
    apply_parser = commands.add_parser("apply")
    apply_parser.add_argument("--plan", required=True)
    apply_parser.add_argument("--sha256", required=True)
    args = parser.parse_args(argv)
    policy, policy_sha = read_json(args.policy)
    reader = DockerRetention()
    if args.operation == "check":
        registry, _ = read_json(args.registry)
        evidence, _ = read_json(args.evidence)
        print(json.dumps(check_activation(registry, evidence, reader, policy), ensure_ascii=False))
    elif args.operation == "plan":
        registry, registry_sha = read_json(args.registry)
        snapshot = reader.snapshot()
        reader.migration_fingerprint(registry, snapshot)
        legacy, legacy_input = None, None
        require(bool(args.legacy_allowlist) == bool(args.legacy_sha256), "历史三元清单路径与固定SHA必须同时提供")
        if args.legacy_allowlist:
            legacy, legacy_sha = read_json(args.legacy_allowlist)
            require(SHA.fullmatch(args.legacy_sha256) and legacy_sha == args.legacy_sha256, "历史三元批准文件SHA不匹配")
            legacy_input = {"path": str(Path(args.legacy_allowlist).resolve()), "sha256": legacy_sha}
        plan = make_plan(snapshot, registry, policy, args.container_name, legacy_allowlist=legacy)
        plan["legacy_allowlist_input"] = legacy_input
        plan.update(registry_path=str(Path(args.registry).resolve()), registry_sha256=registry_sha, policy_path=str(Path(args.policy).resolve()), policy_sha256=policy_sha)
        require(reader.snapshot() == snapshot, "计划生成期间Docker快照变化")
        require(not Path(args.out).exists(), "计划文件已存在，请使用新的精确文件名")
        save_json(args.out, plan)
        containers, images = len(plan["containers_to_remove"]), len(plan["images_to_remove"])
        print(json.dumps({"success": True, "plan": str(Path(args.out).resolve()), "sha256": digest_file(args.out), "targets": containers + images, "containers": containers, "images": images}))
    else:
        plan, plan_sha = read_json(args.plan)
        require(isinstance(args.sha256, str) and SHA.fullmatch(args.sha256) and plan_sha == args.sha256, "批准计划SHA不匹配")
        require(plan.get("simulation_only") is not True, "模拟回收计划不能执行，请按实际快照重新生成")
        require(plan.get("schema_version") == 1 and plan.get("host") == socket.gethostname(), "批准计划版本或执行服务器不匹配")
        with deployment_locks():
            registry, registry_sha = read_json(plan["registry_path"])
            policy, policy_sha = read_json(args.policy)
            require(registry_sha == plan["registry_sha256"] and str(Path(args.policy).resolve()) == plan["policy_path"] and policy_sha == plan["policy_sha256"], "发布登记或回收策略在批准后变化")
            if plan.get("legacy_allowlist") is not None:
                source = plan.get("legacy_allowlist_input")
                require(isinstance(source, dict), "批准计划缺少历史三元文件身份")
                legacy, legacy_sha = read_json(source["path"])
                require(legacy_sha == source["sha256"] and legacy == plan["legacy_allowlist"], "历史三元批准文件在计划后变化")
            audit_path = Path(args.plan).with_name(Path(args.plan).name + ".apply-" + dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%S%fZ") + ".json")
            result = execute_plan(plan, registry, policy, reader, audit_path)
            print(json.dumps({"success": result["success"], "completed_actions": len(result["completed"]), "audit": str(audit_path)}, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (GuardError, OSError, ValueError, TypeError, KeyError) as error:
        print(json.dumps({"success": False, "error": "精确回收已停止：" + str(error)}, ensure_ascii=False), file=sys.stderr)
        sys.exit(1)
