# Deploying CloudChat (Ubuntu + systemd)

This sets up CloudChat on one Linux server (Ubuntu 22.04/24.04) with Caddy for
HTTPS, Redis on the same machine, and CloudChat as a systemd service. Replace
`chat.example.com` with your domain everywhere.

```
Visitor ──> Cloudflare ──443──> Caddy (HTTPS) ──> 127.0.0.1:8080 CloudChat ──> 127.0.0.1:6379 Redis
```

There are two ways to put HTTPS in front of it:

| | Config | When |
|---|---|---|
| **Behind Cloudflare** | [`Caddyfile`](Caddyfile) + [Cloudflare settings](#cloudflare) | Default for servers outside mainland China |
| **Own certificate, no CDN** | [`Caddyfile.own-cert`](Caddyfile.own-cert) or [`nginx.conf`](nginx.conf) | E.g. a server in mainland China (needs ICP filing) with a certificate from your cloud provider |

## 1. Before you start

- Your domain's DNS: behind Cloudflare, an **A** (and optionally **AAAA**) record
  pointing at the server with the **proxy (orange cloud) on**; without a CDN, the
  same records unproxied.
- Firewall: SSH plus port 443 (and 80 without a CDN). Behind Cloudflare, only
  accept web traffic from Cloudflare, so nobody can bypass it:

```bash
sudo ufw allow OpenSSH
# Behind Cloudflare — re-run when https://www.cloudflare.com/ips/ changes:
for ip in $(curl -fsS https://www.cloudflare.com/ips-v4) $(curl -fsS https://www.cloudflare.com/ips-v6); do
  sudo ufw allow from "$ip" to any port 443 proto tcp
done
# Own certificate, no CDN — instead: sudo ufw allow 80,443/tcp
sudo ufw enable
```

## 2. Install the software

```bash
# Redis
sudo apt update && sudo apt install -y redis-server git curl

# Go 1.26+ (Ubuntu's package is too old)
curl -fsSL https://go.dev/dl/go1.26.0.linux-amd64.tar.gz | sudo tar -C /usr/local -xz
echo 'export PATH=$PATH:/usr/local/go/bin' | sudo tee /etc/profile.d/go.sh && source /etc/profile.d/go.sh

# Node.js 22 (only needed to build the frontend)
curl -fsSL https://deb.nodesource.com/setup_22.x | sudo -E bash - && sudo apt install -y nodejs

# Caddy (https://caddyserver.com/docs/install#debian-ubuntu-raspbian)
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt update && sudo apt install -y caddy
```

## 3. Get and build CloudChat

```bash
sudo useradd --system --home /opt/cloudchat --shell /usr/sbin/nologin cloudchat
sudo git clone https://github.com/tudou90/cloudchat.git /opt/cloudchat
cd /opt/cloudchat
(cd frontend && sudo npm ci && sudo npm run build)
sudo /usr/local/go/bin/go build -o cloudchat ./cmd/server
```

The code stays owned by root; the service runs as `cloudchat` and only reads it.

## 4. Configure Redis

Merge [`redis.conf`](redis.conf) into `/etc/redis/redis.conf`: listen on localhost
only, set a password, cap memory, and **turn persistence off** (so deleted chats
don't linger in snapshots on disk).

```bash
openssl rand -hex 32          # use this as the Redis password
sudo nano /etc/redis/redis.conf
sudo systemctl restart redis-server
redis-cli -a 'THE_PASSWORD' ping   # → PONG
```

## 5. Configure CloudChat

```bash
sudo cp deploy/env.production /opt/cloudchat/.env
sudo nano /opt/cloudchat/.env       # set PUBLIC_URL and REDIS_PASSWORD
sudo chown cloudchat:cloudchat /opt/cloudchat/.env && sudo chmod 600 /opt/cloudchat/.env
```

Shared files are stored in `/var/lib/cloudchat/files` (systemd creates it, readable
only by the service) and deleted from disk when their room is; Redis holds only
their name, type and size. `FILE_STORAGE_LIMIT_MB` caps how much disk they use.

Must be right: `PUBLIC_URL=https://your-domain` (secure cookies, HSTS, SEO) and
`TRUSTED_PROXIES=127.0.0.1,::1` (otherwise every visitor shares one rate limit).
Keep it that way behind Cloudflare too: Caddy works out the visitor's real IP
from Cloudflare and passes only that on.

## 6. Start CloudChat

```bash
sudo cp deploy/cloudchat.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now cloudchat
curl http://127.0.0.1:8080/healthz      # → {"status":"ok"}
```

## 7. HTTPS with Caddy

Put the certificate on the server — behind Cloudflare, a **Cloudflare Origin
Certificate** (dashboard → SSL/TLS → Origin Server → Create certificate, PEM);
otherwise the certificate and key from your provider (full chain):

```bash
sudo mkdir -p /etc/caddy/certs
sudo nano /etc/caddy/certs/origin.pem       # Cloudflare: certificate  (own cert: fullchain.pem)
sudo nano /etc/caddy/certs/origin-key.pem   # Cloudflare: private key  (own cert: privkey.pem)
sudo chown -R root:caddy /etc/caddy/certs && sudo chmod 750 /etc/caddy/certs && sudo chmod 640 /etc/caddy/certs/*
```

```bash
sudo cp deploy/Caddyfile /etc/caddy/Caddyfile   # own cert: deploy/Caddyfile.own-cert
sudo nano /etc/caddy/Caddyfile                  # your domain
sudo systemctl reload caddy
curl https://chat.example.com/healthz           # → {"status":"ok"}
```

Prefer nginx? Use [`nginx.conf`](nginx.conf) (own certificate) instead of Caddy.

### Cloudflare

In the Cloudflare dashboard for your domain:

- **SSL/TLS → Overview**: encryption mode **Full (strict)**.
- **SSL/TLS → Edge Certificates**: *Always Use HTTPS* on, *Minimum TLS* 1.2.
  Leave HSTS off there — the app already sends it.
- **Network**: *WebSockets* on (the default).
- **Turn off anything that rewrites pages** — the site's Content Security Policy
  blocks the scripts they inject, and they would break the "no third-party
  scripts" promise in the Privacy Policy:
  - *Scrape Shield → Email Address Obfuscation* (otherwise the contact email on
    the Terms and Privacy pages shows as "[email protected]"),
  - *Speed → Rocket Loader*,
  - *Web Analytics* automatic setup and *Zaraz*.
- **Caching**: the defaults are right (pages, API and files aren't cached). Don't
  add "Cache Everything" rules for `/api/*` or `/chat/*` — deleted files could
  then stay downloadable from Cloudflare's cache.
- Cloudflare's free plan allows 100 MB uploads and keeps idle WebSockets open
  for 100 s; CloudChat's 10 MB files and ~54 s pings fit within both.

## 8. Logs

```bash
journalctl -u cloudchat -f
```

Access logs replace room, file and secret IDs with `:id`. To keep logs short-lived,
set `MaxRetentionSec=7day` in `/etc/systemd/journald.conf` and run
`sudo systemctl restart systemd-journald`.

## Handling abuse reports

Moderation is done on the server with the same binary — there is no web admin
panel to attack. Run from `/opt/cloudchat` as root (so it can read `.env`), and
quote links, since they contain `?` and `#`:

```bash
cd /opt/cloudchat
sudo ./cloudchat admin room list                                              # every room: age, online, messages, files
sudo ./cloudchat admin room show   'https://chat.example.com/chat/?room=…'   # who's there, messages, files
sudo ./cloudchat admin room export 'https://chat.example.com/chat/?room=…' /root/evidence
sudo ./cloudchat admin room delete 'https://chat.example.com/chat/?room=…'   # asks for confirmation
sudo ./cloudchat admin secret delete 'https://chat.example.com/chat/secret/…'
```

- `room delete` removes the room, its messages and files at once, shows everyone
  in it "This session was closed for violating our Terms of Service", disconnects
  them, and makes the invite link stop working.
- **Act fast**: reported rooms delete themselves 5 minutes after everyone leaves,
  so export first if you need to keep evidence.
- **Child sexual abuse material**: don't open or forward it. Export the room (to
  preserve it), delete it, and report it to NCMEC's CyberTipline
  (https://report.cybertip.org). U.S. providers must report and preserve such
  material; check the current requirements with a lawyer.
- Secret notes are encrypted in the sender's browser — they can be deleted but
  not read, by you or anyone else without the link.

## Updating

```bash
sudo /opt/cloudchat/deploy/update.sh
```

It pulls, rebuilds, restarts and waits for `/healthz`. People in a chat see
"Reconnecting…" for a moment and carry on — nothing is lost, because rooms
survive 5 minutes with nobody connected.

## After going live

- Submit `https://your-domain/sitemap.xml` in Google Search Console.
- Behind Cloudflare: check that rate limits see real visitors —
  `journalctl -u cloudchat -n 20` should show visitors' IPs, not Cloudflare's or `127.0.0.1`.
- Check the security headers: https://securityheaders.com
