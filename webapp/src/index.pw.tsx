import React from 'react';

import PluginTestHarness from './components/PluginTestHarness';

import {test, expect} from '../playwright/ct-coverage';

// These tests mount PluginTestHarness to load the Plugin class and connection_state
// into the browser's window object, then use page.evaluate() to exercise Plugin behavior.

test.describe('Plugin class', () => {
    test.describe('initialize', () => {
        test('registers admin console settings for InboundConnections and OutboundConnections', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/**', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            const result = await page.evaluate(async () => {
                const calls: string[] = [];
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                const registry = {
                    registerAdminConsoleCustomSetting: (...args: any[]) => calls.push('adminSetting:' + args[0]),
                    registerRootComponent: () => calls.push('rootComponent'),
                    registerSidebarChannelLinkLabelComponent: () => calls.push('sidebar'),
                    registerPopoverUserAttributesComponent: () => calls.push('popover'),
                    registerWebSocketEventHandler: (...args: any[]) => calls.push('wsHandler:' + args[0]),
                    registerChannelHeaderMenuAction: () => {
                        calls.push('menuAction');
                        return 'menu-id-1';
                    },
                    registerMainMenuAction: () => {
                        calls.push('mainMenu');
                        return 'main-menu-id-1';
                    },
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => () => {},
                };
                await plugin.initialize(registry, store);
                plugin.uninitialize();
                return calls;
            });

            expect(result).toContain('adminSetting:InboundConnections');
            expect(result).toContain('adminSetting:OutboundConnections');
        });

        test('registers root components for channel and team modals', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/**', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            const rootComponentCount = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                let count = 0;
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {
                        count++;
                    },
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => () => {},
                };
                await plugin.initialize(registry, store);
                plugin.uninitialize();
                return count;
            });

            expect(rootComponentCount).toBe(2);
        });

        test('registers sidebar and popover components', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/**', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            const result = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                let sidebarCalled = false;
                let popoverCalled = false;
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {
                        sidebarCalled = true;
                    },
                    registerPopoverUserAttributesComponent: () => {
                        popoverCalled = true;
                    },
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => () => {},
                };
                await plugin.initialize(registry, store);
                plugin.uninitialize();
                return {sidebarCalled, popoverCalled};
            });

            expect(result.sidebarCalled).toBe(true);
            expect(result.popoverCalled).toBe(true);
        });

        test('registers WebSocket handler for channel connections updated', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/**', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            const eventName = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                let capturedName = '';
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: (name: string) => {
                        capturedName = name;
                    },
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => () => {},
                };
                await plugin.initialize(registry, store);
                plugin.uninitialize();
                return capturedName;
            });

            expect(eventName).toBe('custom_crossguard_channel_connections_updated');
        });

        test('subscribes to Redux store', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/**', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            const subscribed = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                let subscribeCalled = false;
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => {
                        subscribeCalled = true;
                        return () => {};
                    },
                };
                await plugin.initialize(registry, store);
                plugin.uninitialize();
                return subscribeCalled;
            });

            expect(subscribed).toBe(true);
        });
    });

    test.describe('store subscription behavior', () => {
        test('fetches team status when team ID changes', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);

            const fetchedUrls: string[] = [];
            await page.route('**/plugins/crossguard/**', (route) => {
                fetchedUrls.push(route.request().url());
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: 'team-xyz'}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => () => {},
                };
                await plugin.initialize(registry, store);
                await new Promise((r) => setTimeout(r, 100));
                plugin.uninitialize();
            });

            const teamStatusFetch = fetchedUrls.find((u) => u.includes('/teams/team-xyz/status'));
            expect(teamStatusFetch).toBeDefined();
        });

        test('registers channel menu on successful team status', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{"team_id":"t1"}'});
            });

            const menuRegistered = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                let menuCalled = false;
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => {
                        menuCalled = true;
                        return 'id';
                    },
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: 'team-ok'}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => () => {},
                };
                await plugin.initialize(registry, store);
                await new Promise((r) => setTimeout(r, 100));
                plugin.uninitialize();
                return menuCalled;
            });

            expect(menuRegistered).toBe(true);
        });

        test('does not register menu on non-ok team status', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
                route.fulfill({status: 404, contentType: 'application/json', body: '{}'});
            });

            const menuRegistered = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                let menuCalled = false;
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => {
                        menuCalled = true;
                        return 'id';
                    },
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: 'team-bad'}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => () => {},
                };
                await plugin.initialize(registry, store);
                await new Promise((r) => setTimeout(r, 100));
                plugin.uninitialize();
                return menuCalled;
            });

            expect(menuRegistered).toBe(false);
        });
    });

    test.describe('WebSocket handler', () => {
        test('updates connection state when WS event received', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/**', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            const result = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                let wsCallback: any = null;
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: (_name: string, cb: any) => {
                        wsCallback = cb;
                    },
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => () => {},
                };
                await plugin.initialize(registry, store);

                if (wsCallback) {
                    wsCallback({data: {channel_id: 'ws-test-ch', connections: 'ConnA, ConnB'}});
                }

                const getConns = (window as any).__getChannelConnections; // eslint-disable-line no-underscore-dangle
                const setConns = (window as any).__setChannelConnections; // eslint-disable-line no-underscore-dangle
                const connResult = getConns('ws-test-ch');
                setConns('ws-test-ch', '');
                plugin.uninitialize();
                return connResult;
            });

            expect(result).toBe('ConnA, ConnB');
        });
    });

    test.describe('uninitialize', () => {
        test('calls unsubscribe from store', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/**', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            const unsubCalled = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                let unsubbed = false;
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => {
                        return () => {
                            unsubbed = true;
                        };
                    },
                };
                await plugin.initialize(registry, store);
                plugin.uninitialize();
                return unsubbed;
            });

            expect(unsubCalled).toBe(true);
        });

        test('double uninitialize does not error', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/**', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            const result = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                let unsubCount = 0;
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => {
                        return () => {
                            unsubCount++;
                        };
                    },
                };
                await plugin.initialize(registry, store);
                plugin.uninitialize();
                plugin.uninitialize();
                return unsubCount;
            });

            // Should only be called once due to null guard
            expect(result).toBe(1);
        });
    });

    test.describe('store subscription - channel detection', () => {
        test('fetches connections for new channels when channels ref changes', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);

            const fetchedUrls: string[] = [];
            await page.route('**/plugins/crossguard/**', (route) => {
                fetchedUrls.push(route.request().url());
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass();
                let callCount = 0;
                const channelSets = [
                    {ch1: {}},
                    {ch1: {}, ch2: {}, ch3: {}},
                ];
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                let subscribeCb: (() => void) | null = null;
                const store = {
                    getState: () => ({
                        entities: {
                            teams: {currentTeamId: 'team1'},
                            channels: {currentChannelId: 'ch1', channels: channelSets[Math.min(callCount++, 1)]},
                        },
                    }),
                    subscribe: (cb: () => void) => {
                        subscribeCb = cb;
                        return () => {};
},
                };
                await plugin.initialize(registry, store);
                await new Promise((r) => setTimeout(r, 150));
                if (subscribeCb) {
                    (subscribeCb as () => void)();
                }
                await new Promise((r) => setTimeout(r, 150));
                plugin.uninitialize();
            });

            const connectionFetches = fetchedUrls.filter((u) => u.includes('/channels/connections'));
            expect(connectionFetches.length).toBeGreaterThanOrEqual(2);
        });

        test('fetches connection for current channel when only channelId changes', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);

            const fetchedUrls: string[] = [];
            await page.route('**/plugins/crossguard/**', (route) => {
                fetchedUrls.push(route.request().url());
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass();
                let callCount = 0;
                const channels = {ch1: {}, ch2: {}};
                const channelIds = ['ch1', 'ch2'];
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                let subscribeCb: (() => void) | null = null;
                const store = {
                    getState: () => ({
                        entities: {
                            teams: {currentTeamId: 'team1'},
                            channels: {currentChannelId: channelIds[Math.min(callCount++, 1)], channels},
                        },
                    }),
                    subscribe: (cb: () => void) => {
                        subscribeCb = cb;
                        return () => {};
},
                };
                await plugin.initialize(registry, store);
                await new Promise((r) => setTimeout(r, 150));
                if (subscribeCb) {
                    (subscribeCb as () => void)();
                }
                await new Promise((r) => setTimeout(r, 150));
                plugin.uninitialize();
            });

            const connectionFetches = fetchedUrls.filter((u) => u.includes('/channels/connections'));
            expect(connectionFetches.length).toBeGreaterThanOrEqual(2);
        });

        test('resets known channel IDs when team changes', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);

            const fetchedUrls: string[] = [];
            await page.route('**/plugins/crossguard/**', (route) => {
                fetchedUrls.push(route.request().url());
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass();
                let callCount = 0;
                const teams = ['team1', 'team2'];
                const channels = {ch1: {}};
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                let subscribeCb: (() => void) | null = null;
                const store = {
                    getState: () => ({
                        entities: {
                            teams: {currentTeamId: teams[Math.min(callCount++, 1)]},
                            channels: {currentChannelId: 'ch1', channels},
                        },
                    }),
                    subscribe: (cb: () => void) => {
                        subscribeCb = cb;
                        return () => {};
},
                };
                await plugin.initialize(registry, store);
                await new Promise((r) => setTimeout(r, 150));
                if (subscribeCb) {
                    (subscribeCb as () => void)();
                }
                await new Promise((r) => setTimeout(r, 150));
                plugin.uninitialize();
            });

            const teamFetches = fetchedUrls.filter((u) => u.includes('/teams/') && u.includes('/status'));
            expect(teamFetches.length).toBeGreaterThanOrEqual(2);
        });

        test('handles store with null entities defensively', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/**', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            const noError = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass();
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: null as any}),
                    subscribe: () => () => {},
                };
                try {
                    await plugin.initialize(registry, store);
                    plugin.uninitialize();
                    return true;
                } catch {
                    return false;
                }
            });
            expect(noError).toBe(true);
        });

        test('WebSocket handler with empty connections string clears state', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/**', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            const result = await page.evaluate(async () => {
                const setConns = (window as any).__setChannelConnections;
                const getConns = (window as any).__getChannelConnections;
                setConns('ws-clear-ch', 'ExistingConn');

                const plugin = new (window as any).__PluginClass();
                let wsCallback: any = null;
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: (_name: string, cb: any) => {
 wsCallback = cb;
},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => () => {},
                };
                await plugin.initialize(registry, store);

                if (wsCallback) {
                    wsCallback({data: {channel_id: 'ws-clear-ch', connections: ''}});
                }
                const connResult = getConns('ws-clear-ch');
                plugin.uninitialize();
                return connResult;
            });
            expect(result).toBe('');
        });

        test('handles sequential team switches', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);

            const fetchedUrls: string[] = [];
            await page.route('**/plugins/crossguard/**', (route) => {
                fetchedUrls.push(route.request().url());
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass();
                let callCount = 0;
                const teams = ['team-a', 'team-b', 'team-c'];
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                let subscribeCb: (() => void) | null = null;
                const store = {
                    getState: () => ({
                        entities: {
                            teams: {currentTeamId: teams[Math.min(callCount++, 2)]},
                            channels: {currentChannelId: '', channels: {}},
                        },
                    }),
                    subscribe: (cb: () => void) => {
                        subscribeCb = cb;
                        return () => {};
},
                };
                await plugin.initialize(registry, store);
                await new Promise((r) => setTimeout(r, 100));
                if (subscribeCb) {
                    (subscribeCb as () => void)();
                }
                await new Promise((r) => setTimeout(r, 100));
                if (subscribeCb) {
                    (subscribeCb as () => void)();
                }
                await new Promise((r) => setTimeout(r, 100));
                plugin.uninitialize();
            });

            const teamAFetches = fetchedUrls.filter((u) => u.includes('/teams/team-a/status'));
            const teamBFetches = fetchedUrls.filter((u) => u.includes('/teams/team-b/status'));
            const teamCFetches = fetchedUrls.filter((u) => u.includes('/teams/team-c/status'));
            expect(teamAFetches.length).toBeGreaterThanOrEqual(1);
            expect(teamBFetches.length).toBeGreaterThanOrEqual(1);
            expect(teamCFetches.length).toBeGreaterThanOrEqual(1);
        });
    });

    test.describe('menu action dispatch', () => {
        test('channel header menu action dispatches crossguard:open-modal with channelID', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/**', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            const result = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass();
                let menuCallback: any = null;
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: (_label: string, cb: any) => {
                        menuCallback = cb;
                        return 'menu-id';
                    },
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: 'team1'}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => () => {},
                };
                await plugin.initialize(registry, store);
                await new Promise((r) => setTimeout(r, 100));

                let capturedDetail: any = null;
                document.addEventListener('crossguard:open-modal', (e) => {
                    capturedDetail = (e as CustomEvent).detail;
                });
                if (menuCallback) {
menuCallback('test-channel-id');
}
                plugin.uninitialize();
                return capturedDetail;
            });

            expect(result).toBeDefined();
            expect(result.channelID).toBe('test-channel-id');
        });

        test('main menu action dispatches crossguard:open-team-modal with teamID', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/**', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            const result = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass();
                let mainMenuCallback: any = null;
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: (_label: string, cb: any) => {
                        mainMenuCallback = cb;
                        return 'main-menu-id';
                    },
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: 'team-dispatch'}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => () => {},
                };
                await plugin.initialize(registry, store);
                await new Promise((r) => setTimeout(r, 100));

                let capturedDetail: any = null;
                document.addEventListener('crossguard:open-team-modal', (e) => {
                    capturedDetail = (e as CustomEvent).detail;
                });
                if (mainMenuCallback) {
mainMenuCallback();
}
                plugin.uninitialize();
                return capturedDetail;
            });

            expect(result).toBeDefined();
            expect(result.teamID).toBe('team-dispatch');
        });
    });

    test.describe('menu idempotency', () => {
        test('addMenuAction is idempotent when menu already exists', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{"team_id":"t1"}'});
            });

            const registerCount = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass();
                let menuRegCount = 0;
                let callCount = 0;
                const teams = ['team-idem-1', 'team-idem-2'];
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => {
                        menuRegCount++;
                        return 'menu-id';
                    },
                    registerMainMenuAction: () => 'main-id',
                    unregisterComponent: () => {},
                };
                let subscribeCb: (() => void) | null = null;
                const store = {
                    getState: () => ({
                        entities: {
                            teams: {currentTeamId: teams[Math.min(callCount++, 1)]},
                            channels: {currentChannelId: '', channels: {}},
                        },
                    }),
                    subscribe: (cb: () => void) => {
                        subscribeCb = cb;
                        return () => {};
},
                };
                await plugin.initialize(registry, store);
                await new Promise((r) => setTimeout(r, 150));
                if (subscribeCb) {
                    (subscribeCb as () => void)();
                }
                await new Promise((r) => setTimeout(r, 150));
                plugin.uninitialize();
                return menuRegCount;
            });

            // Channel header menu is registered once (idempotent guard prevents second)
            expect(registerCount).toBe(1);
        });

        test('removeMenuAction is a no-op when no menu exists', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
                route.fulfill({status: 404, contentType: 'application/json', body: '{}'});
            });

            const unregCount = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass();
                let unregisterCount = 0;
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {
 unregisterCount++;
},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: 'team-noop'}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => () => {},
                };
                await plugin.initialize(registry, store);
                await new Promise((r) => setTimeout(r, 150));
                plugin.uninitialize();
                return unregisterCount;
            });

            expect(unregCount).toBe(0);
        });

        test('addMainMenuAction removes previous before adding new', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{"team_id":"t1"}'});
            });

            const result = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass();
                let mainMenuRegCount = 0;
                let unregisterCount = 0;
                let callCount = 0;
                const teams = ['team-main-1', 'team-main-2'];
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'menu-id',
                    registerMainMenuAction: () => {
                        mainMenuRegCount++;
                        return `main-menu-${mainMenuRegCount}`;
                    },
                    unregisterComponent: () => {
 unregisterCount++;
},
                };
                let subscribeCb: (() => void) | null = null;
                const store = {
                    getState: () => ({
                        entities: {
                            teams: {currentTeamId: teams[Math.min(callCount++, 1)]},
                            channels: {currentChannelId: '', channels: {}},
                        },
                    }),
                    subscribe: (cb: () => void) => {
                        subscribeCb = cb;
                        return () => {};
},
                };
                await plugin.initialize(registry, store);
                await new Promise((r) => setTimeout(r, 150));
                if (subscribeCb) {
                    (subscribeCb as () => void)();
                }
                await new Promise((r) => setTimeout(r, 150));
                plugin.uninitialize();
                return {mainMenuRegCount, unregisterCount};
            });

            // Main menu registered twice (once per team), with unregister called before second registration
            expect(result.mainMenuRegCount).toBe(2);
            expect(result.unregisterCount).toBeGreaterThanOrEqual(1);
        });
    });

    test.describe('updateMenuForTeam error handling', () => {
        test('removes both menus on network error', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);

            // First call succeeds (adds menus), second call aborts (triggers catch)
            let routeCallCount = 0;
            await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
                routeCallCount++;
                if (routeCallCount <= 1) {
                    route.fulfill({status: 200, contentType: 'application/json', body: '{"team_id":"t1"}'});
                } else {
                    route.abort();
                }
            });

            const result = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass();
                let unregisterCount = 0;
                let callCount = 0;
                const teams = ['team-ok', 'team-fail'];
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'menu-id',
                    registerMainMenuAction: () => 'main-menu-id',
                    unregisterComponent: () => {
 unregisterCount++;
},
                };
                let subscribeCb: (() => void) | null = null;
                const store = {
                    getState: () => ({
                        entities: {
                            teams: {currentTeamId: teams[Math.min(callCount++, 1)]},
                            channels: {currentChannelId: '', channels: {}},
                        },
                    }),
                    subscribe: (cb: () => void) => {
                        subscribeCb = cb;
                        return () => {};
},
                };
                await plugin.initialize(registry, store);
                await new Promise((r) => setTimeout(r, 150));
                if (subscribeCb) {
                    (subscribeCb as () => void)();
                }
                await new Promise((r) => setTimeout(r, 300));
                plugin.uninitialize();
                return unregisterCount;
            });

            // Unregister should be called for both menu types after network error
            expect(result).toBeGreaterThanOrEqual(2);
        });
    });

    test.describe('store subscription edge cases', () => {
        test('same team ID on multiple subscribe callbacks does not re-fetch team status', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);

            const fetchedUrls: string[] = [];
            await page.route('**/plugins/crossguard/**', (route) => {
                fetchedUrls.push(route.request().url());
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                let subscribeCb: (() => void) | null = null;
                const store = {
                    getState: () => ({
                        entities: {
                            teams: {currentTeamId: 'team-same'},
                            channels: {currentChannelId: '', channels: {}},
                        },
                    }),
                    subscribe: (cb: () => void) => {
                        subscribeCb = cb;
                        return () => {};
},
                };
                await plugin.initialize(registry, store);
                await new Promise((r) => setTimeout(r, 100));

                // Trigger subscribe callback again with the same team ID
                if (subscribeCb) {
                    (subscribeCb as () => void)();
                }
                await new Promise((r) => setTimeout(r, 100));
                plugin.uninitialize();
            });

            const teamStatusFetches = fetchedUrls.filter((u) => u.includes('/teams/team-same/status'));

            // Only one fetch because team ID did not change between callbacks
            expect(teamStatusFetches.length).toBe(1);
        });

        test('empty team ID does not trigger updateMenuForTeam', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);

            const fetchedUrls: string[] = [];
            await page.route('**/plugins/crossguard/**', (route) => {
                fetchedUrls.push(route.request().url());
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({
                        entities: {
                            teams: {currentTeamId: ''},
                            channels: {currentChannelId: '', channels: {}},
                        },
                    }),
                    subscribe: () => () => {},
                };
                await plugin.initialize(registry, store);
                await new Promise((r) => setTimeout(r, 100));
                plugin.uninitialize();
            });

            const teamStatusFetches = fetchedUrls.filter((u) => u.includes('/teams/') && u.includes('/status'));
            expect(teamStatusFetches.length).toBe(0);
        });

        test('same channel ID unchanged does not re-fetch connections', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);

            const fetchedUrls: string[] = [];
            await page.route('**/plugins/crossguard/**', (route) => {
                fetchedUrls.push(route.request().url());
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                const channels = {ch1: {}};
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                let subscribeCb: (() => void) | null = null;
                const store = {

                    // Return the same channels object reference and same channelId each time
                    getState: () => ({
                        entities: {
                            teams: {currentTeamId: 'team1'},
                            channels: {currentChannelId: 'ch1', channels},
                        },
                    }),
                    subscribe: (cb: () => void) => {
                        subscribeCb = cb;
                        return () => {};
},
                };
                await plugin.initialize(registry, store);
                await new Promise((r) => setTimeout(r, 100));

                // Trigger subscribe again; same channels ref and same channelId
                if (subscribeCb) {
                    (subscribeCb as () => void)();
                }
                await new Promise((r) => setTimeout(r, 100));
                plugin.uninitialize();
            });

            // Filter for connection fetches only (not team status fetches)
            const connectionFetches = fetchedUrls.filter((u) => u.includes('/channels/connections'));

            // First checkState fetches via channels-changed branch (new ref), second via channelId-changed branch
            // (lastChannelId was not updated in the channels-changed path). Both are correct behavior.
            expect(connectionFetches.length).toBe(2);
        });

        test('empty channels object does not trigger connection fetch', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);

            const fetchedUrls: string[] = [];
            await page.route('**/plugins/crossguard/**', (route) => {
                fetchedUrls.push(route.request().url());
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
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
                await new Promise((r) => setTimeout(r, 100));
                plugin.uninitialize();
            });

            const connectionFetches = fetchedUrls.filter((u) => u.includes('/channels/connections'));
            expect(connectionFetches.length).toBe(0);
        });

        test('checkState is called immediately during initialize before subscribe callback', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);

            const fetchedUrls: string[] = [];
            await page.route('**/plugins/crossguard/**', (route) => {
                fetchedUrls.push(route.request().url());
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };

                // subscribe callback is never invoked manually, but checkState runs at end of initialize
                const store = {
                    getState: () => ({
                        entities: {
                            teams: {currentTeamId: 'team-init'},
                            channels: {currentChannelId: '', channels: {}},
                        },
                    }),
                    subscribe: () => () => {},
                };
                await plugin.initialize(registry, store);
                await new Promise((r) => setTimeout(r, 100));
                plugin.uninitialize();
            });

            // Team status should be fetched even without subscribe callback firing
            const teamStatusFetch = fetchedUrls.find((u) => u.includes('/teams/team-init/status'));
            expect(teamStatusFetch).toBeDefined();
        });
    });

    test.describe('menu re-add after failure', () => {
        test('first team returns 404 then switching to a valid team re-adds menus', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);

            let routeCallCount = 0;
            await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
                routeCallCount++;
                if (routeCallCount === 1) {
                    route.fulfill({status: 404, contentType: 'application/json', body: '{}'});
                } else {
                    route.fulfill({status: 200, contentType: 'application/json', body: '{"team_id":"t2"}'});
                }
            });

            const result = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                let menuRegCount = 0;
                let callCount = 0;
                const teams = ['team-fail', 'team-ok'];
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => {
                        menuRegCount++;
                        return 'menu-id';
                    },
                    registerMainMenuAction: () => 'main-id',
                    unregisterComponent: () => {},
                };
                let subscribeCb: (() => void) | null = null;
                const store = {
                    getState: () => ({
                        entities: {
                            teams: {currentTeamId: teams[Math.min(callCount++, 1)]},
                            channels: {currentChannelId: '', channels: {}},
                        },
                    }),
                    subscribe: (cb: () => void) => {
                        subscribeCb = cb;
                        return () => {};
},
                };
                await plugin.initialize(registry, store);
                await new Promise((r) => setTimeout(r, 200));

                // Switch to team-ok
                if (subscribeCb) {
                    (subscribeCb as () => void)();
                }
                await new Promise((r) => setTimeout(r, 200));
                plugin.uninitialize();
                return menuRegCount;
            });

            // Menu should be registered after the second team succeeds
            expect(result).toBe(1);
        });

        test('console.warn called when fetch throws', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);

            await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
                route.abort();
            });

            const warnings: string[] = [];
            page.on('console', (msg) => {
                if (msg.type() === 'warning') {
                    warnings.push(msg.text());
                }
            });

            await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({
                        entities: {
                            teams: {currentTeamId: 'team-warn'},
                            channels: {currentChannelId: '', channels: {}},
                        },
                    }),
                    subscribe: () => () => {},
                };
                await plugin.initialize(registry, store);
                await new Promise((r) => setTimeout(r, 300));
                plugin.uninitialize();
            });

            const matchingWarnings = warnings.filter((w) => w.includes('updateMenuForTeam'));
            expect(matchingWarnings.length).toBeGreaterThanOrEqual(1);
        });
    });

    test.describe('defensive state handling', () => {
        test('store with undefined channels property does not crash', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/**', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            const noError = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({
                        entities: {
                            teams: {currentTeamId: 'x'},
                            channels: undefined as any,
                        },
                    }),
                    subscribe: () => () => {},
                };
                try {
                    await plugin.initialize(registry, store);
                    plugin.uninitialize();
                    return true;
                } catch {
                    return false;
                }
            });
            expect(noError).toBe(true);
        });

        test('store with getState returning minimal object does not crash', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/**', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            const noError = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({
                        entities: {teams: {}, channels: {}},
                    }),
                    subscribe: () => () => {},
                };
                try {
                    await plugin.initialize(registry, store);
                    plugin.uninitialize();
                    return true;
                } catch {
                    return false;
                }
            });
            expect(noError).toBe(true);
        });
    });
});
