# 本文件验证一次性会员赠送的期限、备份、重复执行和邮件不确定结果保护，不连接正式环境。
import datetime as dt
import importlib.util
import json
from pathlib import Path
import smtplib
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch


spec = importlib.util.spec_from_file_location("gift_membership_month", Path(__file__).with_name("gift-membership-month.py"))
gift = importlib.util.module_from_spec(spec)
spec.loader.exec_module(gift)


class GiftSafetyTests(unittest.TestCase):
    """验证批量权益和邮件操作中的关键安全边界。"""

    def setUp(self):
        """创建固定时间和账号样本，避免测试依赖真实用户或时钟。"""
        self.now = dt.datetime(2026, 9, 28, 14, 0, tzinfo=dt.timezone.utc)
        self.user = {"id": "12345678-1234-1234-1234-123456789012", "email": "user@example.com",
                     "status": "active", "subscription": {"member_type": "max", "expires_at": "2026-10-10T12:00:00Z"}}

    def test_unexpired_membership_keeps_original_time(self):
        """尚未到期的会员保留原剩余时间，再完整增加30天。"""
        plan = gift.create_plan([self.user], self.now)
        next_expiry = gift.parse_expiry(plan["users"][0]["new_subscription"]["expires_at"])
        self.assertEqual(next_expiry, gift.parse_expiry("2026-11-09T12:00:00Z"))
        self.assertEqual(plan["users"][0]["new_subscription"]["member_type"], "max")

    def test_expired_and_exactly_expiring_membership_get_full_duration(self):
        """已过期及恰好到期的账号都从执行时间获得完整30天。"""
        for expiry in ("2020-01-01T00:00:00Z", self.now.isoformat()):
            user = dict(self.user, subscription={"member_type": "plus", "expires_at": expiry})
            next_subscription = gift.create_plan([user], self.now)["users"][0]["new_subscription"]
            self.assertEqual(gift.parse_expiry(next_subscription["expires_at"]), self.now + dt.timedelta(days=30))
            self.assertEqual(next_subscription["member_type"], "plus")

    def test_far_future_membership_and_timezone_are_preserved(self):
        """长期会员不会被重置，时区转换不会损失已有有效时间。"""
        user = dict(self.user, subscription={"member_type": "max", "expires_at": "2218-03-23T14:53:26.958052+08:00"})
        next_expiry = gift.create_plan([user], self.now)["users"][0]["new_subscription"]["expires_at"]
        self.assertEqual(gift.parse_expiry(next_expiry), gift.parse_expiry(user["subscription"]["expires_at"]) + dt.timedelta(days=30))

    def test_go_nanosecond_timestamp_keeps_full_precision(self):
        """Go写入的纳秒时间兼容服务器Python3.8，延长后保留原时间精度。"""
        user = dict(self.user, subscription={"member_type": "max", "expires_at": "2026-10-21T03:57:02.368275746Z"})
        next_expiry = gift.create_plan([user], self.now)["users"][0]["new_subscription"]["expires_at"]
        self.assertEqual(next_expiry, "2026-11-20T03:57:02.368275746Z")
        self.assertEqual(gift.parse_expiry(next_expiry).microsecond, 368275)

    def test_blank_email_is_skipped_and_invalid_data_stops_whole_plan(self):
        """空邮箱记录跳过，异常账号不会被悄悄遗漏或获得错误会员。"""
        plan = gift.create_plan([dict(self.user, email=""), self.user], self.now)
        self.assertEqual(len(plan["users"]), 1)
        self.assertEqual(plan["skipped_blank_email"], 1)
        for user in (dict(self.user, email="wrong-address"), dict(self.user, subscription={"member_type": "free", "expires_at": "2026-10-01T00:00:00Z"})):
            with self.assertRaises(ValueError):
                gift.create_plan([user], self.now)
        with self.assertRaises(ValueError):
            gift.parse_expiry("2026-10-01T00:00:00")

    def test_backup_corruption_is_rejected(self):
        """备份改动后必须停止发放，避免用错误旧值更新正式权益。"""
        operation = gift.GiftOperation.__new__(gift.GiftOperation)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            with patch.object(gift, "BACKUP_ROOT", root), patch.object(gift, "BACKUP_DIR", root / "campaign"):
                plan = gift.create_plan([self.user], self.now)
                self.assertEqual(operation.save_plan(plan), plan)
                path = gift.BACKUP_DIR / "users-before-and-plan.json"
                path.write_bytes(path.read_bytes() + b" ")
                with self.assertRaises(ValueError):
                    operation.load_plan()

    def test_transaction_escapes_payload_and_has_explicit_rollback(self):
        """SQL保留含单引号及占位词的邮箱，演练使用回滚，正式操作使用提交。"""
        operation = gift.GiftOperation.__new__(gift.GiftOperation)
        user = dict(self.user, email="o'hara-$gift$-CAMPAIGN-SUBJECT-FINISH@example.com")
        plan = gift.create_plan([user], self.now)
        rehearsed = operation.grant_sql(plan, False)
        self.assertIn("o''hara-$gift$-CAMPAIGN-SUBJECT-FINISH@example.com", rehearsed)
        self.assertTrue(rehearsed.strip().endswith("ROLLBACK;"))
        self.assertTrue(operation.grant_sql(plan, True).strip().endswith("COMMIT;"))

    def test_existing_campaign_does_not_grant_again(self):
        """重复执行已存在活动时只查询结果，不运行更新会员的SQL。"""
        operation = gift.GiftOperation.__new__(gift.GiftOperation)
        operation.batch = Mock(return_value={"id": "existing"})
        operation.report = Mock(return_value={"total": 1})
        operation.query = Mock()
        self.assertTrue(operation.grant()["already_granted"])
        operation.query.assert_not_called()

    def test_email_has_exact_subject_and_personal_expiry(self):
        """邮件标题严格匹配已确认文案，到期时间使用北京时间且不抄送别人。"""
        user = gift.create_plan([self.user], self.now)["users"][0]
        message = gift.build_message({"GOODHR_SMTP_USERNAME": "sender@example.com"}, user)
        self.assertEqual(str(message["Subject"]), "goodhr 赠送一个月会员给您")
        self.assertIn("2026-11-09 20:00:00", message.get_body(preferencelist=("plain",)).get_content())
        self.assertIsNone(message["Cc"])
        self.assertIsNone(message["Bcc"])

    def test_uncertain_send_is_marked_and_never_adjusts_membership(self):
        """网络中断时保留不确定标记，发信过程不会调用会员发放。"""
        operation = gift.GiftOperation.__new__(gift.GiftOperation)
        operation.config = {"GOODHR_SMTP_USERNAME": "sender@example.com"}
        operation.load_plan = Mock(return_value=gift.create_plan([self.user], self.now))
        operation.batch = Mock(return_value={"id": "batch"})
        operation.query = Mock(return_value=[{"id": "recipient", "email": self.user["email"]}])
        operation.mark_recipient = Mock()
        operation.connect_smtp = Mock(return_value=Mock(send_message=Mock(side_effect=smtplib.SMTPServerDisconnected("connection lost"))))
        operation.grant = Mock()
        fake_fcntl = Mock(LOCK_EX=1, LOCK_NB=2)
        with tempfile.TemporaryDirectory() as directory:
            with patch.object(gift, "BACKUP_DIR", Path(directory)), patch.dict(sys.modules, {"fcntl": fake_fcntl}):
                with self.assertRaises(RuntimeError):
                    operation.send(1, 0.5)
        self.assertIn("[gift-uncertain]", operation.mark_recipient.call_args[0][2])
        operation.grant.assert_not_called()

    def test_smtp_connections_only_send_five_messages(self):
        """每个连接最多发送五封，兼顾单连接收件人数和连接频率限制。"""
        operation = gift.GiftOperation.__new__(gift.GiftOperation)
        operation.config = {"GOODHR_SMTP_USERNAME": "sender@example.com"}
        users = [dict(self.user, id=str(index), email="user" + str(index) + "@example.com") for index in range(6)]
        operation.load_plan = Mock(return_value=gift.create_plan(users, self.now))
        operation.batch = Mock(return_value={"id": "batch"})
        operation.query = Mock(return_value=[{"id": str(index), "email": user["email"]} for index, user in enumerate(users)])
        operation.mark_recipient = Mock()
        clients = [Mock(send_message=Mock(return_value={})) for _ in range(2)]
        operation.connect_smtp = Mock(side_effect=clients)
        operation.report = Mock(return_value={"sent": 6})
        with tempfile.TemporaryDirectory() as directory:
            with patch.object(gift, "BACKUP_DIR", Path(directory)), patch.dict(sys.modules, {"fcntl": Mock(LOCK_EX=1, LOCK_NB=2)}), patch.object(gift.time, "sleep"):
                operation.send(6, 0.5)
        self.assertEqual(operation.connect_smtp.call_count, 2)
        self.assertEqual(clients[0].send_message.call_count, 5)
        self.assertEqual(clients[1].send_message.call_count, 1)

    def test_global_provider_limit_stops_after_first_rejection(self):
        """账号级发送限制出现时立即停下，保留其他待发邮件，不持续碰撞限制。"""
        operation = gift.GiftOperation.__new__(gift.GiftOperation)
        operation.config = {"GOODHR_SMTP_USERNAME": "sender@example.com"}
        users = [dict(self.user, id=str(index), email="user" + str(index) + "@example.com") for index in range(2)]
        operation.load_plan = Mock(return_value=gift.create_plan(users, self.now))
        operation.batch = Mock(return_value={"id": "batch"})
        operation.query = Mock(return_value=[{"id": str(index), "email": user["email"]} for index, user in enumerate(users)])
        operation.mark_recipient = Mock()
        error = smtplib.SMTPRecipientsRefused({users[0]["email"]: (554, b"MI:STC provider limit")})
        client = Mock(send_message=Mock(side_effect=error))
        operation.connect_smtp = Mock(return_value=client)
        with tempfile.TemporaryDirectory() as directory:
            with patch.object(gift, "BACKUP_DIR", Path(directory)), patch.dict(sys.modules, {"fcntl": Mock(LOCK_EX=1, LOCK_NB=2)}):
                with self.assertRaises(RuntimeError):
                    operation.send(2, 0.5)
        self.assertEqual(client.send_message.call_count, 1)
        self.assertEqual(operation.mark_recipient.call_args[0][1], "failed")


if __name__ == "__main__":
    unittest.main()
