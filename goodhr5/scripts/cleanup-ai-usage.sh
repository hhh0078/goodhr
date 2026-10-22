#!/usr/bin/env bash
# 文件作用说明：供宝塔或系统每日定时调用受保护的AI消费清理接口；密钥只经标准输入传给curl，不出现在命令参数、URL或日志。
set -euo pipefail

: "${GOODHR_PUBLIC_BASE_URL:?请设置GoodHR公开HTTPS地址}"
: "${GOODHR_AI_USAGE_CLEANUP_TOKEN:?请设置与后端相同的消费清理密钥}"

if [[ ! "$GOODHR_PUBLIC_BASE_URL" =~ ^https://[A-Za-z0-9.-]+(:[0-9]+)?/?$ ]]; then
  printf '%s\n' '公开地址必须是HTTPS站点根地址，不要包含路径、用户身份或查询参数。' >&2
  exit 1
fi
if [[ ! "$GOODHR_AI_USAGE_CLEANUP_TOKEN" =~ ^[A-Za-z0-9._~-]{32,256}$ ]]; then
  printf '%s\n' '清理密钥需要32到256位字母、数字或 . _ ~ - 。' >&2
  exit 1
fi

for ((batch=1; batch<=1000; batch++)); do
  result="$(
    printf 'url = "%s/api/internal/ai-usage/cleanup"\nheader = "Authorization: Bearer %s"\n' \
      "${GOODHR_PUBLIC_BASE_URL%/}" "$GOODHR_AI_USAGE_CLEANUP_TOKEN" |
      curl --config - --request POST --silent --show-error --fail --max-time 45
  )"
  more="$(printf '%s' "$result" | python3 -c '
import json, sys
value = json.load(sys.stdin)
if value.get("ok") is not True or not isinstance(value.get("has_more"), bool):
    raise SystemExit("清理回执不完整，请检查服务状态。")
blocked = value.get("blocked_count", 0)
if not isinstance(blocked, int) or blocked < 0:
    raise SystemExit("清理阻塞数量不正确，请检查服务状态。")
if blocked:
    raise SystemExit("仍有%s条旧账本消费未清理，请先核对0092账本迁移；本轮不能算全部完成。" % blocked)
print("1" if value["has_more"] else "0")
')"
  printf '消费清理第%s批完成。\n' "$batch"
  if [[ "$more" == '0' ]]; then
    printf '%s\n' '七天前消费明细已清理，金额汇总和资金账本已保留。'
    exit 0
  fi
done
printf '%s\n' '已达到本轮批次数上限，请再次运行完成剩余清理。' >&2
exit 1
