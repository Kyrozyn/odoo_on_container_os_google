#!/bin/bash
set -e

echo "=========================================="
echo "==> 0. Menyiapkan Compose Wrapper (Google COS)"
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
echo "==> 1. Menyiapkan Struktur Direktori & Permission"
echo "=========================================="
sudo mkdir -p /var/odoo/postgres_data
sudo mkdir -p /var/odoo/data
sudo mkdir -p /var/odoo/addons
sudo mkdir -p /var/odoo/config
sudo mkdir -p /var/odoo/nginx_conf
sudo mkdir -p /var/odoo/letsencrypt
sudo mkdir -p /var/odoo/certbot_www

# Hentikan container lama & hapus volume postgres lama jika masih format PG15
docker stop odoo_app odoo_db odoo_nginx odoo_certbot 2>/dev/null || true
docker rm odoo_app odoo_db odoo_nginx odoo_certbot 2>/dev/null || true
sudo rm -rf /var/odoo/postgres_data/*

echo "=========================================="
echo "==> 2. Menyiapkan File odoo.conf (Binding 0.0.0.0)"
echo "=========================================="
sudo rm -rf /var/odoo/config/odoo.conf
sudo tee /var/odoo/config/odoo.conf > /dev/null <<EOF
[options]
admin_passwd = admin
db_host = db
db_user = odoo
db_password = tAIre2vRTXDgRFSlqUy2kBFF
http_interface = 0.0.0.0
proxy_mode = True
addons_path = /usr/lib/python3/dist-packages/odoo/addons,/mnt/extra-addons
EOF

# Permission 777 agar UID container (odoo & postgres) bisa baca/tulis di /var
sudo chmod -R 777 /var/odoo

echo "=========================================="
echo "==> 3. Konfigurasi Nginx & Domain"
echo "=========================================="
read -p "Masukkan nama domain (default: andreascloud.my.id): " ODOO_DOMAIN
ODOO_DOMAIN=${ODOO_DOMAIN:-andreascloud.my.id}

read -p "Masukkan email SSL (default: arr@e.email): " CERTBOT_EMAIL
CERTBOT_EMAIL=${CERTBOT_EMAIL:-arr@e.email}

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
        proxy_pass http://odoo;
        proxy_next_upstream error timeout invalid_header http_500 http_502 http_503 http_504;
        proxy_redirect off;

        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }

    location /longpolling {
        proxy_pass http://odoo-chat;
    }

    gzip_types text/css text/less text/plain text/xml application/xml application/json application/javascript;
    gzip on;
}
EOF

echo "=========================================="
echo "==> 4. Menyiapkan docker-compose.yml (Postgres 16)"
echo "=========================================="
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
    volumes:
      - /var/odoo/postgres_data:/var/lib/postgresql/data/pgdata
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

echo "=========================================="
echo "==> 5. Menjalankan Container Stack"
echo "=========================================="
compose_cmd up -d

echo ""
echo "Tunggu sekitar 15-30 detik untuk inisialisasi awal database, lalu cek:"
echo "docker logs -f odoo_app"
