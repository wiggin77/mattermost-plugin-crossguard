#!/usr/bin/env node

const { chromium } = require('playwright');
const { PDFDocument } = require('pdf-lib');
const path = require('path');
const fs = require('fs');

const HELP_DIR = path.resolve(__dirname, '..', 'public', 'help');

const HIDE_SIDEBAR_CSS = `
    .sidebar { display: none !important; }
    .content { margin-left: 0 !important; max-width: 100% !important; }
    .breadcrumb { display: none !important; }

    /* In PDF mode, soft-wrap long lines inside inline code samples so
     * realistic JSON, XML, and Go signatures do not get clipped at the
     * page margin. The ASCII diagrams under .diagram-box use bare
     * <pre> with no nested <code>, so they keep their hard newlines
     * and spacing untouched. */
    pre code {
        white-space: pre-wrap !important;
        word-break: break-all !important;
    }

    /* Allow long unbreakable strings (e.g. servicebus URLs, base64
     * payloads, GUIDs) inside inline <code> in table cells to break so
     * the table fits the page. overflow-wrap only breaks when needed,
     * so short identifiers still render in one piece. */
    td code, th code {
        overflow-wrap: anywhere !important;
    }
    table {
        table-layout: auto;
        width: 100% !important;
    }
`;

const PDF_OPTIONS = {
    format: 'Letter',
    printBackground: true,
    margin: { top: '0.5in', bottom: '0.5in', left: '0.5in', right: '0.5in' },
};

async function generatePDF(page, htmlFile) {
    const filePath = path.join(HELP_DIR, htmlFile);
    await page.goto(`file://${filePath}`, { waitUntil: 'networkidle' });
    await page.addStyleTag({ content: HIDE_SIDEBAR_CSS });
    return page.pdf(PDF_OPTIONS);
}

async function mergePDFs(pdfBuffers) {
    const merged = await PDFDocument.create();
    for (const buffer of pdfBuffers) {
        const doc = await PDFDocument.load(buffer);
        const pages = await merged.copyPages(doc, doc.getPageIndices());
        for (const p of pages) {
            merged.addPage(p);
        }
    }
    return merged.save();
}

async function main() {
    console.log('Launching browser...');
    const browser = await chromium.launch();
    const context = await browser.newContext();
    const page = await context.newPage();

    // Single-file PDFs
    const singles = [
        { html: 'whitepaper.html', output: 'crossguard-whitepaper.pdf' },
        { html: 'threatmodel.html', output: 'crossguard-threatmodel.pdf' },
        { html: 'transport-interface.html', output: 'crossguard-transport-interface.pdf' },
    ];

    for (const { html, output } of singles) {
        console.log(`Generating ${output} from ${html}...`);
        const buffer = await generatePDF(page, html);
        const outPath = path.join(HELP_DIR, output);
        fs.writeFileSync(outPath, buffer);
        console.log(`  Wrote ${outPath}`);
    }

    // Combined help PDF (4 pages merged)
    const helpPages = ['help.html', 'commands.html', 'admin.html', 'api.html', 'error-codes.html'];
    const helpBuffers = [];
    for (const html of helpPages) {
        console.log(`Generating ${html} section for crossguard-help.pdf...`);
        helpBuffers.push(await generatePDF(page, html));
    }

    console.log('Merging help sections...');
    const mergedBytes = await mergePDFs(helpBuffers);
    const helpOutPath = path.join(HELP_DIR, 'crossguard-help.pdf');
    fs.writeFileSync(helpOutPath, mergedBytes);
    console.log(`  Wrote ${helpOutPath}`);

    await browser.close();
    console.log('Done. Generated 4 PDFs.');
}

main().catch((err) => {
    console.error('PDF generation failed:', err);
    process.exit(1);
});
