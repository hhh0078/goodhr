# 本文件用于在正式服务器一次性赠送30天会员，备份原权益并记录邮件进度，不修改业务服务。
import argparse
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import re
import smtplib
import ssl
import subprocess
import sys
import time
from email.message import EmailMessage
from email.utils import format_datetime
from html import escape
from urllib.parse import urlsplit


CAMPAIGN = "gift:membership-month:2026-09-28"
SUBJECT = "goodhr 赠送一个月会员给您"
BACKUP_ROOT = Path("/www/wwwroot/goodhr-production/backups")
BACKUP_DIR = BACKUP_ROOT / "membership-gift-20260928"
PRODUCTION_DIR = "/www/wwwroot/goodhr-production/good-hr-ai-js/goodhr5"
# 少量复用连接，避免单连接收件人数和短时间连接频率两类限制。
MAX_MESSAGES_PER_CONNECTION = 5


def sql_literal(value):
    """将字符串安全转换为SQL字面量，保留邮箱及JSON中的单引号。"""
    return "'" + str(value).replace("'", "''") + "'"


def parse_expiry(value):
    """解析带时区的会员到期时间，并拒绝缺少时区或无效的数据。"""
    normalized = value.replace("Z", "+00:00")
    fraction = re.search(r"\.(\d+)(?=[+-]\d{2}:\d{2}$)", normalized)
    if fraction:
        normalized = normalized[:fraction.start()] + "." + (fraction.group(1) + "000000")[:6] + normalized[fraction.end():]
    expiry = dt.datetime.fromisoformat(normalized)
    if expiry.tzinfo is None:
        raise ValueError("会员到期时间缺少时区")
    return expiry.astimezone(dt.timezone.utc)


def create_plan(users, now):
    """固定有效账号名单，按原到期时间或当前时间增加30天，保留会员类型。"""
    rows, skipped = [], 0
    for user in users:
        email = user["email"]
        if not email.strip():
            skipped += 1
            continue
        if not re.fullmatch(r"[^\s@]+@[^\s@]+\.[^\s@]+", email):
            raise ValueError("发现邮箱格式异常，发放前需要核查")
        old = user["subscription"]
        if old.get("member_type") not in ("plus", "max"):
            raise ValueError("发现未确认的会员类型，停止发放")
        expiry = parse_expiry(old["expires_at"])
        new = dict(old)
        if expiry >= now:
            fraction = re.search(r"\.\d+(?=Z$|[+-]\d{2}:\d{2}$)", old["expires_at"])
            new["expires_at"] = (expiry + dt.timedelta(days=30)).strftime("%Y-%m-%dT%H:%M:%S") + (fraction.group(0) if fraction else "") + "Z"
        else:
            new["expires_at"] = (now + dt.timedelta(days=30)).isoformat().replace("+00:00", "Z")
        rows.append({"id": user["id"], "email": email, "status": user["status"],
                     "old_subscription": old, "new_subscription": new})
    if not rows:
        raise ValueError("没有可赠送的账号")
    return {"campaign": CAMPAIGN, "subject": SUBJECT, "days": 30,
            "planned_at": now.isoformat(), "skipped_blank_email": skipped, "users": rows}


def build_message(config, user):
    """生成指定标题的会员赠送邮件，包含该用户的新到期时间和查看入口。"""
    expiry = parse_expiry(user["new_subscription"]["expires_at"])
    expiry_text = expiry.astimezone(dt.timezone(dt.timedelta(hours=8))).strftime("%Y-%m-%d %H:%M:%S")
    plain = ("你好，感谢你对 GoodHR 的支持。\n\n"
             "我们已为你的账号免费赠送 30 天会员，无需领取。\n"
             "原会员尚未到期的，赠送时间已接在原到期时间后；已过期的，从本次赠送时间开始计算。\n\n"
             "新的会员到期时间：" + expiry_text + "（北京时间）\n"
             "登录 https://goodhr.58it.cn 即可查看。\n\n祝你招聘顺利！\nGoodHR")
    html = ("<!doctype html><html lang='zh-CN'><meta charset='UTF-8'>"
            "<body style='margin:0;background:#f4f7f5;font-family:Arial,sans-serif;color:#1f2a23'>"
            "<div style='max-width:580px;margin:24px auto;padding:24px;background:white;border:1px solid #dce7df;border-radius:12px'>"
            "<h1 style='font-size:22px;color:#16724c'>" + escape(SUBJECT) + "</h1>"
            "<p>你好，感谢你对 GoodHR 的支持。</p><p>我们已为你的账号免费赠送 <strong>30 天会员</strong>，无需领取。</p>"
            "<p>原会员尚未到期的，赠送时间已接在原到期时间后；已过期的，从本次赠送时间开始计算。</p>"
            "<p>新的会员到期时间：<strong>" + escape(expiry_text) + "</strong>（北京时间）</p>"
            "<p><a href='https://goodhr.58it.cn' style='color:#16724c'>登录 GoodHR 查看</a></p>"
            "<p>祝你招聘顺利！<br>GoodHR</p></div></body></html>")
    message = EmailMessage()
    message["From"] = "GoodHR <" + (config.get("GOODHR_SMTP_FROM") or config["GOODHR_SMTP_USERNAME"]) + ">"
    message["To"] = user["email"]
    message["Subject"] = SUBJECT
    message["Date"] = format_datetime(dt.datetime.now(dt.timezone.utc))
    message["Message-ID"] = "<" + hashlib.sha256((CAMPAIGN + user["email"]).encode()).hexdigest() + "@goodhr.58it.cn>"
    message.set_content(plain)
    message.add_alternative(html, subtype="html")
    return message


class GiftOperation:
    """执行固定活动的预览、备份、会员发放及邮件补发。"""

    def __init__(self):
        """读取指定正式容器的配置，并核对项目和数据库以防操作测试环境。"""
        result = subprocess.run(["docker", "inspect", "goodhr-production-backend-1"],
                                capture_output=True, text=True, check=True, timeout=15)
        container = json.loads(result.stdout)[0]
        if container["Config"].get("Labels", {}).get("com.docker.compose.project.working_dir") != PRODUCTION_DIR:
            raise ValueError("正式容器目录与预期不一致")
        self.config = dict(item.split("=", 1) for item in container["Config"].get("Env", []) if "=" in item)
        db = urlsplit(self.config.get("GOODHR_PG_DSN", ""))
        if db.path != "/goodhr5" or db.hostname not in ("127.0.0.1", "localhost") or db.username != "postgres":
            raise ValueError("正式数据库配置与预期不一致")
        if not all(self.config.get(key) for key in ("GOODHR_SMTP_HOST", "GOODHR_SMTP_USERNAME", "GOODHR_SMTP_PASSWORD")):
            raise ValueError("正式邮件服务配置不完整")

    def query(self, sql):
        """通过数据库容器执行有超时限制的SQL，只返回结果，不输出连接凭据。"""
        result = subprocess.run(["docker", "exec", "-i", "-e", "PGOPTIONS=-c statement_timeout=30000 -c lock_timeout=5000",
                                 "postgres", "psql", "-X", "-q", "-A", "-t", "-U", "postgres", "-d", "goodhr5",
                                 "-v", "ON_ERROR_STOP=1"], input=sql, capture_output=True, text=True, timeout=45)
        if result.returncode:
            raise RuntimeError("数据库操作失败：" + result.stderr.strip()[-700:])
        lines = [line for line in result.stdout.splitlines() if line.strip()]
        return json.loads(lines[-1]) if lines else None

    def batch(self):
        """读取活动唯一批次，用于阻止已发放活动再次增加会员。"""
        return self.query("SELECT row_to_json(b) FROM email_batches b WHERE source_key=" + sql_literal(CAMPAIGN) + ";")

    def preview(self):
        """只读取得正式账号名单并预览延长结果，不增加会员也不发邮件。"""
        users = self.query("BEGIN READ ONLY; SELECT COALESCE(json_agg(json_build_object('id',id::text,'email',email,'status',status,'subscription',subscription) ORDER BY id),'[]'::json) FROM users; COMMIT;")
        return create_plan(users, dt.datetime.now(dt.timezone.utc))

    def save_plan(self, plan):
        """排他保存原会员数据和目标权益，写完校验摘要并限制备份文件权限。"""
        if BACKUP_DIR.resolve().parent != BACKUP_ROOT.resolve():
            raise ValueError("备份目录超出允许范围")
        BACKUP_DIR.mkdir(mode=0o700, exist_ok=True)
        path = BACKUP_DIR / "users-before-and-plan.json"
        data = (json.dumps(plan, ensure_ascii=False, indent=2) + "\n").encode("utf-8")
        with open(path, "xb") as file:
            os.chmod(str(path), 0o600)
            file.write(data)
            file.flush()
            os.fsync(file.fileno())
        with open(BACKUP_DIR / "users-before-and-plan.sha256", "x") as file:
            os.chmod(str(BACKUP_DIR / "users-before-and-plan.sha256"), 0o600)
            file.write(hashlib.sha256(data).hexdigest() + "\n")
            file.flush()
            os.fsync(file.fileno())
        return self.load_plan()

    def load_plan(self):
        """校验备份摘要及活动标识，拒绝损坏、错活动或重复账号的备份。"""
        data = (BACKUP_DIR / "users-before-and-plan.json").read_bytes()
        digest = (BACKUP_DIR / "users-before-and-plan.sha256").read_text().strip()
        if hashlib.sha256(data).hexdigest() != digest:
            raise ValueError("会员备份摘要校验失败")
        plan = json.loads(data)
        if plan["campaign"] != CAMPAIGN or plan["days"] != 30 or plan["subject"] != SUBJECT:
            raise ValueError("备份活动内容不一致")
        if len({row["id"] for row in plan["users"]}) != len(plan["users"]):
            raise ValueError("备份中出现重复账号")
        return plan

    def grant_sql(self, plan, commit):
        """生成同一事务内校验旧值、延长权益及创建唯一邮件批次的SQL。"""
        payload = sql_literal(json.dumps(plan["users"], ensure_ascii=False))
        campaign = sql_literal(CAMPAIGN)
        delimiter = "$gift_" + hashlib.sha256(payload.encode("utf-8")).hexdigest() + "$"
        while delimiter in payload:
            delimiter = delimiter[:-1] + "x$"
        return """BEGIN;
SET LOCAL standard_conforming_strings=on;
DO $gift$
DECLARE prepared jsonb := PAYLOAD::jsonb; item jsonb; current_email text;
        current_status text; current_subscription jsonb; batch_id uuid; changed integer;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtext(CAMPAIGN)::bigint);
    IF EXISTS (SELECT 1 FROM email_batches WHERE source_key=CAMPAIGN) THEN
        RAISE EXCEPTION '本次赠送已执行，禁止再次增加会员';
    END IF;
    FOR item IN SELECT value FROM jsonb_array_elements(prepared) LOOP
        SELECT email,status,subscription INTO current_email,current_status,current_subscription
        FROM users WHERE id=(item->>'id')::uuid FOR UPDATE;
        IF NOT FOUND OR current_email IS DISTINCT FROM item->>'email'
           OR current_status IS DISTINCT FROM item->>'status'
           OR current_subscription IS DISTINCT FROM item->'old_subscription' THEN
            RAISE EXCEPTION '备份后账号权益发生变化，整批回滚';
        END IF;
    END LOOP;
    INSERT INTO email_batches(subject,target_summary,source_key,created_by_email,total_count)
    VALUES (SUBJECT,'赠送30天会员；保留原会员类型；跳过空邮箱',CAMPAIGN,'1224299352@qq.com',jsonb_array_length(prepared))
    RETURNING id INTO batch_id;
    FOR item IN SELECT value FROM jsonb_array_elements(prepared) LOOP
        UPDATE users SET subscription=item->'new_subscription' WHERE id=(item->>'id')::uuid;
        GET DIAGNOSTICS changed = ROW_COUNT;
        IF changed <> 1 THEN RAISE EXCEPTION '会员更新数量异常，整批回滚'; END IF;
        INSERT INTO email_recipients(batch_id,email) VALUES(batch_id,item->>'email');
    END LOOP;
END $gift$;
SELECT json_build_object('granted',b.total_count,'batch_id',b.id,'recipients',
 (SELECT count(*) FROM email_recipients r WHERE r.batch_id=b.id))
FROM email_batches b WHERE source_key=CAMPAIGN;
FINISH;
""".replace("$gift$", delimiter).replace("CAMPAIGN", campaign).replace("SUBJECT", sql_literal(SUBJECT)).replace("FINISH", "COMMIT" if commit else "ROLLBACK").replace("PAYLOAD", payload)

    def grant(self):
        """先备份并回滚演练，再一次性发放；已执行的活动直接返回而不重复赠送。"""
        if self.batch():
            return {"already_granted": True, "report": self.report()}
        backup = BACKUP_DIR / "users-before-and-plan.json"
        plan = self.load_plan() if backup.exists() else self.save_plan(self.preview())
        rehearsed = self.query(self.grant_sql(plan, False))
        if rehearsed["granted"] != len(plan["users"]) or rehearsed["recipients"] != len(plan["users"]):
            raise ValueError("发放演练结果不一致")
        print(json.dumps({"backup_verified": True, "rollback_rehearsal": rehearsed["granted"]}), flush=True)
        applied = self.query(self.grant_sql(plan, True))
        return {"applied": applied, "report": self.report()}

    def report(self):
        """统计邮件进度并核对备份名单的会员权益，输出不含用户邮箱的摘要。"""
        batch = self.batch()
        if not batch:
            return {"campaign_exists": False}
        plan = self.load_plan()
        ids = sql_literal(json.dumps(plan["users"]))
        result = self.query("""SELECT json_build_object(
          'total',b.total_count,'sent',count(*) FILTER(WHERE r.status='sent'),
          'failed',count(*) FILTER(WHERE r.status='failed'),
          'pending',count(*) FILTER(WHERE r.status='pending'),
          'uncertain',count(*) FILTER(WHERE r.error_message LIKE '[gift-sending]%' OR r.error_message LIKE '[gift-uncertain]%'),
          'batch_id',b.id,'finished_at',b.finished_at,
          'memberships_verified',(SELECT count(*) FROM jsonb_array_elements(PAYLOAD::jsonb) p
             JOIN users u ON u.id=(p->>'id')::uuid WHERE u.subscription->>'member_type'=p->'new_subscription'->>'member_type'
             AND (u.subscription->>'expires_at')::timestamptz >= (p->'new_subscription'->>'expires_at')::timestamptz))
          FROM email_batches b JOIN email_recipients r ON r.batch_id=b.id
          WHERE b.source_key=CAMPAIGN GROUP BY b.id;
        """.replace("CAMPAIGN", sql_literal(CAMPAIGN)).replace("PAYLOAD", ids))
        result["skipped_blank_email"] = plan["skipped_blank_email"]
        return result

    def connect_smtp(self):
        """使用现有邮件配置建立有超时的TLS连接并登录，不保存或输出密码。"""
        host = self.config["GOODHR_SMTP_HOST"]
        port = int(self.config.get("GOODHR_SMTP_PORT") or "465")
        context = ssl.create_default_context()
        context.minimum_version = ssl.TLSVersion.TLSv1_2
        if port == 465:
            client = smtplib.SMTP_SSL(host, port, timeout=30, context=context)
        else:
            client = smtplib.SMTP(host, port, timeout=30)
            client.ehlo()
            client.starttls(context=context)
            client.ehlo()
        try:
            client.login(self.config["GOODHR_SMTP_USERNAME"], self.config["GOODHR_SMTP_PASSWORD"])
        except Exception:
            client.close()
            raise
        return client

    def mark_recipient(self, recipient_id, status, error=""):
        """保存单封邮件状态并同步批次统计，邮件失败不再次调整会员权益。"""
        self.query("""BEGIN;
            UPDATE email_recipients SET status=STATUS,error_message=ERROR,
            sent_at=CASE WHEN STATUS='sent' THEN now() ELSE sent_at END WHERE id=RECIPIENT::uuid;
            UPDATE email_batches b SET sent_count=c.sent,failed_count=c.failed,
            finished_at=CASE WHEN c.done>=b.total_count THEN COALESCE(b.finished_at,now()) ELSE NULL END
            FROM (SELECT batch_id,count(*) FILTER(WHERE status='sent') AS sent,
              count(*) FILTER(WHERE status='failed') AS failed,count(*) FILTER(WHERE status IN ('sent','failed')) AS done
              FROM email_recipients WHERE batch_id=(SELECT batch_id FROM email_recipients WHERE id=RECIPIENT::uuid)
              GROUP BY batch_id) c WHERE b.id=c.batch_id;
            COMMIT;
        """.replace("STATUS", sql_literal(status)).replace("RECIPIENT", sql_literal(recipient_id)).replace("ERROR", sql_literal(error[:1200])))

    def send(self, limit, delay):
        """仅发送未成功的赠送邮件；已发送及结果不确定的记录不会自动重发。"""
        import fcntl
        plan = self.load_plan()
        if not self.batch():
            raise ValueError("会员尚未发放，不能发送赠送邮件")
        with open(BACKUP_DIR / "mail.lock", "a") as lock:
            os.chmod(str(BACKUP_DIR / "mail.lock"), 0o600)
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            recipients = self.query("""SELECT COALESCE(json_agg(row_to_json(r) ORDER BY (r.email='1224299352@qq.com') DESC,r.email),'[]'::json)
             FROM (SELECT r.id::text,r.email FROM email_recipients r JOIN email_batches b ON b.id=r.batch_id
             WHERE b.source_key=CAMPAIGN AND r.status IN ('pending','failed')
             AND r.error_message NOT LIKE '[gift-sending]%' AND r.error_message NOT LIKE '[gift-uncertain]%') r;
            """.replace("CAMPAIGN", sql_literal(CAMPAIGN)))[:limit]
            users = {row["email"]: row for row in plan["users"]}
            client = None
            try:
                for index, recipient in enumerate(recipients):
                    if client is None or index % MAX_MESSAGES_PER_CONNECTION == 0:
                        if client is not None:
                            client.close()
                        client = self.connect_smtp()
                    self.mark_recipient(recipient["id"], "pending", "[gift-sending] 已开始发送，异常中断后需核查")
                    try:
                        refused = client.send_message(build_message(self.config, users[recipient["email"]]))
                        if refused:
                            raise smtplib.SMTPRecipientsRefused(refused)
                    except smtplib.SMTPRecipientsRefused as error:
                        self.mark_recipient(recipient["id"], "failed", str(error))
                        if any(400 <= reply[0] < 500 or b"MI:STC" in reply[1].upper() for reply in error.recipients.values()):
                            raise RuntimeError("邮件服务限制发送，已停止，恢复后只补发邮件") from error
                    except smtplib.SMTPResponseException as error:
                        self.mark_recipient(recipient["id"], "failed", str(error))
                        raise RuntimeError("邮件服务拒绝发送，已停止，剩余邮件仍待发送") from error
                    except Exception as error:
                        self.mark_recipient(recipient["id"], "pending", "[gift-uncertain] " + type(error).__name__)
                        raise RuntimeError("邮件发送结果不确定，已停止且不会自动重复发送") from error
                    else:
                        self.mark_recipient(recipient["id"], "sent")
                    if (index + 1) % 25 == 0 or index + 1 == len(recipients):
                        print(json.dumps(self.report(), ensure_ascii=False), flush=True)
                    time.sleep(delay)
            finally:
                if client is not None:
                    client.close()
        return self.report()


def main():
    """解析明确的运维动作，默认只预览；发放、发信和查询分别执行。"""
    parser = argparse.ArgumentParser(description="GoodHR一次性赠送30天会员，默认只读预览")
    parser.add_argument("action", choices=("preview", "grant", "send", "report"), nargs="?", default="preview")
    parser.add_argument("--check-smtp", action="store_true", help="只验证邮件登录，不发邮件")
    parser.add_argument("--limit", type=int, default=1000, help="本次最多发送的邮件数")
    parser.add_argument("--delay", type=float, default=2.0, help="每封邮件发送后的等待秒数")
    args = parser.parse_args()
    if args.limit < 1 or args.delay < 0.5:
        parser.error("发送数量必须大于0，每封至少间隔0.5秒")
    operation = GiftOperation()
    if args.check_smtp:
        operation.connect_smtp().close()
        print(json.dumps({"smtp_authenticated": True}), flush=True)
    if args.action == "preview":
        plan = operation.preview()
        print(json.dumps({"users": len(plan["users"]), "skipped_blank_email": plan["skipped_blank_email"],
                          "campaign": CAMPAIGN, "subject": SUBJECT, "days": 30}, ensure_ascii=False), flush=True)
    elif args.action == "grant":
        print(json.dumps(operation.grant(), ensure_ascii=False), flush=True)
    elif args.action == "send":
        print(json.dumps(operation.send(args.limit, args.delay), ensure_ascii=False), flush=True)
    else:
        print(json.dumps(operation.report(), ensure_ascii=False), flush=True)


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(json.dumps({"error": str(error)}, ensure_ascii=False), file=sys.stderr, flush=True)
        sys.exit(1)
