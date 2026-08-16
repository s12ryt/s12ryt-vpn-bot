# 外部 HTTPS 反向代理

應用只提供 HTTP，預設監聽 `0.0.0.0:35699`。正式入口必須是受信任的 HTTPS；防火牆應只允許反向代理或 Tunnel 到達 35699。VLESS REALITY 預設占用主機 **TCP 443**，所以同一公開 IP 的 Web 代理不能也監聽 TCP 443。可採第二個 IP、非 443 HTTPS port，或 Cloudflare Tunnel。

`WEB_PUBLIC_URL` 必須與瀏覽器實際入口完全一致。只有代理來源位於 `TRUSTED_PROXY_CIDRS` 時，應用才信任 `Forwarded`／`X-Forwarded-For`；不要把該 CIDR 設成全網。

## Nginx（第二個 IP）

以下假設 `198.51.100.20` 是只供面板使用的第二個 IP；VPN 的 TCP 443 仍留在另一個 IP：

```nginx
server {
    listen 198.51.100.20:443 ssl http2;
    server_name panel.example.com;
    ssl_certificate /etc/letsencrypt/live/panel.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/panel.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:35699;
        proxy_set_header Host $host;
        proxy_set_header Forwarded "for=$remote_addr;proto=https;host=$host";
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
    }
}
```

將 `TRUSTED_PROXY_CIDRS` 設成 Nginx 到應用時使用的來源，例如 `127.0.0.1/32`。

## Caddy（第二個 IP或自訂 HTTPS port）

```caddyfile
https://panel.example.com:8444 {
    reverse_proxy 127.0.0.1:35699
}
```

此例的 `WEB_PUBLIC_URL` 是 `https://panel.example.com:8444`。若有第二個 IP，可讓 Caddy 綁該 IP 的 443；不要與 VLESS REALITY 共用同一個 IP:port。

## Cloudflare Tunnel

Tunnel 不需在主機占用 TCP 443，最適合單一 IP：

```yaml
tunnel: YOUR_TUNNEL_ID
credentials-file: /etc/cloudflared/YOUR_TUNNEL_ID.json
ingress:
  - hostname: panel.example.com
    service: http://127.0.0.1:35699
  - service: http_status:404
```

在 Cloudflare 建立 DNS route 後，將 `WEB_PUBLIC_URL=https://panel.example.com`。依 cloudflared 實際連線來源設定 `TRUSTED_PROXY_CIDRS`；若同機透過 loopback連入，使用 `127.0.0.1/32`。Cloudflare 憑證與 Tunnel credentials 不得放入 repository。

## 驗收

```bash
curl --fail --silent https://panel.example.com/health/live
curl --fail --silent https://panel.example.com/ | grep -F 'VPN 管理中心'
```

確認 HTTP 入口未直接暴露、Session cookie 帶 `Secure`，以及 VPN 所在 IP 的 TCP 443 仍由 sing-box 使用。
