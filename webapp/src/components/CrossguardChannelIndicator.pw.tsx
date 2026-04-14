import React from 'react';

import CrossguardChannelIndicator from './CrossguardChannelIndicator';
import CrossguardChannelIndicatorStory from './CrossguardChannelIndicatorStory';

import {test, expect} from '../../playwright/ct-coverage';

// Note: The icon span uses an icon font class (icon-circle-multiple-outline) which
// isn't loaded in the test environment. The element exists in the DOM but has zero
// dimensions, so we use toBeAttached() instead of toBeVisible().
//
// Playwright CT's component locator doesn't track content that appears via async
// re-render (the indicator renders null initially, then re-renders after useEffect
// fires setChannelConnections). We use page.getByTestId() for dynamically rendered
// content instead of component.getByTestId().

test.describe('CrossguardChannelIndicator', () => {
    test('renders null when channel has no connections', async ({mount, page}) => {
        await mount(
            <CrossguardChannelIndicator channel={{id: 'ch-empty'}}/>,
        );
        await expect(page.getByTestId('SharedChannelIcon')).not.toBeAttached();
    });

    test('renders SharedChannelIcon when connections exist', async ({mount, page}) => {
        await mount(
            <CrossguardChannelIndicatorStory
                channelId={'ch-icon'}
                connections={'ConnectionA'}
            />,
        );
        await expect(page.getByTestId('SharedChannelIcon')).toBeAttached();
    });

    test('displays connection names as title attribute', async ({mount, page}) => {
        await mount(
            <CrossguardChannelIndicatorStory
                channelId={'ch-title'}
                connections={'ConnA, ConnB'}
            />,
        );
        await expect(page.getByTestId('SharedChannelIcon')).toHaveAttribute('title', 'ConnA, ConnB');
    });

    test('updates reactively when connections change', async ({mount, page}) => {
        const component = await mount(
            <CrossguardChannelIndicatorStory
                channelId={'ch-reactive'}
                connections={''}
            />,
        );

        // Initially no icon
        await expect(page.getByTestId('SharedChannelIcon')).not.toBeAttached();

        // Update with connections
        await component.update(
            <CrossguardChannelIndicatorStory
                channelId={'ch-reactive'}
                connections={'NewConn'}
            />,
        );

        await expect(page.getByTestId('SharedChannelIcon')).toBeAttached();
        await expect(page.getByTestId('SharedChannelIcon')).toHaveAttribute('title', 'NewConn');
    });

    test('removes icon when connections are cleared', async ({mount, page}) => {
        const component = await mount(
            <CrossguardChannelIndicatorStory
                channelId={'ch-clear'}
                connections={'SomeConn'}
            />,
        );
        await expect(page.getByTestId('SharedChannelIcon')).toBeAttached();

        await component.update(
            <CrossguardChannelIndicatorStory
                channelId={'ch-clear'}
                connections={''}
            />,
        );
        await expect(page.getByTestId('SharedChannelIcon')).not.toBeAttached();
    });

    test('shows correct title for its channel', async ({mount, page}) => {
        await mount(
            <CrossguardChannelIndicatorStory
                channelId={'ch-own'}
                connections={'OwnConn'}
            />,
        );
        await expect(page.getByTestId('SharedChannelIcon')).toHaveAttribute('title', 'OwnConn');
    });

    test('has correct CSS class on the icon element', async ({mount, page}) => {
        await mount(
            <CrossguardChannelIndicatorStory
                channelId={'ch-class'}
                connections={'X'}
            />,
        );

        const icon = page.getByTestId('SharedChannelIcon');
        await expect(icon).toBeAttached();
        const className = await icon.getAttribute('class');
        expect(className).toContain('icon');
        expect(className).toContain('icon-circle-multiple-outline');
    });

    test('handles undefined channel id gracefully', async ({mount, page}) => {
        await mount(
            <CrossguardChannelIndicator channel={{id: undefined as unknown as string}}/>,
        );
        await expect(page.getByTestId('SharedChannelIcon')).not.toBeAttached();
    });

    test('icon has correct inline styles', async ({mount, page}) => {
        await mount(
            <CrossguardChannelIndicatorStory
                channelId={'ch-style'}
                connections={'Styled'}
            />,
        );
        const icon = page.getByTestId('SharedChannelIcon');
        await expect(icon).toBeAttached();
        await expect(icon).toHaveCSS('font-size', '14px');
        await expect(icon).toHaveCSS('margin-left', '4px');
    });

    test('re-renders when channel ID prop changes to a channel without connections', async ({mount, page}) => {
        const component = await mount(
            <CrossguardChannelIndicatorStory
                channelId={'ch-switch-a'}
                connections={'HasConn'}
            />,
        );
        await expect(page.getByTestId('SharedChannelIcon')).toBeAttached();

        await component.update(
            <CrossguardChannelIndicator channel={{id: 'ch-switch-b'}}/>,
        );
        await expect(page.getByTestId('SharedChannelIcon')).not.toBeAttached();
    });

    test('empty string connections from Story treated as no connections', async ({mount, page}) => {
        await mount(
            <CrossguardChannelIndicatorStory
                channelId={'ch-empty-str'}
                connections={''}
            />,
        );
        await expect(page.getByTestId('SharedChannelIcon')).not.toBeAttached();
    });

    test.describe('Edge cases', () => {
        test('channel with undefined id renders null without crashing', async ({mount, page}) => {
            await mount(
                <CrossguardChannelIndicator channel={{id: undefined as unknown as string}}/>,
            );
            await expect(page.getByTestId('SharedChannelIcon')).not.toBeAttached();
        });

        test('tooltip title updates when connection string changes', async ({mount, page}) => {
            const component = await mount(
                <CrossguardChannelIndicatorStory
                    channelId={'ch-tooltip'}
                    connections={'ConnA'}
                />,
            );
            await expect(page.getByTestId('SharedChannelIcon')).toHaveAttribute('title', 'ConnA');

            await component.update(
                <CrossguardChannelIndicatorStory
                    channelId={'ch-tooltip'}
                    connections={'ConnA, ConnB'}
                />,
            );
            await expect(page.getByTestId('SharedChannelIcon')).toHaveAttribute('title', 'ConnA, ConnB');
        });

        test('unmounting component and then remounting with new state does not cause errors', async ({mount, page}) => {
            const component = await mount(
                <CrossguardChannelIndicatorStory
                    channelId={'ch-unmount'}
                    connections={'Active'}
                />,
            );
            await expect(page.getByTestId('SharedChannelIcon')).toBeAttached();

            await component.unmount();
            await expect(page.getByTestId('SharedChannelIcon')).not.toBeAttached();

            // Remount with updated connections after prior unmount; no crash
            await mount(
                <CrossguardChannelIndicatorStory
                    channelId={'ch-unmount'}
                    connections={'Updated'}
                />,
            );
            await expect(page.getByTestId('SharedChannelIcon')).toBeAttached();
            await expect(page.getByTestId('SharedChannelIcon')).toHaveAttribute('title', 'Updated');
        });
    });

    test.describe('Subscription lifecycle', () => {
        test('external connection update causes icon to appear', async ({mount, page}) => {
            const component = await mount(
                <CrossguardChannelIndicatorStory
                    channelId={'ch-sub-appear'}
                    connections={''}
                />,
            );
            await expect(page.getByTestId('SharedChannelIcon')).not.toBeAttached();

            await component.update(
                <CrossguardChannelIndicatorStory
                    channelId={'ch-sub-appear'}
                    connections={'NewConnection'}
                />,
            );
            await expect(page.getByTestId('SharedChannelIcon')).toBeAttached();
            await expect(page.getByTestId('SharedChannelIcon')).toHaveAttribute('title', 'NewConnection');
        });

        test('external clear causes icon to disappear', async ({mount, page}) => {
            const component = await mount(
                <CrossguardChannelIndicatorStory
                    channelId={'ch-sub-clear'}
                    connections={'ActiveConn'}
                />,
            );
            await expect(page.getByTestId('SharedChannelIcon')).toBeAttached();

            await component.update(
                <CrossguardChannelIndicatorStory
                    channelId={'ch-sub-clear'}
                    connections={''}
                />,
            );
            await expect(page.getByTestId('SharedChannelIcon')).not.toBeAttached();
        });

        test('multiple rapid updates show correct final value in title', async ({mount, page}) => {
            const component = await mount(
                <CrossguardChannelIndicatorStory
                    channelId={'ch-sub-rapid'}
                    connections={'First'}
                />,
            );
            await expect(page.getByTestId('SharedChannelIcon')).toBeAttached();

            await component.update(
                <CrossguardChannelIndicatorStory
                    channelId={'ch-sub-rapid'}
                    connections={'Second'}
                />,
            );
            await component.update(
                <CrossguardChannelIndicatorStory
                    channelId={'ch-sub-rapid'}
                    connections={'Third'}
                />,
            );

            await expect(page.getByTestId('SharedChannelIcon')).toHaveAttribute('title', 'Third');
        });
    });

    test.describe('Channel prop edge cases', () => {
        test('channel with undefined id renders null without crashing', async ({mount, page}) => {
            await mount(
                <CrossguardChannelIndicator channel={{id: undefined as unknown as string}}/>,
            );
            await expect(page.getByTestId('SharedChannelIcon')).not.toBeAttached();
        });

        test('empty connections string renders null', async ({mount, page}) => {
            await mount(
                <CrossguardChannelIndicatorStory
                    channelId={'ch-prop-empty'}
                    connections={''}
                />,
            );
            await expect(page.getByTestId('SharedChannelIcon')).not.toBeAttached();
        });

        test('title attribute matches exact connections string value', async ({mount, page}) => {
            const connectionsValue = 'ConnAlpha, ConnBeta, ConnGamma';
            await mount(
                <CrossguardChannelIndicatorStory
                    channelId={'ch-prop-title'}
                    connections={connectionsValue}
                />,
            );
            const icon = page.getByTestId('SharedChannelIcon');
            await expect(icon).toBeAttached();
            const title = await icon.getAttribute('title');
            expect(title).toBe(connectionsValue);
        });
    });
});
