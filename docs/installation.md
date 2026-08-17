# 安裝與部署後驗收

> 專案仍在開發中。此流程會建立容器與資料 volume，但在 ACME 自動化完成且部署後人工網路驗收通過前，不應視為正式環境就緒。

## 主機前提

- Linux amd64 或 arm64。
- Docker Engine 與 Docker Compose v2。
- `curl`、`openssl`、`getent`、`ss`、`jq`。
- TCP 443、TCP 8443、UDP 443、UDP 8443 可由公網到達。
- 外部 HTTPS 反向代理已依 [`docs/reverse-proxy.md`](reverse-proxy.md) 規劃，不與 VLESS REALITY 的 TCP 443 衝突。
- 已取得不可變的 sing-box image version 或 digest。若正式 release 被漏洞閘門阻擋，不得改用未掃描的 `latest`。

## 安裝

從 repository 根目錄執行：

```bash
chmod +x scripts/install.sh scripts/post-deploy-check.sh
./scripts/install.sh
```

安裝器會先分別向 `api.ipify.org`、`ifconfig.co` 與 `icanhazip.com` 查詢公開 IPv4／IPv6。只有至少兩個外部來源回報相同位址時，該值才會列為可信候選；部署者必須逐一輸入確認值，也可留空停用單一 address family。IPv4 與 IPv6 不可同時停用，最終未明確確認時腳本會在 Compose 驗證與啟動前中止。

首次執行還會互動收集 Bot token、根擁有者 Telegram ID、公開 HTTPS URL 與 sing-box image，並以 `0600` 建立 `.env`。確認後的 `PUBLIC_IPV4`／`PUBLIC_IPV6` 會保留在新建的 `.env`，供擁有者完成 Web「VPN 與網路」設定時核對；它們不會繞過 Web 對完整核心、TLS 與 REALITY 設定的驗證。腳本不會把 Bot token 或 `APP_MASTER_KEY` 印到終端。既有 `.env` 不會被覆蓋。

安裝器會在啟動前檢查作業系統、CPU 架構、Docker 權限、Compose、必要環境值、DNS、公開 IPv4／IPv6，以及四協定與 Web ports；接著先執行 `docker compose config --quiet`，再 pull/build/up。

啟動後仍須由擁有者登入 Web 面板，完成資格群組、VPN 核心位址、TLS／REALITY 與外部反向代理設定。備份與還原請依 [`docs/backup-restore.md`](backup-restore.md) 驗證。

## 自動化部署後檢查

先準備一位已啟用使用者的私人訂閱 URL、資格群組 ID 及 TLS 名稱：

```bash
export VERIFY_QUALIFICATION_CHAT_ID=-1001234567890
export VERIFY_SUBSCRIPTION_URL='https://vpn.example.com/sub/REDACTED'
export VERIFY_TLS_SERVER_NAME='node.example.com'
export VERIFY_TLS_PORT=8443
export VERIFY_EXTERNAL_EVIDENCE_FILE='/root/vpn-acceptance-evidence.json'
./scripts/post-deploy-check.sh
```

腳本會實際呼叫 Telegram `getMe` 與 `getChatMember`，確認 Bot 是資格群組管理員；驗證 Web live/ready、TLS chain與名稱、私人訂閱四協定／雙棧結構、sing-box config、容器與受限核心控制／traffic spool 掛載。訂閱 URL 只寫入權限受限的暫存 curl config，離開時刪除。腳本最後會驗證外部測試證據；缺少證據或任一項未通過時會以非零狀態結束，不會把局部結構檢查誤報成完整驗收。

外部客戶端與負載測試完成後，建立不含 token、密碼或憑證的 JSON manifest。每個 `evidence` 必須是相對於 manifest 所在目錄的安全路徑，指向實際存在、非 symlink 的測試輸出、監控快照或變更紀錄；絕對路徑、`..` 與經父層 symlink 逃出該目錄的引用都會被拒絕：

```json
{
  "schema_version": 1,
  "recorded_at": "2026-08-18T12:00:00Z",
  "operator": "deployment-owner",
  "host": "vpn.example.com",
  "checks": {
    "protocols_dual_stack": {"passed": true, "evidence": "evidence/protocols-dual-stack.txt"},
    "ipv6_only_egress": {"passed": true, "evidence": "evidence/ipv6-only.txt"},
    "ipv4_enabled_egress": {"passed": true, "evidence": "evidence/ipv4-enabled.txt"},
    "traffic_accounting": {"passed": true, "evidence": "evidence/traffic-accounting.txt"},
    "quota_enforcement": {"passed": true, "evidence": "evidence/quota-enforcement.txt"},
    "period_recovery": {"passed": true, "evidence": "evidence/period-recovery.txt"},
    "restart_behavior": {"passed": true, "evidence": "evidence/restart-behavior.txt"},
    "concurrent_connections_600": {"passed": true, "evidence": "evidence/load-600.txt"}
  }
}
```

## 未完整驗證

單台安裝主機上的結構檢查不能證明真實公網路徑或負載。正式交付前仍必須從外部客戶端逐項留下證據：

1. VLESS REALITY、Hysteria2、TUIC、AnyTLS 的 IPv4 與 IPv6 實際握手及資料傳輸。
2. IPv6-only 出站時 IPv4 literal 被拒絕、網域只走 IPv6；開啟 IPv4 後行為正確。
3. 上下載合計計量、50GB 配額封鎖、固定30日週期恢復與跨協定共享。
4. 計畫重啟30秒通知、安全撤銷立即套用及核心失敗 rollback。
5. 1,000 使用者設定與600並行真實連線的主機吞吐、延遲與資源用量。

以上任一項未執行時，不得建立 `passed: true` 的證據項目；腳本會以「外部驗收證據不完整」失敗。不得以 CI fake、設定語法檢查或人工推測取代真實證據。
