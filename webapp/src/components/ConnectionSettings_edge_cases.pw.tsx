import React from 'react';

import ConnectionSettingsStory from './ConnectionSettingsStory';

import {test, expect} from '../../playwright/ct-coverage';

async function getCalls(page: any): Promise<{onChange: Array<{id: string; value: string}>; saveNeeded: number}> {
    return page.evaluate(() => (window as any).__testCalls);
}

const natsConn = {name: 'test-conn', provider: 'nats', file_transfer_enabled: false, file_filter_mode: '', file_filter_types: '', message_format: 'xml', nats: {address: 'nats://localhost:4222', subject: 'crossguard.test-conn', tls_enabled: false, auth_type: 'none', token: '', username: '', password: '', client_cert: '', client_key: '', ca_cert: ''}};
const azureConn = {name: 'azure-conn', provider: 'azure-queue', file_transfer_enabled: false, file_filter_mode: '', file_filter_types: '', message_format: 'xml', azure_queue: {queue_service_url: 'https://test.queue.core.windows.net', blob_service_url: '', account_name: 'test', account_key: 'dGVzdA==', queue_name: 'test-queue', blob_container_name: ''}};
const serviceBusConn = {name: 'sb-conn', provider: 'azure-servicebus', file_transfer_enabled: false, file_filter_mode: '', file_filter_types: '', message_format: 'xml', azure_servicebus: {connection_string: 'Endpoint=sb://example.servicebus.windows.net/;SharedAccessKeyName=root;SharedAccessKey=abc', queue_name: 'sb-queue', blob_service_url: '', blob_account_name: '', blob_account_key: '', blob_container_name: ''}};

function defaultProps(overrides?: Partial<{id: string; value: string; disabled: boolean}>) {
    return {
        id: overrides?.id ?? 'InboundConnections',
        value: overrides?.value ?? '[]',
        disabled: overrides?.disabled ?? false,
    };
}

test.describe('ConnectionSettings Edge Cases', () => {
    // -------------------------------------------------------------------------
    // 1. Legacy format normalization
    // -------------------------------------------------------------------------
    test.describe('Legacy format normalization', () => {
        test('legacy connection without provider field renders as NATS card with name and address', async ({mount}) => {
            const legacy = {name: 'legacy-nats', address: 'nats://remote:4222', subject: 'crossguard.legacy-nats', tls_enabled: false, auth_type: 'none', token: ''};
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([legacy])})}/>);
            await expect(component.getByText('legacy-nats').first()).toBeVisible();
            await expect(component.getByText('nats://remote:4222')).toBeVisible();
            await expect(component.locator('span', {hasText: 'NATS'}).first()).toBeVisible();
        });

        test('legacy connection with auth_type token shows Token auth badge', async ({mount}) => {
            const legacy = {name: 'token-legacy', address: 'nats://host:4222', subject: 'crossguard.token-legacy', tls_enabled: false, auth_type: 'token', token: 'secret123'};
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([legacy])})}/>);
            await expect(component.locator('span').filter({hasText: /^Token$/}).first()).toBeVisible();
        });

        test('legacy connection missing most fields gets defaults and renders without crash', async ({mount}) => {
            const minimal = {name: 'bare-minimum'};
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([minimal])})}/>);
            await expect(component.getByText('bare-minimum')).toBeVisible();
            await expect(component.locator('span', {hasText: 'NATS'}).first()).toBeVisible();
        });

        test('legacy connection with file_transfer_enabled true shows Files badge', async ({mount}) => {
            const legacy = {name: 'files-legacy', address: 'nats://host:4222', subject: 'crossguard.files-legacy', tls_enabled: false, auth_type: 'none', file_transfer_enabled: true};
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([legacy])})}/>);
            await expect(component.getByText('Files', {exact: true}).first()).toBeVisible();
        });

        test('array mixing legacy and modern format connections renders all correctly', async ({mount}) => {
            const legacy = {name: 'old-conn', address: 'nats://old:4222', subject: 'crossguard.old-conn', tls_enabled: false, auth_type: 'none'};
            const modern = {...natsConn, name: 'new-conn', nats: {...natsConn.nats, subject: 'crossguard.new-conn'}};
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([legacy, modern])})}/>);
            await expect(component.getByText('old-conn').first()).toBeVisible();
            await expect(component.getByText('new-conn').first()).toBeVisible();
        });
    });

    // -------------------------------------------------------------------------
    // 2. Multi-connection CRUD
    // -------------------------------------------------------------------------
    test.describe('Multi-connection CRUD', () => {
        test('add 3 NATS connections, verify all 3 card names visible', async ({mount}) => {
            const conns = [
                {...natsConn, name: 'alpha', nats: {...natsConn.nats, subject: 'crossguard.alpha'}},
                {...natsConn, name: 'bravo', nats: {...natsConn.nats, subject: 'crossguard.bravo'}},
                {...natsConn, name: 'charlie', nats: {...natsConn.nats, subject: 'crossguard.charlie'}},
            ];
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify(conns)})}/>);
            await expect(component.getByText('alpha').first()).toBeVisible();
            await expect(component.getByText('bravo').first()).toBeVisible();
            await expect(component.getByText('charlie').first()).toBeVisible();
        });

        test('add 3 connections, remove middle one, verify 2 remaining names', async ({mount, page}) => {
            const conns = [
                {...natsConn, name: 'first', nats: {...natsConn.nats, subject: 'crossguard.first'}},
                {...natsConn, name: 'second', nats: {...natsConn.nats, subject: 'crossguard.second'}},
                {...natsConn, name: 'third', nats: {...natsConn.nats, subject: 'crossguard.third'}},
            ];
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify(conns)})}/>);
            await component.getByRole('button', {name: 'Remove'}).nth(1).click();
            const calls = await getCalls(page);
            const saved = JSON.parse(calls.onChange[calls.onChange.length - 1].value);
            expect(saved.length).toBe(2);
            expect(saved[0].name).toBe('first');
            expect(saved[1].name).toBe('third');
        });

        test('add 3 connections, click Edit on last one, verify edit form shows that name', async ({mount}) => {
            const conns = [
                {...natsConn, name: 'conn-a', nats: {...natsConn.nats, subject: 'crossguard.conn-a'}},
                {...natsConn, name: 'conn-b', nats: {...natsConn.nats, subject: 'crossguard.conn-b'}},
                {...natsConn, name: 'conn-c', nats: {...natsConn.nats, subject: 'crossguard.conn-c'}},
            ];
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify(conns)})}/>);
            await component.getByRole('button', {name: 'Edit'}).nth(2).click();
            await expect(component.getByText('Edit Connection')).toBeVisible();
            const nameInput = component.locator('input[type="text"]').first();
            await expect(nameInput).toHaveValue('conn-c');
        });

        test('add a connection, remove it, add another, verify only 1 card', async ({mount, page}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([natsConn])})}/>);
            await component.getByRole('button', {name: 'Remove'}).click();
            const calls = await getCalls(page);
            const afterRemove = JSON.parse(calls.onChange[calls.onChange.length - 1].value);
            expect(afterRemove.length).toBe(0);
        });

        test('add NATS then Azure connection, verify both provider badges appear', async ({mount}) => {
            const conns = [natsConn, azureConn];
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify(conns)})}/>);
            await expect(component.locator('span', {hasText: 'NATS'}).first()).toBeVisible();
            await expect(component.getByText('Azure')).toBeVisible();
        });

        test('delete first of 3 while editing third: verify edit form name matches third connection', async ({mount}) => {
            const conns = [
                {...natsConn, name: 'del-me', nats: {...natsConn.nats, subject: 'crossguard.del-me'}},
                {...natsConn, name: 'middle', nats: {...natsConn.nats, subject: 'crossguard.middle'}},
                {...natsConn, name: 'keep-editing', nats: {...natsConn.nats, subject: 'crossguard.keep-editing'}},
            ];
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify(conns)})}/>);
            await component.getByRole('button', {name: 'Edit'}).nth(2).click();
            const nameInput = component.locator('input[type="text"]').first();
            await expect(nameInput).toHaveValue('keep-editing');
        });

        test('delete only connection, verify onChange called with empty array', async ({mount, page}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([natsConn])})}/>);
            await component.getByRole('button', {name: 'Remove'}).click();
            const calls = await getCalls(page);
            const lastChange = calls.onChange[calls.onChange.length - 1];
            expect(JSON.parse(lastChange.value)).toEqual([]);
        });

        test('delete only connection triggers onChange with empty array', async ({mount, page}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([natsConn])})}/>);
            await component.getByRole('button', {name: 'Remove'}).click();
            const calls = await getCalls(page);
            const lastChange = calls.onChange[calls.onChange.length - 1];
            const saved = JSON.parse(lastChange.value);
            expect(saved).toEqual([]);
        });
    });

    // -------------------------------------------------------------------------
    // 3. Test status on delete
    // -------------------------------------------------------------------------
    test.describe('Test status on delete', () => {
        test('test connection at index 0, delete index 1, success banner still visible at index 0', async ({mount, page}) => {
            const conns = [
                {...natsConn, name: 'tested-conn', nats: {...natsConn.nats, subject: 'crossguard.tested-conn'}},
                {...natsConn, name: 'to-delete', nats: {...natsConn.nats, subject: 'crossguard.to-delete'}},
            ];
            await page.route('**/plugins/crossguard/api/v1/test-connection*', async (route) => {
                await route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({message: 'Connection successful'})});
            });
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify(conns)})}/>);
            await component.getByRole('button', {name: 'Test Connection'}).first().click();
            await expect(component.getByText('Connection successful')).toBeVisible();
            await component.getByRole('button', {name: 'Remove'}).nth(1).click();
            await expect(component.getByText('Connection successful')).toBeVisible();
        });

        test('test connection at index 1, delete index 0, verify first card has correct name', async ({mount, page}) => {
            const conns = [
                {...natsConn, name: 'to-delete', nats: {...natsConn.nats, subject: 'crossguard.to-delete'}},
                {...natsConn, name: 'tested-conn', nats: {...natsConn.nats, subject: 'crossguard.tested-conn'}},
            ];
            await page.route('**/plugins/crossguard/api/v1/test-connection*', async (route) => {
                await route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({message: 'Connection successful'})});
            });
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify(conns)})}/>);
            await component.getByRole('button', {name: 'Test Connection'}).nth(1).click();
            await expect(component.getByText('Connection successful')).toBeVisible();
            await component.getByRole('button', {name: 'Remove'}).first().click();
            await expect(component.getByText('tested-conn').first()).toBeVisible();
        });

        test('test connection at index 2, delete index 2, no banner visible', async ({mount, page}) => {
            const conns = [
                {...natsConn, name: 'stay-a', nats: {...natsConn.nats, subject: 'crossguard.stay-a'}},
                {...natsConn, name: 'stay-b', nats: {...natsConn.nats, subject: 'crossguard.stay-b'}},
                {...natsConn, name: 'test-and-delete', nats: {...natsConn.nats, subject: 'crossguard.test-and-delete'}},
            ];
            await page.route('**/plugins/crossguard/api/v1/test-connection*', async (route) => {
                await route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({message: 'Connection successful'})});
            });
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify(conns)})}/>);
            await component.getByRole('button', {name: 'Test Connection'}).nth(2).click();
            await expect(component.getByText('Connection successful')).toBeVisible();
            await component.getByRole('button', {name: 'Remove'}).nth(2).click();
            await expect(component.getByText('Connection successful')).not.toBeVisible();
        });

        test('two test banners visible, delete middle connection, verify banners still show on correct cards', async ({mount, page}) => {
            const conns = [
                {...natsConn, name: 'banner-a', nats: {...natsConn.nats, subject: 'crossguard.banner-a'}},
                {...natsConn, name: 'middle-del', nats: {...natsConn.nats, subject: 'crossguard.middle-del'}},
                {...natsConn, name: 'banner-c', nats: {...natsConn.nats, subject: 'crossguard.banner-c'}},
            ];
            await page.route('**/plugins/crossguard/api/v1/test-connection*', async (route) => {
                await route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({message: 'Connection successful'})});
            });
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify(conns)})}/>);
            await component.getByRole('button', {name: 'Test Connection'}).first().click();
            await expect(component.getByText('Connection successful').first()).toBeVisible();
            await component.getByRole('button', {name: 'Test Connection'}).nth(2).click();
            await component.getByRole('button', {name: 'Remove'}).nth(1).click();
            await expect(component.getByText('banner-a').first()).toBeVisible();
            await expect(component.getByText('banner-c').first()).toBeVisible();
        });
    });

    // -------------------------------------------------------------------------
    // 4. Provider switching
    // -------------------------------------------------------------------------
    test.describe('Provider switching', () => {
        test('switch NATS to Azure, verify Azure-specific fields appear and NATS fields hidden', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([natsConn])})}/>);
            await component.getByRole('button', {name: 'Edit'}).click();
            await expect(component.getByText('Address')).toBeVisible();
            const providerSelect = component.locator('select').first();
            await providerSelect.selectOption('azure-queue');
            await expect(component.getByText('Queue Service URL', {exact: true})).toBeVisible();
            await expect(component.getByText('Queue Name', {exact: true})).toBeVisible();
            await expect(component.locator('input[placeholder="nats://localhost:4222"]')).not.toBeVisible();
        });

        test('switch Azure to NATS, verify NATS-specific fields appear and Azure fields hidden', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([azureConn])})}/>);
            await component.getByRole('button', {name: 'Edit'}).click();
            await expect(component.getByText('Queue Service URL', {exact: true})).toBeVisible();
            const providerSelect = component.locator('select').first();
            await providerSelect.selectOption('nats');
            await expect(component.getByText('Address')).toBeVisible();
            await expect(component.getByText('Subject')).toBeVisible();
            await expect(component.getByText('Queue Service URL', {exact: true})).not.toBeVisible();
        });

        test('switch NATS to Azure to NATS, verify NATS fields appear with new empty config', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const providerSelect = component.locator('select').first();
            await providerSelect.selectOption('azure-queue');
            await providerSelect.selectOption('nats');
            await expect(component.getByText('Address')).toBeVisible();
            await expect(component.getByText('Subject')).toBeVisible();
            const addressInput = component.locator('input[placeholder="nats://localhost:4222"]');
            await expect(addressInput).toHaveValue('nats://localhost:4222');
        });

        test('Azure connection JSON has nats undefined', async ({mount, page}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('azure-test');
            const providerSelect = component.locator('select').first();
            await providerSelect.selectOption('azure-queue');
            const queueURLInput = component.locator('input[placeholder="https://myaccount.queue.core.windows.net"]');
            await queueURLInput.fill('https://test.queue.core.windows.net');
            const accountNameInput = component.locator('input[placeholder="myaccount"]');
            await accountNameInput.fill('test');
            const accountKeyInput = component.locator('input[placeholder="Paste key from Azure portal"]');
            await accountKeyInput.fill('dGVzdA==');
            const queueInput = component.locator('input[placeholder="crossguard-messages"]');
            await queueInput.fill('my-queue');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            const calls = await getCalls(page);
            const saved = JSON.parse(calls.onChange[calls.onChange.length - 1].value);
            expect(saved[0].nats).toBeUndefined();
        });

        test('NATS connection JSON has azure_queue undefined', async ({mount, page}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('nats-test');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            const calls = await getCalls(page);
            const saved = JSON.parse(calls.onChange[calls.onChange.length - 1].value);
            expect(saved[0].azure_queue).toBeUndefined();
        });
    });

    // -------------------------------------------------------------------------
    // 5. Auto-subject edge cases
    // -------------------------------------------------------------------------
    test.describe('Auto-subject edge cases', () => {
        test('type abc in name field, subject shows crossguard.abc', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('abc');
            const subjectInput = component.locator('input[placeholder="crossguard.my-connection"]');
            await expect(subjectInput).toHaveValue('crossguard.abc');
        });

        test('clear name to empty, subject shows crossguard. (just prefix)', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('temp');
            const subjectInput = component.locator('input[placeholder="crossguard.my-connection"]');
            await expect(subjectInput).toHaveValue('crossguard.temp');
            await nameInput.fill('');
            await expect(subjectInput).toHaveValue('crossguard.');
        });

        test('type name ---, subject shows crossguard.---', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('---');
            const subjectInput = component.locator('input[placeholder="crossguard.my-connection"]');
            await expect(subjectInput).toHaveValue('crossguard.---');
        });

        test('manually edit subject to crossguard.custom, then change name, subject stays crossguard.custom', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('initial');
            const subjectInput = component.locator('input[placeholder="crossguard.my-connection"]');
            await expect(subjectInput).toHaveValue('crossguard.initial');
            await subjectInput.fill('crossguard.custom');
            await nameInput.fill('changed');
            await expect(subjectInput).toHaveValue('crossguard.custom');
        });

        test('subject that is just the prefix crossguard. is considered auto-generated and updates when name changes', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const subjectInput = component.locator('input[placeholder="crossguard.my-connection"]');
            await expect(subjectInput).toHaveValue('crossguard.');
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('newname');
            await expect(subjectInput).toHaveValue('crossguard.newname');
        });
    });

    // -------------------------------------------------------------------------
    // 6. Validation edge cases
    // -------------------------------------------------------------------------
    test.describe('Validation edge cases', () => {
        test('name with spaces only is sanitized to empty, shows Name is required', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('   ');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            await expect(component.getByText('Name is required.')).toBeVisible();
        });

        test('NATS address with whitespace only shows Address is required', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('valid-name');
            const addressInput = component.locator('input[placeholder="nats://localhost:4222"]');
            await addressInput.fill('   ');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            await expect(component.getByText('Address is required.')).toBeVisible();
        });

        test('NATS subject set to just crossguard. (prefix only) passes validation', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('prefix-only');
            const subjectInput = component.locator('input[placeholder="crossguard.my-connection"]');
            await subjectInput.fill('crossguard.');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            await expect(component.getByText('Subject is required.')).not.toBeVisible();
            await expect(component.getByText('Subject must start with')).not.toBeVisible();
        });

        test('NATS token auth with whitespace-only token shows token error', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('token-test');
            const authSelect = component.locator('select').nth(1);
            await authSelect.selectOption('token');
            const tokenInput = component.locator('input[placeholder="Enter token"]');
            await tokenInput.fill('   ');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            await expect(component.getByText('Token is required when auth type is Token.')).toBeVisible();
        });

        test('NATS credentials with username but empty password shows credentials error', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('cred-test');
            const authSelect = component.locator('select').nth(1);
            await authSelect.selectOption('credentials');
            const usernameInput = component.locator('input[placeholder="Enter username"]');
            await usernameInput.fill('myuser');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            await expect(component.getByText('Username and password are required when auth type is Credentials.')).toBeVisible();
        });

        test('NATS credentials with password but empty username shows credentials error', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('cred-test-2');
            const authSelect = component.locator('select').nth(1);
            await authSelect.selectOption('credentials');
            const passwordInput = component.locator('input[placeholder="Enter password"]');
            await passwordInput.fill('mypass');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            await expect(component.getByText('Username and password are required when auth type is Credentials.')).toBeVisible();
        });

        test('Azure queue_service_url with whitespace only shows URL error', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('azure-ws');
            const providerSelect = component.locator('select').first();
            await providerSelect.selectOption('azure-queue');
            const queueURLInput = component.locator('input[placeholder="https://myaccount.queue.core.windows.net"]');
            await queueURLInput.fill('   ');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            await expect(component.getByText('Queue Service URL is required.')).toBeVisible();
        });

        test('Azure queue_name with whitespace only shows queue name error', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('azure-q-ws');
            const providerSelect = component.locator('select').first();
            await providerSelect.selectOption('azure-queue');
            const queueURLInput = component.locator('input[placeholder="https://myaccount.queue.core.windows.net"]');
            await queueURLInput.fill('https://test.queue.core.windows.net');
            const accountNameInput = component.locator('input[placeholder="myaccount"]');
            await accountNameInput.fill('test');
            const accountKeyInput = component.locator('input[placeholder="Paste key from Azure portal"]');
            await accountKeyInput.fill('dGVzdA==');
            const queueInput = component.locator('input[placeholder="crossguard-messages"]');
            await queueInput.fill('   ');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            await expect(component.getByText('Queue Name is required.')).toBeVisible();
        });
    });

    // -------------------------------------------------------------------------
    // 7. Disabled state
    // -------------------------------------------------------------------------
    test.describe('Disabled state', () => {
        test('card Edit/Test/Remove buttons disabled when disabled=true', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([natsConn]), disabled: true})}/>);
            await expect(component.getByRole('button', {name: 'Edit'})).toBeDisabled();
            await expect(component.getByRole('button', {name: 'Test Connection'})).toBeDisabled();
            await expect(component.getByRole('button', {name: 'Remove'})).toBeDisabled();
        });

        test('Add Connection button disabled when disabled=true', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({disabled: true})}/>);
            await expect(component.getByRole('button', {name: '+ Add Connection'})).toBeDisabled();
        });

        test('when multiple cards exist, all buttons disabled when disabled=true', async ({mount}) => {
            const conns = [
                {...natsConn, name: 'conn-1', nats: {...natsConn.nats, subject: 'crossguard.conn-1'}},
                {...natsConn, name: 'conn-2', nats: {...natsConn.nats, subject: 'crossguard.conn-2'}},
            ];
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify(conns), disabled: true})}/>);
            const editButtons = component.getByRole('button', {name: 'Edit'});
            const testButtons = component.getByRole('button', {name: 'Test Connection'});
            const removeButtons = component.getByRole('button', {name: 'Remove'});
            const editCount = await editButtons.count();
            const editChecks: Array<Promise<void>> = [];
            for (let i = 0; i < editCount; i++) {
                editChecks.push(
                    expect(editButtons.nth(i)).toBeDisabled(),
                    expect(testButtons.nth(i)).toBeDisabled(),
                    expect(removeButtons.nth(i)).toBeDisabled(),
                );
            }
            await Promise.all(editChecks);
            await expect(component.getByRole('button', {name: '+ Add Connection'})).toBeDisabled();
        });

        test('form save button works when disabled=false (inverse check)', async ({mount, page}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({disabled: false})}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('enabled-conn');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            const calls = await getCalls(page);
            expect(calls.onChange.length).toBe(1);
            const saved = JSON.parse(calls.onChange[0].value);
            expect(saved[0].name).toBe('enabled-conn');
        });
    });

    // -------------------------------------------------------------------------
    // 8. Rendering edge cases
    // -------------------------------------------------------------------------
    test.describe('Rendering edge cases', () => {
        test('JSON object (not array) as value renders empty state', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: '{"name":"foo"}'})}/>);
            await expect(component.getByText('No connections configured')).toBeVisible();
        });

        test('JSON number as value renders empty state', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: '42'})}/>);
            await expect(component.getByText('No connections configured')).toBeVisible();
        });

        test('empty string value renders empty state', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: ''})}/>);
            await expect(component.getByText('No connections configured')).toBeVisible();
        });

        test('empty array "[]" renders empty state', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: '[]'})}/>);
            await expect(component.getByText('No connections configured')).toBeVisible();
        });

        test('connection with empty name string renders card without crash', async ({mount}) => {
            const conn = {...natsConn, name: ''};
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([conn])})}/>);
            await expect(component.locator('span', {hasText: 'NATS'}).first()).toBeVisible();
        });

        test('very long name (200 chars) renders without error', async ({mount}) => {
            const longName = 'a'.repeat(200);
            const conn = {...natsConn, name: longName, nats: {...natsConn.nats, subject: 'crossguard.' + longName}};
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([conn])})}/>);
            await expect(component.getByText(longName).first()).toBeVisible();
        });
    });

    // -------------------------------------------------------------------------
    // 9. Save behavior
    // -------------------------------------------------------------------------
    test.describe('Save behavior', () => {
        test('no onChange when no form open (getCalls returns empty onChange array on mount)', async ({mount, page}) => {
            await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            const calls = await getCalls(page);
            expect(calls.onChange).toHaveLength(0);
        });

        test('add form, fill NATS fields, save, getCalls shows 1 onChange entry', async ({mount, page}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('save-test');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            const calls = await getCalls(page);
            expect(calls.onChange).toHaveLength(1);
            const saved = JSON.parse(calls.onChange[0].value);
            expect(saved[0].name).toBe('save-test');
            expect(saved[0].provider).toBe('nats');
        });

        test('edit existing connection, save, getCalls shows 1 onChange with updated JSON', async ({mount, page}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([natsConn])})}/>);
            await component.getByRole('button', {name: 'Edit'}).click();
            const nameInput = component.locator('input[type="text"]').first();
            await nameInput.fill('updated-conn');
            await component.getByRole('button', {name: 'Update Connection'}).click();
            const calls = await getCalls(page);
            expect(calls.onChange).toHaveLength(1);
            const saved = JSON.parse(calls.onChange[0].value);
            expect(saved[0].name).toBe('updated-conn');
        });

        test('cancel form, verify no new onChange call', async ({mount, page}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('will-cancel');
            await component.getByRole('button', {name: 'Cancel'}).click();
            const calls = await getCalls(page);
            expect(calls.onChange).toHaveLength(0);
        });

        test('form error clears when opening edit mode on a connection', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([natsConn])})}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            await expect(component.getByText('Name is required.')).toBeVisible();
            await component.getByRole('button', {name: 'Cancel'}).click();
            await component.getByRole('button', {name: 'Edit'}).click();
            await expect(component.getByText('Name is required.')).not.toBeVisible();
        });
    });

    // -------------------------------------------------------------------------
    // 10. Azure provider CRUD
    // -------------------------------------------------------------------------
    test.describe('Azure provider CRUD', () => {
        test('Azure card shows Queue metadata in card view', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([azureConn])})}/>);
            await expect(component.getByText('Queue', {exact: true})).toBeVisible();
            await expect(component.getByText('test-queue')).toBeVisible();
        });

        test('Azure card shows Azure provider badge', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([azureConn])})}/>);
            await expect(component.getByText('Azure')).toBeVisible();
        });

        test('Add Azure connection with all fields, verify saved JSON structure', async ({mount, page}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const nameInput = component.locator('input[placeholder="my-connection"]');
            await nameInput.fill('my-azure');
            const providerSelect = component.locator('select').first();
            await providerSelect.selectOption('azure-queue');
            const queueURLInput = component.locator('input[placeholder="https://myaccount.queue.core.windows.net"]');
            await queueURLInput.fill('https://myacct.queue.core.windows.net');
            const accountNameInput = component.locator('input[placeholder="myaccount"]');
            await accountNameInput.fill('myacct');
            const accountKeyInput = component.locator('input[placeholder="Paste key from Azure portal"]');
            await accountKeyInput.fill('c2VjcmV0');
            const queueInput = component.locator('input[placeholder="crossguard-messages"]');
            await queueInput.fill('my-queue');
            await component.getByRole('button', {name: 'Add Connection', exact: true}).click();
            const calls = await getCalls(page);
            const saved = JSON.parse(calls.onChange[calls.onChange.length - 1].value);
            expect(saved[0].name).toBe('my-azure');
            expect(saved[0].provider).toBe('azure-queue');
            expect(saved[0].azure_queue.queue_service_url).toBe('https://myacct.queue.core.windows.net');
            expect(saved[0].azure_queue.account_name).toBe('myacct');
            expect(saved[0].azure_queue.account_key).toBe('c2VjcmV0');
            expect(saved[0].azure_queue.queue_name).toBe('my-queue');
            expect(saved[0].nats).toBeUndefined();
        });

        test('Edit Azure connection, change queue_name, save, verify updated JSON', async ({mount, page}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([azureConn])})}/>);
            await component.getByRole('button', {name: 'Edit'}).click();
            await expect(component.getByText('Edit Connection')).toBeVisible();
            const queueInput = component.locator('input[placeholder="crossguard-messages"]');
            await queueInput.fill('updated-queue');
            await component.getByRole('button', {name: 'Update Connection'}).click();
            const calls = await getCalls(page);
            const saved = JSON.parse(calls.onChange[calls.onChange.length - 1].value);
            expect(saved[0].azure_queue.queue_name).toBe('updated-queue');
        });
    });

    // -------------------------------------------------------------------------
    // 11. Test connection edge cases
    // -------------------------------------------------------------------------
    test.describe('Test connection edge cases', () => {
        test('Test connection success with empty message body shows Connection successful', async ({mount, page}) => {
            await page.route('**/plugins/crossguard/api/v1/test-connection*', async (route) => {
                await route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({})});
            });
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([natsConn])})}/>);
            await component.getByRole('button', {name: 'Test Connection'}).click();
            await expect(component.getByText('Connection successful')).toBeVisible();
        });

        test('Test connection with non-JSON error body shows Connection failed', async ({mount, page}) => {
            await page.route('**/plugins/crossguard/api/v1/test-connection*', async (route) => {
                await route.fulfill({status: 500, contentType: 'text/plain', body: 'Internal Server Error'});
            });
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([natsConn])})}/>);
            await component.getByRole('button', {name: 'Test Connection'}).click();
            await expect(component.getByText('Connection failed')).toBeVisible();
        });

        test('Test connection network abort shows error message', async ({mount, page}) => {
            await page.route('**/plugins/crossguard/api/v1/test-connection*', async (route) => {
                await route.abort('connectionrefused');
            });
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([natsConn])})}/>);
            await component.getByRole('button', {name: 'Test Connection'}).click();

            // Network abort triggers catch block, which sets message from err.message or 'Network error'.
            // The status banner appears with success=false, so we wait for the button to stop showing 'Testing...'
            await expect(component.getByRole('button', {name: 'Test Connection'})).toBeVisible({timeout: 5000});
        });

        test('Test connection loading shows Testing... on button', async ({mount, page}) => {
            await page.route('**/plugins/crossguard/api/v1/test-connection*', async (route) => {
                await new Promise((resolve) => setTimeout(resolve, 5000));
                await route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({message: 'ok'})});
            });
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([natsConn])})}/>);
            await component.getByRole('button', {name: 'Test Connection'}).click();
            await expect(component.getByRole('button', {name: 'Testing...'})).toBeVisible();
        });
    });

    // -------------------------------------------------------------------------
    // 12. Message format (outbound only)
    // -------------------------------------------------------------------------
    test.describe('Message format outbound only', () => {
        test('OutboundConnections id shows Message Format select in form', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({id: 'OutboundConnections'})}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            await expect(component.getByText('Message Format')).toBeVisible();
        });

        test('InboundConnections id does NOT show Message Format in form', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({id: 'InboundConnections'})}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            await expect(component.getByText('Message Format')).not.toBeVisible();
        });

        test('Outbound card with message_format xml shows XML badge', async ({mount}) => {
            const xmlConn = {...natsConn, name: 'xml-out', message_format: 'xml' as const};
            const component = await mount(<ConnectionSettingsStory {...defaultProps({id: 'OutboundConnections', value: JSON.stringify([xmlConn])})}/>);
            await expect(component.getByText('XML', {exact: true}).first()).toBeVisible();
        });

        test('Outbound card with message_format json shows JSON badge and not XML badge', async ({mount}) => {
            const jsonConn = {...natsConn, name: 'json-out', message_format: 'json' as const};
            const component = await mount(<ConnectionSettingsStory {...defaultProps({id: 'OutboundConnections', value: JSON.stringify([jsonConn])})}/>);
            await expect(component.getByText('JSON', {exact: true}).first()).toBeVisible();
            const xmlBadges = component.locator('span').filter({hasText: /^XML$/});
            await expect(xmlBadges).toHaveCount(0);
        });
    });

    // -------------------------------------------------------------------------
    // 13. TLS fields conditional rendering
    // -------------------------------------------------------------------------
    test.describe('TLS fields conditional', () => {
        test('Enable TLS checkbox: Client Cert, Client Key, CA Cert appear', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const tlsCheckbox = component.locator('input[type="checkbox"]').first();
            await tlsCheckbox.check();
            await expect(component.getByText('Client Cert Path')).toBeVisible();
            await expect(component.getByText('Client Key Path')).toBeVisible();
            await expect(component.getByText('CA Cert Path')).toBeVisible();
        });

        test('Disable TLS: cert fields disappear', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps()}/>);
            await component.getByRole('button', {name: '+ Add Connection'}).click();
            const tlsCheckbox = component.locator('input[type="checkbox"]').first();
            await tlsCheckbox.check();
            await expect(component.getByText('Client Cert Path')).toBeVisible();
            await tlsCheckbox.uncheck();
            await expect(component.getByText('Client Cert Path')).not.toBeVisible();
            await expect(component.getByText('Client Key Path')).not.toBeVisible();
            await expect(component.getByText('CA Cert Path')).not.toBeVisible();
        });

        test('Card with tls_enabled true shows TLS badge', async ({mount}) => {
            const tlsConn = {...natsConn, name: 'tls-conn', nats: {...natsConn.nats, tls_enabled: true}};
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([tlsConn])})}/>);
            await expect(component.getByText('TLS', {exact: true})).toBeVisible();
        });

        test('Card with tls_enabled false does NOT show TLS badge', async ({mount}) => {
            const noTlsConn = {...natsConn, name: 'no-tls', nats: {...natsConn.nats, tls_enabled: false}};
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([noTlsConn])})}/>);
            const tlsBadges = component.locator('span').filter({hasText: /^TLS$/});
            await expect(tlsBadges).toHaveCount(0);
        });
    });

    // -------------------------------------------------------------------------
    // 14. Deny file filter mode on card
    // -------------------------------------------------------------------------
    test.describe('Deny file filter badge', () => {
        test('card with deny file filter mode shows Deny badge text', async ({mount}) => {
            const conn = {
                ...natsConn,
                name: 'deny-conn',
                file_transfer_enabled: true,
                file_filter_mode: 'deny',
                file_filter_types: '.exe,.bat',
                nats: {...natsConn.nats, subject: 'crossguard.deny-conn'},
            };
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([conn])})}/>);
            await expect(component.getByText('Deny: .exe,.bat')).toBeVisible();
        });
    });

    // -------------------------------------------------------------------------
    // 15. File transfer with Azure blob container
    // -------------------------------------------------------------------------
    test.describe('File transfer Azure blob', () => {
        test('Azure plus file_transfer_enabled shows Blob Container Name field', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([azureConn])})}/>);
            await component.getByRole('button', {name: 'Edit'}).click();
            const fileCheckbox = component.getByText('Enable File Transfer').locator('..').locator('input[type="checkbox"]');
            await fileCheckbox.check();
            await expect(component.getByText('Blob Container Name')).toBeVisible();
            await expect(component.locator('input[placeholder="crossguard-files"]')).toBeVisible();
        });

        test('NATS plus file_transfer_enabled does NOT show Blob Container Name', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([natsConn])})}/>);
            await component.getByRole('button', {name: 'Edit'}).click();
            const fileCheckbox = component.getByText('Enable File Transfer').locator('..').locator('input[type="checkbox"]');
            await fileCheckbox.check();
            await expect(component.getByText('Blob Container Name')).not.toBeVisible();
        });

        test('Fill blob_container_name, save Azure connection, verify value in JSON', async ({mount, page}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([azureConn])})}/>);
            await component.getByRole('button', {name: 'Edit'}).click();
            const fileCheckbox = component.getByText('Enable File Transfer').locator('..').locator('input[type="checkbox"]');
            await fileCheckbox.check();
            await component.locator('input[placeholder="https://myaccount.blob.core.windows.net"]').fill('https://test.blob.core.windows.net');
            const blobInput = component.locator('input[placeholder="crossguard-files"]');
            await blobInput.fill('my-blob-container');
            await component.getByRole('button', {name: 'Update Connection'}).click();
            const calls = await getCalls(page);
            const saved = JSON.parse(calls.onChange[calls.onChange.length - 1].value);
            expect(saved[0].file_transfer_enabled).toBe(true);
            expect(saved[0].azure_queue.blob_container_name).toBe('my-blob-container');
        });
    });

    // -------------------------------------------------------------------------
    // Azure Service Bus provider
    // -------------------------------------------------------------------------
    test.describe('Azure Service Bus provider', () => {
        test('round-trips an azure-servicebus connection without mutating it', async ({mount, page}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([serviceBusConn])})}/>);
            await expect(component.getByText('sb-conn').first()).toBeVisible();
            await component.getByRole('button', {name: 'Edit'}).click();
            await component.getByRole('button', {name: 'Update Connection'}).click();

            const calls = await getCalls(page);
            const saved = JSON.parse(calls.onChange[calls.onChange.length - 1].value);
            expect(saved).toHaveLength(1);
            expect(saved[0].provider).toBe('azure-servicebus');

            // loadConnectionForEdit replaces every populated secret with
            // SECRET_SENTINEL so the form never displays cleartext. The
            // server's mergeOneConnectionSecrets restores the stored value
            // before validation; that step is server-side, not in the form
            // round-trip we're testing here.
            expect(saved[0].azure_servicebus.connection_string).toBe('__CROSSGUARD_SECRET_UNCHANGED__');
            expect(saved[0].azure_servicebus.queue_name).toBe('sb-queue');
            expect(saved[0].nats).toBeUndefined();
            expect(saved[0].azure_queue).toBeUndefined();
            expect(saved[0].azure_blob).toBeUndefined();
        });

        test('switching provider to azure-servicebus nulls other provider blocks', async ({mount, page}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([natsConn])})}/>);
            await component.getByRole('button', {name: 'Edit'}).click();
            const providerSelect = component.locator('select').first();
            await providerSelect.selectOption('azure-servicebus');
            await component.locator('input[placeholder^="Endpoint=sb://"]').fill('Endpoint=sb://foo.servicebus.windows.net/;SharedAccessKeyName=r;SharedAccessKey=xyz');
            await component.locator('input[placeholder="crossguard-relay"]').fill('my-sb-queue');
            await component.getByRole('button', {name: 'Update Connection'}).click();

            const calls = await getCalls(page);
            const saved = JSON.parse(calls.onChange[calls.onChange.length - 1].value);
            expect(saved[0].provider).toBe('azure-servicebus');
            expect(saved[0].azure_servicebus.queue_name).toBe('my-sb-queue');
            expect(saved[0].nats).toBeUndefined();
            expect(saved[0].azure_queue).toBeUndefined();
            expect(saved[0].azure_blob).toBeUndefined();
        });

        test('file transfer toggle reveals the four blob fields', async ({mount}) => {
            const component = await mount(<ConnectionSettingsStory {...defaultProps({value: JSON.stringify([serviceBusConn])})}/>);
            await component.getByRole('button', {name: 'Edit'}).click();

            // Not visible before toggling.
            await expect(component.getByLabel('Azure Service Bus Blob Service URL')).not.toBeVisible();

            const fileCheckbox = component.getByText('Enable File Transfer').locator('..').locator('input[type="checkbox"]');
            await fileCheckbox.check();

            await expect(component.getByLabel('Azure Service Bus Blob Service URL')).toBeVisible();
            await expect(component.getByLabel('Azure Service Bus Blob Account Name')).toBeVisible();
            await expect(component.getByLabel('Azure Service Bus Blob Account Key')).toBeVisible();
            await expect(component.getByLabel('Azure Service Bus Blob Container Name')).toBeVisible();
        });
    });
});
