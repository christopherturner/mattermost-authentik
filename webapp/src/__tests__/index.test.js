import PluginClass from '../index';

describe('PluginClass routing and SLO hooks', () => {
    afterEach(() => {
        window.history.pushState({}, '', '/');
        jest.restoreAllMocks();
    });

    test('isLoginRoute matches /login and subpath /login, excludes /channels/', () => {
        const plugin = new PluginClass();

        window.history.pushState({}, '', '/login');
        expect(plugin.isLoginRoute()).toBe(true);

        window.history.pushState({}, '', '/mattermost/login');
        expect(plugin.isLoginRoute()).toBe(true);

        window.history.pushState({}, '', '/engineering/channels/login-support');
        expect(plugin.isLoginRoute()).toBe(false);
    });

    test('isAutoRedirectEligible blocks authenticated users and ?no_sso=1', () => {
        const plugin = new PluginClass();
        plugin.initialized = true;

        window.history.pushState({}, '', '/login');
        expect(plugin.isAutoRedirectEligible()).toBe(true);

        window.history.pushState({}, '', '/login?no_sso=1');
        expect(plugin.isAutoRedirectEligible()).toBe(false);

        window.history.pushState({}, '', '/login');
        plugin.store = {
            getState: () => ({
                entities: { users: { currentUserId: 'u1' } },
            }),
        };
        expect(plugin.isAutoRedirectEligible()).toBe(false);
    });

    test('uninitialize restores original window.fetch', async () => {
        const originalFetch = jest.fn().mockResolvedValue({ ok: true });
        window.fetch = originalFetch;

        const plugin = new PluginClass();
        plugin.setupLogoutInterceptor();
        const wrappedFetch = window.fetch;
        expect(wrappedFetch).not.toBe(originalFetch);

        plugin.uninitialize();
        expect(window.fetch).not.toBe(wrappedFetch);
        await window.fetch('/test');
        expect(originalFetch).toHaveBeenCalledWith('/test');
    });
});
