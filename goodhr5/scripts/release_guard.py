"""文件作用：校验发布Dockerfile和候选镜像来源，阻止继承旧业务镜像造成镜像层持续叠加；只执行Docker只读查询。"""

import argparse
import hashlib
import json
from pathlib import Path
import re
import shlex
import subprocess
import sys


SCRIPT_ROOT = Path(__file__).resolve().parent
POLICY_PATH = SCRIPT_ROOT / "release-policy.json"
DOCKERFILES = {
    "backend": SCRIPT_ROOT.parent / "cloud/backend/Dockerfile.release",
    "frontend": SCRIPT_ROOT.parent / "cloud/frontend-next/Dockerfile",
}
DIGEST = re.compile(r"sha256:[0-9a-f]{64}\Z")
OFFICIAL_BASES = {"backend": {"golang:1.24-alpine", "alpine:3.22"}, "frontend": {"node:22-alpine"}}
RUNNER_BASES = {"backend": {"alpine:3.22"}, "frontend": {"node:22-alpine"}}
DOCKERFILE_LABEL = "goodhr.dockerfile_sha256"


class GuardError(ValueError):
    """表示发布来源不符合规则或只读查询无法确认结果。"""


def require(condition, message):
    """require 在验证不成立时立即终止发布检查。"""
    if not condition:
        raise GuardError(message)


def load_policy(path=POLICY_PATH):
    """load_policy 读取经过人工核验的角色专属运行镜像白名单，缺失或异常时拒绝继续。"""
    try:
        value = json.loads(Path(path).read_text(encoding="utf-8-sig"))
        images = value["trusted_runtime_images"]
        require(isinstance(images, dict) and set(images) == set(DOCKERFILES), "发布策略必须分别声明backend和frontend运行镜像")
        for kind, refs in images.items():
            require(isinstance(refs, list) and refs and all(isinstance(ref, str) and DIGEST.fullmatch(ref) for ref in refs), "发布策略运行镜像必须为非空的完整sha256数组：" + kind)
            require(len(refs) == len(set(refs)), "发布策略运行镜像存在重复项：" + kind)
        require(not set(images["backend"]) & set(images["frontend"]), "发布策略不能把同一运行镜像分配给两个角色")
        return {kind: set(refs) for kind, refs in images.items()}
    except (OSError, ValueError, TypeError, KeyError) as error:
        if isinstance(error, GuardError):
            raise
        raise GuardError("发布策略缺失或格式异常：" + str(path)) from None


def instructions(text):
    """instructions 合并标准反斜杠续行，拒绝无法可靠解析的转义或heredoc写法。"""
    pending = ""
    for line in text.splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            require(not re.match(r"#\s*syntax\s*=", stripped, re.IGNORECASE), "发布Dockerfile禁止替换构建解析器")
            if re.match(r"#\s*escape\s*=", stripped, re.IGNORECASE):
                require(stripped.split("=", 1)[1].strip() == "\\", "Dockerfile只允许标准反斜杠转义")
            continue
        require("<<" not in stripped, "发布Dockerfile暂不支持heredoc，请使用可直接核验的指令")
        pending += stripped[:-1] + " " if stripped.endswith("\\") else stripped
        if stripped.endswith("\\"):
            continue
        yield pending
        pending = ""
    require(not pending, "Dockerfile续行没有结束")


def check_dockerfile(text, kind, policy):
    """check_dockerfile 只允许干净官方基础、已核验角色镜像或本文件前置stage，并核验最终运行基础。"""
    require(kind in DOCKERFILES, "未知发布角色")
    require(isinstance(policy, dict) and kind in policy, "发布策略缺少对应角色运行镜像")
    aliases, stages = {}, []
    for instruction in instructions(text):
        if instruction.split(None, 1)[0].upper() != "FROM":
            continue
        require("$" not in instruction, "FROM禁止使用变量，必须明确写出基础镜像")
        try:
            tokens = shlex.split(instruction, comments=False)[1:]
        except ValueError:
            raise GuardError("Dockerfile的FROM格式异常") from None
        if tokens and tokens[0].startswith("--platform="):
            require(re.fullmatch(r"--platform=linux/(amd64|arm64)", tokens[0]), "FROM平台参数不在明确白名单内")
            tokens = tokens[1:]
        require(len(tokens) == 1 or (len(tokens) == 3 and tokens[1].upper() == "AS"), "Dockerfile的FROM格式异常")
        source = tokens[0]
        if source.lower() in aliases:
            base = aliases[source.lower()]
        elif source.isdigit() and int(source) < len(stages):
            base = stages[int(source)]
        elif source in policy[kind]:
            base = source
        else:
            base = re.sub(r"^(docker\.io/)?library/", "", source)
            name, separator, digest = base.partition("@")
            require(not separator or DIGEST.fullmatch(digest), "基础镜像digest格式不正确")
            require(name in OFFICIAL_BASES[kind], "禁止业务成品、未知基础或未核验裸sha作为FROM：" + source)
            base = name
        stages.append(base)
        if len(tokens) == 3:
            alias = tokens[2].lower()
            require(re.fullmatch(r"[a-z_][a-z0-9_.-]*", alias) and alias not in aliases, "Dockerfile的stage名称异常或重复")
            aliases[alias] = base
    require(stages, "Dockerfile缺少FROM")
    require(stages[-1] in RUNNER_BASES[kind] | policy[kind], "最终runner必须使用该角色的干净运行基础")
    return {"kind": kind, "stages": len(stages), "runner_base": stages[-1], "dockerfile_sha256": hashlib.sha256(text.encode("utf-8")).hexdigest()}


def check_dockerfile_path(path, kind, policy):
    """check_dockerfile_path 读取实际构建文件并按原始字节计算标签摘要，兼容Windows换行。"""
    try:
        data = Path(path).read_bytes()
        result = check_dockerfile(data.decode("utf-8-sig"), kind, policy)
    except (OSError, UnicodeError):
        raise GuardError("发布Dockerfile无法读取：" + str(path)) from None
    result["dockerfile_sha256"] = hashlib.sha256(data).hexdigest()
    return result


class DockerReader:
    """只查询镜像身份、标签、父链和文件层，禁止读取或打印环境变量与完整history命令。"""

    def query(self, args):
        """query 执行有超时的Docker只读命令，查询失败时不放行也不回显敏感错误正文。"""
        try:
            result = subprocess.run(["docker"] + args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30, check=False)
            require(result.returncode == 0, "Docker只读查询失败，已停止发布")
            return result.stdout.decode("utf-8")
        except (OSError, subprocess.TimeoutExpired, UnicodeError):
            raise GuardError("Docker只读查询不可用或超时，已停止发布") from None

    def inspect(self, reference):
        """inspect 只读取镜像身份、父镜像、标签及RootFS，不读取Config.Env。"""
        require(isinstance(reference, str) and reference and not reference.startswith("-") and not any(x.isspace() for x in reference), "镜像引用格式异常")
        template = '{"Id":{{json .Id}},"Parent":{{json .Parent}},"RepoTags":{{json .RepoTags}},"Layers":{{json .RootFS.Layers}},"DockerfileSHA":{{if .Config.Labels}}{{json (index .Config.Labels "goodhr.dockerfile_sha256")}}{{else}}null{{end}},"Release":{{if .Config.Labels}}{{json (index .Config.Labels "goodhr.release")}}{{else}}null{{end}},"ReleaseID":{{if .Config.Labels}}{{json (index .Config.Labels "goodhr.release.id")}}{{else}}null{{end}}}'
        try:
            value = json.loads(self.query(["image", "inspect", "--format", template, reference]))
            require(isinstance(value, dict) and DIGEST.fullmatch(value.get("Id", "")), "镜像身份数据异常")
            require(value.get("Parent", "") == "" or DIGEST.fullmatch(value["Parent"]), "镜像父链数据异常")
            require(isinstance(value.get("Layers"), list) and value["Layers"] and all(isinstance(x, str) and DIGEST.fullmatch(x) for x in value["Layers"]), "镜像RootFS信息缺失或异常")
            require(value.get("RepoTags") is None or (isinstance(value["RepoTags"], list) and all(isinstance(x, str) for x in value["RepoTags"])), "镜像标签数据异常")
            require(value.get("DockerfileSHA") is None or isinstance(value.get("DockerfileSHA"), str), "镜像Dockerfile摘要标签异常")
            require(value.get("Release") is None or isinstance(value.get("Release"), str), "镜像发布标签异常")
            require(value.get("ReleaseID") is None or isinstance(value.get("ReleaseID"), str), "镜像发布身份标签异常")
            return value
        except (ValueError, TypeError, KeyError) as error:
            if isinstance(error, GuardError):
                raise
            raise GuardError("镜像检查结果无法解析，已停止发布") from None

    def business_images(self):
        """business_images 按业务仓库名称及发布标签发现本机旧成品，不查询容器或环境。"""
        try:
            rows = [json.loads(line) for line in self.query(["image", "ls", "--all", "--no-trunc", "--format", '{"Id":{{json .ID}},"Repository":{{json .Repository}}}']).splitlines()]
            require(all(isinstance(row, dict) and DIGEST.fullmatch(row.get("Id", "")) and isinstance(row.get("Repository"), str) for row in rows), "本机镜像列表数据异常")
            tagged = {row["Id"] for row in rows if row["Repository"].split("/")[-1].startswith("goodhr-production-")}
            labelled = []
            for label in ("goodhr.release", "goodhr.release.id"):
                labelled.extend(self.query(["image", "ls", "--all", "--quiet", "--no-trunc", "--filter", "label=" + label]).splitlines())
            require(all(DIGEST.fullmatch(value) for value in labelled), "业务镜像标签查询异常")
            return tagged | set(labelled)
        except (ValueError, TypeError, KeyError) as error:
            if isinstance(error, GuardError):
                raise
            raise GuardError("本机业务镜像列表无法解析，已停止发布") from None

    def history(self, reference):
        """history 只读取不截断的历史镜像ID，不采集可能包含ENV内容的CreatedBy。"""
        try:
            rows = [json.loads(line) for line in self.query(["image", "history", "--no-trunc", "--format", "{{json .ID}}", reference]).splitlines()]
            require(rows and all(isinstance(value, str) and (value == "<missing>" or DIGEST.fullmatch(value)) for value in rows), "镜像history身份缺失或异常")
            return rows
        except (ValueError, TypeError) as error:
            if isinstance(error, GuardError):
                raise
            raise GuardError("镜像history无法解析，已停止发布") from None


def is_business_image(image):
    """is_business_image 以明确业务仓库或发布标签识别已存在的应用成品。"""
    return bool(image.get("Release") or image.get("ReleaseID")) or any(tag.split("/")[-1].startswith("goodhr-production-") for tag in image.get("RepoTags") or [])


def same_build_metadata_parent(image, candidate):
    """same_build_metadata_parent 仅豁免同发布且文件层完全相同的纯元数据Parent，旧发布标签和业务成品tag均不豁免。"""
    release_id = candidate.get("ReleaseID")
    return (isinstance(release_id, str) and bool(release_id.strip())
            and image.get("ReleaseID") == release_id and not image.get("Release")
            and image["Layers"] == candidate["Layers"]
            and not any(tag.split("/")[-1].startswith("goodhr-production-") for tag in image.get("RepoTags") or []))


def check_image(reference, kind, dockerfile, policy, reader=None):
    """check_image 强制核验构建文件摘要，拒绝业务祖先；仅豁免实际Parent链中同发布、同文件层的纯元数据节点。"""
    require(kind in DOCKERFILES, "未知发布角色")
    source = check_dockerfile_path(dockerfile, kind, policy)
    reader = reader or DockerReader()
    candidate = reader.inspect(reference)
    require(candidate.get("DockerfileSHA") == source["dockerfile_sha256"], "候选镜像缺少匹配实际受检Dockerfile的摘要标签")
    candidate_id = candidate["Id"]
    business = reader.business_images() - {candidate_id} - policy[kind]
    cache = {candidate_id: candidate}
    parent, seen, metadata_parents = candidate.get("Parent", ""), {candidate_id}, set()
    while parent:
        require(parent not in seen, "镜像父链出现循环")
        seen.add(parent)
        image = reader.inspect(parent)
        require(image["Id"] == parent, "镜像父链查询身份不匹配")
        cache[image["Id"]] = image
        business_parent = image["Id"] in business or is_business_image(image)
        if image["Id"] not in policy[kind] and business_parent:
            require(same_build_metadata_parent(image, candidate), "候选镜像继承了旧业务成品，必须从干净运行基础重新构建")
            metadata_parents.add(image["Id"])
        parent = image.get("Parent", "")
    history = set(reader.history(candidate_id)) - {candidate_id, "<missing>"}
    require(not (history & business) - metadata_parents, "候选镜像history包含旧业务成品，必须重新构建")
    ambiguous = 0
    for image_id in sorted(business):
        image = cache.get(image_id) or reader.inspect(image_id)
        old, new = image["Layers"], candidate["Layers"]
        if len(old) < len(new) and new[:len(old)] == old:
            ambiguous += 1
    return {"kind": kind, "image_id": candidate_id, "checked_business_images": len(business), "dockerfile_sha256": source["dockerfile_sha256"], "ambiguous_prefixes_with_build_proof": ambiguous, "same_build_metadata_parents": len(metadata_parents)}


def main(argv=None):
    """main 提供源码检查和候选镜像检查入口，仅输出不含凭据的审定摘要。"""
    parser = argparse.ArgumentParser(description="GoodHR发布镜像来源检查")
    parser.add_argument("--policy", default=str(POLICY_PATH))
    subparsers = parser.add_subparsers(dest="operation", required=True)
    files = subparsers.add_parser("dockerfiles")
    files.add_argument("--backend", default=str(DOCKERFILES["backend"]))
    files.add_argument("--frontend", default=str(DOCKERFILES["frontend"]))
    image = subparsers.add_parser("image")
    image.add_argument("--image", required=True)
    image.add_argument("--kind", choices=tuple(DOCKERFILES), required=True)
    image.add_argument("--dockerfile")
    args = parser.parse_args(argv)
    policy = load_policy(args.policy)
    if args.operation == "dockerfiles":
        result = [check_dockerfile_path(getattr(args, kind), kind, policy) for kind in DOCKERFILES]
    else:
        result = check_image(args.image, args.kind, args.dockerfile or DOCKERFILES[args.kind], policy)
    print(json.dumps({"success": True, "checks": result}, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except GuardError as error:
        print(json.dumps({"success": False, "error": str(error)}, ensure_ascii=False), file=sys.stderr)
        sys.exit(1)
