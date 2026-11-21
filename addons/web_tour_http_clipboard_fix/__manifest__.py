{
    "name": "Web Tour HTTP Clipboard Fix",
    "version": "20.0.1.0.0",
    "category": "Hidden/Tools",
    "summary": "Prevent @web_tour/tour_helpers/tour_helpers_clipboard crash over plain HTTP",
    "description": """
Provides a safe fallback for navigator.clipboard (writeText / readText) when Odoo
is accessed over a non-secure HTTP origin (such as a LAN IP address), preventing
@web_tour/tour_helpers/tour_helpers_clipboard from throwing a TypeError on load.
""",
    "author": "Custom Fix",
    "license": "LGPL-3",
    "depends": ["web", "web_tour"],
    "installable": True,
    "auto_install": False,
    "assets": {
        "web_tour.interactive": [
            ("prepend", "web_tour_http_clipboard_fix/static/src/js/clipboard_polyfill.js"),
        ],
    },
}
