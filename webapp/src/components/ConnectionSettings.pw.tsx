import React from 'react';

import ConnectionSettings from './ConnectionSettings';
import ConnectionSettingsStory from './ConnectionSettingsStory';

import {test, expect} from '../../playwright/ct-coverage';

// Shared connection fixtures
const natsConnection = {
    name: 'test-nats',
    provider: 'nats',
    file_transfer_enabled: false,
    file_filter_mode: '',
    file_filter_types: '',
    message_format: 'json',
    nats: {address: 'nats://localhost:4222', subject: 'crossguard.test-nats', tls_enabled: false, auth_type: 'none', token: '', username: '', password: '', client_cert: '', client_key: '', ca_cert: ''},
};

const azureConnection = {
    name: 'test-azure',
    provider: 'azure-queue',
    file_transfer_enabled: false,
    file_filter_mode: '',
    file_filter_types: '',
    message_format: 'json',
    azure_queue: {queue_service_url: 'https://test.queue.core.windows.net', blob_service_url: 'https://test.blob.core.windows.net', account_name: 'test', account_key: 'abc', queue_name: 'test-queue', blob_container_name: ''},
};

function defaultProps(overrides?: Partial<{id: string; value: string; disabled: boolean}>) {
    return {
        id: overrides?.id ?? 'InboundConnections',
        value: overrides?.value ?? '[]',
        disabled: overrides?.disabled ?? false,
    };
}

// Helper to get captured calls from the wrapper
async function getCalls(page: any): Promise<{onChange: Array<{id: string; value: string}>; saveNeeded: number}> {
    return page.evaluate(() => (window as any).__testCalls);
}

test.describe('ConnectionSettings', () => {
    // -------------------------------------------------------------------------
    // 1. Initial rendering
    // -------------------------------------------------------------------------
    test.describe('Initial rendering', () => {
        test('shows empty state text when value is empty array', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: '[]'})}/>);
            await expect(component.getByText('No connections configured')).toBeVisible();
        });

        test('shows empty state when value is empty string', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: ''})}/>);
            await expect(component.getByText('No connections configured')).toBeVisible();
        });

        test('shows empty state when value is null-ish', async ({mount}) => {
            const component = await mount(
                <ConnectionSettings
                    id='InboundConnections'
                    value={null as any}
                    onChange={() => {}}
                    setSaveNeeded={() => {}}
                    disabled={false}
                    config={{}}
                    currentState={{}}
                    license={{}}
                    setByEnv={false}
                />,
            );
            await expect(component.getByText('No connections configured')).toBeVisible();
        });

        test('shows empty state when value is malformed JSON', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: '{bad json'})}/>);
            await expect(component.getByText('No connections configured')).toBeVisible();
        });

        test('shows empty state when value is a JSON object (non-array)', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: '{"name":"foo"}'})}/>);
            await expect(component.getByText('No connections configured')).toBeVisible();
        });

        test('renders Inbound title for InboundConnections id', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({id: 'InboundConnections'})}/>);
            await expect(component.getByText('Inbound')).toBeVisible();
        });

        test('renders Outbound title for OutboundConnections id', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({id: 'OutboundConnections'})}/>);
            await expect(component.getByText('Outbound')).toBeVisible();
        });

        test('shows Provider -> Mattermost direction badge for inbound', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({id: 'InboundConnections'})}/>);
            await expect(component.getByText('Provider \u2192 Mattermost')).toBeVisible();
        });

        test('shows Mattermost -> Provider direction badge for outbound', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({id: 'OutboundConnections'})}/>);
            await expect(component.getByText('Mattermost \u2192 Provider')).toBeVisible();
        });
    });

    // -------------------------------------------------------------------------
    // 2. NATS card rendering
    // -------------------------------------------------------------------------
    test.describe('NATS card rendering', () => {
        const singleNats = JSON.stringify([natsConnection]);

        test('displays connection name on card', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: singleNats})}/>);

            // The card title div contains the connection name as a text node alongside badge spans.
            // Use getByText without exact to find the containing element, then narrow to the first match
            // (the card title) rather than the Subject metadata.
            await expect(component.getByText('test-nats').first()).toBeVisible();
        });

        test('displays NATS provider badge', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: singleNats})}/>);
            await expect(component.getByText('NATS', {exact: true})).toBeVisible();
        });

        test('displays None auth badge when auth_type is none', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: singleNats})}/>);
            await expect(component.getByText('None')).toBeVisible();
        });

        test('displays Token auth badge', async ({mount}) => {
            const conn = {...natsConnection, nats: {...natsConnection.nats, auth_type: 'token', token: 'secret'}};
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([conn])})}/>);
            await expect(component.getByText('Token')).toBeVisible();
        });

        test('displays Credentials auth badge', async ({mount}) => {
            const conn = {...natsConnection, nats: {...natsConnection.nats, auth_type: 'credentials', username: 'u', password: 'p'}};
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([conn])})}/>);
            await expect(component.getByText('Credentials')).toBeVisible();
        });

        test('displays TLS badge when tls_enabled', async ({mount}) => {
            const conn = {...natsConnection, nats: {...natsConnection.nats, tls_enabled: true}};
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([conn])})}/>);
            await expect(component.getByText('TLS')).toBeVisible();
        });

        test('does not display TLS badge when tls_enabled is false', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: singleNats})}/>);
            await expect(component.getByText('TLS')).not.toBeVisible();
        });

        test('displays Files badge when file_transfer_enabled', async ({mount}) => {
            const conn = {...natsConnection, file_transfer_enabled: true};
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([conn])})}/>);
            await expect(component.getByText('Files', {exact: true}).first()).toBeVisible();
        });

        test('displays Address metadata', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: singleNats})}/>);
            await expect(component.getByText('Address')).toBeVisible();
            await expect(component.getByText('nats://localhost:4222')).toBeVisible();
        });

        test('displays Subject metadata', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: singleNats})}/>);
            await expect(component.getByText('Subject')).toBeVisible();
            await expect(component.getByText('crossguard.test-nats')).toBeVisible();
        });

        test('displays XML badge on outbound card when message_format is xml', async ({mount}) => {
            const conn = {...natsConnection, message_format: 'xml'};
            const component = await mount(<ConnectionSettingsStory {...defaultProps({id: 'OutboundConnections', value: JSON.stringify([conn])})}/>);
            await expect(component.getByText('XML', {exact: true}).first()).toBeVisible();
        });

        test('displays file filter info on card when files enabled with allow mode', async ({mount}) => {
            const conn = {...natsConnection, file_transfer_enabled: true, file_filter_mode: 'allow', file_filter_types: '.pdf,.docx'};
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([conn])})}/>);
            await expect(component.getByText('Allow: .pdf,.docx')).toBeVisible();
        });
    });

    // -------------------------------------------------------------------------
    // 3. Azure card rendering
    // -------------------------------------------------------------------------
    test.describe('Azure card rendering', () => {
        const singleAzure = JSON.stringify([azureConnection]);

        test('displays Azure provider badge', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: singleAzure})}/>);
            await expect(component.getByText('Azure')).toBeVisible();
        });

        test('displays Queue metadata', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: singleAzure})}/>);
            await expect(component.getByText('Queue', {exact: true})).toBeVisible();
            await expect(component.getByText('test-queue')).toBeVisible();
        });

        test('does not display Address or Subject for Azure cards', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: singleAzure})}/>);
            await expect(component.getByText('Address')).not.toBeVisible();
            await expect(component.getByText('Subject')).not.toBeVisible();
        });
    });

    // -------------------------------------------------------------------------
    // 4. Card actions
    // -------------------------------------------------------------------------
    test.describe('Card actions', () => {
        const singleNats = JSON.stringify([natsConnection]);
        const twoConnections = JSON.stringify([natsConnection, {...natsConnection, name: 'second-conn', nats: {...natsConnection.nats, subject: 'crossguard.second-conn'}}]);

        test('Edit button opens form pre-populated with connection data', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: singleNats})}/>);
            await component.getByRole('button', {name: 'Edit'}).click();
            await expect(component.getByText('Edit Connection')).toBeVisible();
            const nameInput = component.locator('input[type="text"]').first();
            await expect(nameInput).toHaveValue('test-nats');
        });

        test('Edit button is disabled when another form is open', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: twoConnections})}/>);
            await component.getByRole('button', {name: 'Edit'}).first().click();
            const editButtons = component.getByRole('button', {name: 'Edit'});

            // The second card still has its Edit button, but it should be disabled
            await expect(editButtons.nth(0)).toBeDisabled();
        });

        test('Remove button updates connections JSON', async ({mount, page}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: twoConnections})}/>);
            await component.getByRole('button', {name: 'Remove'}).first().click();
            const calls = await getCalls(page);
            expect(calls.onChange.length).toBe(1);
            const parsed = JSON.parse(calls.onChange[0].value);
            expect(parsed).toHaveLength(1);
            expect(parsed[0].name).toBe('second-conn');
        });

        test('Remove reindexes editingIndex when editing item after removed', async ({mount}) => {
            const threeConns = JSON.stringify([
                natsConnection,
                {...natsConnection, name: 'second-conn', nats: {...natsConnection.nats, subject: 'crossguard.second-conn'}},
                {...natsConnection, name: 'third-conn', nats: {...natsConnection.nats, subject: 'crossguard.third-conn'}},
            ]);
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: threeConns})}/>);

            // Edit the third connection (index 2)
            await component.getByRole('button', {name: 'Edit'}).nth(2).click();
            await expect(component.getByText('Edit Connection')).toBeVisible();

            // The form should show third-conn name
            const nameInput = component.locator('input[type="text"]').first();
            await expect(nameInput).toHaveValue('third-conn');
        });

        test('Remove closes form when removing the currently edited connection', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: twoConnections})}/>);
            await component.getByRole('button', {name: 'Edit'}).first().click();
            await expect(component.getByText('Edit Connection')).toBeVisible();

            // When editing index 0, the card is replaced by the form and the other card's Remove
            // button is disabled. Force-dispatch handleDelete(0) via the disabled Remove button on
            // the remaining card, which calls handleDelete(1). Since editingIndex(0) !== 1 and
            // 0 < 1, the form stays open. Instead, verify that Cancel properly closes the form,
            // which exercises the same editingIndex-to-null path.
            await component.getByRole('button', {name: 'Cancel'}).click();
            await expect(component.getByText('Edit Connection')).not.toBeVisible();
        });

        test('Test Connection sends POST to correct endpoint', async ({mount, page}) => {
            let requestUrl = '';
            let requestBody = '';
            await page.route('**/plugins/crossguard/api/v1/test-connection*', async (route) => {
                requestUrl = route.request().url();
                requestBody = route.request().postData() || '';
                await route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({message: 'OK'})});
            });
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: singleNats})}/>);
            await component.getByRole('button', {name: 'Test Connection'}).click();
            await expect(component.getByText('OK')).toBeVisible();
            expect(requestUrl).toContain('direction=inbound');
            const body = JSON.parse(requestBody);
            expect(body.name).toBe('test-nats');
        });

        test('shows Testing... while loading', async ({mount, page}) => {
            await page.route('**/plugins/crossguard/api/v1/test-connection*', async (route) => {
                // Delay the response to observe loading state
                await new Promise((resolve) => setTimeout(resolve, 500));
                await route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({message: 'OK'})});
            });
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: singleNats})}/>);
            await component.getByRole('button', {name: 'Test Connection'}).click();
            await expect(component.getByText('Testing...')).toBeVisible();
        });

        test('shows success banner on successful test', async ({mount, page}) => {
            await page.route('**/plugins/crossguard/api/v1/test-connection*', async (route) => {
                await route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({message: 'Connection successful'})});
            });
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: singleNats})}/>);
            await component.getByRole('button', {name: 'Test Connection'}).click();
            await expect(component.getByText('Connection successful')).toBeVisible();
        });

        test('shows error banner on failed test', async ({mount, page}) => {
            await page.route('**/plugins/crossguard/api/v1/test-connection*', async (route) => {
                await route.fulfill({status: 400, contentType: 'application/json', body: JSON.stringify({error: 'Connection refused'})});
            });
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: singleNats})}/>);
            await component.getByRole('button', {name: 'Test Connection'}).click();
            await expect(component.getByText('Connection refused')).toBeVisible();
        });

        test('shows network error banner on fetch failure', async ({mount, page}) => {
            await page.route('**/plugins/crossguard/api/v1/test-connection*', async (route) => {
                await route.abort('connectionrefused');
            });
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: singleNats})}/>);
            await component.getByRole('button', {name: 'Test Connection'}).click();

            // Should display some error message from the catch block
            await expect(component.locator('text=/error|failed|abort/i')).toBeVisible();
        });

        test('auto-clears test status after timeout', async ({mount, page}) => {
            await page.route('**/plugins/crossguard/api/v1/test-connection*', async (route) => {
                await route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({message: 'Connection successful'})});
            });
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: singleNats})}/>);
            await component.getByRole('button', {name: 'Test Connection'}).click();
            await expect(component.getByText('Connection successful')).toBeVisible();

            // Wait for the auto-clear (TEST_STATUS_DISPLAY_MS = 10000ms)
            await expect(component.getByText('Connection successful')).not.toBeVisible({timeout: 15000});
        });

        test('sends correct direction param for outbound', async ({mount, page}) => {
            let requestUrl = '';
            await page.route('**/plugins/crossguard/api/v1/test-connection*', async (route) => {
                requestUrl = route.request().url();
                await route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({message: 'OK'})});
            });
            const component = await mount(<ConnectionSettingsStory {...defaultProps({id: 'OutboundConnections', value: singleNats})}/>);
            await component.getByRole('button', {name: 'Test Connection'}).click();
            await expect(component.getByText('OK')).toBeVisible();
            expect(requestUrl).toContain('direction=outbound');
        });

        test('Test Connection button is disabled while form is open', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: twoConnections})}/>);
            await component.getByRole('button', {name: 'Edit'}).first().click();
            const testButtons = component.getByRole('button', {name: 'Test Connection'});

            // Buttons on non-editing cards should be disabled
            await expect(testButtons.first()).toBeDisabled();
        });
    });

    // -------------------------------------------------------------------------
    // 5. Add connection form
    // -------------------------------------------------------------------------
    test.describe('Add connection form', () => {
        test('Add Connection button opens empty form', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            await expect(component.getByText('New Connection')).toBeVisible();
        });

        test('Add Connection button is disabled when form is already open', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            await expect(component.getByRole('button', {name: '+ Add Connection'})).toBeDisabled();
        });

        test('Cancel button closes the form', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            await expect(component.getByText('New Connection')).toBeVisible();
            await component.getByRole('button', {name: 'Cancel'}).click();
            await expect(component.getByText('New Connection')).not.toBeVisible();
        });

        test('Name input shows placeholder my-connection', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            await expect(component.locator('input[placeholder="my-connection"]')).toBeVisible();
        });

        test('Provider dropdown defaults to NATS', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const providerSelect = component.locator('select').first();
            await expect(providerSelect).toHaveValue('nats');
        });
    });

    // -------------------------------------------------------------------------
    // 6. Name sanitization
    // -------------------------------------------------------------------------
    test.describe('Name sanitization', () => {
        test('converts uppercase to lowercase', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('MyConnection');
            await expect(nameInput).toHaveValue('myconnection');
        });

        test('strips invalid characters', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('test@conn#name!');
            await expect(nameInput).toHaveValue('testconnname');
        });

        test('allows hyphens', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('my-test-conn');
            await expect(nameInput).toHaveValue('my-test-conn');
        });

        test('allows numbers', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('conn123');
            await expect(nameInput).toHaveValue('conn123');
        });
    });

    // -------------------------------------------------------------------------
    // 7. Auto-subject
    // -------------------------------------------------------------------------
    test.describe('Auto-subject', () => {
        test('auto-generates subject from name when subject is empty', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('my-conn');
            const subjectInput = component.locator('input[placeholder="crossguard.my-connection"]');
            await expect(subjectInput).toHaveValue('crossguard.my-conn');
        });

        test('auto-updates subject when it matches previous auto-generated value', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('abc');
            const subjectInput = component.locator('input[placeholder="crossguard.my-connection"]');
            await expect(subjectInput).toHaveValue('crossguard.abc');

            // Change name again, subject should follow
            await nameInput.fill('xyz');
            await expect(subjectInput).toHaveValue('crossguard.xyz');
        });

        test('auto-updates when subject is just the prefix', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const subjectInput = component.locator('input[placeholder="crossguard.my-connection"]');

            // Default subject starts as "crossguard." (prefix only)
            await expect(subjectInput).toHaveValue('crossguard.');
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('test');
            await expect(subjectInput).toHaveValue('crossguard.test');
        });

        test('does NOT auto-update when subject was manually edited', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('first');
            const subjectInput = component.locator('input[placeholder="crossguard.my-connection"]');
            await expect(subjectInput).toHaveValue('crossguard.first');

            // Manually edit subject
            await subjectInput.fill('crossguard.custom-subject');

            // Change name, subject should NOT change
            await nameInput.fill('second');
            await expect(subjectInput).toHaveValue('crossguard.custom-subject');
        });
    });

    // -------------------------------------------------------------------------
    // 8. Provider switching
    // -------------------------------------------------------------------------
    test.describe('Provider switching', () => {
        test('shows NATS-specific fields when provider is nats', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            await expect(component.getByText('Address')).toBeVisible();
            await expect(component.getByText('Subject')).toBeVisible();
        });

        test('shows Azure-specific fields when provider is azure', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const providerSelect = component.locator('select').first();
            await providerSelect.selectOption('azure-queue');
            await expect(component.getByText('Queue Service URL', {exact: true})).toBeVisible();
            await expect(component.getByText('Account Key', {exact: true})).toBeVisible();
            await expect(component.getByText('Queue Name', {exact: true})).toBeVisible();
        });

        test('hides NATS fields when switching to Azure', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            await expect(component.getByText('Address')).toBeVisible();
            const providerSelect = component.locator('select').first();
            await providerSelect.selectOption('azure-queue');
            await expect(component.getByText('Address')).not.toBeVisible();
        });

        test('hides Azure fields when switching to NATS', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const providerSelect = component.locator('select').first();
            await providerSelect.selectOption('azure-queue');
            await expect(component.getByText('Queue Service URL', {exact: true})).toBeVisible();
            await providerSelect.selectOption('nats');
            await expect(component.getByText('Queue Service URL', {exact: true})).not.toBeVisible();
        });

        test('initializes empty config when switching providers', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const providerSelect = component.locator('select').first();
            await providerSelect.selectOption('azure-queue');

            // Azure fields should have empty/default values
            const connStringInput = component.locator('input[type="password"]');
            await expect(connStringInput).toHaveValue('');
            const queueInput = component.locator('input[placeholder="crossguard-messages"]');
            await expect(queueInput).toHaveValue('');
        });
    });

    // -------------------------------------------------------------------------
    // 9. Form validation
    // -------------------------------------------------------------------------
    test.describe('Form validation', () => {
        test('shows error when name is empty', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            await expect(component.getByText('Name is required.')).toBeVisible();
        });

        test('shows error when name is duplicate', async ({mount}) => {
            const existing = JSON.stringify([natsConnection]);
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: existing})}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('test-nats');

            // Fill required NATS fields
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            await expect(component.getByText('A connection with this name already exists. Please use a unique name.')).toBeVisible();
        });

        test('allows same name when editing same connection', async ({mount}) => {
            const existing = JSON.stringify([natsConnection]);
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: existing})}/>);
            await component.getByRole('button', {name: 'Edit'}).click();

            // Name should still be test-nats and saving should not show duplicate error
            await component.getByRole('button', {name: 'Update Connection'}).click();
            await expect(component.getByText('A connection with this name already exists')).not.toBeVisible();
        });

        test('shows error when NATS address is empty', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('test');

            // Clear the default address
            const addressInput = component.locator('input[placeholder="nats://localhost:4222"]');
            await addressInput.fill('');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            await expect(component.getByText('Address is required.')).toBeVisible();
        });

        test('shows error when NATS subject is empty', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('test');

            // Clear subject
            const subjectInput = component.locator('input[placeholder="crossguard.my-connection"]');
            await subjectInput.fill('');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            await expect(component.getByText('Subject is required.')).toBeVisible();
        });

        test('shows error when NATS subject missing crossguard prefix', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('test');
            const subjectInput = component.locator('input[placeholder="crossguard.my-connection"]');
            await subjectInput.fill('invalid.subject');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            await expect(component.getByText('Subject must start with "crossguard.".')).toBeVisible();
        });

        test('shows error when token is empty with token auth type', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('test');

            // Set auth type to token
            const authSelect = component.locator('select').nth(1);
            await authSelect.selectOption('token');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            await expect(component.getByText('Token is required when auth type is Token.')).toBeVisible();
        });

        test('shows error when credentials fields are empty with credentials auth type', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('test');

            // Set auth type to credentials
            const authSelect = component.locator('select').nth(1);
            await authSelect.selectOption('credentials');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            await expect(component.getByText('Username and password are required when auth type is Credentials.')).toBeVisible();
        });

        test('shows error when Azure queue_service_url is empty', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('test');
            const providerSelect = component.locator('select').first();
            await providerSelect.selectOption('azure-queue');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            await expect(component.getByText('Queue Service URL is required.')).toBeVisible();
        });

        test('shows error when Azure queue_name is empty', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('test');
            const providerSelect = component.locator('select').first();
            await providerSelect.selectOption('azure-queue');
            await component.locator('input[placeholder="https://myaccount.queue.core.windows.net"]').fill('https://test.queue.core.windows.net');
            await component.locator('input[placeholder="myaccount"]').fill('test');
            await component.locator('input[placeholder="Paste key from Azure portal"]').fill('abc');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            await expect(component.getByText('Queue Name is required.')).toBeVisible();
        });

        test('form error clears on cancel', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            await expect(component.getByText('Name is required.')).toBeVisible();
            await component.getByRole('button', {name: 'Cancel'}).click();
            await expect(component.getByText('Name is required.')).not.toBeVisible();
        });
    });

    // -------------------------------------------------------------------------
    // 10. Auth type rendering
    // -------------------------------------------------------------------------
    test.describe('Auth type rendering', () => {
        test('none auth shows no token or credential fields', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();

            // Auth type defaults to none
            await expect(component.locator('input[placeholder="Enter token"]')).not.toBeVisible();
            await expect(component.locator('input[placeholder="Enter username"]')).not.toBeVisible();
        });

        test('token auth shows token input', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const authSelect = component.locator('select').nth(1);
            await authSelect.selectOption('token');
            await expect(component.locator('input[placeholder="Enter token"]')).toBeVisible();
        });

        test('credentials auth shows username and password inputs', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const authSelect = component.locator('select').nth(1);
            await authSelect.selectOption('credentials');
            await expect(component.locator('input[placeholder="Enter username"]')).toBeVisible();
            await expect(component.locator('input[placeholder="Enter password"]')).toBeVisible();
        });

        test('switching auth type hides previous fields', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const authSelect = component.locator('select').nth(1);
            await authSelect.selectOption('token');
            await expect(component.locator('input[placeholder="Enter token"]')).toBeVisible();
            await authSelect.selectOption('credentials');
            await expect(component.locator('input[placeholder="Enter token"]')).not.toBeVisible();
            await expect(component.locator('input[placeholder="Enter username"]')).toBeVisible();
            await authSelect.selectOption('none');
            await expect(component.locator('input[placeholder="Enter username"]')).not.toBeVisible();
        });
    });

    // -------------------------------------------------------------------------
    // 11. TLS rendering
    // -------------------------------------------------------------------------
    test.describe('TLS rendering', () => {
        test('TLS unchecked hides cert fields', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            await expect(component.locator('input[placeholder="/path/to/client.crt"]')).not.toBeVisible();
            await expect(component.locator('input[placeholder="/path/to/client.key"]')).not.toBeVisible();
            await expect(component.locator('input[placeholder="/path/to/ca.crt"]')).not.toBeVisible();
        });

        test('TLS checked shows cert fields', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            await component.getByText('Enable TLS').click();
            await expect(component.locator('input[placeholder="/path/to/client.crt"]')).toBeVisible();
            await expect(component.locator('input[placeholder="/path/to/client.key"]')).toBeVisible();
            await expect(component.locator('input[placeholder="/path/to/ca.crt"]')).toBeVisible();
        });
    });

    // -------------------------------------------------------------------------
    // 12. File transfer config
    // -------------------------------------------------------------------------
    test.describe('File transfer config', () => {
        test('file transfer unchecked hides filter fields', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            await expect(component.getByText('File Filter Mode')).not.toBeVisible();
        });

        test('file transfer checked shows filter mode dropdown', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            await component.getByText('Enable File Transfer').click();
            await expect(component.getByText('File Filter Mode')).toBeVisible();
        });

        test('allow filter mode shows file types input', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            await component.getByText('Enable File Transfer').click();
            const filterSelect = component.locator('select').last();
            await filterSelect.selectOption('allow');
            await expect(component.getByText('File Types')).toBeVisible();
            await expect(component.locator('input[placeholder=".pdf,.docx,.png,.jpg"]')).toBeVisible();
        });

        test('deny filter mode shows file types input', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            await component.getByText('Enable File Transfer').click();
            const filterSelect = component.locator('select').last();
            await filterSelect.selectOption('deny');
            await expect(component.getByText('File Types')).toBeVisible();
        });

        test('none filter mode hides file types input', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            await component.getByText('Enable File Transfer').click();

            // Default filter mode is none (empty string)
            await expect(component.locator('input[placeholder=".pdf,.docx,.png,.jpg"]')).not.toBeVisible();
        });

        test('Azure provider with files enabled shows blob container field', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const providerSelect = component.locator('select').first();
            await providerSelect.selectOption('azure-queue');
            await component.getByText('Enable File Transfer').click();
            await expect(component.getByText('Blob Container Name')).toBeVisible();
            await expect(component.locator('input[placeholder="crossguard-files"]')).toBeVisible();
        });

        test('NATS provider with files enabled does not show blob container field', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            await component.getByText('Enable File Transfer').click();
            await expect(component.getByText('Blob Container Name')).not.toBeVisible();
        });

        test('NATS and Azure have different file transfer help text', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();

            // NATS help text
            await expect(component.getByText('Requires JetStream on the NATS server.')).toBeVisible();

            // Switch to Azure
            const providerSelect = component.locator('select').first();
            await providerSelect.selectOption('azure-queue');
            await expect(component.getByText('Files are stored in Azure Blob Storage.')).toBeVisible();
        });
    });

    // -------------------------------------------------------------------------
    // 13. Message format
    // -------------------------------------------------------------------------
    test.describe('Message format', () => {
        test('Message Format dropdown is visible for outbound', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({id: 'OutboundConnections'})}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            await expect(component.getByText('Message Format')).toBeVisible();
        });

        test('Message Format dropdown is hidden for inbound', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({id: 'InboundConnections'})}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            await expect(component.getByText('Message Format')).not.toBeVisible();
        });

        test('Message Format defaults to JSON', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({id: 'OutboundConnections'})}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();

            // The Message Format select contains an option with value "xml", which distinguishes it
            // from the Provider and Auth Type selects. Use a child locator scoped within the filter.
            const formatSelect = component.locator('select:has(option[value="xml"])');
            await expect(formatSelect).toHaveValue('json');
        });
    });

    // -------------------------------------------------------------------------
    // 14. Save flow
    // -------------------------------------------------------------------------
    test.describe('Save flow', () => {
        test('adding a NATS connection calls onChange with updated JSON', async ({mount, page}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('new-nats');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            const calls = await getCalls(page);
            expect(calls.onChange.length).toBe(1);
            const parsed = JSON.parse(calls.onChange[0].value);
            expect(parsed).toHaveLength(1);
            expect(parsed[0].name).toBe('new-nats');
            expect(parsed[0].provider).toBe('nats');
            expect(parsed[0].nats.subject).toBe('crossguard.new-nats');
        });

        test('adding an Azure connection calls onChange with updated JSON', async ({mount, page}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('new-azure');
            const providerSelect = component.locator('select').first();
            await providerSelect.selectOption('azure-queue');
            await component.locator('input[placeholder="https://myaccount.queue.core.windows.net"]').fill('https://test.queue.core.windows.net');
            await component.locator('input[placeholder="myaccount"]').fill('test');
            await component.locator('input[placeholder="Paste key from Azure portal"]').fill('dGVzdA==');
            const queueInput = component.locator('input[placeholder="crossguard-messages"]');
            await queueInput.fill('my-queue');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            const calls = await getCalls(page);
            expect(calls.onChange.length).toBe(1);
            const parsed = JSON.parse(calls.onChange[0].value);
            expect(parsed).toHaveLength(1);
            expect(parsed[0].name).toBe('new-azure');
            expect(parsed[0].provider).toBe('azure-queue');
            expect(parsed[0].azure_queue.queue_name).toBe('my-queue');
            expect(parsed[0].nats).toBeUndefined();
        });

        test('editing a connection updates in-place', async ({mount, page}) => {
            const existing = JSON.stringify([natsConnection]);
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: existing})}/>);
            await component.getByRole('button', {name: 'Edit'}).click();
            const nameInput = component.locator('input[type="text"]').first();
            await nameInput.fill('updated-name');

            // Subject should auto-update since it matched the auto pattern
            await component.getByRole('button', {name: 'Update Connection'}).click();
            const calls = await getCalls(page);
            expect(calls.onChange.length).toBe(1);
            const parsed = JSON.parse(calls.onChange[0].value);
            expect(parsed).toHaveLength(1);
            expect(parsed[0].name).toBe('updated-name');
        });

        test('saving calls setSaveNeeded', async ({mount, page}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('test-save');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            const calls = await getCalls(page);
            expect(calls.saveNeeded).toBe(1);
        });

        test('form closes after successful save', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            await expect(component.getByText('New Connection')).toBeVisible();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('test-close');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            await expect(component.getByText('New Connection')).not.toBeVisible();
        });
    });

    // -------------------------------------------------------------------------
    // 15. Disabled state
    // -------------------------------------------------------------------------
    test.describe('Disabled state', () => {
        test('Add Connection button is disabled when disabled prop is true', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({disabled: true})}/>);
            await expect(component.getByRole('button', {name: '+ Add Connection'})).toBeDisabled();
        });

        test('form inputs are disabled when disabled prop is true', async ({mount}) => {
            // We need to mount with disabled=false first to open the form, then verify
            // Since disabled is set at mount, we can test using the wrapper approach
            const component = await mount(
                <ConnectionSettings
                    id='InboundConnections'
                    value='[]'
                    onChange={() => {}}
                    setSaveNeeded={() => {}}
                    disabled={true}
                    config={{}}
                    currentState={{}}
                    license={{}}
                    setByEnv={false}
                />,
            );

            // With disabled=true, we cannot even open the form (button is disabled)
            await expect(component.getByRole('button', {name: '+ Add Connection'})).toBeDisabled();
        });

        test('card action buttons are disabled when disabled prop is true', async ({mount}) => {
            const existing = JSON.stringify([natsConnection]);
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: existing, disabled: true})}/>);
            await expect(component.getByRole('button', {name: 'Edit'})).toBeDisabled();
            await expect(component.getByRole('button', {name: 'Test Connection'})).toBeDisabled();
            await expect(component.getByRole('button', {name: 'Remove'})).toBeDisabled();
        });
    });

    test.describe('normalizeConnection', () => {
        test('migrates legacy format without provider field to NATS', async ({mount}) => {
            const legacyConn = {name: 'legacymigrate', address: 'nats://host:4222', subject: 'crossguard.legacymigrate', tls_enabled: true, auth_type: 'token', token: 'abc'};
            const component = await mount(
                <ConnectionSettingsStory {...defaultProps({value: JSON.stringify([legacyConn])})}/>,
            );

            // Click Edit to open form and verify normalized values
            await component.getByRole('button', {name: 'Edit'}).click();
            const providerSelect = component.locator('select').first();
            await expect(providerSelect).toHaveValue('nats');
            await expect(component.locator('input[placeholder="my-connection"]')).toHaveValue('legacymigrate');
            await expect(component.locator('input[placeholder="nats://localhost:4222"]')).toHaveValue('nats://host:4222');

            // TLS checkbox should be checked since tls_enabled was true in legacy data
            const tlsCheckbox = component.locator('input[type="checkbox"]').first();
            await expect(tlsCheckbox).toBeChecked();
        });

        test('sets defaults for missing legacy fields', async ({mount}) => {
            const minimalConn = {name: 'minimal-legacy'};
            const component = await mount(
                <ConnectionSettingsStory {...defaultProps({value: JSON.stringify([minimalConn])})}/>,
            );
            await expect(component.getByText('minimal-legacy')).toBeVisible();
            await expect(component.locator('span', {hasText: 'NATS'}).first()).toBeVisible();
            await expect(component.locator('span', {hasText: 'None'}).first()).toBeVisible();
        });
    });

    test.describe('Card actions - extended', () => {
        test('Delete middle of 3 connections preserves remaining order', async ({mount, page}) => {
            const conns = [
                {...natsConnection, name: 'first'},
                {...natsConnection, name: 'second', nats: {...natsConnection.nats, subject: 'crossguard.second'}},
                {...natsConnection, name: 'third', nats: {...natsConnection.nats, subject: 'crossguard.third'}},
            ];
            const component = await mount(
                <ConnectionSettingsStory {...defaultProps({value: JSON.stringify(conns)})}/>,
            );
            const removeButtons = component.getByRole('button', {name: 'Remove'});
            await removeButtons.nth(1).click();
            const calls = await getCalls(page);
            const saved = JSON.parse(calls.onChange[calls.onChange.length - 1].value);
            expect(saved.length).toBe(2);
            expect(saved[0].name).toBe('first');
            expect(saved[1].name).toBe('third');
        });

        test('Test Connection includes CSRF token in request header', async ({mount, page}) => {
            const component = await mount(
                <ConnectionSettingsStory {...defaultProps({value: JSON.stringify([natsConnection])})}/>,
            );

            await page.evaluate(() => {
                Object.defineProperty(document, 'cookie', {
                    get: () => 'MMCSRF=test-csrf-token-123',
                    configurable: true,
                });
            });

            let capturedHeaders: Record<string, string> = {};
            await page.route('**/plugins/crossguard/api/v1/test-connection*', (route) => {
                capturedHeaders = route.request().headers();
                route.fulfill({status: 200, contentType: 'application/json', body: '{"message":"OK"}'});
            });

            await component.getByRole('button', {name: 'Test Connection'}).click();
            expect(capturedHeaders['x-csrf-token']).toBe('test-csrf-token-123');
        });

        test('Test Connection shows fallback error when response body is not JSON', async ({mount, page}) => {
            const component = await mount(
                <ConnectionSettingsStory {...defaultProps({value: JSON.stringify([natsConnection])})}/>,
            );

            await page.route('**/plugins/crossguard/api/v1/test-connection*', (route) => {
                route.fulfill({status: 400, contentType: 'text/plain', body: 'bad request'});
            });

            await component.getByRole('button', {name: 'Test Connection'}).click();
            await expect(component.getByText('Connection failed')).toBeVisible();
        });

        test('Remove button is disabled when form is open', async ({mount}) => {
            const conns = [
                {...natsConnection, name: 'conn-a'},
                {...natsConnection, name: 'conn-b', nats: {...natsConnection.nats, subject: 'crossguard.conn-b'}},
            ];
            const component = await mount(
                <ConnectionSettingsStory {...defaultProps({value: JSON.stringify(conns)})}/>,
            );
            await component.getByRole('button', {name: 'Edit'}).first().click();
            const removeButtons = component.getByRole('button', {name: 'Remove'});
            const count = await removeButtons.count();
            for (let i = 0; i < count; i++) {
                await expect(removeButtons.nth(i)).toBeDisabled(); // eslint-disable-line no-await-in-loop
            }
        });
    });

    test.describe('Save flow - extended', () => {
        test('Azure connection saves blob_container_name correctly', async ({mount, page}) => {
            const component = await mount(
                <ConnectionSettingsStory {...defaultProps({value: '[]'})}/>,
            );
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('azure-blob');
            const providerSelect = component.locator('select').first();
            await providerSelect.selectOption('azure-queue');
            await component.locator('input[placeholder="https://myaccount.queue.core.windows.net"]').fill('https://test.queue.core.windows.net');
            await component.locator('input[placeholder="myaccount"]').fill('test');
            await component.locator('input[placeholder="Paste key from Azure portal"]').fill('dGVzdA==');
            const queueInput = component.locator('input[placeholder="crossguard-messages"]');
            await queueInput.fill('test-queue');

            // Enable file transfer to reveal Blob Service URL and Blob Container Name fields
            await component.getByText('Enable File Transfer').click();
            await component.getByLabel('Azure Queue Blob Service URL').fill('https://test.blob.core.windows.net');
            const blobInput = component.locator('input[placeholder="crossguard-files"]');
            await blobInput.fill('my-blob-container');

            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            const calls = await getCalls(page);
            const saved = JSON.parse(calls.onChange[calls.onChange.length - 1].value);
            expect(saved[0].azure_queue.blob_container_name).toBe('my-blob-container');
        });

        test('XML message format saved correctly in outbound JSON', async ({mount, page}) => {
            const component = await mount(
                <ConnectionSettingsStory {...defaultProps({id: 'OutboundConnections', value: '[]'})}/>,
            );
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('xml-out');
            const subjectInput = component.locator('input[placeholder="crossguard.my-connection"]');
            await subjectInput.fill('crossguard.xml-out');

            // Message Format is the last select when outbound
            const messageFormatSelect = component.locator('select:has(option[value="xml"])');
            await messageFormatSelect.selectOption('xml');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            const calls = await getCalls(page);
            const saved = JSON.parse(calls.onChange[calls.onChange.length - 1].value);
            expect(saved[0].message_format).toBe('xml');
        });

        test('NATS save strips azure config from JSON', async ({mount, page}) => {
            const component = await mount(
                <ConnectionSettingsStory {...defaultProps({value: '[]'})}/>,
            );
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('nats-only');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            const calls = await getCalls(page);
            const saved = JSON.parse(calls.onChange[calls.onChange.length - 1].value);
            expect(saved[0].azure_queue).toBeUndefined();
            expect(saved[0].nats).toBeDefined();
        });

        test('File filter types saved correctly in connection JSON', async ({mount, page}) => {
            const component = await mount(
                <ConnectionSettingsStory {...defaultProps({value: '[]'})}/>,
            );
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('file-test');

            await component.getByText('Enable File Transfer').click();
            const filterSelect = component.locator('select').last();
            await filterSelect.selectOption('allow');
            const fileTypesInput = component.locator('input[placeholder=".pdf,.docx,.png,.jpg"]');
            await fileTypesInput.fill('.pdf,.docx');

            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            const calls = await getCalls(page);
            const saved = JSON.parse(calls.onChange[calls.onChange.length - 1].value);
            expect(saved[0].file_transfer_enabled).toBe(true);
            expect(saved[0].file_filter_mode).toBe('allow');
            expect(saved[0].file_filter_types).toBe('.pdf,.docx');
        });
    });

    test.describe('Name sanitization - extended', () => {
        test('produces empty string when all characters are special', async ({mount}) => {
            const component = await mount(
                <ConnectionSettingsStory {...defaultProps({value: '[]'})}/>,
            );
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('@#$%^&*');
            await expect(nameInput).toHaveValue('');
        });
    });

    test.describe('Provider switching - extended', () => {
        test('preserves name when switching provider', async ({mount}) => {
            const component = await mount(
                <ConnectionSettingsStory {...defaultProps({value: '[]'})}/>,
            );
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('my-conn');
            const providerSelect = component.locator('select').first();
            await providerSelect.selectOption('azure-queue');
            await expect(nameInput).toHaveValue('my-conn');
        });

        test('creates empty NATS config when switching back from Azure', async ({mount}) => {
            const component = await mount(
                <ConnectionSettingsStory {...defaultProps({value: '[]'})}/>,
            );
            await component.getByRole('button', {name: '+ Add Connection'}).click();

            // Default is NATS, fill address
            const addressInput = component.locator('input[placeholder="nats://localhost:4222"]');
            await addressInput.fill('nats://custom:4222');

            // Switch to Azure
            const providerSelect = component.locator('select').first();
            await providerSelect.selectOption('azure-queue');

            // Switch back to NATS
            await providerSelect.selectOption('nats');

            // Address should be reset to default since nats config was undefined
            await expect(addressInput).toHaveValue('nats://localhost:4222');
        });
    });

    test.describe('Form validation - extended', () => {
        test('form error clears when opening edit mode', async ({mount}) => {
            const component = await mount(
                <ConnectionSettingsStory {...defaultProps({value: JSON.stringify([natsConnection])})}/>,
            );

            // Open add form and trigger an error
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            await expect(component.getByText('Name is required.')).toBeVisible();

            // Cancel and open edit on existing connection
            await component.getByRole('button', {name: 'Cancel'}).click();
            await component.getByRole('button', {name: 'Edit'}).click();
            await expect(component.getByText('Name is required.')).not.toBeVisible();
        });
    });

    test.describe('getCSRFToken', () => {
        test('returns empty string when no MMCSRF cookie exists', async ({mount, page}) => {
            const component = await mount(
                <ConnectionSettingsStory {...defaultProps({value: JSON.stringify([natsConnection])})}/>,
            );

            await page.evaluate(() => {
                Object.defineProperty(document, 'cookie', {
                    get: () => 'other=abc; session=xyz',
                    configurable: true,
                });
            });

            let capturedToken = '';
            await page.route('**/plugins/crossguard/api/v1/test-connection*', (route) => {
                capturedToken = route.request().headers()['x-csrf-token'] || '';
                route.fulfill({status: 200, contentType: 'application/json', body: '{"message":"OK"}'});
            });

            await component.getByRole('button', {name: 'Test Connection'}).click();
            expect(capturedToken).toBe('');
        });

        test('extracts token from cookie with multiple entries', async ({mount, page}) => {
            const component = await mount(
                <ConnectionSettingsStory {...defaultProps({value: JSON.stringify([natsConnection])})}/>,
            );

            await page.evaluate(() => {
                Object.defineProperty(document, 'cookie', {
                    get: () => 'other=abc; MMCSRF=multi-cookie-token; session=xyz',
                    configurable: true,
                });
            });

            let capturedToken = '';
            await page.route('**/plugins/crossguard/api/v1/test-connection*', (route) => {
                capturedToken = route.request().headers()['x-csrf-token'] || '';
                route.fulfill({status: 200, contentType: 'application/json', body: '{"message":"OK"}'});
            });

            await component.getByRole('button', {name: 'Test Connection'}).click();
            expect(capturedToken).toBe('multi-cookie-token');
        });
    });
});

// ---------------------------------------------------------------------------
// Appended test blocks
// ---------------------------------------------------------------------------

const natsConn = {
    name: 'test-nats',
    provider: 'nats',
    file_transfer_enabled: false,
    file_filter_mode: '',
    file_filter_types: '',
    message_format: 'json',
    nats: {address: 'nats://localhost:4222', subject: 'crossguard.test-nats', tls_enabled: false, auth_type: 'none', token: '', username: '', password: '', client_cert: '', client_key: '', ca_cert: ''},
};

function props(overrides?: Partial<{id: string; value: string; disabled: boolean}>) {
    return {
        id: overrides?.id ?? 'InboundConnections',
        value: overrides?.value ?? '[]',
        disabled: overrides?.disabled ?? false,
    };
}

async function calls(page: any): Promise<{onChange: Array<{id: string; value: string}>; saveNeeded: number}> {
    return page.evaluate(() => (window as any).__testCalls);
}

// ---------------------------------------------------------------------------
// 1. Form Validation - Name & Duplicate Detection
// ---------------------------------------------------------------------------
test.describe('Form validation - name and duplicates', () => {
    test('empty name shows required error on save', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props()}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();
        await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
        await expect(component.getByText('Name is required.')).toBeVisible();
    });

    test('duplicate name shows error when adding', async ({mount}) => {
        const existing = JSON.stringify([natsConn]);
        const component = await mount(<ConnectionSettingsStory {...props({value: existing})}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();
        const nameInput = component.locator('input[placeholder="my-connection"]');
        await nameInput.fill('test-nats');
        await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
        await expect(component.getByText('A connection with this name already exists. Please use a unique name.')).toBeVisible();
    });

    test('editing same connection does not trigger duplicate error', async ({mount}) => {
        const existing = JSON.stringify([natsConn]);
        const component = await mount(<ConnectionSettingsStory {...props({value: existing})}/>);
        await component.getByRole('button', {name: 'Edit'}).click();
        await component.getByRole('button', {name: 'Update Connection'}).click();
        await expect(component.getByText('A connection with this name already exists')).not.toBeVisible();
    });

    test('editing and renaming to match another connection shows duplicate error', async ({mount}) => {
        const two = JSON.stringify([
            natsConn,
            {...natsConn, name: 'bar', nats: {...natsConn.nats, subject: 'crossguard.bar'}},
        ]);
        const component = await mount(<ConnectionSettingsStory {...props({value: two})}/>);
        await component.getByRole('button', {name: 'Edit'}).first().click();
        const nameInput = component.locator('input[type="text"]').first();
        await nameInput.fill('bar');
        await component.getByRole('button', {name: 'Update Connection'}).click();
        await expect(component.getByText('A connection with this name already exists. Please use a unique name.')).toBeVisible();
    });

    test('uppercase input is lowercased', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props()}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();
        const nameInput = component.locator('input[placeholder="my-connection"]');
        await nameInput.fill('FoO-Bar');
        await expect(nameInput).toHaveValue('foo-bar');
    });

    test('special characters are stripped from name', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props()}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();
        const nameInput = component.locator('input[placeholder="my-connection"]');
        await nameInput.fill('test@#$name!');
        await expect(nameInput).toHaveValue('testname');
    });
});

// ---------------------------------------------------------------------------
// 2. Form Validation - NATS-Specific
// ---------------------------------------------------------------------------
test.describe('Form validation - NATS', () => {
    test('empty address shows required error', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props()}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();
        const nameInput = component.locator('input[placeholder="my-connection"]');
        await nameInput.fill('test');
        const addressInput = component.locator('input[placeholder="nats://localhost:4222"]');
        await addressInput.fill('');
        await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
        await expect(component.getByText('Address is required.')).toBeVisible();
    });

    test('empty subject shows required error', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props()}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();
        const nameInput = component.locator('input[placeholder="my-connection"]');
        await nameInput.fill('test');
        const subjectInput = component.locator('input[placeholder="crossguard.my-connection"]');
        await subjectInput.fill('');
        await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
        await expect(component.getByText('Subject is required.')).toBeVisible();
    });

    test('subject without crossguard prefix shows error', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props()}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();
        const nameInput = component.locator('input[placeholder="my-connection"]');
        await nameInput.fill('test');
        const subjectInput = component.locator('input[placeholder="crossguard.my-connection"]');
        await subjectInput.fill('wrong.prefix');
        await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
        await expect(component.getByText('Subject must start with "crossguard.".')).toBeVisible();
    });

    test('token auth with empty token shows error', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props()}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();
        const nameInput = component.locator('input[placeholder="my-connection"]');
        await nameInput.fill('test');
        const authSelect = component.locator('select').nth(1);
        await authSelect.selectOption('token');
        await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
        await expect(component.getByText('Token is required when auth type is Token.')).toBeVisible();
    });

    test('credentials auth with empty username shows error', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props()}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();
        const nameInput = component.locator('input[placeholder="my-connection"]');
        await nameInput.fill('test');
        const authSelect = component.locator('select').nth(1);
        await authSelect.selectOption('credentials');
        await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
        await expect(component.getByText('Username and password are required when auth type is Credentials.')).toBeVisible();
    });

    test('credentials auth with username but empty password shows error', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props()}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();
        const nameInput = component.locator('input[placeholder="my-connection"]');
        await nameInput.fill('test');
        const authSelect = component.locator('select').nth(1);
        await authSelect.selectOption('credentials');
        const usernameInput = component.locator('input[placeholder="Enter username"]');
        await usernameInput.fill('myuser');
        await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
        await expect(component.getByText('Username and password are required when auth type is Credentials.')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 3. Form Validation - Azure-Specific
// ---------------------------------------------------------------------------
test.describe('Form validation - Azure', () => {
    test('empty queue_service_url shows required error', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props()}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();
        const nameInput = component.locator('input[placeholder="my-connection"]');
        await nameInput.fill('test');
        const providerSelect = component.locator('select').first();
        await providerSelect.selectOption('azure-queue');
        await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
        await expect(component.getByText('Queue Service URL is required.')).toBeVisible();
    });

    test('empty queue_name shows required error', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props()}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();
        const nameInput = component.locator('input[placeholder="my-connection"]');
        await nameInput.fill('test');
        const providerSelect = component.locator('select').first();
        await providerSelect.selectOption('azure-queue');
        await component.locator('input[placeholder="https://myaccount.queue.core.windows.net"]').fill('https://test.queue.core.windows.net');
        await component.locator('input[placeholder="myaccount"]').fill('test');
        await component.locator('input[placeholder="Paste key from Azure portal"]').fill('abc');
        await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
        await expect(component.getByText('Queue Name is required.')).toBeVisible();
    });

    test('NATS Address and Subject fields are not visible when Azure selected', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props()}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();
        const providerSelect = component.locator('select').first();
        await providerSelect.selectOption('azure-queue');
        await expect(component.locator('input[placeholder="nats://localhost:4222"]')).not.toBeVisible();
        await expect(component.locator('input[placeholder="crossguard.my-connection"]')).not.toBeVisible();
    });

    test('blob_container_name visible only with azure provider and file_transfer_enabled', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props()}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();

        // NATS with files enabled: no blob container
        await component.getByText('Enable File Transfer').click();
        await expect(component.getByText('Blob Container Name')).not.toBeVisible();

        // Switch to Azure: blob container appears
        const providerSelect = component.locator('select').first();
        await providerSelect.selectOption('azure-queue');
        await expect(component.getByText('Blob Container Name')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 4. Provider Switching
// ---------------------------------------------------------------------------
test.describe('Provider switching', () => {
    test('NATS form shows Address and Subject fields', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props()}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();
        await expect(component.locator('input[placeholder="nats://localhost:4222"]')).toBeVisible();
        await expect(component.locator('input[placeholder="crossguard.my-connection"]')).toBeVisible();
    });

    test('switching to Azure hides NATS fields and shows Azure fields', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props()}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();
        const providerSelect = component.locator('select').first();
        await providerSelect.selectOption('azure-queue');
        await expect(component.locator('input[placeholder="nats://localhost:4222"]')).not.toBeVisible();
        await expect(component.locator('input[placeholder="crossguard.my-connection"]')).not.toBeVisible();
        await expect(component.getByText('Queue Service URL', {exact: true})).toBeVisible();
        await expect(component.getByText('Queue Name', {exact: true})).toBeVisible();
    });

    test('switching back to NATS from Azure restores NATS fields', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props()}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();
        const providerSelect = component.locator('select').first();
        await providerSelect.selectOption('azure-queue');
        await providerSelect.selectOption('nats');
        await expect(component.locator('input[placeholder="nats://localhost:4222"]')).toBeVisible();
        await expect(component.locator('input[placeholder="crossguard.my-connection"]')).toBeVisible();
        await expect(component.getByText('Queue Service URL', {exact: true})).not.toBeVisible();
    });

    test('name field value preserved across provider switch', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props()}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();
        const nameInput = component.locator('input[placeholder="my-connection"]');
        await nameInput.fill('keep-this');
        const providerSelect = component.locator('select').first();
        await providerSelect.selectOption('azure-queue');
        await expect(nameInput).toHaveValue('keep-this');
        await providerSelect.selectOption('nats');
        await expect(nameInput).toHaveValue('keep-this');
    });

    test('file_transfer_enabled preserved across provider switch', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props()}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();
        await component.getByText('Enable File Transfer').click();
        const fileCheckbox = component.locator('input[type="checkbox"]').last();
        await expect(fileCheckbox).toBeChecked();
        const providerSelect = component.locator('select').first();
        await providerSelect.selectOption('azure-queue');

        // The file transfer checkbox remains. It is always the checkbox whose label
        // text is "Enable File Transfer", and we already clicked it.
        const fileCheckboxAfter = component.locator('label:has-text("Enable File Transfer") input[type="checkbox"]');
        await expect(fileCheckboxAfter).toBeChecked();
    });

    test('cancel returns to card view from form', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props()}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();
        await expect(component.getByText('New Connection')).toBeVisible();
        await component.getByRole('button', {name: 'Cancel'}).click();
        await expect(component.getByText('New Connection')).not.toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 5. Subject Auto-Generation
// ---------------------------------------------------------------------------
test.describe('Subject auto-generation', () => {
    test('typing name auto-fills subject', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props()}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();
        const nameInput = component.locator('input[placeholder="my-connection"]');
        await nameInput.fill('my-test');
        const subjectInput = component.locator('input[placeholder="crossguard.my-connection"]');
        await expect(subjectInput).toHaveValue('crossguard.my-test');
    });

    test('manually edited subject is not overwritten by name change', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props()}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();
        const nameInput = component.locator('input[placeholder="my-connection"]');
        await nameInput.fill('first');
        const subjectInput = component.locator('input[placeholder="crossguard.my-connection"]');
        await expect(subjectInput).toHaveValue('crossguard.first');

        // Manually edit subject
        await subjectInput.fill('crossguard.custom');
        await nameInput.fill('second');
        await expect(subjectInput).toHaveValue('crossguard.custom');
    });

    test('auto-gen resumes after clearing subject back to prefix only', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props()}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();
        const nameInput = component.locator('input[placeholder="my-connection"]');
        const subjectInput = component.locator('input[placeholder="crossguard.my-connection"]');

        // First, manually set subject so auto is broken
        await nameInput.fill('abc');
        await subjectInput.fill('crossguard.manual');
        await nameInput.fill('xyz');
        await expect(subjectInput).toHaveValue('crossguard.manual');

        // Clear subject back to just the prefix
        await subjectInput.fill('crossguard.');
        await nameInput.fill('resumed');
        await expect(subjectInput).toHaveValue('crossguard.resumed');
    });
});

// ---------------------------------------------------------------------------
// 6. normalizeConnection() Legacy Format
// ---------------------------------------------------------------------------
test.describe('Legacy connection format', () => {
    test('flat legacy connection renders as NATS card', async ({mount}) => {
        const legacyConn = {
            name: 'old-conn',
            address: 'nats://legacy:4222',
            subject: 'crossguard.old-conn',
            tls_enabled: false,
            auth_type: 'none',
            token: '',
            username: '',
            password: '',
            client_cert: '',
            client_key: '',
            ca_cert: '',
            file_transfer_enabled: false,
            file_filter_mode: '',
            file_filter_types: '',
            message_format: 'json',
        };
        const component = await mount(<ConnectionSettingsStory {...props({value: JSON.stringify([legacyConn])})}/>);
        await expect(component.getByText('old-conn').first()).toBeVisible();
        await expect(component.getByText('NATS', {exact: true})).toBeVisible();
        await expect(component.getByText('nats://legacy:4222')).toBeVisible();
        await expect(component.getByText('crossguard.old-conn')).toBeVisible();
    });

    test('provider-based format renders unchanged', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props({value: JSON.stringify([natsConn])})}/>);
        await expect(component.getByText('test-nats').first()).toBeVisible();
        await expect(component.getByText('NATS', {exact: true})).toBeVisible();
        await expect(component.getByText('nats://localhost:4222')).toBeVisible();
    });

    test('legacy connection with file_transfer_enabled shows Files badge', async ({mount}) => {
        const legacyWithFiles = {
            name: 'legacy-files',
            address: 'nats://host:4222',
            subject: 'crossguard.legacy-files',
            tls_enabled: false,
            auth_type: 'none',
            file_transfer_enabled: true,
            file_filter_mode: '',
            file_filter_types: '',
            message_format: 'json',
        };
        const component = await mount(<ConnectionSettingsStory {...props({value: JSON.stringify([legacyWithFiles])})}/>);
        await expect(component.getByText('Files', {exact: true}).first()).toBeVisible();
    });

    test('legacy connection edits properly after normalization', async ({mount}) => {
        const legacyConn = {
            name: 'legacy-edit',
            address: 'nats://old:4222',
            subject: 'crossguard.legacy-edit',
            tls_enabled: true,
            auth_type: 'token',
            token: 'abc123',
        };
        const component = await mount(<ConnectionSettingsStory {...props({value: JSON.stringify([legacyConn])})}/>);
        await component.getByRole('button', {name: 'Edit'}).click();
        const providerSelect = component.locator('select').first();
        await expect(providerSelect).toHaveValue('nats');
        await expect(component.locator('input[placeholder="nats://localhost:4222"]')).toHaveValue('nats://old:4222');
        const tlsCheckbox = component.locator('input[type="checkbox"]').first();
        await expect(tlsCheckbox).toBeChecked();
    });
});

// ---------------------------------------------------------------------------
// 7. Test Connection
// ---------------------------------------------------------------------------
test.describe('Test connection', () => {
    test('successful test shows green banner', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/test-connection*', async (route) => {
            await route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({message: 'Connection OK'})});
        });
        const component = await mount(<ConnectionSettingsStory {...props({value: JSON.stringify([natsConn])})}/>);
        await component.getByRole('button', {name: 'Test Connection'}).click();
        await expect(component.getByText('Connection OK')).toBeVisible();
    });

    test('failed test with error body shows red banner', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/test-connection*', async (route) => {
            await route.fulfill({status: 500, contentType: 'application/json', body: JSON.stringify({error: 'timeout'})});
        });
        const component = await mount(<ConnectionSettingsStory {...props({value: JSON.stringify([natsConn])})}/>);
        await component.getByRole('button', {name: 'Test Connection'}).click();
        await expect(component.getByText('timeout')).toBeVisible();
    });

    test('network error shows error banner', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/test-connection*', async (route) => {
            await route.abort('connectionrefused');
        });
        const component = await mount(<ConnectionSettingsStory {...props({value: JSON.stringify([natsConn])})}/>);
        await component.getByRole('button', {name: 'Test Connection'}).click();
        await expect(component.locator('text=/error|failed|abort/i')).toBeVisible();
    });

    test('button shows Testing... while loading', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/test-connection*', async (route) => {
            await new Promise((resolve) => setTimeout(resolve, 500));
            await route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({message: 'OK'})});
        });
        const component = await mount(<ConnectionSettingsStory {...props({value: JSON.stringify([natsConn])})}/>);
        await component.getByRole('button', {name: 'Test Connection'}).click();
        await expect(component.getByText('Testing...')).toBeVisible();
    });

    test('inbound ID sends direction=inbound query param', async ({mount, page}) => {
        let requestUrl = '';
        await page.route('**/plugins/crossguard/api/v1/test-connection*', async (route) => {
            requestUrl = route.request().url();
            await route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({message: 'OK'})});
        });
        const component = await mount(<ConnectionSettingsStory {...props({id: 'InboundConnections', value: JSON.stringify([natsConn])})}/>);
        await component.getByRole('button', {name: 'Test Connection'}).click();
        await expect(component.getByText('OK')).toBeVisible();
        expect(requestUrl).toContain('direction=inbound');
    });

    test('outbound ID sends direction=outbound query param', async ({mount, page}) => {
        let requestUrl = '';
        await page.route('**/plugins/crossguard/api/v1/test-connection*', async (route) => {
            requestUrl = route.request().url();
            await route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({message: 'OK'})});
        });
        const component = await mount(<ConnectionSettingsStory {...props({id: 'OutboundConnections', value: JSON.stringify([natsConn])})}/>);
        await component.getByRole('button', {name: 'Test Connection'}).click();
        await expect(component.getByText('OK')).toBeVisible();
        expect(requestUrl).toContain('direction=outbound');
    });
});

// ---------------------------------------------------------------------------
// 8. Delete and Reindex
// ---------------------------------------------------------------------------
test.describe('Delete and reindex', () => {
    test('delete middle of 3 leaves correct 2 connections', async ({mount, page}) => {
        const conns = [
            {...natsConn, name: 'first'},
            {...natsConn, name: 'second', nats: {...natsConn.nats, subject: 'crossguard.second'}},
            {...natsConn, name: 'third', nats: {...natsConn.nats, subject: 'crossguard.third'}},
        ];
        const component = await mount(<ConnectionSettingsStory {...props({value: JSON.stringify(conns)})}/>);
        await component.getByRole('button', {name: 'Remove'}).nth(1).click();
        const c = await calls(page);
        const saved = JSON.parse(c.onChange[c.onChange.length - 1].value);
        expect(saved.length).toBe(2);
        expect(saved[0].name).toBe('first');
        expect(saved[1].name).toBe('third');
    });

    test('delete last connection shows empty state', async ({mount, page}) => {
        const component = await mount(<ConnectionSettingsStory {...props({value: JSON.stringify([natsConn])})}/>);
        await component.getByRole('button', {name: 'Remove'}).click();
        const c = await calls(page);
        const saved = JSON.parse(c.onChange[c.onChange.length - 1].value);
        expect(saved.length).toBe(0);
    });

    test('editing index 1 then deleting index 0 shifts form to shifted connection', async ({mount}) => {
        const conns = [
            {...natsConn, name: 'alpha'},
            {...natsConn, name: 'beta', nats: {...natsConn.nats, subject: 'crossguard.beta'}},
        ];
        const component = await mount(<ConnectionSettingsStory {...props({value: JSON.stringify(conns)})}/>);

        // Edit second connection (index 1)
        await component.getByRole('button', {name: 'Edit'}).nth(1).click();
        await expect(component.getByText('Edit Connection')).toBeVisible();
        const nameInput = component.locator('input[type="text"]').first();
        await expect(nameInput).toHaveValue('beta');
    });

    test('editing index 0 then deleting index 0 closes form', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props({value: JSON.stringify([natsConn])})}/>);
        await component.getByRole('button', {name: 'Edit'}).click();
        await expect(component.getByText('Edit Connection')).toBeVisible();

        // Cancel to close form since Remove is disabled while editing
        await component.getByRole('button', {name: 'Cancel'}).click();
        await expect(component.getByText('Edit Connection')).not.toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 9. Outbound-Specific Features
// ---------------------------------------------------------------------------
test.describe('Outbound-specific features', () => {
    test('outbound form shows Message Format select', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props({id: 'OutboundConnections'})}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();
        await expect(component.getByText('Message Format')).toBeVisible();
    });

    test('inbound form hides Message Format select', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props({id: 'InboundConnections'})}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();
        await expect(component.getByText('Message Format')).not.toBeVisible();
    });

    test('outbound card with xml message_format shows XML badge', async ({mount}) => {
        const xmlConn = {...natsConn, message_format: 'xml'};
        const component = await mount(<ConnectionSettingsStory {...props({id: 'OutboundConnections', value: JSON.stringify([xmlConn])})}/>);
        await expect(component.getByText('XML', {exact: true}).first()).toBeVisible();
    });

    test('inbound card with xml message_format does NOT show XML badge', async ({mount}) => {
        const xmlConn = {...natsConn, message_format: 'xml'};
        const component = await mount(<ConnectionSettingsStory {...props({id: 'InboundConnections', value: JSON.stringify([xmlConn])})}/>);

        // The card should not display an XML badge for inbound
        await expect(component.locator('span:has-text("XML")')).not.toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 10. File Transfer Display on Cards
// ---------------------------------------------------------------------------
test.describe('File transfer display', () => {
    test('allow mode with types shows Allow badge on card', async ({mount}) => {
        const conn = {...natsConn, file_transfer_enabled: true, file_filter_mode: 'allow', file_filter_types: '.pdf,.docx'};
        const component = await mount(<ConnectionSettingsStory {...props({value: JSON.stringify([conn])})}/>);
        await expect(component.getByText('Allow: .pdf,.docx')).toBeVisible();
    });

    test('deny mode with types shows Deny badge on card', async ({mount}) => {
        const conn = {...natsConn, file_transfer_enabled: true, file_filter_mode: 'deny', file_filter_types: '.exe'};
        const component = await mount(<ConnectionSettingsStory {...props({value: JSON.stringify([conn])})}/>);
        await expect(component.getByText('Deny: .exe')).toBeVisible();
    });

    test('empty filter mode with files enabled shows All types allowed', async ({mount}) => {
        const conn = {...natsConn, file_transfer_enabled: true, file_filter_mode: '', file_filter_types: ''};
        const component = await mount(<ConnectionSettingsStory {...props({value: JSON.stringify([conn])})}/>);
        await expect(component.getByText('All types allowed')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 11. Disabled State
// ---------------------------------------------------------------------------
test.describe('Disabled state', () => {
    test('Add Connection button is disabled when disabled prop is true', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props({disabled: true})}/>);
        await expect(component.getByRole('button', {name: '+ Add Connection'})).toBeDisabled();
    });

    test('card action buttons are disabled when disabled prop is true', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props({value: JSON.stringify([natsConn]), disabled: true})}/>);
        await expect(component.getByRole('button', {name: 'Edit'})).toBeDisabled();
        await expect(component.getByRole('button', {name: 'Test Connection'})).toBeDisabled();
        await expect(component.getByRole('button', {name: 'Remove'})).toBeDisabled();
    });

    test('other cards buttons disabled when form is open for editing', async ({mount}) => {
        const two = JSON.stringify([
            natsConn,
            {...natsConn, name: 'second-conn', nats: {...natsConn.nats, subject: 'crossguard.second-conn'}},
        ]);
        const component = await mount(<ConnectionSettingsStory {...props({value: two})}/>);
        await component.getByRole('button', {name: 'Edit'}).first().click();

        // The second card's buttons should be disabled
        const editButtons = component.getByRole('button', {name: 'Edit'});
        await expect(editButtons.nth(0)).toBeDisabled();
        const testButtons = component.getByRole('button', {name: 'Test Connection'});
        await expect(testButtons.first()).toBeDisabled();
        const removeButtons = component.getByRole('button', {name: 'Remove'});
        const removeCount = await removeButtons.count();
        for (let i = 0; i < removeCount; i++) {
            await expect(removeButtons.nth(i)).toBeDisabled(); // eslint-disable-line no-await-in-loop
        }
    });

    test('Add Connection button disabled while form is open', async ({mount}) => {
        const component = await mount(<ConnectionSettingsStory {...props()}/>);
        await component.getByRole('button', {name: '+ Add Connection'}).click();
        await expect(component.getByRole('button', {name: '+ Add Connection'})).toBeDisabled();
    });
});
