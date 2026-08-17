# 操作與驗證紀錄

## 2026-08-18：正式 release 補齊備份映像

- 懷疑式對照 Compose 與 release workflow 發現部署必填 `BACKUP_IMAGE`，但正式 release 只發布 app、sing-box 與 controller；即使上游漏洞解除，部署者仍拿不到受同等供應鏈保護的備份服務映像。
- RED 擴充 release 與 README 契約，要求 `s12ryt-vpn-backup`、本地 amd64 build、發布前 Trivy、amd64／arm64、SBOM、provenance、digest metadata 與首次可見度提醒；舊 workflow 精確失敗於整段缺失。
- GREEN 新增 backup image 的 local build／scan／multi-arch push，將 `backup_digest` 寫入 immutable source metadata；README 同步列出第四個 GHCR package。直接 deploy tests／vet、全量 Go tests／vet／Windows與Linux builds、前端 Vitest 14/14／lint／build全綠；GitHub Docker/Trivy 行為待 push 後 CI 與未來上游修復後的 release run 補證。
- 三個原子 commits 推送後，GitHub CI run `32076801477` 全綠：Go race／vet／ShellCheck／Linux build、PostgreSQL 17 integration、Web、Compose 與 app／controller／backup image builds 均通過。正式 workflow 的 backup scan／multi-arch push 仍位於上游 sing-box 嚴格 Trivy 閘門之後，只能在上游 fixed stable 發布後由 weekly run 實證，不以移動或放寬第一道閘門換取假綠燈。

## 2026-08-18：安裝器 Bot／ACME／Web HTTPS 預檢

- 懷疑式對照需求發現兩個缺口：公開位址提示顯示候選為 default，但 Enter 實際停用該 family；重跑既有 `.env` 又收集不會寫回的值。先以部署契約建立 RED，再讓 Enter 採用候選、`-` 明確停用，既有 `.env` 改為驗證保存值並在 Compose 前再次確認。
- 使用者確認安裝器只驗 Bot 身分，資格群組管理員權限留給 Web 啟用規則時逐條強制驗證；ACME 預檢只保存 mode／domain／challenge／條款等非秘密參考值，不收集 DuckDNS token、不冒充簽發；Web HTTPS 必選第二 IP、自訂 port 或 Cloudflare Tunnel 並驗基本一致性。需求已同步 `agent/question.md`。
- GREEN 新增 Telegram `getMe` 驗證、ACME 網域／DNS／候選 CA 條款與 challenge 前置檢查，以及 Web HTTPS 拓撲驗證。Bot token 只寫入 0700 安裝暫存目錄中的 0600 curl config，exit／HUP／INT／TERM 均清理；bootstrap Bot token、owner ID、公開 URL 與不可變 image 先採白名單驗證再做網路呼叫。
- 品質審查確認正式 TLS schema／Web 只支援 DuckDNS provider token；custom DNS-01 無 provider 名稱／憑證持久化路徑。新增 RED 後將第一版自有網域固定為 HTTP-01，既有 `.env` 宣告 custom DNS-01 會 fail closed，文件明確說明能力邊界。
- 首次 GitHub CI run `32074686153` 的 ShellCheck 以 SC2015 抓出 port 驗證使用 `A && B || fail` 的模糊控制流；新增直接契約 RED，改為明確 `if` 後，run `32074992110` 全綠：Go race／vet／ShellCheck／Linux build、PostgreSQL 17 integration、Web、Compose 與 app／controller／backup images 均通過。

## 2026-08-18：安裝精靈公開位址確認閘門

- 懷疑式對照 `agent/question.md` 發現既有 `install.sh` 每個 address family 只查一個外部服務，僅列印結果且未要求部署者確認；先新增 deploy contract RED，精確證明缺少多來源、人工修改／確認及 Compose 前順序閘門。
- GREEN 將 IPv4／IPv6 各自向 api.ipify.org、ifconfig.co 與 icanhazip.com 查詢，嚴格驗證回應；至少兩個來源一致才提供可信候選。部署者逐一輸入最終值，可停用單一 family，但不可同時停用；未明確確認時在 `docker compose config`／pull／up 前 fail closed。
- 新建 `.env` 保存 `PUBLIC_IPV4`／`PUBLIC_IPV6` 作 Web「VPN 與網路」設定核對資料，不自動繞過完整核心、TLS／REALITY 驗證。品質複查同時移除 install.sh 遺留、已無權威性的 `BACKUP_RETENTION_DAYS=7`。
- 驗證：deploy RED→GREEN、`go test ./...`、`go vet ./...`、Windows server／backup／restore build、`bash -n`、前端 Vitest 14/14、ESLint與Vite build全綠；ShellCheck與真實 Linux互動流程由GitHub CI補證。
- 首次 push 的 CI run `32068751759` 讓本機不可用的 ShellCheck 找到 `local -n` nameref 賦值被判定為 SC2034。保留行為並改用 Bash `printf -v` 動態賦值後，GitHub CI run `32069222987` 全綠：race、vet、ShellCheck、Linux build、PostgreSQL 17 integration、Web、Compose及 app／controller／backup images 均通過。

## 2026-08-18：部署後驗收 fail-closed 證據閘門

- 懷疑式複查發現 `scripts/post-deploy-check.sh` 完成容器、Telegram、TLS、訂閱與設定檢查後，只列印「需人工續驗」卻仍以 0 結束；CI 或操作員可能把局部檢查誤判為完整驗收通過。
- 先新增 deploy contract RED，要求 `VERIFY_EXTERNAL_EVIDENCE_FILE` 及四協定雙棧、IPv6-only／IPv4 出站、流量入帳、配額封鎖、週期恢復、重啟與 600 連線八類證據，並禁止舊成功提示。GREEN 改為驗證 schema v1 JSON；每項必須 `passed: true` 且有非空 evidence 引用，缺檔、symlink、格式錯誤或缺項一律非零退出。
- 品質審查抓到初版 jq `all` 內 scope 指向檢查名稱而非根物件；改先捕捉 `$root`。以真實合法與缺項 JSON 執行 jq，分別驗證成功與拒絕。此閘門不代替外部測試，實際 Linux／Docker／Telegram／ACME 與 E2E evidence 仍屬外部阻塞。
- 再次懷疑式檢查發現非空 evidence 字串仍可虛構。新增 RED 後，GREEN 要求每個引用是 manifest 目錄內的安全相對路徑；以 `realpath -e` 驗證實體檔與 canonical containment，拒絕絕對路徑、`.`／`..`、重複斜線、非一般檔、最終 symlink 與父層 symlink 逃逸。

## 2026-08-18：REALITY 目前目標健康監控

- 需求澄清確認每 1 小時檢查一次，只在健康狀態轉換時通知：首次故障一次、持續故障不重複、恢復一次；未設定核心安全略過，擁有者確認後才可切換，系統永不自動修改目標。已同步 `agent/question.md`。
- `internal/reality.HealthMonitor` 沿用既有 DNS／TCP443／TLS1.3／憑證名稱／延遲 prober；啟動立即檢查、之後每小時，單輪錯誤不殺服務。RED/GREEN 覆蓋未設定、故障／持續故障／恢復、可取消排程與缺依賴。
- migration 013 建立 `reality_health` singleton，保存 target、healthy、last checked/transition/notification 與 pending transition。`RealityHealthStore` 交易鎖定後回傳故障／恢復；通知與稽核成功才確認 pending，避免狀態已提交後程序中斷造成通知永久遺失。新目標健康建立無通知基準，新目標首次失敗仍通知。
- `TelegramHealthNotifier` 對所有 active 管理者逐一傳送固定訊息；故障訊息明確要求在管理面板確認後再切換並說明不會自動切換。單一私訊失敗不重送給已成功者，只以 attempted/failed 稽核；收件人查詢或稽核失敗則保留 pending 等下輪重試。
- 正式 server 以 `RealityHealthStore`、既有 TLS prober、AuthStore、swap-aware Bot client 與 AuditStore 組裝必要 goroutine。全量 `go test ./...`、`go vet ./...`、Windows server build、integration tagged compile 與前端 Vitest 14/14／lint／build通過；真實 migration 013/race 待 GitHub Linux CI。
- 8 個原子 commits 推送至 main 後，GitHub CI run `32063957698` 全綠：Go race／vet／ShellCheck／Linux build、PostgreSQL 17 migration 013 與冪等／600-user integration、Web、Compose、app/controller/backup images 均通過。

## 2026-08-18：外部阻塞重新驗證

- GitHub 官方 Releases API 再次確認最新 stable 仍為 `v1.13.19`（published `2026-08-17T09:47:06Z`，commit `b5ebaa1fc0f2b94256180b95468e73ef53caa27d`）；沒有可供正式 release 使用的新 stable。
- 直接讀取官方 `v1.13.19` `go.mod`，受 Trivy 阻擋的版本仍為 `golang.org/x/crypto v0.48.0`、`golang.org/x/net v0.50.0`、`golang.org/x/text v0.34.0`、`google.golang.org/grpc v1.79.1`。因此維持嚴格 HIGH／CRITICAL gate，不採 prerelease、不忽略 CVE、不私改上游 dependency graph。
- 本機重新執行環境檢查：`docker`／`docker compose` 指令仍不存在；`BOT_TOKEN`、`APP_MASTER_KEY`、`DATABASE_URL`、`WEB_PUBLIC_URL` 均未提供。故真實 Linux／Docker／Telegram／ACME 部署後驗收仍無可執行環境，不得虛報完成。
- release weekly workflow 與部署後驗收腳本已就緒；這次檢查沒有出現可安全推進正式發佈或真實部署驗收的新條件。
- 文件懷疑式複查發現 README 的「尚未完成」章節只列 release，沒有明列真實部署驗收。先新增文件契約 RED，再補上實際 Linux／Docker／Telegram／ACME 條件與 `scripts/post-deploy-check.sh` 入口；契約同時禁止恢復已完成的 ACME／備份／設定頁舊敘述。

## 2026-08-17：1,000 使用者真實核心檢查與發佈閘門實證

- release run `32039261580` 首次走到真實 `sing-box check` 時，mode 0600 的測試私鑰由 runner 擁有，而 distroless 預設 nonroot UID 不同，故讀取 `/run/tls/privkey.pem` 被拒。先以契約測試建立 RED，再維持私鑰 0600，讓隔離 check container 使用 `--user "$(id -u):$(id -g)"`；沒有放寬檔案權限，network-none／read-only／no-new-privileges／cap-drop ALL 仍保留。
- commit `530aaf0` 推送後，一般 CI run `32040218650` 初次因 GitHub codeload 對 pinned `setup-go` 回 429／502／503 而在專案步驟前失敗；只重跑失敗 jobs 後全綠：Go race／vet／ShellCheck／Linux build、PostgreSQL 17 migration 012 與 integration、Web、Compose 及 app/controller/backup images。
- Release images run `32040495443` 的前兩個 attempts 同樣停在 GitHub action codeload；平台恢復後 attempt 3 完整執行。來源解析固定為 sing-box v1.13.19；purego/glibc/libcronet runtime pre-build 成功，接著 1,000 使用者×四協定×雙棧的 8 inbound 設定通過真實 `sing-box check -c`。
- Trivy 隨即對本地、尚未發布的 image 報告 Debian 12.15 OS packages 0 漏洞、sing-box Go binary 17 項（HIGH 16、CRITICAL 1），包含 x/crypto v0.48.0、x/net v0.50.0、x/text v0.34.0 與 grpc v1.79.1 的已知 fixed-version 漏洞。掃描以 exit 1 正確阻擋，所有 sing-box/app/controller push、digest 與 release metadata 步驟均 skipped。
- 發佈後獨立查核：`gh api /user/packages/container/s12ryt-sing-box/versions` 回 HTTP 404 Package not found，證明本輪沒有重建或誤發 package。正式 release 唯一剩餘條件仍是上游 stable 修復 dependency graph；維持 weekly 自動重試、嚴格 Trivy gate、不忽略漏洞、不私改上游依賴。

## 2026-08-17：Web 備份保留政策與 release transient failure

- 懷疑式重讀 `agent/question.md` 發現第 15 節「預設保留 7 日且可由 Web 調整」尚未落地：備份程序只在啟動時讀 `BACKUP_RETENTION_DAYS`，Web 也沒有備份頁。先以 domain／PostgreSQL／HTTP／server／Web／部署契約建立 RED，確認缺少 singleton 設定、owner API、啟動必填依賴與前端入口。
- migration 012 新增 `backup_settings` singleton（1..3650 日、預設 7）；`BackupSettingsStore` 提供防禦性讀取與交易式 owner 更新，成功寫 `backup.settings.update` actor 稽核。owner API 為 `GET/PUT /api/settings/backup`，PUT 要求 double-submit CSRF；Web「備份與保留」頁只允許 owner 操作。
- `cmd/backup` 每次 24 小時循環都重讀 PostgreSQL 最新保留期；設定不可用或資料非法時仍建立當次加密 archive，但 retention=0 跳過 prune，避免依過時／推測政策刪除檔案。Compose 與 `.env.example` 移除舊 `BACKUP_RETENTION_DAYS` 權威入口。
- Compose 複查發現 backup service 原在 bridge network，但共用的 `DATABASE_URL` 指向 loopback-only PostgreSQL publication，container 內 `127.0.0.1` 會指向自身。新增 RED 契約後讓 backup 使用 host network，與 app 的資料庫拓撲一致。
- release run `32036312881` 未走到 purego runtime／1,000-user check；在 `Resolve latest stable sing-box source` 遭 GitHub Releases API HTTP 504 提前失敗。release workflow 新增 workflow token Authorization、curl API／tarball 5 次 all-errors retry，以及 `git ls-remote` 5 次退避；漏洞掃描與 scan-before-push 閘門未變。

## 2026-08-17：規模驗收契約補齊

- 懷疑式重讀 `agent/question.md` 發現既定第 17 節的規模驗收從未真正建立：CI 應生成 1,000 使用者×四協定×雙棧設定並執行固定 release sing-box `check -c`；另需 600 使用者並行流量匯入、交易一致性、race 與 bounded time 證據。
- `internal/singbox/generator_scale_test.go` 驗證 1,000 使用者在 8 個 inbound 中各完整存在、stats users 1,000 筆、設定 deterministic、30 秒內完成且不洩漏 subscription token。`cmd/scaleconfig` 提供 release 專用 deterministic config；第一輪 GREEN 正確抓出非 canonical REALITY private key，改用明確 32-byte Raw Base64URL 編碼而未放寬 generator。
- release workflow 在本地 `scan-local:singbox` image 建成後、Trivy 與任何 push 前，生成一次性測試 TLS 憑證與 1,000-user config，以 read-only/network-none/cap-drop container 執行真實 `sing-box check -c`。順序刻意放在已知上游漏洞的 Trivy 前，讓上游未修期間仍可取得 schema check 證據。
- `internal/trafficstats/collector_scale_test.go` 以 600 位使用者的四協定／雙棧聚合計數，對同一 immutable Collector 執行 16 路並行採集；一般 Go CI 的 `-race` 驗證共享路徑無資料競爭。
- `integration/traffic_scale_test.go` 在 PostgreSQL 17 建立 600 位 active users，以 12 個 goroutine、每批 50 人呼叫 `RecordPendingBatch`；驗證 600 筆完整提交、每人 used_bytes=24、活動時間更新、無錯誤核心 transition、重播 batch 不重複計費及 60 秒 bounded time。CI integration job 改為 `go test -race -tags=integration ./integration`。
- 本機證據：規模套件 tests、全量 `go test ./...`、`go vet ./...`、Windows server build、Linux amd64 server/controller build、前端 Vitest 13/13、ESLint、TypeScript/Vite build全綠。本機無 PostgreSQL/Docker，真實 integration 與 sing-box check 待 GitHub Linux workflow 執行，不提前宣稱通過。

## 2026-08-17：Bot Token 熱切換

- `telegram.SwapAwareClient`：mutex 保護的原子委派 client，實作所有既有窄介面（UpdateClient/GetMe/GetChatMember/SendMessage/SendPhoto/SendApprovalRequest/AnswerCallbackQuery）；`Swap` 以可注入 verifier（正式傳真 `GetMe`）驗證候選 token 屬同一 Bot（同 ID、IsBot、非空 username），失敗絕不替換；`ErrBotIdentityChanged`／`ErrBotVerificationFailed` 為封閉 sentinel。
- migration 011 `bot_settings`：token nonce/ciphertext 配對 CHECK；`BotSettingsStore.Save` 以 `telegram/bot-token` 用途 AEAD 密文＋`bot.token.update` 稽核（無明文）；`Token` 解密（未設定回 `ErrBotTokenNotConfigured`）；`Overview` 只讀 username/updated_at。
- httpapi `GET/PUT /api/settings/bot`：owner-only（`PermissionManageSecrets`）＋CSRF＋strict JSON；錯誤映射封閉代碼（`bot_identity_changed`／`bot_verification_failed`／`bot_settings_operation_failed`）；初次 GREEN 曾把路由誤掛在 coreSettings guard 內（同 TLS 教訓），已改獨立 guard。
- `cmd/server.botTokenManager`：順序固定為 驗證→持久化→Swap（持久化失敗不換 live client，避免重啟回退到已撤銷 token）；main 以 `bot-token-encryption` HKDF 金鑰建 store、DB token 優先於環境 bootstrap、啟動 `getMe` 身分傳入 wrapper、下游（recheck lookup/rule manager/approval/notifier/buildApplication/traffic notifier）全部改用 wrapper，避免殘留舊 token 引用。
- Web「Bot 與 Token」頁：username／上次輪替、write-only token、二次確認、固定錯誤訊息；測試需 stub `window.confirm`。Vitest 12/12、ESLint、Vite build、全量 Go tests/vet、Windows/Linux 交叉 build 全綠。

## 2026-08-17：TLS 未核發閘門與設定頁

- 契約「成功前不得輸出任何 VPN 節點」落地：`CredentialStore.ListActive` 再 CROSS JOIN `tls_settings`，要求 `state='issued' AND certificate_expires_at > now()`；`subscription.Service.WithTLSReadiness` 在 Render 前檢查，未核發回 `ErrTLSNotReady`（HTTP 404），readiness 錯誤原樣傳播。`TLSSettingsStore.Issued/TLSIssued` 輕量查詢不含密文欄位。
- owner TLS API：`GET/PUT /api/settings/tls`，權限 `PermissionManageVPNSettings`；PUT double-submit CSRF＋strict JSON（`duckdns_token` write-only），回應只含 `has_duckdns_token` 布林。初次 GREEN 曾把註冊誤綁在 coreSettings guard 內導致 404，改為獨立 guard。
- `buildApplication` composite 擴為必含 `TLSSettingsManager`（RED 證明缺件可啟動）；main 將 `tlsSettingsStore` 提前建立並同時供 subscription readiness、TLS runtime 與 handler 使用。
- Web「TLS 與網域」頁：模式切換自動帶出對應 challenge（duckdns→dns_01、sslip_io→http_01）、網域／Email／CA 清單、DuckDNS token 寫入式欄位（留空保留）、條款勾選、憑證狀態（未核發／已核發／簽發失敗）與到期顯示；未核發時明確提示不輸出節點。Vitest 10/10、ESLint、Vite build 全綠。
- 測試修正三處（均為測試問題非正式碼）：密碼欄位需 `getByLabelText`、狀態文字為完整句子、`user.type` 為附加需先 clear。

## 2026-08-17：TLS 簽發鏈

- `TLSSettingsStore`：Save 以交易鎖列、`acme.ValidateSettings` 前置驗證、DuckDNS token 以 `acme/duckdns-token` AEAD 密文落地（空值保留既有）、寫 `tls.settings.update` 稽核（不含 token／Email）；GetOverview 只以 `duckdns_token_nonce IS NOT NULL` 衍生 token 存在性，絕不 SELECT 密文；LoadForIssuance 解密 token 並回傳憑證到期時間，未設定回 `acme.ErrNotConfigured`。
- `RecordIssuance` 交易內更新 state/expiry/CA、清失敗欄位、排 active users reconcile 與稽核；`RecordFailure` 只接受封閉 reason（目前 `all_cas_failed`），已簽發且未過期者不受續期失敗影響。migration 010 建立 singleton `tls_settings` 與 mode／challenge／token 配對／簽發完整性 CHECK。
- `acme.FileInstaller`：clean absolute 路徑、憑證 0644／私鑰 0600、同目錄暫存＋fsync＋rename，不留暫存檔；非 PEM 或空材料拒絕且不落半套檔案。
- `tlsrunner.Coordinator`：未設定略過、憑證有效期內（到期前 30 天 margin 前）不重簽、簽發成功記錄、全部 CA 失敗記錄封閉原因並回傳錯誤；`Run` 每小時檢查、context 取消正常退出、單輪失敗不中止。測試證明 X.509 秒級時間截斷，RecordIssuance 以解析後葉憑證 NotAfter 為準。
- `cmd/server` TLS runtime：`reloadingCertificateInstaller` 每次安裝重讀 core settings 的憑證路徑（owner 改路徑免重啟）；`buildTLSRuntime` 缺依賴拒絕；正式 main 以 lego issuer＋TLSSettingsStore 組裝第七個必要 goroutine。
- 全量驗證：`go test ./...`、`go vet ./...`、Windows／Linux amd64 server 與 controller build 全綠；以 6 個原子 commits 推送。

## 2026-08-17：安裝腳本與 ACME 服務

- VPN 核心設定切片以 7 個原子 commits 推送（`a6e7006`..`e65f039`），GitHub CI run `31980626310` 全綠。
- 安裝／驗收腳本 TDD：RED 鎖定 Linux／x86_64／aarch64、Docker Compose、必要環境、埠位（含 ACME HTTP-01 的 TCP 80）、`docker compose config --quiet` 先驗、不得輸出秘密；部署後腳本鎖定 getMe／getChatMember（以 getMe 剖析 Bot ID，而非根擁有者）、`/health/live|ready`、openssl s_client 憑證鏈、Base64 訂閱四協定＋IPv6、sing-box check、核心控制 socket 與 traffic spool 掛載，並明列需人工／負載的未完整驗證項。
- 品質修正：`.env` 權限改 octal bitmask（禁止任何 group/other bits）；jq 驗證 Bot 為 administrator/creator；腳本禁用 `set -x`。
- ACME 服務 TDD：`internal/acme.Service` 以可替換 Issuer／Installer 鎖定條款同意、模式（sslip.io=HTTP-01+`.sslip.io` 尾碼、DuckDNS=DNS-01+token 必填、custom=挑戰類型一致）、CA URL 白名單（1–5 個、去重、HTTPS、無 userinfo/query）、憑證 X509 金鑰對匹配／網域名稱／有效期驗證；多 CA 依序備援、驗證失敗或簽發失敗都不安裝。
- lego v5.3.1 adapter：`registration.User` 需 `crypto.Signer` 與 `*acme.ExtendedAccount`（以 `go doc` 查證修正，非猜測 API）；HTTP-01 provider 綁 `:80`；DNS provider 白名單僅 `duckdns`；單元測試僅覆蓋離線驗證與 provider 映射，真實網路簽發留部署後驗收。`go mod tidy` 後全量 Go tests／vet／Windows build、bash -n、前端 9/9／lint／build 全綠。

## 2026-08-17：核心設定、稽核、角色與全域政策

- VPN 核心設定 TDD：owner-only API／Web 可管理明確 IPv4/IPv6、四協定 ports、TLS 路徑與網域、REALITY 目標／short ID、Stats endpoint及IPv4出站。REALITY 私鑰為 write-only；空值保留既有 AEAD 密文，新值先加密，資料庫與回應不含明文。
- 核心設定更新在交易前後都以同一 sing-box generator 驗證；generator 新增 canonical 32-byte Raw Base64URL X25519 private key驗證。成功交易會寫不含秘密的 audit 並排 active users reconcile outbox。前端 RED 後 Vitest 9/9、ESLint、TypeScript/Vite build及全量 Go tests/vet均通過。
- 稽核 TDD 完成倒序 stable cursor PostgreSQL 讀模型、RBAC HTTP API與響應式唯讀工作區；details 僅接受 JSON object，查詢不碰憑證表。
- 角色管理以 PostgreSQL transaction advisory lock 序列化；根擁有者與最後擁有者保護在交易內重查，移除時同步撤銷登入碼與全部 Session。
- 全域政策 RED 證明單純設定 CRUD 不足；GREEN 將資格模式、重查參數、共享配額與閒置門檻更新放在單一交易，配額／閒置狀態轉換同時寫 durable core outbox 與 audit，commit 後觸發資格重查。
- Web owner-only「資格與配額」頁以受控表單修改政策；閒置由停用改為啟用或降低門檻時，先查受影響人數並二次確認。Vitest 7/7、ESLint、TypeScript/Vite build通過。
- 資格規則 CRUD TDD：owner 透過 Web 輸入 chat ID/type/title；後端先以 Bot identity 執行 `getChatMember`，只有 administrator/creator 才在交易中啟用並寫 actor audit。停用同樣交易稽核；兩者提交後觸發全量補償重查。前端 Vitest 增至 8/8。
- 正式 release 第二輪已成功建置並推送 amd64/arm64 sing-box，但 Trivy 正確阻擋官方 stable v1.13.18 的 16 HIGH/1 CRITICAL Go 依賴漏洞（含 grpc-go CVE-2026-33186）。依契約不忽略漏洞、不修改官方 stable dependency graph，等待上游 stable 修復。
- 公開 GitHub repository 已建立並推送；GitHub Actions 已實際通過 Linux race、PostgreSQL migration integration、Compose config、app/controller container build及Web驗證。
- Release workflow TDD：要求排除 prerelease/draft、固化 sing-box tag／peeled source commit／source SHA-256，Dockerfile只在官方 default tags後加入 `with_v2ray_api`；三個 GHCR images皆建 amd64/arm64並啟用BuildKit SBOM/provenance，固定版Trivy對發布映像作HIGH/CRITICAL gate，不發布mutable latest。
- 首次 release 暴露官方 Linux naive outbound 需要 `with_purego` 且下游必須沿用 `release/LDFLAGS`；新增 RED 後修正 Dockerfile 與 metadata。第二次 multi-arch run `31977587653` 正在以 QEMU 編譯，未重複觸發。
- 備份 TDD：RED 鎖定用途隔離、篡改／錯誤金鑰拒絕、0600 原子檔、資料庫 URL 不進 argv、每日排程及非 root Compose service；GREEN 完成 AES-256-GCM archive、`pg_dump`／`pg_restore` 命令、預設7日保留與還原 runbook。完整 Go tests/vet及前端7項測試/lint/build通過。

## 2026-08-17：Web 使用者工作區

- RED：登入成功後原頁面仍停留在登入畫面；新增測試要求取得 CSRF、載入 `/api/users?limit=50`、顯示共享流量及送出核准動作。
- GREEN：React 工作區接上使用者讀模型與核准／拒絕／撤銷／輪替 API；所有 mutation 帶 double-submit CSRF，危險操作要求確認，API 錯誤只顯示固定繁中訊息。
- 第二輪 RED／GREEN：以可讀 CSRF cookie 還原既有 Session，先驗 `/api/auth/me` 再載入資料；登出撤銷 server Session 並回到登入畫面，不使用 localStorage 保存秘密。
- Vitest 4/4、ESLint、TypeScript/Vite build 通過。Playwright 實測 375px 與桌面無水平溢出；修正手機 grid 導覽被拉高至 254px 的問題，修後導覽 59px；瀏覽器 console 無錯誤。
- SPA 服務 TDD：Go HTTP handler 直接服務 Vite assets，未知前端 route fallback `index.html`；`/api`、`/health`、`/sub` 不被 SPA 吞掉。`WEB_ASSET_DIR` 只接受 clean absolute Unix path，正式 server 已接 `os.DirFS`。
- Compose RED 因部署檔不存在失敗；GREEN 建立 Node/Go 多階段 distroless nonroot 映像、host-network app/sing-box、loopback PostgreSQL與唯一 Docker socket sidecar。第一次 GREEN 測試抓到控制器多餘的 `DOCKER_SOCKET` 環境值造成安全契約計數失敗，移除後 `go test ./deploy`、`go vet ./deploy` 通過。
- `.env.example` RED 證明環境範本缺失且 Compose 不應直接拼接未跳脫 PostgreSQL 密碼；GREEN 改為明確必填 `DATABASE_URL`，範本只含假值、`openssl rand -base64 32` 產生方式與不可變 sing-box digest 提示。瀏覽器驗證 PNG 已刪除，`.playwright-mcp/` 與手機截圖模式加入 `.gitignore`。
- 本機仍無 Docker，因此尚未執行 `docker compose config` 或容器 build；必須由 GitHub Linux runner 補足，不可誤報已驗證。

## 2026-08-16

- 讀取工作區：只有使用者既有的空白 `vpn.md`，沒有程式碼、套件清單、測試或專案規範。
- 確認工作區尚未初始化 Git。
- 確認本機工具：Go 1.26.3、Node.js 24.11.1、npm 11.7.0；本機沒有 Docker。
- 完成需求澄清並建立 `agent/question.md`。
- 配額 TDD 第 1 輪：RED 證明缺少共享配額行為；GREEN 完成跨協定／雙棧聚合、固定 30 日週期與達額封鎖。
- 配額 TDD 第 2 輪：RED 證明缺少輸入維度、溢位與配額調整；GREEN 完成白名單、無突變溢位拒絕及現期立即調額。
- 建立 `internal/config`；測試涵蓋安全 bootstrap 必填值、32-byte Base64 根金鑰、根擁有者與 HTTP 監聽預設／覆寫。
- 建立 `/health/live` 與 `/health/ready`，readiness 不洩漏 PostgreSQL 錯誤細節。
- 建立 `cmd/server`，以 pgxpool 組裝並支援 graceful shutdown；`go test ./...`、`go vet ./...`、`go build ./cmd/server` 通過。
- 本機 `go test -race` 因 `CGO_ENABLED=0` 無法執行；保留 GitHub Linux runner 驗證。`gopls` 未安裝，改以編譯、測試及 vet 驗證。
- 建立 React 19 + TypeScript + Vite 8 前端。登入頁 TDD RED 因 App 不存在失敗，GREEN 後 Vitest 1 項通過；ESLint、TypeScript、Vite build 通過，npm audit 0 項弱點。
- 存取生命週期 TDD：完成首次自助領取、離群立即撤銷後需審、重新批准新週期、三種管理撤銷模式、永久封鎖、憑證輪替保留／重設週期及閒置淘汰後自助重領。
- 多群資格 TDD：完成 any/all 的 eligible／ineligible／indeterminate 決策；暫時 Telegram API 錯誤不改帳號，只有明確不合格會立即撤銷。
- 憑證 TDD：以可注入 `io.Reader` 驗證 UUID v4、32-byte Base64URL token／password、四協定獨立與全量輪替；正式預設 `crypto/rand`，亂數錯誤回零值 bundle。
- 核發協調 TDD：Claim／Approve／Rotate 在憑證產生失敗時不改帳號，成功才推進 generation 與週期。
- 配額／活動 TDD：達額轉換發出立即撤銷，固定週期可在無流量下自動解除；流量與最後活動時間 copy-then-commit，錯誤不突變，零 byte 不算活動。
- 閒置預覽 TDD：僅列出仍 active 且到達 N 日邊界的 Telegram ID，結果排序，`0` 表示停用。
- 領域完成後 `go test ./...`、`go vet ./...` 與前端 Vitest 全數通過。發現 Go 根模組會誤掃 `web/node_modules` 的第三方 Go 範例，新增 `web/go.mod` 作為模組邊界。
- 實作 Telegram 指令前確認 Bot command 不允許連字號；使用者選擇將管理登入指令由 `/admin-login` 改為合法的 `/adminlogin`，已同步需求契約。
- 管理驗證 TDD：完成 owner／administrator 權限矩陣、根擁有者與最後 owner 保護；8 位無偏差 CSPRNG 登入碼只存 HMAC digest、一次性且精確 5 分鐘到期。
- Session TDD：32-byte Base64URL token 只存 HMAC digest；精確 12 小時閒置與 7 日絕對期限；驗證與 touch 由 store 原子執行。Web 登入只接受嚴格 JSON，成功設置 Secure、SameSite=Strict 的 HttpOnly Session cookie 與 CSRF cookie。
- PostgreSQL auth TDD：登入碼以 `DELETE ... USING ... RETURNING` 原子消耗；Session 以單一 `UPDATE ... FROM ... RETURNING` 檢查目前管理者、雙期限並 touch；查無結果正規化，不洩漏資料庫細節。
- Migration TDD：embedded `001_initial.sql` 建立 administrators、login codes、sessions 及資料約束；advisory lock 序列化，版本表跳過已套用 migration，失敗不寫版本標記，DDL 可安全重跑。
- 根擁有者 bootstrap 會對 `OWNER_TG_ID` 冪等修復為 active root owner。`APP_MASTER_KEY` 透過 HKDF-SHA256 依 `admin-login-code`、`admin-session` 用途衍生不同 32-byte key。
- Telegram client 新增 `getMe` 驗證 Bot identity；long poll runner 維護 offset、映射 private/group message、API 暫時失敗退避，context 取消正常退出。Bot API 錯誤不含 token。
- `cmd/server` 整合 TDD 證明 `/adminlogin@username` 產碼後可由 `/api/auth/login` 兌換 Session；正式啟動已接 migration、根 owner、Bot identity／runner、HTTP API 與共同 signal shutdown。
- 本輪 `go test ./...`、`go vet ./...` 及 Windows server build 全數通過；尚未完成 CSRF request 驗證、限速、登出／Session 撤銷、資格事件與 PostgreSQL 真實容器整合。
- Session 撤銷 TDD：新增單一 token 與指定管理者全部 Session 撤銷，PostgreSQL 使用 digest／telegram_id 刪除。HTTP 新增 `/api/auth/me` 與要求有效 Session、canonical 32-byte double-submit CSRF 的 `/api/auth/logout`；成功清除兩個 Secure SameSite=Strict cookie。目標 auth/httpapi/postgres/server tests 與 vet 通過。
- 登入限速衍生需求已確認：登入頁改送 Telegram ID + 8 位碼；帳號 5 次／15 分鐘、來源 IP 20 次／15 分鐘、全域 100 次／分鐘。可信反代由 `TRUSTED_PROXY_CIDRS` 明列，命中才解析 `Forwarded`，其次 `X-Forwarded-For`，異常回退 RemoteAddr。
- 登入 API 已完成 Telegram ID 綁定、可信代理來源解析與併發安全三層限速；成功只清帳號／IP 失敗紀錄，全域桶只隨時間滑動，超限統一 429 與 `Retry-After: 900`。前端同步提交 Telegram ID + 8 位碼，Vitest、ESLint、TypeScript 與 Vite build 通過；補上 Testing Library 全域 cleanup，避免測試 DOM 污染。
- 使用者確認 Bot `/adminlogin` 依 Telegram ID 每分鐘最多發碼 3 次。TDD 完成精確滑動邊界與 20 路併發限制，正式 application 已接線；超限沿用通用錯誤，不揭露授權狀態。
- Telegram `chat_member` TDD 已完成 payload 解碼、member／not_member／indeterminate 防腐映射與 runner 分派；未知／缺少 `is_member` 的狀態保持 indeterminate。資格處理暫時失敗時不推進 update offset，退避後重試。
- 資格持久化 TDD：`AccessStore.ApplyQualification` 會在單一 PostgreSQL 交易中建立未知使用者、`SELECT ... FOR UPDATE`、以 `RestoreAccessAccount` 重建 domain 狀態、套用三值決策並持久化；`indeterminate` 完全不開交易，錯誤 rollback。
- 品質審查發現「狀態已提交但核心撤銷失敗」會遺失安全動作，新增 `003_core_action_outbox.sql` 與同交易 revoke outbox；重播明確不合格不會排重複 pending revoke。
- 即時資格整合 TDD：`Checker.EvaluateEvent` 對事件所在啟用群組使用已觀察結果，只查其他規則；未配置群組忽略。`MembershipHandler` 僅持久化明確 eligible／ineligible，正式 server 已組合 qualification store、checker、access store 與 Telegram runner。
- 使用者確認補償重查預設 10 Telegram requests/s、每批 50 人，Web 可調為 1–20 requests/s、10–200 人；429 尊重 `retry_after`，其他暫時錯誤含 jitter 指數退避，單項最多 5 次；每輪彙總通知，整體故障立即告警。
- 補償重查 TDD：PostgreSQL 以穩定 Telegram ID cursor 分頁已知使用者；`004_recheck_tuning.sql` 新增每秒請求與批次設定，store 驗證間隔 1 分鐘至 7 日、速率 1–20、批次 10–200。
- 新增 RecheckCoordinator 與可取消 RecheckScheduler：啟動立即執行，每輪重讀設定；讀取設定失敗以 1 分鐘退避避免忙迴圈，正常輪次使用最新 interval。RuleManager 成功寫入已驗證規則後可用非阻塞 Trigger 合併排隊重查。
- 補償通知 TDD：查詢所有 active administrator 並逐一發送單次彙總；個別送達失敗不阻止其他收件人，故障訊息不包含底層錯誤。彙總分類 Telegram 暫時錯誤、未知 membership status 與未分類來源。
- 品質審查修復：Checker 只有對 Telegram temporary error 產生 indeterminate；永久 4xx／設定錯誤向上回報並觸發重大故障通知，仍不產生撤銷決策。server 已並列啟動 HTTP、Bot long poll 與補償重查 scheduler。
- 憑證安全 TDD：新增 AES-256-GCM credential cipher，以 Telegram ID 與 generation 作 AAD；私人訂閱 token 另以用途隔離 HMAC-SHA256 digest 查詢。無效輸入、零值 cipher、nonce／密文錯誤均 fail closed，資料庫不保存明文。
- 核發持久化 TDD：Claim／Approve／Rotate 先完整產生秘密，再於單一交易鎖定使用者、推進 domain 狀態、密封憑證、更新共享配額、排 `reconcile` outbox 並寫稽核；任何錯誤 rollback 且不回半套秘密。migration 005 允許核心 `reconcile` 動作。
- Bootstrap 新增必填 `WEB_PUBLIC_URL`，只接受無 userinfo/query/fragment 的 HTTPS base URL並保留 path prefix。私人 `/sub/{token}` 連結只接受 canonical 32-byte Raw Base64URL token。
- VPN access TDD：`/vpn` 即時查核資格；明確不合格落地後拒絕、暫時未決不突變、首次合格交易式核發、既有 active 使用者回原私人訂閱連結。Bot 只在私聊回 URL，錯誤訊息不洩漏底層細節。
- 組裝層 RED 證明缺少 VPN provider 時原本會靜默啟動；GREEN 收緊為必填，正式 `main` 以 `credential-encryption` 與 `subscription-token-digest` HKDF 金鑰組合 cipher、provisioning store、credential store、link builder 及 access service。目標 tests 與 vet 通過。
- sing-box 設定生成前完成衍生需求澄清：雙棧採每協定明確綁定部署者確認的 IPv4 與 IPv6，各自建立 inbound，不依賴 IPv4-mapped wildcard；四協定 `users[].name` 採 Telegram ID 十進位字串以統一 V2Ray API 統計聚合。
- sing-box generator TDD：雙棧產生 8 個 inbound、單棧只產生 4 個；四協定使用相同 Telegram ID 統計名稱，空使用者可生成，預設拒絕 IPv4 出站，明確開啟後採 `prefer_ipv6`。RED 抓到 IPv4-mapped IPv6 可繞過位址族限制，GREEN 改為拒絕 `Is4In6()`，並拒絕純空白協定密碼。
- active credential 快照 TDD：PostgreSQL 只讀 active+eligible 並依 Telegram ID 穩定排序；四個聚合陣列先驗長度、ID、generation，再逐筆 AEAD 解密，任何資料或解密錯誤整批回 nil，不產生部分核心設定。
- 通用秘密值 TDD：新增用途綁定 AES-256-GCM `ValueCipher`，每次 fresh nonce、錯誤 purpose／空值／零值 cipher fail closed。migration 006 建立 `core_settings` singleton，未配置可安全存在，配置後 DB 約束完整位址、ports、TLS、REALITY、loopback stats 與加密私鑰；store 只在載入時解密 REALITY 私鑰。
- 核心快照與 outbox TDD：`SnapshotBuilder` 每次重讀設定與 active users，覆蓋任何殘留 users，錯誤不回部分 JSON。`CoreActionStore` 以 `FOR UPDATE SKIP LOCKED` 原子批次租約領取，優先 revoke；完成與重試只接受正且唯一 ID，`last_error` 只保存封閉錯誤代碼。
- 安裝交易 TDD：候選設定先 stage/check，成功才 promote/restart；check 失敗 discard。新核心 restart 失敗時 rollback 舊設定並再次 restart，rollback／舊核心重啟失敗回明確 stage，outbox 不得誤完成。
- 核心 worker 衍生需求確認：空閒每 1 秒輪詢；失敗依 attempts 採 5 秒／15 秒／1 分／5 分／15 分後封頂 15 分退避；計畫預告部分送達失敗不阻止重啟；預告期間安全撤銷插隊時以最新快照一次完成兩類 outbox。
- 核心 worker TDD：一般變更只通知一次並合併 30 秒、計畫重啟每 60 秒最多一次、安全 revoke 立即插隊、每筆 action 個別退避且只寫封閉錯誤分類。品質回歸修復 completion SQL 暫時失敗造成重複重啟，以及非法 claim batch 留下部分 pending 狀態。
- 使用者選擇受限控制 sidecar；只有 sidecar 可接觸 Docker Engine 且只能控制固定 sing-box container，主 Web 應用只走權限受限 Unix socket。FileDeployment TDD 已完成同目錄 0600 candidate、controller check、原子 rename promote、restart 失敗還原舊檔與暫存清理。
- 核心控制 sidecar TDD：Unix protocol 只允許受控 candidate 的 `/v1/check` 與固定 `/v1/restart`；RuntimeController 不經 shell 執行固定 sing-box argv，且 Docker API 只重啟白名單容器。`cmd/core-controller` 使用安全 Unix 路徑、0660 socket、signal shutdown，Linux amd64 交叉 build 通過。
- 核心通知 TDD：計畫重啟向所有 active+eligible VPN 使用者送固定訊息，單一送達失敗不阻止其他收件人或重啟；核心失敗只向 active 管理者送封閉 failure code。PostgreSQL 稽核只保存 attempted／failed 與白名單分類，不保存 Telegram 底層錯誤。
- 主應用新增 `CORE_CONTROL_SOCKET`（預設 `/run/s12ryt/core-control.sock`）與 `SINGBOX_CONFIG_PATH`（預設 `/var/lib/s12ryt/sing-box/config.json`），只接受 clean absolute Unix path。正式 server 已組裝 encrypted core settings、active credential snapshot、Unix controller、FileDeployment、Installer、CoreActionStore、TelegramNotifier 與 core worker，並列為第四個必要 goroutine。
- 本輪完整回歸：`go test ./...`、`go vet ./...`、Windows server build、Linux amd64 server／core-controller 交叉 build及前端 Vitest／ESLint／TypeScript／Vite build 全數通過。本機仍無 Docker、gopls，race 留 GitHub Linux runner。
- sing-box Stats API 契約已依官方原始碼固定：使用 `/v2ray.core.app.stats.command.StatsService/QueryStats`、`patterns` + `regexp=true` + `reset=true`，解析 `user>>><TelegramID>>>traffic>>>uplink|downlink` 並跨四協定／雙棧聚合。以 grpc-go 窄 client 與 protowire codec 實作，不引入整個 sing-box module。
- 流量入帳 TDD：Collector 嚴格拒絕 malformed／重複／負值／溢位計數；`TrafficStore.RecordBatch` 在單一交易鎖定使用者與共享 quota，更新 activity／usage／blocked 並同交易排 revoke 或 reconcile。核心 active users 快照新增 `quota.blocked=FALSE`，避免達額者被一般 reconcile 誤恢復。
- 使用者確認流量故障策略：reset 後先寫 0600 原子持久 spool、DB commit 後刪除且重啟優先重播；首次故障立即告警、每 15 分鐘摘要、恢復通知；持續 5 分鐘後在 DB 可用時撤銷全部 active 憑證，補帳恢復後 reconcile。已同步需求契約。
- 流量 spool TDD：批次使用內容決定 SHA-256 ID；migration 007 與 `RecordPendingBatch` 在 quota/outbox 同交易去重，修復 DB commit 後刪除 spool 前崩潰造成的雙重計費。
- migration 008 建立 `traffic_health` 全域封閉狀態。`SetFailClosed` 僅在狀態轉換時批次排 durable revoke/reconcile；核心 active credential 快照強制 `fail_closed=FALSE`，避免完整 reconcile 繞過封閉閘門。PostgreSQL、trafficrunner、trafficstats 與 domain 測試及 vet 全綠。
- 訂閱格式協商 TDD：明確 `?format=sing-box|clash|base64` 優先於 User-Agent；sing-box、Mihomo/Clash UA 自動選格式，未知 UA 固定 Base64，未知明確格式 fail closed。subscription tests/vet 全綠。
- 持久故障監控 TDD：PostgreSQL 鎖定 singleton 後保存首次故障時間、白名單 stage 與最後通知時間；持續 5 分鐘原子切換 fail-closed，首次及每 15 分鐘才允許通知，恢復時保留舊狀態後清除並為未達額 active users 排 reconcile。
- 新增 traffic Telegram notifier 與封閉 audit 欄位；個別送達失敗不中止其他管理者，訊息與稽核不包含底層 Stats/spool/DB 錯誤。DynamicCollector 每 15 秒重讀核心 StatsListen、dial/collect/close，正式 server 以第五個必要 goroutine 啟動。
- 本輪完整回歸：`go test ./...`、`go vet ./...`、Linux amd64 server/core-controller 交叉 build，以及前端 Vitest 2/2、ESLint、TypeScript、Vite build 全數通過。
- 三格式訂閱 TDD：Base64 URI、sing-box JSON 與 Mihomo 設定均輸出雙棧四協定節點，REALITY 只由私鑰推導並輸出公鑰，不輸出訂閱 token 或私鑰；明確 query 優先、未知 UA 預設 Base64。
- 訂閱 HTTP TDD：`GET /sub/{token}` 使用 no-store/nosniff，格式錯誤固定 400、無效或不可用 token 固定 404且遮蔽底層。組裝 RED 證明缺少 renderer 會造成部署路徑不完整；GREEN 將 subscription service 設為 `buildApplication` 必填並與 core worker 共用 `CoreSettingsStore`。
- sing-box 工作項完整回歸：`go test ./...`、`go vet ./...`、Windows server build、Linux amd64 server/core-controller build，以及前端 Vitest 2/2、ESLint、TypeScript/Vite build 全數通過；本機仍無 Docker/gopls，race 與容器整合留 GitHub Linux CI。

## REALITY 搜尋切片（2026-08-17）
- `internal/reality`：searcher（樣本 200／並發 5／預算 60s、嚴格域名白名單、依延遲排序、逾時回部分結果）、tls_prober（TLS1.3+憑證鏈+主機名驗證、僅 443、可注入 dial hermetic 測試）、embedded dataset（`dataset-top-domains.txt` pinned SHA-256 `e0545a8a...`、sync.Once 快取、空檔/篡改拒絕）、背景 `Service`（idle/running/completed/failed、ErrSearchRunning、Snapshot 深複製）。
- 缺陷修復兩項：(1) Windows 寫入造成 dataset CRLF 汙染使 pinned checksum 不符 → 以 LF 正規化還原（hash 完全吻合 pinned）並新增 `.gitattributes`（dataset/*.sh 強制 eol=lf、*.ps1/*.bat crlf、其餘 text 正規化），未改 pinned 常數；(2) `Target.Latency` 原標籤 latency_ms 會輸出 ns → 改 `json:"-"` + MarshalJSON/UnmarshalJSON 毫秒整數，RED 斷言 `"latency_ms":42}` 曾因子字串誤綠已加嚴。
- pre-existing 測試炸彈修復：`cmd/server/tls_runtime_test.go` fixture 用真實 time.Now 產 NotBefore 但 Ensure 固定 now=2026-08-17 11:00 UTC，過 11:01 即紅；改 fixture 接收 now（NotBefore=now-1m、NotAfter=now+90d）。
- HTTP：`POST /api/settings/reality/search`（owner+CSRF、WithoutCancel 背景、409 running/500 start_failed/202 running）、`GET` 回快照；handler.go 獨立 guard 註冊。
- server 接線：`buildApplicationWithOptions` 要求 managementSettings 實作 `RealitySearchManager`；main 以 EmbeddedDataset+TLSProber(5s)+Service(200/5/60s) 組裝。
- Web：core 頁新增「搜尋 REALITY 目標」按鈕（POST 無 body 僅 CSRF header）→ 500ms 輪詢 GET 至非 running（上限 130 次）→ 結果清單顯示 domain/毫秒(toFixed(1))/TLS1.3 與「採用」按鈕填入 reality_server；REALITY 目標 label 改「REALITY 目標網域」；logout 清理。
- 驗證：`go test ./internal/reality`、全量 `go test ./...`、`go vet ./...`、Windows server build、Linux amd64 server/core-controller 交叉 build、前端 Vitest 13/13、ESLint、Vite build 全綠。
- 上游阻塞複查（2026-08-17）：sing-box 同日發布 v1.13.19 stable，但 go.mod 仍為 x/crypto v0.48.0、x/net v0.50.0、grpc v1.79.1，未達 Trivy 修復版（grpc>=1.79.3、x/crypto>=0.52.0、x/net>=0.56.0）；1.14.0 仍為 beta（prerelease，workflow 正確排除）。維持 release 失敗現場，不重複觸發、不寬鬆閘門；weekly 排程會在上游修復後自動重試。
- 剩餘任務懷疑式複查（2026-08-17）：(1) release 阻塞驗證——上游最新 stable 仍為 v1.13.19（同日 09:47 UTC）且依賴未修復；release.yml weekly cron（週一 03:17 UTC）存在、GitHub workflow state=active、stable 解析排除 prerelease/draft、`gh release view` 冪等保護與 `--ignore-unfixed` 均確認；發現並修復 `gh release create --notes` 內字面 `\n` 缺陷（bash 雙引號不轉義，首次成功發佈時 notes 會顯示字面反斜線），改以 printf 組真實換行並加 contract regression test（commit 0f4b2b0，CI 32026260891 綠）。(2) 部署後驗收準備驗證——`scripts/install.sh` 與 `scripts/post-deploy-check.sh` `bash -n` 語法通過、工作副本純 LF（0 個 CR bytes）、`git add --renormalize` 零變更；真實主機執行仍為外部依賴（本機無 Docker、需真實 Telegram/ACME/網域）。
- 發佈閘門重大缺陷發現與修復（2026-08-17）：檢查發現 release 採「先 --push 再 Trivy 掃描」，導致失敗 run（31977587653 手動、31992918951 排程——排程於週一 03:59 UTC 實際觸發，自動重試機制已獲實證）把含 16 HIGH+1 CRITICAL 的 `ghcr.io/s12ryt/s12ryt-sing-box:1.13.18-45ca32dcb966` 推到公開 registry。修復（commit 000656e）：三個 image 都改為「amd64 --load 本地建 → Trivy 掃描通過 → 才 multi-arch --sbom --provenance --push」，加 contract regression test（RED 先抓到 push 早於 scan）。實證：觸發 run 32027102307，`Scan sing-box image before publishing` 失敗、其後所有 push 步驟 skipped，匿名 registry tag 清單不變（無新漏洞 tag 公開）。
- 待 owner 清理：公開殘留漏洞 tag `1.13.18-45ca32dcb966`（修復前產生）。本機 gh token scopes 僅 gist/read:org/repo/workflow，無 packages 權限無法代刪。owner 指令：`gh auth refresh -s read:packages,delete:packages` 後 `gh api /user/packages/container/s12ryt-sing-box/versions` 找出該 tag 的 version id，再 `gh api -X DELETE /user/packages/container/s12ryt-sing-box/versions/<id>`。
- 漏洞 tag 清理完成（2026-08-17，commits 5c3b676/481096b）：查證官方文件確認「package 由 repo workflow 發佈→repo 取得 admin role→GITHUB_TOKEN 可在 Actions 內以 REST API 刪除 package」，因此新增受契約測試保護的 `package-cleanup.yml`（workflow_dispatch、固定只針對 s12ryt-sing-box、嚴格 tag 格式、拒絕歧義匹配、刪後驗證；無 PAT/秘密）。首次嘗試刪單一 version 被 GitHub 400 拒絕（最後一個 tagged version 不可單刪），加入明確 `delete_package` 整包模式後 run 32028309428 success；匿名 registry 查詢由原本可列出 tag 變為 403，公開漏洞 image `1.13.18-45ca32dcb966` 確認清除。下次成功 release 會重建 package。
- 部署腳本靜態分析納入 CI（2026-08-17，commit 0eb9d62）：ci.yml go job 新增 `shellcheck scripts/install.sh scripts/post-deploy-check.sh`（runner 內建 shellcheck；contract test 先 RED 後 GREEN）；CI run 32028769506 四 job 全綠，兩腳本在真實 Linux runner 通過 shellcheck。至此部署腳本具備：bash -n、byte 級 LF、renormalize、shellcheck、CI 映像建置五層證據；真實主機驗收仍待實機。
- 套件可見度運維缺口補完（2026-08-17）：刪除漏洞 package 後發現 GHCR 由 workflow 首次推送建立的 package 預設 private（舊 package 之曾可匿名拉取是因曾被設 public），故下次 release 成功時三個 package 都會以 private 重建、匿名 pull 會被拒。已補：README「套件可見度（首次成功發佈後必做）」段落（改 public 或以 read:packages token docker login）、release.yml 成功時輸出 `::notice::` 提醒、掃描步驟註明 amd64 為 arm64 之代理掃描（同 Go 依賴圖＋同 distroless 版本）；以 `TestReleasePublicationVisibilityIsDocumented` 契約測試鎖定（先 RED）。
- 安裝腳本拉取失敗提示（2026-08-17，commit f884ee2）：install.sh 的 `docker compose pull postgres sing-box` 改為 if ! 包裹，失敗時輸出指向 GitHub 套件可見度設定或 read:packages docker login 的繁中提示（對應首次 release 後 package 以 private 重建的情境）；installer contract test 先 RED 後 GREEN，CI 32029903292 全綠（含 shellcheck 對新寫法通過）。至此四輪懷疑式複查共修復：release notes 字面 \n、先推後掃閘門、漏洞 package 清理機制、套件可見度文件、安裝拉取提示；兩項外部阻塞（上游依賴修復、實機驗收）在本地可推進之事已全部完成。
