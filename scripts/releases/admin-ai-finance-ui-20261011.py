"""文件作用：发布用户AI余额统计及简历UI，复用已审查启动器，严格禁止生产迁移和数据库恢复。"""
import argparse
import hashlib
import importlib.util
import json
import os
import pathlib
import re
import signal
import socket
import subprocess
import urllib.request

ROOT = pathlib.Path("/opt/goodhr-release/admin-ai-finance-ui-20261011")
DATABASE = "goodhr_hr_verify_20261011_finance"
CLONE = "goodhr-hr-db-finance-20261011"
LAUNCHER = pathlib.Path("/opt/goodhr-release/hr-interview-20261010-63ad61cd/hr-release.py")
LAUNCHER_SHA256 = "f23375cf68107eb2ed21637d38b2462f7346c14b82ab9a3d069893ea700fef0c"
BASELINE = {
    "goodhr-production-backend-1": ("66dd55bb25c7f35be2ad8affedc6b495027502b019f950551e02dcb787e34e60", "sha256:f82fa9573811363b3bbeeab428e776371f3830ed9fa32bcd43815b3044750477", "2026-10-10T15:30:02.730190862Z"),
    "goodhr-production-frontend-1": ("88399efbe2afee578a3c8d810d72b41b1393f656192c69a05a40f106f6eaa7ce", "sha256:069c89ff154a0c7cbb35c190b6ede92d50d292626f464ccd0c05fad886b28e0c", "2026-10-10T15:30:05.257018977Z"),
    "postgres": ("7ae7305d3a222272d23cf4a68ae302cf877fc71d2ce1fab9ad3a97b3fe02d89d", "sha256:413d57cb67bb3585acae8769a48661aa1dcac66076e3a5f88ea6c5be55f8916e", "2026-09-20T12:25:51.09539213Z"),
    "redis": ("78a704cd2eb89019c62bfb6ab2ceef603bf2cd293fe3d522ca7c132a1da72025", "sha256:646a47c903c5c45f99081094d8c3ba51ec2a1091babf42dc5a928cef024e6fcd", "2026-09-20T12:28:59.032970342Z"),
}


def launcher():
    """核对部署器原文字节摘要后加载，仅调整已明确授权的隔离库名。"""
    if LAUNCHER.is_symlink() or hashlib.sha256(LAUNCHER.read_bytes()).hexdigest() != LAUNCHER_SHA256:
        raise RuntimeError("复用部署器摘要不一致，未执行发布")
    spec = importlib.util.spec_from_file_location("reviewed_hr_launcher", LAUNCHER)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    module.VERIFY_DATABASE = DATABASE
    module.existing = safe_existing
    return module


def safe_existing(name):
    """只有Docker明确报告精确对象不存在时返回空，daemon、权限和超时错误一律停止。"""
    try:
        result = subprocess.run(["docker", "inspect", name], stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30)
    except (subprocess.TimeoutExpired, OSError):
        raise RuntimeError("容器查询未得到明确结果，保留现场") from None
    if result.returncode:
        if result.stdout.strip() == b"[]" and result.stderr.strip() == ("Error: No such object: " + name).encode():
            return None
        raise RuntimeError("容器查询失败，不能当作不存在")
    try:
        values = json.loads(result.stdout)
        if not isinstance(values, list) or len(values) != 1 or not isinstance(values[0], dict) or values[0].get("Name") != "/" + name:
            raise ValueError("unexpected identity")
        return values[0]
    except (ValueError, TypeError, KeyError, IndexError):
        raise RuntimeError("容器查询返回了不确定对象，保留现场") from None


def verify_baseline(hr, apps=True):
    """核对服务器IP及应用、PG、Redis的精确身份；漂移时在停止前拒绝。"""
    if socket.gethostname() != "ldclouda31539" or "43.228.79.108" not in hr.run(["hostname", "-I"]).decode().split():
        raise RuntimeError("服务器身份不一致")
    result = []
    for name, expected in BASELINE.items():
        if not apps and name.startswith("goodhr-production-"):
            continue
        item = hr.inspect(name)
        if (item["Id"], item["Image"], item["State"]["StartedAt"]) != expected or not item["State"]["Running"]:
            raise RuntimeError("生产基线已变化：" + name)
        result.append(item)
    return result


def image_proof(hr, image_id, commit):
    """仅接受完整不可变镜像ID并核对OCI提交，拒绝可变tag或截断ID。"""
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", image_id or ""):
        raise RuntimeError("镜像必须是完整SHA256 ID")
    image = hr.inspect(image_id, image=True)
    if image["Id"] != image_id or (image["Config"].get("Labels") or {}).get("org.opencontainers.image.revision") != commit:
        raise RuntimeError("镜像身份或OCI提交不一致")
    return image


def migration_proof(hr, images, source_hash=None):
    """只读取新旧镜像SQL与正式ledger，98份正文必须完全相同，绝不执行DDL。"""
    old = hr.image_migrations(BASELINE["goodhr-production-backend-1"][1])
    new = hr.image_migrations(images["backend"]["id"])
    applied = hr.ledger("postgres", "goodhr5")
    if len(applied) != 98 or applied != sorted(old) or old != new:
        raise RuntimeError("新旧98项迁移或已执行ledger不一致，禁止发布")
    if source_hash:
        if hr.digest(ROOT / "source.tar") != source_hash:
            raise RuntimeError("源码归档摘要变化")
        source = hr.migrations_from_tar((ROOT / "source.tar").read_bytes(), "goodhr5/cloud/backend/db/migrations/")
        if source != new:
            raise RuntimeError("归档与镜像迁移正文不一致")
    return {"ledger": applied, "migration_sha256": {name: hashlib.sha256(text.encode()).hexdigest() for name, text in new.items()}}


def finance_totals(hr, container, database):
    """只读汇总总余额和成功AI充值，不读取账号、订单正文或更新资金。"""
    query = "SELECT json_build_object('total_users',count(*),'total_ai_balance_units',COALESCE(sum(ai_balance_units),0),'ai_recharge_amount_cents',(SELECT COALESCE(sum(amount_cents),0) FROM payment_orders WHERE order_type='ai_balance' AND status='paid')) FROM users;"
    return json.loads(hr.sql(container, database, query, readonly=True))


def clone_proof(hr, state):
    """只接受本轮预先恢复的精确PG容器、受控库名和环回55441端口。"""
    name = state["database_container"]
    if name != CLONE or state["database"] != DATABASE or not re.fullmatch(r"[0-9a-f]{64}", state["database_id"] or ""):
        raise RuntimeError("隔离数据库参数不符合本轮约束")
    item = hr.inspect(name)
    ports = {"5432/tcp": [{"HostIp": "127.0.0.1", "HostPort": "55441"}]}
    labels = item["Config"].get("Labels") or {}
    if item["Id"] != state["database_id"] or item["Image"] != BASELINE["postgres"][1] or not item["State"]["Running"] or labels.get("goodhr.release") != ROOT.name or item["HostConfig"].get("PortBindings") != ports:
        raise RuntimeError("独立PG身份、标签或端口不一致")
    return item


def materials(hr, commit):
    """每阶段重新核对提交、归档、dump及镜像，阻止材料在验收后被替换。"""
    value = hr.load(ROOT, "manifest.private.json")
    if value["commit"] != commit or not re.fullmatch(r"[0-9a-f]{40}", commit or ""):
        raise RuntimeError("阶段提交与明确CLI提交不一致")
    for kind in ("backend", "frontend"):
        image_proof(hr, value["images"][kind]["id"], commit)
    actual = migration_proof(hr, value["images"], value["source_sha256"])
    if actual["ledger"] != value["ledger"] or actual["migration_sha256"] != value["migration_sha256"] or hr.digest(ROOT / "database.dump") != value["dump"]["sha256"]:
        raise RuntimeError("迁移证明或完整备份摘要变化")
    # 提前检查完整旧Config/HostConfig能否复用，避免停止后才发现启动参数不受支持。
    for kind in ("backend", "frontend"):
        hr.application_payload(ROOT, kind, value["images"][kind]["id"])
    return value


def prepare(hr, args):
    """备份原应用完整配置并验证root已建立的完整恢复库，只读正式库。"""
    current = verify_baseline(hr)
    if any((ROOT / name).exists() for name in ("manifest.private.json", "before-inspect.private.json", "rehearsal.private.json")):
        raise RuntimeError("已有准备材料，禁止覆盖")
    build = hr.load(ROOT, "build-result.json")
    if build["commit"] != args.commit:
        raise RuntimeError("构建证明与CLI提交不一致")
    for kind in ("backend", "frontend"):
        image_proof(hr, build["images"][kind]["id"], args.commit)
    if not re.fullmatch(r"[0-9a-f]{64}", build.get("source_sha256", "")):
        raise RuntimeError("缺少源码归档SHA256")
    migration = migration_proof(hr, build["images"], build["source_sha256"])
    dump = ROOT / "database.dump"
    with dump.open("rb") as stream:
        if stream.read(5) != b"PGDMP":
            raise RuntimeError("完整备份不是PGDMP格式")
    with dump.open("rb") as stream:
        if not hr.run(["docker", "exec", "-i", "postgres", "pg_restore", "--list"], input_file=stream):
            raise RuntimeError("完整备份目录校验未通过")
    state = {"database_container": args.clone_container, "database_id": args.clone_id, "database": DATABASE, "candidates": {}, "cleaned": False}
    clone_proof(hr, state)
    if hr.ledger(args.clone_container, DATABASE) != migration["ledger"]:
        raise RuntimeError("恢复库98项ledger与正式基线不一致")
    manifest = {"commit": args.commit, "images": build["images"], "source_sha256": build["source_sha256"], **migration,
                "dump": {"bytes": dump.stat().st_size, "sha256": hr.digest(dump)}, "baseline": BASELINE,
                "finance_before": finance_totals(hr, "postgres", "goodhr5")}
    hr.save(ROOT, "before-inspect.private.json", current)
    hr.save(ROOT, "manifest.private.json", manifest)
    hr.save(ROOT, "rehearsal.private.json", state)
    for kind in ("backend", "frontend"):
        hr.application_payload(ROOT, kind, manifest["images"][kind]["id"])
    verify_baseline(hr)
    print(json.dumps({"prepared": True, "commit": args.commit, "ledger_count": 98, "production_migrations": 0, "dump": manifest["dump"]}))


def verify_http(hr, backend, frontend, public=False):
    """匿名检查登录、管理员权限和保留招聘接口，不登录、不写业务资料。"""
    hr.wait_http(backend + "/health")
    for path in ("/api/admin/users", "/api/candidates", "/api/recruitment/company"):
        hr.wait_http(backend + path, 401)
    hr.wait_http(frontend + "/login")
    hr.wait_http(frontend + "/admin/users")
    hr.wait_http(frontend + "/admin/resumes")
    hr.wait_http(frontend + "/admin/recruitment")
    if public:
        hr.wait_http("https://goodhr.58it.cn/login")
        hr.wait_http("https://goodhr.58it.cn/api/candidates", 401)
        verify_public_content("https://goodhr.58it.cn")
    else:
        verify_public_content(frontend)


def verify_public_content(base):
    """核对已批准的下载分区、近期三条记录、原图和不变Chrome安装包摘要。"""
    with urllib.request.urlopen(base + "/download", timeout=20) as response:
        text = response.read().decode("utf-8")
    if not all(value in text for value in ("本地程序下载", "浏览器插件下载", "展示更多")) or text.count("<article") != 3:
        raise RuntimeError("下载分区和近期记录未符合已批准UI")
    for path, expected in (("/assets/resume-guagua.jpg", "fa25738fbcfadc37757f5e445738620a5344b58834fd0a8a6ea557b58daf8b16"), ("/downloads/goodhr-extension-chrome-1.0.3.zip", "bd03b682e419a43124b1775b5e13edaec3be4fc48e34b6eb35c3c97265801dc7")):
        with urllib.request.urlopen(base + path, timeout=20) as response:
            if hashlib.sha256(response.read()).hexdigest() != expected:
                raise RuntimeError("公开静态资源摘要不一致")


def recognize(hr, manifest, kind, item, identity=None):
    """核对本轮应用ID、镜像及三项标签，未知创建结果也不能误动外来容器。"""
    labels = item["Config"].get("Labels") or {}
    if item["Image"] != manifest["images"][kind]["id"] or (identity and item["Id"] != identity) or labels.get("goodhr.release") != ROOT.name or labels.get("goodhr.source_commit") != manifest["commit"] or labels.get("goodhr.release_kind") != kind:
        raise RuntimeError("应用不属于本轮，保留现场")


def rehearse(hr, args):
    """在停发信、禁Redis、独立日志目录的恢复库上验证候选前后端及原行摘要。"""
    verify_baseline(hr)
    manifest = materials(hr, args.commit)
    state = hr.load(ROOT, "rehearsal.private.json")
    clone_proof(hr, state)
    if state["candidates"] or (ROOT / "rehearsal-result.private.json").exists():
        raise RuntimeError("已有候选验收现场，禁止盲目重跑")
    for port in (8089, 5178):
        with socket.socket() as probe:
            probe.bind(("127.0.0.1", port))
    names = hr.tables(state["database_container"], DATABASE)
    before = hr.snapshot(state["database_container"], DATABASE, names)
    totals = finance_totals(hr, state["database_container"], DATABASE)
    for kind in ("backend", "frontend"):
        name = "goodhr-finance-" + kind + "-candidate-20261011"
        state["candidates"][kind] = {"name": name, "id": None, "image": manifest["images"][kind]["id"]}
        hr.save(ROOT, "rehearsal.private.json", state)
        identity = hr.create_application(ROOT, kind, name, manifest["images"][kind]["id"], candidate=True)
        state["candidates"][kind]["id"] = identity
        hr.save(ROOT, "rehearsal.private.json", state)
        hr.start_container(identity)
        hr.wait_http("http://127.0.0.1:8089/health" if kind == "backend" else "http://127.0.0.1:5178/login")
    verify_http(hr, "http://127.0.0.1:8089", "http://127.0.0.1:5178")
    if before != hr.snapshot(state["database_container"], DATABASE, names) or totals != finance_totals(hr, state["database_container"], DATABASE) or hr.ledger(state["database_container"], DATABASE) != manifest["ledger"]:
        raise RuntimeError("候选启动改变恢复库行或98项ledger，未通过验收")
    verify_baseline(hr)
    result = {"ready": True, "commit": args.commit, "images": manifest["images"], "ledger_count": 98, "clone_rows_unchanged": True, "finance_totals": totals}
    hr.save(ROOT, "rehearsal-result.private.json", result)
    print(json.dumps(result))


def rollback(hr, args):
    """只恢复原应用容器，不恢复全库，不改变正式数据、迁移记录或PG/Redis进程。"""
    verify_baseline(hr, apps=False)
    manifest = hr.load(ROOT, "manifest.private.json")
    if manifest["commit"] != args.commit:
        raise RuntimeError("回退提交不符合本轮材料")
    state = hr.load(ROOT, "cutover-progress.private.json")
    for kind in reversed(state["changed"]):
        name = "goodhr-production-" + kind + "-1"
        old = BASELINE[name]
        backup = name + "-backup-" + ROOT.name
        current = hr.existing(name)
        if current and current["Id"] != old[0]:
            recognize(hr, manifest, kind, current, state["new"].get(kind))
            failed = name + "-failed-" + ROOT.name + "-" + current["Id"][:12]
            if hr.existing(failed):
                raise RuntimeError("失败容器保留名已占用")
            hr.intent(ROOT, state, "rollback_stop_" + kind)
            if current["State"]["Running"]:
                hr.run(["docker", "stop", "-t", "20", current["Id"]], timeout=45)
            hr.run(["docker", "rename", current["Id"], failed])
            current = None
        if current is None:
            saved = hr.existing(backup)
            if not saved or (saved["Id"], saved["Image"]) != old[:2]:
                raise RuntimeError("旧应用身份不符合备份")
            hr.intent(ROOT, state, "rollback_restore_" + kind)
            hr.run(["docker", "rename", saved["Id"], name])
            current = hr.inspect(name)
        if (current["Id"], current["Image"]) != old[:2]:
            raise RuntimeError("原应用身份变化")
        if not current["State"]["Running"]:
            hr.start_container(current["Id"])
        hr.wait_http("http://127.0.0.1:8086/health" if kind == "backend" else "http://127.0.0.1:5175/login")
    state["rolled_back"] = True
    hr.intent(ROOT, state, "rollback_complete")
    hr.save(ROOT, "rollback-result.private.json", {"restored": True, "database_restored": False, "database_written": False})


def cutover(hr, args):
    """候选验收通过后仅切换两应用，任何异常按精确ID回退，正式SQL始终只读。"""
    verify_baseline(hr)
    manifest = materials(hr, args.commit)
    ready = hr.load(ROOT, "rehearsal-result.private.json")
    if not ready.get("ready") or ready["commit"] != args.commit or ready["images"] != manifest["images"] or (ROOT / "cutover-progress.private.json").exists():
        raise RuntimeError("缺少本轮验收或已有切换现场")
    for kind in ("backend", "frontend"):
        if hr.existing("goodhr-production-" + kind + "-1-backup-" + ROOT.name):
            raise RuntimeError("备份应用名已占用")
    state = {"changed": [], "new": {}, "rolled_back": False}
    hr.intent(ROOT, state, "begin_cutover")
    try:
        for kind in ("backend", "frontend"):
            name = "goodhr-production-" + kind + "-1"
            # 在停止每个原容器前仍按精确ID、镜像、启动时间验证，防止阶段间并行发布。
            old = hr.inspect(name)
            if (old["Id"], old["Image"], old["State"]["StartedAt"]) != BASELINE[name] or not old["State"]["Running"]:
                raise RuntimeError("切换阶段原应用漂移")
            state["changed"].append(kind)
            hr.intent(ROOT, state, "stop_old_" + kind)
            hr.run(["docker", "stop", "-t", "20", old["Id"]], timeout=45)
            hr.intent(ROOT, state, "rename_old_" + kind)
            hr.run(["docker", "rename", old["Id"], name + "-backup-" + ROOT.name])
            hr.intent(ROOT, state, "create_new_" + kind)
            identity = hr.create_application(ROOT, kind, name, manifest["images"][kind]["id"])
            state["new"][kind] = identity
            hr.intent(ROOT, state, "start_new_" + kind)
            hr.start_container(identity)
            hr.wait_http("http://127.0.0.1:8086/health" if kind == "backend" else "http://127.0.0.1:5175/login")
        verify_http(hr, "http://127.0.0.1:8086", "http://127.0.0.1:5175", public=True)
        verify_baseline(hr, apps=False)
        for kind in ("backend", "frontend"):
            item = hr.inspect("goodhr-production-" + kind + "-1")
            recognize(hr, manifest, kind, item, state["new"][kind])
            if not item["State"]["Running"]:
                raise RuntimeError("新应用未保持运行")
        if hr.ledger("postgres", "goodhr5") != manifest["ledger"]:
            raise RuntimeError("新应用启动后ledger改变")
        state["complete"] = True
        hr.intent(ROOT, state, "cutover_complete")
        result = {"complete": True, "commit": args.commit, "new_container_ids": state["new"], "images": manifest["images"], "production_migrations": 0, "ledger_count": 98, "database_redis_unchanged": True, "finance_totals": finance_totals(hr, "postgres", "goodhr5")}
        hr.save(ROOT, "deployment-result.private.json", result)
        print(json.dumps(result))
    except BaseException:
        try:
            rollback(hr, args)
        except BaseException:
            raise RuntimeError("发布及自动回退未完成；保留现场，禁止全库恢复或误动外来容器") from None
        raise RuntimeError("发布未完成，已恢复原应用，数据库没有迁移或恢复") from None


def cleanup(hr, args):
    """仅移除本轮记录且身份完全一致的候选容器与独立PG，保留dump和数据目录。"""
    verify_baseline(hr, apps=False)
    manifest = hr.load(ROOT, "manifest.private.json")
    if manifest["commit"] != args.commit:
        raise RuntimeError("清理提交不符合本轮材料")
    state = hr.load(ROOT, "rehearsal.private.json")
    if state["database"] != DATABASE or state["database_container"] != CLONE:
        raise RuntimeError("独立恢复库名不符合本轮范围")
    targets = list(reversed(list(state["candidates"].values()))) + [{"name": state["database_container"], "id": state["database_id"], "image": BASELINE["postgres"][1]}]
    removed = []
    for value in targets:
        if value["name"] in BASELINE or (not value["name"].startswith("goodhr-finance-") and value["name"] != state["database_container"]):
            raise RuntimeError("清理目标包含非本轮服务")
        item = hr.existing(value["name"])
        if not item:
            removed.append(value["name"])
            continue
        labels = item["Config"].get("Labels") or {}
        if item["Image"] != value["image"] or (value["id"] and item["Id"] != value["id"]) or labels.get("goodhr.release") != ROOT.name:
            raise RuntimeError("候选身份变化，不进行清理")
        if value["name"] != CLONE:
            kind = next((key for key, entry in state["candidates"].items() if entry["name"] == value["name"]), None)
            if not kind:
                raise RuntimeError("候选应用不在本轮记录内")
            recognize(hr, manifest, kind, item, value["id"])
        value["id"] = item["Id"]
        hr.save(ROOT, "rehearsal.private.json", state)
        if item["State"]["Running"]:
            hr.run(["docker", "stop", "-t", "10", item["Id"]], timeout=30)
        hr.run(["docker", "rm", item["Id"]])
        removed.append(value["name"])
    state["cleaned"] = True
    hr.save(ROOT, "rehearsal.private.json", state)
    hr.save(ROOT, "cleanup-result.private.json", {"removed_containers": removed, "isolated_data_retained": True, "dump_retained": True})
    print(json.dumps({"cleaned": True, "removed_containers": removed}))


def interrupted(signum, frame):
    """将正常停止转换为异常，使切换阶段保留进度并尝试恢复旧容器。"""
    raise RuntimeError("发布进程收到停止信号")


def main():
    """固定本轮根盘目录，按完整提交和共享发布锁执行明确阶段。"""
    import fcntl
    parser = argparse.ArgumentParser(description="应用发布，无生产迁移")
    parser.add_argument("operation", choices=("prepare", "rehearse", "cutover", "rollback", "cleanup"))
    parser.add_argument("--commit", required=True)
    parser.add_argument("--clone-container")
    parser.add_argument("--clone-id")
    args = parser.parse_args()
    if not re.fullmatch(r"[0-9a-f]{40}", args.commit) or ROOT.is_symlink() or ROOT.resolve() != ROOT:
        raise RuntimeError("提交或固定目录不符合约束")
    os.umask(0o077)
    ROOT.mkdir(mode=0o700, parents=True, exist_ok=True)
    ROOT.chmod(0o700)
    if os.stat(ROOT).st_dev != os.stat("/").st_dev:
        raise RuntimeError("发布目录必须位于根盘")
    signal.signal(signal.SIGTERM, interrupted)
    with (ROOT.parent / ".hr-interview-release.lock").open("a") as lock:
        os.chmod(lock.name, 0o600)
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        hr = launcher()
        {"prepare": prepare, "rehearse": rehearse, "cutover": cutover, "rollback": rollback, "cleanup": cleanup}[args.operation](hr, args)


if __name__ == "__main__":
    try:
        main()
    except Exception as cause:
        print(json.dumps({"success": False, "error": str(cause)}, ensure_ascii=False))
        raise SystemExit(1)
