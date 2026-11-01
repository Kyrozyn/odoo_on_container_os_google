#!/bin/bash
set -e

echo "=========================================="
echo "==> 0. Deteksi Spesifikasi Sistem & Resource"
echo "=========================================="

# Baca total RAM dalam Megabytes (MB)
TOTAL_RAM_MB=$(free -m | awk '/^Mem:/{print $2}')
# Baca jumlah vCPU
CPU_CORES=$(nproc)

echo "Terdeteksi: ${CPU_CORES} vCPU | RAM: ${TOTAL_RAM_MB} MB"

# Hitung kalkulasi PostgreSQL dinamis berbasis RAM
PG_SHARED_BUFFERS="$((TOTAL_RAM_MB / 4))MB"
PG_EFFECTIVE_CACHE="$((TOTAL_RAM_MB / 2))MB"
PG_MAINTENANCE_WORK_MEM="$((TOTAL_RAM_MB / 16))MB"
[ "$((TOTAL_RAM_MB / 16))" -lt 32 ] && PG_MAINTENANCE_WORK_MEM="32MB"
PG_WORK_MEM="4MB"
PG_MAX_CONN="50"

# Hitung kalkulasi Odoo Workers & RAM Limits
if [ "$TOTAL_RAM_MB" -le 1500 ]; then
    # e2-micro (1GB RAM) mode hemat (threaded)
    ODOO_WORKERS=0
    LIMIT_MEM_SOFT=671088640   # 640 MB
    LIMIT_MEM_HARD=805306368   # 768 MB
else
    # Untuk VM dengan RAM > 1.5GB
    ODOO_WORKERS=$(( (CPU_CORES * 2) + 1 ))
    [ "$ODOO_WORKERS" -gt 4 ] && ODOO_WORKERS=4
    LIMIT_MEM_SOFT=805306368   # 768 MB
    LIMIT_MEM_HARD=1073741824  # 1 GB
fi

echo "Konfigurasi PostgreSQL: shared_buffers=${PG_SHARED_BUFFERS}, work_mem=${PG_WORK_MEM}"
echo "Konfigurasi Odoo: workers=${ODOO_WORKERS}, soft_limit=${LIMIT_MEM_SOFT}"

echo "=========================================="
echo "==> 1. Menyiapkan Compose Wrapper (Google COS)"
echo "=========================================="

compose_cmd() {
    docker run --rm \
        -v /var/run/docker.sock:/var/run/docker.sock \
        -v /var/odoo:/var/odoo \
        -w /var/odoo \
        docker/compose:latest "$@"
}

docker pull docker/compose:latest > /dev/null

echo "=========================================="
echo "==> 2. Menyiapkan Struktur Direktori & Permission"
echo "=========================================="
sudo mkdir -p /var/odoo/postgres_data
sudo mkdir -p /var/odoo/postgres_config
sudo mkdir -p /var/odoo/data
sudo mkdir -p /var/odoo/addons
sudo mkdir -p /var/odoo/config
sudo mkdir -p /var/odoo/nginx_conf
sudo mkdir -p /var/odoo/letsencrypt
sudo mkdir -p /var/odoo/certbot_www

docker stop odoo_app odoo_db odoo_nginx odoo_certbot 2>/dev/null || true
docker rm odoo_app odoo_db odoo_nginx odoo_certbot 2>/dev/null || true

echo "=========================================="
echo "==> 3. Generate Konfigurasi Teroptimasi"
echo "=========================================="

# File odoo.conf teroptimasi
sudo tee /var/odoo/config/odoo.conf > /dev/null <<EOF
[options]
admin_passwd = admin
db_host = db
db_user = odoo
db_password = tAIre2vRTXDgRFSlqUy2kBFF
http_interface = 0.0.0.0
proxy_mode = True
addons_path = /usr/lib/python3/dist-packages/odoo/addons,/mnt/extra-addons

; Resource limits dinamis
workers = ${ODOO_WORKERS}
limit_memory_soft = ${LIMIT_MEM_SOFT}
limit_memory_hard = ${LIMIT_MEM_HARD}
limit_time_cpu = 120
limit_time_real = 240
max_cron_threads = 1
EOF

# File postgresql.conf custom
sudo tee /var/odoo/postgres_config/custom.conf > /dev/null <<EOF
shared_buffers = ${PG_SHARED_BUFFERS}
effective_cache_size = ${PG_EFFECTIVE_CACHE}
maintenance_work_mem = ${PG_MAINTENANCE_WORK_MEM}
work_mem = ${PG_WORK_MEM}
max_connections = ${PG_MAX_CONN}
random_page_cost = 1.1
checkpoint_completion_target = 0.9
EOF

sudo chmod -R 777 /var/odoo

echo "=========================================="
echo "==> 4. Parameter Domain & Email"
echo "=========================================="
read -p "Masukkan nama domain (default: andreascloud.my.id): " ODOO_DOMAIN
ODOO_DOMAIN=${ODOO_DOMAIN:-andreascloud.my.id}

read -p "Masukkan email SSL (default: arr@e.email): " CERTBOT_EMAIL
CERTBOT_EMAIL=${CERTBOT_EMAIL:-arr@e.email}

# Konfigurasi Nginx sementara untuk Challenge Let's Encrypt
sudo tee /var/odoo/nginx_conf/odoo.conf > /dev/null <<EOF
server {
    listen 80;
    server_name ${ODOO_DOMAIN};

    location /.well-known/acme-challenge/ {
        root /var/www/certbot;
    }

    location / {
        return 200 'Persiapan sertifikat SSL sedang berjalan...';
        add_header Content-Type text/plain;
    }
}
EOF

echo "==> 5. Menyiapkan docker-compose.yml..."
sudo tee /var/odoo/docker-compose.yml > /dev/null <<EOF
version: '3.8'

services:
  db:
    image: postgres:16
    container_name: odoo_db
    environment:
      - POSTGRES_DB=postgres
      - POSTGRES_PASSWORD=tAIre2vRTXDgRFSlqUy2kBFF
      - POSTGRES_USER=odoo
      - PGDATA=/var/lib/postgresql/data/pgdata
    command: postgres -c config_file=/etc/postgresql/postgresql.conf -c include_dir=/etc/postgresql/conf.d
    volumes:
      - /var/odoo/postgres_data:/var/lib/postgresql/data/pgdata
      - /var/odoo/postgres_config:/etc/postgresql/conf.d:ro
    restart: always

  odoo:
    image: odoo:latest
    container_name: odoo_app
    depends_on:
      - db
    volumes:
      - /var/odoo/data:/var/lib/odoo
      - /var/odoo/config/odoo.conf:/etc/odoo/odoo.conf
      - /var/odoo/addons:/mnt/extra-addons
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
    restart: always

  certbot:
    image: certbot/certbot
    container_name: odoo_certbot
    volumes:
      - /var/odoo/letsencrypt:/etc/letsencrypt
      - /var/odoo/certbot_www:/var/www/certbot
    entrypoint: "/bin/sh -c 'trap exit TERM; while :; do certbot renew; sleep 12h & wait \$\${!}; done;'"
EOF

echo "Menjalankan Nginx untuk verifikasi domain..."
compose_cmd up -d nginx

echo "=========================================="
echo "==> 6. Menerbitkan Sertifikat SSL (Let's Encrypt)"
echo "=========================================="
if [ ! -f "/var/odoo/letsencrypt/live/${ODOO_DOMAIN}/fullchain.pem" ]; then
    echo "Meminta sertifikat SSL baru dari Let's Encrypt..."
    docker run -it --rm \
      -v /var/odoo/letsencrypt:/etc/letsencrypt \
      -v /var/odoo/certbot_www:/var/www/certbot \
      certbot/certbot certonly --webroot \
      --webroot-path=/var/www/certbot \
      --email "${CERTBOT_EMAIL}" \
      --agree-tos --no-eff-email \
      -d "${ODOO_DOMAIN}"
    echo "Sertifikat SSL berhasil diterbitkan!"
else
    echo "Sertifikat SSL untuk domain ${ODOO_DOMAIN} sudah ada."
fi

echo "=========================================="
echo "==> 7. Mengaktifkan Nginx HTTPS (Port 443)"
echo "=========================================="
sudo tee /var/odoo/nginx_conf/odoo.conf > /dev/null <<EOF
upstream odoo {
    server odoo:8069;
}

upstream odoo-chat {
    server odoo:8072;
}

# Redirect HTTP ke HTTPS
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

# Server HTTPS Utama
server {
    listen 443 ssl;
    server_name ${ODOO_DOMAIN};

    ssl_certificate /etc/letsencrypt/live/${ODOO_DOMAIN}/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/${ODOO_DOMAIN}/privkey.pem;

    proxy_read_timeout 720s;
    proxy_connect_timeout 720s;
    proxy_send_timeout 720s;

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

    # Buffer Nginx hemat RAM
    client_max_body_size 64M;
    proxy_buffers 16 1024k;
    proxy_buffer_size 1024k;

    gzip_types text/css text/less text/plain text/xml application/xml application/json application/javascript;
    gzip on;
}
EOF

echo "=========================================="
echo "==> 8. Menjalankan Seluruh Stack Container"
echo "=========================================="
compose_cmd up -d
docker exec odoo_nginx nginx -s reload

echo ""
echo "=========================================="
echo "Instalasi selesai!"
echo "Akses Odoo di: https://${ODOO_DOMAIN}"
echo "=========================================="
