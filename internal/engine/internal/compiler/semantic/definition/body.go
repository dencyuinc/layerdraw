// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package definition

import (
	"fmt"
	"math"
	"net/netip"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
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
		seen[key] = entry.span
		out[key] = normalizeString(entry.value.string())
	}
	return out
}

func (c *compiler) optionalString(b body, name string, src resolve.DeclarationSource, subject, owner string) *string {
	it := b.stmt(name)
	if it == nil {
		return nil
	}
	if len(it.args) != 1 || it.args[0].kind != syntax.TokenString && it.args[0].kind != syntax.TokenHeredoc {
		c.diag("LDL1102", "unknown_or_duplicate_schema_member", src, it.span, "expected string", subject, owner)
		return nil
	}
	s := it.args[0].string()
	return &s
}

func (c *compiler) requiredString(b body, name string, src resolve.DeclarationSource, subject, owner, code, key string) string {
	s := c.optionalString(b, name, src, subject, owner)
	if s == nil {
		c.diag(code, key, src, src.Range, "missing required string", subject, owner)
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
		c.diag(code, key, src, it.span, "expected boolean", subject, owner)
		return def
	}
	return it.args[0].raw == "true"
}

func (c *compiler) optionalEnumDefault(b body, name, def string, allowed map[string]bool, src resolve.DeclarationSource, subject, owner, code, key string) string {
	it := b.stmt(name)
	if it == nil {
		return def
	}
	if len(it.args) != 1 || !allowed[it.args[0].raw] {
		c.diag(code, key, src, it.span, "invalid enum", subject, owner)
		return def
	}
	return it.args[0].raw
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
	s := c.optionalString(b, name, src, subject, "")
	if s == nil {
		return nil
	}
	locator, ok := resolveAssetLocator(src.Module.Path, *s)
	if !ok {
		c.diag("LDL1201", "module_pack_or_asset_resolution_failed", src, b.stmt(name).args[0].span, "invalid asset locator", subject, "")
		return nil
	}
	return &AuthoredAsset{AuthoredPath: *s, Locator: locator, Origin: src.Module.Origin, ModulePath: src.Module.Path, SourceRange: c.rangeOf(src, b.stmt(name).args[0].span)}
}

func resolveAssetLocator(modulePath, raw string) (string, bool) {
	if raw == "" || assetSchemePattern.MatchString(raw) || strings.HasPrefix(raw, "/") || strings.Contains(raw, "\\") || containsControl(raw) {
		return "", false
	}
	clean := path.Clean(path.Join(path.Dir(modulePath), raw))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}

var assetSchemePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)

func containsControl(raw string) bool {
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func (c *compiler) representation(b body, src resolve.DeclarationSource, subject, owner string) Representation {
	it := b.stmt("representation")
	if it == nil || len(it.args) == 0 {
		c.diag("LDL1102", "unknown_or_duplicate_schema_member", src, src.Range, "missing representation", subject, owner)
		return Representation{}
	}
	switch it.args[0].raw {
	case "container", "table":
		if len(it.args) != 1 {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, it.span, "invalid representation", subject, owner)
			return Representation{}
		}
		return Representation{Kind: it.args[0].raw}
	case "shape":
		if len(it.args) != 2 || !set("rect", "rounded", "ellipse", "diamond", "cylinder", "cloud", "hexagon", "person", "device")[it.args[1].raw] {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, it.span, "invalid representation", subject, owner)
			return Representation{}
		}
		return Representation{Kind: "shape", Shape: it.args[1].raw}
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
		col := Column{ID: id, Address: decl.Address, DisplayName: normalizeString(stmt.args[0].string()), ValueType: ScalarType(stmt.args[1].raw), ReservedEnumValues: []string{}}
		c.columnModifiers(&col, stmt.args[1], stmt.args[2:], src, owner.Address)
		cols = append(cols, col)
	}
	return cols
}

func (c *compiler) columnModifiers(col *Column, typeValue value, args []value, src resolve.DeclarationSource, owner string) {
	if !set("string", "integer", "number", "boolean", "enum", "date", "datetime")[string(col.ValueType)] {
		c.diag("LDL1401", "scalar_or_column_type_mismatch", src, typeValue.span, "invalid scalar type", col.Address, owner)
	}
	seen := map[string]syntax.Span{}
	lastRank := -1
	defaultSpan := typeValue.span
	if len(args) > 0 && firstNode(args[0].node, syntax.NodeList) != nil {
		if col.ValueType != ScalarEnum {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, args[0].span, "enum values forbidden", col.Address, owner)
		} else {
			col.EnumValues = c.enumList(args[0], false, src, col.Address, owner)
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
			col.Default = c.scalar(operand, col, src, owner)
			defaultSpan = operand.span
			i++
		case "format":
			format := operand.raw
			if operand.kind != syntax.TokenIdentifier || col.ValueType != ScalarString || !stringFormats[format] {
				c.diag("LDL1401", "scalar_or_column_type_mismatch", src, operand.span, "invalid format", col.Address, owner)
			}
			col.Format = &format
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
			}
			if name == "min" {
				col.Min = &f
			} else {
				col.Max = &f
			}
			i++
		case "min_length", "max_length":
			n, ok := operand.integer()
			if !ok || n < 0 || !jsonSafeInteger(n) || col.ValueType != ScalarString {
				c.diag("LDL1401", "scalar_or_column_type_mismatch", src, operand.span, "invalid length bound", col.Address, owner)
			}
			if name == "min_length" {
				col.MinLength = &n
			} else {
				col.MaxLength = &n
			}
			i++
		case "reserve_values":
			if col.ValueType != ScalarEnum || firstNode(operand.node, syntax.NodeList) == nil {
				c.diag("LDL1401", "scalar_or_column_type_mismatch", src, operand.span, "reserved enum values forbidden", col.Address, owner)
			} else {
				col.ReservedEnumValues = c.enumList(operand, true, src, col.Address, owner)
			}
			i++
		}
	}
	if col.ValueType == ScalarEnum && len(col.EnumValues) == 0 {
		c.diag("LDL1401", "scalar_or_column_type_mismatch", src, typeValue.span, "enum requires values", col.Address, owner)
	}
	for _, active := range col.EnumValues {
		if contains(col.ReservedEnumValues, active) {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, typeValue.span, "active and reserved enum values overlap", col.Address, owner)
		}
	}
	if col.Min != nil && col.Max != nil && *col.Min > *col.Max || col.MinLength != nil && col.MaxLength != nil && *col.MinLength > *col.MaxLength {
		c.diag("LDL1401", "scalar_or_column_type_mismatch", src, typeValue.span, "invalid bounds", col.Address, owner)
	}
	if col.Default != nil {
		c.validateDefault(col, src, owner, defaultSpan)
	}
}

var columnModifierRanks = map[string]int{
	"reserve_values": 0, "required": 1, "default": 2, "format": 3,
	"min": 4, "max": 5, "min_length": 6, "max_length": 7,
}

var stringFormats = set("uri", "email", "hostname", "ipv4", "ipv6", "cidr")

func (c *compiler) enumList(v value, canonical bool, src resolve.DeclarationSource, subject, owner string) []string {
	var out []string
	seen := map[string]syntax.Span{}
	for _, item := range listValues(v.node) {
		if item.kind != syntax.TokenIdentifier && item.kind != syntax.TokenString {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, item.span, "invalid enum value", subject, owner)
			continue
		}
		s := normalizeString(item.string())
		if s == "" {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, item.span, "empty enum value", subject, owner)
			continue
		}
		if prev, ok := seen[s]; ok {
			c.diagRelated("LDL1102", "unknown_or_duplicate_schema_member", src, item.span, "duplicate enum value", subject, owner, prev)
			continue
		}
		seen[s] = item.span
		out = append(out, s)
	}
	if canonical {
		sort.Strings(out)
	}
	return out
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

func (c *compiler) validateDefault(col *Column, src resolve.DeclarationSource, owner string, span syntax.Span) {
	s := col.Default
	if s == nil {
		return
	}
	if col.ValueType == ScalarString && col.Format != nil {
		normalized, ok := normalizeStringFormat(*col.Format, s.String)
		if !ok {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, span, "default format mismatch", col.Address, owner)
			return
		}
		s.String = normalized
	}
	if col.ValueType == ScalarString {
		length := int64(utf8.RuneCountInString(s.String))
		if col.MinLength != nil && length < *col.MinLength || col.MaxLength != nil && length > *col.MaxLength {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, span, "default length mismatch", col.Address, owner)
		}
	}
	if col.ValueType == ScalarInteger {
		value := float64(s.Int)
		if col.Min != nil && value < *col.Min || col.Max != nil && value > *col.Max {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, span, "default range mismatch", col.Address, owner)
		}
	}
	if col.ValueType == ScalarNumber {
		if col.Min != nil && s.Float < *col.Min || col.Max != nil && s.Float > *col.Max {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, span, "default range mismatch", col.Address, owner)
		}
	}
}

func normalizeStringFormat(format, raw string) (string, bool) {
	switch format {
	case "uri":
		u, err := url.Parse(raw)
		if err != nil || !u.IsAbs() || u.Scheme == "" {
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

var emailPattern = regexp.MustCompile("^[A-Za-z0-9!#$%&'*+/=?^_`{|}~-]+(?:\\.[A-Za-z0-9!#$%&'*+/=?^_`{|}~-]+)*@([A-Za-z0-9.-]+)$")

func validEmail(raw string) bool {
	if len(raw) > 254 {
		return false
	}
	m := emailPattern.FindStringSubmatch(raw)
	if m == nil {
		return false
	}
	local := raw[:strings.LastIndexByte(raw, '@')]
	if len(local) > 64 {
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
	for _, col := range cols {
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
		seenColumns := map[string]syntax.Span{}
		for _, v := range listValues(it.args[1].node) {
			if prev, duplicate := seenColumns[v.raw]; duplicate {
				c.diagRelated("LDL1102", "unknown_or_duplicate_schema_member", src, v.span, "duplicate unique column", u.Address, owner.Address, prev)
				continue
			}
			seenColumns[v.raw] = v.span
			addr, ok := colByID[v.raw]
			if !ok {
				c.diag("LDL1301", "unknown_or_ambiguous_symbol", src, v.span, "unknown column", u.Address, owner.Address)
				continue
			}
			u.ColumnAddresses = append(u.ColumnAddresses, addr)
		}
		if len(u.ColumnAddresses) == 0 {
			c.diag("LDL1102", "unknown_or_duplicate_schema_member", src, it.span, "empty unique", u.Address, owner.Address)
		}
		out = append(out, u)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	return out
}

func (c *compiler) validateReserve(it *item, src resolve.DeclarationSource, owner string) {
	if it == nil {
		return
	}
	c.rejectUnknown(it.nested, src, specs("columns", "constraints"))
	for _, name := range []string{"columns", "constraints"} {
		member := it.nested.stmt(name)
		if member == nil {
			continue
		}
		if len(member.args) != 1 || firstNode(member.args[0].node, syntax.NodeList) == nil {
			c.diag("LDL1102", "unknown_or_duplicate_schema_member", src, member.span, "reservation requires one list", owner, "")
		}
	}
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
	key   string
	value value
	span  syntax.Span
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
			key:   tokenString(toks[0]),
			value: value{raw: valueRaw(valueTokens), kind: valueTokens[0].Kind, span: valueNode.Span, node: valueNode},
			span:  child.Span,
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
