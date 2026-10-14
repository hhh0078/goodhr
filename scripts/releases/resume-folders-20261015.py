"""文件作用：为原始简历标签与收藏夹分离、招聘与个人配置页面优化执行固定主机发布、备份、隔离迁移演练、应用切换及插件配置更新。"""

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
ROOT = pathlib.Path('/opt/goodhr-release/resume-folders-20261015')
BUILD_ROOT = pathlib.Path('/www/wwwroot/goodhr-production/releases/resume-folders-20261015')
MIGRATION_NAME = '0096_separate_resume_folders.sql'
MIGRATIONS = {MIGRATION_NAME}
NEW_TABLES = set()
FOLDER_TABLES = {'candidate_tags', 'candidate_tag_links'}
BASELINE = {
    'goodhr-production-backend-1': ('4f5b6c2f8039d42b7a0827e4cd8782618c7f35eade5714881cfee49a845429e8', 'sha256:83a686b5a93abe1f395c28aba073e4487405a79ad8b17c5310872dc945cd57c9', '2026-10-14T17:07:58.247730531Z'),
    'goodhr-production-frontend-1': ('1ce7b90ac5e7201a64b15a8ba5be4474b71089b875f32c0da324be2d97e9b71f', 'sha256:1cf3a428477eeebdcb422050a7bdbafddeb69da8048b72063545d2825f1f9af6', '2026-10-14T17:08:01.291230484Z'),
    'postgres': ('7ae7305d3a222272d23cf4a68ae302cf877fc71d2ce1fab9ad3a97b3fe02d89d', 'sha256:413d57cb67bb3585acae8769a48661aa1dcac66076e3a5f88ea6c5be55f8916e', '2026-09-20T12:25:51.09539213Z'),
    'redis': ('78a704cd2eb89019c62bfb6ab2ceef603bf2cd293fe3d522ca7c132a1da72025', 'sha256:646a47c903c5c45f99081094d8c3ba51ec2a1091babf42dc5a928cef024e6fcd', '2026-09-20T12:28:59.032970342Z'),
}


def load_launcher():
    """核对可复用启动器原文字节，适配本轮表范围、隔离端口和完整业务数据校验。"""
    if LAUNCHER.is_symlink() or hashlib.sha256(LAUNCHER.read_bytes()).hexdigest() != LAUNCHER_SHA256:
        raise RuntimeError('复用发布启动器摘要不一致')
    spec = importlib.util.spec_from_file_location('resume_folders_launcher', LAUNCHER)
    hr = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(hr)
    hr.BASELINE = BASELINE
    hr.ALLOWED = MIGRATIONS
    hr.NEW_TABLES = NEW_TABLES
    hr.VERIFY_DATABASE = 'goodhr_resume_folders_verify'
    hr.PORTS = (55441, 8089, 5182)
    original_wait = hr.wait_http
    original_payload = hr.application_payload
    original_rollback = hr.rollback
    original_rehearse = hr.rehearse
    original_cutover = hr.cutover

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
        """只接受固定归档的0096，删除仅作用于明确旧自动分类，不改原简历行。"""
        if set(texts) != MIGRATIONS:
            raise RuntimeError('本轮迁移范围不一致')
        reviewed = (BUILD_ROOT / 'source/goodhr5/cloud/backend/db/migrations' / MIGRATION_NAME).read_text()
        if texts[MIGRATION_NAME] != reviewed:
            raise RuntimeError('0096迁移与固定提交源文件不一致')
        clean = '\n'.join(line for line in reviewed.splitlines() if not line.lstrip().startswith('--')).upper()
        targets = re.findall(r'\bDELETE\s+FROM\s+([A-Z_]+)', clean)
        updates = re.findall(r'\bUPDATE\s+([A-Z_]+)', clean)
        if targets != ['CANDIDATE_TAGS'] or updates != ['CANDIDATE_TAGS'] or any(term in clean for term in ('DROP TABLE', 'TRUNCATE ', 'COMMIT;', 'ROLLBACK;')):
            raise RuntimeError('0096出现审定范围以外的数据操作')
        if "SOURCE_KIND='LEGACY_RESUME_TAG'" not in clean or 'UPDATED_AT=T.CREATED_AT' not in clean:
            raise RuntimeError('0096缺少来源或未人工编辑条件')

    def deletion_query():
        """直接复用已审定SQL来源条件预估清理ID，避免维护第二套删除条件。"""
        text = hr.load(ROOT, 'migrations.private.json')[MIGRATION_NAME]
        mark = text.split("UPDATE candidate_tags t SET source_kind='legacy_resume_tag'\n", 1)[1].split('\n DELETE FROM', 1)[0].strip().rstrip(';')
        mark = mark.replace("t.source_kind='manual'", "COALESCE(to_jsonb(t)->>'source_kind','manual')='manual'")
        mark = mark.replace('legacy_applied_at', "(SELECT applied_at FROM schema_migrations WHERE filename='0094_candidate_tags.sql')").replace('previous_applied_at', "(SELECT max(applied_at) FROM schema_migrations WHERE filename<'0094_candidate_tags.sql')")
        return "SELECT t.id FROM candidate_tags t " + mark + " UNION SELECT t.id FROM candidate_tags t WHERE to_jsonb(t)->>'source_kind'='legacy_resume_tag' AND t.updated_at=t.created_at AND NOT EXISTS(SELECT 1 FROM candidate_tag_links l WHERE l.tag_id=t.id AND l.created_at<>t.created_at)"

    def snapshot(container, database, names):
        """同一只读快照比较全部业务字段，记录收藏夹保留摘要和精确清理范围。"""
        queries = []
        for name in sorted(set(names) - FOLDER_TABLES):
            queries.append("SELECT %s AS name,jsonb_build_object('rows',count(*),'fingerprint',md5(COALESCE(string_agg(md5(to_jsonb(t)::text),'' ORDER BY md5(to_jsonb(t)::text)),''))) AS value FROM %s t" % (hr.quote(name), hr.identifier(name)))
        business = "SELECT jsonb_object_agg(name,value) FROM (" + ' UNION ALL '.join(queries) + ") s"
        folders = "SELECT COALESCE(jsonb_object_agg(id::text,md5((to_jsonb(t)-'source_kind')::text)),'{}'::jsonb) FROM candidate_tags t"
        links = "SELECT COALESCE(jsonb_object_agg(tag_id::text||':'||candidate_id::text,jsonb_build_object('folder',tag_id::text,'hash',md5(to_jsonb(t)::text))),'{}'::jsonb) FROM candidate_tag_links t"
        sources = "SELECT COALESCE(jsonb_object_agg(id::text,COALESCE(to_jsonb(t)->>'source_kind','manual')),'{}'::jsonb) FROM candidate_tags t"
        deletion = "SELECT COALESCE(jsonb_agg(id::text ORDER BY id::text),'[]'::jsonb) FROM (" + deletion_query() + ") d"
        query = "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SELECT jsonb_build_object('business',(" + business + "),'folders',(" + folders + "),'links',(" + links + "),'sources',(" + sources + "),'delete_ids',(" + deletion + ")); COMMIT;"
        return json.loads(next(line for line in hr.sql(container, database, query).splitlines() if line.startswith('{')))

    def migration_sql(root):
        """行锁校验旧迁移清单后事务分离原始标签和手动收藏夹，保留原简历和历史迁移行。"""
        manifest = hr.load(root, 'manifest.private.json')
        texts = hr.load(root, 'migrations.private.json')
        validate_new_migrations(texts)
        expected = hr.quote(json.dumps(sorted(manifest['ledger']))) + '::jsonb'
        commands = ['BEGIN; LOCK TABLE schema_migrations IN EXCLUSIVE MODE;', "SELECT pg_advisory_xact_lock(hashtext('goodhr-resume-folders-release'));", "DO $guard$ BEGIN IF (SELECT jsonb_agg(filename ORDER BY filename) FROM schema_migrations) <> " + expected + " THEN RAISE EXCEPTION 'migration ledger changed'; END IF; END; $guard$;"]
        commands.append('LOCK TABLE ' + ','.join(hr.identifier(name) for name in manifest['tables'] if name != 'schema_migrations') + ' IN SHARE MODE;')
        for name in sorted(MIGRATIONS):
            commands += [texts[name], 'INSERT INTO schema_migrations(filename) VALUES (' + hr.quote(name) + ');']
        return '\n'.join(commands + ['COMMIT;'])

    def verify_migrated(root, container, database, before, old_hash):
        """验证原简历不变，删除只含自动分类，人工收藏夹及关联完整保留。"""
        manifest = hr.load(root, 'manifest.private.json')
        expected = sorted(set(manifest['ledger']) | MIGRATIONS)
        if set(hr.tables(container, database)) != set(manifest['tables']) or hr.ledger(container, database) != expected:
            raise RuntimeError('迁移后的表或记录范围不一致')
        after = snapshot(container, database, set(manifest['tables']) - {'schema_migrations'})
        planned = set(before['delete_ids'])
        folders = {key: value for key, value in before['folders'].items() if key not in planned}
        links = {key: value for key, value in before['links'].items() if value['folder'] not in planned}
        sources = {key: value for key, value in before['sources'].items() if key not in planned}
        if after['business'] != before['business'] or hr.old_ledger_snapshot(container, database, manifest['ledger']) != old_hash:
            raise RuntimeError('原简历或其他业务资料、历史迁移行发生变化')
        if after['folders'] != folders or after['links'] != links or after['sources'] != sources or after['delete_ids']:
            raise RuntimeError('收藏夹清理超出计划或人工资料发生变化')
        return {'old_tables_verified': len(before['business']), 'old_rows_unchanged': True, 'raw_resume_tags_unchanged': True, 'old_ledger_unchanged': True, 'ledger_count': len(expected), 'automatic_folders_removed': len(planned), 'manual_folders_preserved': len(folders), 'manual_links_preserved': len(links)}

    def verify_http(backend, frontend):
        """检查简历、收藏夹和岗位接口匿名边界及更新页面，避免把公开页面200当作接口权限验证。"""
        wait_http(backend + '/health')
        for path in ('/api/candidates', '/api/candidates/folders', '/api/candidate-recommendations', '/api/positions?purpose=recommendation', '/api/positions?is_open=false'):
            wait_http(backend + path, 401)
        for path in ('/login', '/admin/resumes', '/admin/recommendations', '/admin/positions', '/admin/recruitment', '/admin/personal-config', '/download'):
            wait_http(frontend + path)

    def rehearse(root):
        """演练正确新版本及修复标签覆写后的兼容回退后端，二者均只接隔离恢复库。"""
        original_rehearse(root)
        proof = hr.load(root, 'safe-rollback-build.private.json')
        state = hr.load(root, 'rehearsal.private.json')
        name = 'goodhr-hr-safe-backend-candidate-' + hr.load(root, 'manifest.private.json')['commit'][:12]
        if hr.existing(name):
            raise RuntimeError('安全回退演练容器名称已占用')
        payload = application_payload(root, 'backend', proof['image']['id'], candidate=True)
        env = dict(item.split('=', 1) for item in payload['Env'])
        env['GOODHR_CLOUD_ADDR'] = '127.0.0.1:8090'
        payload['Env'] = [key + '=' + value for key, value in env.items()]
        state['candidates']['safe_rollback_backend'] = {'name': name, 'id': None, 'image': proof['image']['id']}
        hr.save(root, 'rehearsal.private.json', state)
        result = hr.docker_request('POST', hr.docker_version() + '/containers/create?name=' + name, payload)
        state['candidates']['safe_rollback_backend']['id'] = result['Id']
        hr.save(root, 'rehearsal.private.json', state)
        hr.start_container(result['Id'])
        wait_http('http://127.0.0.1:8090/health')
        wait_http('http://127.0.0.1:8090/api/candidates', 401)
        wait_http('http://127.0.0.1:8090/api/candidates/tags', 401)
        report = hr.load(root, 'rehearsal-result.private.json')
        report['safe_rollback_backend_verified'] = True
        hr.save(root, 'rehearsal-result.private.json', report)
        print(json.dumps({'safe_rollback_rehearsal': 'passed'}))

    def rollback(root):
        """0096成功后只恢复修正过标签覆写的后端，绝不重启会混写原标签的旧二进制。"""
        if MIGRATION_NAME not in hr.ledger('postgres', 'goodhr5'):
            return original_rollback(root)
        hr.verify_baseline(root, apps=False)
        state = hr.load(root, 'cutover-progress.private.json')
        originals = {item['Name'].lstrip('/'): item for item in hr.load(root, 'before-inspect.private.json')}
        fallback = hr.load(root, 'safe-rollback-build.private.json')['image']['id']
        if (root / 'extension-after.private.json').exists() and not (root / 'extension-rollback-result.private.json').exists():
            rollback_config(hr)
        for kind in reversed(state['changed']):
            name = 'goodhr-production-' + kind + '-1'
            old = originals[name]
            saved_name = name + '-backup-' + root.name
            current = hr.existing(name)
            if kind == 'backend' and current and current['Image'] == fallback:
                labels = current['Config'].get('Labels') or {}
                if labels.get('goodhr.release') != root.name or labels.get('goodhr.source_commit') != hr.load(root, 'manifest.private.json')['commit'] or (state.get('safe_fallback_backend_id') and current['Id'] != state['safe_fallback_backend_id']):
                    raise RuntimeError('安全回退容器身份发生变化')
                state['safe_fallback_backend_id'] = current['Id']
                hr.intent(root, state, 'rollback_recognize_safe_backend')
                if not current['State']['Running']:
                    hr.start_container(current['Id'])
                wait_http('http://127.0.0.1:8086/health')
                continue
            if current and current['Id'] != old['Id']:
                hr.recognize_new(root, kind, current, state['new'].get(kind))
                hr.intent(root, state, 'rollback_preserve_new_' + kind)
                if current['State']['Running']:
                    hr.run(['docker', 'stop', '-t', '20', current['Id']], timeout=45)
                failed = name + '-failed-' + root.name + '-' + current['Id'][:12]
                if hr.existing(failed):
                    raise RuntimeError('失败容器保留名称已占用')
                hr.run(['docker', 'rename', current['Id'], failed])
                state.setdefault('failed', {})[kind] = {'name': failed, 'id': current['Id']}
                current = None
            if kind == 'frontend':
                if current is None:
                    backup = hr.existing(saved_name)
                    if not backup or backup['Id'] != old['Id'] or backup['Image'] != old['Image']:
                        raise RuntimeError('原前端备份身份不符')
                    hr.run(['docker', 'rename', backup['Id'], name])
                    current = hr.inspect(name)
                if current['Id'] != old['Id'] or current['Image'] != old['Image']:
                    raise RuntimeError('原前端身份不符')
                if not current['State']['Running']:
                    hr.start_container(current['Id'])
                wait_http('http://127.0.0.1:5175/login')
            else:
                if current:
                    if current['Id'] != old['Id'] or current['Image'] != old['Image'] or hr.existing(saved_name):
                        raise RuntimeError('原后端保留身份不符')
                    if current['State']['Running']:
                        hr.run(['docker', 'stop', '-t', '20', current['Id']], timeout=45)
                    hr.run(['docker', 'rename', current['Id'], saved_name])
                backup = hr.existing(saved_name)
                if not backup or backup['Id'] != old['Id'] or backup['Image'] != old['Image']:
                    raise RuntimeError('原后端安全保留未确认')
                hr.intent(root, state, 'rollback_create_safe_backend')
                state['safe_fallback_backend_id'] = hr.create_application(root, kind, name, fallback)
                hr.intent(root, state, 'rollback_start_safe_backend')
                hr.start_container(state['safe_fallback_backend_id'])
                wait_http('http://127.0.0.1:8086/health')
        hr.verify_baseline(root, apps=False)
        wait_http('https://goodhr.58it.cn/login')
        state['rolled_back'] = True
        hr.intent(root, state, 'rollback_safe_complete')
        hr.save(root, 'rollback-result.private.json', {'restored': True, 'database_restored': False, 'safe_backend_image': fallback, 'raw_tags_preserved': True, 'ledger_retained': True})
        print(json.dumps({'rollback': 'complete', 'safe_backend': True, 'database_restored': False}))

    def cutover(root):
        """安全回退镜像和隔离启动也必须验证成功，才允许暂停正式应用。"""
        report = hr.load(root, 'rehearsal-result.private.json')
        if not report.get('safe_rollback_backend_verified'):
            raise RuntimeError('缺少安全回退后端演练证据')
        proof = hr.load(root, 'safe-rollback-build.private.json')
        image = hr.inspect(proof['image']['id'], image=True)
        if (image['Config'].get('Labels') or {}).get('org.opencontainers.image.revision') != hr.load(root, 'manifest.private.json')['commit']:
            raise RuntimeError('安全回退镜像发布提交不一致')
        if hr.digest(root / 'safe-rollback-server') != proof['binary_sha256'] or hr.digest(root / 'safe-rollback-source.tar') != proof['source_sha256']:
            raise RuntimeError('安全回退材料摘要不一致')
        return original_cutover(root)

    hr.wait_http = wait_http
    hr.application_payload = application_payload
    hr.validate_new_migrations = validate_new_migrations
    hr.snapshot = snapshot
    hr.migration_sql = migration_sql
    hr.verify_migrated = verify_migrated
    hr.verify_http = verify_http
    hr.PORTS = (55441, 8089, 5182, 8090)
    hr.rehearse = rehearse
    hr.rollback = rollback
    hr.cutover = cutover
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
    if archive.stat().st_size > 300 * 1024 ** 2 or shutil.disk_usage(BUILD_ROOT).free < 3 * 1024 ** 3:
        raise RuntimeError('源码归档大小异常或构建空间不足')
    if (ROOT / 'build.private.json').exists() or (BUILD_ROOT / 'source').exists():
        raise RuntimeError('已有构建现场，禁止重跑覆盖')
    shutil.copyfile(archive, ROOT / 'source.tar')
    shutil.copyfile(binary, ROOT / 'goodhr-server')
    source = BUILD_ROOT / 'source'
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
    (backend / 'Dockerfile.folders').write_text('\n'.join(backend_docker) + '\n')
    tags = {kind: 'goodhr-production-' + kind + ':resume-folders-' + args.commit[:8] for kind in ('backend', 'frontend')}
    with (ROOT / 'build.private.log').open('xb') as log:
        hr.run(['docker', 'build', '--network=none', '-f', str(backend / 'Dockerfile.folders'), '-t', tags['backend'], str(backend)], output=log, timeout=900)
        command = 'npm ci --include=dev --registry=https://registry.npmmirror.com --no-audit --no-fund && npm run build'
        hr.run(['docker', 'run', '--rm', '--memory=2048m', '--memory-swap=3072m', '--cpus=2', '--name', 'goodhr-folders-build-' + args.commit[:8], '--entrypoint', 'sh', '--mount', 'type=bind,src=' + str(frontend) + ',dst=/work', '-w', '/work', '-e', 'NODE_ENV=production', '-e', 'NODE_OPTIONS=--max-old-space-size=1536', '-e', 'CIRCLE_NODE_TOTAL=2', '-e', 'NEXT_PUBLIC_CLOUD_API_BASE=https://goodhr.58it.cn', '-e', 'CLOUD_API_BASE=https://goodhr.58it.cn', '-e', 'NEXT_PUBLIC_SITE_URL=https://goodhr.58it.cn', BASELINE['goodhr-production-frontend-1'][1], '-c', command], output=log, timeout=1800)
        runtime = BUILD_ROOT / 'frontend-runtime'
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


def build_safe_rollback(hr, args):
    """为原1.0.8业务基线构建只修复标签混写的兼容回退镜像，不接正式数据库。"""
    hr.verify_baseline()
    proof = hr.load(ROOT, 'build.private.json')
    if args.commit != proof['commit'] or (ROOT / 'safe-rollback-build.private.json').exists():
        raise RuntimeError('安全回退发布提交或阶段不符')
    archive, binary = pathlib.Path(args.archive), pathlib.Path(args.binary)
    if hr.digest(archive) != args.archive_sha256 or hr.digest(binary) != args.binary_sha256 or binary.read_bytes()[:4] != b'\x7fELF':
        raise RuntimeError('安全回退源码或Linux二进制摘要不符')
    source = BUILD_ROOT / 'safe-rollback-source'
    source.mkdir(mode=0o700)
    with tarfile.open(archive) as stream:
        for member in stream.getmembers():
            path = pathlib.PurePosixPath(member.name)
            if path.is_absolute() or '..' in path.parts or not (member.isfile() or member.isdir()):
                raise RuntimeError('安全回退归档路径不符')
        stream.extractall(source)
    if hr.migrations_from_tar(archive.read_bytes(), 'db/migrations/') != hr.image_migrations(BASELINE['goodhr-production-backend-1'][1]):
        raise RuntimeError('安全回退必须保留102项旧迁移原文')
    shutil.copyfile(archive, ROOT / 'safe-rollback-source.tar')
    shutil.copyfile(binary, ROOT / 'safe-rollback-server')
    shutil.copyfile(binary, source / 'goodhr-server')
    (source / 'goodhr-server').chmod(0o700)
    (source / 'Dockerfile').write_text('\n'.join(['FROM ' + BASELINE['goodhr-production-backend-1'][1], 'LABEL org.opencontainers.image.revision=' + args.commit, 'LABEL goodhr.fallback_base=01afd88ddabd36c28e73117e50e4b026aa355d4d', 'COPY goodhr-server /usr/local/bin/goodhr-server']) + '\n')
    tag = 'goodhr-production-backend:safe-rollback-' + args.commit[:8]
    with (ROOT / 'safe-rollback-build.private.log').open('xb') as log:
        hr.run(['docker', 'build', '--network=none', '-t', tag, str(source)], output=log, timeout=900)
    image = {'id': hr.inspect(tag, image=True)['Id'], 'tag': tag}
    if hr.image_migrations(image['id']) != hr.image_migrations(BASELINE['goodhr-production-backend-1'][1]):
        raise RuntimeError('安全回退镜像历史迁移发生变化')
    result = {'commit': args.commit, 'base_commit': '01afd88ddabd36c28e73117e50e4b026aa355d4d', 'image': image, 'binary_sha256': args.binary_sha256, 'source_sha256': args.archive_sha256}
    hr.save(ROOT, 'safe-rollback-build.private.json', result)
    print(json.dumps({'safe_rollback_build': 'complete', **result}))


def prepare(hr):
    """保存精确生产基线、校验0096迁移正文，并完成可恢复的完整PostgreSQL备份。"""
    current = hr.verify_baseline()
    proof = hr.load(ROOT, 'build.private.json')
    if hr.load(ROOT, 'safe-rollback-build.private.json')['commit'] != proof['commit']:
        raise RuntimeError('缺少当前发布的安全回退镜像')
    if (ROOT / 'manifest.private.json').exists() or (ROOT / 'database.dump.partial').exists():
        raise RuntimeError('已有准备材料，禁止覆盖')
    source = hr.migrations_from_tar((ROOT / 'source.tar').read_bytes(), 'goodhr5/cloud/backend/db/migrations/')
    previous = hr.image_migrations(BASELINE['goodhr-production-backend-1'][1])
    candidate = hr.image_migrations(proof['images']['backend']['id'])
    applied = hr.ledger('postgres', 'goodhr5')
    if len(applied) != 102 or set(applied) != set(previous) or candidate != source or set(candidate) - set(previous) != MIGRATIONS or any(candidate.get(name) != text for name, text in previous.items()):
        raise RuntimeError('102项旧迁移、归档和候选镜像正文不一致')
    texts = {name: candidate[name] for name in sorted(MIGRATIONS)}
    hr.validate_new_migrations(texts)
    old_tables = hr.tables('postgres', 'goodhr5')
    if not FOLDER_TABLES.issubset(old_tables):
        raise RuntimeError('正式库缺少历史收藏夹表，与发布基线不符')
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
    package = BUILD_ROOT / 'source/goodhr5/cloud/frontend-next/public/downloads/goodhr-extension-chrome-1.0.9.zip'
    sha = hr.digest(package)
    desired = {'version': '1.0.9', 'version_chrome': '1.0.9', 'version_firefox': old['config_value'].get('version_firefox') or old['config_value']['version'], 'note': 'Chrome v1.0.9：原始标签与收藏夹独立，收藏夹支持搜索选择。Firefox 当前签名版保留，新版待上传。', 'url_chrome': 'https://goodhr.58it.cn/downloads/goodhr-extension-chrome-1.0.9.zip', 'sha256_chrome': sha}
    if not isinstance(old['config_value'].get('agent_compatibility'), dict):
        raise RuntimeError('缺少既有Agent兼容快照，拒绝发布混合版本配置')
    desired['agent_compatibility'] = old['config_value']['agent_compatibility']
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
    parser.add_argument('operation', choices=['build', 'build-safe-rollback', 'prepare', 'rehearse', 'cutover', 'publish-config', 'rollback-config', 'rollback', 'cleanup'])
    for name in ('commit', 'archive', 'archive-sha256', 'binary', 'binary-sha256'):
        parser.add_argument('--' + name)
    args = parser.parse_args()
    if socket.gethostname() != 'ldclouda31539':
        raise RuntimeError('发布主机不匹配')
    os.umask(0o077)
    ROOT.mkdir(mode=0o700, exist_ok=True)
    if ROOT.is_symlink() or ROOT.resolve() != ROOT or ROOT.stat().st_dev != pathlib.Path('/').stat().st_dev:
        raise RuntimeError('发布目录必须为固定根盘目录')
    BUILD_ROOT.mkdir(mode=0o700, parents=True, exist_ok=True)
    if BUILD_ROOT.is_symlink() or BUILD_ROOT.resolve() != BUILD_ROOT or BUILD_ROOT.stat().st_dev != pathlib.Path('/www').stat().st_dev:
        raise RuntimeError('构建目录必须为固定数据盘私有目录')
    with pathlib.Path('/www/wwwroot/goodhr-production/backups/.goodhr-container-deploy.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        hr = load_launcher()
        signal.signal(signal.SIGTERM, hr.interrupt_release)
        if args.operation == 'build':
            build(hr, args)
        elif args.operation == 'build-safe-rollback':
            build_safe_rollback(hr, args)
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
