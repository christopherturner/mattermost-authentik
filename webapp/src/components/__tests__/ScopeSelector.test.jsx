import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import ScopeSelector from '../ScopeSelector';

describe('ScopeSelector', () => {
    test('renders mandatory scopes and prevents removing them', () => {
        const onChange = jest.fn();
        render(<ScopeSelector id="Scope" value="openid profile email groups" onChange={onChange} />);

        expect(screen.getByText('openid')).toBeTruthy();
        expect(screen.getByText('profile')).toBeTruthy();
        expect(screen.getByText('email')).toBeTruthy();
        expect(screen.getByText('groups')).toBeTruthy();
        expect(screen.queryByTitle('Remove openid')).toBeNull();
    });

    test('adds and removes custom scopes while preserving mandatory scopes', () => {
        const onChange = jest.fn();
        const setSaveNeeded = jest.fn();
        render(
            <ScopeSelector
                id="Scope"
                value="openid profile email groups offline_access"
                onChange={onChange}
                setSaveNeeded={setSaveNeeded}
            />
        );

        const removeBtn = screen.getByTitle('Remove offline_access');
        fireEvent.click(removeBtn);

        expect(onChange).toHaveBeenCalledWith('Scope', 'openid profile email groups');
        expect(setSaveNeeded).toHaveBeenCalled();
    });
});
