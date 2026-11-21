#!/bin/bash
set -e

# Dapatkan lokasi folder repositori Git lokal
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOCAL_ADDONS_DIR="${SCRIPT_DIR}/addons"

echo "=========================================="
echo "==> 0. Deteksi Spesifikasi Sistem & Resource"
echo "=========================================="

TOTAL_RAM_MB=$(free -m | awk '/^Mem:/{print $2}')
CPU_CORES=$(nproc)

echo "Terdeteksi: ${CPU_CORES} vCPU | RAM: ${TOTAL_RAM_MB} MB"
echo "Direktori Addons Git: ${LOCAL_ADDONS_DIR}"

mkdir -p "${LOCAL_ADDONS_DIR}"
sudo chmod -R 777 "${LOCAL_ADDONS_DIR}"

# Pindai subfolder modul yang memuat __manifest__.py secara otomatis
echo "Memindai direktori modul Odoo..."
EXTRA_PATHS=$(find "${LOCAL_ADDONS_DIR}" -type f -name "__manifest__.py" 2>/dev/null | while read -r manifest; do
    mod_dir="$(dirname "$manifest")"
    parent_dir="$(dirname "$mod_dir")"
    echo "$parent_dir"
done | sort -u | sed "s|${LOCAL_ADDONS_DIR}|/mnt/extra-addons|g")

# Susun daftar addons_path dan hilangkan duplikasi
ADDONS_LIST="/usr/lib/python3/dist-packages/odoo/addons
/mnt/extra-addons"

if [ -n "$EXTRA_PATHS" ]; then
    ADDONS_LIST="${ADDONS_LIST}
${EXTRA_PATHS}"
fi

FINAL_ADDONS_PATH=$(echo "$ADDONS_LIST" | grep -v '^$' | sort -u | tr '\n' ',' | sed 's/,$//')
echo "Addons Path terpasang: ${FINAL_ADDONS_PATH}"

# Hitung konfigurasi PostgreSQL dinamis
PG_SHARED_BUFFERS="$((TOTAL_RAM_MB / 4))MB"
PG_EFFECTIVE_CACHE="$((TOTAL_RAM_MB / 2))MB"
PG_MAINTENANCE_WORK_MEM="$((TOTAL_RAM_MB / 16))MB"
[ "$((TOTAL_RAM_MB / 16))" -lt 32 ] && PG_MAINTENANCE_WORK_MEM="32MB"
PG_WORK_MEM="4MB"
PG_MAX_CONN="50"

# Odoo worker & memory limits untuk e2-micro / spek kecil
if [ "$TOTAL_RAM_MB" -le 1500 ]; then
    ODOO_WORKERS=0
    LIMIT_MEM_SOFT=671088640
    LIMIT_MEM_HARD=805306368
else
    ODOO_WORKERS=$(( (CPU_CORES * 2) + 1 ))
    [ "$ODOO_WORKERS" -gt 4 ] && ODOO_WORKERS=4
    LIMIT_MEM_SOFT=805306368
    LIMIT_MEM_HARD=1073741824
fi

echo "=========================================="
echo "==> 1. Deteksi OS & Menyiapkan Docker / Compose"
echo "=========================================="

IS_COS=false
OS_ID="unknown"
OS_PRETTY="Linux"
if [ -f /etc/os-release ]; then
    . /etc/os-release
    OS_ID="${ID:-unknown}"
    OS_PRETTY="${PRETTY_NAME:-$OS_ID}"
    if [ "$OS_ID" = "cos" ]; then
        IS_COS=true
    fi
fi

if [ "$IS_COS" = true ]; then
    echo "OS terdeteksi: ${OS_PRETTY} (Google Container-Optimized OS)"
    DOCKER_CMD="docker"
    if ! $DOCKER_CMD info >/dev/null 2>&1; then
        DOCKER_CMD="sudo docker"
    fi

    echo "Menyiapkan Compose Wrapper (docker/compose:latest)..."
    $DOCKER_CMD pull docker/compose:latest > /dev/null
    compose_cmd() {
        $DOCKER_CMD run --rm \
            -v /var/run/docker.sock:/var/run/docker.sock \
            -v /var/odoo:/var/odoo \
            -v "${LOCAL_ADDONS_DIR}:${LOCAL_ADDONS_DIR}" \
            -w /var/odoo \
            docker/compose:latest "$@"
    }
else
    echo "OS terdeteksi: ${OS_PRETTY} (Non-COS)"
    if ! command -v docker >/dev/null 2>&1; then
        echo "Docker belum terpasang. Menginstall Docker via get.docker.com..."
        if ! command -v curl >/dev/null 2>&1; then
            if command -v apt-get >/dev/null 2>&1; then
                sudo apt-get update && sudo apt-get install -y curl
            elif command -v dnf >/dev/null 2>&1; then
                sudo dnf install -y curl
            elif command -v yum >/dev/null 2>&1; then
                sudo yum install -y curl
            else
                echo "Error: 'curl' tidak ditemukan dan package manager tidak dikenali."
                exit 1
            fi
        fi
        curl -fsSL https://get.docker.com | sudo sh
        if command -v systemctl >/dev/null 2>&1; then
            sudo systemctl enable --now docker || true
        fi
    else
        echo "Docker sudah terpasang."
    fi

    if [ "$(id -u)" -ne 0 ] && ! id -nG "$USER" | grep -qw docker; then
        echo "Menambahkan user '${USER}' ke grup 'docker'..."
        sudo usermod -aG docker "$USER" || true
    fi

    if docker info >/dev/null 2>&1; then
        DOCKER_CMD="docker"
    else
        DOCKER_CMD="sudo docker"
    fi

    if $DOCKER_CMD compose version >/dev/null 2>&1; then
        compose_cmd() {
            (cd /var/odoo && $DOCKER_CMD compose "$@")
        }
    elif command -v docker-compose >/dev/null 2>&1; then
        DOCKER_COMPOSE_BIN="docker-compose"
        if [ "$DOCKER_CMD" = "sudo docker" ]; then
            DOCKER_COMPOSE_BIN="sudo docker-compose"
        fi
        compose_cmd() {
            (cd /var/odoo && $DOCKER_COMPOSE_BIN "$@")
        }
    else
        echo "Plugin 'docker compose' tidak ditemukan. Menginstall via get.docker.com..."
        curl -fsSL https://get.docker.com | sudo sh
        compose_cmd() {
            (cd /var/odoo && $DOCKER_CMD compose "$@")
        }
    fi
fi

echo "=========================================="
echo "==> 1b. Deteksi PostgreSQL yang Sedang Berjalan"
echo "=========================================="

USE_NEW_PG=true
EXISTING_PG_TYPE=""
EXISTING_PG_CONTAINER=""
PG_HOST="db"
PG_PORT="5432"
PG_USER="odoo"
PG_PASS="tAIre2vRTXDgRFSlqUy2kBFF"

DETECTED_PG_TYPES=()
DETECTED_PG_NAMES=()
DETECTED_PG_DESCS=()
DETECTED_PG_PORTS=()

# 1) Deteksi container Docker PostgreSQL yang sedang berjalan
while IFS='|' read -r c_name c_image c_ports; do
    [ -z "$c_name" ] && continue
    if echo "$c_image" | grep -qiE '(^|/)(postgres|postgresql|postgis|timescaledb|pgvector)' || echo "$c_ports" | grep -qE '5432/tcp'; then
        DETECTED_PG_TYPES+=("container")
        DETECTED_PG_NAMES+=("$c_name")
        DETECTED_PG_DESCS+=("Docker container '${c_name}' (image: ${c_image}${c_ports:+, ports: ${c_ports}})")
        DETECTED_PG_PORTS+=("5432")
    fi
done < <($DOCKER_CMD ps --format '{{.Names}}|{{.Image}}|{{.Ports}}' 2>/dev/null || true)

# 2) Deteksi service/proses PostgreSQL di host (di luar container)
HOST_PG_FOUND=false
HOST_PG_DESC=""
HOST_PG_PORT="5432"
HOST_PG_LISTEN_LOCAL_ONLY=false

if command -v systemctl >/dev/null 2>&1; then
    PG_SYSTEMD_UNITS=$(systemctl list-units --type=service --state=running --no-legend 'postgresql*' 2>/dev/null | awk '{print $1}' | tr '\n' ' ' | sed 's/ *$//' || true)
    if [ -n "$PG_SYSTEMD_UNITS" ]; then
        HOST_PG_FOUND=true
        HOST_PG_DESC="Host systemd service (${PG_SYSTEMD_UNITS})"
    fi
fi

if [ "$HOST_PG_FOUND" = false ] && command -v pgrep >/dev/null 2>&1; then
    for pid in $(pgrep -x postgres 2>/dev/null || true); do
        if [ -r "/proc/${pid}/cgroup" ] && ! grep -qE 'docker|containerd|kubepods|lxc|podman' "/proc/${pid}/cgroup" 2>/dev/null; then
            HOST_PG_FOUND=true
            HOST_PG_DESC="Host PostgreSQL process (PID: ${pid})"
            break
        fi
    done
fi

if command -v ss >/dev/null 2>&1; then
    HOST_PG_SOCKETS=$(sudo ss -tlnp 2>/dev/null | grep -E 'postgres|postmaster' | grep -v 'docker-proxy' || true)
    if [ -z "$HOST_PG_SOCKETS" ]; then
        HOST_PG_SOCKETS=$(ss -tln 2>/dev/null | grep -E ':5432\b' || true)
        if [ -n "$HOST_PG_SOCKETS" ] && $DOCKER_CMD ps --format '{{.Ports}}' 2>/dev/null | grep -qE ':5432->'; then
            HOST_PG_SOCKETS=""
        fi
    fi
    if [ -n "$HOST_PG_SOCKETS" ]; then
        DETECTED_PORT=$(echo "$HOST_PG_SOCKETS" | awk '{print $4}' | sed -E 's/.*:([0-9]+)$/\1/' | head -n1 || true)
        if [[ "$DETECTED_PORT" =~ ^[0-9]+$ ]]; then
            HOST_PG_PORT="$DETECTED_PORT"
        fi
        if [ "$HOST_PG_FOUND" = false ]; then
            HOST_PG_FOUND=true
            HOST_PG_DESC="Host PostgreSQL listener"
        fi
        if ! echo "$HOST_PG_SOCKETS" | awk '{print $4}' | grep -qE '(0\.0\.0\.0|\*|\[::\]):'; then
            HOST_PG_LISTEN_LOCAL_ONLY=true
        fi
    fi
fi

if [ "$HOST_PG_FOUND" = true ]; then
    DETECTED_PG_TYPES+=("host")
    DETECTED_PG_NAMES+=("host")
    DETECTED_PG_DESCS+=("${HOST_PG_DESC} (port: ${HOST_PG_PORT})")
    DETECTED_PG_PORTS+=("$HOST_PG_PORT")
fi

if [ "${#DETECTED_PG_TYPES[@]}" -gt 0 ]; then
    echo "Terdeteksi PostgreSQL yang sedang berjalan:"
    for idx in "${!DETECTED_PG_DESCS[@]}"; do
        echo "  [$((idx + 1))] ${DETECTED_PG_DESCS[$idx]}"
    done
    echo ""
    read -p "Apakah Anda ingin membuat container PostgreSQL baru (odoo_db)? [y/N]: " CONFIRM_NEW_PG
    if [[ "$CONFIRM_NEW_PG" =~ ^[Yy]$ ]]; then
        USE_NEW_PG=true
        echo "Pilihan: Membuat container PostgreSQL baru (odoo_db)."
    else
        USE_NEW_PG=false
        SELECTED_IDX=0
        if [ "${#DETECTED_PG_TYPES[@]}" -gt 1 ]; then
            while true; do
                read -p "Pilih nomor PostgreSQL yang akan digunakan [1-${#DETECTED_PG_TYPES[@]}] (default: 1): " PG_CHOICE
                PG_CHOICE=${PG_CHOICE:-1}
                if [[ "$PG_CHOICE" =~ ^[0-9]+$ ]] && [ "$PG_CHOICE" -ge 1 ] && [ "$PG_CHOICE" -le "${#DETECTED_PG_TYPES[@]}" ]; then
                    SELECTED_IDX=$((PG_CHOICE - 1))
                    break
                fi
                echo "Pilihan tidak valid. Masukkan angka 1 sampai ${#DETECTED_PG_TYPES[@]}."
            done
        fi

        EXISTING_PG_TYPE="${DETECTED_PG_TYPES[$SELECTED_IDX]}"
        EXISTING_PG_CONTAINER="${DETECTED_PG_NAMES[$SELECTED_IDX]}"
        DEFAULT_PG_PORT="${DETECTED_PG_PORTS[$SELECTED_IDX]}"

        if [ "$EXISTING_PG_TYPE" = "container" ]; then
            DEFAULT_PG_HOST="${EXISTING_PG_CONTAINER}"
        else
            DEFAULT_PG_HOST="host.docker.internal"
            if [ "$HOST_PG_LISTEN_LOCAL_ONLY" = true ]; then
                echo ""
                echo "PERINGATAN: PostgreSQL host tampak hanya listen di localhost (127.0.0.1/::1)."
                echo "Agar container Odoo dapat terhubung melalui '${DEFAULT_PG_HOST}', pastikan:"
                echo "  - postgresql.conf mengatur listen_addresses = '*' (atau IP bridge Docker)"
                echo "  - pg_hba.conf mengizinkan koneksi dari subnet Docker (mis. 172.16.0.0/12)"
                echo ""
            fi
        fi

        echo "Menggunakan PostgreSQL yang sudah ada: ${DETECTED_PG_DESCS[$SELECTED_IDX]}"
        read -p "Host PostgreSQL untuk Odoo (default: ${DEFAULT_PG_HOST}): " INPUT_PG_HOST
        PG_HOST=${INPUT_PG_HOST:-$DEFAULT_PG_HOST}

        while true; do
            read -p "Port PostgreSQL (default: ${DEFAULT_PG_PORT}): " INPUT_PG_PORT
            PG_PORT=${INPUT_PG_PORT:-$DEFAULT_PG_PORT}
            if [[ "$PG_PORT" =~ ^[0-9]+$ ]] && [ "$PG_PORT" -ge 1 ] && [ "$PG_PORT" -le 65535 ]; then
                break
            fi
            echo "Port tidak valid. Masukkan angka 1-65535."
        done

        read -p "User PostgreSQL (default: odoo): " INPUT_PG_USER
        PG_USER=${INPUT_PG_USER:-odoo}

        read -s -p "Password PostgreSQL (kosongkan untuk default script): " INPUT_PG_PASS
        echo ""
        PG_PASS=${INPUT_PG_PASS:-tAIre2vRTXDgRFSlqUy2kBFF}
    fi
else
    echo "Tidak ada PostgreSQL yang sedang berjalan terdeteksi. Akan membuat container PostgreSQL baru (odoo_db)."
fi

echo "=========================================="
echo "==> 2. Menyiapkan Direktori & Permission"
echo "=========================================="
if [ "$USE_NEW_PG" = true ]; then
    sudo mkdir -p /var/odoo/postgres_data
fi
sudo mkdir -p /var/odoo/data
sudo mkdir -p /var/odoo/config
sudo mkdir -p /var/odoo/nginx_conf
sudo mkdir -p /var/odoo/letsencrypt
sudo mkdir -p /var/odoo/certbot_www

$DOCKER_CMD stop odoo_app odoo_nginx odoo_certbot 2>/dev/null || true
$DOCKER_CMD rm odoo_app odoo_nginx odoo_certbot 2>/dev/null || true
if [ "$USE_NEW_PG" = true ]; then
    $DOCKER_CMD stop odoo_db 2>/dev/null || true
    $DOCKER_CMD rm odoo_db 2>/dev/null || true
fi

echo "=========================================="
echo "==> 3. Menyiapkan odoo.conf (Unlimited Timeout)"
echo "=========================================="
sudo tee /var/odoo/config/odoo.conf > /dev/null <<EOF
[options]
admin_passwd = admin
db_host = ${PG_HOST}
db_port = ${PG_PORT}
db_user = ${PG_USER}
db_password = ${PG_PASS}
http_interface = 0.0.0.0
proxy_mode = True
addons_path = ${FINAL_ADDONS_PATH}
workers = ${ODOO_WORKERS}
limit_memory_soft = ${LIMIT_MEM_SOFT}
limit_memory_hard = ${LIMIT_MEM_HARD}

; Timeout Maksimal (10 jam, selaras dengan Nginx 36000s; nilai 0 di PreforkServer berarti timeout 0 detik!)
limit_time_cpu = 36000
limit_time_real = 36000
limit_time_real_cron = 36000
max_cron_threads = 1
EOF

sudo chmod -R 777 /var/odoo

echo "=========================================="
echo "==> 4. Mode Akses (Lokal / Domain & SSL)"
echo "=========================================="
echo "Pilih mode akses Odoo:"
echo "  [1] Domain + SSL Let's Encrypt (HTTPS)"
echo "  [2] Lokal / IP / Custom Port (HTTP, tanpa sertifikat SSL)"
while true; do
    read -p "Pilih mode [1/2] (default: 1): " ACCESS_MODE_CHOICE
    ACCESS_MODE_CHOICE=${ACCESS_MODE_CHOICE:-1}
    if [ "$ACCESS_MODE_CHOICE" = "1" ] || [ "$ACCESS_MODE_CHOICE" = "2" ]; then
        break
    fi
    echo "Pilihan tidak valid. Masukkan 1 atau 2."
done

USE_DOMAIN=true
ODOO_DOMAIN=""
CERTBOT_EMAIL=""
LOCAL_PORT="8069"

if [ "$ACCESS_MODE_CHOICE" = "1" ]; then
    USE_DOMAIN=true
    read -p "Masukkan nama domain (default: andreascloud.my.id): " ODOO_DOMAIN
    ODOO_DOMAIN=${ODOO_DOMAIN:-andreascloud.my.id}

    read -p "Masukkan email SSL (default: arr@e.email): " CERTBOT_EMAIL
    CERTBOT_EMAIL=${CERTBOT_EMAIL:-arr@e.email}

    sudo tee /var/odoo/nginx_conf/odoo.conf > /dev/null <<EOF
server {
    listen 80 default_server;
    server_name _;
    return 444;
}

server {
    listen 80;
    server_name ${ODOO_DOMAIN};

    location /.well-known/acme-challenge/ {
        root /var/www/certbot;
    }

    location / {
        return 200 'Persiapan SSL...';
        add_header Content-Type text/plain;
    }
}
EOF
else
    USE_DOMAIN=false
    while true; do
        read -p "Masukkan port untuk akses Odoo lokal (default: 8069): " INPUT_LOCAL_PORT
        LOCAL_PORT=${INPUT_LOCAL_PORT:-8069}
        if [[ "$LOCAL_PORT" =~ ^[0-9]+$ ]] && [ "$LOCAL_PORT" -ge 1 ] && [ "$LOCAL_PORT" -le 65535 ]; then
            break
        fi
        echo "Port tidak valid. Masukkan angka 1-65535."
    done
    echo "Mode lokal dipilih: Odoo akan listen pada port ${LOCAL_PORT} (HTTP tanpa SSL)."
fi

echo "=========================================="
echo "==> 5. Membuat docker-compose.yml"
echo "=========================================="
$DOCKER_CMD network create odoo-net >/dev/null 2>&1 || true
if [ "$USE_NEW_PG" = false ] && [ "$EXISTING_PG_TYPE" = "container" ]; then
    $DOCKER_CMD network connect odoo-net "$EXISTING_PG_CONTAINER" >/dev/null 2>&1 || true
fi

{
    cat <<EOF
version: '3.8'

networks:
  odoo-net:
    external: true

services:
EOF
    if [ "$USE_NEW_PG" = true ]; then
        cat <<EOF
  db:
    image: postgres:16
    container_name: odoo_db
    environment:
      - POSTGRES_DB=postgres
      - POSTGRES_PASSWORD=${PG_PASS}
      - POSTGRES_USER=${PG_USER}
      - PGDATA=/var/lib/postgresql/data/pgdata
    command:
      - "postgres"
      - "-c"
      - "shared_buffers=${PG_SHARED_BUFFERS}"
      - "-c"
      - "effective_cache_size=${PG_EFFECTIVE_CACHE}"
      - "-c"
      - "maintenance_work_mem=${PG_MAINTENANCE_WORK_MEM}"
      - "-c"
      - "work_mem=${PG_WORK_MEM}"
      - "-c"
      - "max_connections=${PG_MAX_CONN}"
    volumes:
      - /var/odoo/postgres_data:/var/lib/postgresql/data/pgdata
    networks:
      - odoo-net
    restart: always

EOF
    fi
    cat <<EOF
  odoo:
    image: odoo:latest
    container_name: odoo_app
EOF
    if [ "$USE_NEW_PG" = true ]; then
        cat <<EOF
    depends_on:
      - db
EOF
    else
        cat <<EOF
    extra_hosts:
      - "host.docker.internal:host-gateway"
EOF
    fi
    cat <<EOF
    volumes:
      - /var/odoo/data:/var/lib/odoo
      - /var/odoo/config/odoo.conf:/etc/odoo/odoo.conf
      - ${LOCAL_ADDONS_DIR}:/mnt/extra-addons
    networks:
      - odoo-net
    restart: always

  nginx:
    image: nginx:alpine
    container_name: odoo_nginx
    ports:
EOF
    if [ "$USE_DOMAIN" = true ]; then
        cat <<EOF
      - "80:80"
      - "443:443"
    volumes:
      - /var/odoo/nginx_conf:/etc/nginx/conf.d:ro
      - /var/odoo/letsencrypt:/etc/letsencrypt:ro
      - /var/odoo/certbot_www:/var/www/certbot:ro
EOF
    else
        cat <<EOF
      - "${LOCAL_PORT}:80"
    volumes:
      - /var/odoo/nginx_conf:/etc/nginx/conf.d:ro
EOF
    fi
    cat <<EOF
    depends_on:
      - odoo
    networks:
      - odoo-net
    restart: always
EOF
    if [ "$USE_DOMAIN" = true ]; then
        cat <<EOF

  certbot:
    image: certbot/certbot
    container_name: odoo_certbot
    volumes:
      - /var/odoo/letsencrypt:/etc/letsencrypt
      - /var/odoo/certbot_www:/var/www/certbot
    entrypoint: "/bin/sh -c 'trap exit TERM; while :; do certbot renew; sleep 12h & wait \$\${!}; done;'"
    networks:
      - odoo-net
EOF
    fi
} | sudo tee /var/odoo/docker-compose.yml > /dev/null

if [ "$USE_DOMAIN" = true ]; then
    echo "Menjalankan Nginx untuk verifikasi ACME..."
    compose_cmd up -d nginx

    echo "=========================================="
    echo "==> 6. Menerbitkan Sertifikat SSL"
    echo "=========================================="
    if [ ! -f "/var/odoo/letsencrypt/live/${ODOO_DOMAIN}/fullchain.pem" ]; then
        echo "Meminta sertifikat Let's Encrypt..."
        $DOCKER_CMD run -it --rm \
          -v /var/odoo/letsencrypt:/etc/letsencrypt \
          -v /var/odoo/certbot_www:/var/www/certbot \
          certbot/certbot certonly --webroot \
          --webroot-path=/var/www/certbot \
          --email "${CERTBOT_EMAIL}" \
          --agree-tos --no-eff-email \
          -d "${ODOO_DOMAIN}"
    else
        echo "Sertifikat SSL untuk ${ODOO_DOMAIN} sudah tersedia."
    fi

    echo "=========================================="
    echo "==> 7. Mengaktifkan Nginx HTTPS (Timeout Maksimal)"
    echo "=========================================="
    sudo tee /var/odoo/nginx_conf/odoo.conf > /dev/null <<EOF
upstream odoo {
    server odoo:8069;
}

upstream odoo-chat {
    server odoo:8072;
}

server {
    listen 80 default_server;
    server_name _;
    return 444;
}

server {
    listen 443 ssl default_server;
    server_name _;
    ssl_reject_handshake on;
}

server {
    listen 80;
    server_name ${ODOO_DOMAIN};

    location /.well-known/acme-challenge/ {
        root /var/www/certbot;
    }

    location / {
        return 301 https://\$host\$request_uri;
    }
}

server {
    listen 443 ssl;
    server_name ${ODOO_DOMAIN};

    ssl_certificate /etc/letsencrypt/live/${ODOO_DOMAIN}/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/${ODOO_DOMAIN}/privkey.pem;

    proxy_read_timeout 36000s;
    proxy_connect_timeout 36000s;
    proxy_send_timeout 36000s;
    send_timeout 36000s;
    keepalive_timeout 36000s;

    access_log /var/log/nginx/odoo.access.log;
    error_log /var/log/nginx/odoo.error.log;

    location /longpolling {
        proxy_pass http://odoo-chat;
    }

    location / {
        proxy_pass http://odoo;
        proxy_next_upstream error timeout invalid_header http_500 http_502 http_503 http_504;
        proxy_redirect off;

        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }

    client_max_body_size 256M;
    proxy_buffers 16 1024k;
    proxy_buffer_size 1024k;

    gzip_types text/css text/less text/plain text/xml application/xml application/json application/javascript;
    gzip on;
}
EOF
else
    echo "=========================================="
    echo "==> 6. Sertifikat SSL (Dilewati - Mode Lokal)"
    echo "=========================================="
    echo "Mode lokal aktif: tidak perlu menerbitkan sertifikat SSL."

    echo "=========================================="
    echo "==> 7. Mengaktifkan Nginx HTTP Lokal (Port ${LOCAL_PORT})"
    echo "=========================================="
    sudo tee /var/odoo/nginx_conf/odoo.conf > /dev/null <<EOF
upstream odoo {
    server odoo:8069;
}

upstream odoo-chat {
    server odoo:8072;
}

server {
    listen 80 default_server;
    server_name _;

    proxy_read_timeout 36000s;
    proxy_connect_timeout 36000s;
    proxy_send_timeout 36000s;
    send_timeout 36000s;
    keepalive_timeout 36000s;

    access_log /var/log/nginx/odoo.access.log;
    error_log /var/log/nginx/odoo.error.log;

    location /longpolling {
        proxy_pass http://odoo-chat;
    }

    location / {
        proxy_pass http://odoo;
        proxy_next_upstream error timeout invalid_header http_500 http_502 http_503 http_504;
        proxy_redirect off;

        proxy_set_header Host \$http_host;
        proxy_set_header X-Forwarded-Host \$http_host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }

    client_max_body_size 256M;
    proxy_buffers 16 1024k;
    proxy_buffer_size 1024k;

    gzip_types text/css text/less text/plain text/xml application/xml application/json application/javascript;
    gzip on;
}
EOF
fi

echo "=========================================="
echo "==> 8. Menjalankan Seluruh Stack"
echo "=========================================="
compose_cmd up -d
if [ "$USE_DOMAIN" = true ]; then
    $DOCKER_CMD exec odoo_nginx nginx -s reload
fi

echo "=========================================="
echo "==> 9. Memasang Dependensi Python (pip)"
echo "=========================================="
echo "Menginstal openpyxl, ofxparse, dan qifparse ke dalam container Odoo..."
$DOCKER_CMD exec -u 0 odoo_app pip install --no-cache-dir --break-system-packages openpyxl ofxparse qifparse

echo "=========================================="
echo "==> 10. Database & Update Modul Odoo"
echo "=========================================="
run_pg_isready() {
    if [ "$USE_NEW_PG" = true ]; then
        $DOCKER_CMD exec odoo_db pg_isready -U "${PG_USER}" -d postgres
    elif $DOCKER_CMD exec odoo_app sh -c 'command -v pg_isready >/dev/null 2>&1'; then
        $DOCKER_CMD exec odoo_app pg_isready -h "${PG_HOST}" -p "${PG_PORT}" -U "${PG_USER}" -d postgres
    else
        $DOCKER_CMD run --rm --network odoo-net --add-host=host.docker.internal:host-gateway postgres:16 pg_isready -h "${PG_HOST}" -p "${PG_PORT}" -U "${PG_USER}" -d postgres
    fi
}

run_psql() {
    if [ "$USE_NEW_PG" = true ]; then
        $DOCKER_CMD exec -e PGPASSWORD="${PG_PASS}" odoo_db psql -U "${PG_USER}" "$@"
    elif $DOCKER_CMD exec odoo_app sh -c 'command -v psql >/dev/null 2>&1'; then
        $DOCKER_CMD exec -e PGPASSWORD="${PG_PASS}" odoo_app psql -h "${PG_HOST}" -p "${PG_PORT}" -U "${PG_USER}" "$@"
    else
        $DOCKER_CMD run --rm --network odoo-net --add-host=host.docker.internal:host-gateway -e PGPASSWORD="${PG_PASS}" postgres:16 psql -h "${PG_HOST}" -p "${PG_PORT}" -U "${PG_USER}" "$@"
    fi
}

echo "Menunggu PostgreSQL siap..."
PG_READY=false
for i in $(seq 1 30); do
    if run_pg_isready >/dev/null 2>&1; then
        PG_READY=true
        break
    fi
    sleep 1
done

UPDATED_COUNT=0
DB_INSTALLED=0
NEW_DB_NAME=""

if [ "$PG_READY" = true ] && [ "$(run_psql -d postgres -tAc "SELECT 1;" 2>/dev/null || true)" = "1" ]; then
    echo "Mendeteksi database Odoo aktif di PostgreSQL..."
    DB_LIST=$(run_psql -d postgres -tAc "SELECT datname FROM pg_database WHERE datistemplate = false AND datname NOT IN ('postgres');" 2>/dev/null || true)

    ODOO_DBS=""
    if [ -n "$DB_LIST" ]; then
        for DB in $DB_LIST; do
            IS_ODOO_DB=$(run_psql -d "$DB" -tAc "SELECT 1 FROM information_schema.tables WHERE table_name = 'ir_module_module';" 2>/dev/null || true)
            if [ "$IS_ODOO_DB" = "1" ]; then
                ODOO_DBS="${ODOO_DBS}${ODOO_DBS:+ }${DB}"
            fi
        done
    fi

    if [ -n "$ODOO_DBS" ]; then
        echo "Database Odoo terdeteksi: ${ODOO_DBS}"
        read -p "Apakah Anda ingin update semua modul Odoo (-u all)? [y/N]: " CONFIRM_UPDATE
        if [[ "$CONFIRM_UPDATE" =~ ^[Yy]$ ]]; then
            for DB in $ODOO_DBS; do
                echo "------------------------------------------"
                echo "Memproses database: '${DB}'"

                # Deteksi modul yang nyangkut (status to upgrade / to install / to remove)
                STUCK_MODULES=$(run_psql -d "$DB" -tAc "SELECT name FROM ir_module_module WHERE state IN ('to upgrade', 'to install', 'to remove');" 2>/dev/null || true)
                if [ -n "$STUCK_MODULES" ]; then
                    echo "Ditemukan modul menggantung (stuck) pada database '${DB}':"
                    echo "${STUCK_MODULES}"
                fi

                echo "==> Menjalankan update semua modul (-u all) pada database: '${DB}'..."
                $DOCKER_CMD exec -u odoo odoo_app odoo -c /etc/odoo/odoo.conf -d "${DB}" -u all --workers=0 --stop-after-init || true

                # Cek kembali jika masih ada modul yang nyangkut setelah proses upgrade
                STUCK_AFTER=$(run_psql -d "$DB" -tAc "SELECT name FROM ir_module_module WHERE state IN ('to upgrade', 'to install', 'to remove');" 2>/dev/null || true)
                if [ -n "$STUCK_AFTER" ]; then
                    echo "Memulihkan status modul yang masih menggantung: ${STUCK_AFTER}"
                    run_psql -d "$DB" -c "UPDATE ir_module_module SET state = 'installed' WHERE state = 'to upgrade';"
                    run_psql -d "$DB" -c "UPDATE ir_module_module SET state = 'uninstalled' WHERE state = 'to install';"
                fi

                UPDATED_COUNT=$((UPDATED_COUNT + 1))
            done
            echo "Semua database (${UPDATED_COUNT}) telah selesai diperbarui."
        else
            echo "Melewati update modul Odoo."
        fi
    else
        echo "Belum ada database Odoo yang terinstall."
        read -p "Apakah Anda ingin menginstall database Odoo baru? [y/N]: " CONFIRM_INSTALL_DB
        if [[ "$CONFIRM_INSTALL_DB" =~ ^[Yy]$ ]]; then
            while true; do
                read -p "Masukkan nama database baru (default: odoo): " NEW_DB_NAME
                NEW_DB_NAME=${NEW_DB_NAME:-odoo}
                if [[ "$NEW_DB_NAME" =~ ^[a-zA-Z][a-zA-Z0-9_-]*$ ]]; then
                    break
                fi
                echo "Nama database tidak valid. Awali dengan huruf, lalu gunakan huruf, angka, '_' atau '-'."
            done

            read -p "Install tanpa data demo (--without-demo=all)? [Y/n]: " WITHOUT_DEMO
            ODOO_INIT_ARGS=(-c /etc/odoo/odoo.conf -d "${NEW_DB_NAME}" -i base --workers=0 --stop-after-init)
            if [[ ! "$WITHOUT_DEMO" =~ ^[Nn]$ ]]; then
                ODOO_INIT_ARGS+=(--without-demo=all)
            fi

            echo "==> Menginstall database Odoo '${NEW_DB_NAME}' (modul base)..."
            $DOCKER_CMD exec -u odoo odoo_app odoo "${ODOO_INIT_ARGS[@]}"
            DB_INSTALLED=1
            echo "Database '${NEW_DB_NAME}' berhasil diinstall."
        else
            echo "Melewati instalasi database Odoo."
        fi
    fi
else
    echo "PERINGATAN: Tidak dapat terhubung ke PostgreSQL (${PG_HOST}:${PG_PORT}) dengan user '${PG_USER}'."
    echo "Melewati deteksi & inisialisasi database. Pastikan host, port, user, password, dan akses jaringan PostgreSQL sudah benar."
fi

echo "Merestart odoo_app agar seluruh dependensi & modul aktif..."
$DOCKER_CMD restart odoo_app

echo ""
echo "=========================================="
echo "Proses selesai!"
echo "Dependensi 'openpyxl', 'ofxparse', dan 'qifparse' siap digunakan."
if [ "$UPDATED_COUNT" -gt 0 ]; then
    echo "Modul Odoo telah diperbarui (-u all) pada ${UPDATED_COUNT} database."
fi
if [ "$DB_INSTALLED" -eq 1 ]; then
    echo "Database Odoo '${NEW_DB_NAME}' telah berhasil diinstall (login default: admin / admin)."
fi
if [ "$USE_DOMAIN" = true ]; then
    echo "Akses Odoo di: https://${ODOO_DOMAIN}"
else
    echo "Akses Odoo di: http://localhost:${LOCAL_PORT} (atau http://<IP-SERVER>:${LOCAL_PORT})"
fi
echo "=========================================="
