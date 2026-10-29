import React, { useState, useEffect } from 'react';
import AuthentikIcon from './AuthentikIcon';

const PLUGIN_ID = 'io.christopherturner.mattermost-authentik';

function getSafeRedirectPath() {
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

export default function AuthentikLoginButton() {
    const [config, setConfig] = useState(null);
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        const controller = new AbortController();

        fetch(`/plugins/${PLUGIN_ID}/config`, { signal: controller.signal })
            .then((res) => {
                if (!res.ok) {
                    throw new Error(`HTTP error ${res.status}`);
                }
                return res.json();
            })
            .then((data) => {
                setConfig(data);
                setLoading(false);
            })
            .catch((err) => {
                if (err.name !== 'AbortError') {
                    console.debug('Authentik plugin: config not loaded', err);
                    setLoading(false);
                }
            });

        return () => controller.abort();
    }, []);

    // Listen for storage signal from popup window completing authentication
    useEffect(() => {
        const onStorage = (e) => {
            if (e.key === 'mattermost_authentik_login' && e.newValue) {
                const safePath = getSafeRedirectPath();
                window.location.assign(safePath);
            }
        };

        window.addEventListener('storage', onStorage);
        return () => window.removeEventListener('storage', onStorage);
    }, []);

    if (loading || !config || !config.enable) {
        return null;
    }

    const handleClick = () => {
        const returnTo = getSafeRedirectPath();
        const base = `/plugins/${PLUGIN_ID}/oauth2/connect?return_to=${encodeURIComponent(returnTo)}`;

        // Check if running inside Mattermost Desktop app (Electron)
        const ua = window.navigator.userAgent;
        const isDesktopApp = ua.indexOf('Mattermost') !== -1 && ua.indexOf('Electron') !== -1;

        if (!isDesktopApp) {
            window.location.href = base;
            return;
        }

        // Desktop App: open via popup so Electron session handler intercepts OAuth
        const popup = window.open(`${base}&popup=1`, 'authentik_login', 'width=520,height=680');
        if (!popup) {
            window.location.href = base;
        }
    };

    const buttonStyle = {
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        width: '100%',
        padding: '12px 16px',
        marginBottom: '10px',
        marginTop: '6px',
        border: 'none',
        borderRadius: '4px',
        backgroundColor: config.button_color || '#FD4B2D',
        color: '#FFFFFF',
        fontSize: '14px',
        fontWeight: '600',
        cursor: 'pointer',
        boxSizing: 'border-box',
        transition: 'background-color 0.15s ease, opacity 0.15s ease',
        boxShadow: '0 2px 4px rgba(0, 0, 0, 0.1)',
    };

    return (
        <button
            style={buttonStyle}
            onClick={handleClick}
            onMouseOver={(e) => { e.currentTarget.style.opacity = '0.9'; }}
            onMouseOut={(e) => { e.currentTarget.style.opacity = '1'; }}
            type="button"
            id="authentik-login-btn"
        >
            <AuthentikIcon />
            <span>{config.button_text || 'Log in with Authentik'}</span>
        </button>
    );
}
