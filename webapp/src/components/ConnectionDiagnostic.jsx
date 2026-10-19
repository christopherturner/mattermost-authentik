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

    const runDiagnostic = async () => {
        setRunning(true);
        setError(null);
        setResults(null);

        try {
            const res = await fetch(`/plugins/${PLUGIN_ID}/api/v1/admin/diagnose`, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'X-Requested-With': 'XMLHttpRequest',
                    'X-CSRF-Token': getCsrfToken(),
                },
                credentials: 'same-origin',
                body: JSON.stringify({}),
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
        <div className="form-group" style={{ marginBottom: '24px' }}>
            <label className="control-label col-sm-4" style={{ fontWeight: 600, color: 'var(--center-channel-color, #3d3c40)' }}>
                Test Connection:
            </label>
            <div className="col-sm-8">
                <button
                    type="button"
                    className="btn btn-default"
                    onClick={runDiagnostic}
                    disabled={running}
                    style={{ minWidth: '150px', fontWeight: 500 }}
                >
                    {running ? 'Testing...' : '⚡ Test Connection'}
                </button>
                <div className="help-text" style={{ marginTop: '6px', fontSize: '12px', color: 'var(--center-channel-color-64, #777)' }}>
                    Verify network connectivity, OpenID Connect discovery, JWKS cryptographic keys, and credentials against your Authentik server.
                </div>

                {error && (
                    <div className="alert alert-danger" style={{ marginTop: '10px', padding: '8px 12px', fontSize: '12px', borderRadius: '4px' }}>
                        {error}
                    </div>
                )}

                {results && (
                    <div style={{
                        marginTop: '10px',
                        padding: '12px 14px',
                        borderRadius: '4px',
                        background: 'var(--center-channel-color-04, #f8f9fa)',
                        border: '1px solid var(--center-channel-color-12, #e5e5e5)',
                        fontSize: '12px',
                    }}>
                        <div style={{
                            fontWeight: 600,
                            color: results.success ? '#28a745' : '#dc3545',
                            marginBottom: '8px',
                            display: 'flex',
                            justifyContent: 'space-between',
                            alignItems: 'center',
                        }}>
                            <span>
                                {results.success ? '✓ All Diagnostic Checks Passed' : '⚠ Connection Issues Detected'}
                            </span>
                            {results.effective_issuer && (
                                <span style={{ fontSize: '11px', color: '#888', fontWeight: 'normal' }}>
                                    Issuer: {results.effective_issuer}
                                </span>
                            )}
                        </div>

                        {results.steps && results.steps.map((step, idx) => {
                            const isOk = step.status === 'ok';
                            const isWarn = step.status === 'warning';
                            const mark = isOk ? '✓' : isWarn ? '⚠' : '✗';
                            const color = isOk ? '#28a745' : isWarn ? '#ffc107' : '#dc3545';

                            return (
                                <div key={idx} style={{ marginTop: '4px', color: 'var(--center-channel-color-80, #444)' }}>
                                    <span style={{ color, fontWeight: 700, marginRight: '6px' }}>{mark}</span>
                                    <strong>{step.name}:</strong> {step.details}
                                    {step.latency_ms !== undefined && step.latency_ms > 0 && (
                                        <span style={{ color: '#888', marginLeft: '6px' }}>({step.latency_ms}ms)</span>
                                    )}
                                </div>
                            );
                        })}
                    </div>
                )}
            </div>
        </div>
    );
}
