#!/bin/bash
set -e

# ==========================================================
# RunOdoo Installer — Self-Hosted PaaS for Odoo 17-20
# Supports Google Container-Optimized OS (COS) & Standard Linux
# ==========================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DATA_DIR="${RUNODOO_DATA_DIR:-/var/runodoo}"
PANEL_PORT="${RUNODOO_PORT:-3000}"
DEFAULT_PASS="${RUNODOO_DEFAULT_PASSWORD:-runodoo123}"
IMAGE_NAME="${RUNODOO_IMAGE:-runodoo:latest}"

echo "=========================================="
echo "   RunOdoo Control Panel Installer"
echo "=========================================="

# 1. Detect OS (Google COS vs Standard Linux)
IS_COS=false
OS_PRETTY="Linux"
if [ -f /etc/os-release ]; then
    . /etc/os-release
    OS_PRETTY="${PRETTY_NAME:-$ID}"
    if [ "${ID:-}" = "cos" ]; then
        IS_COS=true
    fi
fi

echo "Detected OS: ${OS_PRETTY} (Google COS: ${IS_COS})"

# 2. Ensure Docker is available
DOCKER_CMD="docker"
if [ "$IS_COS" = true ]; then
    if ! $DOCKER_CMD info >/dev/null 2>&1; then
        DOCKER_CMD="sudo docker"
    fi
else
    if ! command -v docker >/dev/null 2>&1; then
        echo "Docker not found. Installing Docker via get.docker.com..."
        curl -fsSL https://get.docker.com | sudo sh
        if command -v systemctl >/dev/null 2>&1; then
            sudo systemctl enable --now docker || true
        fi
    fi
    if ! $DOCKER_CMD info >/dev/null 2>&1; then
        DOCKER_CMD="sudo docker"
    fi
fi

# 2a. Check if Panel Port is available (default 3000)
is_port_in_use() {
    local port="$1"
    if command -v ss >/dev/null 2>&1; then
        if ss -tuln 2>/dev/null | grep -qE "[:.]${port}[[:space:]]"; then
            return 0
        fi
    elif command -v netstat >/dev/null 2>&1; then
        if netstat -tuln 2>/dev/null | grep -qE "[:.]${port}[[:space:]]"; then
            return 0
        fi
    elif (echo >/dev/tcp/127.0.0.1/"${port}") >/dev/null 2>&1; then
        return 0
    fi
    if $DOCKER_CMD ps --format '{{.Names}} {{.Ports}}' 2>/dev/null | grep -v '^runodoo ' | grep -qE ":${port}->"; then
        return 0
    fi
    return 1
}

RUNODOO_OWNS_PORT=false
if $DOCKER_CMD ps -a --format '{{.Names}}' 2>/dev/null | grep -qx "runodoo"; then
    if $DOCKER_CMD port runodoo 3000/tcp 2>/dev/null | grep -qE ":${PANEL_PORT}$"; then
        RUNODOO_OWNS_PORT=true
    fi
fi

if [ "$RUNODOO_OWNS_PORT" = false ] && is_port_in_use "${PANEL_PORT}"; then
    echo ""
    echo "WARNING: Port ${PANEL_PORT} is already in use by another service or container."
    if [ -r /dev/tty ]; then
        while true; do
            printf "Enter a new port for RunOdoo Panel (1-65535): " > /dev/tty
            IFS= read -r NEW_PORT < /dev/tty || NEW_PORT=""
            NEW_PORT="$(echo "$NEW_PORT" | tr -d '[:space:]')"
            if ! [[ "$NEW_PORT" =~ ^[0-9]+$ ]] || [ "$NEW_PORT" -lt 1 ] || [ "$NEW_PORT" -gt 65535 ]; then
                echo "Invalid port. Please enter a number between 1 and 65535." > /dev/tty
                continue
            fi
            if is_port_in_use "${NEW_PORT}"; then
                echo "Port ${NEW_PORT} is also in use. Please choose another port." > /dev/tty
                continue
            fi
            PANEL_PORT="${NEW_PORT}"
            echo "Using port ${PANEL_PORT} for RunOdoo Panel."
            break
        done
    else
        echo "Error: Port ${PANEL_PORT} is already in use and no interactive terminal (/dev/tty) is available." >&2
        echo "Re-run with: RUNODOO_PORT=<port> bash install-runodoo.sh" >&2
        exit 1
    fi
fi

# 2b. Configure global Docker DNS (fixes Alpine apk / image pull DNS issues)
if [ ! -f /etc/docker/daemon.json ] || ! sudo grep -q '"dns"' /etc/docker/daemon.json 2>/dev/null; then
    echo "Configuring global Docker DNS (1.1.1.1, 8.8.8.8)..."
    sudo mkdir -p /etc/docker
    echo '{"dns": ["1.1.1.1", "8.8.8.8"]}' | sudo tee /etc/docker/daemon.json >/dev/null
    if command -v systemctl >/dev/null 2>&1; then
        echo "Restarting Docker daemon to apply DNS settings..."
        sudo systemctl restart docker || true
        sleep 2
    fi
fi

# 3. Prepare host data directory
echo "Preparing data directory: ${DATA_DIR}"
sudo mkdir -p "${DATA_DIR}/config" "${DATA_DIR}/instances" "${DATA_DIR}/addons" \
              "${DATA_DIR}/backups" "${DATA_DIR}/nginx/conf.d" \
              "${DATA_DIR}/certbot/www" "${DATA_DIR}/certbot/conf" \
              "${DATA_DIR}/postgres-shared"

# 4. Create Docker network
$DOCKER_CMD network inspect runodoo-net >/dev/null 2>&1 || \
    $DOCKER_CMD network create --label runodoo.managed=true runodoo-net

# 5. Build RunOdoo image locally if Dockerfile is present
if [ -f "${SCRIPT_DIR}/Dockerfile" ]; then
    echo "Building RunOdoo Docker image (${IMAGE_NAME})..."
    $DOCKER_CMD build -t "${IMAGE_NAME}" "${SCRIPT_DIR}"
else
    echo "Pulling RunOdoo Docker image (${IMAGE_NAME})..."
    $DOCKER_CMD pull "${IMAGE_NAME}"
fi

# 6. Restart RunOdoo container
if $DOCKER_CMD ps -a --format '{{.Names}}' | grep -qx "runodoo"; then
    echo "Removing previous runodoo container..."
    $DOCKER_CMD rm -f runodoo >/dev/null
fi

echo "Starting RunOdoo on port ${PANEL_PORT}..."
$DOCKER_CMD run -d \
    --name runodoo \
    --restart unless-stopped \
    --network runodoo-net \
    --label runodoo.managed=true \
    --label runodoo.role=control-panel \
    -p "${PANEL_PORT}:3000" \
    -e "RUNODOO_PORT=3000" \
    -e "RUNODOO_DATA_DIR=/var/runodoo" \
    -e "RUNODOO_HOST_DATA_DIR=${DATA_DIR}" \
    -e "RUNODOO_DEFAULT_PASSWORD=${DEFAULT_PASS}" \
    -e "RUNODOO_NETWORK=runodoo-net" \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -v "${DATA_DIR}:/var/runodoo" \
    "${IMAGE_NAME}"

# 7. Clean up dangling images and build cache to free disk space
echo "Cleaning up unused Docker build cache & dangling images..."
$DOCKER_CMD image prune -f >/dev/null 2>&1 || true
$DOCKER_CMD builder prune -f >/dev/null 2>&1 || true

# 8. Open firewall ports if UFW is active (non-COS)
if command -v ufw >/dev/null 2>&1 && sudo ufw status | grep -q "Status: active"; then
    echo "Opening UFW ports 80, 443, and ${PANEL_PORT}..."
    sudo ufw allow 80/tcp >/dev/null 2>&1 || true
    sudo ufw allow 443/tcp >/dev/null 2>&1 || true
    sudo ufw allow "${PANEL_PORT}/tcp" >/dev/null 2>&1 || true
fi

SERVER_IP=$(hostname -I 2>/dev/null | awk '{print $1}')
[ -z "$SERVER_IP" ] && SERVER_IP="<YOUR-SERVER-IP>"

echo ""
echo "=========================================="
echo " RunOdoo is now running!"
echo "=========================================="
echo " Panel URL : http://${SERVER_IP}:${PANEL_PORT}"
echo " Password  : ${DEFAULT_PASS}"
echo ""
echo " Tip: Login to the panel, change your password,"
echo " and optionally configure a Root Domain + HTTPS"
echo " in Dashboard / Settings."
echo "=========================================="
