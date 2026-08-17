# 備份與還原

Compose 的 `backup` service 啟動後會立即建立一次 PostgreSQL custom-format dump，之後每 24 小時執行一次。封存檔位於 `backups` volume，預設保留 7 天。擁有者可在 Web 管理面板的「備份」頁將保留期調整為 1 至 3650 天；備份程序會在每次執行時重新讀取 PostgreSQL 中的最新設定，不需重啟 container。

若保留期設定因資料庫或 migration 暫時不可讀，系統仍會建立當次加密備份，但會跳過舊檔清理。這個失敗封閉行為避免使用過時或推測的保留期刪除 archive。

## 安全模型

- `pg_dump` 與 `pg_restore` 透過 `PGDATABASE` 環境取得連線，不把資料庫 URL 放在 process arguments。
- dump 先在記憶體中以 `APP_MASTER_KEY` 經用途隔離 HKDF 衍生的金鑰做 AES-256-GCM 加密。
- 每份 archive 使用新的 nonce，檔案以 `0600`、同目錄暫存、`fsync` 與原子 rename 寫入。
- archive 不含 `APP_MASTER_KEY`。遺失根金鑰後無法還原；根金鑰外洩則所有既有備份都應視為外洩。
- 還原程式會先驗證格式與 AEAD 完整性；驗證失敗時不會執行 `pg_restore`。
- 保留期只由 PostgreSQL 中的 Web 設定控制；環境變數不會覆蓋管理者已確認的政策。

備份目前有 512 MiB 的安全上限，避免錯誤設定耗盡 container 記憶體。資料庫接近此大小前，應改用經審查的串流分塊封存或外部 PostgreSQL 備份平台。

## 檢查備份

```bash
docker compose ps backup
docker compose logs --since=25h backup
docker volume inspect s12ryt-vpn-bot_backups
```

定期將加密 archive 複製到與主機故障域分離、具存取控制的儲存空間。不要把 archive 或 `.env` 放進 Git。

## 還原演練

還原會使用 `--clean --if-exists` 修改目標資料庫。先停止 app、core worker 與流量寫入，並確認 `RESTORE_ARCHIVE` 指向 backup container 內的絕對路徑：

```bash
docker compose stop app backup
docker compose run --rm \
  --entrypoint /usr/local/bin/restore \
  -e RESTORE_ARCHIVE=/var/lib/s12ryt/backups/vpn-20260816T120000Z.dump.enc \
  backup
docker compose start app backup
```

還原後至少驗證：

```bash
curl --fail http://127.0.0.1:35699/health/ready
docker compose logs --since=10m app
```

正式環境應在隔離的測試資料庫定期做還原演練；只確認備份檔存在不等同於可還原。
