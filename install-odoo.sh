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
# /root/.bashrc di Google COS adalah Read-Only, sedangkan /home/<user> writable
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

# Gunakan binary langsung dari /var/bin
COMPOSE_CMD="$BIN_DIR/docker-compose"
echo "Compose handler aktif: $COMPOSE_CMD"

echo "=========================================="
echo "==> 1. Membuat Struktur Direktori Odoo di /var"
echo "=========================================="
sudo mkdir -p /var/odoo/postgres_data
sudo mkdir -p /var/odoo/data
sudo mkdir -p /var/odoo/addons
sudo mkdir -p /var/odoo/config
sudo mkdir -p /var/odoo/nginx_conf
sudo mkdir -p /var/odoo/letsencrypt

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
echo "==> 4. Konfigurasi Nginx & SSL"
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
        return 301 https://\$host\$request_uri;
    }
}

server {
    listen 443 ssl;
    server_name ${ODOO_DOMAIN};

    proxy_read_timeout 720s;
    proxy_connect_timeout 720s;
    proxy_send_timeout 720s;

    ssl_certificate /etc/letsencrypt/live/${ODOO_DOMAIN}/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/${ODOO_DOMAIN}/privkey.pem;

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

    gzip_types text/css text/less text/plain text/xml application/xml application/json application/javascript;
    gzip on;
}
EOF

echo "Konfigurasi selesai. File tersimpan di /var/odoo/nginx_conf/odoo.conf"
