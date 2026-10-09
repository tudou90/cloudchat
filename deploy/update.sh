#!/usr/bin/env bash
# Pull the latest code, rebuild and restart CloudChat. Run as root:
#   sudo /opt/cloudchat/deploy/update.sh
# Connected browsers reconnect on their own after the restart.
set -euo pipefail
cd /opt/cloudchat

git pull --ff-only
(cd frontend && npm ci --no-audit --no-fund && npm run build)
GO=$(command -v go || echo /usr/local/go/bin/go)
"$GO" build -o cloudchat.new ./cmd/server
mv cloudchat.new cloudchat

systemctl restart cloudchat
for i in $(seq 1 20); do
  if curl -fsS http://127.0.0.1:8080/healthz >/dev/null; then echo "CloudChat is up."; exit 0; fi
  sleep 0.5
done
echo "CloudChat did not become healthy — check: journalctl -u cloudchat -n 50" >&2
exit 1
