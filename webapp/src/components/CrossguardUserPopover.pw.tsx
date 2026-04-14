import React from 'react';

import CrossguardUserPopover from './CrossguardUserPopover';

import {test, expect} from '../../playwright/ct-coverage';

test.describe('CrossguardUserPopover', () => {
    test('renders null when user has no props object', async ({mount}) => {
        const component = await mount(<CrossguardUserPopover user={{}}/>);
        await expect(component).toBeEmpty();
    });

    test('renders null when user.props exists but has no CrossguardRemoteUsername', async ({mount}) => {
        const component = await mount(
            <CrossguardUserPopover user={{props: {other: 'value'}}}/>,
        );
        await expect(component).toBeEmpty();
    });

    test('renders null when CrossguardRemoteUsername is empty string', async ({mount}) => {
        const component = await mount(
            <CrossguardUserPopover user={{props: {CrossguardRemoteUsername: ''}}}/>,
        );
        await expect(component).toBeEmpty();
    });

    test('renders relay info with valid props', async ({mount}) => {
        const component = await mount(
            <CrossguardUserPopover
                user={{
                    props: {CrossguardRemoteUsername: 'alice'},
                    last_name: '(via ServerB)',
                }}
            />,
        );
        await expect(component.getByText('Relayed from: alice (via ServerB)')).toBeVisible();
    });

    test('extracts connection name from last_name regex', async ({mount}) => {
        const component = await mount(
            <CrossguardUserPopover
                user={{
                    props: {CrossguardRemoteUsername: 'bob'},
                    last_name: '(via my-conn)',
                }}
            />,
        );
        await expect(component.getByText('Relayed from: bob (via my-conn)')).toBeVisible();
    });

    test('shows unknown when last_name does not match regex pattern', async ({mount}) => {
        const component = await mount(
            <CrossguardUserPopover
                user={{
                    props: {CrossguardRemoteUsername: 'carol'},
                    last_name: 'NotAMatch',
                }}
            />,
        );
        await expect(component.getByText('Relayed from: carol (via unknown)')).toBeVisible();
    });

    test('shows unknown when last_name is undefined', async ({mount}) => {
        const component = await mount(
            <CrossguardUserPopover
                user={{props: {CrossguardRemoteUsername: 'dave'}}}
            />,
        );
        await expect(component.getByText('Relayed from: dave (via unknown)')).toBeVisible();
    });

    test('shows unknown when last_name is empty string', async ({mount}) => {
        const component = await mount(
            <CrossguardUserPopover
                user={{
                    props: {CrossguardRemoteUsername: 'eve'},
                    last_name: '',
                }}
            />,
        );
        await expect(component.getByText('Relayed from: eve (via unknown)')).toBeVisible();
    });

    test('extracts connection name with spaces', async ({mount}) => {
        const component = await mount(
            <CrossguardUserPopover
                user={{
                    props: {CrossguardRemoteUsername: 'frank'},
                    last_name: '(via Server With Spaces)',
                }}
            />,
        );
        await expect(component.getByText('Relayed from: frank (via Server With Spaces)')).toBeVisible();
    });

    test('does not match partial last_name with surrounding text', async ({mount}) => {
        const component = await mount(
            <CrossguardUserPopover
                user={{
                    props: {CrossguardRemoteUsername: 'grace'},
                    last_name: 'prefix (via Foo) suffix',
                }}
            />,
        );

        // Regex uses ^ and $ anchors, so partial match fails and falls back to "unknown"
        await expect(component.getByText('Relayed from: grace (via unknown)')).toBeVisible();
    });

    test('handles user.props being explicitly undefined', async ({mount}) => {
        const component = await mount(
            <CrossguardUserPopover user={{props: undefined}}/>,
        );
        await expect(component).toBeEmpty();
    });

    test('renders null when user object is null', async ({mount}) => {
        const component = await mount(
            <CrossguardUserPopover user={null as any}/>,
        );
        await expect(component).toBeEmpty();
    });

    test('handles connection name with special characters in last_name', async ({mount}) => {
        const component = await mount(
            <CrossguardUserPopover
                user={{
                    props: {CrossguardRemoteUsername: 'alice'},
                    last_name: '(via server-2/alpha)',
                }}
            />,
        );
        await expect(component.getByText('Relayed from: alice (via server-2/alpha)')).toBeVisible();
    });

    test('handles very long remote username', async ({mount}) => {
        const longName = 'u'.repeat(500);
        const component = await mount(
            <CrossguardUserPopover
                user={{
                    props: {CrossguardRemoteUsername: longName},
                    last_name: '(via conn)',
                }}
            />,
        );
        const text = await component.locator('span').textContent();
        expect(text).toContain(longName);
    });

    test('shows unknown when last_name is "(via )" with empty connection after via', async ({mount}) => {
        const component = await mount(
            <CrossguardUserPopover
                user={{
                    props: {CrossguardRemoteUsername: 'bob'},
                    last_name: '(via )',
                }}
            />,
        );
        await expect(component.getByText('(via unknown)')).toBeVisible();
    });

    test('handles numeric string CrossguardRemoteUsername', async ({mount}) => {
        const component = await mount(
            <CrossguardUserPopover
                user={{
                    props: {CrossguardRemoteUsername: '12345'},
                    last_name: '(via conn)',
                }}
            />,
        );
        await expect(component.getByText('Relayed from: 12345 (via conn)')).toBeVisible();
    });
});

test.describe('CrossguardUserPopover - parsing and null/undefined edge cases', () => {
    test('last_name "(via )" with only closing paren after via falls back to unknown', async ({mount}) => {
        const component = await mount(
            <CrossguardUserPopover
                user={{
                    props: {CrossguardRemoteUsername: 'alice'},
                    last_name: '(via )',
                }}
            />,
        );
        await expect(component.getByText('Relayed from: alice (via unknown)')).toBeVisible();
    });

    test('last_name undefined falls back to unknown', async ({mount}) => {
        const component = await mount(
            <CrossguardUserPopover
                user={{
                    props: {CrossguardRemoteUsername: 'bob'},
                    last_name: undefined,
                }}
            />,
        );
        await expect(component.getByText('Relayed from: bob (via unknown)')).toBeVisible();
    });

    test('last_name without via pattern falls back to unknown', async ({mount}) => {
        const component = await mount(
            <CrossguardUserPopover
                user={{
                    props: {CrossguardRemoteUsername: 'carol'},
                    last_name: 'Smith',
                }}
            />,
        );
        await expect(component.getByText('Relayed from: carol (via unknown)')).toBeVisible();
    });

    test('last_name with hyphenated connection name extracts correctly', async ({mount}) => {
        const component = await mount(
            <CrossguardUserPopover
                user={{
                    props: {CrossguardRemoteUsername: 'dave'},
                    last_name: '(via multi-word-connection)',
                }}
            />,
        );
        await expect(component.getByText('Relayed from: dave (via multi-word-connection)')).toBeVisible();
    });

    test('props as empty object with no CrossguardRemoteUsername renders empty', async ({mount}) => {
        const component = await mount(
            <CrossguardUserPopover user={{props: {}}}/>,
        );
        await expect(component).toBeEmpty();
    });

    test('props explicitly set to undefined renders empty', async ({mount}) => {
        const component = await mount(
            <CrossguardUserPopover user={{props: undefined}}/>,
        );
        await expect(component).toBeEmpty();
    });

    test('user object with no props key renders empty', async ({mount}) => {
        const component = await mount(
            <CrossguardUserPopover user={{last_name: 'test'} as any}/>,
        );
        await expect(component).toBeEmpty();
    });

    test('CrossguardRemoteUsername as empty string renders empty', async ({mount}) => {
        const component = await mount(
            <CrossguardUserPopover
                user={{
                    props: {CrossguardRemoteUsername: ''},
                    last_name: '(via SomeConn)',
                }}
            />,
        );
        await expect(component).toBeEmpty();
    });
});
