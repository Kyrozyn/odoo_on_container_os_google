#!/bin/bash

# Berhenti otomatis jika ada perintah yang gagal
set -e

echo "=========================================="
echo "==> 1. Membuat Struktur Direktori Odoo 20"
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
echo "==> 4. Konfigurasi Nginx & SSL (Certbot)"
echo "=========================================="
read -p "Masukkan nama domain Anda (contoh: odoo.domainanda.com): " ODOO_DOMAIN
read -p "Masukkan email Anda untuk sertifikat SSL: " CERTBOT_EMAIL

# Buat file konfigurasi Nginx untuk domain tersebut
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
include /etc/letsencrypt/options-ssl-nginx.conf;
ssl_dhparam /etc/letsencrypt/ssl-dhparams.pem;

access_log /var/log/nginx/odoo.access.log;
error_log /var/log/nginx/odoo.error.log;

location / {
    proxy_pass http://odoo;
    proxy_next_upstream error timeout invalid_header http_500 http_502 http_503 http_504;
    proxy_redirect off;

    proxy_set_header Host \$host;
    proxy_set_header X-Real-IP \$remote_addr;
    proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto https;
}

location /longpolling {
    proxy_pass http://odoo-chat;

    proxy_set_header Host \$host;
    proxy_set_header X-Real-IP \$remote_addr;
    proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto https;
}

gzip_types text/css text/scss text/plain text/xml application/xml application/json application/javascript;
gzip on;
}
EOF

# Perbarui file docker-compose.yml secara otomatis agar menyertakan email certbot
if [ -f "docker-compose.yml" ]; then
    # Mengganti baris CERTBOT_EMAIL= dengan email yang diinput user
    sed -i "s|- CERTBOT_EMAIL=.*|- CERTBOT_EMAIL=${CERTBOT_EMAIL}|g" docker-compose.yml
fi

echo "=========================================="
echo "==> 5. Menjalankan Docker Compose..."
echo "=========================================="
docker compose config > /dev/null
sudo docker compose up -d

echo "==> Selesai! Memeriksa status kontainer..."
sudo docker compose ps