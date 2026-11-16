#!/bin/sh
# 文件作用说明：从统一服务地址提取证书名称，兼容不发送 SNI 的 IP 客户端，并核对 HTTPS 映射端口。
set -eu
case "${GOODHR_PUBLIC_BASE_URL:-}" in
  https://*) authority=${GOODHR_PUBLIC_BASE_URL#https://} ;;
  *) echo "私有系统需要 https:// 开头的内网地址" >&2; exit 1 ;;
esac
authority=${authority%/}
case "$authority" in *[!a-zA-Z0-9.:-]*|'') echo "内网地址只填写 IP/域名及端口" >&2; exit 1 ;; esac
GOODHR_TLS_SERVER_NAME=${authority%%:*}
if [ "$authority" = "$GOODHR_TLS_SERVER_NAME" ]; then service_port=443; else service_port=${authority##*:}; fi
if [ "$service_port" != "${GOODHR_HTTPS_PORT:-8443}" ]; then echo "GOODHR_HTTPS_PORT 必须与服务地址端口一致" >&2; exit 1; fi
export GOODHR_TLS_SERVER_NAME
exec caddy run --config /etc/caddy/Caddyfile --adapter caddyfile
