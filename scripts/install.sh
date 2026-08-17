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

valid_ipv4() {
  local candidate="$1"
  awk -v address="$candidate" 'BEGIN {
    count = split(address, octets, ".")
    if (count != 4) exit 1
    for (index = 1; index <= 4; index++) {
      if (octets[index] !~ /^[0-9]+$/ || octets[index] < 0 || octets[index] > 255) exit 1
      if (length(octets[index]) > 1 && substr(octets[index], 1, 1) == "0") exit 1
    }
  }'
}

valid_ipv6() {
  local candidate="$1"
  [[ "$candidate" == *:* && "$candidate" != *[[:space:]]* ]] || return 1
  getent ahostsv6 "$candidate" >/dev/null 2>&1
}

detect_public_address() {
  local family="$1" curl_family validator source response candidate
  local -a sources=(
    'https://api.ipify.org'
    'https://ifconfig.co/ip'
    'https://icanhazip.com'
  )
  local -A counts=()

  case "$family" in
    4) curl_family='-4'; validator='valid_ipv4' ;;
    6) curl_family='-6'; validator='valid_ipv6' ;;
    *) return 1 ;;
  esac

  for source in "${sources[@]}"; do
    response="$(curl "$curl_family" -fsS --max-time 5 --user-agent 's12ryt-vpn-installer/1' "$source" 2>/dev/null || true)"
    candidate="${response//$'\r'/}"
    candidate="${candidate//$'\n'/}"
    if [[ -n "$candidate" && "$candidate" != *[[:space:]]* ]] && "$validator" "$candidate"; then
      counts["$candidate"]="$(( ${counts["$candidate"]:-0} + 1 ))"
      printf '公開 IPv%s 來源 %s 回報：%s\n' "$family" "$source" "$candidate" >&2
    else
      printf '公開 IPv%s 來源 %s 無有效結果\n' "$family" "$source" >&2
    fi
  done

  for candidate in "${!counts[@]}"; do
    if (( counts["$candidate"] >= 2 )); then
      printf '%s\n' "$candidate"
      return 0
    fi
  done
  printf '公開 IPv%s 未取得至少兩個外部來源的一致結果\n' "$family" >&2
  return 1
}

prompt_public_address() {
  local family="$1" detected="$2" output_name="$3" input validator
  local -n output="$output_name"
  case "$family" in
    4) validator='valid_ipv4' ;;
    6) validator='valid_ipv6' ;;
    *) fail "未知的公開位址 family" ;;
  esac

  printf '可信公開 IPv%s 候選：%s\n' "$family" "${detected:-無；請手動輸入或停用}" >&2
  read -r -p "公開 IPv${family}（留空停用） [${detected:-無}]：" input
  if [[ -n "$input" ]]; then
    "$validator" "$input" || fail "公開 IPv${family} 格式無效"
  fi
  output="$input"
}

confirm_public_addresses() {
  local detected_ipv4 detected_ipv6 confirmation
  detected_ipv4="$(detect_public_address 4 || true)"
  detected_ipv6="$(detect_public_address 6 || true)"
  prompt_public_address 4 "$detected_ipv4" PUBLIC_IPV4
  prompt_public_address 6 "$detected_ipv6" PUBLIC_IPV6
  [[ -n "$PUBLIC_IPV4" || -n "$PUBLIC_IPV6" ]] || fail "公開 IPv4／IPv6 不可同時停用"
  printf '確認值：IPv4=%s，IPv6=%s\n' "${PUBLIC_IPV4:-停用}" "${PUBLIC_IPV6:-停用}"
  read -r -p '確認公開位址並繼續產生設定與啟動？[y/N] ' confirmation
  case "$confirmation" in
    y|Y|yes|YES) ;;
    *) fail "部署者未確認公開位址" ;;
  esac
}

PUBLIC_IPV4=''
PUBLIC_IPV6=''
confirm_public_addresses

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
DOCKER_GID=${DOCKER_GID}
WEB_IP=0.0.0.0
PORT=35699
TRUSTED_PROXY_CIDRS=
PUBLIC_IPV4=${PUBLIC_IPV4}
PUBLIC_IPV6=${PUBLIC_IPV6}
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
