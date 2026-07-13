// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package definition

import (
	"encoding/base64"
	"fmt"
	"math"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
	"golang.org/x/text/unicode/norm"
)

type body struct {
	items []item
}

type item struct {
	name   string
	args   []value
	nested body
	span   syntax.Span
	block  bool
	node   *syntax.Node
}

type value struct {
	raw  string
	kind syntax.TokenKind
	span syntax.Span
	node *syntax.Node
}

func (c *compiler) body(src resolve.DeclarationSource) body {
	return readBlock(firstNode(src.Node, syntax.NodeBlock))
}

func readBlock(n *syntax.Node) body {
	var out body
	for _, child := range children(n) {
		switch child.Kind {
		case syntax.NodeStatement:
			toks := directTokens(child)
			if len(toks) == 0 {
				continue
			}
			out.items = append(out.items, item{name: toks[0].Raw, args: values(child)[1:], span: child.Span, node: child})
		case syntax.NodeNestedBlock:
			toks := directTokens(child)
			if len(toks) == 0 {
				continue
			}
			out.items = append(out.items, item{name: toks[0].Raw, args: values(child)[1:], nested: readBlock(firstNode(child, syntax.NodeBlock)), span: child.Span, block: true, node: child})
		}
	}
	return out
}

func (b body) stmt(name string) *item {
	for i := range b.items {
		if b.items[i].name == name && !b.items[i].block {
			return &b.items[i]
		}
	}
	return nil
}

func (b body) block(name string) *item {
	for i := range b.items {
		if b.items[i].name == name && b.items[i].block {
			return &b.items[i]
		}
	}
	return nil
}

func (b body) blocksByHead(name string) []item {
	var out []item
	for _, it := range b.items {
		if it.name == name {
			out = append(out, it)
		}
	}
	return out
}

func (c *compiler) rejectUnknown(b body, src resolve.DeclarationSource, spec map[string]fieldSpec) {
	seen := map[string]syntax.Span{}
	primitiveSeen := map[string]syntax.Span{}
	for _, it := range b.items {
		fs, ok := spec[it.name]
		if !ok || (!fs.either && fs.nested != it.block) {
			c.diag("LDL1102", "unknown_or_duplicate_schema_member", src, it.span, "unknown or invalid schema member", src.Address, "")
			continue
		}
		key := it.name
		if it.name == "projection" || it.name == "render" {
			if len(it.args) != 1 {
				c.diag("LDL1102", "unknown_or_duplicate_schema_member", src, it.span, "missing primitive", src.Address, "")
				continue
			}
			key = it.name + ":" + it.args[0].raw
			if prev, ok := primitiveSeen[key]; ok {
				c.diagRelated("LDL1102", "unknown_or_duplicate_schema_member", src, it.span, "duplicate primitive", src.Address, "", prev)
			}
			primitiveSeen[key] = it.span
			continue
		}
		if fs.card == singleton {
			if prev, ok := seen[key]; ok {
				c.diagRelated("LDL1102", "unknown_or_duplicate_schema_member", src, it.span, "duplicate schema member", src.Address, "", prev)
			}
			seen[key] = it.span
		}
	}
}

func (c *compiler) common(b body, src resolve.DeclarationSource, subject, owner string) Common {
	annotations := b.stmt("annotations")
	if annotations == nil {
		annotations = b.block("annotations")
	}
	return Common{
		Description: c.optionalString(b, "description", src, subject, owner),
		Tags:        c.tags(b.stmt("tags"), src, subject, owner),
		Annotations: c.annotations(annotations, src, subject, owner),
	}
}

func (c *compiler) tags(it *item, src resolve.DeclarationSource, subject, owner string) []string {
	if it == nil {
		return []string{}
	}
	if len(it.args) != 1 || firstNode(it.args[0].node, syntax.NodeList) == nil {
		c.diag("LDL1102", "unknown_or_duplicate_schema_member", src, it.span, "tags requires one list", subject, owner)
		return []string{}
	}
	tags := []string{}
	seen := map[string]syntax.Span{}
	for _, v := range listValues(it.args[0].node) {
		if v.kind != syntax.TokenIdentifier && v.kind != syntax.TokenString {
			c.diag("LDL1102", "unknown_or_duplicate_schema_member", src, v.span, "invalid tag", subject, owner)
			continue
		}
		s := normalizeString(v.string())
		if s == "" {
			c.diag("LDL1102", "unknown_or_duplicate_schema_member", src, v.span, "invalid tag", subject, owner)
			continue
		}
		if prev, ok := seen[s]; ok {
			c.diagRelated("LDL1102", "unknown_or_duplicate_schema_member", src, v.span, "duplicate tag", subject, owner, prev)
			continue
		}
		seen[s] = v.span
		tags = append(tags, s)
	}
	sort.Strings(tags)
	return tags
}

func (c *compiler) annotations(it *item, src resolve.DeclarationSource, subject, owner string) map[string]string {
	out := map[string]string{}
	if it == nil {
		return out
	}
	if it.block {
		if len(it.nested.items) != 0 {
			c.diag("LDL1102", "unknown_or_duplicate_schema_member", src, it.span, "annotations requires an object", subject, owner)
		}
		return out
	}
	if len(it.args) != 1 {
		c.diag("LDL1102", "unknown_or_duplicate_schema_member", src, it.span, "annotations requires one object", subject, owner)
		return out
	}
	if firstNode(it.args[0].node, syntax.NodeObject) == nil {
		c.diag("LDL1102", "unknown_or_duplicate_schema_member", src, it.span, "annotations requires an object", subject, owner)
		return out
	}
	seen := map[string]syntax.Span{}
	for _, entry := range objectValues(it.args[0].node) {
		key := normalizeString(entry.key)
		if key == "" || entry.value.kind != syntax.TokenString {
			c.diag("LDL1102", "unknown_or_duplicate_schema_member", src, entry.span, "invalid annotation", subject, owner)
			continue
		}
		if prev, ok := seen[key]; ok {
			c.diagRelated("LDL1102", "unknown_or_duplicate_schema_member", src, entry.span, "duplicate annotation", subject, owner, prev)
			continue
		}
		annotationValue := entry.value.string()
		if forbiddenAnnotationKey(key) {
			c.diag("LDL1901", "forbidden_state_credential_or_executable_content", src, entry.keySpan, "forbidden annotation key", subject, owner)
			continue
		}
		if forbiddenAnnotationValue(annotationValue) {
			c.diag("LDL1901", "forbidden_state_credential_or_executable_content", src, entry.value.span, "forbidden annotation value", subject, owner)
			continue
		}
		seen[key] = entry.span
		out[key] = normalizeString(annotationValue)
	}
	return out
}

var forbiddenAnnotationKeys = set(
	"created_at", "updated_at", "created_by", "updated_by",
	"observed_at", "verified_at", "verified_by", "observation", "verification", "confidence", "freshness", "provenance", "source_uri", "source_url", "field_owner", "field_ownership",
	"audit_event", "operation_log", "resource_version", "revision", "managed_fields", "lock", "lease", "presence",
	"credential", "credentials", "password", "passwd", "secret", "api_key", "access_key", "secret_key", "token", "access_token", "refresh_token", "authorization", "auth_token", "bearer_token", "private_key", "client_secret",
	"backend", "backend_binding", "backend_credentials", "data_source_binding",
	"view_data", "viewdata", "render_data", "renderdata", "generated_index", "search_index", "preview", "artifact", "export_artifact", "generated_artifact",
	"binary", "binary_data", "binary_payload", "image_data", "image_bytes", "asset_bytes",
	"javascript", "javascript_source", "js_source", "go_source", "wasm", "wasm_module", "source_code", "script", "shell", "shell_command", "command", "callback",
	"environment_read", "environment_variable", "env_var", "getenv",
)

var environmentReadPattern = regexp.MustCompile(`\$\{[A-Z][A-Z0-9_]*\}`)
var bearerValuePattern = regexp.MustCompile(`(?i)^bearer ([A-Za-z0-9._~+/=-]+)$`)
var authorizationValuePattern = regexp.MustCompile(`(?i)^(?:proxy-)?authorization\s*:\s*(?:basic|bearer)\s+\S+\s*$`)
var goPackagePattern = regexp.MustCompile(`(?m)^package [A-Za-z_][A-Za-z0-9_]*\s*$`)
var goDeclarationPattern = regexp.MustCompile(`(?m)^(?:func|type|var|const|import)\b`)

func forbiddenAnnotationKey(key string) bool {
	name := canonicalAnnotationName(key)
	if name == "password_policy" || strings.HasSuffix(name, "_password_policy") {
		return false
	}
	if forbiddenAnnotationKeys[name] {
		return true
	}
	for forbidden := range forbiddenAnnotationKeys {
		if strings.HasSuffix(name, "_"+forbidden) {
			return true
		}
	}
	return annotationNameContainsWord(name, "credential") ||
		annotationNameContainsWord(name, "credentials") ||
		annotationNameContainsWord(name, "executable") ||
		name == "state_version" || strings.HasSuffix(name, "_state_version") ||
		name == "generated_state" || strings.HasSuffix(name, "_generated_state")
}

func annotationNameContainsWord(name, word string) bool {
	padded := "_" + name + "_"
	return strings.Contains(padded, "_"+word+"_")
}

func canonicalAnnotationName(key string) string {
	trimmed := strings.TrimSpace(key)
	var out strings.Builder
	lastUnderscore := false
	for i, r := range trimmed {
		if r == '-' || r == '.' || r == '/' || r == ' ' {
			if out.Len() > 0 && !lastUnderscore {
				out.WriteByte('_')
				lastUnderscore = true
			}
			continue
		}
		if r >= 'A' && r <= 'Z' {
			previousLowerOrDigit := i > 0 && ((trimmed[i-1] >= 'a' && trimmed[i-1] <= 'z') || (trimmed[i-1] >= '0' && trimmed[i-1] <= '9'))
			nextLower := i+1 < len(trimmed) && trimmed[i+1] >= 'a' && trimmed[i+1] <= 'z'
			if out.Len() > 0 && !lastUnderscore && (previousLowerOrDigit || nextLower) {
				out.WriteByte('_')
			}
			out.WriteRune(r + ('a' - 'A'))
			lastUnderscore = false
			continue
		}
		out.WriteRune(r)
		lastUnderscore = r == '_'
	}
	return strings.Trim(out.String(), "_")
}

func forbiddenAnnotationValue(value string) bool {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{
		"javascript:", "#!", "function ", "async function ", "eval(",
		"bash -c ", "sh -c ", "zsh -c ", "powershell ", "cmd.exe /c ",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	firstLine := lower
	if line, _, ok := strings.Cut(firstLine, "\n"); ok {
		firstLine = strings.TrimSpace(line)
	}
	if strings.HasPrefix(firstLine, "-----begin ") && strings.HasSuffix(firstLine, " private key-----") {
		return true
	}
	if authorizationValuePattern.MatchString(trimmed) {
		return true
	}
	if match := bearerValuePattern.FindStringSubmatch(trimmed); len(match) == 2 {
		token := match[1]
		if !strings.EqualFold(token, "authentication") &&
			(len(token) >= 20 || strings.ContainsAny(token, ".0123456789_~+/=-")) {
			return true
		}
	}
	if strings.HasPrefix(lower, "basic ") {
		encoded := strings.TrimSpace(trimmed[len("basic "):])
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			decoded, err = base64.RawStdEncoding.DecodeString(encoded)
		}
		if err == nil && strings.ContainsRune(string(decoded), ':') {
			return true
		}
	}
	for _, marker := range []string{"process.env.", "os.getenv(", "getenv(", "${env:", "%env%"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	if goPackagePattern.MatchString(trimmed) && goDeclarationPattern.MatchString(trimmed) {
		return true
	}
	return environmentReadPattern.MatchString(trimmed)
}

func (c *compiler) optionalString(b body, name string, src resolve.DeclarationSource, subject, owner string) *string {
	it := b.stmt(name)
	if it == nil {
		return nil
	}
	if len(it.args) != 1 || it.args[0].kind != syntax.TokenString && it.args[0].kind != syntax.TokenHeredoc {
		c.diag("LDL1102", "unknown_or_duplicate_schema_member", src, invalidOperandSpan(it), "expected string", subject, owner)
		return nil
	}
	s := it.args[0].string()
	return &s
}

func (c *compiler) requiredString(b body, name string, src resolve.DeclarationSource, subject, owner, code, key string) string {
	if b.stmt(name) == nil {
		c.diag(code, key, src, declarationHeaderSpan(src), "missing required string", subject, owner)
		return ""
	}
	s := c.optionalString(b, name, src, subject, owner)
	if s == nil {
		return ""
	}
	return *s
}

func (c *compiler) optionalBoolDefault(b body, name string, def bool, src resolve.DeclarationSource, subject, owner, code, key string) bool {
	it := b.stmt(name)
	if it == nil {
		return def
	}
	if len(it.args) != 1 || (it.args[0].raw != "true" && it.args[0].raw != "false") {
		c.diag(code, key, src, invalidOperandSpan(it), "expected boolean", subject, owner)
		return def
	}
	return it.args[0].raw == "true"
}

func (c *compiler) optionalEnumDefault(b body, name, def string, allowed map[string]bool, src resolve.DeclarationSource, subject, owner, code, key string) string {
	value, _ := c.optionalEnumDefaultValid(b, name, def, allowed, src, subject, owner, code, key)
	return value
}

func (c *compiler) optionalEnumDefaultValid(b body, name, def string, allowed map[string]bool, src resolve.DeclarationSource, subject, owner, code, key string) (string, bool) {
	it := b.stmt(name)
	if it == nil {
		return def, true
	}
	if len(it.args) != 1 || it.args[0].kind != syntax.TokenIdentifier || !allowed[it.args[0].raw] {
		c.diag(code, key, src, invalidOperandSpan(it), "invalid enum", subject, owner)
		return def, false
	}
	return it.args[0].raw, true
}

func invalidOperandSpan(it *item) syntax.Span {
	if it == nil {
		return syntax.Span{}
	}
	if len(it.args) > 1 {
		return it.args[1].span
	}
	if len(it.args) == 1 {
		return it.args[0].span
	}
	tokens := directTokens(it.node)
	if len(tokens) > 0 {
		return tokens[0].Span
	}
	return itemHeaderSpan(it)
}

func (c *compiler) optionalColor(b body, name string, src resolve.DeclarationSource, subject, owner string) *string {
	s := c.optionalString(b, name, src, subject, owner)
	if s == nil {
		return nil
	}
	color := strings.ToUpper(*s)
	if !colorPattern.MatchString(color) {
		c.diag("LDL1401", "scalar_or_column_type_mismatch", src, b.stmt(name).args[0].span, "invalid color", subject, owner)
		return nil
	}
	return &color
}

var colorPattern = regexp.MustCompile(`^#[0-9A-F]{6}([0-9A-F]{2})?$`)

func (c *compiler) optionalAsset(b body, name string, src resolve.DeclarationSource, subject string) *AuthoredAsset {
	it := b.stmt(name)
	if it == nil {
		return nil
	}
	if len(it.args) != 1 || it.args[0].kind != syntax.TokenString {
		c.diag("LDL1102", "unknown_or_duplicate_schema_member", src, invalidOperandSpan(it), "expected asset path string", subject, "")
		return nil
	}
	authored, ok := it.args[0].authoredString()
	if !ok {
		c.diag("LDL1201", "module_pack_or_asset_resolution_failed", src, it.args[0].span, "invalid asset locator", subject, "")
		return nil
	}
	locator, ok := resolve.ResolveAuthoredAssetLocator(src.Module.Path, authored)
	if !ok {
		c.diag("LDL1201", "module_pack_or_asset_resolution_failed", src, it.args[0].span, "invalid asset locator", subject, "")
		return nil
	}
	return &AuthoredAsset{AuthoredPath: authored, Locator: locator, Origin: src.Module.Origin, ModulePath: src.Module.Path, SourceRange: c.rangeOf(src, it.args[0].span)}
}

func containsControl(raw string) bool {
	for _, r := range raw {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func (c *compiler) representation(b body, src resolve.DeclarationSource, subject, owner string) Representation {
	it := b.stmt("representation")
	if it == nil || len(it.args) == 0 {
		c.diag("LDL1102", "unknown_or_duplicate_schema_member", src, declarationHeaderSpan(src), "missing representation", subject, owner)
		return Representation{}
	}
	switch it.args[0].raw {
	case "container", "table":
		if len(it.args) != 1 {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, it.span, "invalid representation", subject, owner)
			return Representation{}
		}
		return Representation{Kind: RepresentationKind(it.args[0].raw)}
	case "shape":
		if len(it.args) != 2 || !set("rect", "rounded", "ellipse", "diamond", "cylinder", "cloud", "hexagon", "person", "device")[it.args[1].raw] {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, it.span, "invalid representation", subject, owner)
			return Representation{}
		}
		return Representation{Kind: RepresentationShapeKind, Shape: RepresentationShape(it.args[1].raw)}
	default:
		c.diag("LDL1401", "scalar_or_column_type_mismatch", src, it.span, "invalid representation", subject, owner)
		return Representation{}
	}
}

func (c *compiler) columns(it *item, owner resolve.DeclarationSymbol, src resolve.DeclarationSource) []Column {
	if it == nil {
		return []Column{}
	}
	cols := []Column{}
	seen := map[string]syntax.Span{}
	for _, stmt := range it.nested.items {
		if stmt.block || len(stmt.args) < 2 || stmt.args[0].kind != syntax.TokenString || stmt.args[1].kind != syntax.TokenIdentifier {
			c.diag("LDL1102", "unknown_or_duplicate_schema_member", src, stmt.span, "invalid column", "", owner.Address)
			continue
		}
		id := stmt.name
		if prev, ok := seen[id]; ok {
			c.diagRelated("LDL1102", "unknown_or_duplicate_schema_member", src, stmt.span, "duplicate column", "", owner.Address, prev)
			continue
		}
		seen[id] = stmt.span
		decl := c.columnDecl[childKey(&owner.Symbol, resolve.KindColumn, id)]
		col := Column{ID: id, Address: decl.Address, DisplayName: normalizeString(stmt.args[0].string()), ReservedEnumValues: []string{}}
		if !set("string", "integer", "number", "boolean", "enum", "date", "datetime")[stmt.args[1].raw] {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, stmt.args[1].span, "invalid scalar type", col.Address, owner.Address)
		} else {
			col.ValueType = ScalarType(stmt.args[1].raw)
			c.columnModifiers(&col, stmt.args[1], stmt.args[2:], src, owner.Address)
		}
		cols = append(cols, col)
	}
	return cols
}

func (c *compiler) columnModifiers(col *Column, typeValue value, args []value, src resolve.DeclarationSource, owner string) {
	seen := map[string]syntax.Span{}
	lastRank := -1
	defaultSpan := typeValue.span
	var defaultValue *value
	enumOptionsPresent := len(args) > 0 && firstNode(args[0].node, syntax.NodeList) != nil
	enumOptionsValid := true
	if enumOptionsPresent {
		if col.ValueType != ScalarEnum {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, args[0].span, "enum values forbidden", col.Address, owner)
		} else {
			values, valid := c.enumList(args[0], false, src, col.Address, owner)
			enumOptionsValid = valid
			if valid {
				col.EnumValues = values
			}
		}
		args = args[1:]
	}
	for i := 0; i < len(args); i++ {
		name := args[i].raw
		rank, known := columnModifierRanks[name]
		if !known || args[i].kind != syntax.TokenIdentifier {
			c.diag("LDL1102", "unknown_or_duplicate_schema_member", src, args[i].span, "unknown column modifier", col.Address, owner)
			continue
		}
		if rank < lastRank {
			c.diag("LDL1102", "unknown_or_duplicate_schema_member", src, args[i].span, "column modifier out of order", col.Address, owner)
		}
		lastRank = rank
		if prev, ok := seen[name]; ok {
			c.diagRelated("LDL1102", "unknown_or_duplicate_schema_member", src, args[i].span, "duplicate column modifier", col.Address, owner, prev)
			if name != "required" && i+1 < len(args) {
				i++
			}
			continue
		}
		seen[name] = args[i].span
		if name == "required" {
			col.Required = true
			continue
		}
		if i+1 >= len(args) {
			c.diag("LDL1102", "unknown_or_duplicate_schema_member", src, args[i].span, "missing column modifier value", col.Address, owner)
			continue
		}
		operand := args[i+1]
		switch name {
		case "default":
			copy := operand
			defaultValue = &copy
			defaultSpan = operand.span
			i++
		case "format":
			format := StringFormat(operand.raw)
			if operand.kind != syntax.TokenIdentifier || col.ValueType != ScalarString || !stringFormats[string(format)] {
				c.diag("LDL1401", "scalar_or_column_type_mismatch", src, operand.span, "invalid format", col.Address, owner)
			} else {
				col.Format = &format
			}
			i++
		case "min", "max":
			f, ok := operand.number()
			if col.ValueType == ScalarInteger {
				n, intOK := operand.integer()
				ok = intOK && jsonSafeInteger(n)
				f = float64(n)
			} else if col.ValueType != ScalarNumber {
				ok = false
			}
			if !ok {
				c.diag("LDL1401", "scalar_or_column_type_mismatch", src, operand.span, "invalid numeric bound", col.Address, owner)
			} else if name == "min" {
				col.Min = &f
			} else {
				col.Max = &f
			}
			i++
		case "min_length", "max_length":
			n, ok := operand.integer()
			if !ok || n < 0 || !jsonSafeInteger(n) || col.ValueType != ScalarString {
				c.diag("LDL1401", "scalar_or_column_type_mismatch", src, operand.span, "invalid length bound", col.Address, owner)
			} else if name == "min_length" {
				col.MinLength = &n
			} else {
				col.MaxLength = &n
			}
			i++
		case "reserve_values":
			if col.ValueType != ScalarEnum || firstNode(operand.node, syntax.NodeList) == nil {
				c.diag("LDL1401", "scalar_or_column_type_mismatch", src, operand.span, "reserved enum values forbidden", col.Address, owner)
			} else {
				values, valid := c.enumList(operand, true, src, col.Address, owner)
				if valid {
					col.ReservedEnumValues = values
				}
			}
			i++
		}
	}
	if col.ValueType == ScalarEnum && !enumOptionsPresent {
		c.diag("LDL1401", "scalar_or_column_type_mismatch", src, typeValue.span, "enum requires values", col.Address, owner)
		enumOptionsValid = false
	} else if col.ValueType == ScalarEnum && enumOptionsValid && len(col.EnumValues) == 0 {
		c.diag("LDL1401", "scalar_or_column_type_mismatch", src, typeValue.span, "enum requires values", col.Address, owner)
		enumOptionsValid = false
	}
	reservedValuesValid := true
	for _, active := range col.EnumValues {
		if contains(col.ReservedEnumValues, active) {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, typeValue.span, "active and reserved enum values overlap", col.Address, owner)
			reservedValuesValid = false
		}
	}
	if !reservedValuesValid {
		col.ReservedEnumValues = []string{}
	}
	invalidNumericBounds := col.Min != nil && col.Max != nil && *col.Min > *col.Max
	invalidLengthBounds := col.MinLength != nil && col.MaxLength != nil && *col.MinLength > *col.MaxLength
	if invalidNumericBounds || invalidLengthBounds {
		c.diag("LDL1401", "scalar_or_column_type_mismatch", src, typeValue.span, "invalid bounds", col.Address, owner)
		if invalidNumericBounds {
			col.Min, col.Max = nil, nil
		}
		if invalidLengthBounds {
			col.MinLength, col.MaxLength = nil, nil
		}
	}
	if defaultValue != nil && (col.ValueType != ScalarEnum || enumOptionsValid) {
		col.Default = c.scalar(*defaultValue, col, src, owner)
	}
	if col.Default != nil {
		if !c.validateDefault(col, src, owner, defaultSpan) {
			col.Default = nil
		}
	}
}

var columnModifierRanks = map[string]int{
	"reserve_values": 0, "required": 1, "default": 2, "format": 3,
	"min": 4, "max": 5, "min_length": 6, "max_length": 7,
}

var stringFormats = set("uri", "email", "hostname", "ipv4", "ipv6", "cidr")

func (c *compiler) enumList(v value, canonical bool, src resolve.DeclarationSource, subject, owner string) ([]string, bool) {
	var out []string
	valid := true
	seen := map[string]syntax.Span{}
	for _, item := range listValues(v.node) {
		if item.kind != syntax.TokenIdentifier && item.kind != syntax.TokenString {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, item.span, "invalid enum value", subject, owner)
			valid = false
			continue
		}
		s := normalizeString(item.string())
		if s == "" {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, item.span, "empty enum value", subject, owner)
			valid = false
			continue
		}
		if prev, ok := seen[s]; ok {
			c.diagRelated("LDL1102", "unknown_or_duplicate_schema_member", src, item.span, "duplicate enum value", subject, owner, prev)
			valid = false
			continue
		}
		seen[s] = item.span
		out = append(out, s)
	}
	if canonical {
		sort.Strings(out)
	}
	return out, valid
}

func (c *compiler) scalar(v value, col *Column, src resolve.DeclarationSource, owner string) *Scalar {
	switch col.ValueType {
	case ScalarString:
		if v.kind != syntax.TokenString {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, v.span, "default type mismatch", col.Address, owner)
			return nil
		}
		return &Scalar{Type: ScalarString, String: normalizeString(v.string())}
	case ScalarDate:
		s, ok := normalizeDate(v.string())
		if v.kind != syntax.TokenString || !ok {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, v.span, "invalid date", col.Address, owner)
			return nil
		}
		return &Scalar{Type: ScalarDate, String: s}
	case ScalarDatetime:
		s, ok := normalizeDatetime(v.string())
		if v.kind != syntax.TokenString || !ok {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, v.span, "invalid datetime", col.Address, owner)
			return nil
		}
		return &Scalar{Type: ScalarDatetime, String: s}
	case ScalarInteger:
		n, ok := v.integer()
		if !ok || !jsonSafeInteger(n) {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, v.span, "default type mismatch", col.Address, owner)
			return nil
		}
		return &Scalar{Type: ScalarInteger, Int: n}
	case ScalarNumber:
		f, ok := v.number()
		if !ok {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, v.span, "default type mismatch", col.Address, owner)
			return nil
		}
		if f == 0 {
			f = 0
		}
		return &Scalar{Type: ScalarNumber, Float: f}
	case ScalarBoolean:
		if v.raw != "true" && v.raw != "false" {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, v.span, "default type mismatch", col.Address, owner)
			return nil
		}
		return &Scalar{Type: ScalarBoolean, Bool: v.raw == "true"}
	case ScalarEnum:
		if v.kind != syntax.TokenIdentifier && v.kind != syntax.TokenString {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, v.span, "enum default mismatch", col.Address, owner)
			return nil
		}
		s := normalizeString(v.string())
		if !contains(col.EnumValues, s) || contains(col.ReservedEnumValues, s) {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, v.span, "enum default mismatch", col.Address, owner)
			return nil
		}
		return &Scalar{Type: ScalarEnum, String: s}
	default:
		return nil
	}
}

const maxJSONSafeInteger int64 = 9007199254740991

func jsonSafeInteger(n int64) bool {
	return n >= -maxJSONSafeInteger && n <= maxJSONSafeInteger
}

func normalizeDate(raw string) (string, bool) {
	if !datePattern.MatchString(raw) || strings.HasPrefix(raw, "0000-") {
		return "", false
	}
	if _, err := time.Parse("2006-01-02", raw); err != nil {
		return "", false
	}
	return raw, true
}

var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

var datetimePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,3})?(?:Z|[+-]\d{2}:\d{2})$`)

func normalizeDatetime(raw string) (string, bool) {
	if !datetimePattern.MatchString(raw) || strings.HasPrefix(raw, "0000-") {
		return "", false
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return "", false
	}
	return parsed.UTC().Format(time.RFC3339Nano), true
}

func (c *compiler) validateDefault(col *Column, src resolve.DeclarationSource, owner string, span syntax.Span) bool {
	s := col.Default
	if s == nil {
		return true
	}
	if col.ValueType == ScalarString && col.Format != nil {
		normalized, ok := normalizeStringFormat(string(*col.Format), s.String)
		if !ok {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, span, "default format mismatch", col.Address, owner)
			return false
		}
		s.String = normalized
	}
	if col.ValueType == ScalarString {
		length := int64(utf8.RuneCountInString(s.String))
		if col.MinLength != nil && length < *col.MinLength || col.MaxLength != nil && length > *col.MaxLength {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, span, "default length mismatch", col.Address, owner)
			return false
		}
	}
	if col.ValueType == ScalarInteger {
		value := float64(s.Int)
		if col.Min != nil && value < *col.Min || col.Max != nil && value > *col.Max {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, span, "default range mismatch", col.Address, owner)
			return false
		}
	}
	if col.ValueType == ScalarNumber {
		if col.Min != nil && s.Float < *col.Min || col.Max != nil && s.Float > *col.Max {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, span, "default range mismatch", col.Address, owner)
			return false
		}
	}
	return true
}

func normalizeStringFormat(format, raw string) (string, bool) {
	switch format {
	case "uri":
		if !validAbsoluteURI(raw) {
			return "", false
		}
		return raw, true
	case "email":
		if !validEmail(raw) {
			return "", false
		}
		return raw, true
	case "hostname":
		return normalizeHostname(raw)
	case "ipv4":
		addr, err := netip.ParseAddr(raw)
		if err != nil || !addr.Is4() || addr.String() != raw {
			return "", false
		}
		return addr.String(), true
	case "ipv6":
		addr, err := netip.ParseAddr(raw)
		if err != nil || !addr.Is6() || addr.Zone() != "" {
			return "", false
		}
		return addr.String(), true
	case "cidr":
		prefix, err := netip.ParsePrefix(raw)
		if err != nil || prefix != prefix.Masked() {
			return "", false
		}
		return prefix.String(), true
	default:
		return "", false
	}
}

func validAbsoluteURI(raw string) bool {
	colon := strings.IndexByte(raw, ':')
	if colon <= 0 || !validURIScheme(raw[:colon]) || !isASCII(raw) || strings.Contains(raw, "\\") || containsControl(raw) {
		return false
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] == '%' {
			if i+2 >= len(raw) || !isHex(raw[i+1]) || !isHex(raw[i+2]) {
				return false
			}
			i += 2
			continue
		}
		if !isURICharacter(raw[i]) {
			return false
		}
	}
	remainder := raw[colon+1:]
	if strings.Count(remainder, "#") > 1 {
		return false
	}
	hierAndQuery, fragment, hasFragment := strings.Cut(remainder, "#")
	if hasFragment && !validURIQueryOrFragment(fragment) {
		return false
	}
	hier, query, hasQuery := strings.Cut(hierAndQuery, "?")
	if hasQuery && !validURIQueryOrFragment(query) {
		return false
	}
	if strings.HasPrefix(hier, "//") {
		authorityAndPath := hier[2:]
		pathStart := strings.IndexByte(authorityAndPath, '/')
		authority, uriPath := authorityAndPath, ""
		if pathStart >= 0 {
			authority, uriPath = authorityAndPath[:pathStart], authorityAndPath[pathStart:]
		}
		return validURIAuthority(authority) && validURIPath(uriPath)
	}
	return !strings.HasPrefix(hier, "//") && validURIPath(hier)
}

func validURIScheme(scheme string) bool {
	if scheme == "" || !isAlpha(scheme[0]) {
		return false
	}
	for i := 1; i < len(scheme); i++ {
		if !isAlpha(scheme[i]) && !isDigitByte(scheme[i]) && !strings.ContainsRune("+.-", rune(scheme[i])) {
			return false
		}
	}
	return true
}

func validURIAuthority(authority string) bool {
	if strings.Count(authority, "@") > 1 {
		return false
	}
	hostPort := authority
	if userinfo, rest, ok := strings.Cut(authority, "@"); ok {
		if !validURIComponent(userinfo, true, ":") {
			return false
		}
		hostPort = rest
	}
	if strings.HasPrefix(hostPort, "[") {
		close := strings.IndexByte(hostPort, ']')
		if close <= 1 {
			return false
		}
		literal, rest := hostPort[1:close], hostPort[close+1:]
		if !validIPLiteral(literal) || rest != "" && (!strings.HasPrefix(rest, ":") || !allDigits(rest[1:])) {
			return false
		}
		return true
	}
	if strings.ContainsAny(hostPort, "[]") {
		return false
	}
	host := hostPort
	if colon := strings.LastIndexByte(hostPort, ':'); colon >= 0 {
		host = hostPort[:colon]
		if strings.Contains(host, ":") || !allDigits(hostPort[colon+1:]) {
			return false
		}
	}
	return validURIComponent(host, true, "")
}

func validIPLiteral(literal string) bool {
	if addr, err := netip.ParseAddr(literal); err == nil {
		return addr.Is6() && addr.Zone() == ""
	}
	if len(literal) < 4 || literal[0] != 'v' && literal[0] != 'V' {
		return false
	}
	dot := strings.IndexByte(literal, '.')
	if dot < 2 {
		return false
	}
	for i := 1; i < dot; i++ {
		if !isHex(literal[i]) {
			return false
		}
	}
	return literal[dot+1:] != "" && validURIComponent(literal[dot+1:], false, ":")
}

func validURIPath(uriPath string) bool {
	return validURIComponent(uriPath, true, "/:@")
}

func validURIQueryOrFragment(value string) bool {
	return validURIComponent(value, true, "/?:@")
}

func validURIComponent(value string, allowEmpty bool, extra string) bool {
	if value == "" {
		return allowEmpty
	}
	for i := 0; i < len(value); i++ {
		if value[i] == '%' {
			i += 2
			continue
		}
		if isURIUnreserved(value[i]) || strings.ContainsRune("!$&'()*+,;="+extra, rune(value[i])) {
			continue
		}
		return false
	}
	return true
}

func isURICharacter(ch byte) bool {
	return isURIUnreserved(ch) || strings.ContainsRune(":/?#[]@!$&'()*+,;=%", rune(ch))
}

func isURIUnreserved(ch byte) bool {
	return isAlpha(ch) || isDigitByte(ch) || strings.ContainsRune("-._~", rune(ch))
}

func isAlpha(ch byte) bool {
	return ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z'
}

func isDigitByte(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func isHex(ch byte) bool {
	return isDigitByte(ch) || ch >= 'A' && ch <= 'F' || ch >= 'a' && ch <= 'f'
}

func allDigits(raw string) bool {
	for i := 0; i < len(raw); i++ {
		if !isDigitByte(raw[i]) {
			return false
		}
	}
	return true
}

var emailPattern = regexp.MustCompile("^[A-Za-z0-9!#$%&'*+/=?^_`{|}~-]+(?:\\.[A-Za-z0-9!#$%&'*+/=?^_`{|}~-]+)*@([A-Za-z0-9.-]+)$")

func validEmail(raw string) bool {
	m := emailPattern.FindStringSubmatch(raw)
	if m == nil {
		return false
	}
	normalized, ok := normalizeHostname(m[1])
	return ok && normalized == strings.ToLower(m[1])
}

func normalizeHostname(raw string) (string, bool) {
	host := strings.TrimSuffix(raw, ".")
	if host == "" || len(host) > 253 || !isASCII(host) {
		return "", false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", false
		}
		for _, ch := range label {
			if ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-' {
				continue
			}
			return "", false
		}
	}
	return strings.ToLower(host), true
}

func isASCII(raw string) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func (c *compiler) uniques(b body, owner resolve.DeclarationSymbol, src resolve.DeclarationSource, cols []Column) []UniqueConstraint {
	colByID := map[string]string{}
	invalidColumns := map[string]bool{}
	for _, col := range cols {
		if col.ValueType == "" {
			invalidColumns[col.ID] = true
			continue
		}
		colByID[col.ID] = col.Address
	}
	out := []UniqueConstraint{}
	for _, it := range b.items {
		if it.name != "unique" {
			continue
		}
		if it.block || len(it.args) != 2 || it.args[0].kind != syntax.TokenIdentifier || firstNode(it.args[1].node, syntax.NodeList) == nil {
			c.diag("LDL1102", "unknown_or_duplicate_schema_member", src, it.span, "invalid unique", "", owner.Address)
			continue
		}
		id := it.args[0].raw
		decl := c.columnDecl[childKey(&owner.Symbol, resolve.KindConstraint, id)]
		u := UniqueConstraint{ID: id, Address: decl.Address, ColumnAddresses: []string{}}
		valid := true
		invalidDependency := false
		seenColumns := map[string]syntax.Span{}
		for _, v := range listValues(it.args[1].node) {
			if prev, duplicate := seenColumns[v.raw]; duplicate {
				c.diagRelated("LDL1102", "unknown_or_duplicate_schema_member", src, v.span, "duplicate unique column", u.Address, owner.Address, prev)
				valid = false
				continue
			}
			seenColumns[v.raw] = v.span
			addr, ok := colByID[v.raw]
			if !ok {
				if invalidColumns[v.raw] {
					valid = false
					invalidDependency = true
					continue
				}
				c.diag("LDL1301", "unknown_or_ambiguous_symbol", src, v.span, "unknown column", u.Address, owner.Address)
				valid = false
				continue
			}
			u.ColumnAddresses = append(u.ColumnAddresses, addr)
		}
		if len(u.ColumnAddresses) == 0 && !invalidDependency {
			c.diag("LDL1102", "unknown_or_duplicate_schema_member", src, it.span, "empty unique", u.Address, owner.Address)
			valid = false
		}
		if valid && len(u.ColumnAddresses) > 0 {
			out = append(out, u)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	return out
}

func (c *compiler) childReservations(owner resolve.StableSymbol) ([]string, []string) {
	cols, constraints := []string{}, []string{}
	for _, r := range c.input.Resolve.Identity.Reservations {
		if compareOwner(r.Owner, owner) != 0 {
			continue
		}
		switch r.Kind {
		case resolve.KindColumn:
			cols = append(cols, r.ID)
		case resolve.KindConstraint:
			constraints = append(constraints, r.ID)
		}
	}
	sort.Strings(cols)
	sort.Strings(constraints)
	return cols, constraints
}

func (v value) string() string {
	if v.kind == syntax.TokenString {
		s, err := strconv.Unquote(v.raw)
		if err == nil {
			return normalizeString(s)
		}
	}
	if v.kind == syntax.TokenHeredoc {
		return heredocText(v.raw)
	}
	return normalizeString(v.raw)
}

func (v value) authoredString() (string, bool) {
	if v.kind != syntax.TokenString {
		return "", false
	}
	s, err := strconv.Unquote(v.raw)
	if err != nil {
		return "", false
	}
	return s, true
}

func (v value) integer() (int64, bool) {
	if v.kind != syntax.TokenInteger {
		return 0, false
	}
	n, err := strconv.ParseInt(v.raw, 10, 64)
	return n, err == nil
}

func (v value) number() (float64, bool) {
	if v.kind != syntax.TokenInteger && v.kind != syntax.TokenNumber {
		return 0, false
	}
	f, err := strconv.ParseFloat(v.raw, 64)
	if f == 0 {
		f = 0
	}
	return f, err == nil && !math.IsInf(f, 0) && !math.IsNaN(f)
}

func heredocText(raw string) string {
	normalized := strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n")
	firstBreak := strings.IndexByte(normalized, '\n')
	if firstBreak < 0 {
		return normalizeString(normalized)
	}
	rest := normalized[firstBreak+1:]
	closingBreak := strings.LastIndexByte(rest, '\n')
	content := ""
	if closingBreak >= 0 {
		content = rest[:closingBreak+1]
	}
	lines := strings.Split(content, "\n")
	indent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		width := leadingHorizontalSpace(line)
		if indent < 0 || width < indent {
			indent = width
		}
	}
	if indent > 0 {
		for i, line := range lines {
			if strings.TrimSpace(line) != "" {
				lines[i] = line[indent:]
			}
		}
		content = strings.Join(lines, "\n")
	}
	return normalizeString(content)
}

func leadingHorizontalSpace(raw string) int {
	i := 0
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t') {
		i++
	}
	return i
}

func normalizeString(raw string) string {
	raw = strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n")
	return norm.NFC.String(raw)
}

func (c *compiler) diag(code, key string, src resolve.DeclarationSource, span syntax.Span, msg, subject, owner string) {
	d := resolve.Diagnostic{Code: code, Severity: "error", MessageKey: key, Arguments: map[string]string{}, Message: msg, SubjectAddress: subject, OwnerAddress: owner, Range: c.rangeOf(src, span)}
	c.diagnostics = append(c.diagnostics, d)
}

func (c *compiler) diagRelated(code, key string, src resolve.DeclarationSource, span syntax.Span, msg, subject, owner string, prev syntax.Span) {
	c.diag(code, key, src, span, msg, subject, owner)
	c.diagnostics[len(c.diagnostics)-1].Related = append(c.diagnostics[len(c.diagnostics)-1].Related, resolve.DiagnosticRelated{Relation: "previous", Range: c.rangeOf(src, prev), SubjectAddress: subject, OwnerAddress: owner})
}

func (c *compiler) rangeOf(src resolve.DeclarationSource, span syntax.Span) *resolve.SourceRange {
	return &resolve.SourceRange{Origin: sourceOrigin(src.Module.Origin), ModulePath: src.Module.Path, StartByte: span.Start, EndByte: span.End}
}

func declarationHeaderSpan(src resolve.DeclarationSource) syntax.Span {
	return headerSpan(src.Node, src.Range)
}

func itemHeaderSpan(it *item) syntax.Span {
	if it == nil {
		return syntax.Span{}
	}
	return headerSpan(it.node, it.span)
}

func headerSpan(node *syntax.Node, fallback syntax.Span) syntax.Span {
	if node == nil {
		return fallback
	}
	var first, last *syntax.Token
	appendToken := func(token syntax.Token) {
		switch token.Kind {
		case syntax.TokenNewline, syntax.TokenLineComment, syntax.TokenDocComment, syntax.TokenModuleDoc, syntax.TokenEOF:
			return
		}
		copy := token
		if first == nil {
			first = &copy
		}
		last = &copy
	}
	for _, element := range node.Children {
		switch child := element.(type) {
		case syntax.TokenElement:
			appendToken(child.Token)
		case *syntax.Node:
			if child.Kind == syntax.NodeBlock || child.Kind == syntax.NodeItemBlock {
				if first != nil {
					return syntax.Span{Start: first.Span.Start, End: last.Span.End}
				}
				return fallback
			}
			for _, token := range nodeTokens(child) {
				appendToken(token)
			}
		}
	}
	if first == nil {
		return fallback
	}
	return syntax.Span{Start: first.Span.Start, End: last.Span.End}
}

func sourceOrigin(origin resolve.Origin) resolve.SourceOrigin {
	if origin.Kind == resolve.OriginPack {
		return resolve.SourceOrigin{Kind: resolve.OriginPack, PackAddress: packAddress(origin)}
	}
	return resolve.SourceOrigin{Kind: resolve.OriginProject}
}

func packAddress(origin resolve.Origin) string {
	return "ldl:pack:" + origin.Publisher + ":" + origin.PackName
}

func firstNode(n *syntax.Node, kind syntax.NodeKind) *syntax.Node {
	for _, child := range children(n) {
		if child.Kind == kind {
			return child
		}
	}
	return nil
}

func children(n *syntax.Node) []*syntax.Node {
	if n == nil {
		return nil
	}
	var out []*syntax.Node
	for _, child := range n.Children {
		if node, ok := child.(*syntax.Node); ok {
			out = append(out, node)
		}
	}
	return out
}

func directTokens(n *syntax.Node) []syntax.Token {
	if n == nil {
		return nil
	}
	var out []syntax.Token
	for _, child := range n.Children {
		if tok, ok := child.(syntax.TokenElement); ok {
			out = append(out, tok.Token)
		}
	}
	return out
}

func nodeTokens(n *syntax.Node) []syntax.Token {
	var out []syntax.Token
	syntax.Walk(n, func(node *syntax.Node) {
		for _, child := range node.Children {
			if tok, ok := child.(syntax.TokenElement); ok {
				switch tok.Token.Kind {
				case syntax.TokenNewline, syntax.TokenLineComment, syntax.TokenDocComment, syntax.TokenModuleDoc, syntax.TokenEOF:
				default:
					out = append(out, tok.Token)
				}
			}
		}
	})
	return out
}

func values(n *syntax.Node) []value {
	var out []value
	for _, child := range n.Children {
		switch c := child.(type) {
		case syntax.TokenElement:
			switch c.Token.Kind {
			case syntax.TokenIdentifier, syntax.TokenString, syntax.TokenInteger, syntax.TokenNumber, syntax.TokenHeredoc:
				out = append(out, value{raw: c.Token.Raw, kind: c.Token.Kind, span: c.Token.Span})
			}
		case *syntax.Node:
			if c.Kind == syntax.NodeValue {
				toks := nodeTokens(c)
				if len(toks) > 0 {
					out = append(out, value{raw: valueRaw(toks), kind: toks[0].Kind, span: c.Span, node: c})
				}
			}
		}
	}
	return out
}

func listValues(n *syntax.Node) []value {
	var out []value
	for _, list := range children(n) {
		if list.Kind != syntax.NodeList {
			continue
		}
		for _, child := range children(list) {
			if child.Kind == syntax.NodeValue {
				toks := nodeTokens(child)
				if len(toks) > 0 {
					out = append(out, value{raw: valueRaw(toks), kind: toks[0].Kind, span: child.Span, node: child})
				}
			}
		}
	}
	return out
}

type objectEntry struct {
	key     string
	keySpan syntax.Span
	value   value
	span    syntax.Span
}

func objectValues(n *syntax.Node) []objectEntry {
	object := firstNode(n, syntax.NodeObject)
	if object == nil {
		return nil
	}
	var out []objectEntry
	for _, child := range children(object) {
		if child.Kind != syntax.NodeObjectItem {
			continue
		}
		toks := directTokens(child)
		valueNode := firstNode(child, syntax.NodeValue)
		valueTokens := nodeTokens(valueNode)
		if len(toks) == 0 || len(valueTokens) == 0 {
			continue
		}
		out = append(out, objectEntry{
			key:     tokenString(toks[0]),
			keySpan: toks[0].Span,
			value:   value{raw: valueRaw(valueTokens), kind: valueTokens[0].Kind, span: valueNode.Span, node: valueNode},
			span:    child.Span,
		})
	}
	return out
}

func valueRaw(toks []syntax.Token) string {
	var ids []string
	for _, tok := range toks {
		if tok.Kind == syntax.TokenIdentifier {
			ids = append(ids, tok.Raw)
		}
	}
	if len(ids) > 1 {
		return strings.Join(ids, ".")
	}
	return toks[0].Raw
}

func tokenString(tok syntax.Token) string {
	if tok.Kind == syntax.TokenString {
		s, err := strconv.Unquote(tok.Raw)
		if err == nil {
			return s
		}
	}
	return tok.Raw
}

func childKey(owner *resolve.StableSymbol, kind resolve.SubjectKind, id string) string {
	if owner == nil {
		return "|" + string(kind) + "|" + id
	}
	return fmt.Sprintf("%v|%s|%s", *owner, kind, id)
}

func compareOwner(a, b resolve.StableSymbol) int {
	return strings.Compare(fmt.Sprintf("%v", a), fmt.Sprintf("%v", b))
}

func set(vals ...string) map[string]bool {
	out := map[string]bool{}
	for _, v := range vals {
		out[v] = true
	}
	return out
}

func contains(vals []string, needle string) bool {
	for _, v := range vals {
		if v == needle {
			return true
		}
	}
	return false
}
