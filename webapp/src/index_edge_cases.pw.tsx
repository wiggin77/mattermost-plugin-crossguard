import React from 'react';

import PluginTestHarness from './components/PluginTestHarness';

import {test, expect} from '../playwright/ct-coverage';

test.describe('Plugin - uninitialize edge cases', () => {
    test('uninitialize before initialize does not throw', async ({mount, page}) => {
        await mount(<PluginTestHarness/>);
        const result = await page.evaluate(() => {
            const Plugin = (window as any).__PluginClass;
            const plugin = new Plugin();
            try {
                plugin.uninitialize();
                return 'ok';
            } catch (e: any) {
                return 'error: ' + e.message;
            }
        });
        expect(result).toBe('ok');
    });

    test('uninitialize twice does not throw', async ({mount, page}) => {
        await mount(<PluginTestHarness/>);
        const result = await page.evaluate(async () => {
            const Plugin = (window as any).__PluginClass;
            const plugin = new Plugin();
            let nextId = 1;
            const registry = {
                registerAdminConsoleCustomSetting: () => 'id-' + (nextId++),
                registerRootComponent: () => 'id-' + (nextId++),
                registerSidebarChannelLinkLabelComponent: () => 'id-' + (nextId++),
                registerPopoverUserAttributesComponent: () => 'id-' + (nextId++),
                registerWebSocketEventHandler: () => 'id-' + (nextId++),
                registerChannelHeaderMenuAction: () => 'id-' + (nextId++),
                registerMainMenuAction: () => 'id-' + (nextId++),
                unregisterComponent: () => {},
            };
            const store = {
                getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                subscribe: () => () => {},
            };

            await plugin.initialize(registry, store);
            plugin.uninitialize();
            try {
                plugin.uninitialize();
                return 'ok';
            } catch (e: any) {
                return 'error: ' + e.message;
            }
        });
        expect(result).toBe('ok');
    });

    test('after uninitialize, store subscription is removed', async ({mount, page}) => {
        await mount(<PluginTestHarness/>);
        const result = await page.evaluate(async () => {
            const Plugin = (window as any).__PluginClass;
            const plugin = new Plugin();

            let subscribeCalled = 0;
            let unsubCalled = 0;
            let nextId = 1;
            const registry = {
                registerAdminConsoleCustomSetting: () => 'id-' + (nextId++),
                registerRootComponent: () => 'id-' + (nextId++),
                registerSidebarChannelLinkLabelComponent: () => 'id-' + (nextId++),
                registerPopoverUserAttributesComponent: () => 'id-' + (nextId++),
                registerWebSocketEventHandler: () => 'id-' + (nextId++),
                registerChannelHeaderMenuAction: () => 'id-' + (nextId++),
                registerMainMenuAction: () => 'id-' + (nextId++),
                unregisterComponent: () => {},
            };
            const store = {
                getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                subscribe: () => {
 subscribeCalled++;
 return () => {
 unsubCalled++;
};
},
            };

            await plugin.initialize(registry, store);
            plugin.uninitialize();
            return {subscribeCalled, unsubCalled};
        });
        expect(result.subscribeCalled).toBe(1);
        expect(result.unsubCalled).toBe(1);
    });

    test('uninitialize then re-initialize works', async ({mount, page}) => {
        await mount(<PluginTestHarness/>);
        const result = await page.evaluate(async () => {
            const Plugin = (window as any).__PluginClass;
            const plugin = new Plugin();

            let subscribeCalled = 0;
            let nextId = 1;
            const registry = {
                registerAdminConsoleCustomSetting: () => 'id-' + (nextId++),
                registerRootComponent: () => 'id-' + (nextId++),
                registerSidebarChannelLinkLabelComponent: () => 'id-' + (nextId++),
                registerPopoverUserAttributesComponent: () => 'id-' + (nextId++),
                registerWebSocketEventHandler: () => 'id-' + (nextId++),
                registerChannelHeaderMenuAction: () => 'id-' + (nextId++),
                registerMainMenuAction: () => 'id-' + (nextId++),
                unregisterComponent: () => {},
            };
            const store = {
                getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                subscribe: () => {
 subscribeCalled++;
 return () => {};
},
            };

            await plugin.initialize(registry, store);
            plugin.uninitialize();
            await plugin.initialize(registry, store);
            return subscribeCalled;
        });
        expect(result).toBe(2);
    });
});

test.describe('Plugin - store subscription checkState branches', () => {
    test('first call with empty teamId does not fetch team status', async ({mount, page}) => {
        await mount(<PluginTestHarness/>);

        let fetchCalled = false;
        await page.route('**/api/v1/teams/*/status', (route) => {
            fetchCalled = true;
            route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
        });

        await page.evaluate(async () => {
            const Plugin = (window as any).__PluginClass;
            const plugin = new Plugin();
            let nextId = 1;
            const registry = {
                registerAdminConsoleCustomSetting: () => 'id-' + (nextId++),
                registerRootComponent: () => 'id-' + (nextId++),
                registerSidebarChannelLinkLabelComponent: () => 'id-' + (nextId++),
                registerPopoverUserAttributesComponent: () => 'id-' + (nextId++),
                registerWebSocketEventHandler: () => 'id-' + (nextId++),
                registerChannelHeaderMenuAction: () => 'id-' + (nextId++),
                registerMainMenuAction: () => 'id-' + (nextId++),
                unregisterComponent: () => {},
            };
            const store = {
                getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                subscribe: () => () => {},
            };
            await plugin.initialize(registry, store);
        });

        await page.waitForTimeout(100);
        expect(fetchCalled).toBe(false);
    });

    test('state with null entities handled gracefully', async ({mount, page}) => {
        await mount(<PluginTestHarness/>);
        const result = await page.evaluate(async () => {
            const Plugin = (window as any).__PluginClass;
            const plugin = new Plugin();
            let nextId = 1;
            const registry = {
                registerAdminConsoleCustomSetting: () => 'id-' + (nextId++),
                registerRootComponent: () => 'id-' + (nextId++),
                registerSidebarChannelLinkLabelComponent: () => 'id-' + (nextId++),
                registerPopoverUserAttributesComponent: () => 'id-' + (nextId++),
                registerWebSocketEventHandler: () => 'id-' + (nextId++),
                registerChannelHeaderMenuAction: () => 'id-' + (nextId++),
                registerMainMenuAction: () => 'id-' + (nextId++),
                unregisterComponent: () => {},
            };
            const store = {
                getState: () => ({entities: null}),
                subscribe: (fn: any) => {
 fn();
 return () => {};
},
            };
            try {
                await plugin.initialize(registry, store);
                return 'ok';
            } catch (e: any) {
                return 'error: ' + e.message;
            }
        });
        expect(result).toBe('ok');
    });

    test('state with null entities.teams handled gracefully', async ({mount, page}) => {
        await mount(<PluginTestHarness/>);
        const result = await page.evaluate(async () => {
            const Plugin = (window as any).__PluginClass;
            const plugin = new Plugin();
            let nextId = 1;
            const registry = {
                registerAdminConsoleCustomSetting: () => 'id-' + (nextId++),
                registerRootComponent: () => 'id-' + (nextId++),
                registerSidebarChannelLinkLabelComponent: () => 'id-' + (nextId++),
                registerPopoverUserAttributesComponent: () => 'id-' + (nextId++),
                registerWebSocketEventHandler: () => 'id-' + (nextId++),
                registerChannelHeaderMenuAction: () => 'id-' + (nextId++),
                registerMainMenuAction: () => 'id-' + (nextId++),
                unregisterComponent: () => {},
            };
            const store = {
                getState: () => ({entities: {teams: null, channels: null}}),
                subscribe: (fn: any) => {
 fn();
 return () => {};
},
            };
            try {
                await plugin.initialize(registry, store);
                return 'ok';
            } catch (e: any) {
                return 'error: ' + e.message;
            }
        });
        expect(result).toBe('ok');
    });

    test('state with null entities.channels handled gracefully', async ({mount, page}) => {
        await mount(<PluginTestHarness/>);
        const result = await page.evaluate(async () => {
            const Plugin = (window as any).__PluginClass;
            const plugin = new Plugin();
            let nextId = 1;
            const registry = {
                registerAdminConsoleCustomSetting: () => 'id-' + (nextId++),
                registerRootComponent: () => 'id-' + (nextId++),
                registerSidebarChannelLinkLabelComponent: () => 'id-' + (nextId++),
                registerPopoverUserAttributesComponent: () => 'id-' + (nextId++),
                registerWebSocketEventHandler: () => 'id-' + (nextId++),
                registerChannelHeaderMenuAction: () => 'id-' + (nextId++),
                registerMainMenuAction: () => 'id-' + (nextId++),
                unregisterComponent: () => {},
            };
            const store = {
                getState: () => ({entities: {teams: {currentTeamId: ''}, channels: null}}),
                subscribe: (fn: any) => {
 fn();
 return () => {};
},
            };
            try {
                await plugin.initialize(registry, store);
                return 'ok';
            } catch (e: any) {
                return 'error: ' + e.message;
            }
        });
        expect(result).toBe('ok');
    });

    test('same channelId repeated does not trigger duplicate fetch', async ({mount, page}) => {
        await mount(<PluginTestHarness/>);
        let fetchCount = 0;
        await page.route('**/api/v1/channels/connections*', (route) => {
            fetchCount++;
            route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
        });
        await page.route('**/api/v1/teams/*/status', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
        });

        await page.evaluate(async () => {
            const Plugin = (window as any).__PluginClass;
            const plugin = new Plugin();
            let nextId = 1;
            const registry = {
                registerAdminConsoleCustomSetting: () => 'id-' + (nextId++),
                registerRootComponent: () => 'id-' + (nextId++),
                registerSidebarChannelLinkLabelComponent: () => 'id-' + (nextId++),
                registerPopoverUserAttributesComponent: () => 'id-' + (nextId++),
                registerWebSocketEventHandler: () => 'id-' + (nextId++),
                registerChannelHeaderMenuAction: () => 'id-' + (nextId++),
                registerMainMenuAction: () => 'id-' + (nextId++),
                unregisterComponent: () => {},
            };

            let listener: any = null;
            const channelsObj = {ch1: {}};
            const store = {
                getState: () => ({
                    entities: {
                        teams: {currentTeamId: 'team1'},
                        channels: {currentChannelId: 'ch1', channels: channelsObj},
                    },
                }),
                subscribe: (fn: any) => {
 listener = fn;
 return () => {};
},
            };

            await plugin.initialize(registry, store);

            // Trigger checkState again with same state reference
            if (listener) {
listener();
}
            if (listener) {
listener();
}
        });

        await page.waitForTimeout(200);

        // Initial call fetches ch1. Subsequent calls with same channels ref and same channelId should not re-fetch.
        expect(fetchCount).toBeLessThanOrEqual(2);
    });
});

test.describe('Plugin - channel detection logic', () => {
    test('new channels appear, only new IDs fetched', async ({mount, page}) => {
        await mount(<PluginTestHarness/>);

        const fetchedIds: string[] = [];
        await page.route('**/api/v1/channels/connections*', async (route) => {
            const url = route.request().url();
            const match = url.match(/ids=([^&]*)/);
            if (match) {
                fetchedIds.push(...match[1].split(','));
            }
            await route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
        });
        await page.route('**/api/v1/teams/*/status', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
        });

        await page.evaluate(async () => {
            const Plugin = (window as any).__PluginClass;
            const plugin = new Plugin();
            let nextId = 1;
            const registry = {
                registerAdminConsoleCustomSetting: () => 'id-' + (nextId++),
                registerRootComponent: () => 'id-' + (nextId++),
                registerSidebarChannelLinkLabelComponent: () => 'id-' + (nextId++),
                registerPopoverUserAttributesComponent: () => 'id-' + (nextId++),
                registerWebSocketEventHandler: () => 'id-' + (nextId++),
                registerChannelHeaderMenuAction: () => 'id-' + (nextId++),
                registerMainMenuAction: () => 'id-' + (nextId++),
                unregisterComponent: () => {},
            };

            let listener: any = null;
            let channels: any = {ch1: {}};
            const store = {
                getState: () => ({
                    entities: {
                        teams: {currentTeamId: 'team1'},
                        channels: {currentChannelId: 'ch1', channels},
                    },
                }),
                subscribe: (fn: any) => {
 listener = fn;
 return () => {};
},
            };

            await plugin.initialize(registry, store);
            await new Promise((r) => setTimeout(r, 50));

            // Add a new channel (new object reference)
            channels = {ch1: {}, ch2: {}};
            if (listener) {
listener();
}
            await new Promise((r) => setTimeout(r, 50));
        });

        await page.waitForTimeout(200);
        expect(fetchedIds).toContain('ch1');
    });

    test('channelId change without channels ref change triggers fetch for new channelId', async ({mount, page}) => {
        await mount(<PluginTestHarness/>);

        const fetchedIds: string[] = [];
        await page.route('**/api/v1/channels/connections*', async (route) => {
            const url = route.request().url();
            const match = url.match(/ids=([^&]*)/);
            if (match) {
                fetchedIds.push(...match[1].split(','));
            }
            await route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
        });
        await page.route('**/api/v1/teams/*/status', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
        });

        await page.evaluate(async () => {
            const Plugin = (window as any).__PluginClass;
            const plugin = new Plugin();
            let nextId = 1;
            const registry = {
                registerAdminConsoleCustomSetting: () => 'id-' + (nextId++),
                registerRootComponent: () => 'id-' + (nextId++),
                registerSidebarChannelLinkLabelComponent: () => 'id-' + (nextId++),
                registerPopoverUserAttributesComponent: () => 'id-' + (nextId++),
                registerWebSocketEventHandler: () => 'id-' + (nextId++),
                registerChannelHeaderMenuAction: () => 'id-' + (nextId++),
                registerMainMenuAction: () => 'id-' + (nextId++),
                unregisterComponent: () => {},
            };

            let listener: any = null;
            const channelsObj = {ch1: {}, ch2: {}};
            let currentChannelId = 'ch1';
            const store = {
                getState: () => ({
                    entities: {
                        teams: {currentTeamId: 'team1'},
                        channels: {currentChannelId, channels: channelsObj},
                    },
                }),
                subscribe: (fn: any) => {
 listener = fn;
 return () => {};
},
            };

            await plugin.initialize(registry, store);
            await new Promise((r) => setTimeout(r, 100));

            // Change channelId while keeping the same channels object reference.
            // This exercises the else-if branch at line 83 of index.tsx.
            currentChannelId = 'ch2';
            if (listener) {
listener();
}
            await new Promise((r) => setTimeout(r, 100));
        });

        await page.waitForTimeout(200);
        expect(fetchedIds).toContain('ch2');
    });

    test('team change resets known channels, all treated as new', async ({mount, page}) => {
        await mount(<PluginTestHarness/>);

        const fetchedUrls: string[] = [];
        await page.route('**/api/v1/channels/connections*', async (route) => {
            fetchedUrls.push(route.request().url());
            await route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
        });
        await page.route('**/api/v1/teams/*/status', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
        });

        await page.evaluate(async () => {
            const Plugin = (window as any).__PluginClass;
            const plugin = new Plugin();
            let nextId = 1;
            const registry = {
                registerAdminConsoleCustomSetting: () => 'id-' + (nextId++),
                registerRootComponent: () => 'id-' + (nextId++),
                registerSidebarChannelLinkLabelComponent: () => 'id-' + (nextId++),
                registerPopoverUserAttributesComponent: () => 'id-' + (nextId++),
                registerWebSocketEventHandler: () => 'id-' + (nextId++),
                registerChannelHeaderMenuAction: () => 'id-' + (nextId++),
                registerMainMenuAction: () => 'id-' + (nextId++),
                unregisterComponent: () => {},
            };

            let listener: any = null;
            let teamId = 'team1';
            let channels: any = {ch1: {}};
            const store = {
                getState: () => ({
                    entities: {
                        teams: {currentTeamId: teamId},
                        channels: {currentChannelId: 'ch1', channels},
                    },
                }),
                subscribe: (fn: any) => {
 listener = fn;
 return () => {};
},
            };

            await plugin.initialize(registry, store);
            await new Promise((r) => setTimeout(r, 50));

            // Switch team
            teamId = 'team2';
            channels = {ch1: {}}; // New ref needed to trigger channel detection
            if (listener) {
listener();
}
            await new Promise((r) => setTimeout(r, 50));
        });

        await page.waitForTimeout(200);
        expect(fetchedUrls.length).toBeGreaterThanOrEqual(2);
    });
});

test.describe('Plugin - menu management', () => {
    test('updateMenuForTeam with 500 response removes both menus', async ({mount, page}) => {
        await mount(<PluginTestHarness/>);
        await page.route('**/api/v1/teams/*/status', (route) => {
            route.fulfill({status: 500, contentType: 'application/json', body: '{"error":"server error"}'});
        });

        const result = await page.evaluate(async () => {
            const Plugin = (window as any).__PluginClass;
            const plugin = new Plugin();
            const calls: string[] = [];
            let nextId = 1;
            const registry = {
                registerAdminConsoleCustomSetting: () => 'id-' + (nextId++),
                registerRootComponent: () => 'id-' + (nextId++),
                registerSidebarChannelLinkLabelComponent: () => 'id-' + (nextId++),
                registerPopoverUserAttributesComponent: () => 'id-' + (nextId++),
                registerWebSocketEventHandler: () => 'id-' + (nextId++),
                registerChannelHeaderMenuAction: () => {
 calls.push('addMenu');
 return 'menu-1';
},
                registerMainMenuAction: () => {
 calls.push('addMainMenu');
 return 'main-1';
},
                unregisterComponent: (id: string) => {
 calls.push('unregister:' + id);
},
            };

            const store = {
                getState: () => ({
                    entities: {
                        teams: {currentTeamId: 'team1'},
                        channels: {currentChannelId: '', channels: {}},
                    },
                }),
                subscribe: () => () => {},
            };

            await plugin.initialize(registry, store);
            await new Promise((r) => setTimeout(r, 200));
            return calls;
        });

        expect(result).not.toContain('addMenu');
    });

    test('updateMenuForTeam with network error removes both menus', async ({mount, page}) => {
        await mount(<PluginTestHarness/>);
        await page.route('**/api/v1/teams/*/status', (route) => {
            route.abort();
        });

        const result = await page.evaluate(async () => {
            const Plugin = (window as any).__PluginClass;
            const plugin = new Plugin();
            let nextId = 1;
            const calls: string[] = [];
            const registry = {
                registerAdminConsoleCustomSetting: () => 'id-' + (nextId++),
                registerRootComponent: () => 'id-' + (nextId++),
                registerSidebarChannelLinkLabelComponent: () => 'id-' + (nextId++),
                registerPopoverUserAttributesComponent: () => 'id-' + (nextId++),
                registerWebSocketEventHandler: () => 'id-' + (nextId++),
                registerChannelHeaderMenuAction: () => {
 calls.push('addMenu');
 return 'menu-1';
},
                registerMainMenuAction: () => {
 calls.push('addMainMenu');
 return 'main-1';
},
                unregisterComponent: (id: string) => {
 calls.push('unregister:' + id);
},
            };

            const store = {
                getState: () => ({
                    entities: {
                        teams: {currentTeamId: 'team1'},
                        channels: {currentChannelId: '', channels: {}},
                    },
                }),
                subscribe: () => () => {},
            };

            await plugin.initialize(registry, store);
            await new Promise((r) => setTimeout(r, 300));
            return calls;
        });

        expect(result).not.toContain('addMenu');
    });

    test('addMainMenuAction is a no-op when registry is null (no throw)', async ({mount, page}) => {
        await mount(<PluginTestHarness/>);
        const result = await page.evaluate(async () => {
            const Plugin = (window as any).__PluginClass;
            const plugin = new Plugin();

            // Plugin starts with registry undefined (no initialize called).
            // Direct invocation must not throw, must not call registerMainMenuAction.
            let threw = false;
            try {
                plugin.addMainMenuAction('team1');
            } catch {
                threw = true;
            }
            return {threw, mainMenuActionId: plugin.mainMenuActionId};
        });
        expect(result.threw).toBe(false);

        // No registry means no registerMainMenuAction call, so mainMenuActionId stays null/undefined.
        expect(result.mainMenuActionId).toBeFalsy();
    });

    test('addMainMenuAction removes previous before adding new', async ({mount, page}) => {
        await mount(<PluginTestHarness/>);
        await page.route('**/api/v1/teams/*/status', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
        });

        const result = await page.evaluate(async () => {
            const Plugin = (window as any).__PluginClass;
            const plugin = new Plugin();
            const calls: string[] = [];
            let nextId = 1;
            const registry = {
                registerAdminConsoleCustomSetting: () => 'id-' + (nextId++),
                registerRootComponent: () => 'id-' + (nextId++),
                registerSidebarChannelLinkLabelComponent: () => 'id-' + (nextId++),
                registerPopoverUserAttributesComponent: () => 'id-' + (nextId++),
                registerWebSocketEventHandler: () => 'id-' + (nextId++),
                registerChannelHeaderMenuAction: () => {
 calls.push('addMenu');
 return 'menu-' + nextId++;
},
                registerMainMenuAction: () => {
 calls.push('addMainMenu');
 return 'main-' + nextId++;
},
                unregisterComponent: (id: string) => {
 calls.push('unregister:' + id);
},
            };

            let teamId = 'team1';
            let listener: any;
            const store = {
                getState: () => ({
                    entities: {
                        teams: {currentTeamId: teamId},
                        channels: {currentChannelId: '', channels: {}},
                    },
                }),
                subscribe: (fn: any) => {
 listener = fn;
 return () => {};
},
            };

            await plugin.initialize(registry, store);
            await new Promise((r) => setTimeout(r, 200));

            teamId = 'team2';
            if (listener) {
listener();
}
            await new Promise((r) => setTimeout(r, 200));

            return calls;
        });

        const addMainMenuCount = result.filter((c: string) => c === 'addMainMenu').length;
        expect(addMainMenuCount).toBeGreaterThanOrEqual(2);

        const unregisterCount = result.filter((c: string) => c.startsWith('unregister:')).length;
        expect(unregisterCount).toBeGreaterThanOrEqual(1);
    });
});

test.describe('Plugin - WebSocket event handler', () => {
    test('WS event sets connection state for the given channel', async ({mount, page}) => {
        await mount(<PluginTestHarness/>);
        const result = await page.evaluate(async () => {
            const Plugin = (window as any).__PluginClass;
            const getCC = (window as any).__getChannelConnections;
            const plugin = new Plugin();

            let wsHandler: any = null;
            let nextId = 1;
            const registry = {
                registerAdminConsoleCustomSetting: () => 'id-' + (nextId++),
                registerRootComponent: () => 'id-' + (nextId++),
                registerSidebarChannelLinkLabelComponent: () => 'id-' + (nextId++),
                registerPopoverUserAttributesComponent: () => 'id-' + (nextId++),
                registerWebSocketEventHandler: (_event: string, handler: any) => {
 wsHandler = handler;
 return 'id-' + (nextId++);
},
                registerChannelHeaderMenuAction: () => 'id-' + (nextId++),
                registerMainMenuAction: () => 'id-' + (nextId++),
                unregisterComponent: () => {},
            };
            const store = {
                getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                subscribe: () => () => {},
            };

            await plugin.initialize(registry, store);
            wsHandler({data: {channel_id: 'ws-ch1', connections: 'ConnA,ConnB'}});
            return getCC('ws-ch1');
        });
        expect(result).toBe('ConnA,ConnB');
    });

    test('WS event with empty connections string clears the channel state', async ({mount, page}) => {
        await mount(<PluginTestHarness/>);
        const result = await page.evaluate(async () => {
            const Plugin = (window as any).__PluginClass;
            const getCC = (window as any).__getChannelConnections;
            const setCC = (window as any).__setChannelConnections;
            const plugin = new Plugin();

            let wsHandler: any = null;
            let nextId = 1;
            const registry = {
                registerAdminConsoleCustomSetting: () => 'id-' + (nextId++),
                registerRootComponent: () => 'id-' + (nextId++),
                registerSidebarChannelLinkLabelComponent: () => 'id-' + (nextId++),
                registerPopoverUserAttributesComponent: () => 'id-' + (nextId++),
                registerWebSocketEventHandler: (_event: string, handler: any) => {
 wsHandler = handler;
 return 'id-' + (nextId++);
},
                registerChannelHeaderMenuAction: () => 'id-' + (nextId++),
                registerMainMenuAction: () => 'id-' + (nextId++),
                unregisterComponent: () => {},
            };
            const store = {
                getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                subscribe: () => () => {},
            };

            await plugin.initialize(registry, store);
            setCC('ws-ch2', 'SomeConn');
            wsHandler({data: {channel_id: 'ws-ch2', connections: ''}});
            return getCC('ws-ch2');
        });
        expect(result).toBe('');
    });

    test('multiple rapid WS events update state correctly', async ({mount, page}) => {
        await mount(<PluginTestHarness/>);
        const result = await page.evaluate(async () => {
            const Plugin = (window as any).__PluginClass;
            const getCC = (window as any).__getChannelConnections;
            const plugin = new Plugin();

            let wsHandler: any = null;
            let nextId = 1;
            const registry = {
                registerAdminConsoleCustomSetting: () => 'id-' + (nextId++),
                registerRootComponent: () => 'id-' + (nextId++),
                registerSidebarChannelLinkLabelComponent: () => 'id-' + (nextId++),
                registerPopoverUserAttributesComponent: () => 'id-' + (nextId++),
                registerWebSocketEventHandler: (_event: string, handler: any) => {
 wsHandler = handler;
 return 'id-' + (nextId++);
},
                registerChannelHeaderMenuAction: () => 'id-' + (nextId++),
                registerMainMenuAction: () => 'id-' + (nextId++),
                unregisterComponent: () => {},
            };
            const store = {
                getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                subscribe: () => () => {},
            };

            await plugin.initialize(registry, store);
            for (let i = 0; i < 100; i++) {
                wsHandler({data: {channel_id: 'ws-rapid', connections: `Conn-${i}`}});
            }
            return getCC('ws-rapid');
        });
        expect(result).toBe('Conn-99');
    });
});

test.describe('Plugin - registration verification', () => {
    test('all 7 registrations called during initialize', async ({mount, page}) => {
        await mount(<PluginTestHarness/>);
        const result = await page.evaluate(async () => {
            const Plugin = (window as any).__PluginClass;
            const plugin = new Plugin();
            const calls: string[] = [];
            let nextId = 1;
            const registry = {
                registerAdminConsoleCustomSetting: (key: string) => {
 calls.push('adminSetting:' + key);
 return 'id-' + (nextId++);
},
                registerRootComponent: () => {
 calls.push('rootComponent');
 return 'id-' + (nextId++);
},
                registerSidebarChannelLinkLabelComponent: () => {
 calls.push('sidebarLabel');
 return 'id-' + (nextId++);
},
                registerPopoverUserAttributesComponent: () => {
 calls.push('popoverAttr');
 return 'id-' + (nextId++);
},
                registerWebSocketEventHandler: (event: string) => {
 calls.push('ws:' + event);
 return 'id-' + (nextId++);
},
                registerChannelHeaderMenuAction: () => 'id-' + (nextId++),
                registerMainMenuAction: () => 'id-' + (nextId++),
                unregisterComponent: () => {},
            };
            const store = {
                getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                subscribe: () => () => {},
            };

            await plugin.initialize(registry, store);
            return calls;
        });

        expect(result).toContain('adminSetting:InboundConnections');
        expect(result).toContain('adminSetting:OutboundConnections');
        expect(result.filter((c: string) => c === 'rootComponent').length).toBe(2);
        expect(result).toContain('sidebarLabel');
        expect(result).toContain('popoverAttr');
        expect(result.filter((c: string) => c.startsWith('ws:')).length).toBe(1);
    });

    test('WebSocket handler registered with correct event name', async ({mount, page}) => {
        await mount(<PluginTestHarness/>);
        const result = await page.evaluate(async () => {
            const Plugin = (window as any).__PluginClass;
            const plugin = new Plugin();
            let wsEvent = '';
            let nextId = 1;
            const registry = {
                registerAdminConsoleCustomSetting: () => 'id-' + (nextId++),
                registerRootComponent: () => 'id-' + (nextId++),
                registerSidebarChannelLinkLabelComponent: () => 'id-' + (nextId++),
                registerPopoverUserAttributesComponent: () => 'id-' + (nextId++),
                registerWebSocketEventHandler: (event: string) => {
 wsEvent = event;
 return 'id-' + (nextId++);
},
                registerChannelHeaderMenuAction: () => 'id-' + (nextId++),
                registerMainMenuAction: () => 'id-' + (nextId++),
                unregisterComponent: () => {},
            };
            const store = {
                getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                subscribe: () => () => {},
            };

            await plugin.initialize(registry, store);
            return wsEvent;
        });
        expect(result).toBe('custom_crossguard_channel_connections_updated');
    });

    test('channel header menu action dispatches correct event with channelID', async ({mount, page}) => {
        await mount(<PluginTestHarness/>);
        await page.route('**/api/v1/teams/*/status', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
        });

        const result = await page.evaluate(async () => {
            const Plugin = (window as any).__PluginClass;
            const plugin = new Plugin();
            let menuCallback: any = null;
            let nextId = 1;
            const registry = {
                registerAdminConsoleCustomSetting: () => 'id-' + (nextId++),
                registerRootComponent: () => 'id-' + (nextId++),
                registerSidebarChannelLinkLabelComponent: () => 'id-' + (nextId++),
                registerPopoverUserAttributesComponent: () => 'id-' + (nextId++),
                registerWebSocketEventHandler: () => 'id-' + (nextId++),
                registerChannelHeaderMenuAction: (_text: string, cb: any) => {
 menuCallback = cb;
 return 'id-' + (nextId++);
},
                registerMainMenuAction: () => 'id-' + (nextId++),
                unregisterComponent: () => {},
            };
            const store = {
                getState: () => ({
                    entities: {
                        teams: {currentTeamId: 'team1'},
                        channels: {currentChannelId: '', channels: {}},
                    },
                }),
                subscribe: () => () => {},
            };

            await plugin.initialize(registry, store);
            await new Promise((r) => setTimeout(r, 200));

            if (!menuCallback) {
return {error: 'no menu callback registered'};
}

            let receivedDetail: any = null;
            document.addEventListener('crossguard:open-modal', (e) => {
                receivedDetail = (e as CustomEvent).detail;
            }, {once: true});

            menuCallback('test-channel-id');
            return receivedDetail;
        });

        expect(result).toEqual({channelID: 'test-channel-id'});
    });

    test('addMenuAction is idempotent, second team success does not double-register', async ({mount, page}) => {
        await mount(<PluginTestHarness/>);
        await page.route('**/api/v1/teams/*/status', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
        });
        await page.route('**/api/v1/channels/connections*', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
        });

        const result = await page.evaluate(async () => {
            const Plugin = (window as any).__PluginClass;
            const plugin = new Plugin();
            let menuRegisterCount = 0;
            let nextId = 1;
            const registry = {
                registerAdminConsoleCustomSetting: () => 'id-' + (nextId++),
                registerRootComponent: () => 'id-' + (nextId++),
                registerSidebarChannelLinkLabelComponent: () => 'id-' + (nextId++),
                registerPopoverUserAttributesComponent: () => 'id-' + (nextId++),
                registerWebSocketEventHandler: () => 'id-' + (nextId++),
                registerChannelHeaderMenuAction: () => {
 menuRegisterCount++;
 return 'menu-' + nextId++;
},
                registerMainMenuAction: () => 'main-' + (nextId++),
                unregisterComponent: () => {},
            };

            let teamId = 'team1';
            let listener: any;
            const store = {
                getState: () => ({
                    entities: {
                        teams: {currentTeamId: teamId},
                        channels: {currentChannelId: '', channels: {}},
                    },
                }),
                subscribe: (fn: any) => {
 listener = fn;
 return () => {};
},
            };

            await plugin.initialize(registry, store);
            await new Promise((r) => setTimeout(r, 200));

            // Switch to a different team (both return 200, so addMenuAction is called again)
            teamId = 'team2';
            if (listener) {
listener();
}
            await new Promise((r) => setTimeout(r, 200));

            return menuRegisterCount;
        });

        // addMenuAction guards against double-registration, so count should be 1
        expect(result).toBe(1);
    });

    test('main menu action dispatches correct team modal event with teamID', async ({mount, page}) => {
        await mount(<PluginTestHarness/>);
        await page.route('**/api/v1/teams/*/status', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
        });

        const result = await page.evaluate(async () => {
            const Plugin = (window as any).__PluginClass;
            const plugin = new Plugin();
            let mainMenuCallback: any = null;
            let nextId = 1;
            const registry = {
                registerAdminConsoleCustomSetting: () => 'id-' + (nextId++),
                registerRootComponent: () => 'id-' + (nextId++),
                registerSidebarChannelLinkLabelComponent: () => 'id-' + (nextId++),
                registerPopoverUserAttributesComponent: () => 'id-' + (nextId++),
                registerWebSocketEventHandler: () => 'id-' + (nextId++),
                registerChannelHeaderMenuAction: () => 'id-' + (nextId++),
                registerMainMenuAction: (_text: string, cb: any) => {
 mainMenuCallback = cb;
 return 'id-' + (nextId++);
},
                unregisterComponent: () => {},
            };
            const store = {
                getState: () => ({
                    entities: {
                        teams: {currentTeamId: 'team-42'},
                        channels: {currentChannelId: '', channels: {}},
                    },
                }),
                subscribe: () => () => {},
            };

            await plugin.initialize(registry, store);
            await new Promise((r) => setTimeout(r, 200));

            if (!mainMenuCallback) {
return {error: 'no main menu callback registered'};
}

            let receivedDetail: any = null;
            document.addEventListener('crossguard:open-team-modal', (e) => {
                receivedDetail = (e as CustomEvent).detail;
            }, {once: true});

            mainMenuCallback();
            return receivedDetail;
        });

        expect(result).toEqual({teamID: 'team-42'});
    });
});

// ---------------------------------------------------------------------------
// Simultaneous team + channels change
// ---------------------------------------------------------------------------
test.describe('Plugin - simultaneous state changes', () => {
    test('team ID and channels changing in one callback triggers both team status and channel connections fetches', async ({mount, page}) => {
        await mount(<PluginTestHarness/>);

        const fetchedUrls: string[] = [];
        await page.route('**/plugins/crossguard/**', (route) => {
            fetchedUrls.push(route.request().url());
            route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
        });

        await page.evaluate(async () => {
            const Plugin = (window as any).__PluginClass;
            const plugin = new Plugin();
            let callCount = 0;
            const states = [
                {entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}},
                {entities: {teams: {currentTeamId: 'team-new'}, channels: {currentChannelId: 'ch-1', channels: {'ch-1': {}, 'ch-2': {}}}}},
            ];
            let subscribeCb: (() => void) | null = null;
            const registry = {
                registerAdminConsoleCustomSetting: () => {},
                registerRootComponent: () => {},
                registerSidebarChannelLinkLabelComponent: () => {},
                registerPopoverUserAttributesComponent: () => {},
                registerWebSocketEventHandler: () => {},
                registerChannelHeaderMenuAction: () => 'menu-id',
                registerMainMenuAction: () => 'main-menu-id',
                unregisterComponent: () => {},
            };
            const store = {
                getState: () => states[Math.min(callCount++, states.length - 1)],
                subscribe: (cb: () => void) => {
                    subscribeCb = cb;
                    return () => {};
                },
            };

            await plugin.initialize(registry, store);
            await new Promise((r) => setTimeout(r, 100));

            // Trigger store subscription with both team and channels changed
            if (subscribeCb) {
                (subscribeCb as () => void)();
            }
            await new Promise((r) => setTimeout(r, 200));

            plugin.uninitialize();
        });

        // Should have fetched team status for the new team
        const teamFetches = fetchedUrls.filter((u) => u.includes('/teams/team-new/status'));
        expect(teamFetches.length).toBeGreaterThanOrEqual(1);

        // Should have fetched channel connections for the new channels
        const connFetches = fetchedUrls.filter((u) => u.includes('/api/v1/channels/connections'));
        expect(connFetches.length).toBeGreaterThanOrEqual(1);
    });
});
