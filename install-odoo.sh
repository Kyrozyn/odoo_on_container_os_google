#!/bin/bash

# Berhenti otomatis jika ada perintah yang gagal
set -e

echo "=========================================="
echo "==> 0. Menyiapkan Docker Compose untuk Google COS"
echo "=========================================="

# Di COS, partisi /var bersifat writable & executable
BIN_DIR="/var/bin"
sudo mkdir -p "$BIN_DIR"

# Tambahkan /var/bin ke PATH untuk eksekusi script ini
export PATH="$BIN_DIR:$PATH"

# Persistensi PATH hanya ke user non-root asli (jika dijalankan via sudo)
if [ -n "$SUDO_USER" ] && [ -d "/home/$SUDO_USER" ]; then
    USER_BASHRC="/home/$SUDO_USER/.bashrc"
    if ! grep -q 'export PATH="/var/bin:$PATH"' "$USER_BASHRC" 2>/dev/null; then
        echo 'export PATH="/var/bin:$PATH"' >> "$USER_BASHRC"
        echo "PATH berhasil ditambahkan ke $USER_BASHRC"
    fi
elif [ -w "$HOME/.bashrc" ]; then
    if ! grep -q 'export PATH="/var/bin:$PATH"' "$HOME/.bashrc" 2>/dev/null; then
        echo 'export PATH="/var/bin:$PATH"' >> "$HOME/.bashrc"
        echo "PATH berhasil ditambahkan ke $HOME/.bashrc"
    fi
fi

# Cek apakah binary docker-compose sudah ada di /var/bin
if [ ! -f "$BIN_DIR/docker-compose" ]; then
    echo "Mengunduh binary Docker Compose ke $BIN_DIR..."
    ARCH=$(uname -m)
    sudo curl -L "https://github.com/docker/compose/releases/latest/download/docker-compose-linux-${ARCH}" -o "$BIN_DIR/docker-compose"
    sudo chmod +x "$BIN_DIR/docker-compose"
    echo "Docker Compose berhasil dipasang di $BIN_DIR/docker-compose"
else
    echo "Docker Compose sudah tersedia di $BIN_DIR/docker-compose."
fi

COMPOSE_CMD="$BIN_DIR/docker-compose"

echo "=========================================="
echo "==> 1. Membuat Struktur Direktori Odoo di /var"
echo "=========================================="
sudo mkdir -p /var/odoo/postgres_data
sudo mkdir -p /var/odoo/data
sudo mkdir -p /var/odoo/addons
sudo mkdir -p /var/odoo/config
sudo mkdir -p /var/odoo/nginx_conf
sudo mkdir -p /var/odoo/letsencrypt
sudo mkdir -p /var/odoo/certbot_www

echo "==> 2. Menyiapkan File odoo.conf..."
if [ -d "/var/odoo/config/odoo.conf" ]; then
    sudo rm -rf /var/odoo/config/odoo.conf
fi

if [ ! -f "/var/odoo/config/odoo.conf" ]; then
    sudo touch /var/odoo/config/odoo.conf
    sudo tee /var/odoo/config/odoo.conf > /dev/null <<EOF
[options]
admin_passwd = admin
db_host = db
db_user = odoo
db_password = tAIre2vRTXDgRFSlqUy2kBFF
addons_path = /usr/lib/python3/dist-packages/odoo/addons,/mnt/extra-addons
EOF
fi

echo "==> 3. Mengatur Izin Direktori..."
sudo chmod -R 755 /var/odoo

echo "=========================================="
echo "==> 4. Konfigurasi Nginx & Domain"
echo "=========================================="
read -p "Masukkan nama domain Anda (contoh: odoo.domainanda.com): " ODOO_DOMAIN
read -p "Masukkan email Anda untuk sertifikat SSL: " CERTBOT_EMAIL

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
echo "==> 5. Membuat docker-compose.yml"
echo "=========================================="
sudo tee /var/odoo/docker-compose.yml > /dev/null <<EOF
version: '3.8'

services:
  db:
    image: postgres:15
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
echo "==> 6. Menjalankan Container (Docker Compose Up)"
echo "=========================================="
cd /var/odoo
sudo "$COMPOSE_CMD" up -d

echo ""
echo "Status container aktif:"
sudo "$COMPOSE_CMD" ps
