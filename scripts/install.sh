#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$ROOT_DIR"

fail() { printf '安裝失敗：%s\n' "$1" >&2; exit 1; }
require_cmd() { command -v "$1" >/dev/null 2>&1 || fail "缺少必要指令：$1"; }

[[ "$(uname -s)" == "Linux" ]] || fail "只支援 Linux 主機"
case "$(uname -m)" in
  x86_64|aarch64) ;;
  *) fail "只支援 x86_64（amd64）或 aarch64（arm64）" ;;
esac

for command_name in docker openssl curl getent ss; do require_cmd "$command_name"; done
docker compose version >/dev/null 2>&1 || fail "需要 Docker Compose v2"
docker info >/dev/null 2>&1 || fail "目前帳號無法存取 Docker Engine"

if [[ ! -f .env ]]; then
  printf '首次設定，不會在終端輸出 Bot token 或根金鑰。\n'
  read -r -p 'Telegram Bot Token: ' BOT_TOKEN
  printf '\n'
  read -r -p '根擁有者 Telegram ID: ' OWNER_TG_ID
  read -r -p 'Web 公開 HTTPS URL: ' WEB_PUBLIC_URL
  read -r -p '不可變 sing-box image（version 或 digest）: ' SINGBOX_IMAGE
  [[ "$BOT_TOKEN" != *$'\n'* && -n "$BOT_TOKEN" ]] || fail "BOT_TOKEN 不可為空"
  [[ "$OWNER_TG_ID" =~ ^[1-9][0-9]*$ ]] || fail "OWNER_TG_ID 必須是正整數"
  [[ "$WEB_PUBLIC_URL" == https://* ]] || fail "WEB_PUBLIC_URL 必須使用 HTTPS"
  [[ "$SINGBOX_IMAGE" == *@sha256:* || "$SINGBOX_IMAGE" =~ :v[0-9] ]] || fail "SINGBOX_IMAGE 必須使用不可變版本或 digest"

  APP_MASTER_KEY="$(openssl rand -base64 32 | tr -d '\n')"
  POSTGRES_PASSWORD="$(openssl rand -hex 24)"
  DOCKER_GID="$(stat -c '%g' /var/run/docker.sock)"
  umask 077
  cat > .env <<EOF
APP_MASTER_KEY=${APP_MASTER_KEY}
BOT_TOKEN=${BOT_TOKEN}
OWNER_TG_ID=${OWNER_TG_ID}
WEB_PUBLIC_URL=${WEB_PUBLIC_URL}
POSTGRES_DB=s12ryt_vpn
POSTGRES_USER=s12ryt_vpn
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
POSTGRES_PORT=5432
DATABASE_URL=postgresql://s12ryt_vpn:${POSTGRES_PASSWORD}@127.0.0.1:5432/s12ryt_vpn
SINGBOX_IMAGE=${SINGBOX_IMAGE}
APP_IMAGE=s12ryt-vpn-bot:local
CONTROLLER_IMAGE=s12ryt-vpn-core-controller:local
BACKUP_IMAGE=s12ryt-vpn-backup:local
BACKUP_RETENTION_DAYS=7
DOCKER_GID=${DOCKER_GID}
WEB_IP=0.0.0.0
PORT=35699
TRUSTED_PROXY_CIDRS=
EOF
  chmod 0600 .env
  unset APP_MASTER_KEY BOT_TOKEN POSTGRES_PASSWORD
fi

[[ -O .env ]] || fail ".env 必須由目前帳號擁有"
permissions="$(stat -c '%a' .env)"
(( (8#$permissions & 077) == 0 )) || fail ".env 不得授予 group 或 other 任何權限"

set -a
# shellcheck disable=SC1091
source .env
set +a
for variable_name in APP_MASTER_KEY BOT_TOKEN OWNER_TG_ID WEB_PUBLIC_URL DATABASE_URL SINGBOX_IMAGE DOCKER_GID; do
  [[ -n "${!variable_name:-}" ]] || fail "缺少必要環境值：${variable_name}"
done

detected_ipv4="$(curl -4fsS --max-time 5 https://api.ipify.org || true)"
detected_ipv6="$(curl -6fsS --max-time 5 https://api64.ipify.org || true)"
printf '偵測公開 IPv4：%s\n' "${detected_ipv4:-無}"
printf '偵測公開 IPv6：%s\n' "${detected_ipv6:-無}"
web_host="${WEB_PUBLIC_URL#https://}"; web_host="${web_host%%/*}"; web_host="${web_host%%:*}"
getent ahosts "$web_host" >/dev/null || fail "WEB_PUBLIC_URL DNS 無法解析"

check_port_free() {
  local protocol="$1" port="$2" label="$3"
  if ss -H -l"$protocol"n | awk '{print $5}' | grep -Eq "[:.]${port}$"; then
    fail "${label} 已被其他程序占用"
  fi
}
check_port_free t 80 'TCP 80（ACME HTTP-01）'
check_port_free t 443 'TCP 443（VLESS REALITY）'
check_port_free u 443 'UDP 443（Hysteria2）'
check_port_free u 8443 'UDP 8443（TUIC）'
check_port_free t 8443 'TCP 8443（AnyTLS）'
check_port_free t 35699 '127.0.0.1:35699（Web 管理服務）'

docker compose config --quiet
if ! docker compose pull postgres sing-box; then
  fail "映像拉取失敗；若 sing-box 套件為 private，請先在 GitHub 套件設定改為 public，或以具 read:packages 權限的 token 執行 docker login ghcr.io（見 README「套件可見度」一節）"
fi
docker compose build app backup core-controller
docker compose up -d
printf '容器已啟動。下一步：設定外部 HTTPS 反向代理，完成 Web 核心／TLS 設定，再執行 scripts/post-deploy-check.sh。\n'
