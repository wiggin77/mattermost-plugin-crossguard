package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot returns the repository root, derived from this test file's
// location: scripts/generate-error-codes/main_test.go is two levels deep,
// so the parent's parent is the repo root.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func TestGeneratorEndToEnd(t *testing.T) {
	root := repoRoot(t)
	codesPath := filepath.Join(root, "server", "errcode", "codes.go")
	annotationsPath := filepath.Join(root, "scripts", "generate-error-codes", "annotations.yaml")
	templatePath := filepath.Join(root, "scripts", "generate-error-codes", "error-codes.html.tmpl")

	out, err := run(codesPath, annotationsPath, templatePath)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	s := string(out)

	// Output should mention every block header from codes.go.
	headers, err := parseBlockHeaders(codesPath)
	if err != nil {
		t.Fatalf("parseBlockHeaders: %v", err)
	}
	for _, h := range headers {
		if !strings.Contains(s, h.path) {
			t.Errorf("rendered output is missing block header %q", h.path)
		}
	}

	// Output should mention every parsed constant by name.
	codes, _, err := parseCodes(codesPath)
	if err != nil {
		t.Fatalf("parseCodes: %v", err)
	}
	if len(codes) == 0 {
		t.Fatal("no codes parsed")
	}
	for _, c := range codes {
		if !strings.Contains(s, c.Name) {
			t.Errorf("rendered output is missing constant %q", c.Name)
		}
	}
}

func TestCodesAreUnique(t *testing.T) {
	root := repoRoot(t)
	codesPath := filepath.Join(root, "server", "errcode", "codes.go")
	codes, _, err := parseCodes(codesPath)
	if err != nil {
		t.Fatalf("parseCodes: %v", err)
	}
	seen := map[int]string{}
	for _, c := range codes {
		if prev, dup := seen[c.Value]; dup {
			t.Errorf("duplicate code %d: %s and %s", c.Value, prev, c.Name)
		}
		seen[c.Value] = c.Name
	}
}

func TestCodesFallInsideDeclaredBlocks(t *testing.T) {
	root := repoRoot(t)
	codesPath := filepath.Join(root, "server", "errcode", "codes.go")
	codes, _, err := parseCodes(codesPath)
	if err != nil {
		t.Fatalf("parseCodes: %v", err)
	}
	headers, err := parseBlockHeaders(codesPath)
	if err != nil {
		t.Fatalf("parseBlockHeaders: %v", err)
	}
	for _, c := range codes {
		matched := false
		for _, h := range headers {
			if c.Value >= h.start && c.Value <= h.end {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("constant %s = %d is outside every declared block range", c.Name, c.Value)
		}
	}
}

func TestAnnotationsReferToRealConstants(t *testing.T) {
	root := repoRoot(t)
	codesPath := filepath.Join(root, "server", "errcode", "codes.go")
	annotationsPath := filepath.Join(root, "scripts", "generate-error-codes", "annotations.yaml")

	codes, _, err := parseCodes(codesPath)
	if err != nil {
		t.Fatalf("parseCodes: %v", err)
	}
	ann, err := loadAnnotations(annotationsPath)
	if err != nil {
		t.Fatalf("loadAnnotations: %v", err)
	}

	names := map[string]struct{}{}
	for _, c := range codes {
		names[c.Name] = struct{}{}
	}
	for name := range ann.Codes {
		if _, ok := names[name]; !ok {
			t.Errorf("annotations.yaml references unknown constant %q", name)
		}
	}

	headers, err := parseBlockHeaders(codesPath)
	if err != nil {
		t.Fatalf("parseBlockHeaders: %v", err)
	}
	headerPaths := map[string]struct{}{}
	for _, h := range headers {
		headerPaths[h.path] = struct{}{}
	}
	for path := range ann.Blocks {
		if _, ok := headerPaths[path]; !ok {
			t.Errorf("annotations.yaml references unknown block path %q", path)
		}
	}
}

func TestExtractLevelAndDescription(t *testing.T) {
	tests := []struct {
		input     string
		wantLevel string
		wantDesc  string
	}{
		{"", "", ""},
		{"Error: something failed.", "ERROR", "something failed."},
		{"Warn: heads up.", "WARN", "heads up."},
		{"Info: all good.", "INFO", "all good."},
		{"Debug: trace.", "DEBUG", "trace."},
		{"AzureSPAuditConstructed is emitted once per success.", "", "AzureSPAuditConstructed is emitted once per success."},
		{"  Multi-line\n  comment with\n  whitespace.", "", "Multi-line comment with whitespace."},
	}
	for _, tc := range tests {
		gotLevel, gotDesc := extractLevelAndDescription(tc.input)
		if gotLevel != tc.wantLevel || gotDesc != tc.wantDesc {
			t.Errorf("extractLevelAndDescription(%q) = (%q, %q); want (%q, %q)",
				tc.input, gotLevel, gotDesc, tc.wantLevel, tc.wantDesc)
		}
	}
}

func TestMatchBlockHeader(t *testing.T) {
	tests := []struct {
		input     string
		wantOK    bool
		wantPath  string
		wantStart int
		wantEnd   int
	}{
		{"hooks.go (10000-10999)", true, "hooks.go", 10000, 10999},
		{"store/caching.go (23000-23999)", true, "store/caching.go", 23000, 23999},
		{"wire/* (29000-29999)", true, "wire/*", 29000, 29999},
		{"wire/* (29000-29999). Additional prose.", true, "wire/*", 29000, 29999},
		{"This is not a header", false, "", 0, 0},
		{"prose with (parens) but no range", false, "", 0, 0},
		{"foo.go (not-a-range)", false, "", 0, 0},
	}
	for _, tc := range tests {
		h, ok := matchBlockHeader(tc.input)
		if ok != tc.wantOK {
			t.Errorf("matchBlockHeader(%q) ok = %v; want %v", tc.input, ok, tc.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if h.path != tc.wantPath || h.start != tc.wantStart || h.end != tc.wantEnd {
			t.Errorf("matchBlockHeader(%q) = (%q, %d-%d); want (%q, %d-%d)",
				tc.input, h.path, h.start, h.end, tc.wantPath, tc.wantStart, tc.wantEnd)
		}
	}
}

func TestAnchorOf(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"hooks.go", "hooks"},
		{"store/caching.go", "store-caching"},
		{"azure_servicebus_provider.go", "azure-servicebus-provider"},
		{"wire/*", "wire"},
	}
	for _, tc := range tests {
		if got := anchorOf(tc.in); got != tc.want {
			t.Errorf("anchorOf(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}
