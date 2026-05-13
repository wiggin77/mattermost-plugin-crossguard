import {test, expect} from '@playwright/test';
import manifest from 'manifest';

test.describe('manifest', () => {
    test('plugin manifest, id and version are defined', () => {
        expect(manifest).toBeDefined();
        expect(manifest.id).toBeDefined();
        expect(manifest.version).toBeDefined();
    });

    test('has plugin ID "crossguard"', () => {
        expect(manifest.id).toBe('crossguard');
    });

    test('has name "Cross Guard"', () => {
        expect(manifest.name).toBe('Cross Guard');
    });

    test('has a semantic version string', () => {
        expect(manifest.version).toMatch(/^\d+\.\d+\.\d+/);
    });

    test('has min_server_version defined', () => {
        expect(manifest.min_server_version).toBeDefined();
        expect(typeof manifest.min_server_version).toBe('string');
    });
});

test.describe('settings_schema', () => {
    test('has settings_schema with sections array', () => {
        expect(manifest.settings_schema).toBeDefined();
        expect(Array.isArray(manifest.settings_schema.sections)).toBe(true);
    });

    test('ConnectionConfiguration section has InboundConnections and OutboundConnections custom settings', () => {
        const section = manifest.settings_schema.sections.find(
            (s: any) => s.key === 'ConnectionConfiguration',
        );
        expect(section).toBeDefined();
        const keys = section.settings.map((s: any) => s.key);
        expect(keys).toContain('InboundConnections');
        expect(keys).toContain('OutboundConnections');
        for (const setting of section.settings) {
            expect(setting.type).toBe('custom');
        }
    });

    test('RelaySettings section has RestrictToSystemAdmins bool setting', () => {
        const section = manifest.settings_schema.sections.find(
            (s: any) => s.key === 'RelaySettings',
        );
        expect(section).toBeDefined();
        const keys = section.settings.map((s: any) => s.key);
        expect(keys).toContain('RestrictToSystemAdmins');
        for (const setting of section.settings) {
            expect(setting.type).toBe('bool');
        }
    });

    test('header references documentation link path', () => {
        expect(manifest.settings_schema.header).toBeDefined();
        expect(manifest.settings_schema.header).toContain('/plugins/crossguard/public/help/help.html');
    });

    test('webapp bundle_path is defined', () => {
        expect(manifest.webapp).toBeDefined();
        expect(manifest.webapp.bundle_path).toBeDefined();
        expect(typeof manifest.webapp.bundle_path).toBe('string');
    });
});
