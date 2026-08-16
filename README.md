# S12RYT VPN Bot

通過 Telegram 群組資格驗證後，才可領取私人 VPN 訂閱的管理系統。後端使用 Go 與 PostgreSQL，管理介面使用 React/TypeScript，單一 sing-box 核心提供 VLESS REALITY、Hysteria2、TUIC 與 AnyTLS。

> **開發中**：目前已有核心領域、Telegram 資格查核、管理登入、使用者核准／拒絕／撤銷／輪替、共享配額、流量故障封閉、sing-box 設定、三格式訂閱與加密備份／還原等受測試保護的程式碼。安裝精靈、ACME 自動化、完整設定頁與部署後端到端驗收尚未完成，請勿直接用於正式環境。

## 已實作範圍

- Telegram `chat_member` 即時資格事件，以及具節流、退避與三值判定的週期重查。
- 每位使用者四協定獨立憑證、一個私人訂閱 URL，支援 sing-box JSON、Mihomo/Clash 與 Base64 URI。
- 四協定與 IPv4／IPv6 共用 50 GB、30 日固定週期配額。
- sing-box deterministic 設定、IPv6-only 預設出站、受控重啟、durable outbox 與失敗回滾。
- 15 秒流量採集、原子本機 spool、PostgreSQL 冪等入帳與持續故障 5 分鐘後 fail-closed。
- Bot 私聊 `/adminlogin`、8 位一次性登入碼、安全 Session、CSRF、RBAC 與多層登入限速。
- Bot 私聊 `/vpn`、`/status` 與 QR，以及管理員 inline 核准／拒絕和查詢／撤銷／輪替命令。
- 繁體中文響應式使用者、角色、稽核與資格／配額政策管理頁。
- 非 root 多階段映像與 Compose 安全拓撲契約；只有受限 sidecar 可存取 Docker socket。
- GitHub release workflow 解析最新穩定 sing-box 原始碼，沿用官方 Linux build tags／linker flags與必要的 `with_purego`，另啟用 `with_v2ray_api`，產生 amd64/arm64 GHCR images、SBOM、provenance與漏洞掃描。
- 每日 PostgreSQL custom-format dump、用途隔離 AES-GCM 加密、可調保留期與完整性驗證後還原。

完整需求與驗收契約見 [`agent/question.md`](agent/question.md)。

## 尚未完成

- 完整 Web 儀表板、資格規則新增／停用、VPN／網路、TLS／網域與備份設定頁。
- sslip.io、DuckDNS、自有網域的 ACME 申請與續期。
- 安裝精靈、ACME 自動化與部署後驗收腳本。
- release workflow 的首次真實發佈與 digest 驗收。

## 開發需求

- Go 1.26+
- Node.js 24+ 與 npm 11+
- PostgreSQL 17（只在 integration test 與實際執行時需要）
- Docker Engine 與 Docker Compose v2（容器契約與部署驗證）

## 本地驗證

```bash
go test ./...
go vet ./...
go build ./cmd/server
go build ./cmd/core-controller

cd web
npm ci
npm test
npm run lint
npm run build
```

PostgreSQL migration integration test 需要可用資料庫：

```bash
DATABASE_URL='postgresql://user:password@127.0.0.1:5432/database' \
  go test -tags=integration ./integration
```

## 設定與容器拓撲

從 [`.env.example`](.env.example) 建立本機 `.env`，不得提交真實秘密。至少要設定：

- `APP_MASTER_KEY`：標準 Base64 編碼的 32 bytes 根金鑰。
- `BOT_TOKEN`：Telegram Bot token。
- `OWNER_TG_ID`：不可移除的根擁有者 Telegram ID。
- `WEB_PUBLIC_URL`：外部反向代理提供的公開 HTTPS base URL。
- `DATABASE_URL`：完整且正確 percent-encode 的 PostgreSQL URL。
- `SINGBOX_IMAGE`：不可變版本或 image digest，不可使用 `latest`。
- `DOCKER_GID`：主機 Docker socket 的群組 ID。

`compose.yaml` 不含公開反向代理。應用預設監聽 `0.0.0.0:35699`，正式部署必須由外部 Nginx、Caddy 或 Cloudflare Tunnel 提供受信任 HTTPS，並以防火牆限制應用 port 的來源。VLESS REALITY 預設使用 TCP 443，同一 IP 上的 Web 反向代理不可同時占用該 port。

Compose 目前包含：

- PostgreSQL，僅發布到主機 loopback。
- Go 應用與 sing-box，使用 host network 以明確綁定主機 IPv4／IPv6。
- `core-controller` sidecar，唯一可掛載 Docker socket，且只控制固定的 sing-box container。
- 權限初始化工作，讓非 root containers 使用持久 volumes。
- 每日執行的非 root 加密 PostgreSQL 備份 service；操作方式見 [`docs/backup-restore.md`](docs/backup-restore.md)。

本機尚未具備 Docker；Compose config、image build、PostgreSQL migration 與 race detector 證據由 GitHub Actions 提供。

## 安全注意事項

- 不要在 issue、日誌、截圖或 CI artifact 中提供 Bot token、`APP_MASTER_KEY`、訂閱 token 或 VPN 憑證。
- 管理面板必須位於受信任 HTTPS 後方；不要將 `35699` 無限制公開到 Internet。
- `TRUSTED_PROXY_CIDRS` 只應包含實際受控的反向代理來源。
- Docker socket 等同高權限主機控制面；不得掛載到 Web 應用 container。
- 專案仍在開發中，尚未通過真實 Telegram、ACME、四協定與 600 連線部署後驗收。

## 授權

本專案以 **GNU Affero General Public License v3.0 only** 發布，完整條款見 [`LICENSE`](LICENSE)。透過網路提供修改版本時，必須依 AGPL-3.0 第 13 節向使用者提供對應原始碼。

本程式不提供任何擔保；詳見授權條款第 15、16 節。
