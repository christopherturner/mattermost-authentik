import React, { useState } from 'react';

const MANDATORY_SCOPES = ['openid', 'profile', 'email', 'groups'];

const RECOMMENDED_SCOPES = [
    { name: 'openid', required: true, desc: 'OpenID Connect identity (Required)' },
    { name: 'profile', required: true, desc: 'User profile, names, and Authentik groups (Required)' },
    { name: 'email', required: true, desc: 'Email address and verification status (Required)' },
    { name: 'groups', required: true, desc: 'Authentik group memberships for admin sync (Required)' },
];

export default function ScopeSelector(props) {
    const { id, value, onChange, disabled, setSaveNeeded } = props;
    const [customInput, setCustomInput] = useState('');

    const currentScopes = (typeof value === 'string' && value.trim())
        ? value.trim().split(/\s+/).filter(Boolean)
        : ['openid', 'profile', 'email', 'groups'];

    const updateScopes = (newScopes) => {
        for (const req of MANDATORY_SCOPES) {
            if (!newScopes.includes(req)) {
                newScopes.push(req);
            }
        }
        const joined = newScopes.join(' ');
        if (onChange) {
            onChange(id, joined);
        }
        if (setSaveNeeded) {
            setSaveNeeded();
        }
    };

    const handleRemoveScope = (scopeToRemove) => {
        if (disabled || MANDATORY_SCOPES.includes(scopeToRemove)) return;
        const filtered = currentScopes.filter((s) => s !== scopeToRemove);
        updateScopes(filtered);
    };

    const handleAddScope = (scopeToAdd) => {
        const clean = (scopeToAdd || customInput).trim().toLowerCase();
        if (disabled || !clean || currentScopes.includes(clean)) {
            setCustomInput('');
            return;
        }
        const updated = [...currentScopes, clean];
        updateScopes(updated);
        setCustomInput('');
    };

    const handleKeyDown = (e) => {
        if (e.key === 'Enter') {
            e.preventDefault();
            handleAddScope(customInput);
        }
    };

    const availableSuggestions = RECOMMENDED_SCOPES.filter(
        (s) => !currentScopes.includes(s.name)
    );

    return (
        <div className="form-group" style={{ marginBottom: '24px' }}>
            {/* Left Label column - matches Mattermost native settings */}
            <label className="control-label col-sm-4" style={{ fontWeight: 600, color: 'var(--center-channel-color, #3d3c40)' }}>
                Scopes:
            </label>

            {/* Right Control column - matches Mattermost native input width and alignment */}
            <div className="col-sm-8">
                {/* Pill Container - styled to blend with Mattermost inputs */}
                <div style={{
                    display: 'flex',
                    flexWrap: 'wrap',
                    gap: '6px',
                    alignItems: 'center',
                    padding: '6px 10px',
                    background: 'var(--center-channel-bg, #fff)',
                    border: '1px solid var(--center-channel-color-20, #ccc)',
                    borderRadius: '4px',
                    minHeight: '38px',
                    boxShadow: 'inset 0 1px 1px rgba(0,0,0,0.075)',
                }}>
                    {currentScopes.map((scope) => {
                        const isRequired = MANDATORY_SCOPES.includes(scope);
                        return (
                            <span
                                key={scope}
                                style={{
                                    display: 'inline-flex',
                                    alignItems: 'center',
                                    padding: '3px 8px',
                                    borderRadius: '12px',
                                    fontSize: '12px',
                                    fontWeight: 500,
                                    background: isRequired
                                        ? 'rgba(253, 75, 45, 0.1)'
                                        : 'var(--center-channel-color-08, #f0f2f5)',
                                    color: isRequired
                                        ? '#FD4B2D'
                                        : 'var(--center-channel-color-88, #333)',
                                    border: isRequired
                                        ? '1px solid rgba(253, 75, 45, 0.3)'
                                        : '1px solid var(--center-channel-color-16, #ddd)',
                                }}
                            >
                                <span>{scope}</span>
                                {isRequired ? (
                                    <span style={{ marginLeft: '4px', fontSize: '10px', color: '#888' }}>
                                        (req)
                                    </span>
                                ) : (
                                    <button
                                        type="button"
                                        onClick={() => handleRemoveScope(scope)}
                                        disabled={disabled}
                                        title={`Remove ${scope}`}
                                        style={{
                                            background: 'none',
                                            border: 'none',
                                            marginLeft: '6px',
                                            padding: 0,
                                            cursor: disabled ? 'default' : 'pointer',
                                            color: '#888',
                                            fontSize: '14px',
                                            lineHeight: 1,
                                        }}
                                    >
                                        &times;
                                    </button>
                                )}
                            </span>
                        );
                    })}

                    {/* Inline scope entry */}
                    <div style={{ display: 'inline-flex', alignItems: 'center' }}>
                        <input
                            type="text"
                            value={customInput}
                            onChange={(e) => setCustomInput(e.target.value)}
                            onKeyDown={handleKeyDown}
                            disabled={disabled}
                            placeholder="+ add scope"
                            style={{
                                border: 'none',
                                outline: 'none',
                                fontSize: '12px',
                                padding: '3px 6px',
                                minWidth: '80px',
                                background: 'transparent',
                                color: 'var(--center-channel-color, #333)',
                            }}
                        />
                        {customInput.trim() && (
                            <button
                                type="button"
                                className="btn btn-default btn-xs"
                                onClick={() => handleAddScope(customInput)}
                                style={{ padding: '1px 6px', fontSize: '11px', borderRadius: '3px' }}
                            >
                                Add
                            </button>
                        )}
                    </div>
                </div>

                {/* Quick Add Suggestions */}
                {availableSuggestions.length > 0 && (
                    <div style={{ marginTop: '6px', fontSize: '11px', display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: '5px' }}>
                        <span style={{ color: 'var(--center-channel-color-56, #888)' }}>
                            Quick add:
                        </span>
                        {availableSuggestions.map((rec) => (
                            <button
                                key={rec.name}
                                type="button"
                                onClick={() => handleAddScope(rec.name)}
                                disabled={disabled}
                                title={rec.desc}
                                style={{
                                    background: 'none',
                                    border: 'none',
                                    padding: '0 4px',
                                    fontSize: '11px',
                                    cursor: 'pointer',
                                    color: '#2389D7',
                                    textDecoration: 'underline',
                                }}
                            >
                                + {rec.name}
                            </button>
                        ))}
                    </div>
                )}

                {/* Standard Help Text */}
                <div className="help-text" style={{ marginTop: '6px', fontSize: '12px', color: 'var(--center-channel-color-64, #777)' }}>
                    OAuth scopes to request from Authentik. Default: <code>openid profile email groups</code>. Click <code>&times;</code> to remove or type to add custom scopes.
                </div>
            </div>
        </div>
    );
}
