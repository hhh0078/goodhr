"""文件作用：登记并核验GoodHR独立BuildKit构建器，限制资源和缓存；不操作应用容器、数据卷或其他项目构建器。"""

import argparse
import base64
import hashlib
import json
from pathlib import Path
import socket
import subprocess

from release_activate import DockerEngine, save_json
from release_cutover import require


NAME = "goodhr-release"
CONTAINER = "buildx_buildkit_" + NAME + "0"
TOOLS = Path("/www/wwwroot/goodhr-deploy-tools/docker")
RECORD = TOOLS / "goodhr-builder.json"
CONFIG = Path(__file__).with_name("buildkitd.goodhr.toml")
IMAGE = "moby/buildkit:v0.35.0"
OPTIONS = {"image": IMAGE, "memory": "1536m", "memory-swap": "3g", "cpu-shares": "256", "default-load": "true"}


def call(arguments, **options):
    """只使用GoodHR私有CLI目录，显式选择构建器，不改变系统默认构建器。"""
    return subprocess.run(["docker", "--config", str(TOOLS), "buildx"] + arguments,
                          check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, **options).stdout


def metadata():
    """读取Buildx结构化列表；同一节点的重复行必须完全一致，不依赖不支持的inspect格式化参数。"""
    rows = [json.loads(line) for line in call(["ls", "--format", "{{json .}}"]).decode().splitlines()]
    selected = [row for row in rows if row.get("Name") == NAME]
    require(selected and all(row == selected[0] for row in selected), "GoodHR构建器缺失或同名记录冲突")
    return selected[0]


def verify(engine):
    """核对登记主机、配置摘要、容器和固定镜像ID，拒绝同名外部构建器或资源限额漂移。"""
    require(RECORD.is_file() and not RECORD.is_symlink() and TOOLS.resolve() == TOOLS, "缺少GoodHR构建器登记，请先执行release_builder.py setup")
    record = json.loads(RECORD.read_text())
    require(record.get("host") == socket.gethostname() and record.get("config_sha256") == hashlib.sha256(CONFIG.read_bytes()).hexdigest(), "GoodHR构建器登记或配置变化")
    builder = metadata()
    require(builder.get("Name") == NAME and builder.get("Driver") == "docker-container"
            and len(builder.get("Nodes", [])) == 1 and builder["Nodes"][0].get("DriverOpts") == OPTIONS,
            "GoodHR构建器类型、节点或参数变化")
    node = builder["Nodes"][0]
    require(node.get("Endpoint") == "unix:///var/run/docker.sock"
            and hashlib.sha256(base64.b64decode(node.get("Files", {}).get("buildkitd.toml", ""))).hexdigest()
            == record.get("builder_config_sha256"), "GoodHR构建器连接或实际缓存策略变化")
    container = engine.inspect(CONTAINER)
    require(container["Id"] == record.get("container_id") and container["Image"] == record.get("image_id"), "GoodHR构建器身份变化")
    host = container["HostConfig"]
    require(host.get("Memory") == 1536 * 1024**2 and host.get("MemorySwap") == 3 * 1024**3
            and host.get("CpuShares") == 256, "GoodHR构建器资源限制变化")
    return container


def setup(engine):
    """一次性登记独立构建器；缺少已校验的Buildx插件时停止，不自动下载或退回经典构建。"""
    TOOLS.mkdir(mode=0o700, parents=True, exist_ok=True)
    require(TOOLS.resolve() == TOOLS and not RECORD.is_symlink(), "GoodHR工具目录不安全")
    if RECORD.exists():
        verify(engine)
        return
    require(b"v0.39.0 " in call(["version"]), "需要先安装校验过的Buildx v0.39.0插件")
    builders = call(["ls", "--format", "{{.Name}}" ]).decode().splitlines()
    require(NAME not in builders, "同名构建器尚未登记，停止接管")
    call(["create", "--name", NAME, "--driver", "docker-container", "--buildkitd-config", str(CONFIG),
          "--driver-opt", ",".join(key + "=" + value for key, value in OPTIONS.items()), "unix:///var/run/docker.sock"])
    call(["inspect", NAME, "--bootstrap"])
    container = engine.inspect(CONTAINER)
    save_json(RECORD, {"schema_version": 1, "host": socket.gethostname(), "container_id": container["Id"],
                      "image_id": container["Image"], "config_sha256": hashlib.sha256(CONFIG.read_bytes()).hexdigest(),
                      "builder_config_sha256": hashlib.sha256(base64.b64decode(metadata()["Nodes"][0]["Files"]["buildkitd.toml"])).hexdigest()}, exclusive=True)
    verify(engine)


def trim(engine):
    """仅回收已登记构建器超出4GiB的缓存，保留可复用依赖后停止空闲构建器，不删除镜像或数据卷。"""
    container = verify(engine)
    call(["prune", "--builder", NAME, "--all", "--reserved-space", "0", "--max-used-space", "4GiB"], input=b"y\n")
    verify(engine)
    engine.request("POST", engine.prefix + "/containers/" + container["Id"] + "/stop?t=30")


def main():
    """由持发布锁的正式入口调用；检查失败时禁止继续构建或切换线上。"""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=("setup", "check", "trim"))
    args = parser.parse_args()
    engine = DockerEngine()
    if args.operation == "setup":
        setup(engine)
    elif args.operation == "trim":
        trim(engine)
    else:
        verify(engine)
    print(json.dumps({"builder": NAME, "operation": args.operation, "success": True}))


if __name__ == "__main__":
    main()
