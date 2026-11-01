#!/bin/bash

# Berhenti otomatis jika ada perintah yang gagal
set -e

echo "=========================================="
echo "==> 0. Menyiapkan Docker Compose untuk Google COS"
echo "=========================================="

# Di COS, partisi /var bersifat writable & executable
BIN_DIR="/var/bin"
sudo mkdir -p "$BIN_DIR"

# Tambahkan /var/bin ke PATH untuk sesi script saat ini
export PATH="$BIN_DIR:$PATH"

# Persistensi PATH ke ~/.bashrc untuk user saat ini (agar terbaca pada sesi SSH berikutnya)
if ! grep -q 'export PATH="/var/bin:$PATH"' "$HOME/.bashrc" 2>/dev/null; then
    echo 'export PATH="/var/bin:$PATH"' >> "$HOME/.bashrc"
    echo "PATH berhasil ditambahkan ke $HOME/.bashrc"
fi

# Persistensi PATH untuk root jika script dieksekusi dengan sudo
if [ "$HOME" != "/root" ] && [ -d "/root" ]; then
    sudo bash -c 'grep -q "export PATH=\"/var/bin:\$PATH\"" /root/.bashrc 2>/dev/null || echo "export PATH=\"/var/bin:\$PATH\"" >> /root/.bashrc'
fi

# Cek apakah docker-compose sudah terpasang di /var/bin atau docker compose plugin aktif
if ! command -v docker-compose &> /dev/null && ! docker compose version &> /dev/null && [ ! -f "$BIN_DIR/docker-compose" ]; then
    echo "Mengunduh binary Docker Compose ke $BIN_DIR..."
    
    ARCH=$(uname -m)
    sudo curl -L "https://github.com/docker/compose/releases/latest/download/docker-compose-linux-${ARCH}" -o "$BIN_DIR/docker-compose"
    sudo chmod +x "$BIN_DIR/docker-compose"
    
    echo "Docker Compose berhasil dipasang di $BIN_DIR/docker-compose"
else
    echo "Docker Compose sudah siap digunakan."
fi

# Tentukan binary/command Compose yang valid
if [ -x "$BIN_DIR/docker-compose" ]; then
    COMPOSE_CMD="$BIN_DIR/docker-compose"
elif command -v docker-compose &> /dev/null; then
    COMPOSE_CMD="docker-compose"
elif docker compose version &> /dev/null; then
    COMPOSE_CMD="docker compose"
else
    COMPOSE_CMD="docker run --rm -v /var/run/docker.sock:/var/run/docker.sock -v \"\$PWD:\$PWD\" -w \"\$PWD\" docker/compose:latest"
fi

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
