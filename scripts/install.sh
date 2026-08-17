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

for command_name in docker openssl curl getent jq ss; do require_cmd "$command_name"; done
docker compose version >/dev/null 2>&1 || fail "需要 Docker Compose v2"
docker info >/dev/null 2>&1 || fail "目前帳號無法存取 Docker Engine"

INSTALL_TEMP_DIR="$(mktemp -d)"
chmod 0700 "$INSTALL_TEMP_DIR"
cleanup_install_temp() { rm -rf -- "$INSTALL_TEMP_DIR"; }
trap cleanup_install_temp EXIT
trap 'exit 130' INT
trap 'exit 129' HUP
trap 'exit 143' TERM

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

valid_domain() {
  local candidate="$1"
  [[ ${#candidate} -le 253 && "$candidate" =~ ^([A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63}$ ]]
}

parse_web_public_url() {
  local remainder authority host port=''
  [[ "$WEB_PUBLIC_URL" == https://* && "$WEB_PUBLIC_URL" != *['?'#@]* ]] || fail "WEB_PUBLIC_URL 必須是無 userinfo／query／fragment 的 HTTPS URL"
  remainder="${WEB_PUBLIC_URL#https://}"
  authority="${remainder%%/*}"
  [[ -n "$authority" ]] || fail "WEB_PUBLIC_URL 缺少 host"
  if [[ "$authority" == \[* ]]; then
    [[ "$authority" == *']'* ]] || fail "WEB_PUBLIC_URL IPv6 host 格式無效"
    host="${authority#\[}"
    host="${host%%\]*}"
    remainder="${authority#*\]}"
    if [[ -n "$remainder" ]]; then
      [[ "$remainder" == :* ]] || fail "WEB_PUBLIC_URL authority 格式無效"
      port="${remainder#:}"
    fi
    valid_ipv6 "$host" || fail "WEB_PUBLIC_URL IPv6 host 格式無效"
  else
    [[ "$authority" != *:*:* ]] || fail "WEB_PUBLIC_URL IPv6 host 必須使用方括號"
    host="${authority%%:*}"
    if [[ "$authority" == *:* ]]; then
      port="${authority##*:}"
    fi
    if ! valid_ipv4 "$host"; then
      valid_domain "$host" || fail "WEB_PUBLIC_URL host 格式無效"
    fi
  fi
  if [[ -n "$port" ]]; then
    [[ "$port" =~ ^[0-9]+$ ]] && (( port >= 1 && port <= 65535 )) || fail "WEB_PUBLIC_URL port 格式無效"
  fi
  WEB_URL_HOST="$host"
  WEB_URL_PORT="$port"
}

validate_bootstrap_inputs() {
  [[ "$BOT_TOKEN" =~ ^[0-9]+:[A-Za-z0-9_-]{20,}$ ]] || fail "BOT_TOKEN 格式無效"
  [[ "$OWNER_TG_ID" =~ ^[1-9][0-9]*$ ]] || fail "OWNER_TG_ID 必須是正整數"
  [[ "$SINGBOX_IMAGE" =~ ^[A-Za-z0-9./_-]+(:v[0-9][A-Za-z0-9._-]*|@sha256:[a-f0-9]{64})$ ]] || fail "SINGBOX_IMAGE 必須使用安全的不可變版本或 digest"
  [[ "$WEB_PUBLIC_URL" =~ ^https://[A-Za-z0-9._~:/%+,=\[\]-]+$ ]] || fail "WEB_PUBLIC_URL bootstrap 值含有不安全字元"
  parse_web_public_url
}

verify_bot_identity() {
  local response config
  response="${INSTALL_TEMP_DIR}/telegram-get-me.json"
  config="${INSTALL_TEMP_DIR}/telegram-get-me.curl"
  : > "$response"
  : > "$config"
  chmod 0600 "$response" "$config"
  printf 'url = "https://api.telegram.org/bot%s/getMe"\nsilent\nshow-error\nfail\n' "$BOT_TOKEN" > "$config"
  if ! curl --config "$config" -o "$response"; then
    rm -f -- "$response" "$config"
    fail "Telegram Bot token 無法通過 getMe 驗證"
  fi
  if ! jq -e '.ok == true and (.result.id | type == "number" and . > 0) and .result.is_bot == true and (.result.username | type == "string" and length > 0)' "$response" >/dev/null; then
    rm -f -- "$response" "$config"
    fail "Telegram getMe 未回傳有效 Bot 身分"
  fi
  rm -f -- "$response" "$config"
}

validate_acme_preflight() {
  case "$ACME_MODE_REFERENCE" in
    sslip_io)
      [[ "$ACME_DOMAIN_REFERENCE" == *.sslip.io ]] || fail "sslip.io mode 的網域必須以 .sslip.io 結尾"
      [[ "$ACME_CHALLENGE_REFERENCE" == http_01 ]] || fail "sslip.io mode 必須使用 HTTP-01"
      ;;
    duckdns)
      [[ "$ACME_DOMAIN_REFERENCE" == *.duckdns.org ]] || fail "DuckDNS mode 的網域必須以 .duckdns.org 結尾"
      [[ "$ACME_CHALLENGE_REFERENCE" == dns_01 ]] || fail "DuckDNS mode 必須使用 DNS-01"
      ;;
    custom)
      [[ "$ACME_CHALLENGE_REFERENCE" == http_01 ]] || fail "自有網域目前只支援 HTTP-01"
      ;;
    *) fail "ACME mode 必須是 sslip_io、duckdns 或 custom" ;;
  esac
  valid_domain "$ACME_DOMAIN_REFERENCE" || fail "ACME 網域格式無效"
  getent ahosts "$ACME_DOMAIN_REFERENCE" >/dev/null || fail "ACME 網域 DNS 無法解析"
  [[ "$ACME_TERMS_ACCEPTED_REFERENCE" == true ]] || fail "必須先同意候選 ACME CA 條款"
}

collect_acme_preflight() {
  local terms
  read -r -p 'ACME mode（sslip_io／duckdns／custom）：' ACME_MODE_REFERENCE
  read -r -p 'VPN TLS 網域：' ACME_DOMAIN_REFERENCE
  case "$ACME_MODE_REFERENCE" in
    sslip_io) ACME_CHALLENGE_REFERENCE='http_01' ;;
    duckdns) ACME_CHALLENGE_REFERENCE='dns_01' ;;
    custom) ACME_CHALLENGE_REFERENCE='http_01' ;;
    *) fail "ACME mode 必須是 sslip_io、duckdns 或 custom" ;;
  esac
  printf '候選 CA 條款：\n- https://letsencrypt.org/repository/\n- https://zerossl.com/terms/\n'
  read -r -p '已閱讀並同意候選 ACME CA 條款？[y/N] ' terms
  case "$terms" in
    y|Y|yes|YES) ACME_TERMS_ACCEPTED_REFERENCE=true ;;
    *) fail "部署者未同意候選 ACME CA 條款" ;;
  esac
  validate_acme_preflight
}

web_host_resolves_to() {
  local expected="$1" family resolved expected_canonical
  if valid_ipv4 "$expected"; then family='ahostsv4'; else family='ahostsv6'; fi
  expected_canonical="$(getent "$family" "$expected" | awk 'NR == 1 { print $1 }')"
  [[ -n "$expected_canonical" ]] || return 1
  while read -r resolved _; do
    [[ "$resolved" == "$expected_canonical" ]] && return 0
  done < <(getent "$family" "$WEB_URL_HOST")
  return 1
}

validate_web_https_topology() {
  parse_web_public_url
  case "$WEB_HTTPS_TOPOLOGY" in
    second_ip)
      [[ -n "$WEB_PROXY_IP" ]] || fail "第二 IP topology 必須設定 WEB_PROXY_IP"
      valid_ipv4 "$WEB_PROXY_IP" || valid_ipv6 "$WEB_PROXY_IP" || fail "WEB_PROXY_IP 格式無效"
      [[ "$WEB_PROXY_IP" != "$PUBLIC_IPV4" && "$WEB_PROXY_IP" != "$PUBLIC_IPV6" ]] || fail "第二 IP 必須與 VPN 公開位址不同"
      web_host_resolves_to "$WEB_PROXY_IP" || fail "WEB_PUBLIC_URL 未解析到 WEB_PROXY_IP"
      ;;
    custom_port)
      [[ -n "$WEB_URL_PORT" && "$WEB_URL_PORT" != 443 ]] || fail "自訂 HTTPS port 不可為 443"
      case "$WEB_URL_PORT" in
        80|443|8443|35699) fail "自訂 HTTPS port 與既有 TCP 服務衝突" ;;
      esac
      [[ -z "$WEB_PROXY_IP" ]] || fail "custom_port topology 不使用 WEB_PROXY_IP"
      ;;
    cloudflare_tunnel)
      [[ -z "$WEB_PROXY_IP" ]] || fail "Cloudflare Tunnel topology 不使用 WEB_PROXY_IP"
      ;;
    *) fail "WEB HTTPS topology 必須是 second_ip、custom_port 或 cloudflare_tunnel" ;;
  esac
}

collect_web_https_topology() {
  read -r -p 'Web HTTPS topology（second_ip／custom_port／cloudflare_tunnel）：' WEB_HTTPS_TOPOLOGY
  WEB_PROXY_IP=''
  if [[ "$WEB_HTTPS_TOPOLOGY" == second_ip ]]; then
    read -r -p 'Web 反向代理第二個公開 IP：' WEB_PROXY_IP
  fi
  validate_web_https_topology
}

run_installation_preflight() {
  local mode="$1"
  verify_bot_identity
  if [[ "$mode" == new ]]; then
    collect_acme_preflight
    collect_web_https_topology
  else
    validate_acme_preflight
    validate_web_https_topology
  fi
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
  case "$family" in
    4) validator='valid_ipv4' ;;
    6) validator='valid_ipv6' ;;
    *) fail "未知的公開位址 family" ;;
  esac

  printf '可信公開 IPv%s 候選：%s\n' "$family" "${detected:-無；請手動輸入或停用}" >&2
  read -r -p "公開 IPv${family} [${detected:-無}]（Enter 採用，輸入 - 停用）：" input
  input="${input:-$detected}"
  if [[ "$input" == '-' ]]; then
    input=''
  fi
  if [[ -n "$input" ]]; then
    "$validator" "$input" || fail "公開 IPv${family} 格式無效"
  fi
  printf -v "$output_name" '%s' "$input"
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

confirm_configured_public_addresses() {
  local confirmation
  PUBLIC_IPV4="${PUBLIC_IPV4:-}"
  PUBLIC_IPV6="${PUBLIC_IPV6:-}"
  [[ -z "$PUBLIC_IPV4" ]] || valid_ipv4 "$PUBLIC_IPV4" || fail ".env PUBLIC_IPV4 格式無效"
  [[ -z "$PUBLIC_IPV6" ]] || valid_ipv6 "$PUBLIC_IPV6" || fail ".env PUBLIC_IPV6 格式無效"
  [[ -n "$PUBLIC_IPV4" || -n "$PUBLIC_IPV6" ]] || fail ".env 公開 IPv4／IPv6 不可同時停用"
  printf '既有 .env 值：IPv4=%s，IPv6=%s\n' "${PUBLIC_IPV4:-停用}" "${PUBLIC_IPV6:-停用}"
  read -r -p '確認既有 .env 公開位址並繼續？[y/N] ' confirmation
  case "$confirmation" in
    y|Y|yes|YES) ;;
    *) fail "部署者未確認既有 .env 公開位址" ;;
  esac
}

PUBLIC_IPV4=''
PUBLIC_IPV6=''
WEB_URL_HOST=''
WEB_URL_PORT=''
ACME_MODE_REFERENCE=''
ACME_DOMAIN_REFERENCE=''
ACME_CHALLENGE_REFERENCE=''
ACME_TERMS_ACCEPTED_REFERENCE=''
WEB_HTTPS_TOPOLOGY=''
WEB_PROXY_IP=''
new_environment=false

if [[ ! -f .env ]]; then
  confirm_public_addresses
  printf '首次設定，不會在終端輸出 Bot token 或根金鑰。\n'
  read -r -p 'Telegram Bot Token: ' BOT_TOKEN
  printf '\n'
  read -r -p '根擁有者 Telegram ID: ' OWNER_TG_ID
  read -r -p 'Web 公開 HTTPS URL: ' WEB_PUBLIC_URL
  read -r -p '不可變 sing-box image（version 或 digest）: ' SINGBOX_IMAGE
  validate_bootstrap_inputs
  run_installation_preflight new

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
ACME_MODE_REFERENCE=${ACME_MODE_REFERENCE}
ACME_DOMAIN_REFERENCE=${ACME_DOMAIN_REFERENCE}
ACME_CHALLENGE_REFERENCE=${ACME_CHALLENGE_REFERENCE}
ACME_TERMS_ACCEPTED_REFERENCE=${ACME_TERMS_ACCEPTED_REFERENCE}
WEB_HTTPS_TOPOLOGY=${WEB_HTTPS_TOPOLOGY}
WEB_PROXY_IP=${WEB_PROXY_IP}
EOF
  chmod 0600 .env
  unset APP_MASTER_KEY BOT_TOKEN POSTGRES_PASSWORD
  new_environment=true
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
for reference_name in ACME_MODE_REFERENCE ACME_DOMAIN_REFERENCE ACME_CHALLENGE_REFERENCE ACME_TERMS_ACCEPTED_REFERENCE WEB_HTTPS_TOPOLOGY; do
  [[ -n "${!reference_name:-}" ]] || fail "缺少安裝預檢參考值：${reference_name}"
done
validate_bootstrap_inputs
if [[ "$new_environment" != true ]]; then
  confirm_configured_public_addresses
  run_installation_preflight existing
fi

getent ahosts "$WEB_URL_HOST" >/dev/null || fail "WEB_PUBLIC_URL DNS 無法解析"

check_port_free() {
  local protocol="$1" port="$2" label="$3"
  if ss -H -l"$protocol"n | awk '{print $5}' | grep -Eq "[:.]${port}$"; then
    fail "${label} 已被其他程序占用"
  fi
}
if [[ "$ACME_CHALLENGE_REFERENCE" == http_01 ]]; then
  check_port_free t 80 'TCP 80（ACME HTTP-01）'
fi
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
