import { FormEvent, useEffect, useState } from 'react'
import { Check, KeyRound, LogOut, RefreshCw, ShieldCheck, ShieldX, UserRound, Users, X } from 'lucide-react'

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

  useEffect(() => {
    if (!initialCSRFToken) return
    void (async () => {
      try {
        const identity = await fetch('/api/auth/me', { credentials: 'include' })
        if (!identity.ok) throw new Error('session invalid')
        await loadUsers()
      } catch {
        setCSRFToken('')
        setUsers([])
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
      setLoginCode('')
    } catch {
      setError('登出失敗，請稍後再試。')
    }
  }

  function confirmAction(message: string, action: () => void) {
    if (window.confirm(message)) action()
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
            <button className="nav-active" type="button"><Users size={18} />使用者</button>
            <button type="button" disabled>資格群組</button>
            <button type="button" disabled>VPN 與網路</button>
            <button type="button" disabled>稽核紀錄</button>
          </nav>
          <section className="workspace">
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
          </section>
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
