import React from 'react';

import CrossguardTeamModal from './CrossguardTeamModal';

import {test, expect} from '../../playwright/ct-coverage';

function teamStatusResponse(overrides?: any) {
    return {team_id: 'team1', team_name: 'test-team', team_display_name: 'Test Team', initialized: true, connections: [], ...overrides};
}

function connStatus(overrides?: any) {
    return {name: 'my-conn', direction: 'inbound', provider: 'nats', linked: false, orphaned: false, file_transfer_enabled: false, ...overrides};
}

async function openModal(page: any, teamID = 'team1') {
    await page.evaluate((id: string) => {
        document.dispatchEvent(new CustomEvent('crossguard:open-team-modal', {detail: {teamID: id}}));
    }, teamID);
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
    const responseBody = body || teamStatusResponse({connections: [connStatus()]});
    await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route: any) => {
        route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(responseBody)});
    });
}

async function mountAndOpen(page: any, mount: any, teamID = 'team1', body?: unknown) {
    const responseBody = body || teamStatusResponse({connections: [connStatus()]});
    await routeStatusOk(page, responseBody);
    const component = await mount(<CrossguardTeamModal/>);
    await openModal(page, teamID);
    await page.getByText('Test Team').waitFor({state: 'visible', timeout: 5000}).catch(() => {});
    return component;
}

// ---------------------------------------------------------------------------
// 1. Modal lifecycle
// ---------------------------------------------------------------------------
test.describe('Modal lifecycle edge cases', () => {
    test('opening for a different team resets previous state', async ({mount, page}) => {
        let callCount = 0;
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route: any) => {
            callCount++;
            const body = callCount === 1 ? teamStatusResponse({connections: [connStatus({name: 'first-conn'})], team_display_name: 'First Team'}) : teamStatusResponse({connections: [connStatus({name: 'second-conn'})], team_display_name: 'Second Team'});
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(body)});
        });
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');
        await expect(page.getByText('first-conn')).toBeVisible();

        await page.locator('button').filter({hasText: '\u00D7'}).click();
        await openModal(page, 'team2');
        await expect(page.getByText('second-conn')).toBeVisible();
        await expect(page.getByText('first-conn')).not.toBeVisible();
    });

    test('re-opening the same team re-fetches status', async ({mount, page}) => {
        let callCount = 0;
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route: any) => {
            callCount++;
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(teamStatusResponse({connections: [connStatus()]}))});
        });
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');
        await expect(page.getByText('my-conn')).toBeVisible();
        const firstCount = callCount;

        await page.locator('button').filter({hasText: '\u00D7'}).click();
        await openModal(page, 'team1');
        await expect(page.getByText('my-conn')).toBeVisible();
        expect(callCount).toBeGreaterThan(firstCount);
    });

    test('re-opening replaces previous content with new data', async ({mount, page}) => {
        let callCount = 0;
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route: any) => {
            callCount++;
            const body = callCount === 1 ? teamStatusResponse({connections: [connStatus({name: 'alpha'})], team_display_name: 'Alpha'}) : teamStatusResponse({connections: [connStatus({name: 'beta'})], team_display_name: 'Beta'});
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(body)});
        });
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');
        await expect(page.getByText('alpha', {exact: true})).toBeVisible();

        await page.locator('button').filter({hasText: '\u00D7'}).click();
        await openModal(page, 'team2');
        await expect(page.getByText('beta', {exact: true})).toBeVisible();
        await expect(page.getByText('Cross Guard Settings for Beta')).toBeVisible();
    });

    test('null teamID in event detail does not render modal', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardTeamModal/>);
        await page.evaluate(() => {
            document.dispatchEvent(new CustomEvent('crossguard:open-team-modal', {detail: {teamID: null}}));
        });
        await expect(component).toBeEmpty();
    });

    test('missing teamID in event detail does not render modal', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardTeamModal/>);
        await page.evaluate(() => {
            document.dispatchEvent(new CustomEvent('crossguard:open-team-modal', {detail: {}}));
        });
        await expect(component).toBeEmpty();
    });
});

// ---------------------------------------------------------------------------
// 2. Fetch errors
// ---------------------------------------------------------------------------
test.describe('Fetch errors edge cases', () => {
    test('500 with error JSON shows the error message', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route: any) => {
            route.fulfill({status: 500, contentType: 'application/json', body: JSON.stringify({error: 'Internal server error'})});
        });
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');
        await expect(page.getByText('Internal server error')).toBeVisible();
    });

    test('500 with non-JSON body shows network error', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route: any) => {
            route.fulfill({status: 500, contentType: 'text/plain', body: 'Bad Gateway'});
        });
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');
        await expect(page.getByText('Network error loading team status.')).toBeVisible();
    });

    test('network abort shows network error message', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route: any) => {
            route.abort('connectionfailed');
        });
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');
        await expect(page.getByText('Network error loading team status.')).toBeVisible();
    });

    test('connections null shows empty state', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route: any) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(teamStatusResponse({connections: null}))});
        });
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');
        await expect(page.getByText('No connections available. Configure connections in the System Console.')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 3. Rewrite controls
// ---------------------------------------------------------------------------
test.describe('Rewrite controls edge cases', () => {
    test('outbound connection does not show rewrite controls', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'out-conn', direction: 'outbound'})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await expect(page.getByText('out-conn')).toBeVisible();
        await expect(page.getByText('No remote team rewrite')).not.toBeVisible();
        await expect(page.getByRole('button', {name: 'Set'})).not.toBeVisible();
    });

    test('inbound without rewrite shows Set button', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: ''})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await expect(page.getByText('No remote team rewrite')).toBeVisible();
        await expect(page.getByRole('button', {name: 'Set'})).toBeVisible();
    });

    test('inbound with rewrite shows Edit and Clear', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: 'target-team'})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await expect(page.getByText('target-team')).toBeVisible();
        await expect(page.getByRole('button', {name: 'Edit'})).toBeVisible();
        await expect(page.getByRole('button', {name: 'Clear'})).toBeVisible();
    });

    test('Set opens empty input field', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: ''})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await page.getByRole('button', {name: 'Set'}).click();
        const input = page.getByPlaceholder('Remote team name');
        await expect(input).toBeVisible();
        await expect(input).toHaveValue('');
    });

    test('Edit pre-fills the input with current remote_team_name', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: 'existing-name'})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await page.getByRole('button', {name: 'Edit'}).click();
        const input = page.getByPlaceholder('Remote team name');
        await expect(input).toHaveValue('existing-name');
    });

    test('Save disabled when input is empty', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: ''})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await page.getByRole('button', {name: 'Set'}).click();
        await expect(page.getByRole('button', {name: 'Save'})).toBeDisabled();
    });

    test('Save disabled when input is whitespace only', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: ''})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await page.getByRole('button', {name: 'Set'}).click();
        await page.getByPlaceholder('Remote team name').fill('   ');
        await expect(page.getByRole('button', {name: 'Save'})).toBeDisabled();
    });

    test('Enter does not save when input is empty', async ({mount, page}) => {
        await setCsrfCookie(page);
        let saved = false;
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route: any) => {
            if (route.request().method() === 'POST') {
                saved = true;
                route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
            } else {
                route.continue();
            }
        });
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: ''})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await page.getByRole('button', {name: 'Set'}).click();
        await page.getByPlaceholder('Remote team name').press('Enter');
        expect(saved).toBe(false);
        await expect(page.getByPlaceholder('Remote team name')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 4. Rewrite operations
// ---------------------------------------------------------------------------
test.describe('Rewrite operations edge cases', () => {
    test('Save sends POST with correct body', async ({mount, page}) => {
        await setCsrfCookie(page);
        let capturedBody = '';
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route: any) => {
            if (route.request().method() === 'POST') {
                capturedBody = route.request().postData() || '';
                route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
            } else {
                route.continue();
            }
        });
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: 'old-name'})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await page.getByRole('button', {name: 'Edit'}).click();
        await page.getByPlaceholder('Remote team name').fill('new-name');
        await page.getByRole('button', {name: 'Save'}).click();
        await expect(() => {
            expect(JSON.parse(capturedBody)).toEqual({connection: 'in-conn', remote_team_name: 'new-name'});
        }).toPass();
    });

    test('Clear sends DELETE with correct query param', async ({mount, page}) => {
        await setCsrfCookie(page);
        let capturedUrl = '';
        let capturedMethod = '';
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite*', (route: any) => {
            capturedUrl = route.request().url();
            capturedMethod = route.request().method();
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        const body = teamStatusResponse({
            connections: [connStatus({name: 'clear-conn', direction: 'inbound', remote_team_name: 'some-team'})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await page.getByRole('button', {name: 'Clear'}).click();
        await expect(() => {
            expect(capturedMethod).toBe('DELETE');
            expect(capturedUrl).toContain('/rewrite?connection=clear-conn');
        }).toPass();
    });

    test('save clears editing state after success', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route: any) => {
            if (route.request().method() === 'POST') {
                route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
            } else {
                route.continue();
            }
        });
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: 'old'})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await page.getByRole('button', {name: 'Edit'}).click();
        await page.getByPlaceholder('Remote team name').fill('new-val');
        await page.getByRole('button', {name: 'Save'}).click();
        await expect(page.getByPlaceholder('Remote team name')).not.toBeVisible();
    });

    test('clear clears editing state after success', async ({mount, page}) => {
        await setCsrfCookie(page);
        let fetchCount = 0;
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route: any) => {
            fetchCount++;
            const respBody = fetchCount > 1 ? teamStatusResponse({connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: ''})]}) : teamStatusResponse({connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: 'old-team'})]});
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(respBody)});
        });
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite*', (route: any) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');
        await expect(page.getByText('old-team')).toBeVisible();
        await page.getByRole('button', {name: 'Clear'}).click();
        await expect(page.getByText('No remote team rewrite')).toBeVisible();
    });

    test('save error shows banner with error message', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route: any) => {
            if (route.request().method() === 'POST') {
                route.fulfill({status: 400, contentType: 'application/json', body: JSON.stringify({error: 'Rewrite conflict'})});
            } else {
                route.continue();
            }
        });
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: 'old'})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await page.getByRole('button', {name: 'Edit'}).click();
        await page.getByPlaceholder('Remote team name').fill('conflict-name');
        await page.getByRole('button', {name: 'Save'}).click();
        await expect(page.getByText('Rewrite conflict')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 5. Escape in rewrite
// ---------------------------------------------------------------------------
test.describe('Escape in rewrite edge cases', () => {
    test('Escape in input cancels edit but does not close modal', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: 'existing'})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await page.getByRole('button', {name: 'Edit'}).click();
        await expect(page.getByPlaceholder('Remote team name')).toBeVisible();
        await page.getByPlaceholder('Remote team name').press('Escape');
        await expect(page.getByPlaceholder('Remote team name')).not.toBeVisible();
        await expect(page.getByText('Cross Guard Settings for Test Team')).toBeVisible();
    });

    test('Escape outside rewrite input closes modal', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: 'existing'})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await expect(page.getByText('Cross Guard Settings for Test Team')).toBeVisible();
        await page.keyboard.press('Escape');
        await expect(page.getByText('Cross Guard Settings for Test Team')).not.toBeVisible();
    });

    test('Escape in input calls stopPropagation so modal stays open', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: 'team-x'})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await page.getByRole('button', {name: 'Edit'}).click();
        const input = page.getByPlaceholder('Remote team name');
        await expect(input).toBeFocused();

        await input.press('Escape');
        await expect(input).not.toBeVisible();
        await expect(page.getByText('Cross Guard Settings for Test Team')).toBeVisible();

        // Now pressing Escape again should close the modal
        await page.keyboard.press('Escape');
        await expect(page.getByText('Cross Guard Settings for Test Team')).not.toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 6. Mixed states
// ---------------------------------------------------------------------------
test.describe('Mixed states edge cases', () => {
    test('multiple mixed connections render with correct badges', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [
                connStatus({name: 'in-a', direction: 'inbound', linked: true, remote_team_name: 'remote-a'}),
                connStatus({name: 'out-b', direction: 'outbound', linked: false}),
                connStatus({name: 'in-c', direction: 'inbound', linked: false, orphaned: true}),
            ],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await expect(page.getByText('in-a')).toBeVisible();
        await expect(page.getByText('out-b')).toBeVisible();
        await expect(page.getByText('in-c')).toBeVisible();
        const inboundBadges = page.getByText('NATS \u2192 MATTERMOST');
        const outboundBadges = page.getByText('MATTERMOST \u2192 NATS');
        await expect(inboundBadges).toHaveCount(2);
        await expect(outboundBadges).toHaveCount(1);
        await expect(page.getByTitle('Connection no longer in configuration')).toBeVisible();
    });

    test('action disables all buttons across multiple connections', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [
                connStatus({name: 'conn-a', direction: 'inbound', linked: false}),
                connStatus({name: 'conn-b', direction: 'outbound', linked: true}),
            ],
        });
        await routeStatusOk(page, body);
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');
        await expect(page.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        await page.route('**/plugins/crossguard/api/v1/teams/*/init*', async (route: any) => {
            await new Promise((resolve) => setTimeout(resolve, 2000));
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await setCsrfCookie(page);
        await page.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(page.getByText('Linking...')).toBeVisible();
        await expect(page.getByRole('button', {name: 'Unlink', exact: true})).toBeDisabled();
    });

    test('file transfer display variants render correctly', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [
                connStatus({name: 'allow-conn', direction: 'inbound', file_transfer_enabled: true, file_filter_mode: 'allow', file_filter_types: '.pdf,.docx'}),
                connStatus({name: 'deny-conn', direction: 'outbound', file_transfer_enabled: true, file_filter_mode: 'deny', file_filter_types: '.exe'}),
                connStatus({name: 'all-conn', direction: 'inbound', file_transfer_enabled: true}),
                connStatus({name: 'disabled-conn', direction: 'outbound', file_transfer_enabled: false}),
            ],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await expect(page.getByText('Allow .pdf,.docx')).toBeVisible();
        await expect(page.getByText('Deny .exe')).toBeVisible();
        await expect(page.getByText('All types')).toBeVisible();
        await expect(page.getByText('Files: Disabled').first()).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 7. handleToggle error paths
// ---------------------------------------------------------------------------
test.describe('handleToggle error paths', () => {
    test('network abort during toggle shows Network error banner', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'net-fail', linked: false})],
        });
        await routeStatusOk(page, body);
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');
        await expect(page.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        await page.route('**/plugins/crossguard/api/v1/teams/team1/init*', (route: any) => {
            route.abort('connectionfailed');
        });
        await setCsrfCookie(page);
        await page.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(page.getByText('Network error.')).toBeVisible();
    });

    test('API error without error field shows fallback message', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'fallback-conn', linked: false})],
        });
        await routeStatusOk(page, body);
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');
        await expect(page.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        await page.route('**/plugins/crossguard/api/v1/teams/team1/init*', (route: any) => {
            route.fulfill({status: 400, contentType: 'application/json', body: JSON.stringify({status: 'fail'})});
        });
        await setCsrfCookie(page);
        await page.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(page.getByText('Failed to link connection.')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 8. handleSaveRewrite error paths
// ---------------------------------------------------------------------------
test.describe('handleSaveRewrite error paths', () => {
    test('network abort during save rewrite shows Network error banner', async ({mount, page}) => {
        await setCsrfCookie(page);
        const body = teamStatusResponse({
            connections: [connStatus({name: 'save-fail', direction: 'inbound', remote_team_name: ''})],
        });
        await routeStatusOk(page, body);
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');
        await page.getByRole('button', {name: 'Set'}).click();
        await page.getByPlaceholder('Remote team name').fill('new-team');

        await page.route('**/plugins/crossguard/api/v1/teams/team1/rewrite', (route: any) => {
            if (route.request().method() === 'POST') {
                route.abort('connectionfailed');
            } else {
                route.continue();
            }
        });
        await page.getByRole('button', {name: 'Save'}).click();
        await expect(page.getByText('Network error.')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 9. handleClearRewrite error paths
// ---------------------------------------------------------------------------
test.describe('handleClearRewrite error paths', () => {
    test('network abort during clear rewrite shows Network error banner', async ({mount, page}) => {
        await setCsrfCookie(page);
        const body = teamStatusResponse({
            connections: [connStatus({name: 'clear-fail', direction: 'inbound', remote_team_name: 'old-team'})],
        });
        await routeStatusOk(page, body);
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');
        await expect(page.getByText('old-team')).toBeVisible();

        await page.route('**/plugins/crossguard/api/v1/teams/team1/rewrite*', (route: any) => {
            route.abort('connectionfailed');
        });
        await page.getByRole('button', {name: 'Clear'}).click();
        await expect(page.getByText('Network error.')).toBeVisible();
    });

    test('API error during clear shows error message', async ({mount, page}) => {
        await setCsrfCookie(page);
        const body = teamStatusResponse({
            connections: [connStatus({name: 'clear-err', direction: 'inbound', remote_team_name: 'existing-team'})],
        });
        await routeStatusOk(page, body);
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');
        await expect(page.getByText('existing-team')).toBeVisible();

        await page.route('**/plugins/crossguard/api/v1/teams/team1/rewrite*', (route: any) => {
            route.fulfill({status: 400, contentType: 'application/json', body: JSON.stringify({error: 'Rewrite not found'})});
        });
        await page.getByRole('button', {name: 'Clear'}).click();
        await expect(page.getByText('Rewrite not found')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 10. CSRF token in team modal
// ---------------------------------------------------------------------------
test.describe('CSRF token in team modal', () => {
    test('callAPI POST includes X-CSRF-Token header', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'csrf-conn', linked: false})],
        });
        await routeStatusOk(page, body);
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');
        await expect(page.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        let capturedHeaders: Record<string, string> = {};
        await page.route('**/plugins/crossguard/api/v1/teams/team1/init*', (route: any) => {
            capturedHeaders = route.request().headers();
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await setCsrfCookie(page);
        await page.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(() => {
            expect(capturedHeaders['x-csrf-token']).toBe('test-csrf-token');
        }).toPass();
    });

    test('handleClearRewrite DELETE includes X-CSRF-Token header', async ({mount, page}) => {
        await setCsrfCookie(page);
        const body = teamStatusResponse({
            connections: [connStatus({name: 'csrf-clear', direction: 'inbound', remote_team_name: 'some-team'})],
        });
        await routeStatusOk(page, body);
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');
        await expect(page.getByText('some-team')).toBeVisible();

        let capturedHeaders: Record<string, string> = {};
        await page.route('**/plugins/crossguard/api/v1/teams/team1/rewrite*', (route: any) => {
            capturedHeaders = route.request().headers();
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await page.getByRole('button', {name: 'Clear'}).click();
        await expect(() => {
            expect(capturedHeaders['x-csrf-token']).toBe('test-csrf-token');
        }).toPass();
    });
});

// ---------------------------------------------------------------------------
// 11. Full rewrite lifecycle
// ---------------------------------------------------------------------------
test.describe('Full rewrite lifecycle', () => {
    test('complete cycle: Set -> Save -> Edit -> Change -> Save -> Clear', async ({mount, page}) => {
        await setCsrfCookie(page);
        let fetchCount = 0;
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route: any) => {
            fetchCount++;
            let conns;
            if (fetchCount <= 1) {
                conns = [connStatus({name: 'lifecycle-conn', direction: 'inbound', remote_team_name: ''})];
            } else if (fetchCount <= 2) {
                conns = [connStatus({name: 'lifecycle-conn', direction: 'inbound', remote_team_name: 'first-value'})];
            } else if (fetchCount <= 3) {
                conns = [connStatus({name: 'lifecycle-conn', direction: 'inbound', remote_team_name: 'second-value'})];
            } else {
                conns = [connStatus({name: 'lifecycle-conn', direction: 'inbound', remote_team_name: ''})];
            }
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(teamStatusResponse({connections: conns}))});
        });
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite*', (route: any) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });

        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');

        // Step 1: Set initial value
        await expect(page.getByText('No remote team rewrite')).toBeVisible();
        await page.getByRole('button', {name: 'Set'}).click();
        await page.getByPlaceholder('Remote team name').fill('first-value');
        await page.getByRole('button', {name: 'Save'}).click();
        await expect(page.getByText('Remote team rewrite updated for "lifecycle-conn".')).toBeVisible();

        // Step 2: Edit and change the value
        await expect(page.getByRole('button', {name: 'Edit'})).toBeVisible();
        await page.getByRole('button', {name: 'Edit'}).click();
        const input = page.getByPlaceholder('Remote team name');
        await expect(input).toHaveValue('first-value');
        await input.fill('second-value');
        await page.getByRole('button', {name: 'Save'}).click();
        await expect(page.getByText('Remote team rewrite updated for "lifecycle-conn".')).toBeVisible();

        // Step 3: Clear the rewrite
        await expect(page.getByRole('button', {name: 'Clear'})).toBeVisible();
        await page.getByRole('button', {name: 'Clear'}).click();
        await expect(page.getByText('Remote team rewrite cleared for "lifecycle-conn".')).toBeVisible();
        await expect(page.getByText('No remote team rewrite')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 12. Missing response fields
// ---------------------------------------------------------------------------
test.describe('Missing response fields', () => {
    test('response without team_display_name renders modal without crash', async ({mount, page}) => {
        const responseBody = {team_id: 'team1', team_name: 'test-team', initialized: true, connections: [connStatus()]};
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route: any) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(responseBody)});
        });
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');

        // Modal should render connections without crashing even when team_display_name is missing
        await expect(page.getByText('my-conn')).toBeVisible();
    });

    test('response with empty connections array and no team_display_name renders empty state', async ({mount, page}) => {
        const responseBody = {team_id: 'team1', team_name: 'bare', initialized: true, connections: []};
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route: any) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(responseBody)});
        });
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');

        await expect(page.getByText('No connections available')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// Request approval flow
// ---------------------------------------------------------------------------
test.describe('Request approval flow edge cases', () => {
    test('request_pending on unlinked connection renders disabled Request Pending button', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'pending-conn', linked: false, request_pending: true})],
        });
        await mountAndOpen(page, mount, 'team1', body);

        const pendingBtn = page.getByRole('button', {name: 'Request Pending'});
        await expect(pendingBtn).toBeVisible();
        await expect(pendingBtn).toBeDisabled();
        await expect(page.getByRole('button', {name: 'Link', exact: true})).not.toBeVisible();
    });

    test('requestMode=true changes the unlinked link button label to Request Link', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'req-conn', linked: false})],
            request_mode: true,
        });
        await mountAndOpen(page, mount, 'team1', body);
        await expect(page.getByRole('button', {name: 'Request Link', exact: true})).toBeVisible();
    });

    test('toggle response status=request_submitted with message shows that message', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'sub-conn', linked: false})],
            request_mode: true,
        });
        await mountAndOpen(page, mount, 'team1', body);
        await page.route('**/plugins/crossguard/api/v1/teams/team1/init*', (route: any) => {
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({status: 'request_submitted', message: 'Request awaiting admin.'}),
            });
        });
        await setCsrfCookie(page);
        await page.getByRole('button', {name: 'Request Link', exact: true}).click();
        await expect(page.getByText('Request awaiting admin.')).toBeVisible();
    });

    test('toggle response status=request_submitted without message uses default text', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'sub-conn-2', linked: false})],
            request_mode: true,
        });
        await mountAndOpen(page, mount, 'team1', body);
        await page.route('**/plugins/crossguard/api/v1/teams/team1/init*', (route: any) => {
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({status: 'request_submitted'}),
            });
        });
        await setCsrfCookie(page);
        await page.getByRole('button', {name: 'Request Link', exact: true}).click();
        await expect(page.getByText('Your request has been submitted for approval.')).toBeVisible();
    });

    test('rapid toggle clears the previous status timer before scheduling a new one', async ({mount, page}) => {
        // Exercises the clearTimeout(statusTimerRef.current) branch in handleToggle
        // by firing two toggle actions back-to-back while the first banner is still showing.
        const body = teamStatusResponse({
            connections: [
                connStatus({name: 'first', linked: false}),
                connStatus({name: 'second', direction: 'outbound', linked: true}),
            ],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await page.route('**/plugins/crossguard/api/v1/teams/team1/init*', (route: any) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await page.route('**/plugins/crossguard/api/v1/teams/team1/teardown*', (route: any) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await setCsrfCookie(page);

        await page.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(page.getByText('Connection "first" linked.')).toBeVisible();

        await page.getByRole('button', {name: 'Unlink', exact: true}).click();
        await expect(page.getByText('Connection "second" unlinked.')).toBeVisible();
        await expect(page.getByText('Connection "first" linked.')).not.toBeVisible();
    });
});
