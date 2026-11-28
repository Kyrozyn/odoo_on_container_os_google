package monitoring

import "fmt"

func grafanaDatasourceYAML(promContainer string) string {
	return fmt.Sprintf(`apiVersion: 1
datasources:
  - name: Prometheus
    type: prometheus
    uid: runodoo-prom
    access: proxy
    url: http://%s:9090
    isDefault: true
    editable: false
`, promContainer)
}

func grafanaDashboardsProviderYAML() string {
	return `apiVersion: 1
providers:
  - name: RunOdoo
    orgId: 1
    folder: RunOdoo
    type: file
    disableDeletion: false
    editable: true
    updateIntervalSeconds: 30
    options:
      path: /var/lib/grafana/dashboards
`
}

func odooAppsDashboardJSON() string {
	return `{
  "uid": "runodoo-odoo-apps",
  "title": "RunOdoo - Odoo Apps Monitoring",
  "tags": ["runodoo", "odoo"],
  "timezone": "browser",
  "schemaVersion": 39,
  "version": 1,
  "refresh": "10s",
  "templating": {
    "list": [
      {
        "name": "instance",
        "label": "Odoo Instance",
        "type": "query",
        "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
        "query": "label_values(runodoo_odoo_http_up, instance)",
        "refresh": 2,
        "sort": 1,
        "multi": false,
        "includeAll": false
      }
    ]
  },
  "panels": [
    {
      "id": 1,
      "type": "stat",
      "title": "Odoo HTTP Status",
      "gridPos": { "x": 0, "y": 0, "w": 4, "h": 4 },
      "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
      "targets": [{ "expr": "runodoo_odoo_http_up{instance=\"$instance\"}", "refId": "A" }],
      "fieldConfig": {
        "defaults": {
          "mappings": [
            { "type": "value", "options": { "0": { "text": "DOWN", "color": "red" }, "1": { "text": "UP", "color": "green" } } }
          ]
        }
      }
    },
    {
      "id": 2,
      "type": "stat",
      "title": "Active Users",
      "gridPos": { "x": 4, "y": 0, "w": 4, "h": 4 },
      "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
      "targets": [{ "expr": "sum(runodoo_odoo_users_active{instance=\"$instance\"})", "refId": "A" }]
    },
    {
      "id": 3,
      "type": "stat",
      "title": "Installed Modules",
      "gridPos": { "x": 8, "y": 0, "w": 4, "h": 4 },
      "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
      "targets": [{ "expr": "max(runodoo_odoo_installed_modules{instance=\"$instance\"})", "refId": "A" }]
    },
    {
      "id": 4,
      "type": "stat",
      "title": "Configured Workers",
      "gridPos": { "x": 12, "y": 0, "w": 4, "h": 4 },
      "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
      "targets": [{ "expr": "runodoo_odoo_workers_configured{instance=\"$instance\"}", "refId": "A" }]
    },
    {
      "id": 5,
      "type": "stat",
      "title": "Pending Mail Queue",
      "gridPos": { "x": 16, "y": 0, "w": 4, "h": 4 },
      "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
      "targets": [{ "expr": "sum(runodoo_odoo_mail_queue_pending{instance=\"$instance\"})", "refId": "A" }]
    },
    {
      "id": 6,
      "type": "stat",
      "title": "Overdue Cron Jobs",
      "gridPos": { "x": 20, "y": 0, "w": 4, "h": 4 },
      "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
      "targets": [{ "expr": "sum(runodoo_odoo_cron_overdue{instance=\"$instance\"})", "refId": "A" }],
      "fieldConfig": {
        "defaults": {
          "thresholds": {
            "mode": "absolute",
            "steps": [
              { "color": "green", "value": null },
              { "color": "orange", "value": 1 },
              { "color": "red", "value": 5 }
            ]
          }
        }
      }
    },
    {
      "id": 7,
      "type": "timeseries",
      "title": "Container CPU Usage (%)",
      "gridPos": { "x": 0, "y": 4, "w": 12, "h": 8 },
      "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
      "targets": [{ "expr": "runodoo_container_cpu_percent{instance=\"$instance\",role=\"odoo\"}", "legendFormat": "{{container}}", "refId": "A" }],
      "fieldConfig": { "defaults": { "unit": "percent" } }
    },
    {
      "id": 8,
      "type": "timeseries",
      "title": "Container Memory Usage vs Limits (Bytes)",
      "gridPos": { "x": 12, "y": 4, "w": 12, "h": 8 },
      "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
      "targets": [
        { "expr": "runodoo_container_memory_usage_bytes{instance=\"$instance\",role=\"odoo\"}", "legendFormat": "Used RAM", "refId": "A" },
        { "expr": "runodoo_container_memory_limit_bytes{instance=\"$instance\",role=\"odoo\"}", "legendFormat": "Container Limit", "refId": "B" }
      ],
      "fieldConfig": { "defaults": { "unit": "bytes" } }
    },
    {
      "id": 9,
      "type": "timeseries",
      "title": "Odoo HTTP Response Latency",
      "gridPos": { "x": 0, "y": 12, "w": 12, "h": 8 },
      "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
      "targets": [{ "expr": "runodoo_odoo_http_response_seconds{instance=\"$instance\"}", "legendFormat": "HTTP Latency", "refId": "A" }],
      "fieldConfig": { "defaults": { "unit": "s" } }
    },
    {
      "id": 10,
      "type": "timeseries",
      "title": "Cron Jobs & Mail Queue Activity",
      "gridPos": { "x": 12, "y": 12, "w": 12, "h": 8 },
      "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
      "targets": [
        { "expr": "runodoo_odoo_cron_active{instance=\"$instance\"}", "legendFormat": "Active Crons ({{db}})", "refId": "A" },
        { "expr": "runodoo_odoo_cron_overdue{instance=\"$instance\"}", "legendFormat": "Overdue Crons ({{db}})", "refId": "B" },
        { "expr": "runodoo_odoo_mail_queue_pending{instance=\"$instance\"}", "legendFormat": "Pending Mail ({{db}})", "refId": "C" }
      ]
    },
    {
      "id": 11,
      "type": "timeseries",
      "title": "Filestore Attachments Size (Bytes)",
      "gridPos": { "x": 0, "y": 20, "w": 12, "h": 7 },
      "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
      "targets": [{ "expr": "runodoo_odoo_attachments_bytes{instance=\"$instance\"}", "legendFormat": "{{db}}", "refId": "A" }],
      "fieldConfig": { "defaults": { "unit": "bytes" } }
    },
    {
      "id": 12,
      "type": "timeseries",
      "title": "Filestore Attachments Count",
      "gridPos": { "x": 12, "y": 20, "w": 12, "h": 7 },
      "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
      "targets": [{ "expr": "runodoo_odoo_attachments_count{instance=\"$instance\"}", "legendFormat": "{{db}}", "refId": "A" }]
    }
  ]
}
`
}

func postgresqlDashboardJSON() string {
	return `{
  "uid": "runodoo-postgresql",
  "title": "RunOdoo - PostgreSQL Database Monitoring",
  "tags": ["runodoo", "postgresql"],
  "timezone": "browser",
  "schemaVersion": 39,
  "version": 1,
  "refresh": "10s",
  "templating": {
    "list": [
      {
        "name": "instance",
        "label": "Instance / PG Target",
        "type": "query",
        "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
        "query": "label_values(pg_up, instance)",
        "refresh": 2,
        "sort": 1,
        "multi": false,
        "includeAll": false
      },
      {
        "name": "datname",
        "label": "Database",
        "type": "query",
        "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
        "query": "label_values(pg_database_size_bytes{instance=\"$instance\"}, datname)",
        "refresh": 2,
        "sort": 1,
        "multi": true,
        "includeAll": true,
        "current": { "text": "All", "value": "$__all" }
      }
    ]
  },
  "panels": [
    {
      "id": 1,
      "type": "stat",
      "title": "PostgreSQL Up",
      "gridPos": { "x": 0, "y": 0, "w": 4, "h": 4 },
      "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
      "targets": [{ "expr": "pg_up{instance=\"$instance\"}", "refId": "A" }],
      "fieldConfig": {
        "defaults": {
          "mappings": [
            { "type": "value", "options": { "0": { "text": "DOWN", "color": "red" }, "1": { "text": "UP", "color": "green" } } }
          ]
        }
      }
    },
    {
      "id": 2,
      "type": "stat",
      "title": "Active Connections",
      "gridPos": { "x": 4, "y": 0, "w": 5, "h": 4 },
      "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
      "targets": [{ "expr": "sum(pg_stat_activity_count{instance=\"$instance\",state=\"active\"})", "refId": "A" }]
    },
    {
      "id": 3,
      "type": "stat",
      "title": "Max Connections",
      "gridPos": { "x": 9, "y": 0, "w": 5, "h": 4 },
      "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
      "targets": [{ "expr": "pg_settings_max_connections{instance=\"$instance\"}", "refId": "A" }]
    },
    {
      "id": 4,
      "type": "gauge",
      "title": "Cache Hit Ratio (%)",
      "gridPos": { "x": 14, "y": 0, "w": 5, "h": 4 },
      "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
      "targets": [{ "expr": "sum(rate(pg_stat_database_blks_hit{instance=\"$instance\",datname=~\"$datname\"}[5m])) / (sum(rate(pg_stat_database_blks_hit{instance=\"$instance\",datname=~\"$datname\"}[5m])) + sum(rate(pg_stat_database_blks_read{instance=\"$instance\",datname=~\"$datname\"}[5m])) + 0.0001) * 100", "refId": "A" }],
      "fieldConfig": {
        "defaults": {
          "unit": "percent",
          "min": 0,
          "max": 100,
          "thresholds": {
            "mode": "absolute",
            "steps": [
              { "color": "red", "value": null },
              { "color": "orange", "value": 90 },
              { "color": "green", "value": 98 }
            ]
          }
        }
      }
    },
    {
      "id": 5,
      "type": "stat",
      "title": "Total DB Size",
      "gridPos": { "x": 19, "y": 0, "w": 5, "h": 4 },
      "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
      "targets": [{ "expr": "sum(pg_database_size_bytes{instance=\"$instance\",datname=~\"$datname\"})", "refId": "A" }],
      "fieldConfig": { "defaults": { "unit": "bytes" } }
    },
    {
      "id": 6,
      "type": "timeseries",
      "title": "Connections by State",
      "gridPos": { "x": 0, "y": 4, "w": 12, "h": 8 },
      "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
      "targets": [
        { "expr": "sum by (state) (pg_stat_activity_count{instance=\"$instance\"})", "legendFormat": "{{state}}", "refId": "A" },
        { "expr": "pg_settings_max_connections{instance=\"$instance\"}", "legendFormat": "max_connections", "refId": "B" }
      ]
    },
    {
      "id": 7,
      "type": "timeseries",
      "title": "Transactions per Second (Commits & Rollbacks)",
      "gridPos": { "x": 12, "y": 4, "w": 12, "h": 8 },
      "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
      "targets": [
        { "expr": "sum(rate(pg_stat_database_xact_commit{instance=\"$instance\",datname=~\"$datname\"}[1m]))", "legendFormat": "Commits/s", "refId": "A" },
        { "expr": "sum(rate(pg_stat_database_xact_rollback{instance=\"$instance\",datname=~\"$datname\"}[1m]))", "legendFormat": "Rollbacks/s", "refId": "B" }
      ]
    },
    {
      "id": 8,
      "type": "timeseries",
      "title": "Tuple Operations / sec",
      "gridPos": { "x": 0, "y": 12, "w": 12, "h": 8 },
      "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
      "targets": [
        { "expr": "sum(rate(pg_stat_database_tup_fetched{instance=\"$instance\",datname=~\"$datname\"}[1m]))", "legendFormat": "Fetched/s", "refId": "A" },
        { "expr": "sum(rate(pg_stat_database_tup_inserted{instance=\"$instance\",datname=~\"$datname\"}[1m]))", "legendFormat": "Inserted/s", "refId": "B" },
        { "expr": "sum(rate(pg_stat_database_tup_updated{instance=\"$instance\",datname=~\"$datname\"}[1m]))", "legendFormat": "Updated/s", "refId": "C" },
        { "expr": "sum(rate(pg_stat_database_tup_deleted{instance=\"$instance\",datname=~\"$datname\"}[1m]))", "legendFormat": "Deleted/s", "refId": "D" }
      ]
    },
    {
      "id": 9,
      "type": "timeseries",
      "title": "Database Size Breakdown",
      "gridPos": { "x": 12, "y": 12, "w": 12, "h": 8 },
      "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
      "targets": [
        { "expr": "pg_database_size_bytes{instance=\"$instance\",datname=~\"$datname\"}", "legendFormat": "{{datname}}", "refId": "A" }
      ],
      "fieldConfig": { "defaults": { "unit": "bytes" } }
    },
    {
      "id": 10,
      "type": "timeseries",
      "title": "PostgreSQL Container CPU (%)",
      "gridPos": { "x": 0, "y": 20, "w": 12, "h": 7 },
      "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
      "targets": [
        { "expr": "runodoo_container_cpu_percent{role=\"postgres\"}", "legendFormat": "{{container}}", "refId": "A" }
      ],
      "fieldConfig": { "defaults": { "unit": "percent" } }
    },
    {
      "id": 11,
      "type": "timeseries",
      "title": "PostgreSQL Container Memory (Bytes)",
      "gridPos": { "x": 12, "y": 20, "w": 12, "h": 7 },
      "datasource": { "type": "prometheus", "uid": "runodoo-prom" },
      "targets": [
        { "expr": "runodoo_container_memory_usage_bytes{role=\"postgres\"}", "legendFormat": "{{container}}", "refId": "A" }
      ],
      "fieldConfig": { "defaults": { "unit": "bytes" } }
    }
  ]
}
`
}
