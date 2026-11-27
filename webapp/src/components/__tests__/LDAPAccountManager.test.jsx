import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import LDAPAccountManager from '../LDAPAccountManager';

describe('LDAPAccountManager', () => {
    beforeEach(() => {
        global.fetch = jest.fn();
        document.cookie = 'MMCSRF=test-csrf-token';
    });

    afterEach(() => {
        jest.resetAllMocks();
    });

    test('loads LDAP users and unlocks a single user', async () => {
        global.fetch
            .mockResolvedValueOnce({
                ok: true,
                json: async () => ({
                    count: 1,
                    users: [{ id: 'u1', username: 'ldap.user', email: 'ldap@example.com', first_name: 'LDAP', last_name: 'User' }],
                }),
            })
            .mockResolvedValueOnce({
                ok: true,
                json: async () => ({ success: true, unlocked: 1, failed: [] }),
            })
            .mockResolvedValueOnce({
                ok: true,
                json: async () => ({ count: 0, users: [] }),
            });

        render(<LDAPAccountManager />);

        expect(await screen.findByText('ldap.user')).toBeTruthy();

        fireEvent.click(screen.getByRole('button', { name: 'Unlock' }));

        await waitFor(() => {
            expect(global.fetch).toHaveBeenCalledWith(
                '/plugins/io.christopherturner.mattermost-authentik/api/v1/admin/unlock-ldap',
                expect.objectContaining({
                    method: 'POST',
                    body: JSON.stringify({ user_ids: ['u1'] }),
                })
            );
        });

        expect(await screen.findByText(/No LDAP-locked accounts detected/i)).toBeTruthy();
    });
});
