import React, { useState, useEffect } from 'react';

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

export default function LDAPAccountManager() {
    const [loading, setLoading] = useState(true);
    const [actionLoading, setActionLoading] = useState(null);
    const [users, setUsers] = useState([]);
    const [message, setMessage] = useState(null);

    const loadLDAPUsers = async () => {
        setLoading(true);
        setMessage(null);
        try {
            const res = await fetch(`/plugins/${PLUGIN_ID}/api/v1/admin/ldap-users`, {
                headers: { 'X-Requested-With': 'XMLHttpRequest' },
                credentials: 'same-origin',
            });
            if (!res.ok) {
                throw new Error(`Server returned HTTP ${res.status}`);
            }
            const data = await res.json();
            setUsers(data.users || []);
        } catch (err) {
            setMessage({ type: 'danger', text: `Failed to load LDAP accounts: ${err.message}` });
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        loadLDAPUsers();
    }, []);

    const handleUnlock = async (userId = null) => {
        const isAll = !userId;
        if (isAll && !window.confirm('Are you sure you want to unlock all LDAP accounts? Their auth_service will be reset so they can be managed normally and link to Authentik.')) {
            return;
        }

        setActionLoading(userId || 'all-unlock');
        setMessage(null);

        try {
            const body = userId ? { user_ids: [userId] } : { user_ids: [] };
            const res = await fetch(`/plugins/${PLUGIN_ID}/api/v1/admin/unlock-ldap`, {
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
                throw new Error(`Failed to unlock: HTTP ${res.status}`);
            }

            const data = await res.json();
            setMessage({
                type: 'success',
                text: `Successfully unlocked ${data.unlocked || 1} account(s). They are now normal accounts that can be managed in System Console or linked to Authentik on login.`,
            });
            await loadLDAPUsers();
        } catch (err) {
            setMessage({ type: 'danger', text: err.message });
        } finally {
            setActionLoading(null);
        }
    };

    const handleDeactivate = async (userId = null) => {
        const isAll = !userId;
        if (isAll && !window.confirm('Are you sure you want to deactivate all leftover LDAP accounts?')) {
            return;
        }

        setActionLoading(userId || 'all-deactivate');
        setMessage(null);

        try {
            const body = userId ? { user_ids: [userId] } : { user_ids: [] };
            const res = await fetch(`/plugins/${PLUGIN_ID}/api/v1/admin/deactivate-ldap`, {
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
                throw new Error(`Failed to deactivate: HTTP ${res.status}`);
            }

            const data = await res.json();
            setMessage({
                type: 'success',
                text: `Successfully deactivated ${data.deactivated || 1} account(s).`,
            });
            await loadLDAPUsers();
        } catch (err) {
            setMessage({ type: 'danger', text: err.message });
        } finally {
            setActionLoading(null);
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
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '12px' }}>
                <div>
                    <h4 style={{ margin: 0, fontWeight: 600 }}>Legacy LDAP Accounts</h4>
                    <p style={{ margin: '4px 0 0', color: 'var(--center-channel-color-64, #666)' }}>
                        Mattermost locks account modification and deactivation for accounts marked as managed by LDAP (<code>auth_service = 'ldap'</code>).
                        You can unlock them to standard accounts (so they can be managed in System Console &gt; Users or auto-link to Authentik on next login), or deactivate them.
                    </p>
                </div>
                <button
                    type="button"
                    className="btn btn-default btn-sm"
                    onClick={loadLDAPUsers}
                    disabled={loading || actionLoading !== null}
                    style={{ whiteSpace: 'nowrap', marginLeft: '12px' }}
                >
                    {loading ? 'Refreshing...' : 'Refresh List'}
                </button>
            </div>

            {message && (
                <div
                    className={`alert alert-${message.type}`}
                    style={{ padding: '8px 12px', marginBottom: '12px', borderRadius: '4px' }}
                >
                    {message.text}
                </div>
            )}

            {loading ? (
                <div style={{ padding: '16px', textAlign: 'center', color: '#888' }}>
                    Scanning users for LDAP lock...
                </div>
            ) : users.length === 0 ? (
                <div style={{
                    padding: '16px',
                    background: 'rgba(40, 167, 69, 0.08)',
                    borderRadius: '4px',
                    color: '#28a745',
                    fontWeight: 500,
                }}>
                    ✓ No LDAP-locked accounts detected. All user accounts are standard or SSO accounts.
                </div>
            ) : (
                <div>
                    <div style={{
                        display: 'flex',
                        gap: '8px',
                        marginBottom: '12px',
                        alignItems: 'center',
                    }}>
                        <span style={{ fontWeight: 600, marginRight: '8px' }}>
                            {users.length} account{users.length === 1 ? '' : 's'} found:
                        </span>
                        <button
                            type="button"
                            className="btn btn-primary btn-sm"
                            onClick={() => handleUnlock(null)}
                            disabled={actionLoading !== null}
                        >
                            {actionLoading === 'all-unlock' ? 'Unlocking...' : `Unlock All (${users.length})`}
                        </button>
                        <button
                            type="button"
                            className="btn btn-danger btn-sm"
                            onClick={() => handleDeactivate(null)}
                            disabled={actionLoading !== null}
                        >
                            {actionLoading === 'all-deactivate' ? 'Deactivating...' : `Deactivate All (${users.length})`}
                        </button>
                    </div>

                    <table className="table" style={{ margin: 0, fontSize: '12px' }}>
                        <thead>
                            <tr style={{ background: 'var(--center-channel-color-08, #f5f5f5)' }}>
                                <th style={{ padding: '6px 10px' }}>Username</th>
                                <th style={{ padding: '6px 10px' }}>Email</th>
                                <th style={{ padding: '6px 10px' }}>Name</th>
                                <th style={{ padding: '6px 10px', textAlign: 'right' }}>Actions</th>
                            </tr>
                        </thead>
                        <tbody>
                            {users.map((u) => {
                                const isRowBusy = actionLoading === u.id;
                                return (
                                    <tr key={u.id}>
                                        <td style={{ padding: '8px 10px', fontWeight: 500 }}>{u.username}</td>
                                        <td style={{ padding: '8px 10px' }}>{u.email}</td>
                                        <td style={{ padding: '8px 10px' }}>
                                            {`${u.first_name || ''} ${u.last_name || ''}`.trim() || '—'}
                                        </td>
                                        <td style={{ padding: '8px 10px', textAlign: 'right', whiteSpace: 'nowrap' }}>
                                            <button
                                                type="button"
                                                className="btn btn-default btn-xs"
                                                onClick={() => handleUnlock(u.id)}
                                                disabled={actionLoading !== null}
                                                style={{ marginRight: '6px' }}
                                                title="Removes LDAP lock so user can log in or link to Authentik"
                                            >
                                                {isRowBusy ? '...' : 'Unlock'}
                                            </button>
                                            <button
                                                type="button"
                                                className="btn btn-danger btn-xs"
                                                onClick={() => handleDeactivate(u.id)}
                                                disabled={actionLoading !== null}
                                            >
                                                {isRowBusy ? '...' : 'Deactivate'}
                                            </button>
                                        </td>
                                    </tr>
                                );
                            })}
                        </tbody>
                    </table>
                </div>
            )}
        </div>
    );
}
