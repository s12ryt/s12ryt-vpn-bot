# 完整歷史任務

## 2026-08-16：建立專案

- [x] 澄清 Telegram 資格、VPN 協定、配額、訂閱、閒置淘汰、角色、登入、TLS、部署與 CI 契約。
- [x] 確認公開 GitHub repository、AGPL-3.0、公開 GHCR 與 amd64／arm64。
- [x] 寫入 `agent/question.md` 作為第一版實作與驗收依據。
- [x] 建立 Go／React／PostgreSQL 專案骨架與基線。
- [x] 完成資格三值判定、存取生命週期、獨立憑證、共享配額、流量活動與閒置預覽領域契約。
- [x] 完成管理 RBAC、一次性登入碼、Session、Telegram long poll 與 Web 登入最小垂直路徑。
- [x] 建立 PostgreSQL auth schema migration、根擁有者 bootstrap 與登入碼／Session store。
- [x] 完成 CSRF 驗證、登入三層限速、登出／Session 撤銷與 Bot 發碼限速。
- [x] 完成 Telegram `chat_member` 即時資格事件、完整規則重評與交易式狀態／撤銷 outbox 落地。
- [x] 完成 Telegram 補償重驗：可調間隔／速率／批次、429／暫時錯誤重試、穩定分頁、原因分類、管理者彙總、啟動排程與規則變更 trigger。
- [x] 完成四協定 CSPRNG 憑證 AEAD 儲存、交易式核發／核准／輪替、私人訂閱連結與 Bot `/vpn` 正式執行路徑。
- [x] 完成 sing-box 四協定單棧／雙棧 deterministic 設定生成、IPv6-only 出站與 V2Ray 使用者統計設定。
- [x] 完成加密核心設定、active credential 全有或全無快照載入、durable outbox 租約 store 與可 rollback 的安裝交易。
- [x] 完成核心 outbox worker 狀態機：安全撤銷插隊、30 秒預告合併、60 秒計畫重啟限制、封閉錯誤分類與 attempts 退避。
- [x] 完成流量 reset 後 0600 原子 spool、批次內容雜湊與 PostgreSQL 冪等重播，避免崩潰後重複計費。
- [x] 完成全域流量故障封閉閘門；封閉時核心快照排除全部使用者並排 revoke，恢復時只為未達額使用者排 reconcile。
- [x] 完成持久流量故障監控：首次與每 15 分鐘通知、5 分鐘 fail-closed、恢復補帳通知及第五個 server runtime goroutine。
- [x] 完成同檔案系統原子 sing-box 設定 stage／check／promote／rollback 檔案 adapter；確認主應用不得直接掛載 Docker socket。
- [x] 完成受限 Unix socket 核心控制協定、固定容器 sidecar 與主應用 core worker 正式接線。
- [x] 完成計畫重啟／核心失敗 Telegram 通知、部分送達容錯與 attempted／failed 稽核。
- [x] 完成 sing-box Stats 採集、共享配額入帳、持久 spool／故障封閉、配額週期恢復與三格式智慧訂閱正式 HTTP 路徑。
- [x] 完成 Web 使用者讀模型與核准／拒絕／撤銷／輪替 API，以及響應式使用者流量工作區、Session 還原與登出。
- [x] 完成稽核讀模型、owner 角色管理、全域資格／配額／閒置政策交易與對應響應式 Web 工作區。
- [x] 建立非 root 多階段應用／控制器映像與 Compose 安全拓撲契約；PostgreSQL 僅綁 loopback，只有 sidecar 掛載 Docker socket。
- [x] 建立不含真實秘密的 `.env.example`，要求明確資料庫 URL、根金鑰產生方式與不可變 sing-box image digest。
- [ ] 完成 Telegram 使用者命令、inline 審批與管理操作。
- [ ] 依 RED → GREEN → REFACTOR 完成領域、整合、Web、部署與供應鏈。
- [x] 建立並推送公開 `s12ryt/s12ryt-vpn-bot`。
- [ ] 完成 GitHub Actions、正式 GHCR release 與部署後真實驗收。
