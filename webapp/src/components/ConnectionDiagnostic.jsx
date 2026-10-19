import React, { useState } from 'react';

const PLUGIN_ID = 'io.christopherturner.mattermost-authentik';

function getCsrfToken() {
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

export default function ConnectionDiagnostic() {
    const [running, setRunning] = useState(false);
    const [results, setResults] = useState(null);
    const [error, setError] = useState(null);
    const [copied, setCopied] = useState(false);
    const [showCustomForm, setShowCustomForm] = useState(false);

    const [customParams, setCustomParams] = useState({
        authentik_url: '',
        application_slug: '',
        issuer_url: '',
        client_id: '',
        client_secret: '',
        custom_cert: '',
        skip_tls_verify: false,
    });

    const callbackUrl = results?.callback_url || `${typeof window !== 'undefined' ? window.location.origin : ''}/plugins/${PLUGIN_ID}/oauth2/callback`;

    const handleCopyCallback = () => {
        if (navigator.clipboard) {
            navigator.clipboard.writeText(callbackUrl);
            setCopied(true);
            setTimeout(() => setCopied(false), 2500);
        }
    };

    const runDiagnostic = async () => {
        setRunning(true);
        setError(null);
        setResults(null);

        try {
            const body = showCustomForm ? customParams : {};
            const res = await fetch(`/plugins/${PLUGIN_ID}/api/v1/admin/diagnose`, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'X-Requested-With': 'XMLHttpRequest',
                    'X-CSRF-Token': getCsrfToken(),
                },
                credentials: 'same-origin',
                body: JSON.stringify(body),
            });

            if (!res.ok) {
                throw new Error(`Diagnostic endpoint returned HTTP ${res.status}`);
            }

            const data = await res.json();
            setResults(data);
        } catch (err) {
            setError(err.message);
        } finally {
            setRunning(false);
        }
    };

    return (
        <div style={{
            background: 'var(--center-channel-bg, #fff)',
            border: '1px solid var(--center-channel-color-16, #e5e5e5)',
            borderRadius: '6px',
            padding: '16px',
            marginTop: '8px',
            marginBottom: '16px',
            fontSize: '13px',
        }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: '12px' }}>
                <div>
                    <h4 style={{ margin: 0, fontWeight: 600 }}>Authentik Connection &amp; Credentials Diagnostics</h4>
                    <p style={{ margin: '4px 0 0', color: 'var(--center-channel-color-64, #666)' }}>
                        Test network connectivity, OIDC discovery endpoints, JWKS signing keys, and Client ID/Secret verification against Authentik.
                    </p>
                </div>
                <button
                    type="button"
                    className="btn btn-primary btn-sm"
                    onClick={runDiagnostic}
                    disabled={running}
                    style={{ whiteSpace: 'nowrap', marginLeft: '12px' }}
                >
                    {running ? 'Testing Connection...' : '⚡ Test Connection Now'}
                </button>
            </div>

            {/* Quick Redirect URI helper */}
            <div style={{
                background: 'var(--center-channel-color-08, #f8f9fa)',
                padding: '10px 12px',
                borderRadius: '4px',
                marginBottom: '12px',
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
            }}>
                <div style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', marginRight: '8px' }}>
                    <span style={{ fontWeight: 600, color: 'var(--center-channel-color-80, #333)' }}>Authentik Redirect URI: </span>
                    <code style={{ fontSize: '12px' }}>{callbackUrl}</code>
                </div>
                <button
                    type="button"
                    className="btn btn-default btn-xs"
                    onClick={handleCopyCallback}
                    style={{ flexShrink: 0 }}
                >
                    {copied ? '✓ Copied' : 'Copy URI'}
                </button>
            </div>

            {/* Toggle custom test parameters */}
            <div style={{ marginBottom: '12px' }}>
                <a
                    href="#toggle-custom"
                    onClick={(e) => { e.preventDefault(); setShowCustomForm(!showCustomForm); }}
                    style={{ fontSize: '12px', textDecoration: 'none', color: '#166de0' }}
                >
                    {showCustomForm ? '▾ Use saved plugin settings instead' : '▸ Test custom / unsaved parameters before saving'}
                </a>

                {showCustomForm && (
                    <div style={{
                        marginTop: '8px',
                        padding: '12px',
                        background: 'var(--center-channel-color-04, #fbfbfb)',
                        border: '1px solid var(--center-channel-color-12, #eee)',
                        borderRadius: '4px',
                    }}>
                        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '8px', marginBottom: '8px' }}>
                            <div>
                                <label style={{ display: 'block', fontSize: '11px', fontWeight: 600 }}>Authentik Base URL</label>
                                <input
                                    type="text"
                                    className="form-control input-sm"
                                    placeholder="https://authentik.company"
                                    value={customParams.authentik_url}
                                    onChange={(e) => setCustomParams({ ...customParams, authentik_url: e.target.value })}
                                />
                            </div>
                            <div>
                                <label style={{ display: 'block', fontSize: '11px', fontWeight: 600 }}>Application Slug</label>
                                <input
                                    type="text"
                                    className="form-control input-sm"
                                    placeholder="mattermost"
                                    value={customParams.application_slug}
                                    onChange={(e) => setCustomParams({ ...customParams, application_slug: e.target.value })}
                                />
                            </div>
                        </div>
                        <div style={{ marginBottom: '8px' }}>
                            <label style={{ display: 'block', fontSize: '11px', fontWeight: 600 }}>Custom Issuer URL (Optional)</label>
                            <input
                                type="text"
                                className="form-control input-sm"
                                placeholder="Overrides base URL + slug if specified"
                                value={customParams.issuer_url}
                                onChange={(e) => setCustomParams({ ...customParams, issuer_url: e.target.value })}
                            />
                        </div>
                        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '8px' }}>
                            <div>
                                <label style={{ display: 'block', fontSize: '11px', fontWeight: 600 }}>Client ID</label>
                                <input
                                    type="text"
                                    className="form-control input-sm"
                                    placeholder="Authentik OAuth Client ID"
                                    value={customParams.client_id}
                                    onChange={(e) => setCustomParams({ ...customParams, client_id: e.target.value })}
                                />
                            </div>
                            <div>
                                <label style={{ display: 'block', fontSize: '11px', fontWeight: 600 }}>Client Secret</label>
                                <input
                                    type="password"
                                    className="form-control input-sm"
                                    placeholder="Authentik OAuth Client Secret"
                                    value={customParams.client_secret}
                                    onChange={(e) => setCustomParams({ ...customParams, client_secret: e.target.value })}
                                />
                            </div>
                        </div>
                        <div style={{ marginTop: '8px' }}>
                            <label style={{ display: 'block', fontSize: '11px', fontWeight: 600 }}>Custom Root CA / Certificate (Optional PEM)</label>
                            <textarea
                                className="form-control input-sm"
                                rows="3"
                                placeholder="-----BEGIN CERTIFICATE----- ... -----END CERTIFICATE-----"
                                value={customParams.custom_cert}
                                onChange={(e) => setCustomParams({ ...customParams, custom_cert: e.target.value })}
                                style={{ fontFamily: 'monospace', fontSize: '11px' }}
                            />
                        </div>
                        <div style={{ marginTop: '8px' }}>
                            <label style={{ display: 'flex', alignItems: 'center', fontSize: '12px', cursor: 'pointer', margin: 0 }}>
                                <input
                                    type="checkbox"
                                    checked={customParams.skip_tls_verify}
                                    onChange={(e) => setCustomParams({ ...customParams, skip_tls_verify: e.target.checked })}
                                    style={{ marginRight: '6px' }}
                                />
                                Skip TLS Certificate Verification (Insecure)
                            </label>
                        </div>
                    </div>
                )}
            </div>

            {error && (
                <div className="alert alert-danger" style={{ padding: '8px 12px', marginBottom: '12px' }}>
                    {error}
                </div>
            )}

            {results && (
                <div>
                    <div style={{
                        padding: '10px 14px',
                        borderRadius: '4px',
                        fontWeight: 600,
                        marginBottom: '12px',
                        background: results.success ? 'rgba(40, 167, 69, 0.1)' : 'rgba(220, 53, 69, 0.1)',
                        color: results.success ? '#28a745' : '#dc3545',
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'space-between',
                    }}>
                        <span>
                            {results.success ? '✓ All Diagnostics Passed: Connection to Authentik is operational' : '⚠ Diagnostic Issues Detected'}
                        </span>
                        {results.effective_issuer && (
                            <span style={{ fontSize: '11px', color: '#666', fontWeight: 'normal' }}>
                                Issuer: {results.effective_issuer}
                            </span>
                        )}
                    </div>

                    <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
                        {results.steps && results.steps.map((step, idx) => {
                            const isOk = step.status === 'ok';
                            const isWarn = step.status === 'warning';
                            const badgeColor = isOk ? '#28a745' : isWarn ? '#ffc107' : '#dc3545';
                            const badgeText = isOk ? 'PASS' : isWarn ? 'WARN' : 'FAIL';

                            return (
                                <div key={idx} style={{
                                    border: '1px solid var(--center-channel-color-12, #eee)',
                                    borderRadius: '4px',
                                    padding: '10px 12px',
                                    background: 'var(--center-channel-bg, #fff)',
                                }}>
                                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '4px' }}>
                                        <div style={{ fontWeight: 600, fontSize: '13px' }}>
                                            <span style={{
                                                display: 'inline-block',
                                                background: badgeColor,
                                                color: '#fff',
                                                padding: '1px 6px',
                                                borderRadius: '3px',
                                                fontSize: '10px',
                                                fontWeight: 'bold',
                                                marginRight: '8px',
                                            }}>
                                                {badgeText}
                                            </span>
                                            {step.name}
                                        </div>
                                        {step.latency_ms !== undefined && step.latency_ms > 0 && (
                                            <span style={{ fontSize: '11px', color: '#888' }}>
                                                {step.latency_ms} ms
                                            </span>
                                        )}
                                    </div>
                                    <div style={{ fontSize: '12px', color: 'var(--center-channel-color-72, #555)', marginTop: '2px' }}>
                                        {step.details}
                                    </div>
                                    {step.url && (
                                        <div style={{ fontSize: '11px', color: '#888', marginTop: '3px', wordBreak: 'break-all' }}>
                                            URL: <code>{step.url}</code>
                                        </div>
                                    )}
                                </div>
                            );
                        })}
                    </div>
                </div>
            )}
        </div>
    );
}
