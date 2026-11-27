import React, { useState } from 'react';

const PLUGIN_ID = 'io.christopherturner.mattermost-authentik';

function getOrigin() {
    if (typeof window !== 'undefined' && window.location && window.location.origin) {
        return window.location.origin;
    }
    return 'https://mattermost.company';
}

export default function RedirectURIHelper() {
    const [copiedKey, setCopiedKey] = useState(null);
    const origin = getOrigin();

    const urls = [
        {
            key: 'callback',
            label: 'Redirect URI (OAuth2 Callback)',
            value: `${origin}/plugins/${PLUGIN_ID}/oauth2/callback`,
        },
        {
            key: 'backchannel',
            label: 'Back-Channel Logout URI (Optional, for OIDC SLO)',
            value: `${origin}/plugins/${PLUGIN_ID}/oauth2/backchannel-logout`,
        },
    ];

    const handleCopy = async (key, text) => {
        try {
            if (navigator && navigator.clipboard && navigator.clipboard.writeText) {
                await navigator.clipboard.writeText(text);
            } else {
                const el = document.createElement('textarea');
                el.value = text;
                el.setAttribute('readonly', '');
                el.style.position = 'absolute';
                el.style.left = '-9999px';
                document.body.appendChild(el);
                el.select();
                document.execCommand('copy');
                document.body.removeChild(el);
            }
            setCopiedKey(key);
            setTimeout(() => {
                setCopiedKey((prev) => (prev === key ? null : prev));
            }, 2000);
        } catch {
            // Clipboard access denied
        }
    };

    return (
        <div className="form-group" style={{ marginBottom: '24px' }}>
            <label className="control-label col-sm-4" style={{ fontWeight: 600, color: 'var(--center-channel-color, #3d3c40)' }}>
                Authentik Integration URLs:
            </label>
            <div className="col-sm-8">
                {urls.map((item) => (
                    <div key={item.key} style={{ marginBottom: '12px' }}>
                        <div style={{ fontSize: '12px', fontWeight: 600, marginBottom: '4px', color: 'var(--center-channel-color-72, #555)' }}>
                            {item.label}
                        </div>
                        <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
                            <input
                                type="text"
                                className="form-control"
                                readOnly
                                value={item.value}
                                onClick={(e) => e.target.select()}
                                style={{ fontFamily: 'monospace', fontSize: '12px' }}
                            />
                            <button
                                type="button"
                                className="btn btn-default"
                                onClick={() => handleCopy(item.key, item.value)}
                                style={{ whiteSpace: 'nowrap', minWidth: '80px' }}
                            >
                                {copiedKey === item.key ? 'Copied!' : 'Copy'}
                            </button>
                        </div>
                    </div>
                ))}
            </div>
        </div>
    );
}
