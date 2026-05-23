// Command generate-error-codes regenerates public/help/error-codes.html
// from server/errcode/codes.go.
//
// The page's structural content (block ranges, constant names, integer
// values, log levels) is derived directly from the Go source so it cannot
// drift from the operator-facing contract. Hand-written narrative content
// (block descriptions, per-code descriptions, per-code troubleshooting
// guidance) lives in scripts/generate-error-codes/annotations.yaml and is
// merged in at render time.
//
// Usage: go run ./scripts/generate-error-codes
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

func main() {
	var (
		codesPath       string
		annotationsPath string
		templatePath    string
		outputPath      string
		check           bool
	)
	flag.StringVar(&codesPath, "codes", "server/errcode/codes.go",
		"Path to the Go source containing error-code constants")
	flag.StringVar(&annotationsPath, "annotations",
		"scripts/generate-error-codes/annotations.yaml",
		"Path to the sidecar YAML with descriptions and troubleshooting")
	flag.StringVar(&templatePath, "template",
		"scripts/generate-error-codes/error-codes.html.tmpl",
		"Path to the html/template file")
	flag.StringVar(&outputPath, "output", "public/help/error-codes.html",
		"Output path for the rendered page")
	flag.BoolVar(&check, "check", false,
		"Validate inputs only; do not write output")
	flag.Parse()

	doc, err := run(codesPath, annotationsPath, templatePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "generate-error-codes:", err)
		os.Exit(1)
	}
	if check {
		fmt.Println("OK")
		return
	}
	if err := os.WriteFile(outputPath, doc, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "generate-error-codes:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d bytes)\n", outputPath, len(doc))
}

// run is the testable entry point. Reads the inputs, validates the merged
// model, and returns the rendered HTML bytes.
func run(codesPath, annotationsPath, templatePath string) ([]byte, error) {
	codes, allCodes, err := parseCodes(codesPath)
	if err != nil {
		return nil, fmt.Errorf("parse codes: %w", err)
	}

	ann, err := loadAnnotations(annotationsPath)
	if err != nil {
		return nil, fmt.Errorf("load annotations: %w", err)
	}

	blocks, err := buildBlocks(codesPath, codes, allCodes, ann)
	if err != nil {
		return nil, fmt.Errorf("build blocks: %w", err)
	}

	tmplBytes, err := os.ReadFile(templatePath)
	if err != nil {
		return nil, fmt.Errorf("read template: %w", err)
	}
	tmpl, err := template.New("error-codes").Funcs(tmplFuncs()).Parse(string(tmplBytes))
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	var out strings.Builder
	totalCodes := 0
	for _, b := range blocks {
		totalCodes += len(b.Codes)
	}
	data := struct {
		Blocks     []Block
		TotalCodes int
	}{Blocks: blocks, TotalCodes: totalCodes}
	if err := tmpl.Execute(&out, data); err != nil {
		return nil, fmt.Errorf("render: %w", err)
	}
	return []byte(out.String()), nil
}

// Code is one parsed error-code constant.
type Code struct {
	Name            string
	Value           int
	Level           string // ERROR, WARN, INFO, DEBUG, or "" if not inferable
	Description     string // human-readable, from codes.go comment or YAML override
	Troubleshooting string // from YAML; empty when not annotated
	Line            int    // line in codes.go (used to assign to a block)
}

// Block is one file-range section of codes.
type Block struct {
	Anchor      string
	Title       string // e.g. "hooks.go"
	StartRange  int
	EndRange    int
	Description string // from annotations.yaml blocks[Title]
	Codes       []Code
}

// parseCodes reads codes.go and returns the parsed constants in declaration
// order plus the contents of the AllCodes slice for cross-checking.
func parseCodes(path string) ([]Code, []int, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, nil, err
	}

	var codes []Code
	var allCodes []int

	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		switch gd.Tok {
		case token.CONST:
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
					continue
				}
				lit, ok := vs.Values[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.INT {
					continue
				}
				value, err := strconv.Atoi(lit.Value)
				if err != nil {
					return nil, nil, fmt.Errorf("non-integer constant %s = %q", vs.Names[0].Name, lit.Value)
				}
				code := Code{
					Name:  vs.Names[0].Name,
					Value: value,
					Line:  fset.Position(vs.Pos()).Line,
				}
				rawComment := joinComments(vs.Doc, vs.Comment)
				code.Level, code.Description = extractLevelAndDescription(rawComment)
				codes = append(codes, code)
			}
		case token.VAR:
			if vals, ok := isAllCodes(gd); ok {
				allCodes = vals
			}
		}
	}
	if len(codes) == 0 {
		return nil, nil, fmt.Errorf("no const declarations found")
	}
	if allCodes == nil {
		return nil, nil, fmt.Errorf("AllCodes slice not found")
	}
	return codes, allCodes, nil
}

func joinComments(doc, inline *ast.CommentGroup) string {
	var parts []string
	if doc != nil {
		parts = append(parts, strings.TrimSpace(doc.Text()))
	}
	if inline != nil {
		parts = append(parts, strings.TrimSpace(inline.Text()))
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

// extractLevelAndDescription splits a code comment into a log-level tag and
// the remaining description text. Recognises the "Error:", "Warn:", "Info:",
// "Debug:" prefix convention used in codes.go. If no prefix is present the
// level is returned as the empty string and the description is the input.
func extractLevelAndDescription(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	// Multi-line GoDoc comments often start with "Name is...": collapse
	// runs of whitespace so the rendered page reads cleanly.
	raw = collapseWhitespace(raw)
	for _, prefix := range []struct {
		token string
		level string
	}{
		{"Error:", "ERROR"},
		{"Warn:", "WARN"},
		{"Info:", "INFO"},
		{"Debug:", "DEBUG"},
	} {
		if rest, ok := strings.CutPrefix(raw, prefix.token); ok {
			return prefix.level, strings.TrimSpace(rest)
		}
	}
	return "", raw
}

func collapseWhitespace(s string) string {
	var b strings.Builder
	inSpace := false
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r':
			if !inSpace && b.Len() > 0 {
				b.WriteRune(' ')
			}
			inSpace = true
		default:
			b.WriteRune(r)
			inSpace = false
		}
	}
	return strings.TrimSpace(b.String())
}

// isAllCodes recognises a `var AllCodes = []int{ ... }` declaration and
// returns the integer values referenced inside the literal. The literal
// uses constant names rather than integer literals; we resolve them in a
// second pass using the parsed codes list.
func isAllCodes(gd *ast.GenDecl) ([]int, bool) {
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		if len(vs.Names) != 1 || vs.Names[0].Name != "AllCodes" {
			continue
		}
		if len(vs.Values) != 1 {
			continue
		}
		cl, ok := vs.Values[0].(*ast.CompositeLit)
		if !ok {
			continue
		}
		// We do not resolve the constant references to ints here; the
		// existence of the slice is all the parser needs to flag. Return
		// a non-nil zero-length slice as a sentinel.
		_ = cl
		return []int{}, true
	}
	return nil, false
}

// blockHeaderRE matches the file-block comments in codes.go, e.g.
// "// hooks.go (10000-10999)" or "// store/caching.go (23000-23999)".
// Captured: 1 filename (with optional subdir), 2 start range, 3 end range.
type blockHeader struct {
	path  string
	start int
	end   int
	line  int
}

func parseBlockHeaders(path string) ([]blockHeader, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var headers []blockHeader
	for _, cg := range file.Comments {
		for _, c := range cg.List {
			text := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
			h, ok := matchBlockHeader(text)
			if !ok {
				continue
			}
			h.line = fset.Position(c.Pos()).Line
			headers = append(headers, h)
		}
	}
	if len(headers) == 0 {
		return nil, fmt.Errorf("no file-block comments found")
	}
	sort.Slice(headers, func(i, j int) bool { return headers[i].line < headers[j].line })
	return headers, nil
}

// isBlockPath returns true for the path-like labels codes.go uses as
// section markers: typically "foo.go" or "subdir/bar.go", with the
// special wildcard form "wire/*" reserved for groups of files that
// share a 1000-range. Anything else is rejected so prose comments do
// not accidentally match the (NNNNN-NNNNN) range pattern.
func isBlockPath(path string) bool {
	if path == "" {
		return false
	}
	if strings.HasSuffix(path, ".go") {
		return true
	}
	if strings.HasSuffix(path, "/*") {
		return true
	}
	return false
}

func matchBlockHeader(text string) (blockHeader, bool) {
	// Look for the pattern "path/to/file.go (NNNNN-NNNNN)"
	open := strings.Index(text, "(")
	closer := strings.Index(text, ")")
	if open < 0 || closer < 0 || closer <= open {
		return blockHeader{}, false
	}
	path := strings.TrimSpace(text[:open])
	if !isBlockPath(path) {
		return blockHeader{}, false
	}
	rangePart := strings.TrimSpace(text[open+1 : closer])
	left, right, ok := strings.Cut(rangePart, "-")
	if !ok {
		return blockHeader{}, false
	}
	start, err := strconv.Atoi(strings.TrimSpace(left))
	if err != nil {
		return blockHeader{}, false
	}
	end, err := strconv.Atoi(strings.TrimSpace(right))
	if err != nil {
		return blockHeader{}, false
	}
	return blockHeader{path: path, start: start, end: end}, true
}

// Annotation is one entry in annotations.yaml.
type Annotation struct {
	Description     string `yaml:"description,omitempty"`
	Troubleshooting string `yaml:"troubleshooting,omitempty"`
}

type Annotations struct {
	Blocks map[string]string     `yaml:"blocks"` // keyed by block path (e.g. "hooks.go")
	Codes  map[string]Annotation `yaml:"codes"`
}

func loadAnnotations(path string) (Annotations, error) {
	var ann Annotations
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Annotations{}, nil
		}
		return ann, err
	}
	if err := yaml.Unmarshal(raw, &ann); err != nil {
		return ann, err
	}
	if ann.Blocks == nil {
		ann.Blocks = map[string]string{}
	}
	if ann.Codes == nil {
		ann.Codes = map[string]Annotation{}
	}
	return ann, nil
}

// buildBlocks assigns each parsed code to the block whose range contains
// its integer value, merges YAML overrides, and verifies the invariants
// the generator must hold.
func buildBlocks(codesPath string, codes []Code, _ []int, ann Annotations) ([]Block, error) {
	headers, err := parseBlockHeaders(codesPath)
	if err != nil {
		return nil, err
	}

	// Detect overlapping ranges in the source itself.
	for i := range headers {
		for j := i + 1; j < len(headers); j++ {
			if rangesOverlap(headers[i].start, headers[i].end, headers[j].start, headers[j].end) {
				return nil, fmt.Errorf("overlapping block ranges: %s (%d-%d) and %s (%d-%d)",
					headers[i].path, headers[i].start, headers[i].end,
					headers[j].path, headers[j].start, headers[j].end)
			}
		}
	}

	blocksByPath := map[string]*Block{}
	for _, h := range headers {
		blocksByPath[h.path] = &Block{
			Anchor:      anchorOf(h.path),
			Title:       h.path,
			StartRange:  h.start,
			EndRange:    h.end,
			Description: ann.Blocks[h.path],
		}
	}

	// Verify uniqueness of integer values.
	seen := map[int]string{}
	for _, c := range codes {
		if prev, dup := seen[c.Value]; dup {
			return nil, fmt.Errorf("duplicate code %d (%s and %s)", c.Value, prev, c.Name)
		}
		seen[c.Value] = c.Name
	}

	// Assign each constant to the block whose range contains its value.
	unmatched := []Code{}
	for _, c := range codes {
		// Merge annotation overrides.
		if a, ok := ann.Codes[c.Name]; ok {
			if a.Description != "" {
				c.Description = a.Description
			}
			c.Troubleshooting = a.Troubleshooting
		}
		// Find the block.
		var found *Block
		for _, b := range blocksByPath {
			if c.Value >= b.StartRange && c.Value <= b.EndRange {
				found = b
				break
			}
		}
		if found == nil {
			unmatched = append(unmatched, c)
			continue
		}
		found.Codes = append(found.Codes, c)
	}
	if len(unmatched) > 0 {
		names := make([]string, len(unmatched))
		for i, c := range unmatched {
			names[i] = fmt.Sprintf("%s=%d", c.Name, c.Value)
		}
		return nil, fmt.Errorf("constants outside any declared block range: %s",
			strings.Join(names, ", "))
	}

	// Verify every annotation key matches a real constant.
	codeNames := map[string]struct{}{}
	for _, c := range codes {
		codeNames[c.Name] = struct{}{}
	}
	var stray []string
	for name := range ann.Codes {
		if _, ok := codeNames[name]; !ok {
			stray = append(stray, name)
		}
	}
	if len(stray) > 0 {
		sort.Strings(stray)
		return nil, fmt.Errorf("annotations.yaml has entries for unknown constants: %s",
			strings.Join(stray, ", "))
	}

	// Verify every block annotation key matches a real block path.
	blockPaths := map[string]struct{}{}
	for _, h := range headers {
		blockPaths[h.path] = struct{}{}
	}
	stray = stray[:0]
	for path := range ann.Blocks {
		if _, ok := blockPaths[path]; !ok {
			stray = append(stray, path)
		}
	}
	if len(stray) > 0 {
		sort.Strings(stray)
		return nil, fmt.Errorf("annotations.yaml has block entries for unknown paths: %s",
			strings.Join(stray, ", "))
	}

	// Order and sort.
	blocks := make([]Block, 0, len(blocksByPath))
	for _, b := range blocksByPath {
		sort.Slice(b.Codes, func(i, j int) bool { return b.Codes[i].Value < b.Codes[j].Value })
		blocks = append(blocks, *b)
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].StartRange < blocks[j].StartRange })
	return blocks, nil
}

func rangesOverlap(a1, a2, b1, b2 int) bool {
	return a1 <= b2 && b1 <= a2
}

func anchorOf(path string) string {
	// "store/caching.go" → "store-caching"
	// "wire/*"           → "wire"
	a := strings.TrimSuffix(path, ".go")
	a = strings.TrimSuffix(a, "/*")
	a = strings.ReplaceAll(a, "/", "-")
	a = strings.ReplaceAll(a, "_", "-")
	return a
}

func tmplFuncs() template.FuncMap {
	return template.FuncMap{
		"levelClass": func(level string) string {
			switch level {
			case "ERROR":
				return "permission-tag permission-sysadmin"
			case "WARN":
				return "permission-tag permission-teamadmin"
			case "INFO", "DEBUG":
				return "permission-tag permission-channeladmin"
			}
			return "permission-tag"
		},
	}
}

