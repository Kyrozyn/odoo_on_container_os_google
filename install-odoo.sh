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
echo "Memindai modul Odoo dan subfoldernya..."
EXTRA_PATHS=$(find "${LOCAL_ADDONS_DIR}" -type f -name "__manifest__.py" -exec dirname {} + 2>/dev/null \
    | xargs -I {} dirname {} 2>/dev/null \
    | sort -u \
    | sed "s|${LOCAL_ADDONS_DIR}|/mnt/extra-addons|g" \
    | tr '\n' ',' \
    | sed 's/,$//')

FINAL_ADDONS_PATH="/usr/lib/python3/dist-packages/odoo/addons,/mnt/extra-addons"
if [ -n "$EXTRA_PATHS" ]; then
    FINAL_ADDONS_PATH="${FINAL_ADDONS_PATH},${EXTRA_PATHS}"
fi

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
echo "==> 1. Menyiapkan Compose Wrapper (Google COS)"
echo "=========================================="

compose_cmd() {
    docker run --rm \
        -v /var/run/docker.sock:/var/run/docker.sock \
        -v /var/odoo:/var/odoo \
        -v "${LOCAL_ADDONS_DIR}:${LOCAL_ADDONS_DIR}" \
        -w /var/odoo \
        docker/compose:latest "$@"
}

docker pull docker/compose:latest > /dev/null

echo "=========================================="
echo "==> 2. Menyiapkan Direktori & Permission"
echo "=========================================="
sudo mkdir -p /var/odoo/postgres_data
sudo mkdir -p /var/odoo/data
sudo mkdir -p /var/odoo/config
sudo mkdir -p /var/odoo/nginx_conf
sudo mkdir -p /var/odoo/letsencrypt
sudo mkdir -p /var/odoo/certbot_www

docker stop odoo_app odoo_db odoo_nginx odoo_certbot 2>/dev/null || true
docker rm odoo_app odoo_db odoo_nginx odoo_certbot 2>/dev/null || true

echo "=========================================="
echo "==> 3. Menyiapkan odoo.conf (Unlimited Timeout)"
echo "=========================================="
sudo tee /var/odoo/config/odoo.conf > /dev/null <<EOF
[options]
admin_passwd = admin
db_host = db
db_user = odoo
db_password = tAIre2vRTXDgRFSlqUy2kBFF
http_interface = 0.0.0.0
proxy_mode = True
addons_path = ${FINAL_ADDONS_PATH}
workers = ${ODOO_WORKERS}
limit_memory_soft = ${LIMIT_MEM_SOFT}
limit_memory_hard = ${LIMIT_MEM_HARD}

; Unlimited Timeout
limit_time_cpu = 0
limit_time_real = 0
max_cron_threads = 1
EOF

sudo chmod -R 777 /var/odoo

echo "=========================================="
echo "==> 4. Parameter Domain & Email"
echo "=========================================="
read -p "Masukkan nama domain (default: andreascloud.my.id): " ODOO_DOMAIN
ODOO_DOMAIN=${ODOO_DOMAIN:-andreascloud.my.id}

read -p "Masukkan email SSL (default: arr@e.email): " CERTBOT_EMAIL
CERTBOT_EMAIL=${CERTBOT_EMAIL:-arr@e.email}

sudo tee /var/odoo/nginx_conf/odoo.conf > /dev/null <<EOF
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

echo "=========================================="
echo "==> 5. Membuat docker-compose.yml"
echo "=========================================="
sudo tee /var/odoo/docker-compose.yml > /dev/null <<EOF
version: '3.8'

networks:
  odoo-net:
    driver: bridge

services:
  db:
    image: postgres:16
    container_name: odoo_db
    environment:
      - POSTGRES_DB=postgres
      - POSTGRES_PASSWORD=tAIre2vRTXDgRFSlqUy2kBFF
      - POSTGRES_USER=odoo
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

  odoo:
    image: odoo:latest
    container_name: odoo_app
    depends_on:
      - db
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
      - "80:80"
      - "443:443"
    volumes:
      - /var/odoo/nginx_conf:/etc/nginx/conf.d:ro
      - /var/odoo/letsencrypt:/etc/letsencrypt:ro
      - /var/odoo/certbot_www:/var/www/certbot:ro
    depends_on:
      - odoo
    networks:
      - odoo-net
    restart: always

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

echo "Menjalankan Nginx untuk verifikasi ACME..."
compose_cmd up -d nginx

echo "=========================================="
echo "==> 6. Menerbitkan Sertifikat SSL"
echo "=========================================="
if [ ! -f "/var/odoo/letsencrypt/live/${ODOO_DOMAIN}/fullchain.pem" ]; then
    echo "Meminta sertifikat Let's Encrypt..."
    docker run -it --rm \
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

echo "=========================================="
echo "==> 8. Menjalankan Seluruh Stack"
echo "=========================================="
compose_cmd up -d
docker exec odoo_nginx nginx -s reload

echo "=========================================="
echo "==> 9. Memasang Dependensi Python (pip)"
echo "=========================================="
echo "Menginstal openpyxl, ofxparse, dan qifparse ke dalam container Odoo..."
docker exec -u 0 odoo_app pip install --no-cache-dir --break-system-packages openpyxl ofxparse qifparse

echo "Merestart odoo_app agar dependensi Python aktif..."
docker restart odoo_app

echo ""
echo "=========================================="
echo "Instalasi selesai!"
echo "Dependensi 'openpyxl', 'ofxparse', dan 'qifparse' siap digunakan."
echo "Akses Odoo di: https://${ODOO_DOMAIN}"
echo "=========================================="
