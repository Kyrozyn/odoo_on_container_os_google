# -*- coding: utf-8 -*-
import html
import time
from odoo import models, fields, api, _
from odoo.exceptions import AccessError


class DryRunRollback(Exception):
    """Custom exception used to trigger safe savepoint rollback in dry-run mode."""
    pass


class ExecuteSql(models.TransientModel):
    _name = 'execute.sql'
    _description = 'Execute SQL Query'

    name = fields.Char(string="Name", default="Execute SQL", readonly=True)
    query = fields.Text(
        string="SQL Query",
        required=True,
        default="SELECT id, login, name, active FROM res_users ORDER BY id LIMIT 10;",
        help="Input raw SQL query to execute.",
    )
    max_rows = fields.Integer(
        string="Max Rows to Fetch",
        default=100,
        help="Limit the number of fetched rows in SELECT queries to prevent browser freezing.",
    )
    dry_run = fields.Boolean(
        string="Dry Run (Auto-Rollback)",
        default=False,
        help="Execute query inside a transaction savepoint and roll back automatically, ensuring no changes persist.",
    )
    row_count = fields.Integer(string="Rows Count / Affected", readonly=True)
    execution_time = fields.Float(string="Duration (ms)", readonly=True)
    has_error = fields.Boolean(string="Has Error", readonly=True)
    result_html = fields.Html(
        string="Query Result",
        readonly=True,
        sanitize=False,
    )

    def action_execute(self):
        self.ensure_one()
        # Security check: only system administrators
        if not self.env.user.has_group('base.group_system'):
            raise AccessError(_("Access Denied: Only System Administrators can execute direct SQL queries."))

        raw_query = (self.query or "").strip()
        if not raw_query:
            self.write({
                'has_error': True,
                'row_count': 0,
                'execution_time': 0.0,
                'result_html': '<div class="alert alert-warning mb-0">Please enter an SQL query to execute.</div>',
            })
            return self._reopen_form()

        max_rows = self.max_rows if (self.max_rows and self.max_rows > 0) else 100
        start_time = time.time()
        duration_ms = 0.0
        columns = []
        rows = []
        row_count = 0
        is_select = False
        is_dry_run_done = False

        try:
            with self.env.cr.savepoint():
                self.env.cr.execute(raw_query)
                duration_ms = round((time.time() - start_time) * 1000, 2)

                if self.env.cr.description:
                    is_select = True
                    columns = [col[0] for col in self.env.cr.description]
                    rows = self.env.cr.fetchmany(max_rows)
                    row_count = len(rows)
                else:
                    is_select = False
                    row_count = self.env.cr.rowcount if self.env.cr.rowcount >= 0 else 0

                if self.dry_run:
                    raise DryRunRollback()

        except DryRunRollback:
            is_dry_run_done = True
            duration_ms = round((time.time() - start_time) * 1000, 2)
        except Exception as e:
            duration_ms = round((time.time() - start_time) * 1000, 2)
            self.write({
                'has_error': True,
                'row_count': 0,
                'execution_time': duration_ms,
                'result_html': self._format_error_html(str(e), duration_ms),
            })
            return self._reopen_form()

        if is_select:
            formatted_html = self._format_table_html(columns, rows, row_count, max_rows, duration_ms, is_dry_run_done)
        else:
            formatted_html = self._format_dml_html(row_count, duration_ms, is_dry_run_done)

        self.write({
            'has_error': False,
            'row_count': row_count,
            'execution_time': duration_ms,
            'result_html': formatted_html,
        })
        return self._reopen_form()

    def action_clear(self):
        self.ensure_one()
        self.write({
            'query': '',
            'result_html': False,
            'row_count': 0,
            'execution_time': 0.0,
            'has_error': False,
        })
        return self._reopen_form()

    def _format_table_html(self, columns, rows, row_count, max_rows, duration_ms, is_dry_run=False):
        parts = []

        dry_badge = '<span class="badge bg-warning text-dark me-2" style="font-size: 11px;">Dry Run (Rolled Back)</span>' if is_dry_run else ''
        limit_notice = f'<span class="text-muted small ms-2">(Displaying first {max_rows} rows)</span>' if row_count >= max_rows else ''

        header_summary = f"""
        <div class="d-flex justify-content-between align-items-center p-2 mb-2 bg-light border rounded">
            <div>
                {dry_badge}
                <span class="badge bg-success me-2" style="font-size: 11px;">{row_count} row(s) returned</span>
                <span class="text-muted small">Executed in <strong>{duration_ms} ms</strong></span>
                {limit_notice}
            </div>
            <div class="text-muted small">
                {len(columns)} column(s)
            </div>
        </div>
        """
        parts.append(header_summary)

        parts.append('<div class="table-responsive" style="max-height: 520px; overflow: auto; border: 1px solid #dee2e6; border-radius: 4px;">')
        parts.append('<table class="table table-sm table-striped table-bordered table-hover mb-0" style="font-family: SFMono-Regular, Menlo, Monaco, Consolas, monospace; font-size: 12px; white-space: nowrap;">')

        # Table Header
        parts.append('<thead class="table-dark" style="position: sticky; top: 0; z-index: 2;"><tr>')
        parts.append('<th style="width: 45px; text-align: center;">#</th>')
        for col in columns:
            parts.append(f'<th class="px-2 py-1">{html.escape(str(col))}</th>')
        parts.append('</tr></thead>')

        # Table Body
        parts.append('<tbody>')
        for idx, row in enumerate(rows, 1):
            parts.append('<tr>')
            parts.append(f'<td style="text-align: center; color: #888;" class="px-2 py-1">{idx}</td>')
            for val in row:
                if val is None:
                    cell = '<span class="text-muted fst-italic">NULL</span>'
                elif isinstance(val, bool):
                    badge_class = 'bg-success' if val else 'bg-secondary'
                    cell = f'<span class="badge {badge_class}" style="font-size: 10px;">{val}</span>'
                else:
                    str_val = str(val)
                    if len(str_val) > 200:
                        cell = f'<span title="{html.escape(str_val)}">{html.escape(str_val[:200])}...</span>'
                    else:
                        cell = html.escape(str_val)
                parts.append(f'<td class="px-2 py-1">{cell}</td>')
            parts.append('</tr>')
        parts.append('</tbody>')
        parts.append('</table></div>')

        return "".join(parts)

    def _format_dml_html(self, row_count, duration_ms, is_dry_run=False):
        if is_dry_run:
            badge = '<span class="badge bg-warning text-dark me-2">Dry Run Mode</span>'
            status_text = f"Query executed successfully and changes were <strong>rolled back</strong>. ({row_count} row(s) affected)."
            alert_class = "alert-warning"
        else:
            badge = '<span class="badge bg-success me-2">Committed</span>'
            status_text = f"Query executed successfully. <strong>{row_count}</strong> row(s) affected."
            alert_class = "alert-success"

        return f"""
        <div class="alert {alert_class} d-flex align-items-center mb-0" role="alert">
            <div>
                {badge}
                <span>{status_text}</span>
                <span class="text-muted small ms-2">(Duration: <strong>{duration_ms} ms</strong>)</span>
            </div>
        </div>
        """

    def _format_error_html(self, error_message, duration_ms):
        escaped_err = html.escape(error_message)
        return f"""
        <div class="alert alert-danger mb-0" role="alert">
            <h6 class="alert-heading fw-bold mb-1">
                <i class="fa fa-exclamation-triangle me-1"></i> PostgreSQL Execution Error
            </h6>
            <div class="text-muted small mb-2">Duration: {duration_ms} ms</div>
            <pre class="mb-0 p-2 bg-white text-danger border rounded" style="font-family: SFMono-Regular, Menlo, Monaco, Consolas, monospace; font-size: 12px; white-space: pre-wrap; word-break: break-all;">{escaped_err}</pre>
        </div>
        """

    def _reopen_form(self):
        return {
            'type': 'ir.actions.act_window',
            'name': 'Execute SQL',
            'res_model': self._name,
            'res_id': self.id,
            'view_mode': 'form',
            'target': 'current',
        }
