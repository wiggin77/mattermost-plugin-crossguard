package main

import (
	"encoding/xml"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerate_SimpleType_StringWithPatternAndMaxLength verifies the
// canonical string-with-constraints mapping: pattern is anchored with
// ^...$, maxLength is preserved as an integer.
func TestGenerate_SimpleType_StringWithPatternAndMaxLength(t *testing.T) {
	xsd := `<?xml version="1.0"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:simpleType name="SlugType">
    <xs:restriction base="xs:string">
      <xs:pattern value="[a-z0-9_]{1,32}"/>
      <xs:maxLength value="32"/>
    </xs:restriction>
  </xs:simpleType>
  <xs:element name="Root">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="Name" type="SlugType"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	out := mustGenerate(t, xsd)
	defs := out["$defs"].(map[string]any)
	slug := defs["SlugType"].(map[string]any)
	assert.Equal(t, "string", slug["type"])
	assert.Equal(t, "^[a-z0-9_]{1,32}$", slug["pattern"])
	assert.Equal(t, 32, slug["maxLength"])
}

// TestGenerate_SimpleType_Enumeration verifies that an xs:enumeration
// restriction becomes a JSON Schema enum.
func TestGenerate_SimpleType_Enumeration(t *testing.T) {
	xsd := `<?xml version="1.0"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:simpleType name="StatusEnum">
    <xs:restriction base="xs:string">
      <xs:enumeration value="online"/>
      <xs:enumeration value="offline"/>
    </xs:restriction>
  </xs:simpleType>
  <xs:element name="Root">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="S" type="StatusEnum"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	out := mustGenerate(t, xsd)
	defs := out["$defs"].(map[string]any)
	se := defs["StatusEnum"].(map[string]any)
	assert.Equal(t, "string", se["type"])
	assert.Equal(t, []any{"online", "offline"}, se["enum"])
}

// TestGenerate_ContainerType_BecomesArray verifies that a complexType
// holding a single repeated child becomes a JSON array. The wrapper
// element does not appear in JSON; the property at the use site is
// directly an array.
func TestGenerate_ContainerType_BecomesArray(t *testing.T) {
	xsd := `<?xml version="1.0"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:complexType name="ItemsType">
    <xs:sequence>
      <xs:element name="Item" type="xs:string" minOccurs="0" maxOccurs="5"/>
    </xs:sequence>
  </xs:complexType>
  <xs:element name="Root">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="Items" type="ItemsType" minOccurs="0"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	out := mustGenerate(t, xsd)
	defs := out["$defs"].(map[string]any)
	items := defs["ItemsType"].(map[string]any)
	assert.Equal(t, "array", items["type"])
	assert.Equal(t, 5, items["maxItems"])
	itemSchema := items["items"].(map[string]any)
	assert.Equal(t, "string", itemSchema["type"])
}

// TestGenerate_AttributeBecomesSiblingProperty verifies that an XSD
// attribute (here a complexType extension carrying an id attribute) is
// rendered as a sibling object property at the same level as the
// element's other properties.
func TestGenerate_AttributeBecomesSiblingProperty(t *testing.T) {
	xsd := `<?xml version="1.0"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:complexType name="ItemType">
    <xs:sequence>
      <xs:element name="Name" type="xs:string"/>
    </xs:sequence>
  </xs:complexType>
  <xs:complexType name="ItemsType">
    <xs:sequence>
      <xs:element name="Item" minOccurs="0" maxOccurs="10">
        <xs:complexType>
          <xs:complexContent>
            <xs:extension base="ItemType">
              <xs:attribute name="id" type="xs:string" use="required"/>
            </xs:extension>
          </xs:complexContent>
        </xs:complexType>
      </xs:element>
    </xs:sequence>
  </xs:complexType>
  <xs:element name="Root">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="Items" type="ItemsType"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	out := mustGenerate(t, xsd)
	defs := out["$defs"].(map[string]any)
	itemsArr := defs["ItemsType"].(map[string]any)
	itemSchema := itemsArr["items"].(map[string]any)
	props := itemSchema["properties"].(map[string]any)
	assert.Contains(t, props, "id")
	assert.Contains(t, props, "Name")
	required := itemSchema["required"].([]string)
	assert.Contains(t, required, "id")
	assert.Contains(t, required, "Name")
}

// TestGenerate_MinOccursZero_NotRequired verifies that an element with
// minOccurs="0" is omitted from the required list.
func TestGenerate_MinOccursZero_NotRequired(t *testing.T) {
	xsd := `<?xml version="1.0"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="Root">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="Required"  type="xs:string"/>
        <xs:element name="Optional"  type="xs:string" minOccurs="0"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	out := mustGenerate(t, xsd)
	required := out["required"].([]string)
	assert.Contains(t, required, "Required")
	assert.NotContains(t, required, "Optional")
}

// TestGenerate_UnsupportedConstruct_Errors verifies that an XSD
// construct outside the supported ruleset produces a hard error, so
// future XSD changes either fit the rules or force a generator update.
func TestGenerate_UnsupportedConstruct_Errors(t *testing.T) {
	xsd := `<?xml version="1.0"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:simpleType name="WeirdType">
    <xs:restriction base="xs:double"/>
  </xs:simpleType>
  <xs:element name="Root">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="N" type="WeirdType"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	var schema xsdSchema
	require.NoError(t, xml.Unmarshal([]byte(xsd), &schema))
	_, err := generate(&schema)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported simpleType base")
}

// mustGenerate parses an XSD string and returns the generated schema
// or fails the test.
func mustGenerate(t *testing.T, xsd string) map[string]any {
	t.Helper()
	var schema xsdSchema
	require.NoError(t, xml.Unmarshal([]byte(xsd), &schema))
	out, err := generate(&schema)
	require.NoError(t, err)
	return out
}
