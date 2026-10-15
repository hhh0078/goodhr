#!/usr/bin/env bash
# 文件作用：给正式发布统一接入基础镜像、空间、互斥锁、隔离验收证据及发布收尾检查；由deploy-environment.sh加载。

# production_prepare 校验源码和空间，生成不可变镜像标签，并持有与人工发布共用的部署锁。
production_prepare() {
  local source_lock="$root/.release-source.lock"
  [[ ! -L "$source_lock" && "$(realpath "$root")" == "$root" ]] || return 1
  exec 11>>"$source_lock"
  flock -n 11 || { echo "源码正在更新或发布，本次先停一下" >&2; return 1; }
  python3 "$root/scripts/release_guard.py" dockerfiles
  export GOODHR_SOURCE_COMMIT="${GOODHR_SOURCE_COMMIT:-$(git rev-parse HEAD)}"
  [[ "$GOODHR_SOURCE_COMMIT" =~ ^[0-9a-f]{40}$ ]] || { echo "源码提交格式不正确" >&2; return 1; }
  if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    [[ "$(git rev-parse HEAD)" == "$GOODHR_SOURCE_COMMIT" && -z "$(git status --porcelain --untracked-files=normal)" ]] ||
      { echo "源码有未提交改动或提交不一致，先停一下发布" >&2; return 1; }
  fi
  export GOODHR_RELEASE_IMAGE_TAG="${GOODHR_RELEASE_IMAGE_TAG:-${GOODHR_SOURCE_COMMIT:0:12}-$(date -u +%Y%m%dT%H%M%SZ)}"
  [[ "$GOODHR_RELEASE_IMAGE_TAG" =~ ^[a-zA-Z0-9_][a-zA-Z0-9_.-]{0,127}$ ]] ||
    { echo "发布标签格式不正确" >&2; return 1; }
  local lock_root=/opt/goodhr-release
  mkdir -p "$lock_root"
  [[ ! -L "$lock_root" && "$(realpath "$lock_root")" == "$lock_root" ]] || return 1
  exec 7>>"$lock_root/.release-operation.lock"
  exec 8>>"$lock_root/.backend-deploy.lock"
  exec 9>>"$lock_root/.frontend-deploy.lock"
  local container_lock=/www/wwwroot/goodhr-production/backups/.goodhr-container-deploy.lock
  [[ ! -L "$container_lock" && "$(realpath "$(dirname "$container_lock")")" == "$(dirname "$container_lock")" ]] || return 1
  exec 10>>"$container_lock"
  flock -n 7 && flock -n 8 && flock -n 9 && flock -n 10 ||
    { echo "已有发布在进行，本次先停一下" >&2; return 1; }
  production_check_source
  local docker_root
  docker_root="$(docker info --format '{{.DockerRootDir}}')"
  python3 - "$lock_root" "$docker_root" <<'PY'
import shutil, sys
for path, minimum in ((sys.argv[1], 6 * 1024**3), (sys.argv[2], 3 * 1024**3)):
    if shutil.disk_usage(path).free < minimum:
        raise SystemExit("可用空间不足，发布提前停止；先核对发布材料和回退保留清单")
PY
  production_release_dir="$lock_root/$GOODHR_RELEASE_IMAGE_TAG"
  [[ ! -L "$production_release_dir" ]] || return 1
  if [[ "$action" != --activate-only && -e "$production_release_dir" ]]; then
    echo "发布目录已存在，请使用新标签，避免覆盖上次材料" >&2
    return 1
  fi
  mkdir -p "$production_release_dir"
  [[ "$(realpath "$production_release_dir")" == "$production_release_dir" ]] || return 1
  chmod 700 "$production_release_dir"
  export GOODHR_BACKEND_DOCKERFILE_SHA GOODHR_FRONTEND_DOCKERFILE_SHA
  GOODHR_BACKEND_DOCKERFILE_SHA="$(sha256sum "$root/cloud/backend/Dockerfile.release" | cut -d' ' -f1)"
  GOODHR_FRONTEND_DOCKERFILE_SHA="$(sha256sum "$root/cloud/frontend-next/Dockerfile" | cut -d' ' -f1)"
  printf '发布标签：%s\n' "$GOODHR_RELEASE_IMAGE_TAG"
}

# production_check_source 复核持锁后和构建完成时的源码身份，防止混入另一轮工作区改动。
production_check_source() {
  [[ "$(git rev-parse HEAD)" == "$GOODHR_SOURCE_COMMIT" && -z "$(git status --porcelain --untracked-files=normal)" ]] ||
    { echo "发布期间源码发生变化，请重新核对候选" >&2; return 1; }
}

# production_require_new_images 拒绝覆盖任一已经存在的版本标签，查询失败也必须停止构建。
production_require_new_images() {
  local kind existing image
  for kind in backend frontend; do
    image="$COMPOSE_PROJECT_NAME-$kind:$GOODHR_RELEASE_IMAGE_TAG"
    existing="$(docker image ls --quiet --no-trunc "$image")" || return
    [[ -z "$existing" ]] || { echo "版本镜像已存在，请换一个发布标签再构建" >&2; return 1; }
  done
}

# production_check_images 构建后检查实际镜像来源，防止旧成品继承或镜像标签漂移。
production_check_images() {
  production_check_source
  local kind image metadata expected_sha
  for kind in backend frontend; do
    image="$COMPOSE_PROJECT_NAME-$kind:$GOODHR_RELEASE_IMAGE_TAG"
    python3 "$root/scripts/release_guard.py" image --kind "$kind" --image "$image"
    expected_sha="$GOODHR_FRONTEND_DOCKERFILE_SHA"
    [[ "$kind" != backend ]] || expected_sha="$GOODHR_BACKEND_DOCKERFILE_SHA"
    metadata="$(docker image inspect --format '{"Commit":{{if .Config.Labels}}{{json (index .Config.Labels "goodhr.source_commit")}}{{else}}null{{end}},"DockerfileSHA":{{if .Config.Labels}}{{json (index .Config.Labels "goodhr.dockerfile_sha256")}}{{else}}null{{end}},"Role":{{if .Config.Labels}}{{json (index .Config.Labels "goodhr.release_kind")}}{{else}}null{{end}},"ReleaseID":{{if .Config.Labels}}{{json (index .Config.Labels "goodhr.release.id")}}{{else}}null{{end}}}' "$image")"
    python3 - "$metadata" "$GOODHR_SOURCE_COMMIT" "$expected_sha" "$kind" "$GOODHR_RELEASE_IMAGE_TAG" <<'PY'
import json, sys
v = json.loads(sys.argv[1])
if [v.get(k) for k in ("Commit", "DockerfileSHA", "Role", "ReleaseID")] != sys.argv[2:]:
    raise SystemExit("镜像源码、Dockerfile或角色标签不一致，发布已停止")
PY
  done
  production_backend_id="$(docker image inspect --format '{{.Id}}' "$COMPOSE_PROJECT_NAME-backend:$GOODHR_RELEASE_IMAGE_TAG")"
  production_frontend_id="$(docker image inspect --format '{{.Id}}' "$COMPOSE_PROJECT_NAME-frontend:$GOODHR_RELEASE_IMAGE_TAG")"
  production_check_source
}

# production_require_evidence 仅允许激活与本次镜像身份完全一致、完成隔离数据及回退验证的候选。
production_require_evidence() {
  [[ -n "${GOODHR_VERIFIED_RELEASE_EVIDENCE:-}" && -f "$GOODHR_VERIFIED_RELEASE_EVIDENCE" ]] ||
    { echo "候选已构建，请先完成隔离验证，再提供GOODHR_VERIFIED_RELEASE_EVIDENCE激活" >&2; return 1; }
  [[ -f "${GOODHR_RETENTION_REGISTRY:-/opt/goodhr-release/retention-registry.json}" ]] ||
    { echo "缺少经过验证的回退登记，先保留可用回退再发布" >&2; return 1; }
  python3 - "$GOODHR_VERIFIED_RELEASE_EVIDENCE" "$production_backend_id" "$production_frontend_id" "$GOODHR_SOURCE_COMMIT" <<'PY'
import json, pathlib, socket, sys
p = pathlib.Path(sys.argv[1])
if p.is_symlink() or p.resolve() != p.absolute():
    raise SystemExit("验收记录路径不安全")
v = json.loads(p.read_text(encoding="utf-8"))
if not (v.get("stage") == "isolated_validation" and v.get("success") is True and v.get("legacy_data_preserved") is True
        and v.get("rollback_verified") is True and v.get("host") == socket.gethostname()
        and v.get("backend_image") == sys.argv[2] and v.get("frontend_image") == sys.argv[3]
        and v.get("source_commit") == sys.argv[4]):
    raise SystemExit("验收记录与本次候选不匹配，发布已停止")
PY
  python3 "$root/scripts/release_retention.py" check \
    --registry "${GOODHR_RETENTION_REGISTRY:-/opt/goodhr-release/retention-registry.json}" \
    --evidence "$GOODHR_VERIFIED_RELEASE_EVIDENCE"
}

# production_finalize 在健康验收后清理已登记的临时目录，再按真实兼容记录回收本项目旧容器和镜像。
production_finalize() {
  local manifest="$production_release_dir/release-hygiene-manifest.json"
  python3 - "$manifest" "$production_release_dir" <<'PY' || return
import json, os, pathlib, socket, sys
p = pathlib.Path(sys.argv[1])
if p.exists():
    raise SystemExit("收尾记录已存在，请核对上次发布结果后再处理")
with p.open("x", encoding="utf-8") as f:
    os.chmod(p, 0o600)
    json.dump({"schema_version": 1, "host": socket.gethostname(), "release_dir": sys.argv[2],
               "status": "succeeded", "completed_workdirs": []}, f, ensure_ascii=False, indent=2)
PY
  local plan="$production_release_dir/hygiene-plan.json" plan_sha
  python3 "$root/scripts/release_hygiene.py" plan --release-dir "$production_release_dir" --out "$plan" || return
  plan_sha="$(sha256sum "$plan" | cut -d' ' -f1)" || return
  python3 "$root/scripts/release_hygiene.py" apply --plan "$plan" --sha256 "$plan_sha" || return
  # 回收模块自行重新持锁、重读Docker快照，避免子进程与父进程重复持锁。
  flock -u 9 || return
  flock -u 8 || return
  flock -u 10 || return
  plan="$production_release_dir/docker-retention-plan.json"
  python3 "$root/scripts/release_retention.py" plan \
    --registry "${GOODHR_RETENTION_REGISTRY:-/opt/goodhr-release/retention-registry.json}" --out "$plan" || return
  plan_sha="$(sha256sum "$plan" | cut -d' ' -f1)" || return
  python3 "$root/scripts/release_retention.py" apply --plan "$plan" --sha256 "$plan_sha" || return
}
