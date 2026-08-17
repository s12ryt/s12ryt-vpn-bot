import { FormEvent, useEffect, useState } from 'react'
import { Activity, Check, Globe, KeyRound, LogOut, Network, RefreshCw, ScrollText, Settings2, ShieldCheck, ShieldX, Trash2, UserCog, UserRound, Users, X } from 'lucide-react'

import './styles.css'

type VPNUser = {
  telegram_id: number
  eligible: boolean
  status: string
  generation: number
  used_bytes: number
  limit_bytes: number
  quota_blocked: boolean
}

type AdministratorIdentity = {
  telegram_id: number
  role: 'owner' | 'administrator'
  root: boolean
}

type AuditEvent = {
  id: number
  actor_telegram_id: number | null
  action: string
  target_type: string
  target_id: string
  details: Record<string, unknown>
  created_at: string
}

type ManagementSettings = {
  qualification_mode: 'any' | 'all'
  recheck_interval_minutes: number
  recheck_requests_per_second: number
  recheck_batch_size: number
  inactivity_threshold_days: number
  quota_limit_bytes: number
}

type QualificationRule = {
  chat_id: number
  chat_type: 'supergroup' | 'channel'
  title: string
  enabled: boolean
  bot_administrator_passed: boolean
}

type CoreSettings = {
  configured: boolean
  listen_ipv4: string
  listen_ipv6: string
  vless_port: number
  hysteria2_port: number
  tuic_port: number
  anytls_port: number
  tls_server_name: string
  tls_certificate_path: string
  tls_key_path: string
  reality_server: string
  reality_server_port: number
  reality_short_id: string
  stats_listen: string
  allow_ipv4_outbound: boolean
  has_reality_private_key: boolean
}

type TLSSettings = {
  configured: boolean
  mode: 'sslip_io' | 'duckdns' | 'custom'
  domain: string
  challenge: 'http_01' | 'dns_01'
  email: string
  ca_directory_urls: string[]
  terms_accepted: boolean
  has_duckdns_token: boolean
  state: 'unissued' | 'issued' | 'failed'
  certificate_expires_at: string
  last_issued_ca: string
}

type Overview = {
  users: { total_users: number; active_users: number; pending_approvals: number; blocked_users: number; total_used_bytes: number }
  tls_issued: boolean
  core_configured: boolean
}

type WorkspaceView = 'overview' | 'users' | 'administrators' | 'settings' | 'core' | 'tls' | 'audit'

function formatBytes(bytes: number) {
  return `${(bytes / 1_000_000_000).toFixed(2)} GB`
}

const statusLabels: Record<string, string> = {
  active: '使用中',
  pending_approval: '等待核准',
  approval_rejected: '已拒絕',
  self_service: '可重新領取',
  permanently_blocked: '永久封鎖',
  unclaimed: '尚未領取',
}

function LegalNotice({ compact = false }: { compact?: boolean }) {
  return (
    <span className={compact ? 'legal-notice legal-notice--compact' : 'legal-notice'}>
      <a href="https://github.com/s12ryt/s12ryt-vpn-bot" target="_blank" rel="noreferrer">AGPL-3.0 原始碼</a>
      {!compact && <span>本程式不提供任何擔保</span>}
    </span>
  )
}

function currentCSRFToken() {
  const prefix = 'vpn_csrf_token='
  const token = document.cookie.split(';').map((part) => part.trim()).find((part) => part.startsWith(prefix))?.slice(prefix.length) ?? ''
  return /^[A-Za-z0-9_-]{43}$/.test(token) ? token : ''
}

function App() {
  const [loginCode, setLoginCode] = useState('')
  const [telegramID, setTelegramID] = useState('')
  const [initialCSRFToken] = useState(currentCSRFToken)
  const [csrfToken, setCSRFToken] = useState(initialCSRFToken)
  const [users, setUsers] = useState<VPNUser[]>([])
  const [identity, setIdentity] = useState<AdministratorIdentity | null>(null)
  const [administrators, setAdministrators] = useState<AdministratorIdentity[]>([])
  const [auditEvents, setAuditEvents] = useState<AuditEvent[]>([])
  const [managementSettings, setManagementSettings] = useState<ManagementSettings | null>(null)
  const [savedInactivityDays, setSavedInactivityDays] = useState(0)
  const [qualificationRules, setQualificationRules] = useState<QualificationRule[]>([])
  const [coreSettings, setCoreSettings] = useState<CoreSettings | null>(null)
  const [realityPrivateKey, setRealityPrivateKey] = useState('')
  const [tlsSettings, setTLSSettings] = useState<TLSSettings | null>(null)
  const [duckdnsToken, setDuckdnsToken] = useState('')
  const [overview, setOverview] = useState<Overview | null>(null)
  const [qualificationChatID, setQualificationChatID] = useState('')
  const [qualificationChatType, setQualificationChatType] = useState<'supergroup' | 'channel'>('supergroup')
  const [qualificationTitle, setQualificationTitle] = useState('')
  const [view, setView] = useState<WorkspaceView>('users')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  const numericTelegramID = Number(telegramID)
  const telegramIDValid = /^[1-9]\d*$/.test(telegramID) && Number.isSafeInteger(numericTelegramID)
  const canSubmit = telegramIDValid && loginCode.length === 8 && !submitting

  async function loadUsers() {
    const response = await fetch('/api/users?limit=50', { credentials: 'include' })
    if (!response.ok) throw new Error('users unavailable')
    const payload = await response.json() as { users: VPNUser[] }
    setUsers(Array.isArray(payload.users) ? payload.users : [])
  }

  async function loadIdentity() {
    const response = await fetch('/api/auth/me', { credentials: 'include' })
    if (!response.ok) throw new Error('session invalid')
    const payload = await response.json() as AdministratorIdentity
    if (!Number.isSafeInteger(payload.telegram_id) || payload.telegram_id <= 0 || !['owner', 'administrator'].includes(payload.role) || (payload.root && payload.role !== 'owner')) {
      throw new Error('identity invalid')
    }
    setIdentity(payload)
    return payload
  }

  async function loadAdministrators() {
    const response = await fetch('/api/administrators', { credentials: 'include' })
    if (!response.ok) throw new Error('administrators unavailable')
    const payload = await response.json() as { administrators: AdministratorIdentity[] }
    setAdministrators(Array.isArray(payload.administrators) ? payload.administrators : [])
  }

  async function loadAuditEvents() {
    const response = await fetch('/api/audit?limit=50', { credentials: 'include' })
    if (!response.ok) throw new Error('audit unavailable')
    const payload = await response.json() as { events: AuditEvent[] }
    setAuditEvents(Array.isArray(payload.events) ? payload.events : [])
  }

  async function loadManagementSettings() {
    const response = await fetch('/api/settings/management', { credentials: 'include' })
    if (!response.ok) throw new Error('settings unavailable')
    const payload = await response.json() as { settings: ManagementSettings, rules: QualificationRule[] }
    if (!payload.settings || !['any', 'all'].includes(payload.settings.qualification_mode)) throw new Error('settings invalid')
    setManagementSettings(payload.settings)
    setSavedInactivityDays(payload.settings.inactivity_threshold_days)
    setQualificationRules(Array.isArray(payload.rules) ? payload.rules : [])
  }

  async function loadCoreSettings() {
    const response = await fetch('/api/settings/core', { credentials: 'include' })
    if (!response.ok) throw new Error('core settings unavailable')
    const payload = await response.json() as CoreSettings
    if (!payload || typeof payload.configured !== 'boolean' || typeof payload.has_reality_private_key !== 'boolean') throw new Error('core settings invalid')
    setCoreSettings(payload)
    setRealityPrivateKey('')
  }

  async function loadOverview() {
    const response = await fetch('/api/overview', { credentials: 'include' })
    if (!response.ok) throw new Error('overview unavailable')
    const payload = await response.json() as Overview
    if (!payload.users || typeof payload.tls_issued !== 'boolean' || typeof payload.core_configured !== 'boolean') throw new Error('overview invalid')
    setOverview(payload)
  }

  async function showOverview() {
    setError('')
    try {
      await loadOverview()
      setView('overview')
    } catch {
      setError('無法載入營運總覽。')
    }
  }

  async function loadTLSSettings() {
    const response = await fetch('/api/settings/tls', { credentials: 'include' })
    if (!response.ok) throw new Error('tls settings unavailable')
    const payload = await response.json() as { settings: TLSSettings }
    const settings = payload.settings
    if (!settings || !['sslip_io', 'duckdns', 'custom'].includes(settings.mode) || !['unissued', 'issued', 'failed'].includes(settings.state)) throw new Error('tls settings invalid')
    setTLSSettings(settings)
    setDuckdnsToken('')
  }

  useEffect(() => {
    if (!initialCSRFToken) return
    void (async () => {
      try {
        await loadIdentity()
        await loadUsers()
      } catch {
        setCSRFToken('')
        setUsers([])
        setIdentity(null)
      }
    })()
  }, [initialCSRFToken])

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!canSubmit) return
    setSubmitting(true)
    setError('')
    try {
      const response = await fetch('/api/auth/login', {
        method: 'POST', credentials: 'include', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ telegram_id: numericTelegramID, code: loginCode }),
      })
      if (!response.ok) throw new Error('login failed')
      const payload = await response.json() as { csrf_token?: string }
      if (!payload.csrf_token) throw new Error('missing csrf')
      setCSRFToken(payload.csrf_token)
      await loadIdentity()
      await loadUsers()
    } catch {
      setError('登入失敗，請重新取得登入碼。')
    } finally {
      setSubmitting(false)
    }
  }

  async function runAction(id: number, action: string, body?: object) {
    setError('')
    try {
      const response = await fetch(`/api/users/${id}/${action}`, {
        method: 'POST', credentials: 'include',
        headers: body ? { 'X-CSRF-Token': csrfToken, 'Content-Type': 'application/json' } : { 'X-CSRF-Token': csrfToken },
        body: body ? JSON.stringify(body) : undefined,
      })
      if (!response.ok) throw new Error('operation failed')
      await loadUsers()
    } catch {
      setError('操作失敗，請重新整理後再試。')
    }
  }

  async function logout() {
    try {
      const response = await fetch('/api/auth/logout', {
        method: 'POST', credentials: 'include', headers: { 'X-CSRF-Token': csrfToken },
      })
      if (!response.ok) throw new Error('logout failed')
      setCSRFToken('')
      setUsers([])
      setIdentity(null)
      setAdministrators([])
      setAuditEvents([])
      setManagementSettings(null)
      setQualificationRules([])
      setCoreSettings(null)
      setRealityPrivateKey('')
      setTLSSettings(null)
      setDuckdnsToken('')
      setOverview(null)
      setView('users')
      setLoginCode('')
    } catch {
      setError('登出失敗，請稍後再試。')
    }
  }

  function confirmAction(message: string, action: () => void) {
    if (window.confirm(message)) action()
  }

  async function showAdministrators() {
    setError('')
    try {
      await loadAdministrators()
      setView('administrators')
    } catch {
      setError('無法載入管理員資料。')
    }
  }

  async function showAudit() {
    setError('')
    try {
      await loadAuditEvents()
      setView('audit')
    } catch {
      setError('無法載入稽核紀錄。')
    }
  }

  async function showManagementSettings() {
    setError('')
    try {
      await loadManagementSettings()
      setView('settings')
    } catch {
      setError('無法載入全域設定。')
    }
  }

  async function showCoreSettings() {
    setError('')
    try {
      await loadCoreSettings()
      setView('core')
    } catch {
      setError('無法載入 VPN 核心設定。')
    }
  }

  async function showTLSSettings() {
    setError('')
    try {
      await loadTLSSettings()
      setView('tls')
    } catch {
      setError('無法載入 TLS 憑證設定。')
    }
  }

  function updateManagementSetting<K extends keyof ManagementSettings>(key: K, value: ManagementSettings[K]) {
    setManagementSettings((current) => current ? { ...current, [key]: value } : current)
  }

  async function saveManagementSettings(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!managementSettings) return
    setError('')
    let confirmInactivityRemoval = false
    try {
      const thresholdRemovesUsers = managementSettings.inactivity_threshold_days > 0 && (savedInactivityDays === 0 || managementSettings.inactivity_threshold_days < savedInactivityDays)
      if (thresholdRemovesUsers) {
        const preview = await fetch('/api/settings/management/inactivity-preview', {
          method: 'POST', credentials: 'include',
          headers: { 'X-CSRF-Token': csrfToken, 'Content-Type': 'application/json' },
          body: JSON.stringify({ threshold_days: managementSettings.inactivity_threshold_days }),
        })
        if (!preview.ok) throw new Error('preview failed')
        const payload = await preview.json() as { affected_users: number }
        if (!window.confirm(`這會立即移除 ${payload.affected_users} 位閒置使用者的權限，是否繼續？`)) return
        confirmInactivityRemoval = true
      }
      const response = await fetch('/api/settings/management', {
        method: 'PUT', credentials: 'include',
        headers: { 'X-CSRF-Token': csrfToken, 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...managementSettings, confirm_inactivity_removal: confirmInactivityRemoval }),
      })
      if (!response.ok) throw new Error('settings update failed')
      await loadManagementSettings()
    } catch {
      setError('全域設定儲存失敗。')
    }
  }

  async function enableQualificationRule(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!/^-?[1-9]\d*$/.test(qualificationChatID)) return
    setError('')
    try {
      const response = await fetch(`/api/settings/qualification-rules/${qualificationChatID}`, {
        method: 'PUT', credentials: 'include',
        headers: { 'X-CSRF-Token': csrfToken, 'Content-Type': 'application/json' },
        body: JSON.stringify({ chat_type: qualificationChatType, title: qualificationTitle }),
      })
      if (!response.ok) throw new Error('rule enable failed')
      setQualificationChatID('')
      setQualificationTitle('')
      await loadManagementSettings()
    } catch {
      setError('資格群組驗證失敗，請確認 Bot 已是管理員。')
    }
  }

  async function disableQualificationRule(chatID: number) {
    setError('')
    try {
      const response = await fetch(`/api/settings/qualification-rules/${chatID}`, {
        method: 'DELETE', credentials: 'include', headers: { 'X-CSRF-Token': csrfToken },
      })
      if (!response.ok) throw new Error('rule disable failed')
      await loadManagementSettings()
    } catch {
      setError('資格群組停用失敗。')
    }
  }

  function updateCoreSetting<K extends keyof CoreSettings>(key: K, value: CoreSettings[K]) {
    setCoreSettings((current) => current ? { ...current, [key]: value } : current)
  }

  async function saveCoreSettings(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!coreSettings) return
    setError('')
    try {
      const { has_reality_private_key: _hasRealityPrivateKey, ...publicSettings } = coreSettings
      void _hasRealityPrivateKey
      const response = await fetch('/api/settings/core', {
        method: 'PUT', credentials: 'include',
        headers: { 'X-CSRF-Token': csrfToken, 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...publicSettings, reality_private_key: realityPrivateKey }),
      })
      if (!response.ok) throw new Error('core settings update failed')
      await loadCoreSettings()
    } catch {
      setError('VPN 核心設定儲存失敗，請檢查位址、連接埠與憑證資料。')
    }
  }

  function updateTLSSetting<K extends keyof TLSSettings>(key: K, value: TLSSettings[K]) {
    setTLSSettings((current) => current ? { ...current, [key]: value } : current)
  }

  function changeTLSMode(mode: TLSSettings['mode']) {
    setTLSSettings((current) => {
      if (!current) return current
      const next: TLSSettings = { ...current, mode }
      if (mode === 'duckdns') next.challenge = 'dns_01'
      if (mode === 'sslip_io') next.challenge = 'http_01'
      return next
    })
  }

  async function saveTLSSettings(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!tlsSettings) return
    setError('')
    try {
      const response = await fetch('/api/settings/tls', {
        method: 'PUT', credentials: 'include',
        headers: { 'X-CSRF-Token': csrfToken, 'Content-Type': 'application/json' },
        body: JSON.stringify({
          mode: tlsSettings.mode, domain: tlsSettings.domain, challenge: tlsSettings.challenge,
          email: tlsSettings.email, ca_directory_urls: tlsSettings.ca_directory_urls,
          terms_accepted: tlsSettings.terms_accepted, duckdns_token: duckdnsToken,
        }),
      })
      if (!response.ok) throw new Error('tls settings update failed')
      await loadTLSSettings()
    } catch {
      setError('TLS 設定儲存失敗，請確認網域、模式與條款同意。')
    }
  }

  async function setAdministratorRole(id: number, role: AdministratorIdentity['role']) {
    setError('')
    try {
      const response = await fetch(`/api/administrators/${id}/role`, {
        method: 'POST', credentials: 'include',
        headers: { 'X-CSRF-Token': csrfToken, 'Content-Type': 'application/json' },
        body: JSON.stringify({ role }),
      })
      if (!response.ok) throw new Error('role update failed')
      await loadAdministrators()
    } catch {
      setError('管理員角色操作失敗。')
    }
  }

  async function removeAdministrator(id: number) {
    setError('')
    try {
      const response = await fetch(`/api/administrators/${id}`, {
        method: 'DELETE', credentials: 'include', headers: { 'X-CSRF-Token': csrfToken },
      })
      if (!response.ok) throw new Error('administrator removal failed')
      await loadAdministrators()
    } catch {
      setError('管理員角色操作失敗。')
    }
  }

  if (csrfToken) {
    return (
      <main className="app-shell">
        <header className="app-header">
          <span className="brand-mark"><ShieldCheck size={20} aria-hidden="true" /></span>
          <strong>S12RYT VPN</strong>
          <span className="service-state"><span className="service-state__dot" />核心受控</span>
          <LegalNotice compact />
          <button className="logout-button" type="button" onClick={() => void logout()}><LogOut size={17} />登出</button>
        </header>
        <div className="app-layout">
          <nav className="side-nav" aria-label="管理功能">
            <button className={view === 'overview' ? 'nav-active' : ''} type="button" onClick={() => void showOverview()}><Activity size={18} />總覽</button>
            <button className={view === 'users' ? 'nav-active' : ''} type="button" onClick={() => setView('users')}><Users size={18} />使用者</button>
            {identity?.role === 'owner' && <button className={view === 'administrators' ? 'nav-active' : ''} type="button" onClick={() => void showAdministrators()}><UserCog size={18} />管理員</button>}
            {identity?.role === 'owner' && <button className={view === 'settings' ? 'nav-active' : ''} type="button" onClick={() => void showManagementSettings()}><Settings2 size={18} />資格群組</button>}
            {identity?.role === 'owner' && <button className={view === 'core' ? 'nav-active' : ''} type="button" onClick={() => void showCoreSettings()}><Network size={18} />VPN 與網路</button>}
            {identity?.role === 'owner' && <button className={view === 'tls' ? 'nav-active' : ''} type="button" onClick={() => void showTLSSettings()}><Globe size={18} />TLS 憑證</button>}
            <button className={view === 'audit' ? 'nav-active' : ''} type="button" onClick={() => void showAudit()}><ScrollText size={18} />稽核紀錄</button>
          </nav>
          {view === 'overview' ? <section className="workspace">
            <div className="workspace-heading"><div><span className="section-label">系統狀態</span><h1>營運總覽</h1></div></div>
            {error && <p role="alert" className="form-error">{error}</p>}
            {overview && <>
              <div className="settings-grid">
                <div className="overview-stat"><strong>{overview.users.total_users.toLocaleString('en-US')}</strong><span>已知使用者</span></div>
                <div className="overview-stat"><strong>{overview.users.active_users.toLocaleString('en-US')}</strong><span>使用中</span></div>
                <div className="overview-stat"><strong>{overview.users.pending_approvals.toLocaleString('en-US')}</strong><span>等待核准</span></div>
                <div className="overview-stat"><strong>{overview.users.blocked_users.toLocaleString('en-US')}</strong><span>已封鎖</span></div>
                <div className="overview-stat"><strong>{formatBytes(overview.users.total_used_bytes)}</strong><span>本期總流量</span></div>
              </div>
              {!overview.tls_issued && <p role="alert" className="form-error">TLS 憑證尚未核發，系統不會輸出 VPN 節點。</p>}
              {!overview.core_configured && <p role="alert" className="form-error">VPN 核心尚未完成設定。</p>}
            </>}
          </section> : view === 'users' ? <section className="workspace">
            <div className="workspace-heading">
              <div><span className="section-label">存取控制</span><h1>使用者與流量</h1></div>
              <button className="icon-button" type="button" title="重新整理" aria-label="重新整理" onClick={() => void loadUsers()}><RefreshCw size={18} /></button>
            </div>
            {error && <p role="alert" className="form-error">{error}</p>}
            <div className="user-table" role="table" aria-label="VPN 使用者">
              <div className="user-row user-row--head" role="row"><span>Telegram ID</span><span>狀態</span><span>共享配額</span><span>操作</span></div>
              {users.map((vpnUser) => (
                <div className="user-row" role="row" key={vpnUser.telegram_id}>
                  <strong>{vpnUser.telegram_id}</strong>
                  <span className={`status status--${vpnUser.status}`}>{statusLabels[vpnUser.status] ?? '未知狀態'}</span>
                  <span>{formatBytes(vpnUser.used_bytes)} / {formatBytes(vpnUser.limit_bytes)}</span>
                  <span className="row-actions">
                    {(vpnUser.status === 'pending_approval' || vpnUser.status === 'approval_rejected') && <>
                      <button type="button" aria-label={`核准 ${vpnUser.telegram_id}`} title="核准" onClick={() => void runAction(vpnUser.telegram_id, 'approve')}><Check size={17} /></button>
                      {vpnUser.status === 'pending_approval' && <button type="button" aria-label={`拒絕 ${vpnUser.telegram_id}`} title="拒絕" onClick={() => confirmAction(`拒絕 ${vpnUser.telegram_id} 的申請？`, () => void runAction(vpnUser.telegram_id, 'reject'))}><X size={17} /></button>}
                    </>}
                    {vpnUser.status === 'active' && <>
                      <button type="button" onClick={() => confirmAction(`輪替 ${vpnUser.telegram_id} 的全部憑證？`, () => void runAction(vpnUser.telegram_id, 'rotate', { reset_period: false }))}>輪替</button>
                      <button type="button" aria-label={`撤銷 ${vpnUser.telegram_id}`} title="撤銷並要求審核" onClick={() => confirmAction(`立即撤銷 ${vpnUser.telegram_id} 的 VPN 存取？`, () => void runAction(vpnUser.telegram_id, 'revoke', { mode: 'requires_approval' }))}><ShieldX size={17} /></button>
                    </>}
                  </span>
                </div>
              ))}
              {users.length === 0 && <p className="empty-state">目前沒有使用者資料。</p>}
            </div>
          </section> : view === 'administrators' ? <section className="workspace">
            <div className="workspace-heading">
              <div><span className="section-label">權限控制</span><h1>管理員與角色</h1></div>
              <button className="icon-button" type="button" title="重新整理" aria-label="重新整理管理員" onClick={() => void loadAdministrators()}><RefreshCw size={18} /></button>
            </div>
            {error && <p role="alert" className="form-error">{error}</p>}
            <div className="administrator-table" role="table" aria-label="管理員角色">
              <div className="administrator-row administrator-row--head" role="row"><span>Telegram ID</span><span>角色</span><span>操作</span></div>
              {administrators.map((administrator) => (
                <div className="administrator-row" role="row" key={administrator.telegram_id}>
                  <strong>{administrator.telegram_id}</strong>
                  <span className={`status ${administrator.root ? 'status--active' : ''}`}>{administrator.root ? '根擁有者' : administrator.role === 'owner' ? '擁有者' : '一般管理員'}</span>
                  <span className="row-actions">
                    {!administrator.root && administrator.role === 'administrator' && <button type="button" aria-label={`設為擁有者 ${administrator.telegram_id}`} onClick={() => void setAdministratorRole(administrator.telegram_id, 'owner')}>設為擁有者</button>}
                    {!administrator.root && administrator.role === 'owner' && <button type="button" aria-label={`設為一般管理員 ${administrator.telegram_id}`} onClick={() => confirmAction(`將 ${administrator.telegram_id} 降級為一般管理員？`, () => void setAdministratorRole(administrator.telegram_id, 'administrator'))}>設為管理員</button>}
                    {!administrator.root && administrator.telegram_id !== identity?.telegram_id && <button type="button" aria-label={`移除管理員 ${administrator.telegram_id}`} title="移除管理員" onClick={() => confirmAction(`移除管理員 ${administrator.telegram_id}？`, () => void removeAdministrator(administrator.telegram_id))}><Trash2 size={17} /></button>}
                  </span>
                </div>
              ))}
              {administrators.length === 0 && <p className="empty-state">目前沒有管理員資料。</p>}
            </div>
          </section> : view === 'settings' ? <section className="workspace">
            <div className="workspace-heading"><div><span className="section-label">全域存取政策</span><h1>資格與配額</h1></div></div>
            {error && <p role="alert" className="form-error">{error}</p>}
            {managementSettings && <form className="settings-form" onSubmit={(event) => void saveManagementSettings(event)}>
              <div className="settings-grid">
                <label>資格符合模式<select aria-label="資格符合模式" value={managementSettings.qualification_mode} onChange={(event) => updateManagementSetting('qualification_mode', event.target.value as 'any' | 'all')}><option value="any">任一群組</option><option value="all">全部群組</option></select></label>
                <label>重查間隔（分鐘）<input type="number" min="1" max="10080" value={managementSettings.recheck_interval_minutes} onChange={(event) => updateManagementSetting('recheck_interval_minutes', Number(event.target.value))} /></label>
                <label>每秒查核請求<input type="number" min="1" max="20" value={managementSettings.recheck_requests_per_second} onChange={(event) => updateManagementSetting('recheck_requests_per_second', Number(event.target.value))} /></label>
                <label>每批使用者<input type="number" min="10" max="200" value={managementSettings.recheck_batch_size} onChange={(event) => updateManagementSetting('recheck_batch_size', Number(event.target.value))} /></label>
                <label>閒置移除天數<input type="number" min="0" value={managementSettings.inactivity_threshold_days} onChange={(event) => updateManagementSetting('inactivity_threshold_days', Number(event.target.value))} /></label>
                <label>每期共享配額（bytes）<input type="number" min="1" value={managementSettings.quota_limit_bytes} onChange={(event) => updateManagementSetting('quota_limit_bytes', Number(event.target.value))} /></label>
              </div>
              <button className="primary-action" type="submit">儲存全域設定</button>
            </form>}
            <form className="rule-editor" onSubmit={(event) => void enableQualificationRule(event)}>
              <label>群組 Chat ID<input aria-label="群組 Chat ID" type="text" inputMode="numeric" value={qualificationChatID} onChange={(event) => setQualificationChatID(event.target.value.replace(/(?!^-)[^0-9]/g, ''))} required /></label>
              <label>類型<select aria-label="群組類型" value={qualificationChatType} onChange={(event) => setQualificationChatType(event.target.value as 'supergroup' | 'channel')}><option value="supergroup">Supergroup</option><option value="channel">Channel</option></select></label>
              <label>群組名稱<input aria-label="群組名稱" type="text" maxLength={200} value={qualificationTitle} onChange={(event) => setQualificationTitle(event.target.value)} /></label>
              <button className="primary-action" type="submit">驗證並啟用群組</button>
            </form>
            <div className="rule-list" aria-label="資格群組清單">
              <h2>已登記群組</h2>
              {qualificationRules.map((rule) => <div className="rule-row" key={rule.chat_id}><div><strong>{rule.title || rule.chat_id}</strong><span>{rule.chat_type} / {rule.chat_id}</span></div><span className="rule-state"><span className={`status status--${rule.enabled ? 'active' : 'unclaimed'}`}>{rule.enabled ? '啟用' : '停用'}</span>{rule.enabled && <button type="button" aria-label={`停用群組 ${rule.chat_id}`} title="停用群組" onClick={() => confirmAction(`停用資格群組 ${rule.chat_id}？`, () => void disableQualificationRule(rule.chat_id))}><Trash2 size={16} /></button>}</span></div>)}
              {qualificationRules.length === 0 && <p className="empty-state">尚未登記資格群組。</p>}
            </div>
          </section> : view === 'core' ? <section className="workspace">
            <div className="workspace-heading"><div><span className="section-label">核心與連線</span><h1>VPN 與網路</h1></div></div>
            {error && <p role="alert" className="form-error">{error}</p>}
            {coreSettings && <form className="settings-form" onSubmit={(event) => void saveCoreSettings(event)}>
              <div className="settings-grid">
                <label>公開 IPv4<input aria-label="公開 IPv4" value={coreSettings.listen_ipv4} onChange={(event) => updateCoreSetting('listen_ipv4', event.target.value)} placeholder="203.0.113.10" /></label>
                <label>公開 IPv6<input aria-label="公開 IPv6" value={coreSettings.listen_ipv6} onChange={(event) => updateCoreSetting('listen_ipv6', event.target.value)} placeholder="2001:db8::10" /></label>
                <label>VLESS TCP port<input type="number" min="1" max="65535" value={coreSettings.vless_port} onChange={(event) => updateCoreSetting('vless_port', Number(event.target.value))} /></label>
                <label>Hysteria2 UDP port<input type="number" min="1" max="65535" value={coreSettings.hysteria2_port} onChange={(event) => updateCoreSetting('hysteria2_port', Number(event.target.value))} /></label>
                <label>TUIC UDP port<input type="number" min="1" max="65535" value={coreSettings.tuic_port} onChange={(event) => updateCoreSetting('tuic_port', Number(event.target.value))} /></label>
                <label>AnyTLS TCP port<input type="number" min="1" max="65535" value={coreSettings.anytls_port} onChange={(event) => updateCoreSetting('anytls_port', Number(event.target.value))} /></label>
                <label>TLS 網域<input value={coreSettings.tls_server_name} onChange={(event) => updateCoreSetting('tls_server_name', event.target.value)} /></label>
                <label>TLS 憑證路徑<input value={coreSettings.tls_certificate_path} onChange={(event) => updateCoreSetting('tls_certificate_path', event.target.value)} /></label>
                <label>TLS 私鑰路徑<input value={coreSettings.tls_key_path} onChange={(event) => updateCoreSetting('tls_key_path', event.target.value)} /></label>
                <label>REALITY 目標<input value={coreSettings.reality_server} onChange={(event) => updateCoreSetting('reality_server', event.target.value)} /></label>
                <label>REALITY 目標 port<input type="number" min="1" max="65535" value={coreSettings.reality_server_port} onChange={(event) => updateCoreSetting('reality_server_port', Number(event.target.value))} /></label>
                <label>REALITY short ID<input value={coreSettings.reality_short_id} onChange={(event) => updateCoreSetting('reality_short_id', event.target.value.replace(/[^0-9A-Fa-f]/g, '').slice(0, 16))} /></label>
                <label>Stats 監聽<input value={coreSettings.stats_listen} onChange={(event) => updateCoreSetting('stats_listen', event.target.value)} /></label>
                <label>REALITY 私鑰<input type="password" autoComplete="new-password" value={realityPrivateKey} onChange={(event) => setRealityPrivateKey(event.target.value)} placeholder={coreSettings.has_reality_private_key ? '留空以保留現有私鑰' : '32-byte Base64URL 私鑰'} /></label>
              </div>
              <p className="secret-state">{coreSettings.has_reality_private_key ? 'REALITY 私鑰已安全儲存' : '尚未設定 REALITY 私鑰'}</p>
              <label className="toggle-control"><input aria-label="允許 IPv4 出站" type="checkbox" checked={coreSettings.allow_ipv4_outbound} onChange={(event) => updateCoreSetting('allow_ipv4_outbound', event.target.checked)} /><span>允許 IPv4 出站</span></label>
              <button className="primary-action" type="submit">儲存 VPN 設定</button>
            </form>}
          </section> : view === 'tls' ? <section className="workspace">
            <div className="workspace-heading"><div><span className="section-label">受信任 TLS 憑證</span><h1>TLS 與網域</h1></div></div>
            {error && <p role="alert" className="form-error">{error}</p>}
            {tlsSettings && <form className="settings-form" onSubmit={(event) => void saveTLSSettings(event)}>
              <div className="settings-grid">
                <label>憑證模式<select aria-label="憑證模式" value={tlsSettings.mode} onChange={(event) => changeTLSMode(event.target.value as TLSSettings['mode'])}>
                  <option value="sslip_io">sslip.io（HTTP-01）</option>
                  <option value="duckdns">DuckDNS（DNS-01）</option>
                  <option value="custom">自有網域</option>
                </select></label>
                <label>連線網域<input aria-label="連線網域" type="text" value={tlsSettings.domain} onChange={(event) => updateTLSSetting('domain', event.target.value)} placeholder="node.duckdns.org" /></label>
                <label>ACME Email（可留空）<input aria-label="ACME Email" type="email" value={tlsSettings.email} onChange={(event) => updateTLSSetting('email', event.target.value)} placeholder="owner@example.com" /></label>
                <label>CA 目錄（每行一個）<textarea aria-label="CA 目錄" rows={3} value={tlsSettings.ca_directory_urls.join('\n')} onChange={(event) => updateTLSSetting('ca_directory_urls', event.target.value.split('\n').map((line) => line.trim()).filter((line) => line.length > 0))} /></label>
                <label>DuckDNS Token<input aria-label="DuckDNS Token" type="password" autoComplete="new-password" value={duckdnsToken} onChange={(event) => setDuckdnsToken(event.target.value)} placeholder={tlsSettings.has_duckdns_token ? '留空以保留現有 Token' : 'DuckDNS 帳號 token'} /></label>
              </div>
              <p className="secret-state">憑證狀態：{tlsSettings.state === 'issued' ? '已核發' : tlsSettings.state === 'failed' ? '簽發失敗' : '未核發'}</p>
              {tlsSettings.state === 'issued' && <p className="secret-state">到期：{new Date(tlsSettings.certificate_expires_at).toISOString().replace('.000Z', 'Z')}</p>}
              {tlsSettings.state !== 'issued' && <p className="secret-state">未取得受信任憑證前，系統不會輸出任何 VPN 節點。</p>}
              <label className="toggle-control"><input aria-label="同意憑證機構條款" type="checkbox" checked={tlsSettings.terms_accepted} onChange={(event) => updateTLSSetting('terms_accepted', event.target.checked)} /><span>同意憑證機構條款</span></label>
              <button className="primary-action" type="submit">儲存 TLS 設定</button>
            </form>}
          </section> : <section className="workspace">
            <div className="workspace-heading">
              <div><span className="section-label">系統追蹤</span><h1>稽核紀錄</h1></div>
              <button className="icon-button" type="button" title="重新整理" aria-label="重新整理稽核紀錄" onClick={() => void loadAuditEvents()}><RefreshCw size={18} /></button>
            </div>
            {error && <p role="alert" className="form-error">{error}</p>}
            <div className="audit-list" aria-label="系統稽核紀錄">
              {auditEvents.map((event) => (
                <article className="audit-entry" key={event.id}>
                  <div><strong>{event.action}</strong><span>{new Date(event.created_at).toISOString().replace('.000Z', 'Z')}</span></div>
                  <div><span>{event.target_type} / {event.target_id || '-'}</span><span>操作者 {event.actor_telegram_id ?? '系統'}</span></div>
                  <code>{JSON.stringify(event.details)}</code>
                </article>
              ))}
              {auditEvents.length === 0 && <p className="empty-state">目前沒有稽核紀錄。</p>}
            </div>
          </section>}
        </div>
      </main>
    )
  }

  return (
    <main className="login-shell">
      <header className="brand-bar" aria-label="S12RYT VPN">
        <span className="brand-mark" aria-hidden="true"><ShieldCheck size={20} strokeWidth={2} /></span>
        <strong>S12RYT VPN</strong>
        <span className="service-state"><span className="service-state__dot" aria-hidden="true" />管理服務</span>
      </header>
      <section className="login-workspace">
        <div className="login-heading"><span className="section-label">安全登入</span><h1>VPN 管理中心</h1></div>
        <form className="login-form" onSubmit={handleSubmit}>
          <label htmlFor="telegram-id">Telegram ID</label>
          <div className="field-control"><UserRound size={19} aria-hidden="true" /><input id="telegram-id" name="telegram-id" type="text" value={telegramID} onChange={(event) => setTelegramID(event.target.value.replace(/\D/g, '').slice(0, 16))} autoComplete="username" inputMode="numeric" pattern="[1-9][0-9]*" required /></div>
          <label htmlFor="login-code">8 位登入碼</label>
          <div className="code-control"><KeyRound size={19} aria-hidden="true" /><input id="login-code" name="login-code" type="text" value={loginCode} onChange={(event) => setLoginCode(event.target.value.replace(/[^A-Za-z0-9]/g, '').slice(0, 8))} autoComplete="one-time-code" autoCapitalize="none" spellCheck={false} inputMode="text" maxLength={8} required /><span className="code-count" aria-hidden="true">{loginCode.length}/8</span></div>
          {error && <p role="alert" className="form-error">{error}</p>}
          <button type="submit" disabled={!canSubmit}>{submitting ? '登入中' : '登入管理面板'}</button>
        </form>
        <footer className="login-footer"><LegalNotice /><span aria-hidden="true">TLS</span></footer>
      </section>
    </main>
  )
}

export default App
