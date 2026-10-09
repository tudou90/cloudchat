# Deploying CloudChat (Ubuntu + systemd)

This sets up CloudChat on one Linux server (Ubuntu 22.04/24.04) with Caddy for
HTTPS, Redis on the same machine, and CloudChat as a systemd service. Replace
`chat.example.com` with your domain everywhere.

```
Internet ──443──> Caddy (HTTPS) ──> 127.0.0.1:8080 CloudChat ──> 127.0.0.1:6379 Redis
```

## 1. Before you start

- A DNS **A** (and optionally **AAAA**) record for your domain pointing at the server.
- Ports **80** and **443** open (Caddy needs 80 to obtain the certificate).

```bash
sudo ufw allow OpenSSH && sudo ufw allow 80,443/tcp && sudo ufw enable
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

Must be right: `PUBLIC_URL=https://your-domain` (secure cookies, HSTS, SEO) and
`TRUSTED_PROXIES=127.0.0.1,::1` (otherwise every visitor shares one rate limit).

## 6. Start CloudChat

```bash
sudo cp deploy/cloudchat.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now cloudchat
curl http://127.0.0.1:8080/healthz      # → {"status":"ok"}
```

## 7. HTTPS with Caddy

```bash
sudo cp deploy/Caddyfile /etc/caddy/Caddyfile
sudo nano /etc/caddy/Caddyfile          # your domain
sudo systemctl reload caddy
curl https://chat.example.com/healthz   # → {"status":"ok"}
```

Prefer nginx? Use [`nginx.conf`](nginx.conf) with certbot instead of Caddy.

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
- Check the security headers: https://securityheaders.com
