#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$ROOT_DIR"
fail() { printf '驗收失敗：%s\n' "$1" >&2; exit 1; }
pass() { printf '通過：%s\n' "$1"; }
require_cmd() { command -v "$1" >/dev/null 2>&1 || fail "缺少必要指令：$1"; }

for command_name in docker curl openssl grep base64 jq; do require_cmd "$command_name"; done
[[ -f .env ]] || fail "找不到 .env"
set -a
# shellcheck disable=SC1091
source .env
set +a
: "${BOT_TOKEN:?BOT_TOKEN 未設定}"
: "${WEB_PUBLIC_URL:?WEB_PUBLIC_URL 未設定}"
: "${VERIFY_QUALIFICATION_CHAT_ID:?請設定 VERIFY_QUALIFICATION_CHAT_ID}"
: "${VERIFY_SUBSCRIPTION_URL:?請設定 VERIFY_SUBSCRIPTION_URL}"
: "${VERIFY_TLS_SERVER_NAME:?請設定 VERIFY_TLS_SERVER_NAME}"
: "${VERIFY_EXTERNAL_EVIDENCE_FILE:?請設定 VERIFY_EXTERNAL_EVIDENCE_FILE 指向外部驗收證據 JSON}"
VERIFY_TLS_PORT="${VERIFY_TLS_PORT:-8443}"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
chmod 0700 "$tmp_dir"

telegram_call() {
  local method="$1" data="${2:-}" response="$tmp_dir/telegram-response.json" config="$tmp_dir/curl.conf"
  umask 077
  printf 'url = "https://api.telegram.org/bot%s/%s"\nsilent\nshow-error\nfail\n' "$BOT_TOKEN" "$method" > "$config"
  if [[ -n "$data" ]]; then
    curl --config "$config" -H 'Content-Type: application/json' --data "$data" -o "$response"
  else
    curl --config "$config" -o "$response"
  fi
  grep -q '"ok"[[:space:]]*:[[:space:]]*true' "$response" || fail "Telegram ${method} 未成功"
}

docker compose ps --status running | grep -q 'app' || fail "app 容器未運行"
docker compose ps --status running | grep -q 'core-controller' || fail "core-controller 容器未運行"
docker inspect -f '{{.State.Running}}' s12ryt-sing-box | grep -q true || fail "s12ryt-sing-box 未運行"
docker inspect s12ryt-sing-box | grep -q '/var/lib/s12ryt/sing-box/config.json' || fail "sing-box 設定未掛載"
docker compose config | grep -q 'core-control.sock' || fail "core-control.sock 未配置"
docker compose config | grep -q 'traffic/pending.json' || fail "traffic/pending.json spool 未配置"
pass 'docker compose ps 與核心控制邊界'

curl -fsS "${WEB_PUBLIC_URL%/}/health/live" | grep -q '"status":"ok"' || fail "/health/live 失敗"
curl -fsS "${WEB_PUBLIC_URL%/}/health/ready" | grep -q '"status":"ready"' || fail "/health/ready 失敗"
pass 'Web /health/live 與 /health/ready'

telegram_call getMe
bot_id="$(jq -er '.result.id | select(type == "number" and . > 0)' "$tmp_dir/telegram-response.json")" || fail "getMe 未回傳有效 Bot ID"
telegram_call getChatMember "{\"chat_id\":${VERIFY_QUALIFICATION_CHAT_ID},\"user_id\":${bot_id}}"
jq -e '.result.status == "administrator" or .result.status == "creator"' "$tmp_dir/telegram-response.json" >/dev/null || fail "Bot 不是資格群組管理員"
pass 'Telegram getMe 與 getChatMember 真實 API'

openssl s_client -connect "${VERIFY_TLS_SERVER_NAME}:${VERIFY_TLS_PORT}" -servername "$VERIFY_TLS_SERVER_NAME" -verify_return_error </dev/null >"$tmp_dir/tls.txt" 2>&1 || fail "TLS 憑證或名稱驗證失敗"
grep -q 'Verify return code: 0' "$tmp_dir/tls.txt" || fail "TLS chain 驗證失敗"
pass '受信任 TLS 憑證與名稱'

subscription_config="$tmp_dir/subscription.conf"
printf 'url = "%s"\nsilent\nshow-error\nfail\n' "$VERIFY_SUBSCRIPTION_URL" > "$subscription_config"
chmod 0600 "$subscription_config"
curl --config "$subscription_config" -o "$tmp_dir/subscription.txt"
base64 -d "$tmp_dir/subscription.txt" > "$tmp_dir/uris.txt" 2>/dev/null || fail "Base64 訂閱無法解碼"
for protocol in 'vless://' 'hysteria2://' 'tuic://' 'anytls://'; do
  grep -qi "$protocol" "$tmp_dir/uris.txt" || fail "訂閱缺少 ${protocol}"
done
grep -q '\[' "$tmp_dir/uris.txt" || fail "訂閱未包含 IPv6 節點"
pass 'sing-box、Hysteria2、TUIC、AnyTLS 與雙棧訂閱結構'

docker exec s12ryt-sing-box /usr/local/bin/sing-box check -c /var/lib/s12ryt/sing-box/config.json >/dev/null || fail "sing-box check 失敗"
docker exec s12ryt-sing-box /usr/local/bin/sing-box version >/dev/null || fail "sing-box binary 不可執行"
pass 'sing-box 核心設定'

[[ -f "$VERIFY_EXTERNAL_EVIDENCE_FILE" && ! -L "$VERIFY_EXTERNAL_EVIDENCE_FILE" ]] || fail "外部驗收證據必須是一般檔案且不可為符號連結"
jq -e '
  . as $root |
  ($root | type == "object") and
  $root.schema_version == 1 and
  ($root.recorded_at | type == "string" and length > 0) and
  ($root.operator | type == "string" and length > 0) and
  ($root.host | type == "string" and length > 0) and
  ([
    "protocols_dual_stack",
    "ipv6_only_egress",
    "ipv4_enabled_egress",
    "traffic_accounting",
    "quota_enforcement",
    "period_recovery",
    "restart_behavior",
    "concurrent_connections_600"
  ] | all(. as $name |
    ($root.checks[$name].passed == true) and
    ($root.checks[$name].evidence | type == "string" and length > 0)
  ))
' "$VERIFY_EXTERNAL_EVIDENCE_FILE" >/dev/null || fail "外部驗收證據不完整"
pass '外部四協定雙棧、IPv6-only／IPv4 出站、計量、配額、重啟與600連線驗收證據'
