import React from 'react';
import ReactDOM from 'react-dom';
import AuthentikLoginButton from './components/AuthentikLoginButton';
import LDAPAccountManager from './components/LDAPAccountManager';
import ConnectionDiagnostic from './components/ConnectionDiagnostic';
import ScopeSelector from './components/ScopeSelector';

const PLUGIN_ID = 'io.christopherturner.mattermost-authentik';

class PluginClass {
    constructor() {
        this.observer = null;
        this.reactRoot = null;
        this.store = null;
    }

    initialize(registry, store) {
        this.store = store;

        // Fetch plugin config eagerly so brand colors and settings are readily available
        this.fetchPluginConfig();

        // Register custom login button component using official webapp registry hook
        if (registry && registry.registerCustomLoginButtonComponent) {
            registry.registerCustomLoginButtonComponent(AuthentikLoginButton);
        }

        // Register Admin Console custom settings
        if (registry && registry.registerAdminConsoleCustomSetting) {
            registry.registerAdminConsoleCustomSetting('LDAPAccountManager', LDAPAccountManager);
            registry.registerAdminConsoleCustomSetting('ConnectionDiagnostic', ConnectionDiagnostic);
            registry.registerAdminConsoleCustomSetting('Scope', ScopeSelector);
        }

        // Start DOM observer for:
        // 1. Fallback login button injection (if not mounted by registry)
        // 2. Profile and Account Settings email display
        this.setupDOMObserver();
    }

    setupDOMObserver() {
        if (typeof window === 'undefined' || typeof document === 'undefined') {
            return;
        }

        let scheduled = false;
        const throttledHandleDOMChanges = () => {
            if (scheduled) {
                return;
            }
            scheduled = true;
            window.requestAnimationFrame(() => {
                scheduled = false;
                this.injectLoginButtonFallback();
                this.enhanceProfileAndSecurityEmail();
            });
        };

        throttledHandleDOMChanges();

        this.observer = new MutationObserver(() => {
            throttledHandleDOMChanges();
        });

        this.observer.observe(document.body, { childList: true, subtree: true });
    }

    async fetchPluginConfig() {
        if (this.pluginConfig) {
            return this.pluginConfig;
        }
        try {
            const res = await fetch(`/plugins/${PLUGIN_ID}/config`);
            if (res.ok) {
                this.pluginConfig = await res.json();
            }
        } catch (e) {}
        return this.pluginConfig || {};
    }

    injectLoginButtonFallback() {
        const pathname = window.location.pathname;
        if (!pathname.startsWith('/login') && !pathname.includes('/login')) {
            return;
        }

        // 1. Check if Mattermost rendered the "No sign-in methods enabled" empty state screen
        const bodyText = (document.body && document.body.innerText) || '';
        const isNoMethodsScreen = bodyText.includes('sign-in methods enabled');

        if (isNoMethodsScreen) {
            if (document.getElementById('mattermost-authentik-empty-state-card')) {
                return;
            }

            const candidateContainers = [
                document.querySelector('.signup-body-content'),
                document.querySelector('.signup-body'),
                document.querySelector('.login-body-card'),
                document.querySelector('.login-body'),
                document.querySelector('.card'),
                document.querySelector('#root > div'),
                document.body,
            ];

            let targetCard = null;
            for (const c of candidateContainers) {
                if (c && c.innerText && c.innerText.includes('sign-in methods enabled')) {
                    targetCard = c;
                    break;
                }
            }

            if (!targetCard) {
                targetCard = document.querySelector('.signup-body-content') || document.body;
            }

            if (targetCard) {
                // Hide the empty state illustration and message
                Array.from(targetCard.children).forEach((child) => {
                    if (child.id !== 'mattermost-authentik-empty-state-card') {
                        child.style.display = 'none';
                    }
                });

                const card = document.createElement('div');
                card.id = 'mattermost-authentik-empty-state-card';
                card.style.cssText = 'max-width: 420px; width: 100%; margin: 60px auto; padding: 36px 28px; background: var(--center-channel-bg, #fff); border: 1px solid var(--center-channel-color-16, #e5e5e5); border-radius: 8px; text-align: center; box-shadow: 0 4px 16px rgba(0,0,0,0.06);';

                card.innerHTML = `
                    <div style="margin-bottom: 24px;">
                        <h2 style="font-size: 22px; font-weight: 600; margin-bottom: 8px; color: var(--center-channel-color, #333);">Log In to Mattermost</h2>
                        <p style="color: var(--center-channel-color-64, #666); font-size: 14px; margin: 0;">Sign in with your organization's Authentik account.</p>
                    </div>
                    <div id="mattermost-authentik-empty-btn-mount" style="width: 100%;"></div>
                `;

                targetCard.appendChild(card);

                const mountPoint = document.getElementById('mattermost-authentik-empty-btn-mount');
                const ReactLib = window.React || React;
                const ReactDOMLib = window.ReactDOM || ReactDOM;

                if (ReactDOMLib && ReactDOMLib.createRoot) {
                    this.reactRoot = ReactDOMLib.createRoot(mountPoint);
                    this.reactRoot.render(ReactLib.createElement(AuthentikLoginButton));
                } else if (ReactDOMLib && ReactDOMLib.render) {
                    ReactDOMLib.render(ReactLib.createElement(AuthentikLoginButton), mountPoint);
                }
                return;
            }
        }

        // 2. Normal login screen with form
        if (document.getElementById('mattermost-authentik-btn-container')) {
            return;
        }

        const selectors = [
            '.signup-team__container',
            '.login-body-card-content',
            '.login-body-card',
            '.login__body',
            '.signup-body-content',
            '#login_section',
            'form.login-form',
            '.signup__content',
        ];

        let targetContainer = null;
        for (const selector of selectors) {
            targetContainer = document.querySelector(selector);
            if (targetContainer) {
                break;
            }
        }

        if (!targetContainer) {
            return;
        }

        const container = document.createElement('div');
        container.id = 'mattermost-authentik-btn-container';
        container.style.width = '100%';
        container.style.marginBottom = '12px';

        const form = targetContainer.querySelector('form');
        if (form) {
            form.parentNode.insertBefore(container, form);
        } else {
            targetContainer.prepend(container);
        }

        const ReactLib = window.React || React;
        const ReactDOMLib = window.ReactDOM || ReactDOM;

        if (ReactDOMLib && ReactDOMLib.createRoot) {
            this.reactRoot = ReactDOMLib.createRoot(container);
            this.reactRoot.render(ReactLib.createElement(AuthentikLoginButton));
        } else if (ReactDOMLib && ReactDOMLib.render) {
            ReactDOMLib.render(ReactLib.createElement(AuthentikLoginButton), container);
        }

        // If EnforceSSO is active, hide the username/password form so only SSO is available
        this.fetchPluginConfig().then((cfg) => {
            if (cfg && cfg.enforce_sso) {
                const formsToHide = document.querySelectorAll('form.login-form, .signup-body-card-form, #login_section, .login-body-card-form-divider, .signup-body-card-form-divider');
                formsToHide.forEach((f) => {
                    f.style.display = 'none';
                });
            }
        });
    }

    getBadgeColor() {
        if (this.pluginConfig && this.pluginConfig.button_color) {
            return this.pluginConfig.button_color;
        }
        return '#FD4B2D';
    }

    enhanceProfileAndSecurityEmail() {
        if (!this.store) {
            return;
        }

        const state = this.store.getState();
        if (!state || !state.entities || !state.entities.users) {
            return;
        }

        const currentUserId = state.entities.users.currentUserId;
        const currentUser = state.entities.users.profiles && state.entities.users.profiles[currentUserId];
        if (!currentUser) {
            return;
        }

        if (!this.pluginConfig && !this.fetchingConfig) {
            this.fetchingConfig = true;
            this.fetchPluginConfig().then(() => {
                this.fetchingConfig = false;
                this.enhanceProfileAndSecurityEmail();
            });
        }

        const badgeColor = this.getBadgeColor();

        // Clean up legacy custom rows if present
        const legacyProfileRow = document.getElementById('authentik-profile-email-row');
        if (legacyProfileRow) {
            legacyProfileRow.remove();
        }

        // Ensure styles for non-clickable security row
        if (!document.getElementById('authentik-custom-styles')) {
            const style = document.createElement('style');
            style.id = 'authentik-custom-styles';
            style.textContent = `
                #authentik-security-sso-row {
                    cursor: default !important;
                }
                #authentik-security-sso-row:hover {
                    background: transparent !important;
                    background-color: transparent !important;
                    cursor: default !important;
                }
            `;
            document.head.appendChild(style);
        }

        // Account Settings modal: Security tab (#securitySettings) Authentik OIDC info banner
        // Positioned below the "Security Settings" header right before the password row
        const securitySettings = document.getElementById('securitySettings');
        if (securitySettings && currentUser.auth_service === 'authentik') {
            const passwordTitle = document.getElementById('passwordTitle');
            const passwordDesc = document.getElementById('passwordDesc');
            const passwordEdit = document.getElementById('passwordEdit');
            const passwordSectionMin = (passwordTitle || passwordDesc || passwordEdit)?.closest('.section-min');

            let row = document.getElementById('authentik-security-sso-row');
            if (!row) {
                row = document.createElement('div');
                row.id = 'authentik-security-sso-row';
                row.className = 'section-min isDisabled';
                row.style.borderBottom = '1px solid var(--center-channel-color-16, #e5e5e5)';
                row.style.padding = '12px 15px';
                row.style.cursor = 'default';
                row.style.background = 'transparent';

                row.innerHTML = `
                    <div class="user-settings__row" style="display: flex; justify-content: space-between; align-items: center;">
                        <div>
                            <div class="user-settings__title" style="font-weight: 600; font-size: 14px; margin-bottom: 4px;">Sign-in Method</div>
                            <div class="user-settings__desc" style="color: var(--center-channel-color-72, #555); font-size: 13px;">
                                <span>Authentik Single Sign-On (OIDC)</span>
                                <span id="authentik-active-badge" style="margin-left: 8px; background: ${badgeColor}; color: white; padding: 2px 7px; border-radius: 4px; font-size: 11px; font-weight: 600;">Active</span>
                                <div style="margin-top: 4px; font-size: 12px; color: var(--center-channel-color-56, #888); line-height: 1.5;">
                                    <div>Authentik Username: <strong>${this.escapeHtml(currentUser.username)}</strong></div>
                                    <div>Authentik Email: <strong>${this.escapeHtml(currentUser.email)}</strong></div>
                                </div>
                            </div>
                        </div>
                    </div>
                `;

                if (passwordSectionMin && passwordSectionMin.parentNode) {
                    passwordSectionMin.parentNode.insertBefore(row, passwordSectionMin);
                } else {
                    const userSettingsContainer = securitySettings.querySelector('.user-settings') || securitySettings;
                    userSettingsContainer.prepend(row);
                }
            } else {
                // Ensure row is placed right before password row if passwordSectionMin is available
                if (passwordSectionMin && passwordSectionMin.parentNode && row.nextSibling !== passwordSectionMin) {
                    passwordSectionMin.parentNode.insertBefore(row, passwordSectionMin);
                }
                const activeBadge = row.querySelector('#authentik-active-badge');
                if (activeBadge) {
                    activeBadge.style.backgroundColor = badgeColor;
                }
            }
        }

        // Lock and enhance Full Name, Username, Email, Password, and Sign-in Method if user is authenticated via Authentik
        this.lockProfileFieldsForAuthentik(currentUser, badgeColor);
    }

    lockProfileFieldsForAuthentik(currentUser, badgeColor) {
        if (!currentUser || currentUser.auth_service !== 'authentik') {
            return;
        }

        const color = badgeColor || this.getBadgeColor();

        // 1. Lock Full Name
        // A. Collapsed state
        const nameTitle = document.getElementById('nameTitle');
        const nameDesc = document.getElementById('nameDesc');
        const nameEdit = document.getElementById('nameEdit');
        const nameSectionMin = (nameTitle || nameDesc || nameEdit)?.closest('.section-min');
        if (nameSectionMin) {
            nameSectionMin.style.pointerEvents = 'none';
            nameSectionMin.style.cursor = 'default';
            nameSectionMin.setAttribute('tabindex', '-1');
            if (nameEdit) {
                nameEdit.style.display = 'none';
            }
            const existingBadge = document.getElementById('authentik-name-badge');
            if (nameDesc && !existingBadge) {
                const badge = document.createElement('span');
                badge.id = 'authentik-name-badge';
                badge.style.cssText = `margin-left: 8px; background: ${color}; color: white; padding: 2px 7px; border-radius: 4px; font-size: 11px; font-weight: 600; vertical-align: middle;`;
                badge.innerText = 'Managed by Authentik';
                nameDesc.appendChild(badge);
            } else if (existingBadge) {
                existingBadge.style.backgroundColor = color;
            }
        }

        // B. Expanded state (if already rendered)
        const firstNameInput = document.getElementById('firstName');
        const lastNameInput = document.getElementById('lastName');
        if (firstNameInput || lastNameInput) {
            if (firstNameInput) {
                firstNameInput.disabled = true;
                firstNameInput.readOnly = true;
                firstNameInput.style.backgroundColor = 'var(--center-channel-color-08, #f5f5f5)';
                firstNameInput.style.cursor = 'not-allowed';
            }
            if (lastNameInput) {
                lastNameInput.disabled = true;
                lastNameInput.readOnly = true;
                lastNameInput.style.backgroundColor = 'var(--center-channel-color-08, #f5f5f5)';
                lastNameInput.style.cursor = 'not-allowed';
            }
            const nameSectionMax = (firstNameInput || lastNameInput).closest('.section-max');
            if (nameSectionMax) {
                const saveBtn = nameSectionMax.querySelector('#saveSetting') || nameSectionMax.querySelector('button[type="submit"]');
                if (saveBtn) {
                    saveBtn.style.display = 'none';
                }
                if (!document.getElementById('authentik-name-locked-notice')) {
                    const notice = document.createElement('div');
                    notice.id = 'authentik-name-locked-notice';
                    notice.style.cssText = 'margin-top: 12px; padding: 8px 12px; background: rgba(253, 75, 45, 0.1); border-left: 3px solid #FD4B2D; border-radius: 3px; font-size: 13px; color: var(--center-channel-color, #333);';
                    notice.innerText = 'First and last names are synchronized with your Authentik account and cannot be modified here.';
                    nameSectionMax.appendChild(notice);
                }
            }
        }

        // 2. Lock Username
        // A. Collapsed state
        const usernameTitle = document.getElementById('usernameTitle');
        const usernameDesc = document.getElementById('usernameDesc');
        const usernameEdit = document.getElementById('usernameEdit');
        const usernameSectionMin = (usernameTitle || usernameDesc || usernameEdit)?.closest('.section-min');
        if (usernameSectionMin) {
            usernameSectionMin.style.pointerEvents = 'none';
            usernameSectionMin.style.cursor = 'default';
            usernameSectionMin.setAttribute('tabindex', '-1');
            if (usernameEdit) {
                usernameEdit.style.display = 'none';
            }
            const existingBadge = document.getElementById('authentik-username-badge');
            if (usernameDesc && !existingBadge) {
                const badge = document.createElement('span');
                badge.id = 'authentik-username-badge';
                badge.style.cssText = `margin-left: 8px; background: ${color}; color: white; padding: 2px 7px; border-radius: 4px; font-size: 11px; font-weight: 600; vertical-align: middle;`;
                badge.innerText = 'Managed by Authentik';
                usernameDesc.appendChild(badge);
            } else if (existingBadge) {
                existingBadge.style.backgroundColor = color;
            }
        }

        // B. Expanded state (if already rendered)
        const usernameInput = document.getElementById('username');
        if (usernameInput && usernameInput.closest('#generalSettings, .user-settings')) {
            usernameInput.disabled = true;
            usernameInput.readOnly = true;
            usernameInput.style.backgroundColor = 'var(--center-channel-color-08, #f5f5f5)';
            usernameInput.style.cursor = 'not-allowed';
            const usernameSectionMax = usernameInput.closest('.section-max');
            if (usernameSectionMax) {
                const saveBtn = usernameSectionMax.querySelector('#saveSetting') || usernameSectionMax.querySelector('button[type="submit"]');
                if (saveBtn) {
                    saveBtn.style.display = 'none';
                }
                if (!document.getElementById('authentik-username-locked-notice')) {
                    const notice = document.createElement('div');
                    notice.id = 'authentik-username-locked-notice';
                    notice.style.cssText = 'margin-top: 12px; padding: 8px 12px; background: rgba(253, 75, 45, 0.1); border-left: 3px solid #FD4B2D; border-radius: 3px; font-size: 13px; color: var(--center-channel-color, #333);';
                    notice.innerText = 'Username is synchronized with your Authentik account and cannot be modified here.';
                    usernameSectionMax.appendChild(notice);
                }
            }
        }

        // 3. Lock native Email row if present and inject email value + badge
        const emailTitle = document.getElementById('emailTitle');
        const emailDesc = document.getElementById('emailDesc');
        const emailEdit = document.getElementById('emailEdit');
        const emailSectionMin = (emailTitle || emailDesc || emailEdit)?.closest('.section-min');
        if (emailSectionMin) {
            emailSectionMin.style.pointerEvents = 'none';
            emailSectionMin.style.cursor = 'default';
            emailSectionMin.setAttribute('tabindex', '-1');
            if (emailEdit) {
                emailEdit.style.display = 'none';
            }
            if (emailDesc) {
                const badgeId = 'authentik-email-badge';
                const existingBadge = document.getElementById(badgeId);
                if (!existingBadge || !emailDesc.textContent.trim()) {
                    const emailText = this.escapeHtml(currentUser.email || 'No email configured');
                    emailDesc.innerHTML = `<span>${emailText}</span> <span id="${badgeId}" style="margin-left: 8px; background: ${color}; color: white; padding: 2px 7px; border-radius: 4px; font-size: 11px; font-weight: 600; vertical-align: middle;">Managed by Authentik</span>`;
                } else if (existingBadge) {
                    existingBadge.style.backgroundColor = color;
                }
            }
        }

        // B. Expanded state for Email (if open)
        const primaryEmailInput = document.getElementById('primaryEmail');
        const confirmEmailInput = document.getElementById('confirmEmail');
        if (primaryEmailInput || confirmEmailInput) {
            if (primaryEmailInput) {
                primaryEmailInput.disabled = true;
                primaryEmailInput.readOnly = true;
                primaryEmailInput.style.backgroundColor = 'var(--center-channel-color-08, #f5f5f5)';
                primaryEmailInput.style.cursor = 'not-allowed';
            }
            if (confirmEmailInput) {
                confirmEmailInput.disabled = true;
                confirmEmailInput.readOnly = true;
                confirmEmailInput.style.backgroundColor = 'var(--center-channel-color-08, #f5f5f5)';
                confirmEmailInput.style.cursor = 'not-allowed';
            }
            const emailSectionMax = (primaryEmailInput || confirmEmailInput).closest('.section-max');
            if (emailSectionMax) {
                const saveBtn = emailSectionMax.querySelector('#saveSetting') || emailSectionMax.querySelector('button[type="submit"]');
                if (saveBtn) {
                    saveBtn.style.display = 'none';
                }
                if (!document.getElementById('authentik-email-locked-notice')) {
                    const notice = document.createElement('div');
                    notice.id = 'authentik-email-locked-notice';
                    notice.style.cssText = 'margin-top: 12px; padding: 8px 12px; background: rgba(253, 75, 45, 0.1); border-left: 3px solid #FD4B2D; border-radius: 3px; font-size: 13px; color: var(--center-channel-color, #333);';
                    notice.innerText = 'Email is synchronized with your Authentik account and cannot be modified here.';
                    emailSectionMax.appendChild(notice);
                }
            }
        }

        // 4. Lock native Password row in Security settings (no badge to prevent text box overflow)
        const passwordTitle = document.getElementById('passwordTitle');
        const passwordDesc = document.getElementById('passwordDesc');
        const passwordEdit = document.getElementById('passwordEdit');
        const passwordSectionMin = (passwordTitle || passwordDesc || passwordEdit)?.closest('.section-min');
        if (passwordSectionMin) {
            passwordSectionMin.style.pointerEvents = 'none';
            passwordSectionMin.style.cursor = 'default';
            passwordSectionMin.setAttribute('tabindex', '-1');
            if (passwordEdit) {
                passwordEdit.style.display = 'none';
            }
            if (passwordDesc) {
                passwordDesc.innerText = 'Login occurs through Authentik. Password cannot be updated.';
            }
        }

        // 5. Hide native Sign-in Method row if present (replaced by Authentik SSO banner)
        const signinTitle = document.getElementById('signinTitle');
        const signinDesc = document.getElementById('signinDesc');
        const signinEdit = document.getElementById('signinEdit');
        const signinSectionMin = (signinTitle || signinDesc || signinEdit)?.closest('.section-min');
        if (signinSectionMin) {
            signinSectionMin.style.display = 'none';
        }
    }

    escapeHtml(str) {
        if (!str) return '';
        return String(str)
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;')
            .replace(/'/g, '&#39;');
    }

    uninitialize() {
        if (this.observer) {
            this.observer.disconnect();
            this.observer = null;
        }

        const btnContainer = document.getElementById('mattermost-authentik-btn-container');
        if (btnContainer) {
            if (this.reactRoot) {
                try { this.reactRoot.unmount(); } catch (e) {}
                this.reactRoot = null;
            }
            btnContainer.remove();
        }

        const profileRow = document.getElementById('authentik-profile-email-row');
        if (profileRow) {
            profileRow.remove();
        }

        const secRow = document.getElementById('authentik-security-sso-row');
        if (secRow) {
            secRow.remove();
        }
    }
}

if (typeof window !== 'undefined') {
    window.registerPlugin(PLUGIN_ID, new PluginClass());
}
