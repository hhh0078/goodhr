"""文件作用：验证套餐升级只允许固定套餐种子及注册赠送归零，其他配置和历史数据必须保留。"""
import copy
import hashlib
from pathlib import Path
import unittest
from release_cutover import CutoverError
from release_rehearse_auto import TIME_CONFIG_MIGRATIONS, verify_time_config_upgrade


class TimePackageReleaseTests(unittest.TestCase):
    """使用纯合成配置验证发布允许范围，不连接真实业务库。"""

    def test_exact_changes_and_rejected_drift(self):
        """只允许新增固定套餐和注册归零，其他字段、价格与更新时间的变化均拒绝。"""
        before = [{"id": "old", "config_key": "system.onboarding_config", "config_value": {
            "trial_days": 7, "wechat_follow_reward_days": 2, "local_agent": [{"version": "old"}]},
            "updated_at": "old"}]
        after = copy.deepcopy(before)
        after[0]["config_value"]["trial_days"] = 0
        plans = [{"id": key, "name": name, "duration_days": days, "price_cents": price,
                  "balance_cents": 99900, "enabled": True}
                 for key, name, days, price in (("monthly", "包月套餐", 30, 4000),
                                                ("quarterly", "季度套餐", 90, 12000),
                                                ("yearly", "包年套餐", 365, 34000))]
        after.append({"id": "11111111-1111-4111-8111-111111111111", "config_key": "system.ai_time_plans",
                      "config_value": plans, "description": "AI时间套餐：价格、天数和独立可用额度（金额单位分）",
                      "enabled": True, "created_at": "2026-11-27T00:30:00Z", "updated_at": "2026-11-27T00:30:00Z"})
        migrations = list(TIME_CONFIG_MIGRATIONS)
        result = verify_time_config_upgrade(before, after, migrations, "2026-11-27T00:00:00Z", "2026-11-27T01:00:00Z")
        self.assertTrue(result["exact_config_changes_verified"])
        bad = copy.deepcopy(after)
        bad[-1]["config_value"][0]["price_cents"] = 1
        with self.assertRaises(CutoverError):
            verify_time_config_upgrade(before, bad, migrations, "2026-11-27T00:00:00Z", "2026-11-27T01:00:00Z")
        for key in ("updated_at", "wechat_follow_reward_days", "local_agent"):
            damaged = copy.deepcopy(after)
            target = damaged[0] if key == "updated_at" else damaged[0]["config_value"]
            target[key] = "bad"
            with self.assertRaises(CutoverError):
                verify_time_config_upgrade(before, damaged, migrations, "2026-11-27T00:00:00Z", "2026-11-27T01:00:00Z")

    def test_frozen_sql(self):
        """数据变化例外只适用于本次实际审查的精确SQL。"""
        root = Path(__file__).parent.parent / "cloud/backend/db/migrations"
        for name, digest in TIME_CONFIG_MIGRATIONS.items():
            self.assertEqual(hashlib.sha256((root / name).read_bytes()).hexdigest(), digest)


if __name__ == "__main__":
    unittest.main()
