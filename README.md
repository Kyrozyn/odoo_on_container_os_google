# RunOdoo — Self-Hosted PaaS Control Panel for Odoo

**RunOdoo** is a lightweight, self-hosted PaaS control panel purpose-built for deploying and managing **Odoo Community (17, 18, 19, and 20)**, PostgreSQL databases, custom addons, Nginx reverse proxies with Let's Encrypt SSL, Docker containers, and automated backups — all from a single web dashboard on **port 3000** or your **custom domain**.

Built with a **Go** backend (single lightweight binary with minimal RAM footprint, ideal for small VPS instances, Google Cloud `e2-micro`, and Google Container-Optimized OS) and a **React (Vite + Tailwind CSS)** frontend.

---

## Key Features

- **Multi-Version Odoo Community (17, 18, 19, 20)**
  - Deploy multiple isolated Odoo instances side-by-side (`odoo:17.0`, `odoo:18.0`, `odoo:19.0`, `odoo:20.0`, or any custom Docker image).
  - Automatic resource tuning for `workers`, `limit_memory_soft`, `limit_memory_hard`, and `limit_time_cpu/real` based on host CPU & RAM.
  - Real-time deployment progress tracking with live container & image pull logs.
  - Interactive `odoo.conf` editor supporting both key-value rows and Bulk Edit mode, with a live configuration preview.

- **Domain Routing, Nginx Reverse Proxy & HTTPS Hardening**
  - Ready immediately on `http://<SERVER-IP>:3000` without requiring a domain upfront.
  - **Root Domain** wildcard routing (`*.yourdomain.com` → control panel at `panel.yourdomain.com`, Odoo apps at `<app>.yourdomain.com`) plus **Custom Domains** per instance.
  - Automated **Nginx** reverse proxy pre-configured for Odoo (`/websocket`, `/longpolling`, `client_max_body_size 512m`) with **1-click Let's Encrypt SSL (Certbot)**.
  - **Force HTTPS Redirect** on the panel domain and per-app domains.
  - **Domain-Only Access Hardening**:
    - Option to **disable direct port 3000 access** so the RunOdoo control panel is reachable only through the configured Panel Domain.
    - Per-instance option to **disable Direct HTTP Port** (`0.0.0.0:<port> -> 8069`) so each Odoo app is accessible only through its domain via Nginx.

- **Flexible PostgreSQL Management**
  - Choose between **Shared PostgreSQL** (RAM-efficient, isolated roles per instance) or a **Dedicated PostgreSQL Container** (full container isolation) when creating an instance.
  - Create, initialize (admin user, language, country, demo data), duplicate (DB + filestore), and drop databases directly from the UI.

- **Addons & Recommended Addons**
  - Built-in `addons/` folder serves as a **Recommended Addons** catalog — select only the modules you need per instance, and only those modules are mounted into the container.
  - Clone & pull **Git repositories** (including private repositories with personal access tokens) or upload `.zip` archives.
  - Recursive `__manifest__.py` scanning and automatic `addons_path` generation per instance.
  - **Update Apps List**, **Clean Missing Apps** (checks `addons_path` vs. `ir.module.module` per database and removes stale uninstalled records with a detailed breakdown), and CLI module upgrades (`odoo -u <module> --stop-after-init`) directly from the UI.

- **Backup & Restore (DB + Filestore `.zip`)**
  - Standard Odoo `.zip` backups (`dump.sql` + `filestore/` + `manifest.json`), with 1-click download, restore, and upload-and-restore.
  - **Scheduled Cron Backups** with configurable retention policies.

- **Container Management & Security**
  - Start, stop, restart, inspect live logs, and monitor real-time CPU & RAM usage for all containers (Odoo, PostgreSQL, Nginx).
  - Admin authentication using bcrypt-hashed passwords and JWT tokens.
  - Tested for **Google Container-Optimized OS (COS)** as well as standard Linux distributions (**Ubuntu, Debian, RHEL**).

---

## Quick Start (Google COS & Linux)

### Option 1: One-Command Installer (Recommended)

Run the installer script on your server:

```bash
chmod +x install-runodoo.sh
./install-runodoo.sh
```

The script automatically detects Google COS or standard Linux, installs Docker if missing, configures Docker DNS, prepares `/var/runodoo`, builds the RunOdoo image, and starts the container on port `3000`.

### Option 2: Docker Compose

```bash
docker compose up -d --build
```

### Accessing the Control Panel

Once running, open the panel in your browser:

- **URL**: `http://<SERVER-IP>:3000`
- **Default Password**: `runodoo123` *(change this immediately in **Settings**, or set `RUNODOO_DEFAULT_PASSWORD` before starting)*

---

## Recommended Post-Installation Steps

1. **Change Admin Password**: Go to **Settings → Change Panel Admin Password** and update the default password.
2. **Configure Root Domain & SSL**:
   - Point a wildcard DNS `A` record (`*.yourdomain.com`) to your server's public IP.
   - Enter your Root Domain and Let's Encrypt email in **Dashboard** or **Settings**, then click **Enable HTTPS (Certbot)**.
3. **Enable Domain-Only Hardening (Optional)**:
   - In **Settings**, enable **Force HTTPS redirect on Panel Domain** and **Disable direct port 3000 access (Panel Domain only)** once your Panel Domain is confirmed reachable.
   - On each Odoo instance (**HTTP & Domains** tab), disable **Direct HTTP Port** once a domain is attached so the app is only reachable through Nginx.

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
├── backend/                # Go 1.25 API server + pure-Go Docker Engine Unix-socket client
│   ├── cmd/runodoo/        # Application entrypoint (:3000)
│   └── internal/           # api, auth, addons, backup, config, database, docker, models, odoo, proxy, store, system
├── frontend/               # React 19 + TypeScript + Vite + Tailwind CSS v4
├── Dockerfile              # Multi-stage build (Node -> Go -> Alpine runtime)
├── docker-compose.yml      # Docker Compose stack for RunOdoo
├── install-runodoo.sh      # Automated installer for Google COS & standard Linux
└── addons/                 # Built-in Recommended Addons catalog (selectively mounted per instance)
```
