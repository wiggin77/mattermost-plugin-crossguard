
import type {Page} from '@playwright/test';
import React from 'react';

import CrossguardTeamModal from './CrossguardTeamModal';

import {test, expect} from '../../playwright/ct-coverage';

const mockTeamStatus = {
    team_id: 'team-456',
    team_name: 'alpha-team',
    team_display_name: 'Alpha Team',
    initialized: true,
    connections: [
        {name: 'inbound-relay', direction: 'inbound', linked: true, remote_team_name: 'beta-team', file_transfer_enabled: true, file_filter_mode: ''},
        {name: 'outbound-relay', direction: 'outbound', linked: false, remote_team_name: '', file_transfer_enabled: false, file_filter_mode: ''},
    ],
};

async function openTeamModal(page: Page, teamID: string) {
    await page.evaluate((id: string) => {
        document.dispatchEvent(new CustomEvent('crossguard:open-team-modal', {detail: {teamID: id}}));
    }, teamID);
}

async function setCsrfCookie(page: Page) {
    await page.evaluate(() => {
        Object.defineProperty(document, 'cookie', {
            get: () => 'MMCSRF=test-csrf-token',
            configurable: true,
        });
    });
}

async function routeStatusOk(page: Page, response = mockTeamStatus) {
    await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
        route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify(response),
        });
    });
}

async function mountAndOpen(page: Page, mount: any, teamID = 'team-456', response = mockTeamStatus) {
    await routeStatusOk(page, response);
    const component = await mount(<CrossguardTeamModal/>);
    await openTeamModal(page, teamID);
    await page.getByText('Alpha Team').waitFor({state: 'visible', timeout: 5000}).catch(() => {});
    return component;
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. Modal lifecycle
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Modal lifecycle', () => {
    test('renders nothing before the custom event is dispatched', async ({mount}) => {
        const component = await mount(<CrossguardTeamModal/>);
        await expect(component).toBeEmpty();
    });

    test('opens when crossguard:open-team-modal event fires', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardTeamModal/>);
        await expect(component).toBeEmpty();
        await openTeamModal(page, 'team-456');
        await expect(page.getByText('Cross Guard Settings for')).toBeVisible();
    });

    test('shows loading state while fetching', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', async (route) => {
            await new Promise((r) => setTimeout(r, 2000));
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify(mockTeamStatus),
            });
        });
        await mount(<CrossguardTeamModal/>);
        await openTeamModal(page, 'team-456');
        await expect(page.getByText('Loading...')).toBeVisible();
    });

    test('displays team name in the header after fetch completes', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await expect(page.getByText('Cross Guard Settings for Alpha Team')).toBeVisible();
    });

    test('shows "..." placeholder before team name is loaded', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', async (route) => {
            await new Promise((r) => setTimeout(r, 3000));
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify(mockTeamStatus),
            });
        });
        await mount(<CrossguardTeamModal/>);
        await openTeamModal(page, 'team-456');
        await expect(page.getByText('Cross Guard Settings for ...')).toBeVisible();
    });

    test('clears previous state when reopened with a new team', async ({mount, page}) => {
        let callCount = 0;
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
            callCount++;
            const body = callCount === 1 ? mockTeamStatus : {
                ...mockTeamStatus,
                team_display_name: 'Bravo Team',
                connections: [],
            };
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify(body),
            });
        });
        await mount(<CrossguardTeamModal/>);
        await openTeamModal(page, 'team-456');
        await expect(page.getByText('Alpha Team')).toBeVisible();

        // Close via close button
        await page.locator('button').filter({hasText: '\u00D7'}).click();
        await expect(page.getByText('Alpha Team')).not.toBeVisible();

        // Open with new team
        await openTeamModal(page, 'team-789');
        await expect(page.getByText('Bravo Team')).toBeVisible();
        await expect(page.getByText('No connections available')).toBeVisible();
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 2. Closing
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Closing', () => {
    test('closes when close button is clicked', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await page.locator('button').filter({hasText: '\u00D7'}).click();
        await expect(page.getByText('Cross Guard Settings for')).not.toBeVisible();
    });

    test('closes when Escape key is pressed', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await page.keyboard.press('Escape');
        await expect(page.getByText('Cross Guard Settings for')).not.toBeVisible();
    });

    test('closes when backdrop is clicked', async ({mount, page}) => {
        await mountAndOpen(page, mount);

        // Click position 0,0 which is on the backdrop, not the modal
        await page.mouse.click(1, 1);
        await expect(page.getByText('Cross Guard Settings for')).not.toBeVisible();
    });

    test('stays open when clicking inside the modal', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await page.getByText('Cross Guard Settings for Alpha Team').click();
        await expect(page.getByText('Cross Guard Settings for Alpha Team')).toBeVisible();
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 3. Connection cards
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Connection cards', () => {
    test('renders a card for each connection', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await expect(page.getByText('inbound-relay')).toBeVisible();
        await expect(page.getByText('outbound-relay')).toBeVisible();
    });

    test('shows inbound badge with correct arrow text', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await expect(page.getByText('NATS \u2192 MATTERMOST').first()).toBeVisible();
    });

    test('shows outbound badge with correct arrow text', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await expect(page.getByText('MATTERMOST \u2192 NATS')).toBeVisible();
    });

    test('displays orphaned indicator when connection is orphaned', async ({mount, page}) => {
        const orphanedStatus = {
            ...mockTeamStatus,
            connections: [
                {name: 'old-conn', direction: 'inbound', linked: true, orphaned: true, remote_team_name: '', file_transfer_enabled: false, file_filter_mode: ''},
            ],
        };
        await mountAndOpen(page, mount, 'team-456', orphanedStatus);
        await expect(page.getByTitle('Connection no longer in configuration')).toBeVisible();
    });

    test('shows file transfer info based on connection settings', async ({mount, page}) => {
        const fileStatus = {
            ...mockTeamStatus,
            connections: [
                {name: 'allow-conn', direction: 'inbound', linked: true, remote_team_name: '', file_transfer_enabled: true, file_filter_mode: 'allow', file_filter_types: '.pdf,.docx'},
                {name: 'deny-conn', direction: 'outbound', linked: false, remote_team_name: '', file_transfer_enabled: true, file_filter_mode: 'deny', file_filter_types: '.exe'},
                {name: 'all-conn', direction: 'inbound', linked: false, remote_team_name: '', file_transfer_enabled: true, file_filter_mode: ''},
                {name: 'disabled-conn', direction: 'outbound', linked: false, remote_team_name: '', file_transfer_enabled: false, file_filter_mode: ''},
            ],
        };
        await mountAndOpen(page, mount, 'team-456', fileStatus);
        await expect(page.getByText('Allow .pdf,.docx')).toBeVisible();
        await expect(page.getByText('Deny .exe')).toBeVisible();
        await expect(page.getByText('All types')).toBeVisible();
        await expect(page.getByText('Files: Disabled')).toBeVisible();
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 4. Link/unlink actions
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Link/unlink actions', () => {
    test('shows Unlink button for linked connections and Link for unlinked', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await expect(page.getByRole('button', {name: 'Unlink'})).toBeVisible();
        await expect(page.getByRole('button', {name: 'Link', exact: true})).toBeVisible();
    });

    test('sends POST to teardown URL when unlinking', async ({mount, page}) => {
        await setCsrfCookie(page);
        let capturedUrl = '';
        await page.route('**/plugins/crossguard/api/v1/teams/*/teardown*', (route) => {
            capturedUrl = route.request().url();
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Unlink'}).click();
        expect(capturedUrl).toContain('/teardown?connection_name=inbound%3Ainbound-relay');
    });

    test('sends POST to init URL when linking', async ({mount, page}) => {
        await setCsrfCookie(page);
        let capturedUrl = '';
        await page.route('**/plugins/crossguard/api/v1/teams/*/init*', (route) => {
            capturedUrl = route.request().url();
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Link', exact: true}).click();
        expect(capturedUrl).toContain('/init?connection_name=outbound%3Aoutbound-relay');
    });

    test('shows loading text during link action', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/init*', async (route) => {
            await new Promise((r) => setTimeout(r, 1500));
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(page.getByText('Linking...')).toBeVisible();
    });

    test('shows loading text during unlink action', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/teardown*', async (route) => {
            await new Promise((r) => setTimeout(r, 1500));
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Unlink'}).click();
        await expect(page.getByText('Unlinking...')).toBeVisible();
    });

    test('disables all action buttons while an action is in progress', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/teardown*', async (route) => {
            await new Promise((r) => setTimeout(r, 1500));
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Unlink'}).click();
        await expect(page.getByRole('button', {name: 'Unlinking...'})).toBeDisabled();
        await expect(page.getByRole('button', {name: 'Link', exact: true})).toBeDisabled();
    });

    test('shows success banner after successful link', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/init*', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(page.getByText('Connection "outbound-relay" linked.')).toBeVisible();
    });

    test('shows error banner when API returns error', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/init*', (route) => {
            route.fulfill({status: 400, contentType: 'application/json', body: JSON.stringify({error: 'Connection limit exceeded'})});
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(page.getByText('Connection limit exceeded')).toBeVisible();
    });

    test('shows network error banner on fetch failure', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/teardown*', (route) => {
            route.abort('connectionrefused');
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Unlink'}).click();
        await expect(page.getByText('Network error.')).toBeVisible();
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 5. Rewrite display
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Rewrite display', () => {
    test('inbound connection with remote_team_name shows Edit and Clear buttons', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await expect(page.getByText('Remote team:')).toBeVisible();
        await expect(page.getByText('beta-team')).toBeVisible();
        await expect(page.getByRole('button', {name: 'Edit'})).toBeVisible();
        await expect(page.getByRole('button', {name: 'Clear'})).toBeVisible();
    });

    test('inbound connection without remote_team_name shows Set button', async ({mount, page}) => {
        const noRewriteStatus = {
            ...mockTeamStatus,
            connections: [
                {name: 'inbound-relay', direction: 'inbound', linked: true, remote_team_name: '', file_transfer_enabled: true, file_filter_mode: ''},
            ],
        };
        await mountAndOpen(page, mount, 'team-456', noRewriteStatus);
        await expect(page.getByText('No remote team rewrite')).toBeVisible();
        await expect(page.getByRole('button', {name: 'Set'})).toBeVisible();
    });

    test('outbound connection does not show rewrite controls', async ({mount, page}) => {
        const outboundOnly = {
            ...mockTeamStatus,
            connections: [
                {name: 'outbound-relay', direction: 'outbound', linked: false, remote_team_name: '', file_transfer_enabled: false, file_filter_mode: ''},
            ],
        };
        await mountAndOpen(page, mount, 'team-456', outboundOnly);
        await expect(page.getByText('No remote team rewrite')).not.toBeVisible();
        await expect(page.getByText('Remote team:')).not.toBeVisible();
        await expect(page.getByRole('button', {name: 'Set'})).not.toBeVisible();
        await expect(page.getByRole('button', {name: 'Edit'})).not.toBeVisible();
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 6. Rewrite edit flow
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Rewrite edit flow', () => {
    test('Set button opens empty input field', async ({mount, page}) => {
        const noRewriteStatus = {
            ...mockTeamStatus,
            connections: [
                {name: 'inbound-relay', direction: 'inbound', linked: true, remote_team_name: '', file_transfer_enabled: true, file_filter_mode: ''},
            ],
        };
        await mountAndOpen(page, mount, 'team-456', noRewriteStatus);
        await page.getByRole('button', {name: 'Set'}).click();
        const input = page.getByPlaceholder('Remote team name');
        await expect(input).toBeVisible();
        await expect(input).toHaveValue('');
    });

    test('Edit button pre-fills the input with current remote_team_name', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        const input = page.getByPlaceholder('Remote team name');
        await expect(input).toHaveValue('beta-team');
    });

    test('input field receives focus automatically', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        const input = page.getByPlaceholder('Remote team name');
        await expect(input).toBeFocused();
    });

    test('Save button is disabled when input is empty', async ({mount, page}) => {
        const noRewriteStatus = {
            ...mockTeamStatus,
            connections: [
                {name: 'inbound-relay', direction: 'inbound', linked: true, remote_team_name: '', file_transfer_enabled: true, file_filter_mode: ''},
            ],
        };
        await mountAndOpen(page, mount, 'team-456', noRewriteStatus);
        await page.getByRole('button', {name: 'Set'}).click();
        await expect(page.getByRole('button', {name: 'Save'})).toBeDisabled();
    });

    test('Save button is enabled when input has text', async ({mount, page}) => {
        const noRewriteStatus = {
            ...mockTeamStatus,
            connections: [
                {name: 'inbound-relay', direction: 'inbound', linked: true, remote_team_name: '', file_transfer_enabled: true, file_filter_mode: ''},
            ],
        };
        await mountAndOpen(page, mount, 'team-456', noRewriteStatus);
        await page.getByRole('button', {name: 'Set'}).click();
        await page.getByPlaceholder('Remote team name').fill('gamma-team');
        await expect(page.getByRole('button', {name: 'Save'})).toBeEnabled();
    });

    test('Save sends POST with correct body', async ({mount, page}) => {
        await setCsrfCookie(page);
        let capturedBody = '';
        let capturedHeaders: Record<string, string> = {};
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route) => {
            if (route.request().method() === 'POST') {
                capturedBody = route.request().postData() || '';
                capturedHeaders = route.request().headers();
                route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
            } else {
                route.continue();
            }
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        await page.getByPlaceholder('Remote team name').fill('gamma-team');
        await page.getByRole('button', {name: 'Save'}).click();
        expect(JSON.parse(capturedBody)).toEqual({connection: 'inbound-relay', remote_team_name: 'gamma-team'});
        expect(capturedHeaders['x-csrf-token']).toBe('test-csrf-token');
    });

    test('Enter key triggers save when input is non-empty', async ({mount, page}) => {
        await setCsrfCookie(page);
        let saved = false;
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route) => {
            if (route.request().method() === 'POST') {
                saved = true;
                route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
            } else {
                route.continue();
            }
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        await page.getByPlaceholder('Remote team name').fill('delta-team');
        await page.getByPlaceholder('Remote team name').press('Enter');
        await expect(page.getByText('Remote team rewrite updated for "inbound-relay".')).toBeVisible();
        expect(saved).toBe(true);
    });

    test('Enter key does not save when input is empty', async ({mount, page}) => {
        await setCsrfCookie(page);
        let saved = false;
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route) => {
            if (route.request().method() === 'POST') {
                saved = true;
                route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
            } else {
                route.continue();
            }
        });
        const noRewriteStatus = {
            ...mockTeamStatus,
            connections: [
                {name: 'inbound-relay', direction: 'inbound', linked: true, remote_team_name: '', file_transfer_enabled: true, file_filter_mode: ''},
            ],
        };
        await mountAndOpen(page, mount, 'team-456', noRewriteStatus);
        await page.getByRole('button', {name: 'Set'}).click();
        await page.getByPlaceholder('Remote team name').press('Enter');
        expect(saved).toBe(false);

        // Input should still be visible (editing not cancelled)
        await expect(page.getByPlaceholder('Remote team name')).toBeVisible();
    });

    test('Escape key cancels editing but does not close modal', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        await expect(page.getByPlaceholder('Remote team name')).toBeVisible();
        await page.getByPlaceholder('Remote team name').press('Escape');

        // Input should be gone
        await expect(page.getByPlaceholder('Remote team name')).not.toBeVisible();

        // Modal should still be open (stopPropagation prevents the modal Escape handler)
        await expect(page.getByText('Cross Guard Settings for Alpha Team')).toBeVisible();
    });

    test('Cancel button exits editing mode', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        await expect(page.getByPlaceholder('Remote team name')).toBeVisible();
        await page.getByRole('button', {name: 'Cancel'}).click();
        await expect(page.getByPlaceholder('Remote team name')).not.toBeVisible();

        // Rewrite display returns
        await expect(page.getByText('Remote team:')).toBeVisible();
    });

    test('shows success banner after saving rewrite', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route) => {
            if (route.request().method() === 'POST') {
                route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
            } else {
                route.continue();
            }
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        await page.getByPlaceholder('Remote team name').fill('gamma-team');
        await page.getByRole('button', {name: 'Save'}).click();
        await expect(page.getByText('Remote team rewrite updated for "inbound-relay".')).toBeVisible();
    });

    test('shows error banner when rewrite save fails', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route) => {
            if (route.request().method() === 'POST') {
                route.fulfill({status: 400, contentType: 'application/json', body: JSON.stringify({error: 'Invalid team name'})});
            } else {
                route.continue();
            }
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        await page.getByPlaceholder('Remote team name').fill('bad-team');
        await page.getByRole('button', {name: 'Save'}).click();
        await expect(page.getByText('Invalid team name')).toBeVisible();
    });

    test('shows network error banner when save request fails completely', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route) => {
            if (route.request().method() === 'POST') {
                route.abort('connectionrefused');
            } else {
                route.continue();
            }
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        await page.getByPlaceholder('Remote team name').fill('gamma-team');
        await page.getByRole('button', {name: 'Save'}).click();
        await expect(page.getByText('Network error.')).toBeVisible();
    });

    test('editing state is cleared after successful save', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route) => {
            if (route.request().method() === 'POST') {
                route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
            } else {
                route.continue();
            }
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        await page.getByPlaceholder('Remote team name').fill('gamma-team');
        await page.getByRole('button', {name: 'Save'}).click();

        // Input should be gone after save completes
        await expect(page.getByPlaceholder('Remote team name')).not.toBeVisible();
    });

    test('re-fetches team status after successful save', async ({mount, page}) => {
        await setCsrfCookie(page);
        let fetchCount = 0;
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
            fetchCount++;
            const body = fetchCount > 1 ? {
                ...mockTeamStatus,
                connections: [
                    {name: 'inbound-relay', direction: 'inbound', linked: true, remote_team_name: 'gamma-team', file_transfer_enabled: true, file_filter_mode: ''},
                    mockTeamStatus.connections[1],
                ],
            } : mockTeamStatus;
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(body)});
        });
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route) => {
            if (route.request().method() === 'POST') {
                route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
            } else {
                route.continue();
            }
        });
        await mount(<CrossguardTeamModal/>);
        await openTeamModal(page, 'team-456');
        await expect(page.getByText('beta-team')).toBeVisible();
        await page.getByRole('button', {name: 'Edit'}).click();
        await page.getByPlaceholder('Remote team name').fill('gamma-team');
        await page.getByRole('button', {name: 'Save'}).click();

        // After re-fetch the new remote_team_name should appear
        await expect(page.getByText('gamma-team')).toBeVisible();
        expect(fetchCount).toBeGreaterThanOrEqual(2);
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 7. Rewrite clear flow
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Rewrite clear flow', () => {
    test('Clear sends DELETE with correct query param', async ({mount, page}) => {
        await setCsrfCookie(page);
        let capturedUrl = '';
        let capturedMethod = '';
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite*', (route) => {
            capturedUrl = route.request().url();
            capturedMethod = route.request().method();
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Clear'}).click();
        expect(capturedMethod).toBe('DELETE');
        expect(capturedUrl).toContain('/rewrite?connection=inbound-relay');
    });

    test('Clear includes CSRF token header', async ({mount, page}) => {
        await setCsrfCookie(page);
        let capturedHeaders: Record<string, string> = {};
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite*', (route) => {
            capturedHeaders = route.request().headers();
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Clear'}).click();
        expect(capturedHeaders['x-csrf-token']).toBe('test-csrf-token');
    });

    test('shows success banner after clearing rewrite', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite*', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Clear'}).click();
        await expect(page.getByText('Remote team rewrite cleared for "inbound-relay".')).toBeVisible();
    });

    test('shows error banner when clear fails', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite*', (route) => {
            route.fulfill({status: 500, contentType: 'application/json', body: JSON.stringify({error: 'Server error on clear'})});
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Clear'}).click();
        await expect(page.getByText('Server error on clear')).toBeVisible();
    });

    test('shows network error banner when clear request fails completely', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite*', (route) => {
            route.abort('connectionrefused');
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Clear'}).click();
        await expect(page.getByText('Network error.')).toBeVisible();
    });

    test('editing state is cleared after successful clear', async ({mount, page}) => {
        await setCsrfCookie(page);
        let fetchCount = 0;
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
            fetchCount++;
            const body = fetchCount > 1 ? {
                ...mockTeamStatus,
                connections: [
                    {name: 'inbound-relay', direction: 'inbound', linked: true, file_transfer_enabled: true, file_filter_mode: ''},
                    mockTeamStatus.connections[1],
                ],
            } : mockTeamStatus;
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(body)});
        });
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite*', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mount(<CrossguardTeamModal/>);
        await openTeamModal(page, 'team-456');
        await expect(page.getByText('beta-team')).toBeVisible();
        await page.getByRole('button', {name: 'Clear'}).click();

        // After clear + re-fetch, should show "No remote team rewrite"
        await expect(page.getByText('No remote team rewrite')).toBeVisible();
    });

    test('re-fetches team status after successful clear', async ({mount, page}) => {
        await setCsrfCookie(page);
        let fetchCount = 0;
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
            fetchCount++;
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(mockTeamStatus)});
        });
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite*', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mount(<CrossguardTeamModal/>);
        await openTeamModal(page, 'team-456');
        await expect(page.getByText('Alpha Team')).toBeVisible();
        const fetchBefore = fetchCount;
        await page.getByRole('button', {name: 'Clear'}).click();
        await expect(page.getByText('Remote team rewrite cleared')).toBeVisible();
        expect(fetchCount).toBeGreaterThan(fetchBefore);
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 8. Status banner
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Status banner', () => {
    test('no banner is visible on initial open', async ({mount, page}) => {
        await mountAndOpen(page, mount);

        // The status banner contains success/error messages. Neither should appear on load.
        await expect(page.getByText('Connection "')).not.toBeVisible();
        await expect(page.getByText('Network error.')).not.toBeVisible();
        await expect(page.getByText('Remote team rewrite')).not.toBeVisible();
    });

    test('auto-hides banner after 5 seconds', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/init*', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mountAndOpen(page, mount);

        await page.clock.install();
        await page.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(page.getByText('Connection "outbound-relay" linked.')).toBeVisible();

        // Advance time past the auto-hide threshold
        await page.clock.fastForward(5100);
        await expect(page.getByText('Connection "outbound-relay" linked.')).not.toBeVisible();
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 9. Empty state and errors
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Empty state and errors', () => {
    test('shows empty message when there are no connections', async ({mount, page}) => {
        const emptyStatus = {...mockTeamStatus, connections: []};
        await mountAndOpen(page, mount, 'team-456', emptyStatus);
        await expect(page.getByText('No connections available. Configure connections in the System Console.')).toBeVisible();
    });

    test('shows error when fetchStatus returns non-ok response', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
            route.fulfill({
                status: 500,
                contentType: 'application/json',
                body: JSON.stringify({error: 'Internal server error'}),
            });
        });
        await mount(<CrossguardTeamModal/>);
        await openTeamModal(page, 'team-456');
        await expect(page.getByText('Internal server error')).toBeVisible();
    });

    test('shows network error when fetchStatus request fails', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
            route.abort('connectionrefused');
        });
        await mount(<CrossguardTeamModal/>);
        await openTeamModal(page, 'team-456');
        await expect(page.getByText('Network error loading team status.')).toBeVisible();
    });

    test('null connections in response treated as empty array', async ({mount, page}) => {
        const nullConnStatus = {...mockTeamStatus, connections: null};
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify(nullConnStatus),
            });
        });
        await mount(<CrossguardTeamModal/>);
        await openTeamModal(page, 'team-456');
        await expect(page.getByText('No connections available. Configure connections in the System Console.')).toBeVisible();
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 10. Help link
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Help link', () => {
    test('renders documentation link with correct URL', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        const link = page.getByRole('link', {name: 'View Cross Guard Documentation'});
        await expect(link).toHaveAttribute('href', '/plugins/crossguard/public/help/help.html');
    });

    test('documentation link opens in new tab with security attributes', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        const link = page.getByRole('link', {name: 'View Cross Guard Documentation'});
        await expect(link).toHaveAttribute('target', '_blank');
        await expect(link).toHaveAttribute('rel', 'noopener noreferrer');
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 11. Rewrite edit flow - extended
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Rewrite edit flow - extended', () => {
    test('Save disabled when input is only whitespace', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    team_id: 'team-ws',
                    team_name: 'ws-team',
                    team_display_name: 'WS Team',
                    initialized: true,
                    connections: [{name: 'ws-conn', direction: 'inbound', linked: true, file_transfer_enabled: false}],
                }),
            });
        });

        const component = await mount(<CrossguardTeamModal/>);
        await page.evaluate(() => {
            document.dispatchEvent(new CustomEvent('crossguard:open-team-modal', {detail: {teamID: 'team-ws'}}));
        });
        await component.getByRole('button', {name: 'Set'}).click();
        await component.getByPlaceholder('Remote team name').fill('   ');
        await expect(component.getByRole('button', {name: 'Save'})).toBeDisabled();
    });

    test('Enter key does not save when input is whitespace-only', async ({mount, page}) => {
        let postCalled = false;
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    team_id: 'team-enter-ws',
                    team_name: 'enter-ws',
                    team_display_name: 'Enter WS',
                    initialized: true,
                    connections: [{name: 'enter-conn', direction: 'inbound', linked: true, file_transfer_enabled: false}],
                }),
            });
        });
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route) => {
            postCalled = true;
            route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
        });

        const component = await mount(<CrossguardTeamModal/>);
        await page.evaluate(() => {
            document.dispatchEvent(new CustomEvent('crossguard:open-team-modal', {detail: {teamID: 'team-enter-ws'}}));
        });
        await component.getByRole('button', {name: 'Set'}).click();
        await component.getByPlaceholder('Remote team name').fill('   ');
        await component.getByPlaceholder('Remote team name').press('Enter');
        expect(postCalled).toBe(false);
    });

    test('only actively edited connection shows edit input', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    team_id: 'team-multi',
                    team_name: 'multi-team',
                    team_display_name: 'Multi Team',
                    initialized: true,
                    connections: [
                        {name: 'conn-x', direction: 'inbound', linked: true, file_transfer_enabled: false},
                        {name: 'conn-y', direction: 'inbound', linked: true, file_transfer_enabled: false},
                    ],
                }),
            });
        });

        const component = await mount(<CrossguardTeamModal/>);
        await page.evaluate(() => {
            document.dispatchEvent(new CustomEvent('crossguard:open-team-modal', {detail: {teamID: 'team-multi'}}));
        });

        // Click Set on first inbound connection
        await component.getByRole('button', {name: 'Set'}).first().click();
        const inputs = component.getByPlaceholder('Remote team name');
        expect(await inputs.count()).toBe(1);
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 12. Rewrite clear flow - extended
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Rewrite clear flow - extended', () => {
    test('handleClearRewrite includes X-Requested-With header', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    team_id: 'team-xhr',
                    team_name: 'xhr-team',
                    team_display_name: 'XHR Team',
                    initialized: true,
                    connections: [{name: 'xhr-conn', direction: 'inbound', linked: true, remote_team_name: 'remote-x', file_transfer_enabled: false}],
                }),
            });
        });

        let capturedHeaders: Record<string, string> = {};
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite*', (route) => {
            capturedHeaders = route.request().headers();
            route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
        });

        const component = await mount(<CrossguardTeamModal/>);
        await page.evaluate(() => {
            document.dispatchEvent(new CustomEvent('crossguard:open-team-modal', {detail: {teamID: 'team-xhr'}}));
        });
        await component.getByRole('button', {name: 'Clear'}).click();
        expect(capturedHeaders['x-requested-with']).toBe('XMLHttpRequest');
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 13. Team modal lifecycle - extended
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Team modal lifecycle - extended', () => {
    test('ignores event with no teamID in detail', async ({mount, page}) => {
        const component = await mount(<CrossguardTeamModal/>);
        await page.evaluate(() => {
            document.dispatchEvent(new CustomEvent('crossguard:open-team-modal', {detail: {}}));
        });
        await expect(component).toBeEmpty();
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 14. Team modal errors - extended
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Team modal errors - extended', () => {
    test('shows fallback error when status response has no error field', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
            route.fulfill({status: 403, contentType: 'application/json', body: '{}'});
        });

        const component = await mount(<CrossguardTeamModal/>);
        await page.evaluate(() => {
            document.dispatchEvent(new CustomEvent('crossguard:open-team-modal', {detail: {teamID: 'team-fallback'}}));
        });
        await expect(component.getByText('Failed to load team status.')).toBeVisible();
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 15. Team modal status timer
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Team modal status timer', () => {
    test('timer cleared when modal re-opened prevents stale banner', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    team_id: 'team-timer',
                    team_name: 'timer-team',
                    team_display_name: 'Timer Team',
                    initialized: true,
                    connections: [{name: 'timer-conn', direction: 'inbound', linked: false, file_transfer_enabled: false}],
                }),
            });
        });
        await page.route('**/plugins/crossguard/api/v1/teams/*/init*', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
        });

        const component = await mount(<CrossguardTeamModal/>);

        // Open and trigger a link action (creates status banner with timer)
        await page.evaluate(() => {
            document.dispatchEvent(new CustomEvent('crossguard:open-team-modal', {detail: {teamID: 'team-timer'}}));
        });
        await component.getByRole('button', {name: 'Link'}).click();
        await expect(component.getByText(/linked/i)).toBeVisible();

        // Close modal
        await page.keyboard.press('Escape');
        await expect(component).toBeEmpty();

        // Re-open - should not show stale banner
        await page.evaluate(() => {
            document.dispatchEvent(new CustomEvent('crossguard:open-team-modal', {detail: {teamID: 'team-timer'}}));
        });
        await expect(component.getByText('Cross Guard Settings')).toBeVisible();

        // The banner should not persist from the previous session
        await expect(component.getByText(/linked/i)).not.toBeVisible();
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 16. Rewrite display and interaction
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Rewrite display and interaction', () => {
    test('inbound connection with remote_team_name shows label and value', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await expect(page.getByText('Remote team:')).toBeVisible();
        await expect(page.getByText('beta-team')).toBeVisible();
    });

    test('inbound connection without remote_team_name shows no-rewrite text and Set button', async ({mount, page}) => {
        const noRewrite = {
            ...mockTeamStatus,
            connections: [
                {name: 'inbound-relay', direction: 'inbound', linked: true, remote_team_name: '', file_transfer_enabled: false, file_filter_mode: ''},
            ],
        };
        await mountAndOpen(page, mount, 'team-456', noRewrite);
        await expect(page.getByText('No remote team rewrite')).toBeVisible();
        await expect(page.getByRole('button', {name: 'Set'})).toBeVisible();
        await expect(page.getByRole('button', {name: 'Edit'})).not.toBeVisible();
        await expect(page.getByRole('button', {name: 'Clear'})).not.toBeVisible();
    });

    test('outbound connection does not show Remote team label or Set/Edit/Clear buttons', async ({mount, page}) => {
        const outboundOnly = {
            ...mockTeamStatus,
            connections: [
                {name: 'outbound-relay', direction: 'outbound', linked: false, remote_team_name: '', file_transfer_enabled: false, file_filter_mode: ''},
            ],
        };
        await mountAndOpen(page, mount, 'team-456', outboundOnly);
        await expect(page.getByText('Remote team:')).not.toBeVisible();
        await expect(page.getByText('No remote team rewrite')).not.toBeVisible();
        await expect(page.getByRole('button', {name: 'Set'})).not.toBeVisible();
        await expect(page.getByRole('button', {name: 'Edit'})).not.toBeVisible();
        await expect(page.getByRole('button', {name: 'Clear'})).not.toBeVisible();
    });

    test('clicking Edit pre-fills input with current remote_team_name', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        await expect(page.getByPlaceholder('Remote team name')).toHaveValue('beta-team');
    });

    test('clicking Set opens edit form with empty input', async ({mount, page}) => {
        const noRewrite = {
            ...mockTeamStatus,
            connections: [
                {name: 'inbound-relay', direction: 'inbound', linked: true, remote_team_name: '', file_transfer_enabled: false, file_filter_mode: ''},
            ],
        };
        await mountAndOpen(page, mount, 'team-456', noRewrite);
        await page.getByRole('button', {name: 'Set'}).click();
        await expect(page.getByPlaceholder('Remote team name')).toBeVisible();
        await expect(page.getByPlaceholder('Remote team name')).toHaveValue('');
    });

    test('clicking Cancel in edit form hides the input', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        await expect(page.getByPlaceholder('Remote team name')).toBeVisible();
        await page.getByRole('button', {name: 'Cancel'}).click();
        await expect(page.getByPlaceholder('Remote team name')).not.toBeVisible();
        await expect(page.getByText('Remote team:')).toBeVisible();
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 17. Save rewrite API
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Save rewrite API', () => {
    test('POST to rewrite endpoint with correct body on save', async ({mount, page}) => {
        await setCsrfCookie(page);
        let capturedUrl = '';
        let capturedBody = '';
        let capturedMethod = '';
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route) => {
            capturedUrl = route.request().url();
            capturedMethod = route.request().method();
            capturedBody = route.request().postData() || '';
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        const noRewrite = {
            ...mockTeamStatus,
            connections: [
                {name: 'inbound-relay', direction: 'inbound', linked: true, remote_team_name: '', file_transfer_enabled: false, file_filter_mode: ''},
                mockTeamStatus.connections[1],
            ],
        };
        await mountAndOpen(page, mount, 'team-456', noRewrite);
        await page.getByRole('button', {name: 'Set'}).click();
        await page.getByPlaceholder('Remote team name').fill('new-team');
        await page.getByRole('button', {name: 'Save'}).click();
        expect(capturedMethod).toBe('POST');
        expect(capturedUrl).toContain('/teams/team-456/rewrite');
        expect(JSON.parse(capturedBody)).toEqual({connection: 'inbound-relay', remote_team_name: 'new-team'});
    });

    test('shows success banner after successful rewrite save', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route) => {
            if (route.request().method() === 'POST') {
                route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
            } else {
                route.continue();
            }
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        await page.getByPlaceholder('Remote team name').fill('new-team');
        await page.getByRole('button', {name: 'Save'}).click();
        await expect(page.getByText('Remote team rewrite updated for "inbound-relay".')).toBeVisible();
    });

    test('shows server error when rewrite POST returns 500', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route) => {
            if (route.request().method() === 'POST') {
                route.fulfill({status: 500, contentType: 'application/json', body: JSON.stringify({error: 'forbidden'})});
            } else {
                route.continue();
            }
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        await page.getByPlaceholder('Remote team name').fill('bad-team');
        await page.getByRole('button', {name: 'Save'}).click();
        await expect(page.getByText('forbidden')).toBeVisible();
    });

    test('shows network error when rewrite POST aborts', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route) => {
            if (route.request().method() === 'POST') {
                route.abort('connectionrefused');
            } else {
                route.continue();
            }
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        await page.getByPlaceholder('Remote team name').fill('any-team');
        await page.getByRole('button', {name: 'Save'}).click();
        await expect(page.getByText('Network error.')).toBeVisible();
    });

    test('Save button is disabled when input is empty', async ({mount, page}) => {
        const noRewrite = {
            ...mockTeamStatus,
            connections: [
                {name: 'inbound-relay', direction: 'inbound', linked: true, remote_team_name: '', file_transfer_enabled: false, file_filter_mode: ''},
            ],
        };
        await mountAndOpen(page, mount, 'team-456', noRewrite);
        await page.getByRole('button', {name: 'Set'}).click();
        await expect(page.getByRole('button', {name: 'Save'})).toBeDisabled();
    });

    test('Save button is disabled when input is only whitespace', async ({mount, page}) => {
        const noRewrite = {
            ...mockTeamStatus,
            connections: [
                {name: 'inbound-relay', direction: 'inbound', linked: true, remote_team_name: '', file_transfer_enabled: false, file_filter_mode: ''},
            ],
        };
        await mountAndOpen(page, mount, 'team-456', noRewrite);
        await page.getByRole('button', {name: 'Set'}).click();
        await page.getByPlaceholder('Remote team name').fill('   ');
        await expect(page.getByRole('button', {name: 'Save'})).toBeDisabled();
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 18. Clear rewrite API
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Clear rewrite API', () => {
    test('sends DELETE to rewrite endpoint with connection query param', async ({mount, page}) => {
        await setCsrfCookie(page);
        let capturedUrl = '';
        let capturedMethod = '';
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite*', (route) => {
            capturedUrl = route.request().url();
            capturedMethod = route.request().method();
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Clear'}).click();
        expect(capturedMethod).toBe('DELETE');
        expect(capturedUrl).toContain('/teams/team-456/rewrite?connection=inbound-relay');
    });

    test('shows success banner after clearing rewrite', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite*', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Clear'}).click();
        await expect(page.getByText('Remote team rewrite cleared for "inbound-relay".')).toBeVisible();
    });

    test('shows server error when clear DELETE returns 500', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite*', (route) => {
            route.fulfill({status: 500, contentType: 'application/json', body: JSON.stringify({error: 'not found'})});
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Clear'}).click();
        await expect(page.getByText('not found')).toBeVisible();
    });

    test('shows network error when clear DELETE aborts', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite*', (route) => {
            route.abort('connectionrefused');
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Clear'}).click();
        await expect(page.getByText('Network error.')).toBeVisible();
    });

    test('after clear success the connection shows no-rewrite text', async ({mount, page}) => {
        await setCsrfCookie(page);
        let fetchCount = 0;
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
            fetchCount++;
            const body = fetchCount > 1 ? {
                ...mockTeamStatus,
                connections: [
                    {name: 'inbound-relay', direction: 'inbound', linked: true, remote_team_name: '', file_transfer_enabled: true, file_filter_mode: ''},
                    mockTeamStatus.connections[1],
                ],
            } : mockTeamStatus;
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(body)});
        });
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite*', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mount(<CrossguardTeamModal/>);
        await openTeamModal(page, 'team-456');
        await expect(page.getByText('beta-team')).toBeVisible();
        await page.getByRole('button', {name: 'Clear'}).click();
        await expect(page.getByText('No remote team rewrite')).toBeVisible();
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 19. Rewrite keyboard shortcuts
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Rewrite keyboard shortcuts', () => {
    test('Enter triggers save when input is non-empty (POST sent)', async ({mount, page}) => {
        await setCsrfCookie(page);
        let postSent = false;
        let capturedBody = '';
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route) => {
            if (route.request().method() === 'POST') {
                postSent = true;
                capturedBody = route.request().postData() || '';
                route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
            } else {
                route.continue();
            }
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        await page.getByPlaceholder('Remote team name').fill('team-name');
        await page.getByPlaceholder('Remote team name').press('Enter');
        await expect(page.getByText('Remote team rewrite updated')).toBeVisible();
        expect(postSent).toBe(true);
        expect(JSON.parse(capturedBody)).toEqual({connection: 'inbound-relay', remote_team_name: 'team-name'});
    });

    test('Enter does not save when input is empty (no POST sent)', async ({mount, page}) => {
        await setCsrfCookie(page);
        let postSent = false;
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route) => {
            if (route.request().method() === 'POST') {
                postSent = true;
                route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
            } else {
                route.continue();
            }
        });
        const noRewrite = {
            ...mockTeamStatus,
            connections: [
                {name: 'inbound-relay', direction: 'inbound', linked: true, remote_team_name: '', file_transfer_enabled: false, file_filter_mode: ''},
            ],
        };
        await mountAndOpen(page, mount, 'team-456', noRewrite);
        await page.getByRole('button', {name: 'Set'}).click();
        await page.getByPlaceholder('Remote team name').press('Enter');
        expect(postSent).toBe(false);
        await expect(page.getByPlaceholder('Remote team name')).toBeVisible();
    });

    test('Escape closes edit form but modal stays open', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        await expect(page.getByPlaceholder('Remote team name')).toBeVisible();
        await page.getByPlaceholder('Remote team name').press('Escape');

        // Edit form should be gone
        await expect(page.getByPlaceholder('Remote team name')).not.toBeVisible();

        // Modal should still be open (stopPropagation prevents the document Escape handler)
        await expect(page.getByText('Cross Guard Settings for Alpha Team')).toBeVisible();
    });

    test('Escape in rewrite input does not close the modal via stopPropagation', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        const input = page.getByPlaceholder('Remote team name');
        await expect(input).toBeVisible();
        await expect(input).toBeFocused();

        // Press Escape while focused in the input
        await input.press('Escape');

        // The rewrite input should be dismissed
        await expect(input).not.toBeVisible();

        // The modal must remain open because stopPropagation prevents the document-level handler
        await expect(page.getByText('Cross Guard Settings for Alpha Team')).toBeVisible();
        await expect(page.getByText('Remote team:')).toBeVisible();
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 20. Rewrite state reset
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Rewrite state reset', () => {
    test('opening modal for different team clears editing rewrite state', async ({mount, page}) => {
        let callCount = 0;
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
            callCount++;
            const body = callCount <= 1 ? mockTeamStatus : {
                team_id: 'team-bravo',
                team_name: 'bravo-team',
                team_display_name: 'Bravo Team',
                initialized: true,
                connections: [
                    {name: 'bravo-conn', direction: 'inbound', linked: true, remote_team_name: 'remote-bravo', file_transfer_enabled: false, file_filter_mode: ''},
                ],
            };
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(body)});
        });

        await mount(<CrossguardTeamModal/>);
        await openTeamModal(page, 'team-456');
        await expect(page.getByText('Alpha Team')).toBeVisible();

        // Start editing rewrite on first team
        await page.getByRole('button', {name: 'Edit'}).click();
        await expect(page.getByPlaceholder('Remote team name')).toBeVisible();

        // Dispatch open event for a different team
        await openTeamModal(page, 'team-bravo');
        await expect(page.getByText('Bravo Team')).toBeVisible();

        // The rewrite input should NOT be visible (editingRewrite was cleared)
        await expect(page.getByPlaceholder('Remote team name')).not.toBeVisible();
    });

    test('successful save resets editing state (no input visible)', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route) => {
            if (route.request().method() === 'POST') {
                route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
            } else {
                route.continue();
            }
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        await expect(page.getByPlaceholder('Remote team name')).toBeVisible();
        await page.getByPlaceholder('Remote team name').fill('updated-team');
        await page.getByRole('button', {name: 'Save'}).click();

        // After successful save, editing state resets
        await expect(page.getByPlaceholder('Remote team name')).not.toBeVisible();
        await expect(page.getByText('Remote team rewrite updated')).toBeVisible();
    });

    test('rewriteInput value is cleared when modal opens for new team', async ({mount, page}) => {
        let callCount = 0;
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
            callCount++;
            const body = callCount <= 1 ? mockTeamStatus : {
                team_id: 'team-charlie',
                team_name: 'charlie-team',
                team_display_name: 'Charlie Team',
                initialized: true,
                connections: [
                    {name: 'charlie-conn', direction: 'inbound', linked: true, remote_team_name: '', file_transfer_enabled: false, file_filter_mode: ''},
                ],
            };
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(body)});
        });

        await mount(<CrossguardTeamModal/>);
        await openTeamModal(page, 'team-456');
        await expect(page.getByText('Alpha Team')).toBeVisible();

        // Start editing and type something
        await page.getByRole('button', {name: 'Edit'}).click();
        await page.getByPlaceholder('Remote team name').fill('partial-input');

        // Open for a different team
        await openTeamModal(page, 'team-charlie');
        await expect(page.getByText('Charlie Team')).toBeVisible();

        // Click Set on the new team's inbound connection to open the edit form
        await page.getByRole('button', {name: 'Set'}).click();

        // The input should be empty, not carrying over "partial-input" from previous team
        await expect(page.getByPlaceholder('Remote team name')).toHaveValue('');
    });
});
