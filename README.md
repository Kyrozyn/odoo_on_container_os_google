# RunOdoo — PaaS Control Panel for Odoo

**RunOdoo** adalah control panel self-hosted yang dirancang khusus untuk mengelola **Odoo Community (17, 18, 19, 20)**, database PostgreSQL, custom addons, reverse proxy Nginx + SSL Let's Encrypt, container Docker, serta backup & restore — semuanya dari satu dashboard web di **port 3000** atau **custom domain**.

Dibangun dengan **Go** (single binary super ringan & hemat RAM, ideal untuk VPS kecil / Google Cloud `e2-micro` / Google Container-Optimized OS) + **React (Vite + Tailwind CSS)**.

---

## Fitur Utama

- **Multi-Version Odoo Community (17, 18, 19, 20)**
  - Install banyak instance Odoo sekaligus (`odoo:17.0`, `odoo:18.0`, `odoo:19.0`, `odoo:20.0`, atau custom Docker image).
  - Auto-tuning `workers`, `limit_memory_soft/hard`, dan `limit_time_cpu/real = 36000` berdasarkan spesifikasi CPU & RAM server.
  - Editor `odoo.conf` dan lihat konfigurasi yang di-generate secara live.
- **Port 3000 & Custom Domain + HTTPS**
  - Langsung bisa diakses di `http://<IP-SERVER>:3000` tanpa perlu setup domain terlebih dahulu.
  - Dukungan **Root Domain** (`*.yourdomain.com` -> panel di `panel.yourdomain.com`, app di `<app>.yourdomain.com`) dan **Custom Domain** per instance Odoo.
  - Reverse proxy **Nginx** otomatis dengan konfigurasi khusus Odoo (`/websocket`, `/longpolling`, `client_max_body_size 512m`) + **1-klik SSL Let's Encrypt (Certbot)**.
- **Manajemen Database PostgreSQL Fleksibel**
  - Pilih **Shared PostgreSQL** (hemat RAM, role terisolasi per instance) atau **Dedicated PostgreSQL Container** (isolasi penuh) saat membuat instance.
  - Create, Duplicate (DB + Filestore), Drop database langsung dari panel.
- **Manajemen Addons & Recommended Addons**
  - Folder `addons/` bawaan dijadikan katalog **Recommended Addons** — saat membuat/mengedit instance Odoo, user cukup mencentang modul mana saja yang ingin di-install (hanya modul yang dipilih yang di-mount ke container).
  - Clone & Pull **Git Repository** (termasuk private repo dengan token) atau **Upload `.zip`**.
  - Otomatis memindai `__manifest__.py` secara rekursif dan menyusun `addons_path` per instance.
  - Jalankan upgrade modul (`odoo -u <module> --stop-after-init`) langsung dari UI.
- **Backup & Restore (DB + Filestore `.zip`)**
  - Backup standar Odoo (`.zip` berisi `dump.sql` + `filestore/` + `manifest.json`), Download, Restore, dan Upload `.zip` untuk restore.
  - **Scheduled Cron Backup** otomatis lengkap dengan retention policy.
- **Manajemen Container & Keamanan**
  - Start, Stop, Restart, Live Logs, dan CPU/RAM stats untuk semua container (Odoo, PostgreSQL, Nginx).
  - Autentikasi panel dengan password admin (bcrypt) + JWT.
  - Kompatibel dengan **Google Container-Optimized OS (COS)** maupun Linux standar (**Ubuntu, Debian, RHEL**).

---

## Quick Start (Google COS & Linux)

Jalankan installer 1-perintah berikut di server Anda:

```bash
chmod +x install-runodoo.sh
./install-runodoo.sh
```

Atau menggunakan Docker Compose:

```bash
docker compose up -d --build
```

Setelah selesai, buka panel di browser:

- **URL**: `http://<IP-SERVER>:3000`
- **Password default**: `runodoo123` *(segera ubah di menu **Settings** atau set via env `RUNODOO_DEFAULT_PASSWORD`)*

---

## Struktur Proyek

```
├── backend/              # Go 1.25 API Server + pure-Go Docker Engine Unix-socket client
│   ├── cmd/runodoo/      # Entrypoint (:3000)
│   └── internal/         # api, auth, addons, backup, config, database, docker, models, odoo, proxy, store, system
├── frontend/             # React 19 + TypeScript + Vite + Tailwind CSS v4
├── Dockerfile            # Multi-stage build (Node -> Go -> Alpine runtime)
├── docker-compose.yml    # Compose stack untuk RunOdoo
├── install-runodoo.sh    # Installer otomatis Google COS & Linux standar
└── addons/               # Katalog Recommended Addons (bisa dipilih selektif saat install Odoo)
```
