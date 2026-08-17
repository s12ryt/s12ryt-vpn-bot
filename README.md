# S12RYT VPN Bot

通過 Telegram 群組資格驗證後，才可領取私人 VPN 訂閱的管理系統。後端使用 Go 與 PostgreSQL，管理介面使用 React/TypeScript，單一 sing-box 核心提供 VLESS REALITY、Hysteria2、TUIC 與 AnyTLS。

> **開發中**：目前已有核心領域、Telegram 資格查核、管理登入、使用者核准／拒絕／撤銷／輪替、共享配額、流量故障封閉、sing-box 設定、三格式訂閱、加密備份／還原、主機安裝／部署後檢查腳本，以及 ACME 簽發、TLS 加密持久化、未簽發閘門與自動續期排程等受測試保護的程式碼。真實 Linux 主機上的 Telegram、ACME、四協定與 600 連線端到端驗收尚未完成，請勿直接用於正式環境。

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
- 每日 PostgreSQL custom-format dump、用途隔離 AES-GCM 加密、Web 動態可調保留期與完整性驗證後還原。
- 主機安裝腳本（環境／架構／埠位檢查、三個外部來源交叉偵測公開 IPv4／IPv6、部署者修改與確認、0600 `.env` 產生）與部署後自動檢查腳本（Telegram、TLS、訂閱、核心邊界），見 [`docs/installation.md`](docs/installation.md)。
- ACME 簽發服務與 lego adapter：條款同意、多 CA 備援、sslip.io HTTP-01／DuckDNS DNS-01、憑證與私鑰／網域／有效期驗證，失敗不安裝。
- Bot Token 安全熱切換：AES-GCM 加密保存、`getMe` 驗證同一 Bot 身分後先持久化再原子替換 live client，owner Web「Bot 與 Token」頁操作並留稽核。
- TLS 未核發閘門：核心 active 快照與訂閱輸出在受信任憑證核發且未過期前一律不輸出節點。
- Web 營運總覽：使用者／待審／封鎖統計、TLS 與核心設定狀態與未完成警示。
- REALITY 偽裝目標搜尋與健康監控：內建 pinned 熱門網域資料集，僅探測 443 且要求 TLS 1.3；擁有者於 Web 搜尋並確認採用。目前目標每小時檢查一次，故障／恢復只在持久狀態轉換時通知管理者，永不自動切換。
- 規模驗證：一般 CI 以 race detector 併發匯入 600 位使用者流量並在 PostgreSQL 17 驗證交易一致性；release 在任何 push 前生成 1,000 使用者、四協定、雙棧設定並以當次固定 sing-box binary 執行 `check -c`。

完整需求與驗收契約見 [`agent/question.md`](agent/question.md)。

## 尚未完成

- release workflow 的首次真實發佈與 digest 驗收（目前被上游 sing-box stable 依賴漏洞阻擋）。
- 在實際 Linux 主機以 Docker、真實 Telegram Bot／群組與 ACME 網域完成部署後驗收；自動檢查腳本已備於 [`scripts/post-deploy-check.sh`](scripts/post-deploy-check.sh)，但本機沒有可執行此驗收的環境與秘密。

### 套件可見度（首次成功發佈後必做）

GHCR 上由 workflow 首次推送建立的 package 一律是 **private**。首次 release 成功後，請在 GitHub 套件頁面（Packages → `s12ryt-sing-box`、`s12ryt-vpn-bot`、`s12ryt-vpn-core-controller` → Package settings → Danger Zone → Change visibility → Public）將需要匿名拉取的套件設為 public；或讓部署主機以具 `read:packages` 權限的 token 執行 `docker login ghcr.io`。未調整前，匿名 `docker pull ghcr.io/s12ryt/...` 會被拒絕。

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
  go test -race -tags=integration ./integration
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
- 每日執行的非 root 加密 PostgreSQL 備份 service；保留期由擁有者在 Web 動態管理，操作方式見 [`docs/backup-restore.md`](docs/backup-restore.md)。

本機尚未具備 Docker；Compose config、image build、PostgreSQL migration、600-user 流量交易與 race detector 證據由 GitHub Actions 提供。

## 安全注意事項

- 不要在 issue、日誌、截圖或 CI artifact 中提供 Bot token、`APP_MASTER_KEY`、訂閱 token 或 VPN 憑證。
- 管理面板必須位於受信任 HTTPS 後方；不要將 `35699` 無限制公開到 Internet。
- `TRUSTED_PROXY_CIDRS` 只應包含實際受控的反向代理來源。
- Docker socket 等同高權限主機控制面；不得掛載到 Web 應用 container。
- 專案仍在開發中，尚未通過真實 Telegram、ACME、四協定與 600 連線部署後驗收。

## 授權

本專案以 **GNU Affero General Public License v3.0 only** 發布，完整條款見 [`LICENSE`](LICENSE)。透過網路提供修改版本時，必須依 AGPL-3.0 第 13 節向使用者提供對應原始碼。

本程式不提供任何擔保；詳見授權條款第 15、16 節。
