export function getSafeRedirectPath() {
    try {
        const raw = new URLSearchParams(window.location.search).get('redirect_to');
        if (!raw) {
            return '/';
        }
        const parsed = new URL(raw, window.location.origin);
        if (parsed.origin !== window.location.origin) {
            return '/';
        }
        if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
            return '/';
        }
        const safePath = parsed.pathname + parsed.search + parsed.hash;
        if (!safePath.startsWith('/') || safePath.startsWith('//') || safePath.includes('\\')) {
            return '/';
        }
        return safePath;
    } catch {
        return '/';
    }
}
