# 已確認需求契約

最後更新：2026-08-16

本文件是第一版實作與驗收依據。需求異動時必須先同步更新本文件，再修改正式程式碼。

## 1. 交付目標

- 建立一套通過 Telegram 群組資格驗證後才能領取 VPN 的 Telegram Bot 與繁體中文 Web 管理面板。
- 採 Go 模組化單體後端、React + TypeScript 前端、PostgreSQL、Docker Compose 與單一 sing-box 核心。
- 支援 amd64、arm64，公開至 `s12ryt/s12ryt-vpn-bot` 與公開 GHCR。
- 公開 repository 採 AGPL-3.0。

## 2. Telegram 資格

- Web 可管理多個 Telegram supergroup 與 channel。
- 群組啟用前必須驗證 Bot 具備可靠查核成員資格所需的管理員權限。
- 資格規則可選「任一符合」或「全部符合」。
- `chat_member` update 是即時主路徑；明確離群時立即撤銷。
- 每小時分批重新查核全部已知使用者，間隔可由 Web 修改。
- 啟動與群組規則變更後，所有已知使用者排隊重查。
- Telegram 暫時錯誤、限速或逾時不得視為不合格；保留既有狀態、退避重試並告警。只有明確不合格結果可撤銷。
- 查核必須有 rate limit、backoff 與可觀察進度。
- 補償重查預設每秒最多 10 個 Telegram API request、每批 50 位使用者；Web 可調範圍分別為每秒 1–20 request、每批 10–200 位。
- Telegram 429 優先尊重 `retry_after`；其他暫時錯誤使用含 jitter 的指數退避，單一查核最多嘗試 5 次。超限後保留原資格狀態並列為本輪未決。
- 每輪完成後向所有擁有者與管理員彙總成功、明確撤銷、未決數與原因分類，不逐使用者發暫時錯誤通知；無法讀取規則／使用者清單等整體故障須立即告警。

## 3. 使用者生命週期

- 首次符合資格者可由 Bot 私聊自助領取。
- 離群後撤銷全部憑證；重新加入必須由管理員核准。
- 核准可由 Web 或 Bot inline 按鈕完成。
- 管理員手動撤銷仍合格者時，每次可選：可自助重領、需審核、永久封鎖。
- 管理員輪替憑證時，每次可選保留現有用量與週期，或重置為新的 30 天／50GB 週期。
- 撤銷、核准、拒絕、輪替、封鎖、解除封鎖與設定異動都必須留下稽核紀錄。

## 4. VPN 協定與憑證

- 同一 sing-box 核心必須支援：VLESS TCP REALITY、Hysteria2、TUIC、AnyTLS。
- 每位使用者、每種協定使用獨立的高熵隨機憑證。
- VLESS、TUIC 使用 UUID 類憑證；Hysteria2、AnyTLS 使用獨立高熵密碼，實際欄位依 release 時解析出的穩定 sing-box 版本 schema。
- 撤銷或重新核准時，四組協定憑證與訂閱 token 一起失效及輪替。
- 預設 port：VLESS TCP 443、Hysteria2 UDP 443、TUIC UDP 8443、AnyTLS TCP 8443；Web 可修改並在套用前檢查 TCP／UDP 實際衝突。
- 入站模式可選 IPv6-only（預設）、IPv4-only、雙棧。
- 安裝精靈透過多個外部來源自動偵測公開 IPv4／IPv6，再要求部署者確認或修改。
- 雙棧時，每種協定各提供 IPv4 與 IPv6 節點；同一協定的兩個節點共用該協定憑證。
- 雙棧監聽不得依賴 IPv6 wildcard 的 IPv4-mapped 行為；部署者確認伺服器位址後，每種協定分別建立明確綁定 IPv4 與 IPv6 的 inbound。容器部署必須使用可實際綁定這些位址的網路拓撲並在安裝時驗證。
- 四協定所有 `users[].name` 統一使用 Telegram ID 十進位字串，作為僅限本機統計 API 的穩定非秘密識別鍵；不得使用可變 username、姓名或任何憑證內容。

## 5. 訂閱

- 每位使用者只需管理一個不可猜測、可輪替的私人訂閱 URL 與 QR code。
- 同一 URL 依 User-Agent 智慧輸出 sing-box JSON、Mihomo/Clash YAML 或 Base64 URI。
- 未知 User-Agent 預設 Base64；支援 `?format=sing-box|clash|base64` 明確覆寫。
- 只輸出目標客戶端實際支援的協定，禁止產生已知無效節點。
- 進階頁才展開四協定及 IPv4／IPv6 個別 URI 與 QR。
- Bot 只在私聊顯示訂閱、憑證、QR、狀態、用量與重置時間；群組中不得公開秘密。
- 日誌、錯誤與稽核內容不得記錄完整訂閱 token 或完整 VPN 憑證。

## 6. 配額與週期

- 預設配額是每位使用者每期上傳加下載合計 `50,000,000,000 bytes`。
- 四協定、IPv4 與 IPv6 所有節點共享單一配額與單一週期，切換節點不增加額度。
- 週期從首次核發或選擇重置的重新核准時間起算，固定為連續 `30 * 24 小時`。
- 未選擇重置時，有效憑證跨週期沿用。
- 流量最多每 15 秒採樣及歸集，接受採樣間隔造成的少量超額。
- sing-box Stats API 以 `reset=true` 讀取後，必須先把 Telegram ID、上下載 bytes 與採樣時間寫入不含憑證的本機持久 spool，再進行 PostgreSQL 入帳；spool 使用 `0600`、同目錄暫存檔、檔案與目錄同步及原子 rename。程序重啟時先重播未入帳批次，成功提交後才刪除，存在 pending spool 時不得再次 reset Stats。此設計將 reset 到落盤間的極短崩潰窗口降到最低，但不宣稱跨程序絕對原子。
- Stats API、spool 或 PostgreSQL 流量入帳故障時，首次立即通知所有擁有者與管理員；同一故障持續期間每 15 分鐘彙總一次，恢復且完成補帳後通知一次。
- 流量計量故障持續 5 分鐘後採 fail-closed：待 PostgreSQL 可用時撤銷全部 active 憑證；計量恢復並完成補帳後才排 reconcile 恢復符合資格且未達額者。
- 達額後立即撤銷核心存取，到下一週期自動恢復。
- Web 修改配額須立即套用現有當期：降低到低於已用量時立即停用，提高後若重新低於上限則恢復。
- CI 必須模擬 600 名使用者並行匯入統計，驗證跨協定／雙棧合併、交易一致性與無資料競爭。

## 7. 閒置淘汰

- Web 提供全域「連續 N 天無 VPN 流量即移除權限」。
- `0` 代表停用且是預設值；啟用時最小值為 1 天。
- 任一協定、任一 IPv4／IPv6 節點有 uplink 或 downlink bytes 時，更新 `last_vpn_activity_at`。
- 降低門檻前須預覽受影響人數、二次確認並記錄稽核。
- 確認後一次交易標記全部受影響者、逐一排 Bot 私訊，只生成一次核心設定並立即重啟。
- Bot 私訊失敗不阻止淘汰。
- 閒置淘汰者若仍符合 Telegram 資格，可自助重新領取；重新驗證後產生全新訂閱 token、四組新憑證與新週期，不恢復舊憑證。

## 8. 核心設定、流量與重啟

- 後端原子產生 sing-box 設定，先執行官方設定檢查，成功後才可替換現行設定。
- sing-box 須由官方穩定版原始碼建置，只額外加入官方 `with_v2ray_api` build tag。
- release workflow 才解析當時官方最新穩定版；同一次 release 的所有平台共用同一解析版本，並輸出來源 commit、版本、校驗值、SBOM、漏洞掃描結果與 image digest 作為可重建 manifest。
- 不使用 `latest` 作正式部署依據；Compose 與 release 文件使用不可變版本／digest。
- 因官方沒有完整動態使用者管理 API，接受設定更新時受控重啟 sing-box，既有連線中斷並由客戶端重連。
- 一般核發、核准、輪替與非安全設定變更：第一個事件觸發 Bot 向所有有效且可送達使用者私訊「系統即將重啟! 請暫時切換至別的節點」，等待 30 秒並合併期間變更；計畫重啟每 60 秒最多一次。
- 「別的節點」指其他提供商節點，本系統不承諾 A/B 備援。
- 離群、永久封鎖、達配額、閒置淘汰等安全撤銷立即更新核心，不等待 30 秒；同批大量撤銷只重啟一次。
- 設定檢查或重啟失敗必須保留最後可用設定、標記重大告警並通知所有擁有者與管理員。
- 核心 worker 無待處理動作時每 1 秒輪詢一次 outbox；安全撤銷應優先於一般變更處理。
- 核心快照、設定檢查、提升或重啟失敗時，依該 outbox action 的 attempts 採 `5 秒、15 秒、1 分鐘、5 分鐘、15 分鐘` 指數式退避，之後固定每 15 分鐘重試；每次失敗都以封閉錯誤分類通知所有擁有者與管理員，不得包含秘密或底層錯誤文字。
- 一般計畫重啟的預告採盡力送達；個別使用者無法私訊不得阻止 30 秒後重啟，須記錄送達失敗數並供管理者查核。
- 30 秒計畫重啟等待期間若出現安全撤銷，立即生成最新完整快照並只重啟一次；該快照同時套用已等待的一般變更，成功後兩類 outbox 一併完成。

## 9. 出站網路

- 預設硬性 IPv6-only 出站：阻擋 IPv4 literal／IPv4 目的；網域解析只允許 AAAA 路徑。
- Web 提供「允許 IPv4 出站」開關，預設關閉，變更立即套用。
- 設定與測試必須證明關閉時不會透過 DNS fallback 或其他路由洩漏 IPv4。

## 10. TLS、ACME 與免費網域

- Hysteria2、TUIC、AnyTLS 共用同一份與連線名稱匹配的受信任 TLS 憑證。
- 支援 sslip.io 零帳號模式、DuckDNS 免費固定網域與自有網域進階模式。
- sslip.io 使用 HTTP-01；DuckDNS 使用 DNS-01，token 加密保存；自有網域依能力選 HTTP-01／DNS-01。
- 系統自動申請、續期憑證；續期成功後走一般 30 秒合併重啟。
- 初次允許 ACME contact 無 Email，安裝前顯示候選 CA 條款並取得明確同意，再自動嘗試多個可配置 CA。
- 所有無 Email 申請都失敗時，VPN 四協定全部停用；Bot 與管理面板維持可用並顯示重大告警。
- 擁有者可在 Web 填入 ACME Email 後重試；成功前不得輸出任何 VPN 節點或以自簽憑證冒充成功。
- Web 面板本身的公開 HTTPS 由外部反向代理或 Tunnel 負責，不與 VPN ACME 憑證生命週期混用。

## 11. REALITY 目標搜尋

- 安裝時下載具版本與校驗值的公開熱門網域資料集。
- 最多抽樣 200 個網域，只做 DNS、TCP 443、TLS 1.3、憑證名稱與延遲檢查；並行上限 5，總時間上限 60 秒。
- 禁止掃描 IP 範圍或 TCP 443 以外的 port。
- 合格候選排序後必須由部署者確認，全部失敗時要求手動輸入。
- 部署後定期健康檢查；故障時只告警並讓擁有者確認切換，不自動改參數。

## 12. Web 管理登入與角色

- 不採 Telegram Login Widget 或 OIDC。
- 固定根擁有者由環境變數 `OWNER_TG_ID` 指定；不得在 Web 刪除或降級。
- 根擁有者可新增其他擁有者與一般管理員；一般管理員不能管理角色。
- 擁有者可管理 Bot Token、管理員、VPN／網路／網域、全域規則與全部使用者。
- 一般管理員可查詢、核准、拒絕、撤銷、輪替、處理配額及查看稽核，但不可修改系統秘密與基礎網路設定。
- 管理角色只能在 Bot 私聊使用 `/adminlogin`，群組中一律拒絕。原規劃 `/admin-login` 因 Telegram Bot 指令不允許連字號，經使用者確認改為 `/adminlogin`。
- `/adminlogin` 依 Telegram ID 採滑動視窗限速，每分鐘最多產生 3 次登入碼；新碼仍立即使同一管理者的舊碼失效。超限沿用通用失敗訊息，不揭露授權狀態或限速細節。
- 指令產生密碼學安全的大小寫英數 8 碼；一次性、5 分鐘到期、只存雜湊、每位管理者同時只保留一碼，新碼使舊碼失效。
- Bot 私聊只回傳 8 位碼，不回傳面板 URL；管理者在面板登入頁手動輸入。
- 登入頁提交管理者 Telegram ID 與 8 位碼；錯誤回應不得揭露帳號是否存在。Telegram ID 讓登入碼消耗前可執行帳號層級限速。
- 登入碼驗證限速採滑動視窗：每帳號 5 次／15 分鐘、每來源 IP 20 次／15 分鐘、全域 100 次／分鐘。超限統一回 429 與不揭露命中層級的 `Retry-After`；成功登入清除該帳號與來源 IP 的失敗計數，全域桶只隨時間滑動。
- 來源 IP 預設只採 TCP `RemoteAddr`。新增 `TRUSTED_PROXY_CIDRS`；只有直連來源命中可信 CIDR 時才優先解析標準 `Forwarded` 的 `for=`，其次解析 `X-Forwarded-For` 最左有效 IP。格式異常時忽略轉送值並回退直連 IP，禁止無條件信任轉送 header。
- 登入成功或失敗都留下不含原碼的安全稽核。
- Web session 使用 HttpOnly、Secure、SameSite cookie 與 CSRF；閒置 12 小時到期，絕對期限 7 天；支援主動登出與擁有者撤銷全部 session。
- 待審、核心失敗、憑證失敗、資格查核異常與安全告警通知所有擁有者與管理員；非安全通知可個別關閉，重大安全告警不可關閉。

## 13. Web 面板

- 全繁體中文、桌面與手機響應式。
- 至少包含：儀表板、使用者與流量、待審、資格群組、VPN／REALITY、Bot 與管理員、TLS／網域、備份、稽核紀錄。
- 危險操作需明確確認；秘密預設遮罩，修改時不可回傳原明文。
- 除 `APP_MASTER_KEY`、`OWNER_TG_ID` 與首次 bootstrap 必要值外，設定可在 Web 修改並稽核。
- 不得移除最後一位可用擁有者；環境根擁有者永遠保留。
- 預設後端監聽 `WEB_IP=0.0.0.0`、`PORT=35699`。
- `WEB_IP`、`PORT` 只表示應用 HTTP 監聽；正式環境必須由外部反代／Tunnel 提供受信任 HTTPS，並以防火牆限制 35699 的來源。
- Bootstrap 必填 `WEB_PUBLIC_URL` 作為面板與私人訂閱的公開 HTTPS base URL；可含 path prefix，但不得含 userinfo、query 或 fragment，尾端斜線正規化後在其下產生 `/sub/{token}`。
- VLESS REALITY 優先占用 TCP 443；同一 IP 的面板反代不可再占同一 TCP 443。文件提供 Nginx、Caddy、Cloudflare Tunnel，以及第二 IP／自訂 HTTPS port 的拓撲說明，Compose 不內建反代服務。

## 14. 秘密與安全

- `APP_MASTER_KEY` 永遠只從環境取得，不可寫入資料庫或透過 Web 修改。
- 首次 `BOT_TOKEN`、`OWNER_TG_ID`、Web 監聽值可由環境 bootstrap；Bot Token 後續可由擁有者在 Web 修改並安全熱切換。
- Bot Token、DuckDNS token、VPN 憑證、訂閱 token 及其他秘密以用途隔離的 AEAD 金鑰加密保存。
- `.env.example` 只放假值與產生指令，不得提交真實秘密。
- API 需具輸入驗證、RBAC、CSRF、登入與 Bot 指令限速、安全 header、錯誤遮蔽及稽核。
- 不得允許群組訊息、日誌、錯誤追蹤、備份 metadata 或 CI artifact 洩漏完整秘密。

## 15. 備份與復原

- 內建每日 PostgreSQL `pg_dump` 排程，預設保留 7 日且可由 Web 調整。
- 第一版目的地支援本機掛載目錄。
- 備份使用從 `APP_MASTER_KEY` 經用途隔離 KDF 衍生的金鑰加密，並記錄完整性資訊。
- 提供備份、還原、驗證腳本與文件；還原流程必須先驗證完整性並防止覆蓋錯誤環境。

## 16. 安裝與部署

- 提供完整安裝精靈，檢查 Linux、Docker Compose、CPU 架構、DNS、IPv4／IPv6、必要 TCP／UDP ports、Telegram Bot 管理員權限、Web HTTPS 拓撲、ACME 條款與網域。
- 自動偵測值必須顯示並由部署者確認後才生成設定與啟動。
- Compose 包含應用、PostgreSQL、自建 sing-box 核心及必要維護工作，不含公開反向代理。
- Compose 使用受限核心控制 sidecar 執行 sing-box 設定檢查與重啟；只有該 sidecar 可接觸 Docker Engine，且只能控制設定中固定的 sing-box container。對外 Web 應用不得掛載或直接存取 Docker socket，只能透過共享、檔案權限受限的 Unix socket 發送封閉的 check／restart 命令。
- 提供部署、升級、回滾、備份、復原、Nginx、Caddy、Cloudflare Tunnel 與故障排除文件。
- 本機沒有 Docker，因此真實 Compose 執行由 GitHub Actions 與部署後驗收腳本提供證據；本機仍需執行可用的單元、整合替身、lint、型別與 build 驗證。

## 17. GitHub、CI 與供應鏈

- 建立公開 `s12ryt/s12ryt-vpn-bot` repository、初始化 Git 並推送；使用者已授權。
- CI 執行 Go tests／race／lint／build、React tests／lint／typecheck／build、PostgreSQL integration、Docker build、Compose/config smoke。
- CI 建立 1,000 名使用者、四協定、雙棧設定並通過固定 release 所解析版本的 `sing-box check`。
- CI 模擬 600 名使用者並行統計匯入，驗證配額一致性、資料競爭與有界批次時間；實機 VPN 吞吐與 600 真實連線列入部署後驗收。
- release 建置 amd64／arm64 應用與 sing-box 映像，推送公開 GHCR，產生 SBOM、漏洞掃描報告、來源 provenance 與 digest。
- GitHub hosted runner 不使用正式 Telegram、網域、DuckDNS 或 ACME secrets；外部整合用契約 fake／測試容器。
- 提供部署後真實驗收腳本，驗證 Telegram 權限、DNS、ACME、四協定、雙棧、IPv6-only 出站、流量採集、配額封鎖、重啟與訂閱。

## 18. 完成條件

- 每項領域行為均有 RED 失敗證據與 GREEN 通過證據，或明列合理測試例外。
- 相關 Go／React 測試、race、lint、typecheck、build、PostgreSQL integration、Docker／Compose smoke 與 sing-box config check 全部通過，或如實標示環境阻礙。
- GitHub Actions 在公開 repository 通過；GHCR release artifact 具有 SBOM、掃描結果與不可變 digest。
- 不得以 fake CI 宣稱真實 Telegram、ACME 或 VPN 網路端到端已通過；必須另外執行並記錄部署後驗收。
- 沒有未處理的重大或高風險正確性、安全性、資料一致性或秘密洩漏問題。
