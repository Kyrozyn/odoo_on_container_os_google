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
echo "==> 1. Menyiapkan Struktur Direktori"
echo "=========================================="
sudo mkdir -p /var/odoo/postgres_data
sudo mkdir -p /var/odoo/data
sudo mkdir -p /var/odoo/addons
sudo mkdir -p /var/odoo/config
sudo mkdir -p /var/odoo/nginx_conf
sudo mkdir -p /var/odoo/letsencrypt
sudo mkdir -p /var/odoo/certbot_www

# Bersihkan container lama jika ada
docker stop odoo_app odoo_db odoo_nginx odoo_certbot 2>/dev/null || true
docker rm odoo_app odoo_db odoo_nginx odoo_certbot 2>/dev/null || true

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

sudo chmod -R 777 /var/odoo

echo "=========================================="
echo "==> 3. Parameter Domain & Email"
echo "=========================================="
read -p "Masukkan nama domain (default: andreascloud.my.id): " ODOO_DOMAIN
ODOO_DOMAIN=${ODOO_DOMAIN:-andreascloud.my.id}

read -p "Masukkan email SSL (default: arr@e.email): " CERTBOT_EMAIL
CERTBOT_EMAIL=${CERTBOT_EMAIL:-arr@e.email}

echo "=========================================="
echo "==> 4. Tahap Awal: Nginx HTTP Port 80 (Untuk Certbot Challenge)"
echo "=========================================="
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

echo "Menjalankan Nginx untuk verifikasi domain..."
compose_cmd up -d nginx

echo "=========================================="
echo "==> 6. Menerbitkan Sertifikat SSL (Let's Encrypt)"
echo "=========================================="
# Cek apakah sertifikat sudah pernah dibuat sebelumnya
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
    echo "Sertifikat SSL untuk domain ${ODOO_DOMAIN} sudah ada, melewati request baru."
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
echo "Akses Odoo Anda di: https://${ODOO_DOMAIN}"
echo "=========================================="
