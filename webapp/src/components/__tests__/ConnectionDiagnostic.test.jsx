import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import ConnectionDiagnostic from '../ConnectionDiagnostic';

describe('ConnectionDiagnostic', () => {
    beforeEach(() => {
        global.fetch = jest.fn();
        document.cookie = 'MMCSRF=test-csrf-token';
    });

    afterEach(() => {
        jest.resetAllMocks();
    });

    test('runs diagnostic and renders step results', async () => {
        global.fetch.mockResolvedValueOnce({
            ok: true,
            json: async () => ({
                success: true,
                effective_issuer: 'https://auth.example.com/application/o/mattermost',
                steps: [
                    { name: 'OIDC Discovery', status: 'ok', details: 'Valid OIDC configuration found.', latency_ms: 15 },
                ],
            }),
        });

        render(<ConnectionDiagnostic />);

        fireEvent.click(screen.getByRole('button', { name: /Test Connection/i }));

        await waitFor(() => {
            expect(global.fetch).toHaveBeenCalledWith(
                '/plugins/io.christopherturner.mattermost-authentik/api/v1/admin/diagnose',
                expect.objectContaining({
                    method: 'POST',
                    headers: expect.objectContaining({
                        'X-CSRF-Token': 'test-csrf-token',
                    }),
                })
            );
        });

        expect(await screen.findByText(/All Diagnostic Checks Passed/i)).toBeTruthy();
        expect(screen.getByText(/OIDC Discovery:/i)).toBeTruthy();
    });

    test('renders error alert when diagnostic request fails', async () => {
        global.fetch.mockResolvedValueOnce({
            ok: false,
            status: 403,
        });

        render(<ConnectionDiagnostic />);
        fireEvent.click(screen.getByRole('button', { name: /Test Connection/i }));

        expect(await screen.findByText(/Diagnostic endpoint returned HTTP 403/i)).toBeTruthy();
    });
});
