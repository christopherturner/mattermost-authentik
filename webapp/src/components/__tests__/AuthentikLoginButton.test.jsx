import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import AuthentikLoginButton from '../AuthentikLoginButton';

describe('AuthentikLoginButton', () => {
    beforeEach(() => {
        global.fetch = jest.fn();
    });

    afterEach(() => {
        jest.resetAllMocks();
    });

    test('renders configured login button when plugin is enabled', async () => {
        global.fetch.mockResolvedValueOnce({
            ok: true,
            json: async () => ({
                enable: true,
                button_text: 'Sign in with Authentik SSO',
                button_color: '#123456',
            }),
        });

        render(<AuthentikLoginButton />);

        const button = await screen.findByRole('button', { name: /Sign in with Authentik SSO/i });
        expect(button).toBeTruthy();
        expect(button.style.backgroundColor).toBe('rgb(18, 52, 86)');
    });

    test('renders nothing when plugin is disabled', async () => {
        global.fetch.mockResolvedValueOnce({
            ok: true,
            json: async () => ({
                enable: false,
            }),
        });

        const { container } = render(<AuthentikLoginButton />);

        await waitFor(() => {
            expect(global.fetch).toHaveBeenCalledTimes(1);
        });
        expect(container.firstChild).toBeNull();
    });
});
