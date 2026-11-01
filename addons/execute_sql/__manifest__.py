# -*- coding: utf-8 -*-
{
    'name': 'Execute SQL',
    'version': '1.0.0',
    'category': 'Technical',
    'summary': 'Execute direct SQL queries from Technical Settings in Developer Mode',
    'description': """
Execute SQL
===========
Allows system administrators to run raw PostgreSQL queries directly within the Odoo interface:
* Located in Settings > Technical > Execute SQL.
* Visible only when Developer Mode (debug=1) is enabled.
* Supports both SELECT queries (rendered as an interactive table) and DML/DDL (INSERT/UPDATE/DELETE/ALTER).
* Includes Dry Run mode with automatic transaction savepoint rollback.
* Gracefully catches and displays SQL errors without aborting the main database connection.
* Restricted strictly to System Administrators (base.group_system).
    """,
    'author': 'Custom',
    'license': 'LGPL-3',
    'depends': ['base'],
    'data': [
        'security/ir.model.access.csv',
        'views/execute_sql_views.xml',
        'views/execute_sql_menus.xml',
    ],
    'installable': True,
    'application': False,
    'auto_install': False,
}
