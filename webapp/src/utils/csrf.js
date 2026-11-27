export function getCsrfToken() {
    if (typeof document === 'undefined') return '';
    const cookies = document.cookie.split(';');
    for (let c of cookies) {
        c = c.trim();
        if (c.startsWith('MMCSRF=')) {
            return c.substring('MMCSRF='.length);
        }
    }
    return '';
}
