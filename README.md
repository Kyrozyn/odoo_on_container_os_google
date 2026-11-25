# RunOdoo — Self-Hosted PaaS Control Panel for Odoo

**RunOdoo** is a lightweight, self-hosted PaaS control panel purpose-built for deploying and managing **Odoo Community (17, 18, 19, and 20)**, PostgreSQL databases, custom addons, Nginx reverse proxies with Let's Encrypt SSL, Docker containers, interactive container terminals, and automated backups — all from a single web dashboard.

Built with a **Go** backend (single lightweight static binary with minimal RAM footprint, ideal for small VPS instances, Google Cloud `e2-micro`, and **Google Container-Optimized OS**) and a **React (Vite + Tailwind CSS)** frontend.

---

## Key Features

- **Multi-Version Odoo Community (17, 18, 19, 20)**
  - Deploy multiple isolated Odoo instances side-by-side (`odoo:17.0`, `odoo:18.0`, `odoo:19.0`, `odoo:20.0`, or any custom Docker image).
  - Automatic resource tuning (`workers`, `limit_memory_soft`, `limit_memory_hard`, `limit_time_cpu/real`) based on host CPU & RAM.
  - Real-time deployment progress tracking with live container & image pull logs.
  - Interactive `odoo.conf` editor supporting both key-value rows and Bulk Edit mode with live configuration preview.

- **Interactive Container Terminal & Live Logs**
  - Built-in **Web Terminal** (`xterm.js` over WebSocket PTY) inside each Odoo instance's detail view — interact directly with `bash`/`sh`, `odoo shell`, `python3`, `top`, `nano`/`vi`, and `Tab` autocomplete.
  - Live container log viewer (`Logs` tab) and real-time CPU/RAM container stats.

- **Domain Routing, Nginx Reverse Proxy & HTTPS Hardening**
  - Ready immediately on `http://<SERVER-IP>:3000` without requiring a domain upfront.
  - **Root Domain** wildcard routing (`*.yourdomain.com` → control panel at `panel.yourdomain.com`, Odoo apps at `<app>.yourdomain.com`) plus **Custom Domains** per instance.
  - Automated **Nginx** reverse proxy pre-configured for Odoo (`/websocket`, `/longpolling`, `client_max_body_size 512m`) and panel WebSocket terminal connections, with **1-click Let's Encrypt SSL (Certbot)**.
  - **Force HTTPS Redirect** on the panel domain and per-app domains.
  - **Domain-Only Access Hardening**:
    - Option to **disable direct port 3000 access** so the RunOdoo control panel is reachable only through the configured Panel Domain.
    - Per-instance option to **disable Direct HTTP Port** (`0.0.0.0:<port> -> 8069`) so each Odoo app is accessible only through its domain via Nginx.

- **Flexible PostgreSQL Management**
  - Choose between **Shared PostgreSQL** (RAM-efficient, isolated roles per instance) or a **Dedicated PostgreSQL Container** (full container isolation) when creating an instance.
  - Create, initialize (admin user, language, country, demo data), duplicate (DB + filestore), and drop databases directly from the UI.

- **Addons, Python Libs & Live CLI Module Upgrade**
  - Built-in `addons/` folder serves as a **Recommended Addons** catalog — select only the modules you need per instance, and only those modules are mounted into the container.
  - Clone & pull **Git repositories** (including private repositories with personal access tokens) or upload `.zip` archives.
  - Recursive `__manifest__.py` scanning and automatic `addons_path` generation per instance.
  - **Update Apps List**, **Clean Missing Apps** (checks `addons_path` vs. `ir.module.module` per database and removes stale uninstalled records with a detailed breakdown), **Python Libs** manager (`pip` inside `/var/lib/odoo/.local`), and **CLI Module Upgrade (`odoo -u`)** with **real-time streaming CLI output**.

- **Backup & Restore (DB + Filestore `.zip`)**
  - Standard Odoo `.zip` backups (`dump.sql` + `filestore/` + `manifest.json`), with 1-click download, restore, and upload-and-restore.
  - **Scheduled Cron Backups** with configurable retention policies.

---

## Supported Platforms (Google COS & Linux)

RunOdoo is tested and ready for both **Google Cloud Container-Optimized OS (COS)** and standard Linux distributions (**Ubuntu, Debian, RHEL**).

### Google Cloud Container-Optimized OS (COS) Notes
Google COS uses a read-only root filesystem and comes with Docker pre-installed (without `apt`, `yum`, or `ufw`). RunOdoo is specifically designed to work cleanly on COS:
- **Zero host package dependencies**: RunOdoo runs as a container talking to `/var/run/docker.sock` and stores all persistent data under `/var/runodoo` (writable on COS).
- **Automatic OS detection**: `install-runodoo.sh` detects `ID=cos` in `/etc/os-release`, skips host Docker installation, configures Docker DNS (`1.1.1.1`, `8.8.8.8`) if needed, prepares `/var/runodoo`, and builds/starts the `runodoo` container.
- **GCP Firewall Rules**: Because COS has no host `ufw`, make sure your **Google Cloud VPC Firewall** allows incoming TCP traffic on:
  - `3000` — Initial RunOdoo Control Panel access *(can be closed once Panel Domain + HTTPS hardening is enabled)*
  - `80` and `443` — HTTP/HTTPS traffic for Nginx reverse proxy & Let's Encrypt Certbot challenges
  - Any direct Odoo HTTP ports (e.g. `8069+`) only if you choose not to route exclusively via Nginx domains.

---

## Quick Start

### 1. Clone the Repository

```bash
git clone https://github.com/Kyrozyn/odoo_on_container_os_google.git
cd odoo_on_container_os_google
```

### 2. Run the Installer (Recommended for Google COS & Linux)

```bash
chmod +x install-runodoo.sh
./install-runodoo.sh
```

The script automatically detects **Google COS** or **standard Linux**, installs Docker if missing (non-COS), configures Docker DNS, prepares `/var/runodoo`, syncs `./addons`, builds the `runodoo:latest` Docker image, and starts the container on port `3000`.

### Alternative: Docker Compose

```bash
docker compose up -d --build
```

---

## Initial Setup Checklist (What to Do First)

After the container is running, follow these steps in order:

1. **Open the Control Panel**
   - Visit `http://<SERVER-IP>:3000` in your browser.
   - Sign in using the initial admin password (`runodoo123` by default, or the value you set in `RUNODOO_DEFAULT_PASSWORD`).
2. **Change the Admin Password Immediately**
   - Go to **Settings → Change Panel Admin Password** and set a strong password.
3. **Configure DNS & Enable HTTPS (Recommended)**
   - In your DNS provider, create a wildcard `A` record (`*.yourdomain.com`) and/or `A` record (`panel.yourdomain.com`) pointing to your server's public IP.
   - In **Dashboard** or **Settings**, enter your **Root Domain** (e.g., `yourdomain.com`) and **Let's Encrypt Email**, then click **Enable HTTPS (Certbot)**.
   - Verify you can reach the panel at `https://panel.yourdomain.com`.
4. **Enable Domain-Only Security Hardening (Optional)**
   - In **Settings**, enable **Force HTTPS redirect on Panel Domain** and **Disable direct port 3000 access (Panel Domain only)**.
   - For each Odoo instance (**HTTP & Domains** tab), attach its domain and disable **Direct HTTP Port** so it is only accessible through Nginx over HTTPS.
5. **Deploy Your First Odoo App**
   - Go to **Odoo Apps → Create Instance**, pick your Odoo version (`17`–`20`), PostgreSQL mode (**Shared** or **Dedicated**), and any **Recommended Addons**.
   - Once running, open **Databases → Create Database** to initialize your first Odoo database, or use the **Terminal** tab for direct container access.

---

## Updating RunOdoo

To pull the latest changes and rebuild the control panel without losing any data in `/var/runodoo`:

```bash
git pull
./install-runodoo.sh
```

If you changed Nginx configuration templates in an update, open **Settings** in the UI and click **Sync Nginx** so active vhost configs are regenerated and reloaded.

---

## Environment Variables

| Variable | Default | Description |
| :--- | :--- | :--- |
| `RUNODOO_PORT` | `3000` | Port the control panel listens on |
| `RUNODOO_DATA_DIR` | `/var/runodoo` | Data directory inside the RunOdoo container |
| `RUNODOO_HOST_DATA_DIR` | `/var/runodoo` | Host path mapped to `RUNODOO_DATA_DIR` (used for sibling container bind mounts) |
| `RUNODOO_RECOMMENDED_ADDONS_DIR` | `/var/runodoo/recommended-addons` | Recommended addons path inside the container |
| `RUNODOO_HOST_RECOMMENDED_ADDONS_DIR` | `/var/runodoo/recommended-addons` | Host path for recommended addons |
| `RUNODOO_DEFAULT_PASSWORD` | `runodoo123` | Initial admin password on first boot |
| `RUNODOO_NETWORK` | `runodoo-net` | Docker bridge network shared by RunOdoo, Nginx, Odoo, and PostgreSQL |
| `RUNODOO_NGINX_CONTAINER` | `runodoo-nginx` | Managed Nginx reverse proxy container name |
| `RUNODOO_SHARED_PG_CONTAINER` | `runodoo-postgres-shared` | Shared PostgreSQL container name |
| `DOCKER_SOCKET` | `/var/run/docker.sock` | Path to the Docker Engine Unix socket |

---

## Host Data Directory Layout

RunOdoo stores all persistent state, instance data, and proxy/SSL configs under `RUNODOO_HOST_DATA_DIR` (default `/var/runodoo`):

```text
/var/runodoo/
├── config/state.json       # Control panel settings, instances, addons, and backup metadata
├── instances/<name>/       # Per-instance odoo.conf, filestore data, and extra-addons
├── addons/                 # Cloned Git repositories and uploaded addon ZIPs
├── recommended-addons/     # Built-in recommended addons synced from ./addons
├── backups/                # Saved .zip backup archives
├── nginx/conf.d/           # Auto-generated Nginx virtual host configs
├── certbot/                # Let's Encrypt webroot challenges and SSL certificates
└── postgres-shared/        # Persistent data directory for the shared PostgreSQL container
```

---

## Project Structure

```text
├── backend/                # Go 1.25 API server + pure-Go Docker Engine Unix-socket client + WebSocket PTY
│   ├── cmd/runodoo/        # Application entrypoint (:3000)
│   └── internal/           # api, auth, addons, backup, config, database, docker, models, odoo, proxy, store, system
├── frontend/               # React 19 + TypeScript + Vite + Tailwind CSS v4 + xterm.js
├── Dockerfile              # Multi-stage build (Node -> Go -> Alpine runtime)
├── docker-compose.yml      # Docker Compose stack for RunOdoo
├── install-runodoo.sh      # Automated installer for Google COS & standard Linux
└── addons/                 # Built-in Recommended Addons catalog (selectively mounted per instance)
```
