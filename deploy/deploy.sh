#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="${REPO_ROOT}/bin"
BIN_NAME="hips"
OUT_BIN="${BUILD_DIR}/${BIN_NAME}"
ENV_FILE="/etc/hips.env"
SERVICE_FILE="/etc/systemd/system/${BIN_NAME}.service"

mkdir -p "${BUILD_DIR}"

echo "============================================"
echo "  HIPS Deployment Script"
echo "============================================"

echo "[check] checking libvips dependency..."
if ! pkg-config --exists vips 2>/dev/null; then
  echo "[error] libvips not found. Please install it first:"
  echo "  Ubuntu/Debian: sudo apt install libvips-dev"
  echo "  CentOS/RHEL:   sudo yum install vips-devel"
  echo "  macOS:         brew install vips"
  exit 1
fi
echo "[check] libvips found: $(pkg-config --modversion vips)"

echo "[build] go build -o ${OUT_BIN} cmd/server/main.go"
cd "${REPO_ROOT}"
CGO_ENABLED=1 GO111MODULE=on go build -ldflags="-s -w" -o "${OUT_BIN}" cmd/server/main.go

echo "[install] sudo install -m 0755 ${OUT_BIN} /usr/local/bin/${BIN_NAME}"
sudo install -m 0755 "${OUT_BIN}" "/usr/local/bin/${BIN_NAME}"

echo "[install] sudo install -m 0644 deploy/${BIN_NAME}.service ${SERVICE_FILE}"
sudo install -m 0644 "${REPO_ROOT}/deploy/${BIN_NAME}.service" "${SERVICE_FILE}"

CACHE_DIR="/var/cache/hips"
echo "[init] creating cache directory: ${CACHE_DIR}"
sudo mkdir -p "${CACHE_DIR}"
sudo chown nobody:nogroup "${CACHE_DIR}" 2>/dev/null || sudo chown nobody:nobody "${CACHE_DIR}"

if [[ ! -f "${ENV_FILE}" ]]; then
  echo "[init] create ${ENV_FILE} (edit this file with your R2 credentials)"
  sudo tee "${ENV_FILE}" >/dev/null <<'EOF'
# ===========================================
# HIPS Configuration
# ===========================================

# ---- Required: Cloudflare R2 Storage ----
R2_ENDPOINT=""
R2_ACCESS_KEY=""
R2_SECRET_KEY=""
R2_BUCKET=""

# ---- Server ----
PORT=8080

# ---- Concurrency ----
# MAX_WORKERS=32
# MAX_QUEUE_SIZE=320
# TASK_TIMEOUT=30s
# ENABLE_ASYNC=true

# ---- libvips ----
# VIPS_CONCURRENCY=0
# VIPS_CACHE_SIZE=300
# VIPS_CACHE_MEM_MB=512

# ---- Cache: L1 Memory ----
CACHE_L1_ENABLED=true
CACHE_L1_MAX_MEMORY_MB=1024

# ---- Cache: L2 Redis ----
CACHE_L2_ENABLED=false
# REDIS_ADDR=localhost:6379
# REDIS_PASSWORD=
# REDIS_DB=0
# CACHE_L2_MAX_MEMORY_MB=3072

# ---- Cache: L3 Disk ----
CACHE_L3_ENABLED=true
CACHE_DISK_DIR=/var/cache/hips
CACHE_L3_MAX_DISK_GB=10

# ---- Third Party Providers (JSON array) ----
# THIRD_PARTY_PROVIDERS_JSON='[{"name":"example","allowed_hosts":["example.com"]}]'
EOF
  echo "[warn] Please edit ${ENV_FILE} with your R2 credentials before starting the service!"
else
  echo "[skip] ${ENV_FILE} already exists, skipping..."
fi

echo "[systemd] daemon-reload"
sudo systemctl daemon-reload

echo "[systemd] enable ${BIN_NAME} (will start on boot)"
sudo systemctl enable "${BIN_NAME}"

if grep -q '^R2_ENDPOINT=""' "${ENV_FILE}"; then
  echo ""
  echo "============================================"
  echo "[!] WARNING: R2 credentials not configured!"
  echo "============================================"
  echo "Please edit ${ENV_FILE} with your R2 credentials, then run:"
  echo "  sudo systemctl start ${BIN_NAME}"
  echo ""
else
  echo "[systemd] start/restart ${BIN_NAME} now"
  sudo systemctl restart "${BIN_NAME}"
  echo ""
  echo "============================================"
  echo "  Deployment Complete!"
  echo "============================================"
  echo "Service status: sudo systemctl status ${BIN_NAME}"
  echo "View logs:      sudo journalctl -u ${BIN_NAME} -f"
  echo "Config file:    ${ENV_FILE}"
fi

