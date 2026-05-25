// Command xsd2jsonschema generates a JSON Schema (draft 2020-12) from
// schema/crossguard.xsd. The output is the JSON-encoding contract that
// the wire JSON marshalers in server/wire/ are written to and that the
// compliance pipeline ingests alongside the XSD.
//
// The generator is intentionally not a general-purpose tool. It handles
// only the XSD constructs present in crossguard.xsd:
//
//   - xs:simpleType with xs:restriction (base=xs:string + xs:pattern +
//     xs:maxLength, or xs:enumeration, or base=xs:dateTime).
//   - xs:complexType with xs:sequence of xs:element children.
//   - xs:complexType with xs:complexContent / xs:extension (inlined
//     into the using element).
//   - xs:attribute (rendered as a sibling object property).
//   - xs:choice (children rendered as optional sibling properties; the
//     XSD remains the mutual-exclusion gate).
//   - Anonymous (inline) complexTypes inside xs:element.
//   - Container complexTypes whose sequence holds a single element with
//     maxOccurs > 1 (the JSON form is an array of the child schema,
//     per the "repeated elements become JSON arrays" convention).
//
// Any other XSD construct is a hard error pointing at the unsupported
// element so a future XSD change either fits the existing ruleset or
// forces a generator update.
package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"maps"
	"os"
	"sort"
	"strconv"
	"strings"
)

// JSONSchemaURI identifies the draft this generator targets.
const JSONSchemaURI = "https://json-schema.org/draft/2020-12/schema"

// SchemaID is the canonical URL where the JSON Schema document lives.
// Kept in lockstep with schema/crossguard.xsd so the two artifacts are
// discoverable as a pair.
const SchemaID = "https://raw.githubusercontent.com/MattermostFederal/mattermost-plugin-crossguard/main/schema/crossguard.schema.json"

// ----------------------------------------------------------------------
// XSD AST. We parse only the constructs we support; an unrecognized
// element triggers a hard error at generation time, not at parse time.
// ----------------------------------------------------------------------

type xsdSchema struct {
	XMLName      xml.Name         `xml:"schema"`
	SimpleTypes  []xsdSimpleType  `xml:"simpleType"`
	ComplexTypes []xsdComplexType `xml:"complexType"`
	Elements     []xsdElement     `xml:"element"`
}

type xsdSimpleType struct {
	Name        string         `xml:"name,attr"`
	Restriction xsdRestriction `xml:"restriction"`
}

type xsdRestriction struct {
	Base         string         `xml:"base,attr"`
	Pattern      *xsdValueAttr  `xml:"pattern"`
	MaxLength    *xsdValueAttr  `xml:"maxLength"`
	Enumerations []xsdValueAttr `xml:"enumeration"`
}

type xsdValueAttr struct {
	Value string `xml:"value,attr"`
}

type xsdComplexType struct {
	Name           string             `xml:"name,attr"`
	Sequence       *xsdSequence       `xml:"sequence"`
	Attributes     []xsdAttribute     `xml:"attribute"`
	ComplexContent *xsdComplexContent `xml:"complexContent"`
}

type xsdComplexContent struct {
	Extension *xsdExtension `xml:"extension"`
}

type xsdExtension struct {
	Base       string         `xml:"base,attr"`
	Attributes []xsdAttribute `xml:"attribute"`
	Sequence   *xsdSequence   `xml:"sequence"`
}

type xsdSequence struct {
	Elements []xsdElement `xml:"element"`
	Choices  []xsdChoice  `xml:"choice"`
}

type xsdChoice struct {
	MinOccurs string       `xml:"minOccurs,attr"`
	MaxOccurs string       `xml:"maxOccurs,attr"`
	Elements  []xsdElement `xml:"element"`
}

type xsdElement struct {
	Name        string          `xml:"name,attr"`
	Type        string          `xml:"type,attr"`
	MinOccurs   string          `xml:"minOccurs,attr"`
	MaxOccurs   string          `xml:"maxOccurs,attr"`
	ComplexType *xsdComplexType `xml:"complexType"`
}

type xsdAttribute struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
	Use  string `xml:"use,attr"`
}

// ----------------------------------------------------------------------
// Generator entry point.
// ----------------------------------------------------------------------

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: xsd2jsonschema <path-to-xsd>")
		os.Exit(2)
	}
	in, err := os.ReadFile(os.Args[1]) //nolint:gosec // single-shot CLI tool, path comes from caller
	if err != nil {
		fmt.Fprintf(os.Stderr, "read xsd: %v\n", err)
		os.Exit(1)
	}
	var schema xsdSchema
	if err := xml.Unmarshal(in, &schema); err != nil {
		fmt.Fprintf(os.Stderr, "parse xsd: %v\n", err)
		os.Exit(1)
	}
	out, err := generate(&schema)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate: %v\n", err)
		os.Exit(1)
	}
	buf, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal: %v\n", err)
		os.Exit(1)
	}
	// Trailing newline keeps the file POSIX-friendly for diff-driven CI.
	buf = append(buf, '\n')
	if _, err := os.Stdout.Write(buf); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}
}

// ----------------------------------------------------------------------
// Schema generation.
// ----------------------------------------------------------------------

type gen struct {
	simpleByName  map[string]*xsdSimpleType
	complexByName map[string]*xsdComplexType
	defs          map[string]any
}

func generate(schema *xsdSchema) (map[string]any, error) {
	g := &gen{
		simpleByName:  make(map[string]*xsdSimpleType),
		complexByName: make(map[string]*xsdComplexType),
		defs:          make(map[string]any),
	}
	for i := range schema.SimpleTypes {
		st := &schema.SimpleTypes[i]
		g.simpleByName[st.Name] = st
	}
	for i := range schema.ComplexTypes {
		ct := &schema.ComplexTypes[i]
		g.complexByName[ct.Name] = ct
	}

	for _, name := range sortedKeys(g.simpleByName) {
		s, err := g.genSimpleType(g.simpleByName[name])
		if err != nil {
			return nil, fmt.Errorf("simpleType %s: %w", name, err)
		}
		g.defs[name] = s
	}
	for _, name := range sortedKeys(g.complexByName) {
		s, err := g.genComplexType(g.complexByName[name])
		if err != nil {
			return nil, fmt.Errorf("complexType %s: %w", name, err)
		}
		g.defs[name] = s
	}

	if len(schema.Elements) != 1 {
		return nil, fmt.Errorf("expected exactly one top-level element, got %d", len(schema.Elements))
	}
	root := schema.Elements[0]
	rootSchema, err := g.genRootElement(&root)
	if err != nil {
		return nil, fmt.Errorf("root element %s: %w", root.Name, err)
	}

	out := map[string]any{
		"$schema":     JSONSchemaURI,
		"$id":         SchemaID,
		"title":       root.Name,
		"description": "Cross Guard wire envelope, JSON encoding. Mechanically derived from schema/crossguard.xsd by build/xsd2jsonschema. Do not edit by hand. Run 'make generate-json-schema' to regenerate.",
		"$defs":       g.defs,
	}
	maps.Copy(out, rootSchema)
	return out, nil
}

// genRootElement emits the schema for the top-level xs:element by
// generating its inline complexType (or resolving a type reference)
// and lifting the result into the document root.
func (g *gen) genRootElement(el *xsdElement) (map[string]any, error) {
	if el.ComplexType != nil {
		return g.genComplexType(el.ComplexType)
	}
	if el.Type != "" {
		return g.genTypeRef(el.Type)
	}
	return nil, fmt.Errorf("root element %q has neither inline complexType nor type ref", el.Name)
}

// genSimpleType generates the JSON schema fragment for a named simple
// type. Restrictions on xs:string become string + pattern + maxLength;
// enumerations become enum; xs:dateTime becomes string + format.
func (g *gen) genSimpleType(st *xsdSimpleType) (map[string]any, error) {
	r := &st.Restriction
	out := map[string]any{}

	if len(r.Enumerations) > 0 {
		vals := make([]any, len(r.Enumerations))
		for i, e := range r.Enumerations {
			vals[i] = e.Value
		}
		out["type"] = "string"
		out["enum"] = vals
		return out, nil
	}

	switch r.Base {
	case "xs:string":
		out["type"] = "string"
	case "xs:dateTime":
		out["type"] = "string"
		out["format"] = "date-time"
	default:
		return nil, fmt.Errorf("unsupported simpleType base %q (only xs:string and xs:dateTime are supported with restrictions)", r.Base)
	}

	if r.Pattern != nil {
		// Anchors are explicit in JSON Schema; xs:pattern is implicitly
		// anchored to the whole value.
		out["pattern"] = "^" + r.Pattern.Value + "$"
	}
	if r.MaxLength != nil {
		n, err := strconv.Atoi(r.MaxLength.Value)
		if err != nil {
			return nil, fmt.Errorf("maxLength %q: %w", r.MaxLength.Value, err)
		}
		out["maxLength"] = n
	}
	return out, nil
}

// genComplexType generates a JSON schema fragment for a complex type.
//
// Container types (a sequence holding a single element with
// maxOccurs > 1) translate to a JSON array of the child schema,
// per the "repeated elements become JSON arrays" convention. All
// other complex types produce an object schema with properties and
// required built from xs:attribute, xs:sequence, xs:choice, and
// xs:complexContent/xs:extension.
func (g *gen) genComplexType(ct *xsdComplexType) (map[string]any, error) {
	if child, max, ok := containerChild(ct); ok {
		childSchema, err := g.genElementSchema(child)
		if err != nil {
			return nil, err
		}
		arr := map[string]any{
			"type":  "array",
			"items": childSchema,
		}
		if max > 0 {
			arr["maxItems"] = max
		}
		return arr, nil
	}

	out := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
	}
	props := map[string]any{}
	var required []string

	if ct.ComplexContent != nil {
		if ct.ComplexContent.Extension == nil {
			return nil, fmt.Errorf("complexContent without extension is not supported")
		}
		ext := ct.ComplexContent.Extension
		base, ok := g.complexByName[ext.Base]
		if !ok {
			return nil, fmt.Errorf("complexContent extension base %q not found", ext.Base)
		}
		baseSchema, err := g.genComplexType(base)
		if err != nil {
			return nil, fmt.Errorf("extension base %s: %w", ext.Base, err)
		}
		if bp, ok := baseSchema["properties"].(map[string]any); ok {
			maps.Copy(props, bp)
		}
		if br, ok := baseSchema["required"].([]string); ok {
			required = append(required, br...)
		}
		for _, attr := range ext.Attributes {
			s, err := g.genTypeRef(attr.Type)
			if err != nil {
				return nil, fmt.Errorf("extension attribute %q: %w", attr.Name, err)
			}
			props[attr.Name] = s
			if attr.Use == "required" {
				required = append(required, attr.Name)
			}
		}
		if ext.Sequence != nil {
			ap, ar, err := g.genSequence(ext.Sequence)
			if err != nil {
				return nil, err
			}
			maps.Copy(props, ap)
			required = append(required, ar...)
		}
	}

	for _, attr := range ct.Attributes {
		s, err := g.genTypeRef(attr.Type)
		if err != nil {
			return nil, fmt.Errorf("attribute %q: %w", attr.Name, err)
		}
		props[attr.Name] = s
		if attr.Use == "required" {
			required = append(required, attr.Name)
		}
	}

	if ct.Sequence != nil {
		ap, ar, err := g.genSequence(ct.Sequence)
		if err != nil {
			return nil, err
		}
		maps.Copy(props, ap)
		required = append(required, ar...)
	}

	out["properties"] = props
	if len(required) > 0 {
		sort.Strings(required)
		out["required"] = required
	}
	return out, nil
}

// containerChild reports whether ct is a "container" complex type that
// wraps a single repeated child element, and returns the child plus
// its integer maxOccurs (0 for "unbounded"). The detection is
// intentionally strict: a wrapper has only a sequence with exactly one
// element whose maxOccurs > 1, with no attributes, no choices, and no
// complexContent.
func containerChild(ct *xsdComplexType) (*xsdElement, int, bool) {
	if ct.ComplexContent != nil || len(ct.Attributes) > 0 {
		return nil, 0, false
	}
	if ct.Sequence == nil || len(ct.Sequence.Elements) != 1 || len(ct.Sequence.Choices) > 0 {
		return nil, 0, false
	}
	child := &ct.Sequence.Elements[0]
	if child.MaxOccurs == "" || child.MaxOccurs == "1" {
		return nil, 0, false
	}
	max := 0
	if child.MaxOccurs != "unbounded" {
		if n, err := strconv.Atoi(child.MaxOccurs); err == nil {
			max = n
		}
	}
	return child, max, true
}

// genSequence generates the properties and required list for an
// xs:sequence. Returns the properties map and the list of required
// property names.
func (g *gen) genSequence(seq *xsdSequence) (map[string]any, []string, error) {
	props := map[string]any{}
	var required []string
	for i := range seq.Elements {
		el := &seq.Elements[i]
		s, err := g.genElementSchema(el)
		if err != nil {
			return nil, nil, err
		}
		props[el.Name] = s
		if el.MinOccurs != "0" {
			required = append(required, el.Name)
		}
	}
	// xs:choice within a sequence: each option becomes an optional
	// sibling property. The XSD enforces the mutual-exclusion rule.
	for _, ch := range seq.Choices {
		for i := range ch.Elements {
			el := &ch.Elements[i]
			s, err := g.genElementSchema(el)
			if err != nil {
				return nil, nil, err
			}
			props[el.Name] = s
		}
	}
	return props, required, nil
}

// genElementSchema generates the JSON schema fragment for an
// xs:element appearing as a property value. The element's type is
// either a named reference, an inline complexType, or a built-in.
func (g *gen) genElementSchema(el *xsdElement) (map[string]any, error) {
	if el.Type != "" {
		return g.genTypeRef(el.Type)
	}
	if el.ComplexType != nil {
		return g.genComplexType(el.ComplexType)
	}
	return nil, fmt.Errorf("element %q has no type and no inline complexType", el.Name)
}

// genTypeRef returns the schema fragment for a named type reference.
// Built-in xs:* types are inlined; named simple/complex types become
// $ref pointers into $defs.
func (g *gen) genTypeRef(name string) (map[string]any, error) {
	if strings.HasPrefix(name, "xs:") {
		return builtinType(name)
	}
	if _, ok := g.simpleByName[name]; ok {
		return map[string]any{"$ref": "#/$defs/" + name}, nil
	}
	if _, ok := g.complexByName[name]; ok {
		return map[string]any{"$ref": "#/$defs/" + name}, nil
	}
	return nil, fmt.Errorf("unknown type reference %q", name)
}

// builtinType maps an xs:* built-in type to its JSON Schema equivalent.
// Only the types used by crossguard.xsd are handled.
func builtinType(name string) (map[string]any, error) {
	switch name {
	case "xs:string":
		return map[string]any{"type": "string"}, nil
	case "xs:boolean":
		return map[string]any{"type": "boolean"}, nil
	case "xs:int", "xs:integer", "xs:long":
		return map[string]any{"type": "integer", "format": "int64"}, nil
	case "xs:unsignedLong":
		return map[string]any{"type": "integer", "format": "int64", "minimum": 0}, nil
	case "xs:dateTime":
		return map[string]any{"type": "string", "format": "date-time"}, nil
	}
	return nil, fmt.Errorf("unsupported built-in type %q", name)
}

// sortedKeys returns the alphabetically sorted keys of a map[string]T.
// Used to make $defs emission order deterministic.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
