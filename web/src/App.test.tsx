import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import App from './App'

describe('管理面板登入', () => {
	afterEach(() => {
		vi.unstubAllGlobals()
		document.cookie = 'vpn_csrf_token=; Max-Age=0; Path=/'
	})

	it('要求管理者輸入 Telegram ID 與 Bot 私訊提供的 8 位登入碼', async () => {
    const user = userEvent.setup()
    render(<App />)

    expect(screen.getByRole('heading', { name: 'VPN 管理中心' })).toBeInTheDocument()
		const codeInput = screen.getByRole('textbox', { name: '8 位登入碼' })
		const telegramIDInput = screen.getByRole('textbox', { name: 'Telegram ID' })
    const submit = screen.getByRole('button', { name: '登入管理面板' })
    expect(submit).toBeDisabled()
		expect(screen.getByRole('link', { name: 'AGPL-3.0 原始碼' })).toHaveAttribute('href', 'https://github.com/s12ryt/s12ryt-vpn-bot')

		await user.type(telegramIDInput, '12a345')
		await user.type(codeInput, 'Ab12!cd345')

		expect(telegramIDInput).toHaveValue('12345')
		expect(codeInput).toHaveValue('Ab12cd34')
		expect(submit).toBeEnabled()
	})

	it('以 cookie session 提交精確登入契約且不顯示伺服器錯誤細節', async () => {
		const user = userEvent.setup()
		const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 401 })
		vi.stubGlobal('fetch', fetchMock)
		render(<App />)

		await user.type(screen.getByRole('textbox', { name: 'Telegram ID' }), '12345')
		await user.type(screen.getByRole('textbox', { name: '8 位登入碼' }), 'Ab12Cd34')
		await user.click(screen.getByRole('button', { name: '登入管理面板' }))

		expect(fetchMock).toHaveBeenCalledWith('/api/auth/login', {
			method: 'POST',
			credentials: 'include',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ telegram_id: 12345, code: 'Ab12Cd34' }),
		})
		expect(await screen.findByRole('alert')).toHaveTextContent('登入失敗，請重新取得登入碼。')
	})

	it('登入後載入使用者與共享配額並可核准待審使用者', async () => {
		const user = userEvent.setup()
		const fetchMock = vi.fn()
			.mockResolvedValueOnce({ ok: true, json: async () => ({ csrf_token: 'csrf-token' }) })
			.mockResolvedValueOnce({ ok: true, json: async () => ({ telegram_id: 77, role: 'administrator', root: false }) })
			.mockResolvedValueOnce({ ok: true, json: async () => ({ users: [{ telegram_id: 12345, eligible: true, status: 'pending_approval', generation: 1, period_started_at: '2026-08-01T00:00:00Z', last_vpn_activity_at: '2026-08-02T00:00:00Z', used_bytes: 25000000000, limit_bytes: 50000000000, quota_blocked: false }] }) })
			.mockResolvedValueOnce({ ok: true })
			.mockResolvedValueOnce({ ok: true, json: async () => ({ users: [] }) })
		vi.stubGlobal('fetch', fetchMock)
		render(<App />)
		await user.type(screen.getByRole('textbox', { name: 'Telegram ID' }), '77')
		await user.type(screen.getByRole('textbox', { name: '8 位登入碼' }), 'Ab12Cd34')
		await user.click(screen.getByRole('button', { name: '登入管理面板' }))
		expect(await screen.findByRole('heading', { name: '使用者與流量' })).toBeInTheDocument()
		expect(screen.getByText('25.00 GB / 50.00 GB')).toBeInTheDocument()
		await user.click(screen.getByRole('button', { name: '核准 12345' }))
		expect(fetchMock).toHaveBeenCalledWith('/api/users/12345/approve', expect.objectContaining({
			method: 'POST',
			headers: { 'X-CSRF-Token': 'csrf-token' },
			credentials: 'include',
		}))
	})

	it('沿用現有安全 session 並可登出', async () => {
		document.cookie = 'vpn_csrf_token=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA; Path=/'
		const fetchMock = vi.fn()
			.mockResolvedValueOnce({ ok: true, json: async () => ({ telegram_id: 77, role: 'owner', root: true }) })
			.mockResolvedValueOnce({ ok: true, json: async () => ({ users: [] }) })
			.mockResolvedValueOnce({ ok: true })
		vi.stubGlobal('fetch', fetchMock)
		const user = userEvent.setup()
		render(<App />)
		expect(await screen.findByRole('heading', { name: '使用者與流量' })).toBeInTheDocument()
		expect(screen.getByRole('link', { name: 'AGPL-3.0 原始碼' })).toHaveAttribute('href', 'https://github.com/s12ryt/s12ryt-vpn-bot')
		await user.click(screen.getByRole('button', { name: '登出' }))
		expect(fetchMock).toHaveBeenCalledWith('/api/auth/logout', {
			method: 'POST', credentials: 'include',
			headers: { 'X-CSRF-Token': 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA' },
		})
		expect(await screen.findByRole('heading', { name: 'VPN 管理中心' })).toBeInTheDocument()
	})

	it('擁有者可管理非根管理者角色', async () => {
		document.cookie = 'vpn_csrf_token=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA; Path=/'
		const fetchMock = vi.fn()
			.mockResolvedValueOnce({ ok: true, json: async () => ({ telegram_id: 77, role: 'owner', root: true }) })
			.mockResolvedValueOnce({ ok: true, json: async () => ({ users: [] }) })
			.mockResolvedValueOnce({ ok: true, json: async () => ({ administrators: [
				{ telegram_id: 77, role: 'owner', root: true },
				{ telegram_id: 202, role: 'administrator', root: false },
			] }) })
			.mockResolvedValueOnce({ ok: true })
			.mockResolvedValueOnce({ ok: true, json: async () => ({ administrators: [] }) })
		vi.stubGlobal('fetch', fetchMock)
		const user = userEvent.setup()
		render(<App />)
		await user.click(await screen.findByRole('button', { name: '管理員' }))
		expect(await screen.findByRole('heading', { name: '管理員與角色' })).toBeInTheDocument()
		expect(screen.getByText('根擁有者')).toBeInTheDocument()
		await user.click(screen.getByRole('button', { name: '設為擁有者 202' }))
		expect(fetchMock).toHaveBeenCalledWith('/api/administrators/202/role', {
			method: 'POST', credentials: 'include',
			headers: { 'X-CSRF-Token': 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA', 'Content-Type': 'application/json' },
			body: JSON.stringify({ role: 'owner' }),
		})
	})

	it('管理者可檢視不含秘密的稽核紀錄', async () => {
		document.cookie = 'vpn_csrf_token=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA; Path=/'
		const fetchMock = vi.fn()
			.mockResolvedValueOnce({ ok: true, json: async () => ({ telegram_id: 77, role: 'administrator', root: false }) })
			.mockResolvedValueOnce({ ok: true, json: async () => ({ users: [] }) })
			.mockResolvedValueOnce({ ok: true, json: async () => ({ events: [{
				id: 9, actor_telegram_id: 77, action: 'vpn.revoke', target_type: 'vpn_user', target_id: '12345',
				details: { mode: 'requires_approval' }, created_at: '2026-08-17T00:00:00Z',
			}] }) })
		vi.stubGlobal('fetch', fetchMock)
		const user = userEvent.setup()
		render(<App />)
		await user.click(await screen.findByRole('button', { name: '稽核紀錄' }))
		expect(await screen.findByRole('heading', { name: '稽核紀錄' })).toBeInTheDocument()
		expect(screen.getByText('vpn.revoke')).toBeInTheDocument()
		expect(screen.getByText('vpn_user / 12345')).toBeInTheDocument()
		expect(fetchMock).toHaveBeenCalledWith('/api/audit?limit=50', { credentials: 'include' })
	})

	it('擁有者可檢視並更新資格、重查、閒置與共享配額設定', async () => {
		document.cookie = 'vpn_csrf_token=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA; Path=/'
		const fetchMock = vi.fn()
			.mockResolvedValueOnce({ ok: true, json: async () => ({ telegram_id: 77, role: 'owner', root: true }) })
			.mockResolvedValueOnce({ ok: true, json: async () => ({ users: [] }) })
			.mockResolvedValueOnce({ ok: true, json: async () => ({
				settings: { qualification_mode: 'any', recheck_interval_minutes: 60, recheck_requests_per_second: 10, recheck_batch_size: 50, inactivity_threshold_days: 0, quota_limit_bytes: 50000000000 },
				rules: [{ chat_id: -1001, chat_type: 'supergroup', title: '會員群', enabled: true, bot_administrator_passed: true }],
			}) })
			.mockResolvedValueOnce({ ok: true })
		vi.stubGlobal('fetch', fetchMock)
		const user = userEvent.setup()
		render(<App />)
		await user.click(await screen.findByRole('button', { name: '資格群組' }))
		expect(await screen.findByRole('heading', { name: '資格與配額' })).toBeInTheDocument()
		expect(screen.getByText('會員群')).toBeInTheDocument()
		await user.selectOptions(screen.getByRole('combobox', { name: '資格符合模式' }), 'all')
		await user.click(screen.getByRole('button', { name: '儲存全域設定' }))
		expect(fetchMock).toHaveBeenCalledWith('/api/settings/management', {
			method: 'PUT', credentials: 'include',
			headers: { 'X-CSRF-Token': 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA', 'Content-Type': 'application/json' },
			body: JSON.stringify({ qualification_mode: 'all', recheck_interval_minutes: 60, recheck_requests_per_second: 10, recheck_batch_size: 50, inactivity_threshold_days: 0, quota_limit_bytes: 50000000000, confirm_inactivity_removal: false }),
		})
	})

	it('擁有者啟用資格群組時提交 Bot 管理員驗證契約', async () => {
		document.cookie = 'vpn_csrf_token=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA; Path=/'
		const settings = { qualification_mode: 'any', recheck_interval_minutes: 60, recheck_requests_per_second: 10, recheck_batch_size: 50, inactivity_threshold_days: 0, quota_limit_bytes: 50000000000 }
		const fetchMock = vi.fn()
			.mockResolvedValueOnce({ ok: true, json: async () => ({ telegram_id: 77, role: 'owner', root: true }) })
			.mockResolvedValueOnce({ ok: true, json: async () => ({ users: [] }) })
			.mockResolvedValueOnce({ ok: true, json: async () => ({ settings, rules: [] }) })
			.mockResolvedValueOnce({ ok: true })
			.mockResolvedValueOnce({ ok: true, json: async () => ({ settings, rules: [{ chat_id: -1001, chat_type: 'supergroup', title: '會員群', enabled: true, bot_administrator_passed: true }] }) })
		vi.stubGlobal('fetch', fetchMock)
		const user = userEvent.setup()
		render(<App />)
		await user.click(await screen.findByRole('button', { name: '資格群組' }))
		await user.type(screen.getByRole('textbox', { name: '群組 Chat ID' }), '-1001')
		await user.type(screen.getByRole('textbox', { name: '群組名稱' }), '會員群')
		await user.click(screen.getByRole('button', { name: '驗證並啟用群組' }))
		expect(fetchMock).toHaveBeenCalledWith('/api/settings/qualification-rules/-1001', {
			method: 'PUT', credentials: 'include',
			headers: { 'X-CSRF-Token': 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA', 'Content-Type': 'application/json' },
			body: JSON.stringify({ chat_type: 'supergroup', title: '會員群' }),
		})
		expect(await screen.findByText('會員群')).toBeInTheDocument()
	})

	it('擁有者可更新不回傳私鑰的 VPN 與網路設定', async () => {
		document.cookie = 'vpn_csrf_token=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA; Path=/'
		const coreSettings = {
			configured: true, listen_ipv4: '203.0.113.10', listen_ipv6: '2001:db8::10',
			vless_port: 443, hysteria2_port: 443, tuic_port: 8443, anytls_port: 8443,
			tls_server_name: 'vpn.example.com', tls_certificate_path: '/run/tls/fullchain.pem', tls_key_path: '/run/tls/privkey.pem',
			reality_server: 'www.example.com', reality_server_port: 443, reality_short_id: '0123456789abcdef',
			stats_listen: '127.0.0.1:10085', allow_ipv4_outbound: false, has_reality_private_key: true,
		}
		const fetchMock = vi.fn()
			.mockResolvedValueOnce({ ok: true, json: async () => ({ telegram_id: 77, role: 'owner', root: true }) })
			.mockResolvedValueOnce({ ok: true, json: async () => ({ users: [] }) })
			.mockResolvedValueOnce({ ok: true, json: async () => coreSettings })
			.mockResolvedValueOnce({ ok: true })
			.mockResolvedValueOnce({ ok: true, json: async () => ({ ...coreSettings, allow_ipv4_outbound: true }) })
		vi.stubGlobal('fetch', fetchMock)
		const user = userEvent.setup()
		render(<App />)
		await user.click(await screen.findByRole('button', { name: 'VPN 與網路' }))
		expect(await screen.findByRole('heading', { name: 'VPN 與網路' })).toBeInTheDocument()
		expect(screen.getByText('REALITY 私鑰已安全儲存')).toBeInTheDocument()
		await user.click(screen.getByRole('checkbox', { name: '允許 IPv4 出站' }))
		await user.click(screen.getByRole('button', { name: '儲存 VPN 設定' }))
		expect(fetchMock).toHaveBeenCalledWith('/api/settings/core', {
			method: 'PUT', credentials: 'include',
			headers: { 'X-CSRF-Token': 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA', 'Content-Type': 'application/json' },
			body: JSON.stringify({ ...coreSettings, has_reality_private_key: undefined, allow_ipv4_outbound: true, reality_private_key: '' }),
		})
	})

	it('擁有者可設定 TLS 網域模式與 DuckDNS token', async () => {
		document.cookie = 'vpn_csrf_token=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA; Path=/'
		const tlsSettings = {
			configured: false, mode: 'custom', domain: 'vpn.example.com', challenge: 'http_01', email: '',
			ca_directory_urls: ['https://acme-v02.api.letsencrypt.org/directory'], terms_accepted: false,
			has_duckdns_token: false, state: 'unissued', certificate_expires_at: '0001-01-01T00:00:00Z', last_issued_ca: '',
		}
		const fetchMock = vi.fn()
			.mockResolvedValueOnce({ ok: true, json: async () => ({ telegram_id: 77, role: 'owner', root: true }) })
			.mockResolvedValueOnce({ ok: true, json: async () => ({ users: [] }) })
			.mockResolvedValueOnce({ ok: true, json: async () => ({ settings: tlsSettings }) })
			.mockResolvedValueOnce({ ok: true })
			.mockResolvedValueOnce({ ok: true, json: async () => ({ settings: { ...tlsSettings, mode: 'duckdns', state: 'issued' } }) })
		vi.stubGlobal('fetch', fetchMock)
		const user = userEvent.setup()
		render(<App />)
		await user.click(await screen.findByRole('button', { name: 'TLS 憑證' }))
		expect(await screen.findByRole('heading', { name: 'TLS 與網域' })).toBeInTheDocument()
		expect(screen.getByText('憑證狀態：未核發')).toBeInTheDocument()
		await user.selectOptions(screen.getByRole('combobox', { name: '憑證模式' }), 'duckdns')
		const domainInput = screen.getByRole('textbox', { name: '連線網域' })
		await user.clear(domainInput)
		await user.type(domainInput, 'node.duckdns.org')
		await user.type(screen.getByLabelText('DuckDNS Token'), 'duck-token')
		await user.click(screen.getByRole('checkbox', { name: '同意憑證機構條款' }))
		await user.click(screen.getByRole('button', { name: '儲存 TLS 設定' }))
		expect(fetchMock).toHaveBeenCalledWith('/api/settings/tls', {
			method: 'PUT', credentials: 'include',
			headers: { 'X-CSRF-Token': 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA', 'Content-Type': 'application/json' },
			body: JSON.stringify({
				mode: 'duckdns', domain: 'node.duckdns.org', challenge: 'dns_01',
				email: '', ca_directory_urls: ['https://acme-v02.api.letsencrypt.org/directory'],
				terms_accepted: true, duckdns_token: 'duck-token',
			}),
		})
	})

	it('登入後顯示營運總覽與未完成設定警示', async () => {
		document.cookie = 'vpn_csrf_token=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA; Path=/'
		const fetchMock = vi.fn()
			.mockResolvedValueOnce({ ok: true, json: async () => ({ telegram_id: 77, role: 'administrator', root: false }) })
			.mockResolvedValueOnce({ ok: true, json: async () => ({ users: [] }) })
			.mockResolvedValueOnce({ ok: true, json: async () => ({ users: { total_users: 1200, active_users: 900, pending_approvals: 15, blocked_users: 4, total_used_bytes: 4500000000000 }, tls_issued: false, core_configured: true }) })
		vi.stubGlobal('fetch', fetchMock)
		const user = userEvent.setup()
		render(<App />)
		await user.click(await screen.findByRole('button', { name: '總覽' }))
		expect(await screen.findByRole('heading', { name: '營運總覽' })).toBeInTheDocument()
		expect(screen.getByText('1,200')).toBeInTheDocument()
		expect(screen.getByText('900')).toBeInTheDocument()
		expect(screen.getByText('15')).toBeInTheDocument()
		expect(fetchMock).toHaveBeenCalledWith('/api/overview', expect.objectContaining({ credentials: 'include' }))
		expect(screen.getByText('TLS 憑證尚未核發，系統不會輸出 VPN 節點。')).toBeInTheDocument()
	})
})
