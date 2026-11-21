/** @odoo-module **/

/**
 * Polyfill navigator.clipboard in non-secure HTTP contexts (e.g. http://192.168.x.x:8069).
 *
 * Browsers leave navigator.clipboard undefined outside HTTPS/localhost, which causes
 * @web_tour/tour_helpers/tour_helpers_clipboard to throw a TypeError on module load
 * when web_tour.interactive is loaded.
 */
function createFallbackClipboard() {
    return {
        writeText(text) {
            try {
                if (document?.body && typeof document.execCommand === "function") {
                    const textarea = document.createElement("textarea");
                    textarea.value = text ?? "";
                    textarea.setAttribute("readonly", "");
                    textarea.style.position = "fixed";
                    textarea.style.opacity = "0";
                    textarea.style.pointerEvents = "none";
                    document.body.appendChild(textarea);
                    textarea.select();
                    document.execCommand("copy");
                    textarea.remove();
                }
            } catch {
                // Ignore copy errors in non-secure contexts
            }
            return Promise.resolve();
        },
        readText() {
            return Promise.resolve("");
        },
    };
}

function ensureClipboardPolyfill(nav) {
    if (!nav || (nav.clipboard && typeof nav.clipboard.writeText === "function")) {
        return;
    }
    const fallbackClipboard = createFallbackClipboard();
    const descriptor = {
        configurable: true,
        enumerable: true,
        get() {
            return fallbackClipboard;
        },
    };
    try {
        Object.defineProperty(nav, "clipboard", descriptor);
    } catch {
        if (typeof Navigator !== "undefined" && Navigator.prototype) {
            try {
                Object.defineProperty(Navigator.prototype, "clipboard", descriptor);
            } catch {
                // Ignore if the host environment disallows redefining Navigator.prototype.clipboard
            }
        }
    }
}

ensureClipboardPolyfill(globalThis.navigator);
