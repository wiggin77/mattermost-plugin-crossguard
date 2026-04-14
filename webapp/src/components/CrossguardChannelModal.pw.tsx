import type {Page, Route} from '@playwright/test';
import React from 'react';

import CrossguardChannelModal from './CrossguardChannelModal';

import {test, expect} from '../../playwright/ct-coverage';

const mockChannelStatus = {
    channel_id: 'ch-123',
    channel_name: 'test-channel',
    channel_display_name: 'Test Channel',
    team_name: 'team-alpha',
    team_connections: [
        {
            name: 'inbound-conn',
            direction: 'inbound',
            linked: false,
            file_transfer_enabled: true,
            file_filter_mode: 'allow',
            file_filter_types: '.pdf,.docx',
        },
        {
            name: 'outbound-conn',
            direction: 'outbound',
            linked: true,
            file_transfer_enabled: false,
        },
    ],
};

async function openModal(page: Page, channelID: string) {
    await page.evaluate((id: string) => {
        document.dispatchEvent(new CustomEvent('crossguard:open-modal', {detail: {channelID: id}}));
    }, channelID);
}

async function setCsrfCookie(page: Page) {
    await page.evaluate(() => {
        Object.defineProperty(document, 'cookie', {
            get: () => 'MMCSRF=test-csrf-token',
            configurable: true,
        });
    });
}

async function routeStatusOk(page: Page, body: unknown = mockChannelStatus) {
    await page.route('**/plugins/crossguard/api/v1/channels/*/status', (route: Route) => {
        route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(body)});
    });
}

async function routeStatusError(page: Page, status: number, body: unknown) {
    await page.route('**/plugins/crossguard/api/v1/channels/*/status', (route: Route) => {
        route.fulfill({status, contentType: 'application/json', body: JSON.stringify(body)});
    });
}

async function routeStatusNetworkError(page: Page) {
    await page.route('**/plugins/crossguard/api/v1/channels/*/status', (route: Route) => {
        route.abort('connectionfailed');
    });
}

// ---------------------------------------------------------------------------
// 1. Modal lifecycle
// ---------------------------------------------------------------------------
test.describe('Modal lifecycle', () => {
    test('renders nothing before the open event is dispatched', async ({mount}) => {
        const component = await mount(<CrossguardChannelModal/>);
        await expect(component).toBeEmpty();
    });

    test('opens the modal when crossguard:open-modal event is dispatched', async ({mount, page}) => {
        const component = await mount(<CrossguardChannelModal/>);
        await routeStatusOk(page);
        await openModal(page, 'ch-123');
        await expect(component.locator('h2')).toBeVisible();
    });

    test('shows Loading... while fetching channel status', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/channels/*/status', async (route) => {
            await new Promise((resolve) => setTimeout(resolve, 2000));
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(mockChannelStatus)});
        });
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByText('Loading...')).toBeVisible();
    });

    test('shows team and channel name in the header after loading', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByText('Cross Guard Settings for team-alpha > Test Channel')).toBeVisible();
    });

    test('shows "..." placeholders before status response arrives', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/channels/*/status', async (route) => {
            await new Promise((resolve) => setTimeout(resolve, 5000));
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(mockChannelStatus)});
        });
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByText('Cross Guard Settings for ... > ...')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 2. Closing
// ---------------------------------------------------------------------------
test.describe('Closing', () => {
    test('closes when the X button is clicked', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.locator('h2')).toBeVisible();
        await component.getByText('\u00D7').click();
        await expect(component).toBeEmpty();
    });

    test('closes when the Escape key is pressed', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.locator('h2')).toBeVisible();
        await page.keyboard.press('Escape');
        await expect(component).toBeEmpty();
    });

    test('closes when the backdrop is clicked', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.locator('h2')).toBeVisible();

        // Click at (5, 5) in the viewport, which lands on the fixed backdrop
        // overlay but outside the centered modal panel.
        await page.mouse.click(5, 5);
        await expect(component).toBeEmpty();
    });

    test('does NOT close when clicking inside the modal body', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.locator('h2')).toBeVisible();
        await component.locator('h2').click();
        await expect(component.locator('h2')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 3. Connection cards
// ---------------------------------------------------------------------------
test.describe('Connection cards', () => {
    test('renders connection cards for each team_connection', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByText('inbound-conn')).toBeVisible();
        await expect(component.getByText('outbound-conn')).toBeVisible();
    });

    test('shows NATS \u2192 MATTERMOST badge for inbound connections', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByText('NATS \u2192 MATTERMOST').first()).toBeVisible();
    });

    test('shows MATTERMOST \u2192 NATS badge for outbound connections', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByText('MATTERMOST \u2192 NATS').first()).toBeVisible();
    });

    test('displays connection name text in each card', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByText('inbound-conn', {exact: true})).toBeVisible();
        await expect(component.getByText('outbound-conn', {exact: true})).toBeVisible();
    });

    test('shows orphaned indicator when connection is orphaned', async ({mount, page}) => {
        const statusWithOrphan = {
            ...mockChannelStatus,
            team_connections: [
                {name: 'orphan-conn', direction: 'inbound', linked: false, orphaned: true, file_transfer_enabled: true},
            ],
        };
        await routeStatusOk(page, statusWithOrphan);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        const orphanMarker = component.locator('span[title="Connection no longer in configuration"]');
        await expect(orphanMarker).toBeVisible();
    });

    test('does not show orphaned indicator for non-orphaned connections', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        const orphanMarker = component.locator('span[title="Connection no longer in configuration"]');
        await expect(orphanMarker).toHaveCount(0);
    });

    test('displays remote_team_name when present', async ({mount, page}) => {
        const statusWithRemote = {
            ...mockChannelStatus,
            team_connections: [
                {name: 'remote-conn', direction: 'outbound', linked: true, remote_team_name: 'remote-team-bravo', file_transfer_enabled: false},
            ],
        };
        await routeStatusOk(page, statusWithRemote);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByText('Remote team: remote-team-bravo')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 4. File transfer display
// ---------------------------------------------------------------------------
test.describe('File transfer display', () => {
    test('shows allow filter with file types', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByText('Allow .pdf,.docx')).toBeVisible();
    });

    test('shows deny filter with file types', async ({mount, page}) => {
        const statusWithDeny = {
            ...mockChannelStatus,
            team_connections: [
                {name: 'deny-conn', direction: 'inbound', linked: false, file_transfer_enabled: true, file_filter_mode: 'deny', file_filter_types: '.exe,.bat'},
            ],
        };
        await routeStatusOk(page, statusWithDeny);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByText('Deny .exe,.bat')).toBeVisible();
    });

    test('shows All types when file transfer enabled but no filter mode', async ({mount, page}) => {
        const statusNoFilter = {
            ...mockChannelStatus,
            team_connections: [
                {name: 'all-conn', direction: 'outbound', linked: false, file_transfer_enabled: true},
            ],
        };
        await routeStatusOk(page, statusNoFilter);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByText('All types')).toBeVisible();
    });

    test('shows Files: Disabled when file transfer is off', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByText('Files: Disabled')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 5. Link/unlink actions
// ---------------------------------------------------------------------------
test.describe('Link/unlink actions', () => {
    test('shows Link button for unlinked connections', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();
    });

    test('shows Unlink button for linked connections', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByRole('button', {name: 'Unlink', exact: true})).toBeVisible();
    });

    test('Link posts to the init endpoint with correct URL', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        let capturedUrl = '';
        await page.route('**/plugins/crossguard/api/v1/channels/ch-123/init*', (route) => {
            capturedUrl = new URL(route.request().url()).pathname + new URL(route.request().url()).search;
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(() => {
            expect(capturedUrl).toContain('/plugins/crossguard/api/v1/channels/ch-123/init');
            expect(capturedUrl).toContain('connection_name=inbound%3Ainbound-conn');
        }).toPass();
    });

    test('Unlink posts to the teardown endpoint with correct URL', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByRole('button', {name: 'Unlink', exact: true})).toBeVisible();

        let capturedUrl = '';
        await page.route('**/plugins/crossguard/api/v1/channels/ch-123/teardown*', (route) => {
            capturedUrl = new URL(route.request().url()).pathname + new URL(route.request().url()).search;
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Unlink', exact: true}).click();
        await expect(() => {
            expect(capturedUrl).toContain('/plugins/crossguard/api/v1/channels/ch-123/teardown');
            expect(capturedUrl).toContain('connection_name=outbound%3Aoutbound-conn');
        }).toPass();
    });

    test('sends X-CSRF-Token header on link action', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        let csrfHeader = '';
        await page.route('**/plugins/crossguard/api/v1/channels/ch-123/init*', (route) => {
            csrfHeader = route.request().headers()['x-csrf-token'] || '';
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(() => {
            expect(csrfHeader).toBe('test-csrf-token');
        }).toPass();
    });

    test('shows Linking... text while action is in progress', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        await page.route('**/plugins/crossguard/api/v1/channels/ch-123/init*', async (route) => {
            await new Promise((resolve) => setTimeout(resolve, 2000));
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(component.getByText('Linking...')).toBeVisible();
    });

    test('shows Unlinking... text while unlink action is in progress', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByRole('button', {name: 'Unlink', exact: true})).toBeVisible();

        await page.route('**/plugins/crossguard/api/v1/channels/ch-123/teardown*', async (route) => {
            await new Promise((resolve) => setTimeout(resolve, 2000));
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Unlink', exact: true}).click();
        await expect(component.getByText('Unlinking...')).toBeVisible();
    });

    test('disables all buttons while an action is in progress', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        await page.route('**/plugins/crossguard/api/v1/channels/ch-123/init*', async (route) => {
            await new Promise((resolve) => setTimeout(resolve, 2000));
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(component.getByText('Linking...')).toBeVisible();
        await expect(component.getByRole('button', {name: 'Unlink', exact: true})).toBeDisabled();
    });

    test('re-fetches channel status after a successful link action', async ({mount, page}) => {
        let fetchCount = 0;
        await page.route('**/plugins/crossguard/api/v1/channels/*/status', (route) => {
            fetchCount++;
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(mockChannelStatus)});
        });
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();
        const initialFetches = fetchCount;

        await page.route('**/plugins/crossguard/api/v1/channels/ch-123/init*', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Link', exact: true}).click();

        await expect(() => {
            expect(fetchCount).toBeGreaterThan(initialFetches);
        }).toPass();
    });

    test('shows success banner after a successful link', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        await page.route('**/plugins/crossguard/api/v1/channels/ch-123/init*', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(component.getByText('Connection "inbound-conn" linked.')).toBeVisible();
    });

    test('shows error banner when link action returns an error', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        await page.route('**/plugins/crossguard/api/v1/channels/ch-123/init*', (route) => {
            route.fulfill({status: 400, contentType: 'application/json', body: JSON.stringify({error: 'Channel already linked.'})});
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(component.getByText('Channel already linked.')).toBeVisible();
    });

    test('shows network error banner when link action has a network failure', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        await page.route('**/plugins/crossguard/api/v1/channels/ch-123/init*', (route) => {
            route.abort('connectionfailed');
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(component.getByText('Network error.')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 6. Status banner
// ---------------------------------------------------------------------------
test.describe('Status banner', () => {
    test('no status banner is shown initially', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByText('inbound-conn')).toBeVisible();
        const successBanners = component.getByText(/linked\./);
        await expect(successBanners).toHaveCount(0);
    });

    test('success banner has green-tinted background and error banner has red-tinted background', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        // Trigger a successful link to get the success banner
        await page.route('**/plugins/crossguard/api/v1/channels/ch-123/init*', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Link', exact: true}).click();

        const banner = component.getByText('Connection "inbound-conn" linked.');
        await expect(banner).toBeVisible();

        // The banner div wraps the text. It has inline background-color from statusSuccess.
        // Verify the background contains the green-tinted rgba value.
        const bgColor = await banner.evaluate((el) => {
            const parent = el.closest('div[style]');
            return parent ? getComputedStyle(parent).backgroundColor : '';
        });
        expect(bgColor).toContain('61, 184, 135');
    });

    test('auto-hides after 5 seconds', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        await page.route('**/plugins/crossguard/api/v1/channels/ch-123/init*', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await setCsrfCookie(page);

        await page.clock.install();
        await component.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(component.getByText('Connection "inbound-conn" linked.')).toBeVisible();

        await page.clock.fastForward(5100);
        await expect(component.getByText('Connection "inbound-conn" linked.')).toHaveCount(0);
    });
});

// ---------------------------------------------------------------------------
// 7. Empty state
// ---------------------------------------------------------------------------
test.describe('Empty state', () => {
    test('shows empty message when there are no connections', async ({mount, page}) => {
        const emptyStatus = {
            ...mockChannelStatus,
            team_connections: [],
        };
        await routeStatusOk(page, emptyStatus);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByText('No connections available. Configure connections in the System Console.')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 8. Help link
// ---------------------------------------------------------------------------
test.describe('Help link', () => {
    test('has the correct href pointing to help page', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        const link = component.getByRole('link', {name: 'View Cross Guard Documentation'});
        await expect(link).toHaveAttribute('href', '/plugins/crossguard/public/help/help.html');
    });

    test('opens in a new tab with security attributes', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        const link = component.getByRole('link', {name: 'View Cross Guard Documentation'});
        await expect(link).toHaveAttribute('target', '_blank');
        await expect(link).toHaveAttribute('rel', 'noopener noreferrer');
    });
});

// ---------------------------------------------------------------------------
// 9. API error handling
// ---------------------------------------------------------------------------
test.describe('API error handling', () => {
    test('shows server error message from response data.error', async ({mount, page}) => {
        await routeStatusError(page, 403, {error: 'You do not have permission.'});
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByText('You do not have permission.')).toBeVisible();
    });

    test('shows fallback error message when response has no error field', async ({mount, page}) => {
        await routeStatusError(page, 500, {});
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByText('Failed to load channel status.')).toBeVisible();
    });

    test('shows network error message on fetch failure', async ({mount, page}) => {
        await routeStatusNetworkError(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByText('Network error loading channel status.')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 10. Modal state reset
// ---------------------------------------------------------------------------
test.describe('Modal state reset', () => {
    test('resets state when re-opened for a different channel', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/channels/ch-first/status', (route) => {
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    channel_id: 'ch-first',
                    channel_name: 'first-channel',
                    channel_display_name: 'First Channel',
                    team_name: 'Team A',
                    team_connections: [{name: 'conn-a', direction: 'inbound', linked: true, file_transfer_enabled: false}],
                }),
            });
        });
        await page.route('**/plugins/crossguard/api/v1/channels/ch-second/status', (route) => {
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    channel_id: 'ch-second',
                    channel_name: 'second-channel',
                    channel_display_name: 'Second Channel',
                    team_name: 'Team B',
                    team_connections: [{name: 'conn-b', direction: 'outbound', linked: false, file_transfer_enabled: false}],
                }),
            });
        });

        const component = await mount(<CrossguardChannelModal/>);

        // Open for first channel
        await page.evaluate(() => {
            document.dispatchEvent(new CustomEvent('crossguard:open-modal', {detail: {channelID: 'ch-first'}}));
        });
        await expect(component.getByText('First Channel')).toBeVisible();
        await expect(component.getByText('conn-a')).toBeVisible();

        // Close
        await page.keyboard.press('Escape');
        await expect(component).toBeEmpty();

        // Open for second channel
        await page.evaluate(() => {
            document.dispatchEvent(new CustomEvent('crossguard:open-modal', {detail: {channelID: 'ch-second'}}));
        });
        await expect(component.getByText('Second Channel')).toBeVisible();
        await expect(component.getByText('conn-b')).toBeVisible();
        await expect(component.getByText('conn-a')).not.toBeVisible();
    });

    test('ignores event with no channelID in detail', async ({mount, page}) => {
        const component = await mount(<CrossguardChannelModal/>);
        await page.evaluate(() => {
            document.dispatchEvent(new CustomEvent('crossguard:open-modal', {detail: {}}));
        });
        await expect(component).toBeEmpty();
    });
});

// ---------------------------------------------------------------------------
// 11. Link/unlink actions - extended
// ---------------------------------------------------------------------------
test.describe('Link/unlink actions - extended', () => {
    test('uses POST method for link action', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/channels/*/status', (route) => {
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    channel_id: 'ch-method',
                    channel_name: 'method-ch',
                    channel_display_name: 'Method Channel',
                    team_name: 'Team',
                    team_connections: [{name: 'conn-method', direction: 'inbound', linked: false, file_transfer_enabled: false}],
                }),
            });
        });

        let capturedMethod = '';
        await page.route('**/plugins/crossguard/api/v1/channels/*/init*', (route) => {
            capturedMethod = route.request().method();
            route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
        });

        const component = await mount(<CrossguardChannelModal/>);
        await page.evaluate(() => {
            document.dispatchEvent(new CustomEvent('crossguard:open-modal', {detail: {channelID: 'ch-method'}}));
        });
        await component.getByRole('button', {name: 'Link'}).click();
        expect(capturedMethod).toBe('POST');
    });

    test('includes Content-Type application/json header', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/channels/*/status', (route) => {
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    channel_id: 'ch-ct',
                    channel_name: 'ct-ch',
                    channel_display_name: 'CT Channel',
                    team_name: 'Team',
                    team_connections: [{name: 'conn-ct', direction: 'inbound', linked: false, file_transfer_enabled: false}],
                }),
            });
        });

        let capturedContentType = '';
        await page.route('**/plugins/crossguard/api/v1/channels/*/init*', (route) => {
            capturedContentType = route.request().headers()['content-type'] || '';
            route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
        });

        const component = await mount(<CrossguardChannelModal/>);
        await page.evaluate(() => {
            document.dispatchEvent(new CustomEvent('crossguard:open-modal', {detail: {channelID: 'ch-ct'}}));
        });
        await component.getByRole('button', {name: 'Link'}).click();
        expect(capturedContentType).toBe('application/json');
    });

    test('shows fallback error when API returns no error field', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/channels/*/status', (route) => {
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    channel_id: 'ch-fallback',
                    channel_name: 'fallback-ch',
                    channel_display_name: 'Fallback',
                    team_name: 'Team',
                    team_connections: [{name: 'conn-fb', direction: 'inbound', linked: false, file_transfer_enabled: false}],
                }),
            });
        });
        await page.route('**/plugins/crossguard/api/v1/channels/*/init*', (route) => {
            route.fulfill({status: 400, contentType: 'application/json', body: '{}'});
        });

        const component = await mount(<CrossguardChannelModal/>);
        await page.evaluate(() => {
            document.dispatchEvent(new CustomEvent('crossguard:open-modal', {detail: {channelID: 'ch-fallback'}}));
        });
        await component.getByRole('button', {name: 'Link'}).click();
        await expect(component.getByText('Failed to link connection.')).toBeVisible();
    });

    test('shows success banner for unlink with correct verb', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/channels/*/status', (route) => {
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    channel_id: 'ch-verb',
                    channel_name: 'verb-ch',
                    channel_display_name: 'Verb Channel',
                    team_name: 'Team',
                    team_connections: [{name: 'verb-conn', direction: 'outbound', linked: true, file_transfer_enabled: false}],
                }),
            });
        });
        await page.route('**/plugins/crossguard/api/v1/channels/*/teardown*', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
        });

        const component = await mount(<CrossguardChannelModal/>);
        await page.evaluate(() => {
            document.dispatchEvent(new CustomEvent('crossguard:open-modal', {detail: {channelID: 'ch-verb'}}));
        });
        await component.getByRole('button', {name: 'Unlink'}).click();
        await expect(component.getByText('Connection "verb-conn" unlinked.')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 12. API error handling - extended
// ---------------------------------------------------------------------------
test.describe('API error handling - extended', () => {
    test('handles non-JSON error response from status endpoint', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/channels/*/status', (route) => {
            route.fulfill({status: 500, contentType: 'text/plain', body: 'Internal Server Error'});
        });

        const component = await mount(<CrossguardChannelModal/>);
        await page.evaluate(() => {
            document.dispatchEvent(new CustomEvent('crossguard:open-modal', {detail: {channelID: 'ch-nonjson'}}));
        });
        await expect(component.getByText('Network error loading channel status.')).toBeVisible();
    });

    test('handles team_connections being null in response', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/channels/*/status', (route) => {
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    channel_id: 'ch-null',
                    channel_name: 'null-ch',
                    channel_display_name: 'Null Channel',
                    team_name: 'Team',
                    team_connections: null,
                }),
            });
        });

        const component = await mount(<CrossguardChannelModal/>);
        await page.evaluate(() => {
            document.dispatchEvent(new CustomEvent('crossguard:open-modal', {detail: {channelID: 'ch-null'}}));
        });
        await expect(component.getByText('No connections available.')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 13. fetchStatus error paths (detailed)
// ---------------------------------------------------------------------------
test.describe('fetchStatus error paths', () => {
    test('500 with data.error shows the server-provided error message', async ({mount, page}) => {
        await routeStatusError(page, 500, {error: 'Internal server error'});
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByText('Internal server error')).toBeVisible();
    });

    test('403 with empty body shows fallback error message', async ({mount, page}) => {
        await routeStatusError(page, 403, {});
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByText('Failed to load channel status.')).toBeVisible();
    });

    test('network failure shows network error message', async ({mount, page}) => {
        await routeStatusNetworkError(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByText('Network error loading channel status.')).toBeVisible();
    });

    test('200 with team_connections undefined does not crash and shows empty state', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/channels/*/status', (route) => {
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    channel_id: 'ch-undef',
                    channel_name: 'undef-ch',
                    channel_display_name: 'Undef Channel',
                    team_name: 'Team Undef',
                }),
            });
        });
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-undef');
        await expect(component.getByText('No connections available. Configure connections in the System Console.')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 14. Empty and edge states (detailed)
// ---------------------------------------------------------------------------
test.describe('Empty and edge states', () => {
    test('empty team_connections array shows the empty-state message', async ({mount, page}) => {
        const emptyStatus = {
            channel_id: 'ch-empty',
            channel_name: 'empty-ch',
            channel_display_name: 'Empty Channel',
            team_name: 'Team Empty',
            team_connections: [],
        };
        await routeStatusOk(page, emptyStatus);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-empty');
        await expect(component.getByText('No connections available. Configure connections in the System Console.')).toBeVisible();
    });

    test('help link is always present after modal opens', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        const link = component.getByRole('link', {name: 'View Cross Guard Documentation'});
        await expect(link).toBeVisible();
        await expect(link).toHaveAttribute('href', '/plugins/crossguard/public/help/help.html');
    });

    test('help link has target _blank and rel noopener noreferrer', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        const link = component.getByRole('link', {name: 'View Cross Guard Documentation'});
        await expect(link).toHaveAttribute('target', '_blank');
        await expect(link).toHaveAttribute('rel', 'noopener noreferrer');
    });

    test('renders all connection cards when there are 5+ connections', async ({mount, page}) => {
        const manyConnections = {
            channel_id: 'ch-many',
            channel_name: 'many-ch',
            channel_display_name: 'Many Channel',
            team_name: 'Team Many',
            team_connections: [
                {name: 'conn-alpha', direction: 'inbound', linked: false, file_transfer_enabled: true},
                {name: 'conn-bravo', direction: 'outbound', linked: true, file_transfer_enabled: false},
                {name: 'conn-charlie', direction: 'inbound', linked: false, file_transfer_enabled: true, file_filter_mode: 'deny', file_filter_types: '.exe'},
                {name: 'conn-delta', direction: 'outbound', linked: false, file_transfer_enabled: true, file_filter_mode: 'allow', file_filter_types: '.pdf'},
                {name: 'conn-echo', direction: 'inbound', linked: true, file_transfer_enabled: false},
            ],
        };
        await routeStatusOk(page, manyConnections);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-many');
        await expect(component.getByText('conn-alpha')).toBeVisible();
        await expect(component.getByText('conn-bravo')).toBeVisible();
        await expect(component.getByText('conn-charlie')).toBeVisible();
        await expect(component.getByText('conn-delta')).toBeVisible();
        await expect(component.getByText('conn-echo')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 15. Multi-open state reset (detailed)
// ---------------------------------------------------------------------------
test.describe('Multi-open state reset', () => {
    test('header updates with new channel name when opened for a different channel', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/channels/ch-one/status', (route) => {
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    channel_id: 'ch-one',
                    channel_name: 'one-ch',
                    channel_display_name: 'Channel One',
                    team_name: 'Team X',
                    team_connections: [{name: 'conn-x', direction: 'inbound', linked: false, file_transfer_enabled: false}],
                }),
            });
        });
        await page.route('**/plugins/crossguard/api/v1/channels/ch-two/status', (route) => {
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    channel_id: 'ch-two',
                    channel_name: 'two-ch',
                    channel_display_name: 'Channel Two',
                    team_name: 'Team Y',
                    team_connections: [{name: 'conn-y', direction: 'outbound', linked: true, file_transfer_enabled: false}],
                }),
            });
        });

        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-one');
        await expect(component.getByText('Cross Guard Settings for Team X > Channel One')).toBeVisible();

        // Directly open for ch-two without closing (simulates rapid switch)
        await openModal(page, 'ch-two');
        await expect(component.getByText('Cross Guard Settings for Team Y > Channel Two')).toBeVisible();
    });

    test('previous connections from first channel are not visible after opening second channel', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/channels/ch-prev/status', (route) => {
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    channel_id: 'ch-prev',
                    channel_name: 'prev-ch',
                    channel_display_name: 'Prev Channel',
                    team_name: 'Team Prev',
                    team_connections: [{name: 'conn-prev', direction: 'inbound', linked: false, file_transfer_enabled: false}],
                }),
            });
        });
        await page.route('**/plugins/crossguard/api/v1/channels/ch-next/status', (route) => {
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    channel_id: 'ch-next',
                    channel_name: 'next-ch',
                    channel_display_name: 'Next Channel',
                    team_name: 'Team Next',
                    team_connections: [{name: 'conn-next', direction: 'outbound', linked: true, file_transfer_enabled: false}],
                }),
            });
        });

        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-prev');
        await expect(component.getByText('conn-prev')).toBeVisible();

        await openModal(page, 'ch-next');
        await expect(component.getByText('conn-next')).toBeVisible();
        await expect(component.getByText('conn-prev')).not.toBeVisible();
    });

    test('status banner is cleared when modal is closed and reopened', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        // Trigger a link action to get a status banner
        await page.route('**/plugins/crossguard/api/v1/channels/ch-123/init*', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(component.getByText('Connection "inbound-conn" linked.')).toBeVisible();

        // Close and reopen
        await page.keyboard.press('Escape');
        await expect(component).toBeEmpty();
        await openModal(page, 'ch-123');
        await expect(component.getByText('inbound-conn')).toBeVisible();
        const staleSuccessBanner = component.getByText('Connection "inbound-conn" linked.');
        await expect(staleSuccessBanner).toHaveCount(0);
    });

    test('actionInProgress text is not visible after close and reopen', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        // Start a slow link action so "Linking..." appears
        await page.route('**/plugins/crossguard/api/v1/channels/ch-123/init*', async (route) => {
            await new Promise((resolve) => setTimeout(resolve, 5000));
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(component.getByText('Linking...')).toBeVisible();

        // Close via Escape and reopen
        await page.keyboard.press('Escape');
        await expect(component).toBeEmpty();
        await openModal(page, 'ch-123');
        await expect(component.getByText('inbound-conn')).toBeVisible();
        const staleLinkingText = component.getByText('Linking...');
        await expect(staleLinkingText).toHaveCount(0);
    });
});

// ---------------------------------------------------------------------------
// 16. Status banner behavior (detailed)
// ---------------------------------------------------------------------------
test.describe('Status banner behavior', () => {
    test('successful link action shows banner with connection name and "linked" verb', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/channels/*/status', (route) => {
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    channel_id: 'ch-banner',
                    channel_name: 'banner-ch',
                    channel_display_name: 'Banner Channel',
                    team_name: 'Team Banner',
                    team_connections: [{name: 'banner-conn', direction: 'inbound', linked: false, file_transfer_enabled: false}],
                }),
            });
        });
        await page.route('**/plugins/crossguard/api/v1/channels/*/init*', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });

        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-banner');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(component.getByText('Connection "banner-conn" linked.')).toBeVisible();
    });

    test('failed action shows server error from data.error field', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/channels/*/status', (route) => {
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    channel_id: 'ch-fail',
                    channel_name: 'fail-ch',
                    channel_display_name: 'Fail Channel',
                    team_name: 'Team Fail',
                    team_connections: [{name: 'fail-conn', direction: 'inbound', linked: false, file_transfer_enabled: false}],
                }),
            });
        });
        await page.route('**/plugins/crossguard/api/v1/channels/*/init*', (route) => {
            route.fulfill({status: 409, contentType: 'application/json', body: JSON.stringify({error: 'Conflict: channel is locked.'})});
        });

        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-fail');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(component.getByText('Conflict: channel is locked.')).toBeVisible();
    });

    test('starting a new action clears the previous status banner', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardChannelModal/>);
        await openModal(page, 'ch-123');
        await expect(component.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        // First action: succeed to get a banner
        await page.route('**/plugins/crossguard/api/v1/channels/ch-123/init*', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await setCsrfCookie(page);
        await component.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(component.getByText('Connection "inbound-conn" linked.')).toBeVisible();

        // Second action: click Unlink on the outbound connection
        // The handleToggle sets status to {loading: true} which hides the banner
        await page.route('**/plugins/crossguard/api/v1/channels/ch-123/teardown*', async (route) => {
            await new Promise((resolve) => setTimeout(resolve, 1500));
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await component.getByRole('button', {name: 'Unlink', exact: true}).click();

        // The old "linked" banner should be gone because status was set to {loading: true}
        const oldBanner = component.getByText('Connection "inbound-conn" linked.');
        await expect(oldBanner).toHaveCount(0);
    });
});
