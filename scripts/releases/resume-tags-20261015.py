"""文件作用：为简历标签、岗位开关和猎头推荐执行固定主机发布、备份、隔离迁移演练、应用切换及插件配置更新。"""

import argparse
import fcntl
import hashlib
import importlib.util
import json
import os
import pathlib
import re
import shutil
import signal
import socket
import subprocess
import tarfile

LAUNCHER = pathlib.Path('/opt/goodhr-release/hr-interview-20261010-63ad61cd/hr-release.py')
LAUNCHER_SHA256 = 'f23375cf68107eb2ed21637d38b2462f7346c14b82ab9a3d069893ea700fef0c'
ROOT = pathlib.Path('/opt/goodhr-release/resume-tags-20261015')
MIGRATIONS = {'0094_candidate_tags.sql', '0095_position_open.sql'}
NEW_TABLES = {'candidate_tags', 'candidate_tag_links'}
BASELINE = {
    'goodhr-production-backend-1': ('a298df5989c357d538f0089db9de2fa7c767d419eb3bf0aa295e75574f8d65f8', 'sha256:b5c137a51bc0ec8fc8586dbd10d72dee47dbec8c4db06102bfe4e0cc37cc90f3', '2026-10-14T16:52:20.35632474Z'),
    'goodhr-production-frontend-1': ('f1cb42105172a64d0176ce6877cdee796038ee325911549a3308b6a5ad7844d7', 'sha256:92c174e899a46404fdb0edc681b0ca0bd5a1a414c02816208d40b7eff7496823', '2026-10-14T16:52:22.286412332Z'),
    'postgres': ('7ae7305d3a222272d23cf4a68ae302cf877fc71d2ce1fab9ad3a97b3fe02d89d', 'sha256:413d57cb67bb3585acae8769a48661aa1dcac66076e3a5f88ea6c5be55f8916e', '2026-09-20T12:25:51.09539213Z'),
    'redis': ('78a704cd2eb89019c62bfb6ab2ceef603bf2cd293fe3d522ca7c132a1da72025', 'sha256:646a47c903c5c45f99081094d8c3ba51ec2a1091babf42dc5a928cef024e6fcd', '2026-09-20T12:28:59.032970342Z'),
}


def load_launcher():
    """核对可复用启动器原文字节，适配本轮表范围、隔离端口和完整业务数据校验。"""
    if LAUNCHER.is_symlink() or hashlib.sha256(LAUNCHER.read_bytes()).hexdigest() != LAUNCHER_SHA256:
        raise RuntimeError('复用发布启动器摘要不一致')
    spec = importlib.util.spec_from_file_location('resume_tags_launcher', LAUNCHER)
    hr = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(hr)
    hr.BASELINE = BASELINE
    hr.ALLOWED = MIGRATIONS
    hr.NEW_TABLES = NEW_TABLES
    hr.VERIFY_DATABASE = 'goodhr_resume_tags_verify'
    hr.PORTS = (55441, 8089, 5182)
    original_wait = hr.wait_http
    original_payload = hr.application_payload

    def wait_http(url, expected=200):
        """仅替换本次隔离前端端口，正式健康检查仍使用实际现有映射。"""
        return original_wait(url.replace('127.0.0.1:5178', '127.0.0.1:5182'), expected)

    def application_payload(root, kind, image, candidate=False):
        """完整复用原容器参数，隔离环境禁用邮件并使用空白Redis和独立数据库。"""
        payload = original_payload(root, kind, image, candidate)
        if candidate:
            env = dict(item.split('=', 1) for item in payload['Env'])
            if kind == 'frontend':
                env['PORT'] = '5182'
            else:
                env['GOODHR_PUBLIC_BASE_URL'] = 'http://127.0.0.1:5182'
            payload['Env'] = [key + '=' + value for key, value in env.items()]
        return payload

    def validate_new_migrations(texts):
        """只接受归档和候选镜像一致的两项审定迁移，禁止嵌入提交、回滚和删除原资料。"""
        if set(texts) != MIGRATIONS:
            raise RuntimeError('本轮迁移范围不一致')
        for text in texts.values():
            clean = '\n'.join(line for line in text.splitlines() if not line.lstrip().startswith('--')).upper()
            if any(term in clean for term in ('DROP TABLE', 'TRUNCATE ', 'DELETE FROM', 'COMMIT;', 'ROLLBACK;')):
                raise RuntimeError('迁移出现未审定的删除或独立事务操作')

    def snapshot(container, database, names):
        """对旧业务全字段做摘要，岗位只剔除本次新增开关以比较原有字段完全不变。"""
        queries = []
        for name in sorted(names):
            row = "(to_jsonb(t)-'is_open')" if name == 'positions' else 'to_jsonb(t)'
            queries.append("SELECT %s AS name,jsonb_build_object('rows',count(*),'fingerprint',md5(COALESCE(string_agg(md5(%s::text),'' ORDER BY md5(%s::text)),''))) AS value FROM %s t" % (hr.quote(name), row, row, hr.identifier(name)))
        query = 'BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SELECT jsonb_object_agg(name,value) FROM (' + ' UNION ALL '.join(queries) + ') s; COMMIT;'
        return json.loads(next(line for line in hr.sql(container, database, query).splitlines() if line.startswith('{')))

    def migration_sql(root):
        """行锁校验旧迁移清单后事务执行新增标签和岗位开关，保留原简历和历史迁移行。"""
        manifest = hr.load(root, 'manifest.private.json')
        texts = hr.load(root, 'migrations.private.json')
        validate_new_migrations(texts)
        expected = hr.quote(json.dumps(sorted(manifest['ledger']))) + '::jsonb'
        commands = ['BEGIN; LOCK TABLE schema_migrations IN EXCLUSIVE MODE;', "SELECT pg_advisory_xact_lock(hashtext('goodhr-resume-tags-release'));", "DO $guard$ BEGIN IF (SELECT jsonb_agg(filename ORDER BY filename) FROM schema_migrations) <> " + expected + " THEN RAISE EXCEPTION 'migration ledger changed'; END IF; END; $guard$;"]
        commands.append('LOCK TABLE ' + ','.join(hr.identifier(name) for name in manifest['tables'] if name != 'schema_migrations') + ' IN SHARE MODE;')
        for name in sorted(MIGRATIONS):
            commands += [texts[name], 'INSERT INTO schema_migrations(filename) VALUES (' + hr.quote(name) + ');']
        return '\n'.join(commands + ['COMMIT;'])

    def verify_migrated(root, container, database, before, old_hash):
        """验证全部旧行和迁移记录不变、旧岗位默认开启、旧标签已完整保留关联。"""
        manifest = hr.load(root, 'manifest.private.json')
        expected = sorted(set(manifest['ledger']) | MIGRATIONS)
        if set(hr.tables(container, database)) != set(manifest['tables']) | NEW_TABLES or hr.ledger(container, database) != expected:
            raise RuntimeError('迁移后的表或记录范围不一致')
        if snapshot(container, database, set(manifest['tables']) - {'schema_migrations'}) != before or hr.old_ledger_snapshot(container, database, manifest['ledger']) != old_hash:
            raise RuntimeError('原业务资料或历史迁移行发生变化')
        closed = int(hr.sql(container, database, 'SELECT count(*) FROM positions WHERE NOT is_open;'))
        if closed:
            raise RuntimeError('旧岗位未全部默认开启')
        missing = int(hr.sql(container, database, "SELECT count(*) FROM candidate_profiles cp JOIN users u ON u.id=cp.created_by_user_id CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(cp.library_fields->'tags')='array' THEN cp.library_fields->'tags' ELSE '[]'::jsonb END) a(value) WHERE jsonb_typeof(a.value)='string' AND char_length(btrim(a.value #>> '{}')) BETWEEN 1 AND 50 AND NOT EXISTS(SELECT 1 FROM candidate_tags t JOIN candidate_tag_links l ON l.tag_id=t.id WHERE l.candidate_id=cp.id AND t.tenant_id=cp.tenant_id AND t.scope='personal' AND t.created_by_email=u.email AND lower(t.name)=lower(btrim(a.value #>> '{}')));"))
        if missing:
            raise RuntimeError('旧标签回填存在缺失')
        counts = {name: int(hr.sql(container, database, 'SELECT count(*) FROM ' + hr.identifier(name))) for name in NEW_TABLES}
        return {'old_tables_verified': len(before), 'old_rows_unchanged': True, 'old_ledger_unchanged': True, 'ledger_count': len(expected), 'old_positions_open': True, 'legacy_tags_missing': missing, 'new_tables': counts}

    def verify_http(backend, frontend):
        """检查简历、标签和岗位接口匿名边界及更新页面，避免把公开页面200当作接口权限验证。"""
        wait_http(backend + '/health')
        for path in ('/api/candidates', '/api/candidates/tags', '/api/candidate-recommendations', '/api/positions?purpose=recommendation', '/api/positions?is_open=false'):
            wait_http(backend + path, 401)
        for path in ('/login', '/admin/resumes', '/admin/recommendations', '/admin/positions', '/download'):
            wait_http(frontend + path)

    hr.wait_http = wait_http
    hr.application_payload = application_payload
    hr.validate_new_migrations = validate_new_migrations
    hr.snapshot = snapshot
    hr.migration_sql = migration_sql
    hr.verify_migrated = verify_migrated
    hr.verify_http = verify_http
    return hr


def build(hr, args):
    """核对Git归档和Linux二进制，使用现有运行镜像构建候选镜像，尚不操作正式应用。"""
    hr.verify_baseline()
    archive, binary = pathlib.Path(args.archive), pathlib.Path(args.binary)
    if not re.fullmatch(r'[0-9a-f]{40}', args.commit or ''):
        raise RuntimeError('发布必须指定完整Git提交')
    if hr.digest(archive) != args.archive_sha256 or hr.digest(binary) != args.binary_sha256:
        raise RuntimeError('上传材料摘要不一致')
    with binary.open('rb') as stream:
        if stream.read(4) != b'\x7fELF':
            raise RuntimeError('后端二进制必须为Linux ELF')
    if archive.stat().st_size > 300 * 1024 ** 2 or shutil.disk_usage(ROOT).free < 3 * 1024 ** 3:
        raise RuntimeError('源码归档大小异常或构建空间不足')
    if (ROOT / 'build.private.json').exists() or (ROOT / 'source').exists():
        raise RuntimeError('已有构建现场，禁止重跑覆盖')
    shutil.copyfile(archive, ROOT / 'source.tar')
    shutil.copyfile(binary, ROOT / 'goodhr-server')
    source = ROOT / 'source'
    source.mkdir()
    with tarfile.open(archive) as stream:
        if stream.pax_headers.get('comment') != args.commit:
            raise RuntimeError('Git源码归档提交不匹配')
        for member in stream.getmembers():
            path = pathlib.PurePosixPath(member.name)
            if path.is_absolute() or '..' in path.parts or not (member.isfile() or member.isdir()):
                raise RuntimeError('源码归档路径或链接不安全')
        stream.extractall(source)
    backend = source / 'goodhr5/cloud/backend'
    frontend = source / 'goodhr5/cloud/frontend-next'
    shutil.copyfile(ROOT / 'goodhr-server', backend / 'goodhr-server')
    (backend / 'goodhr-server').chmod(0o700)
    backend_docker = ['FROM ' + BASELINE['goodhr-production-backend-1'][1], 'LABEL org.opencontainers.image.revision=' + args.commit, 'COPY goodhr-server /usr/local/bin/goodhr-server', 'COPY db /app/db', 'COPY templates /app/templates']
    (backend / 'Dockerfile.tags').write_text('\n'.join(backend_docker) + '\n')
    tags = {kind: 'goodhr-production-' + kind + ':resume-tags-' + args.commit[:8] for kind in ('backend', 'frontend')}
    with (ROOT / 'build.private.log').open('xb') as log:
        hr.run(['docker', 'build', '--network=none', '-f', str(backend / 'Dockerfile.tags'), '-t', tags['backend'], str(backend)], output=log, timeout=900)
        command = 'npm ci --include=dev --registry=https://registry.npmmirror.com --no-audit --no-fund && npm run build'
        hr.run(['docker', 'run', '--rm', '--memory=2048m', '--memory-swap=3072m', '--cpus=2', '--name', 'goodhr-tags-build-' + args.commit[:8], '--entrypoint', 'sh', '--mount', 'type=bind,src=' + str(frontend) + ',dst=/work', '-w', '/work', '-e', 'NODE_ENV=production', '-e', 'NODE_OPTIONS=--max-old-space-size=1536', '-e', 'CIRCLE_NODE_TOTAL=2', '-e', 'NEXT_PUBLIC_CLOUD_API_BASE=https://goodhr.58it.cn', '-e', 'CLOUD_API_BASE=https://goodhr.58it.cn', '-e', 'NEXT_PUBLIC_SITE_URL=https://goodhr.58it.cn', BASELINE['goodhr-production-frontend-1'][1], '-c', command], output=log, timeout=1800)
        runtime = ROOT / 'frontend-runtime'
        runtime.mkdir(mode=0o700)
        shutil.copytree(frontend / '.next/standalone', runtime / 'standalone')
        shutil.copytree(frontend / '.next/static', runtime / 'static')
        shutil.copytree(frontend / 'public', runtime / 'public')
        docker = ['FROM ' + BASELINE['goodhr-production-frontend-1'][1], 'LABEL org.opencontainers.image.revision=' + args.commit, 'COPY standalone /app/', 'COPY static /app/.next/static', 'COPY public /app/public']
        (runtime / 'Dockerfile').write_text('\n'.join(docker) + '\n')
        hr.run(['docker', 'build', '--network=none', '-t', tags['frontend'], str(runtime)], output=log, timeout=900)
    images = {kind: {'id': hr.inspect(tag, image=True)['Id'], 'tag': tag} for kind, tag in tags.items()}
    proof = {'commit': args.commit, 'source_sha256': args.archive_sha256, 'binary_sha256': args.binary_sha256, 'images': images}
    hr.save(ROOT, 'build.private.json', proof)
    print(json.dumps({'build': 'complete', **proof}))


def prepare(hr):
    """保存精确生产基线、校验两项新增迁移正文，并完成可恢复的完整PostgreSQL备份。"""
    current = hr.verify_baseline()
    proof = hr.load(ROOT, 'build.private.json')
    if (ROOT / 'manifest.private.json').exists() or (ROOT / 'database.dump.partial').exists():
        raise RuntimeError('已有准备材料，禁止覆盖')
    source = hr.migrations_from_tar((ROOT / 'source.tar').read_bytes(), 'goodhr5/cloud/backend/db/migrations/')
    previous = hr.image_migrations(BASELINE['goodhr-production-backend-1'][1])
    candidate = hr.image_migrations(proof['images']['backend']['id'])
    applied = hr.ledger('postgres', 'goodhr5')
    if len(applied) != 100 or set(applied) != set(previous) or candidate != source or set(candidate) - set(previous) != MIGRATIONS or any(candidate.get(name) != text for name, text in previous.items()):
        raise RuntimeError('100项旧迁移、归档和候选镜像正文不一致')
    texts = {name: candidate[name] for name in sorted(MIGRATIONS)}
    hr.validate_new_migrations(texts)
    old_tables = hr.tables('postgres', 'goodhr5')
    if NEW_TABLES.intersection(old_tables):
        raise RuntimeError('正式库已有新标签表，与发布基线不符')
    size = int(hr.sql('postgres', 'goodhr5', 'SELECT pg_database_size(current_database());'))
    if shutil.disk_usage(ROOT).free < size * 2 + 300 * 1024 ** 2:
        raise RuntimeError('根盘不足以备份和恢复演练')
    hr.verify_ports()
    hr.save(ROOT, 'before-inspect.private.json', current)
    hr.save(ROOT, 'migrations.private.json', texts)
    hr.save(ROOT, 'extension-before.private.json', json.loads(hr.sql('postgres', 'goodhr5', "SELECT row_to_json(s) FROM system_configs s WHERE config_key='system.browser_extension';")))
    dump = ROOT / 'database.dump.partial'
    with dump.open('xb') as output:
        hr.run(['docker', 'exec', 'postgres', 'sh', '-c', 'exec pg_dump -U "$POSTGRES_USER" -d goodhr5 -Fc --no-owner --no-acl'], output=output, timeout=900)
        output.flush()
        os.fsync(output.fileno())
    with dump.open('rb') as stream:
        catalog = hr.run(['docker', 'exec', '-i', 'postgres', 'pg_restore', '--list'], input_file=stream)
    if not catalog or dump.read_bytes()[:5] != b'PGDMP':
        raise RuntimeError('完整备份未通过恢复目录检查')
    dump.replace(ROOT / 'database.dump')
    manifest = {'commit': proof['commit'], 'images': proof['images'], 'ledger': applied, 'tables': old_tables, 'database_bytes': size, 'source_sha256': proof['source_sha256'], 'migration_sha256': {name: hashlib.sha256(text.encode()).hexdigest() for name, text in candidate.items()}, 'dump': {'bytes': (ROOT / 'database.dump').stat().st_size, 'sha256': hr.digest(ROOT / 'database.dump')}}
    hr.save(ROOT, 'manifest.private.json', manifest)
    hr.verify_baseline(ROOT)
    print(json.dumps({'prepare': 'complete', 'dump': manifest['dump'], 'pending': sorted(MIGRATIONS)}))


def publish_config(hr):
    """发布Chrome下载版本和摘要，事务校验原配置且保留Firefox签名包和全部未知字段。"""
    if not hr.load(ROOT, 'deployment-result.private.json').get('complete'):
        raise RuntimeError('正式应用尚未完成切换')
    old = hr.load(ROOT, 'extension-before.private.json')
    package = ROOT / 'source/goodhr5/cloud/frontend-next/public/downloads/goodhr-extension-chrome-1.0.8.zip'
    sha = hr.digest(package)
    desired = {'version': '1.0.8', 'version_chrome': '1.0.8', 'version_firefox': old['config_value'].get('version_firefox') or old['config_value']['version'], 'note': 'Chrome v1.0.8：标签可搜索选择，推荐只显示开启的岗位。Firefox 当前签名版保留，新版待上传。', 'url_chrome': 'https://goodhr.58it.cn/downloads/goodhr-extension-chrome-1.0.8.zip', 'sha256_chrome': sha}
    desired['agent_compatibility'] = {key: old['config_value'][key] for key in ('version', 'note', 'url_chrome', 'url_firefox', 'sha256_chrome', 'sha256_firefox', 'firefox_signed') if key in old['config_value']}
    sql = "BEGIN; SELECT config_key FROM system_configs WHERE config_key='system.browser_extension' FOR UPDATE; DO $guard$ BEGIN IF (SELECT to_jsonb(s) FROM system_configs s WHERE config_key='system.browser_extension') IS DISTINCT FROM " + hr.quote(json.dumps(old, ensure_ascii=False)) + "::jsonb THEN RAISE EXCEPTION 'extension config changed'; END IF; END; $guard$; UPDATE system_configs SET config_value=config_value||" + hr.quote(json.dumps(desired, ensure_ascii=False)) + "::jsonb,updated_at=now() WHERE config_key='system.browser_extension'; COMMIT;"
    hr.sql('postgres', 'goodhr5', sql, readonly=False)
    current = json.loads(hr.sql('postgres', 'goodhr5', "SELECT row_to_json(s) FROM system_configs s WHERE config_key='system.browser_extension';"))
    if any(current['config_value'].get(key) != value for key, value in desired.items()):
        raise RuntimeError('发布配置未按预期保存')
    if any(current['config_value'].get(k) != v for k, v in old['config_value'].items() if k not in desired):
        raise RuntimeError('Firefox或未知配置字段意外变化')
    hr.save(ROOT, 'extension-after.private.json', current)
    hr.save(ROOT, 'extension-publish-result.private.json', {'complete': True, **desired, 'firefox_fields_preserved': True})
    print(json.dumps({'extension_config': 'complete', 'version_chrome': desired['version_chrome'], 'version_firefox': desired['version_firefox'], 'url_chrome': desired['url_chrome'], 'sha256_chrome': sha, 'firefox_fields_preserved': True, 'legacy_agent_compatible': True}, ensure_ascii=False))


def rollback_config(hr):
    """在配置整行仍等于本次发布结果时恢复发布前字段，拒绝覆盖其他人的后续更新。"""
    before = hr.load(ROOT, 'extension-before.private.json')
    after = hr.load(ROOT, 'extension-after.private.json')
    sql = "BEGIN; SELECT config_key FROM system_configs WHERE config_key='system.browser_extension' FOR UPDATE; DO $guard$ BEGIN IF (SELECT to_jsonb(s) FROM system_configs s WHERE config_key='system.browser_extension') IS DISTINCT FROM " + hr.quote(json.dumps(after, ensure_ascii=False)) + "::jsonb THEN RAISE EXCEPTION 'extension config changed since release'; END IF; END; $guard$; UPDATE system_configs SET config_value=" + hr.quote(json.dumps(before['config_value'], ensure_ascii=False)) + "::jsonb,enabled=" + ('true' if before['enabled'] else 'false') + ",updated_at=now() WHERE config_key='system.browser_extension'; COMMIT;"
    hr.sql('postgres', 'goodhr5', sql, readonly=False)
    hr.save(ROOT, 'extension-rollback-result.private.json', {'restored': True, 'database_restored': False})
    print(json.dumps({'extension_config': 'restored', 'database_restored': False}))


def main():
    """仅在固定目标主机和根盘目录执行本次阶段操作，现有生产回退入口来自已核对启动器。"""
    parser = argparse.ArgumentParser()
    parser.add_argument('operation', choices=['build', 'prepare', 'rehearse', 'cutover', 'publish-config', 'rollback-config', 'rollback', 'cleanup'])
    for name in ('commit', 'archive', 'archive-sha256', 'binary', 'binary-sha256'):
        parser.add_argument('--' + name)
    args = parser.parse_args()
    if socket.gethostname() != 'ldclouda31539':
        raise RuntimeError('发布主机不匹配')
    os.umask(0o077)
    ROOT.mkdir(mode=0o700, exist_ok=True)
    if ROOT.is_symlink() or ROOT.resolve() != ROOT or ROOT.stat().st_dev != pathlib.Path('/').stat().st_dev:
        raise RuntimeError('发布目录必须为固定根盘目录')
    with pathlib.Path('/www/wwwroot/goodhr-production/backups/.goodhr-container-deploy.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        hr = load_launcher()
        signal.signal(signal.SIGTERM, hr.interrupt_release)
        if args.operation == 'build':
            build(hr, args)
        elif args.operation == 'prepare':
            prepare(hr)
        elif args.operation == 'publish-config':
            publish_config(hr)
        elif args.operation == 'rollback-config':
            rollback_config(hr)
        else:
            getattr(hr, args.operation)(ROOT)


if __name__ == '__main__':
    main()
