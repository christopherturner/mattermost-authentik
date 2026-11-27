import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import RedirectURIHelper from '../RedirectURIHelper';

describe('RedirectURIHelper', () => {
    beforeEach(() => {
        Object.assign(navigator, {
            clipboard: {
                writeText: jest.fn().mockResolvedValue(undefined),
            },
        });
    });

    test('renders callback and back-channel logout URLs and copies on click', async () => {
        render(<RedirectURIHelper />);

        const callbackInput = screen.getByDisplayValue(/\/plugins\/io\.christopherturner\.mattermost-authentik\/oauth2\/callback$/);
        const backchannelInput = screen.getByDisplayValue(/\/plugins\/io\.christopherturner\.mattermost-authentik\/oauth2\/backchannel-logout$/);

        expect(callbackInput).toBeTruthy();
        expect(backchannelInput).toBeTruthy();

        const copyButtons = screen.getAllByRole('button', { name: 'Copy' });
        expect(copyButtons).toHaveLength(2);

        fireEvent.click(copyButtons[0]);

        await waitFor(() => {
            expect(navigator.clipboard.writeText).toHaveBeenCalledWith(callbackInput.value);
            expect(screen.getByRole('button', { name: 'Copied!' })).toBeTruthy();
        });
    });
});
