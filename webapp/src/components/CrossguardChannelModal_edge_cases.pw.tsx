import React from 'react';

import CrossguardChannelModal from './CrossguardChannelModal';

import {test, expect} from '../../playwright/ct-coverage';

function statusResponse(overrides?: any) {
    return {channel_id: 'ch1', channel_name: 'town-square', channel_display_name: 'Town Square', team_name: 'Test Team', team_connections: [], ...overrides};
}

function connStatus(overrides?: any) {
    return {name: 'my-conn', direction: 'inbound', linked: false, orphaned: false, file_transfer_enabled: false, ...overrides};
}

async function openModal(page: any, channelID = 'ch1') {
    await page.evaluate((id: string) => {
        document.dispatchEvent(new CustomEvent('crossguard:open-modal', {detail: {channelID: id}}));
    }, channelID);
}

async function setCsrfCookie(page: any) {
    await page.evaluate(() => {
        Object.defineProperty(document, 'cookie', {
            get: () => 'MMCSRF=test-csrf-token',
            configurable: true,
        });
    });
}

async function routeStatusOk(page: any, body?: unknown) {
    const responseBody = body || statusResponse({team_connections: [connStatus()]});
    await page.route('**/plugins/crossguard/api/v1/channels/*/status', (route: any) => {
        route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(responseBody)});
    });
}

// ---------------------------------------------------------------------------
// 1. Modal lifecycle
// ---------------------------------------------------------------------------
test.describe('Modal lifecycle edge cases', () => {
    test('opening for a different channel resets previous state', async ({mount, page}) => {
        let callCount = 0;
        await page.route('**/plugins/crossguard/api/v1/channels/*/status', (route: any) => {
            callCount++;
            const body = callCount === 1 ? statusResponse({team_connections: [connStatus({name: 'first-conn'})], channel_display_name: 'First'}) : statusResponse({team_connections: [connStatus({name: 'second-conn'})], channel_display_name: 'Second'});
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(body)});
        });
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.getByText('first-conn')).toBeVisible();

        await component.getByText('\u00D7').click();
        await openModal(page, 'ch2');
        await expect(component.getByText('second-conn')).toBeVisible();
        await expect(component.getByText('first-conn')).not.toBeVisible();
    });

    test('re-opening the same channel re-fetches status', async ({mount, page}) => {
        let callCount = 0;
        await page.route('**/plugins/crossguard/api/v1/channels/*/status', (route: any) => {
            callCount++;
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(statusResponse({team_connections: [connStatus()]}))});
        });
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.getByText('my-conn')).toBeVisible();
        const firstCount = callCount;

        await component.getByText('\u00D7').click();
        await openModal(page, 'ch1');
        await expect(component.getByText('my-conn')).toBeVisible();
        expect(callCount).toBeGreaterThan(firstCount);
    });

    test('re-opening replaces previous content with new data', async ({mount, page}) => {
        let callCount = 0;
        await page.route('**/plugins/crossguard/api/v1/channels/*/status', (route: any) => {
            callCount++;
            const body = callCount === 1 ? statusResponse({team_connections: [connStatus({name: 'alpha'})], team_name: 'Team Alpha', channel_display_name: 'Chan A'}) : statusResponse({team_connections: [connStatus({name: 'beta'})], team_name: 'Team Beta', channel_display_name: 'Chan B'});
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(body)});
        });
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.getByText('alpha', {exact: true})).toBeVisible();

        await component.getByText('\u00D7').click();
        await openModal(page, 'ch2');
        await expect(component.getByText('beta', {exact: true})).toBeVisible();
        await expect(component.getByText('Team Beta > Chan B')).toBeVisible();
    });

    test('null channelID in event detail does not render modal', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await page.evaluate(() => {
            document.dispatchEvent(new CustomEvent('crossguard:open-modal', {detail: {channelID: null}}));
        });
        await expect(component).toBeEmpty();
    });

    test('missing channelID in event detail does not render modal', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await page.evaluate(() => {
            document.dispatchEvent(new CustomEvent('crossguard:open-modal', {detail: {}}));
        });
        await expect(component).toBeEmpty();
    });
});

// ---------------------------------------------------------------------------
// 2. Fetch errors
// ---------------------------------------------------------------------------
test.describe('Fetch errors edge cases', () => {
    test('500 with error JSON shows the error message', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/channels/*/status', (route: any) => {
            route.fulfill({status: 500, contentType: 'application/json', body: JSON.stringify({error: 'Internal server error'})});
        });
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.getByText('Internal server error')).toBeVisible();
    });

    test('500 with non-JSON body shows network error', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/channels/*/status', (route: any) => {
            route.fulfill({status: 500, contentType: 'text/plain', body: 'Gateway Timeout'});
        });
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.getByText('Network error loading channel status.')).toBeVisible();
    });

    test('403 shows permission error from response', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/channels/*/status', (route: any) => {
            route.fulfill({status: 403, contentType: 'application/json', body: JSON.stringify({error: 'Forbidden'})});
        });
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.getByText('Forbidden')).toBeVisible();
    });

    test('network abort shows network error message', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/channels/*/status', (route: any) => {
            route.abort('connectionfailed');
        });
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.getByText('Network error loading channel status.')).toBeVisible();
    });

    test('null team_connections shows empty state', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/channels/*/status', (route: any) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(statusResponse({team_connections: null}))});
        });
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.getByText('No connections available. Configure connections in the System Console.')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 3. Connection cards
// ---------------------------------------------------------------------------
test.describe('Connection cards edge cases', () => {
    test('mixed inbound and outbound badges render correctly', async ({mount, page}) => {
        const body = statusResponse({
            team_connections: [
                connStatus({name: 'in-1', direction: 'inbound'}),
                connStatus({name: 'out-1', direction: 'outbound'}),
                connStatus({name: 'in-2', direction: 'inbound'}),
            ],
        });
        await routeStatusOk(page, body);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        const inboundBadges = component.getByText('NATS \u2192 MATTERMOST');
        const outboundBadges = component.getByText('MATTERMOST \u2192 NATS');
        await expect(inboundBadges).toHaveCount(2);
        await expect(outboundBadges).toHaveCount(1);
    });

    test('orphaned indicator is displayed for orphaned connections', async ({mount, page}) => {
        const body = statusResponse({
            team_connections: [connStatus({name: 'orphan', orphaned: true})],
        });
        await routeStatusOk(page, body);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        const orphanMarker = component.locator('span[title="Connection no longer in configuration"]');
        await expect(orphanMarker).toBeVisible();
    });

    test('remote_team_name is displayed when present', async ({mount, page}) => {
        const body = statusResponse({
            team_connections: [connStatus({name: 'conn-r', direction: 'outbound', linked: true, remote_team_name: 'remote-alpha'})],
        });
        await routeStatusOk(page, body);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.getByText('Remote team: remote-alpha')).toBeVisible();
    });

    test('allow filter mode displays correctly', async ({mount, page}) => {
        const body = statusResponse({
            team_connections: [connStatus({file_transfer_enabled: true, file_filter_mode: 'allow', file_filter_types: '.png,.jpg'})],
        });
        await routeStatusOk(page, body);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.getByText('Allow .png,.jpg')).toBeVisible();
    });

    test('deny filter mode displays correctly', async ({mount, page}) => {
        const body = statusResponse({
            team_connections: [connStatus({file_transfer_enabled: true, file_filter_mode: 'deny', file_filter_types: '.exe,.bat'})],
        });
        await routeStatusOk(page, body);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.getByText('Deny .exe,.bat')).toBeVisible();
    });

    test('no filter mode shows All types', async ({mount, page}) => {
        const body = statusResponse({
            team_connections: [connStatus({file_transfer_enabled: true})],
        });
        await routeStatusOk(page, body);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.getByText('All types')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 4. Link/Unlink
// ---------------------------------------------------------------------------
test.describe('Link/Unlink edge cases', () => {
    test('link URL encodes the connection name correctly', async ({mount, page}) => {
        const body = statusResponse({
            team_connections: [connStatus({name: 'special conn/name', direction: 'inbound', linked: false})],
        });
        await routeStatusOk(page, body);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        let capturedUrl = '';
        await page.route('**/plugins/crossguard/api/v1/channels/ch1/init*', (route: any) => {
            capturedUrl = route.request().url();
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(() => {
            expect(capturedUrl).toContain('connection_name=inbound%3Aspecial%20conn%2Fname');
        }).toPass();
    });

    test('unlink posts to the teardown endpoint', async ({mount, page}) => {
        const body = statusResponse({
            team_connections: [connStatus({name: 'teardown-conn', direction: 'outbound', linked: true})],
        });
        await routeStatusOk(page, body);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.getByRole('button', {name: 'Unlink', exact: true})).toBeVisible();

        let capturedUrl = '';
        await page.route('**/plugins/crossguard/api/v1/channels/ch1/teardown*', (route: any) => {
            capturedUrl = route.request().url();
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Unlink', exact: true}).click();
        await expect(() => {
            expect(capturedUrl).toContain('/teardown');
            expect(capturedUrl).toContain('connection_name=outbound%3Ateardown-conn');
        }).toPass();
    });

    test('all buttons disabled during an action', async ({mount, page}) => {
        const body = statusResponse({
            team_connections: [
                connStatus({name: 'conn-a', direction: 'inbound', linked: false}),
                connStatus({name: 'conn-b', direction: 'outbound', linked: true}),
            ],
        });
        await routeStatusOk(page, body);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        await page.route('**/plugins/crossguard/api/v1/channels/ch1/init*', async (route: any) => {
            await new Promise((resolve) => setTimeout(resolve, 2000));
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(component.getByText('Linking...')).toBeVisible();
        await expect(component.getByRole('button', {name: 'Unlink', exact: true})).toBeDisabled();
    });

    test('re-fetch after success shows updated state', async ({mount, page}) => {
        let fetchCount = 0;
        await page.route('**/plugins/crossguard/api/v1/channels/*/status', (route: any) => {
            fetchCount++;
            const conns = fetchCount === 1 ? [connStatus({name: 'toggle-conn', linked: false})] : [connStatus({name: 'toggle-conn', linked: true})];
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(statusResponse({team_connections: conns}))});
        });
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        await page.route('**/plugins/crossguard/api/v1/channels/ch1/init*', (route: any) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(component.getByRole('button', {name: 'Unlink', exact: true})).toBeVisible();
    });

    test('error banner displays data.error from failed action', async ({mount, page}) => {
        const body = statusResponse({
            team_connections: [connStatus({name: 'err-conn', linked: false})],
        });
        await routeStatusOk(page, body);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        await page.route('**/plugins/crossguard/api/v1/channels/ch1/init*', (route: any) => {
            route.fulfill({status: 400, contentType: 'application/json', body: JSON.stringify({error: 'Channel is archived'})});
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(component.getByText('Channel is archived')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 5. Status banner
// ---------------------------------------------------------------------------
test.describe('Status banner edge cases', () => {
    test('success banner has green-tinted background', async ({mount, page}) => {
        const body = statusResponse({
            team_connections: [connStatus({name: 'link-me', linked: false})],
        });
        await routeStatusOk(page, body);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        await page.route('**/plugins/crossguard/api/v1/channels/ch1/init*', (route: any) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Link', exact: true}).click();

        const banner = component.getByText('Connection "link-me" linked.');
        await expect(banner).toBeVisible();
        const bgColor = await banner.evaluate((el: Element) => {
            const parent = el.closest('div[style]');
            return parent ? getComputedStyle(parent).backgroundColor : '';
        });
        expect(bgColor).toContain('61, 184, 135');
    });

    test('error banner has red-tinted background', async ({mount, page}) => {
        const body = statusResponse({
            team_connections: [connStatus({name: 'fail-conn', linked: false})],
        });
        await routeStatusOk(page, body);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        await page.route('**/plugins/crossguard/api/v1/channels/ch1/init*', (route: any) => {
            route.fulfill({status: 400, contentType: 'application/json', body: JSON.stringify({error: 'Something went wrong'})});
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Link', exact: true}).click();

        const banner = component.getByText('Something went wrong');
        await expect(banner).toBeVisible();
        const bgColor = await banner.evaluate((el: Element) => {
            const parent = el.closest('div[style]');
            return parent ? getComputedStyle(parent).backgroundColor : '';
        });
        expect(bgColor).toContain('210, 75, 78');
    });
});

// ---------------------------------------------------------------------------
// 6. Keyboard/mouse
// ---------------------------------------------------------------------------
test.describe('Keyboard/mouse edge cases', () => {
    test('Escape closes the modal', async ({mount, page}) => {
        await routeStatusOk(page, statusResponse({team_connections: [connStatus()]}));
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.locator('h2')).toBeVisible();
        await page.keyboard.press('Escape');
        await expect(component).toBeEmpty();
    });

    test('clicking modal body does not close the modal', async ({mount, page}) => {
        await routeStatusOk(page, statusResponse({team_connections: [connStatus()]}));
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.locator('h2')).toBeVisible();
        await component.locator('h2').click();
        await expect(component.locator('h2')).toBeVisible();
    });

    test('backdrop click closes the modal', async ({mount, page}) => {
        await routeStatusOk(page, statusResponse({team_connections: [connStatus()]}));
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.locator('h2')).toBeVisible();
        await page.mouse.click(5, 5);
        await expect(component).toBeEmpty();
    });
});

// ---------------------------------------------------------------------------
// 7. handleToggle error paths
// ---------------------------------------------------------------------------
test.describe('handleToggle error paths', () => {
    test('network abort during toggle shows Network error banner', async ({mount, page}) => {
        const body = statusResponse({
            team_connections: [connStatus({name: 'net-fail', linked: false})],
        });
        await routeStatusOk(page, body);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        await page.route('**/plugins/crossguard/api/v1/channels/ch1/init*', (route: any) => {
            route.abort('connectionfailed');
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(component.getByText('Network error.')).toBeVisible();
    });

    test('API error without error field shows fallback message', async ({mount, page}) => {
        const body = statusResponse({
            team_connections: [connStatus({name: 'fallback-conn', linked: false})],
        });
        await routeStatusOk(page, body);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        await page.route('**/plugins/crossguard/api/v1/channels/ch1/init*', (route: any) => {
            route.fulfill({status: 400, contentType: 'application/json', body: JSON.stringify({status: 'fail'})});
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(component.getByText('Failed to link connection.')).toBeVisible();
    });

    test('successful link shows Connection linked message', async ({mount, page}) => {
        const body = statusResponse({
            team_connections: [connStatus({name: 'success-conn', linked: false})],
        });
        await routeStatusOk(page, body);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        await page.route('**/plugins/crossguard/api/v1/channels/ch1/init*', (route: any) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(component.getByText('Connection "success-conn" linked.')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 8. CSRF token verification
// ---------------------------------------------------------------------------
test.describe('CSRF token verification', () => {
    test('callAPI includes X-CSRF-Token header from cookie', async ({mount, page}) => {
        const body = statusResponse({
            team_connections: [connStatus({name: 'csrf-conn', linked: false})],
        });
        await routeStatusOk(page, body);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        let capturedHeaders: Record<string, string> = {};
        await page.route('**/plugins/crossguard/api/v1/channels/ch1/init*', (route: any) => {
            capturedHeaders = route.request().headers();
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(() => {
            expect(capturedHeaders['x-csrf-token']).toBe('test-csrf-token');
        }).toPass();
    });
});

// ---------------------------------------------------------------------------
// 9. Files display edge cases
// ---------------------------------------------------------------------------
test.describe('Files display edge cases', () => {
    test('connection with file_transfer_enabled false shows Files Disabled text', async ({mount, page}) => {
        const body = statusResponse({
            team_connections: [connStatus({name: 'no-files', file_transfer_enabled: false})],
        });
        await routeStatusOk(page, body);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.getByText('Files: Disabled')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 10. Multiple connections mixed state
// ---------------------------------------------------------------------------
test.describe('Multiple connections mixed state', () => {
    test('4 connections with various states all render correctly', async ({mount, page}) => {
        const body = statusResponse({
            team_connections: [
                connStatus({name: 'in-linked', direction: 'inbound', linked: true, file_transfer_enabled: true, file_filter_mode: 'allow', file_filter_types: '.pdf'}),
                connStatus({name: 'out-unlinked', direction: 'outbound', linked: false, file_transfer_enabled: false}),
                connStatus({name: 'orphan-conn', direction: 'inbound', linked: false, orphaned: true, file_transfer_enabled: true}),
                connStatus({name: 'out-files', direction: 'outbound', linked: true, file_transfer_enabled: true, file_filter_mode: 'deny', file_filter_types: '.exe,.bat'}),
            ],
        });
        await routeStatusOk(page, body);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');

        await expect(component.getByText('in-linked')).toBeVisible();
        await expect(component.getByText('out-unlinked')).toBeVisible();
        await expect(component.getByText('orphan-conn')).toBeVisible();
        await expect(component.getByText('out-files')).toBeVisible();

        const inboundBadges = component.getByText('NATS \u2192 MATTERMOST');
        const outboundBadges = component.getByText('MATTERMOST \u2192 NATS');
        await expect(inboundBadges).toHaveCount(2);
        await expect(outboundBadges).toHaveCount(2);

        await expect(component.getByRole('button', {name: 'Unlink', exact: true}).first()).toBeVisible();
        await expect(component.getByRole('button', {name: 'Link', exact: true}).first()).toBeVisible();

        const orphanMarker = component.locator('span[title="Connection no longer in configuration"]');
        await expect(orphanMarker).toBeVisible();

        await expect(component.getByText('Allow .pdf')).toBeVisible();
        await expect(component.getByText('Deny .exe,.bat')).toBeVisible();
        await expect(component.getByText('All types')).toBeVisible();
        await expect(component.getByText('Files: Disabled')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 11. Status timer behavior
// ---------------------------------------------------------------------------
test.describe('Status timer behavior', () => {
    test('new action replaces previous status banner', async ({mount, page}) => {
        const body = statusResponse({
            team_connections: [
                connStatus({name: 'first-action', linked: false}),
                connStatus({name: 'second-action', direction: 'outbound', linked: true}),
            ],
        });
        await routeStatusOk(page, body);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch1');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        await page.route('**/plugins/crossguard/api/v1/channels/ch1/init*', (route: any) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(component.getByText('Connection "first-action" linked.')).toBeVisible();

        await page.route('**/plugins/crossguard/api/v1/channels/ch1/teardown*', (route: any) => {
            route.fulfill({status: 400, contentType: 'application/json', body: JSON.stringify({error: 'Teardown failed'})});
        });
        await component.getByRole('button', {name: 'Unlink', exact: true}).click();
        await expect(component.getByText('Teardown failed')).toBeVisible();
        await expect(component.getByText('Connection "first-action" linked.')).not.toBeVisible();
    });
});
