// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package sourceplanner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/index"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
)

func TestPlannerRejectsEveryStalePreconditionClass(t *testing.T) {
	compiler := newTestCompiler()
	planner := NewSourcePlanner(compiler)
	input := testInput(map[string][]byte{
		"document.ldl": []byte("project p \"Project\" {}\nentity_type service \"Service\" {\n representation shape rect\n}\n"),
	})
	base, snapshot := testBase(t, compiler, input)
	valid := fullPreconditions(snapshot, base.Generation)
	badDigest := Digest("sha256:" + strings.Repeat("f", 64))

	tests := []struct {
		name     string
		mutate   func(*EngineEditPreconditions)
		conflict string
	}{
		{
			name: "generation",
			mutate: func(value *EngineEditPreconditions) {
				value.Generation.Value++
			},
			conflict: "stale_revision",
		},
		{
			name: "subject hash",
			mutate: func(value *EngineEditPreconditions) {
				value.ExpectedSubjectHashes[0].Hash = badDigest
			},
			conflict: "subject_changed",
		},
		{
			name: "subtree hash",
			mutate: func(value *EngineEditPreconditions) {
				value.ExpectedSubtreeHashes[0].Hash = badDigest
			},
			conflict: "subtree_changed",
		},
		{
			name: "child set hash",
			mutate: func(value *EngineEditPreconditions) {
				value.ExpectedChildSets[0].Hash = badDigest
			},
			conflict: "child_set_changed",
		},
		{
			name: "source digest",
			mutate: func(value *EngineEditPreconditions) {
				(*value.ExpectedSourceDigests)[0].Digest = badDigest
			},
			conflict: "stale_revision",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			preconditions := clonePreconditions(valid)
			test.mutate(&preconditions)
			plan, err := planner.OrganizeWorkspace(context.Background(), base, OrganizeWorkspaceInput{
				Limits: testLimits(), Preconditions: preconditions, Strategy: "standard_layout",
			})
			if err != nil {
				t.Fatal(err)
			}
			if plan.Preview.Status != "invalid" || len(plan.Preview.Conflicts) != 1 || plan.Preview.Conflicts[0].Kind != test.conflict {
				t.Fatalf("conflicts = %+v, want %q", plan.Preview.Conflicts, test.conflict)
			}
		})
	}
}

func TestPlannerRequiresOperationSpecificGuards(t *testing.T) {
	compiler := newTestCompiler()
	planner := NewSourcePlanner(compiler)
	input := testInput(map[string][]byte{
		"document.ldl": []byte("project p \"Project\" {}\nentity_type service \"Service\" {\n representation shape rect\n}\n"),
	})
	base, _ := testBase(t, compiler, input)
	empty := EngineEditPreconditions{
		Generation: base.Generation, ExpectedChildSets: []ExpectedChildSet{},
		ExpectedSubjectHashes: []ExpectedHash{}, ExpectedSubtreeHashes: []ExpectedHash{},
	}

	patchRef, patchBlobs := testBlob("guard-patch", []byte("x"))
	patchPlan, err := planner.PreviewSourcePatch(context.Background(), base, PreviewSourcePatchInput{
		Limits: testLimits(), Preconditions: empty,
		Patch: SourcePatchBatch{Patches: []SourcePatchInput{{
			SourceRange:          SourceRange{Origin: SourceOrigin{Kind: OriginKindProject}, ModulePath: "document.ldl"},
			ExpectedSourceDigest: digest(input.ProjectSourceTree["document.ldl"]), ReplacementBlob: patchRef,
		}}},
	}, patchBlobs)
	assertMissingGuard(t, patchPlan, err)

	fragmentRef, fragmentBlobs := testBlob("guard-fragment", []byte("entity_type next \"Next\" {\n representation shape rect\n}\n"))
	fragmentPlan, err := planner.PreviewFragment(context.Background(), base, PreviewFragmentInput{
		Limits: testLimits(), Preconditions: empty,
		Fragment: FragmentInput{Intent: "insert", InsertionOwner: "ldl:project:p", AllowedKinds: []SubjectKind{SubjectKindEntityType}, FragmentBlob: fragmentRef},
	}, fragmentBlobs)
	assertMissingGuard(t, fragmentPlan, err)

	formatPlan, err := planner.FormatScope(context.Background(), base, FormatScopeInput{
		Limits: testLimits(), Preconditions: empty, ScopeAddresses: []StableAddress{"ldl:project:p:entity-type:service"},
	})
	assertMissingGuard(t, formatPlan, err)

	organizationPlan, err := planner.OrganizeWorkspace(context.Background(), base, OrganizeWorkspaceInput{
		Limits: testLimits(), Preconditions: empty, Strategy: "standard_layout",
	})
	assertMissingGuard(t, organizationPlan, err)
}

func TestPlannerRejectsInvalidBlobMetadata(t *testing.T) {
	value := []byte("x")
	valid, blobs := testBlob("blob", value)
	tests := []struct {
		name  string
		ref   BlobRef
		blobs PlannerBlobs
	}{
		{name: "missing", ref: valid, blobs: PlannerBlobs{}},
		{name: "wrong lifetime", ref: changedBlobRef(valid, func(ref *BlobRef) { ref.Lifetime = "session" }), blobs: blobs},
		{name: "wrong media type", ref: changedBlobRef(valid, func(ref *BlobRef) { ref.MediaType = "application/octet-stream" }), blobs: blobs},
		{name: "wrong size", ref: changedBlobRef(valid, func(ref *BlobRef) { ref.Size++ }), blobs: blobs},
		{name: "wrong digest", ref: changedBlobRef(valid, func(ref *BlobRef) { ref.Digest = Digest("sha256:" + strings.Repeat("0", 64)) }), blobs: blobs},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, diagnostic := resolveBlob(test.ref, test.blobs, textMediaType)
			if diagnostic == nil || result != nil {
				t.Fatalf("result = %q, diagnostic = %+v", result, diagnostic)
			}
		})
	}
}

func TestFragmentRejectsMalformedSyntaxKindIntentAndPlacement(t *testing.T) {
	compiler := newTestCompiler()
	input := testInput(map[string][]byte{
		"document.ldl": []byte("project p \"Project\" {}\nentity_type service \"Service\" {\n representation shape rect\n}\n"),
	})
	_, before := testBase(t, compiler, input)
	baseFragment := FragmentInput{Intent: "insert", InsertionOwner: "ldl:project:p", AllowedKinds: []SubjectKind{SubjectKindEntityType}}
	target := StableAddress("ldl:project:p:entity-type:service")
	missing := StableAddress("ldl:project:p:entity-type:missing")

	tests := []struct {
		name     string
		fragment []byte
		mutate   func(*FragmentInput)
	}{
		{name: "invalid UTF-8", fragment: []byte{0xff}},
		{name: "invalid syntax", fragment: []byte("entity_type")},
		{name: "no declaration", fragment: []byte("// trivia only\n")},
		{name: "kind outside allowlist", fragment: []byte("reference note <<-TEXT\nhello\nTEXT\n")},
		{name: "unknown intent", fragment: []byte("entity_type next \"Next\" {\n representation shape rect\n}\n"), mutate: func(value *FragmentInput) { value.Intent = "unknown" }},
		{name: "replace without target", fragment: []byte("entity_type next \"Next\" {\n representation shape rect\n}\n"), mutate: func(value *FragmentInput) { value.Intent = "replace" }},
		{name: "replace many", fragment: []byte("entity_type a \"A\" {\n representation shape rect\n}\nentity_type b \"B\" {\n representation shape rect\n}\n"), mutate: func(value *FragmentInput) {
			value.Intent = "replace"
			value.ReplacementTarget = &target
		}},
		{name: "before without anchor", fragment: []byte("entity_type next \"Next\" {\n representation shape rect\n}\n"), mutate: func(value *FragmentInput) { value.Placement = &PlacementHint{Position: "before"} }},
		{name: "missing anchor", fragment: []byte("entity_type next \"Next\" {\n representation shape rect\n}\n"), mutate: func(value *FragmentInput) {
			value.Placement = &PlacementHint{Position: "after", GroupAnchorAddress: &missing}
		}},
		{name: "anchor module mismatch", fragment: []byte("entity_type next \"Next\" {\n representation shape rect\n}\n"), mutate: func(value *FragmentInput) {
			module := "other.ldl"
			value.Placement = &PlacementHint{Position: "after", GroupAnchorAddress: &target, ModulePath: &module}
		}},
		{name: "missing placement module", fragment: []byte("entity_type next \"Next\" {\n representation shape rect\n}\n"), mutate: func(value *FragmentInput) {
			module := "other.ldl"
			value.Placement = &PlacementHint{Position: "end", ModulePath: &module}
		}},
		{name: "invalid position", fragment: []byte("entity_type next \"Next\" {\n representation shape rect\n}\n"), mutate: func(value *FragmentInput) { value.Placement = &PlacementHint{Position: "sideways"} }},
		{name: "missing replacement target", fragment: []byte("entity_type service \"Service\" {\n representation shape rect\n}\n"), mutate: func(value *FragmentInput) {
			value.Intent = "replace"
			value.ReplacementTarget = &missing
		}},
		{name: "replacement owner mismatch", fragment: []byte("entity_type service \"Service\" {\n representation shape rect\n}\n"), mutate: func(value *FragmentInput) {
			value.Intent = "replace"
			value.InsertionOwner = target
			value.ReplacementTarget = &target
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ref, blobs := testBlob("fragment-"+test.name, test.fragment)
			fragment := baseFragment
			fragment.FragmentBlob = ref
			if test.mutate != nil {
				test.mutate(&fragment)
			}
			_, diagnostics, _, err := applyFragment(context.Background(), compiler, input, before, fragment, blobs)
			if err != nil {
				t.Fatal(err)
			}
			if len(diagnostics) == 0 {
				t.Fatal("fragment was accepted")
			}
		})
	}
}

func TestSourceAndSemanticDiffClassification(t *testing.T) {
	beforeSource := map[string][]byte{
		"delete.ldl": []byte("delete"), "move-old.ldl": []byte("move"), "replace.ldl": []byte("hello λ world"),
	}
	afterSource := map[string][]byte{
		"create.ldl": []byte("create"), "move-new.ldl": []byte("move"), "replace.ldl": []byte("hello μ world"),
	}
	attachments := PlannerBlobs{}
	sourceDiff, changed, err := buildSourceDiff(beforeSource, afterSource, attachments)
	if err != nil {
		t.Fatal(err)
	}
	wantKinds := []SourceEditKind{SourceEditKindCreate, SourceEditKindDelete, SourceEditKindMove, SourceEditKindReplace}
	gotKinds := make([]SourceEditKind, len(sourceDiff.Edits))
	for index, edit := range sourceDiff.Edits {
		gotKinds[index] = edit.Kind
	}
	if !reflect.DeepEqual(gotKinds, wantKinds) || len(changed) != 5 || len(attachments) != 2 {
		t.Fatalf("kinds = %v, changed = %v, attachments = %d", gotKinds, changed, len(attachments))
	}

	root := "ldl:project:p"
	before := syntheticSnapshot(root,
		[]materialize.SubjectHash{
			{Address: root + ":entity-type:update", Kind: "entity_type", Hash: repeatedHash("1")},
			{Address: root + ":entity-type:old", Kind: "entity_type", Hash: repeatedHash("2")},
			{Address: root + ":layer:a", Kind: "layer", Hash: repeatedHash("3")},
			{Address: root + ":layer:a:entity:moved", Kind: "entity", Hash: repeatedHash("4")},
			{Address: root + ":reference:deleted", Kind: "reference", Hash: repeatedHash("5")},
			{Address: root + ":relation-type:deleted", Kind: "relation_type", Hash: repeatedHash("7")},
		},
		map[string]string{
			root + ":entity-type:update": root, root + ":entity-type:old": root, root + ":layer:a": root,
			root + ":layer:a:entity:moved": root + ":layer:a", root + ":reference:deleted": root,
			root + ":relation-type:deleted": root,
		})
	after := syntheticSnapshot(root,
		[]materialize.SubjectHash{
			{Address: root + ":entity-type:update", Kind: "entity_type", Hash: repeatedHash("a")},
			{Address: root + ":entity-type:new", Kind: "entity_type", Hash: repeatedHash("b")},
			{Address: root + ":layer:b", Kind: "layer", Hash: repeatedHash("3")},
			{Address: root + ":layer:b:entity:moved", Kind: "entity", Hash: repeatedHash("4")},
			{Address: root + ":reference:created", Kind: "reference", Hash: repeatedHash("6")},
			{Address: root + ":query:created", Kind: "query", Hash: repeatedHash("8")},
		},
		map[string]string{
			root + ":entity-type:update": root, root + ":entity-type:new": root, root + ":layer:b": root,
			root + ":layer:b:entity:moved": root + ":layer:b", root + ":reference:created": root,
			root + ":query:created": root,
		})
	semanticDiff := buildSemanticDiff(before, after)
	changeKinds := map[SemanticChangeKind]int{}
	for _, entry := range semanticDiff.Entries {
		changeKinds[entry.Kind]++
	}
	if changeKinds[SemanticChangeKindUpdated] != 1 || changeKinds[SemanticChangeKindRenamed] == 0 || changeKinds[SemanticChangeKindMoved] == 0 || changeKinds[SemanticChangeKindCreated] == 0 || changeKinds[SemanticChangeKindDeleted] == 0 {
		t.Fatalf("semantic changes = %+v", semanticDiff.Entries)
	}
}

func TestCanonicalDomainEncodingUsesGeneratedScalarShapes(t *testing.T) {
	value := struct {
		Generation Generation  `json:"generation"`
		Range      SourceRange `json:"range"`
		Blob       BlobRef     `json:"blob"`
	}{
		Generation: Generation{Namespace: "endpoint", DocumentID: "document", Value: 7},
		Range:      SourceRange{Origin: SourceOrigin{Kind: OriginKindProject}, ModulePath: "document.ldl", StartByte: 2, EndByte: 9},
		Blob:       BlobRef{BlobID: "blob", Digest: repeatedDigest("a"), Lifetime: BlobLifetimeRequest, MediaType: textMediaType, Size: 12},
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"generation":{"document_handle":{"endpoint_instance_id":"endpoint","value":"document"},"value":"7"},"range":{"end_byte":"9","module_path":"document.ldl","origin":{"kind":"project"},"start_byte":"2"},"blob":{"blob_id":"blob","digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","lifetime":"request","media_type":"text/plain; charset=utf-8","size":"12"}}`
	if string(encoded) != want {
		t.Fatalf("encoded = %s", encoded)
	}
}

func TestPlannerLimitsAndCancellationFailWithoutCandidate(t *testing.T) {
	compiler := newTestCompiler()
	planner := NewSourcePlanner(compiler)
	input := testInput(map[string][]byte{"document.ldl": []byte("project p \"Project\" {}\n// keep\n")})
	base, before := testBase(t, compiler, input)
	ref, blobs := testBlob("limit", []byte("kept"))
	start := bytes.Index(input.ProjectSourceTree["document.ldl"], []byte("keep"))
	request := PreviewSourcePatchInput{
		Limits:        WorkbenchLimits{MaxItems: 1, MaxOutputBytes: 10_000_000},
		Preconditions: fullPreconditions(before, base.Generation),
		Patch: SourcePatchBatch{Patches: []SourcePatchInput{{
			SourceRange:          SourceRange{Origin: SourceOrigin{Kind: OriginKindProject}, ModulePath: "document.ldl", StartByte: start, EndByte: start + 4},
			ExpectedSourceDigest: digest(input.ProjectSourceTree["document.ldl"]), ReplacementBlob: ref,
		}}},
	}
	_, err := planner.PreviewSourcePatch(context.Background(), base, request, blobs)
	var limitError *SourcePlannerLimitError
	if !errors.As(err, &limitError) || limitError.Resource != "max_items" {
		t.Fatalf("limit error = %T %v", err, err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = planner.OrganizeWorkspace(cancelled, base, OrganizeWorkspaceInput{
		Limits: testLimits(), Preconditions: fullPreconditions(before, base.Generation), Strategy: "standard_layout",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestSourcePatchRejectsOriginModuleDigestBlobAndEncodingFailures(t *testing.T) {
	source := []byte("project p \"Project\" {}\n")
	input := testInput(map[string][]byte{"document.ldl": source})
	replacement, blobs := testBlob("replacement", []byte("x"))
	valid := SourcePatchInput{
		SourceRange:          SourceRange{Origin: SourceOrigin{Kind: OriginKindProject}, ModulePath: "document.ldl"},
		ExpectedSourceDigest: digest(source), ReplacementBlob: replacement,
	}
	packAddress := PackRootAddress("ldl:pack:vendor:lib")
	invalidRef, invalidBlobs := testBlob("invalid", []byte{0xff})

	tests := []struct {
		name  string
		input CompileInput
		patch SourcePatchInput
		blobs PlannerBlobs
	}{
		{
			name:  "pack origin",
			input: input,
			patch: func() SourcePatchInput { value := valid; value.SourceRange.Origin.Kind = OriginKindPack; return value }(),
			blobs: blobs,
		},
		{
			name:  "pack address on project origin",
			input: input,
			patch: func() SourcePatchInput {
				value := valid
				value.SourceRange.Origin.PackAddress = &packAddress
				return value
			}(),
			blobs: blobs,
		},
		{
			name:  "missing module",
			input: input,
			patch: func() SourcePatchInput { value := valid; value.SourceRange.ModulePath = "missing.ldl"; return value }(),
			blobs: blobs,
		},
		{
			name:  "stale digest",
			input: input,
			patch: func() SourcePatchInput {
				value := valid
				value.ExpectedSourceDigest = repeatedDigest("0")
				return value
			}(),
			blobs: blobs,
		},
		{name: "missing blob", input: input, patch: valid, blobs: PlannerBlobs{}},
		{
			name:  "invalid replacement",
			input: input,
			patch: func() SourcePatchInput { value := valid; value.ReplacementBlob = invalidRef; return value }(),
			blobs: invalidBlobs,
		},
		{
			name:  "invalid source",
			input: testInput(map[string][]byte{"document.ldl": {0xff}}),
			patch: func() SourcePatchInput {
				value := valid
				value.ExpectedSourceDigest = digest([]byte{0xff})
				return value
			}(),
			blobs: blobs,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, diagnostics, _, err := applySourcePatches(context.Background(), test.input, Snapshot{}, SourcePatchBatch{Patches: []SourcePatchInput{test.patch}}, test.blobs)
			if err != nil {
				t.Fatal(err)
			}
			if len(diagnostics) == 0 {
				t.Fatal("patch was accepted")
			}
		})
	}
}

func TestCanonicalFormatterCoversLDLTokenFamilies(t *testing.T) {
	source := []byte("\xef\xbb\xbfproject p \"P\" {}\r\n// comment\r\nentity_type t \"T\" {\n tags [\"a\",\"b\"]\n}\n")
	formatted, ok := canonicalFormat(source)
	if !ok {
		t.Fatal("valid source was rejected")
	}
	if !bytes.HasPrefix(formatted, []byte{0xef, 0xbb, 0xbf}) || bytes.Contains(formatted, []byte("\r")) {
		t.Fatalf("BOM or line ending changed incorrectly: %q", formatted)
	}
	if !bytes.Contains(formatted, []byte("[\"a\", \"b\"]")) || !bytes.Contains(formatted, []byte("// comment")) {
		t.Fatalf("tokens or comments changed: %q", formatted)
	}
	second, ok := canonicalFormat(formatted)
	if !ok || !bytes.Equal(formatted, second) {
		t.Fatalf("formatter is not idempotent:\nfirst=%q\nsecond=%q", formatted, second)
	}
	if _, ok := canonicalFormat([]byte{0xff}); ok {
		t.Fatal("invalid UTF-8 was accepted")
	}

	kinds := map[string][]SubjectKind{
		"project":       {SubjectKindProject},
		"entity_type":   {SubjectKindEntityType},
		"relation_type": {SubjectKindRelationType},
		"layers":        {SubjectKindLayer},
		"entities":      {SubjectKindEntity},
		"relations":     {SubjectKindRelation},
		"rows":          {SubjectKindEntityRow, SubjectKindRelationRow},
		"relation_rows": {SubjectKindRelationRow},
		"query":         {SubjectKindQuery},
		"view":          {SubjectKindView},
		"reference":     {SubjectKindReference},
		"reserved":      nil,
	}
	for keyword, want := range kinds {
		if got := syntacticKinds([]byte(keyword)); !reflect.DeepEqual(got, want) {
			t.Fatalf("syntacticKinds(%q) = %v, want %v", keyword, got, want)
		}
	}
}

func TestOrganizationFallbackMovesModulesAndRewritesReferences(t *testing.T) {
	root := "ldl:project:p"
	declaration := []byte("entity_type service \"Service\" {\n representation shape rect\n}\n")
	input := testInput(map[string][]byte{
		"document.ldl": []byte("project p \"Project\" {}\n"),
		"one.ldl":      declaration,
		"two.ldl":      declaration,
	})
	origin := resolve.SourceOrigin{Kind: resolve.OriginProject}
	snapshot := Snapshot{SourceMap: index.SourceMapV1{
		Subjects: []index.SourceSubjectRecord{
			{Address: root, Kind: "project", ManifestRoot: true, Module: &index.ModuleRef{Origin: origin, ModulePath: "document.ldl"}},
			{Address: root + ":entity-type:service", Kind: "entity_type", Module: &index.ModuleRef{Origin: origin, ModulePath: "one.ldl"}, DeclarationRange: projectRange("one.ldl", 0, len(declaration)-1)},
			{Address: root + ":entity-type:service", Kind: "entity_type", Module: &index.ModuleRef{Origin: origin, ModulePath: "two.ldl"}, DeclarationRange: projectRange("two.ldl", 0, len(declaration)-1)},
		},
		Bindings: []index.SourceBindingRecord{{
			SourceAddress: root + ":entity-type:service", TargetAddress: "ldl:pack:vendor:lib:entity-type:remote",
		}},
	}}

	candidate, diagnostics, conflicts, err := organizeStandardLayout(context.Background(), input, snapshot)
	if err != nil || len(diagnostics) != 0 || len(conflicts) != 0 {
		t.Fatalf("diagnostics=%+v conflicts=%+v err=%v", diagnostics, conflicts, err)
	}
	primary := "schema/entity_types/service.ldl"
	collision := "schema/entity_types/service-" + shortDigest("two.ldl") + ".ldl"
	if _, ok := candidate.ProjectSourceTree[primary]; !ok {
		t.Fatalf("primary target missing: %v", sortedPaths(candidate.ProjectSourceTree))
	}
	if _, ok := candidate.ProjectSourceTree[collision]; !ok {
		t.Fatalf("collision target missing: %v", sortedPaths(candidate.ProjectSourceTree))
	}

	packInput := input
	packInput.Mode = CompilePack
	if _, diagnostics, _, err := organizeStandardLayout(context.Background(), packInput, snapshot); err != nil || len(diagnostics) == 0 {
		t.Fatalf("pack diagnostics=%v err=%v", diagnostics, err)
	}
	invalidInput := testInput(map[string][]byte{"document.ldl": {0xff}})
	if _, diagnostics, _, err := organizeStandardLayout(context.Background(), invalidInput, Snapshot{}); err != nil || len(diagnostics) == 0 {
		t.Fatalf("invalid-reference diagnostics=%v err=%v", diagnostics, err)
	}

	rewritten, ok := rewriteModuleReferences(
		[]byte("import { service } from \"./old.ldl\"\n"),
		"document.ldl", "document.ldl", map[string]string{"old.ldl": primary},
	)
	if !ok || !bytes.Contains(rewritten, []byte("./schema/entity_types/service.ldl")) {
		t.Fatalf("rewritten = %q, ok = %v", rewritten, ok)
	}
}

func TestAuthoringImpactMatchesAllTypedOperationActions(t *testing.T) {
	root := StableAddress("ldl:project:p")
	owner := StableAddress(string(root) + ":layer:app")
	address := func(id string) *StableAddress {
		value := StableAddress(string(root) + ":entity-type:" + id)
		return &value
	}
	before := Snapshot{
		DefinitionHash:     repeatedHash("1"),
		NormalizedDocument: &materialize.NormalizedDocument{Project: materialize.Project{Address: string(root)}},
		AuthoringSubjectClassification: []AuthoringSubjectClassification{
			{Address: string(*address("delete")), Capability: AuthoringCapabilitySchemaWrite},
			{Address: string(*address("rename-old")), Capability: AuthoringCapabilitySchemaWrite},
		},
		SourceMap: index.SourceMapV1{Subjects: []index.SourceSubjectRecord{
			{Address: string(*address("delete")), DeclarationRange: projectRange("b.ldl", 8, 9)},
			{Address: string(*address("rename-old")), DeclarationRange: projectRange("b.ldl", 3, 4)},
		}},
	}
	after := Snapshot{
		DefinitionHash:     repeatedHash("2"),
		NormalizedDocument: &materialize.NormalizedDocument{Project: materialize.Project{Address: string(root)}},
		AuthoringSubjectClassification: []AuthoringSubjectClassification{
			{Address: string(*address("create")), Capability: AuthoringCapabilityGraphWrite},
			{Address: string(*address("rename-new")), Capability: AuthoringCapabilitySchemaWrite},
			{Address: string(*address("move")), Capability: AuthoringCapabilityQueryWrite},
			{Address: string(*address("bind")), Capability: AuthoringCapabilityReferenceWrite},
			{Address: string(*address("update")), Capability: AuthoringCapabilityViewWrite},
		},
		SourceMap: index.SourceMapV1{Subjects: []index.SourceSubjectRecord{
			{Address: string(*address("create")), DeclarationRange: projectRange("a.ldl", 7, 9)},
			{Address: string(*address("rename-new")), DeclarationRange: projectRange("a.ldl", 1, 2)},
		}},
	}
	kinds := []SemanticChangeKind{
		SemanticChangeKindCreated, SemanticChangeKindDeleted, SemanticChangeKindRenamed,
		SemanticChangeKindMoved, SemanticChangeKindReferenceChanged, SemanticChangeKindUpdated,
	}
	ids := []string{"create", "delete", "rename-new", "move", "bind", "update"}
	entries := make([]SemanticDiffEntry, len(kinds))
	for index, kind := range kinds {
		entries[index] = SemanticDiffEntry{
			Kind: kind, SubjectKind: SubjectKindEntityType, OwnerAddress: &owner,
			BeforeAddress: address(ids[index]), AfterAddress: address(ids[index]), ChangedFieldPaths: []AuthoredFieldPath{},
		}
	}
	entries[0].BeforeAddress = nil
	entries[1].AfterAddress = nil
	entries[2].BeforeAddress = address("rename-old")
	semanticDiff := SemanticDiff{Entries: entries, Digest: repeatedDigest("3")}
	sourceDiff := SourceDiff{Digest: repeatedDigest("4"), Edits: []SourceEdit{{
		Kind: SourceEditKindCreate, AfterModule: pointerToModule(projectModule("a.ldl")),
	}}}

	impact := buildAuthoringImpact(before, after, semanticDiff, sourceDiff)
	if len(impact.Entries) != len(kinds) || len(impact.RequiredCapabilities) != 5 {
		t.Fatalf("impact = %+v", impact)
	}
	actions := map[AuthoringAction]bool{}
	graphFacts := false
	for _, entry := range impact.Entries {
		actions[entry.Action] = true
		graphFacts = graphFacts || entry.GraphFacts != nil
	}
	if len(actions) != len(kinds) || !graphFacts {
		t.Fatalf("actions=%v graphFacts=%v", actions, graphFacts)
	}
}

func TestStandardModulePathsFollowTypedGraphOwnership(t *testing.T) {
	root := "ldl:project:p"
	snapshot := Snapshot{TypedAST: TypedAST{Graph: &graph.MasterGraph{
		Entities:  []graph.Entity{{Address: root + ":layer:app:entity:api", LayerAddress: root + ":layer:app"}},
		Relations: []graph.Relation{{Address: root + ":layer:app:relation:calls", FromAddress: root + ":layer:app:entity:api"}},
	}}}
	tests := []struct {
		kind    materialize.SubjectKind
		address string
		want    string
	}{
		{kind: "entity_type", address: root + ":entity-type:t", want: "schema/entity_types/t.ldl"},
		{kind: "relation_type", address: root + ":relation-type:r", want: "schema/relation_types/r.ldl"},
		{kind: "layer", address: root + ":layer:app", want: "layers/layers.ldl"},
		{kind: "entity", address: root + ":layer:app:entity:api", want: "layers/app/api.ldl"},
		{kind: "relation", address: root + ":layer:app:relation:calls", want: "layers/app/calls.ldl"},
		{kind: "query", address: root + ":query:q", want: "views/q.ldl"},
		{kind: "view", address: root + ":view:v", want: "views/v.ldl"},
		{kind: "reference", address: root + ":reference:ref", want: "references/ref.ldl"},
	}
	for _, test := range tests {
		t.Run(string(test.kind), func(t *testing.T) {
			got := standardModulePath([]index.SourceSubjectRecord{{Address: test.address, Kind: test.kind}}, snapshot)
			if got != test.want {
				t.Fatalf("path = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFragmentReplaceAndAnchoredInsertStayOwnerScoped(t *testing.T) {
	compiler := newTestCompiler()
	planner := NewSourcePlanner(compiler)
	input := testInput(map[string][]byte{
		"document.ldl": []byte("project p \"Project\" {}\nentity_type service \"Service\" {\n representation shape rect\n}\n"),
	})
	base, before := testBase(t, compiler, input)
	target := StableAddress("ldl:project:p:entity-type:service")

	replacementRef, replacementBlobs := testBlob("replacement-fragment", []byte("entity_type service \"Renamed Service\" {\n representation shape rect\n}\n"))
	replacement, err := planner.PreviewFragment(context.Background(), base, PreviewFragmentInput{
		Limits: testLimits(), Preconditions: fullPreconditions(before, base.Generation),
		Fragment: FragmentInput{
			Intent: "replace", InsertionOwner: "ldl:project:p", AllowedKinds: []SubjectKind{SubjectKindEntityType},
			FragmentBlob: replacementRef, ReplacementTarget: &target,
		},
	}, replacementBlobs)
	if err != nil || replacement.Preview.Status != "valid" {
		t.Fatalf("replacement preview=%+v err=%v", replacement.Preview, err)
	}
	if !bytes.Contains(replacement.Candidate.ProjectSourceTree["document.ldl"], []byte("Renamed Service")) {
		t.Fatalf("replacement source=%q", replacement.Candidate.ProjectSourceTree["document.ldl"])
	}

	insertRef, insertBlobs := testBlob("anchored-fragment", []byte("entity_type alpha \"Alpha\" {\n representation shape rect\n}\n"))
	insert, err := planner.PreviewFragment(context.Background(), base, PreviewFragmentInput{
		Limits: testLimits(), Preconditions: fullPreconditions(before, base.Generation),
		Fragment: FragmentInput{
			Intent: "insert", InsertionOwner: "ldl:project:p", AllowedKinds: []SubjectKind{SubjectKindEntityType},
			FragmentBlob: insertRef, Placement: &PlacementHint{Position: "before", GroupAnchorAddress: &target},
		},
	}, insertBlobs)
	if err != nil || insert.Preview.Status != "valid" {
		t.Fatalf("insert preview=%+v err=%v", insert.Preview, err)
	}
	alpha := bytes.Index(insert.Candidate.ProjectSourceTree["document.ldl"], []byte("entity_type alpha"))
	service := bytes.Index(insert.Candidate.ProjectSourceTree["document.ldl"], []byte("entity_type service"))
	if alpha < 0 || service < 0 || alpha >= service {
		t.Fatalf("anchored insertion order is wrong: %q", insert.Candidate.ProjectSourceTree["document.ldl"])
	}
}

func TestScopedFormatterRejectsInvalidAndOverlappingScopes(t *testing.T) {
	source := []byte("project p \"Project\" {}\n")
	input := testInput(map[string][]byte{"document.ldl": source})
	origin := resolve.SourceOrigin{Kind: resolve.OriginProject}
	module := &index.ModuleRef{Origin: origin, ModulePath: "document.ldl"}
	makeSubject := func(address string, start, end int) index.SourceSubjectRecord {
		return index.SourceSubjectRecord{
			Address: address, Module: module,
			DeclarationRange: projectRange("document.ldl", start, end),
		}
	}

	t.Run("unknown scope", func(t *testing.T) {
		_, diagnostics, _, err := formatScopes(context.Background(), input, Snapshot{}, []StableAddress{"missing"})
		if err != nil || len(diagnostics) == 0 {
			t.Fatalf("diagnostics=%v err=%v", diagnostics, err)
		}
	})

	t.Run("stale range", func(t *testing.T) {
		snapshot := Snapshot{SourceMap: index.SourceMapV1{Subjects: []index.SourceSubjectRecord{makeSubject("scope", -1, 2)}}}
		_, diagnostics, _, err := formatScopes(context.Background(), input, snapshot, []StableAddress{"scope"})
		if err != nil || len(diagnostics) == 0 {
			t.Fatalf("diagnostics=%v err=%v", diagnostics, err)
		}
	})

	t.Run("invalid syntax", func(t *testing.T) {
		invalidInput := testInput(map[string][]byte{"document.ldl": {0xff}})
		snapshot := Snapshot{SourceMap: index.SourceMapV1{Subjects: []index.SourceSubjectRecord{makeSubject("scope", 0, 1)}}}
		_, diagnostics, _, err := formatScopes(context.Background(), invalidInput, snapshot, []StableAddress{"scope"})
		if err != nil || len(diagnostics) == 0 {
			t.Fatalf("diagnostics=%v err=%v", diagnostics, err)
		}
	})

	t.Run("contained scope", func(t *testing.T) {
		snapshot := Snapshot{SourceMap: index.SourceMapV1{Subjects: []index.SourceSubjectRecord{
			makeSubject("outer", 0, len(source)), makeSubject("inner", 1, 8),
		}}}
		_, diagnostics, _, err := formatScopes(context.Background(), input, snapshot, []StableAddress{"outer", "inner", "inner"})
		if err != nil || len(diagnostics) != 0 {
			t.Fatalf("diagnostics=%v err=%v", diagnostics, err)
		}
	})

	t.Run("partial overlap", func(t *testing.T) {
		snapshot := Snapshot{SourceMap: index.SourceMapV1{Subjects: []index.SourceSubjectRecord{
			makeSubject("left", 0, 12), makeSubject("right", 8, len(source)),
		}}}
		_, diagnostics, _, err := formatScopes(context.Background(), input, snapshot, []StableAddress{"left", "right"})
		if err != nil || len(diagnostics) == 0 {
			t.Fatalf("diagnostics=%v err=%v", diagnostics, err)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		snapshot := Snapshot{SourceMap: index.SourceMapV1{Subjects: []index.SourceSubjectRecord{makeSubject("scope", 0, len(source))}}}
		_, _, _, err := formatScopes(cancelled, input, snapshot, []StableAddress{"scope"})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestPlannerCompileLifecycleFailsClosed(t *testing.T) {
	base := SourcePlanningBase{
		Input:      testInput(map[string][]byte{"document.ldl": []byte("project p \"Project\" {}\n")}),
		Generation: Generation{Namespace: "test", DocumentID: "document", Value: 1},
	}
	preconditions := EngineEditPreconditions{
		Generation: base.Generation, ExpectedSubjectHashes: []ExpectedHash{},
		ExpectedSubtreeHashes: []ExpectedHash{}, ExpectedChildSets: []ExpectedChildSet{},
	}
	request := OrganizeWorkspaceInput{Limits: testLimits(), Preconditions: preconditions, Strategy: "standard_layout"}

	t.Run("nil context", func(t *testing.T) {
		planner := NewSourcePlanner(&scriptedCompiler{})
		if _, err := planner.OrganizeWorkspace(nil, base, request); err == nil {
			t.Fatal("nil context was accepted")
		}
	})

	t.Run("base compiler error", func(t *testing.T) {
		want := errors.New("compile failed")
		planner := NewSourcePlanner(&scriptedCompiler{errors: []error{want}})
		if _, err := planner.OrganizeWorkspace(context.Background(), base, request); !errors.Is(err, want) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("base diagnostics", func(t *testing.T) {
		compiler := &scriptedCompiler{results: []CompileResult{{Output: Snapshot{Diagnostics: []CompileDiagnostic{{Code: "LDL1101", MessageKey: "invalid_structure_syntax", Message: "invalid"}}}}}}
		plan, err := NewSourcePlanner(compiler).OrganizeWorkspace(context.Background(), base, request)
		if err != nil || plan.Preview.Status != "invalid" || len(plan.Preview.Diagnostics) != 1 {
			t.Fatalf("preview=%+v err=%v", plan.Preview, err)
		}
	})

	t.Run("unsupported strategy", func(t *testing.T) {
		compiler := &scriptedCompiler{results: []CompileResult{{Output: Snapshot{}}}}
		request := request
		request.Strategy = "custom"
		plan, err := NewSourcePlanner(compiler).OrganizeWorkspace(context.Background(), base, request)
		if err != nil || plan.Preview.Status != "invalid" {
			t.Fatalf("preview=%+v err=%v", plan.Preview, err)
		}
	})

	t.Run("invalid limits", func(t *testing.T) {
		snapshot := Snapshot{
			Mode:               CompileProject,
			NormalizedDocument: &materialize.NormalizedDocument{Project: materialize.Project{Address: "ldl:project:p"}},
		}
		compiler := &scriptedCompiler{results: []CompileResult{{Output: snapshot}, {Output: snapshot}}}
		request := request
		request.Limits = WorkbenchLimits{}
		if _, err := NewSourcePlanner(compiler).OrganizeWorkspace(context.Background(), base, request); err == nil {
			t.Fatal("zero limits were accepted")
		}
	})

	t.Run("candidate compiler error", func(t *testing.T) {
		want := errors.New("candidate compile failed")
		snapshot := Snapshot{Mode: CompileProject, NormalizedDocument: &materialize.NormalizedDocument{Project: materialize.Project{Address: "ldl:project:p"}}}
		compiler := &scriptedCompiler{results: []CompileResult{{Output: snapshot}}, errors: []error{nil, want}}
		if _, err := NewSourcePlanner(compiler).OrganizeWorkspace(context.Background(), base, request); !errors.Is(err, want) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("candidate diagnostics", func(t *testing.T) {
		before := Snapshot{Mode: CompileProject, NormalizedDocument: &materialize.NormalizedDocument{Project: materialize.Project{Address: "ldl:project:p"}}}
		after := before
		after.Diagnostics = []CompileDiagnostic{{Code: "LDL1101", MessageKey: "invalid_structure_syntax", Message: "invalid candidate"}}
		compiler := &scriptedCompiler{results: []CompileResult{{Output: before}, {Output: after}}}
		plan, err := NewSourcePlanner(compiler).OrganizeWorkspace(context.Background(), base, request)
		if err != nil || plan.Preview.Status != "invalid" || len(plan.Preview.Diagnostics) != 1 {
			t.Fatalf("preview=%+v err=%v", plan.Preview, err)
		}
	})
}

func TestResultingHashesIncludePackAndCompleteSemanticSets(t *testing.T) {
	graphHash := repeatedHash("9")
	root := "ldl:pack:vendor:lib"
	snapshot := Snapshot{
		Mode: CompilePack, DefinitionHash: repeatedHash("1"), GraphHash: &graphHash,
		NormalizedPackArtifact: &materialize.NormalizedPackArtifact{Pack: materialize.PackRoot{Address: root}},
		SubjectSemanticHashes:  []materialize.SubjectHash{{Address: root + ":entity-type:t", Kind: "entity_type", Hash: repeatedHash("2")}},
		SubtreeHashes:          []materialize.SubtreeHash{{OwnerAddress: root, Hash: repeatedHash("3")}},
		ChildSetHashes: []materialize.ChildSetHash{{
			OwnerAddress: root, ChildKind: "entity_type", Addresses: []string{root + ":entity-type:t"}, Hash: repeatedHash("4"),
		}},
	}
	result := resultingHashes(snapshot)
	if result.PackAddress == nil || result.ProjectAddress != nil || result.GraphHash == nil {
		t.Fatalf("root hashes=%+v", result)
	}
	if len(result.SubjectHashes) != 1 || len(result.SubtreeHashes) != 1 || len(result.ChildSetHashes) != 1 {
		t.Fatalf("semantic hashes=%+v", result)
	}
}

func TestPostCompileFragmentScopeVerificationRejectsSemanticEscape(t *testing.T) {
	root := "ldl:project:p"
	owner := root
	address := root + ":entity-type:service"
	fragment := FragmentInput{InsertionOwner: StableAddress(owner)}
	allowed := map[SubjectKind]bool{SubjectKindEntityType: true}
	before := Snapshot{
		SubjectSemanticHashes: []materialize.SubjectHash{{Address: address, Kind: "entity_type", Hash: "old"}},
		SourceMap:             index.SourceMapV1{Subjects: []index.SourceSubjectRecord{{Address: address, Kind: "entity_type", OwnerAddress: &owner}}},
	}

	t.Run("no semantic declaration", func(t *testing.T) {
		if diagnostic := verifyFragmentScope(before, before, fragment, allowed); diagnostic == nil {
			t.Fatal("no-op fragment was accepted")
		}
	})

	after := before
	after.SubjectSemanticHashes = []materialize.SubjectHash{{Address: address, Kind: "entity_type", Hash: "new"}}
	t.Run("allowed declaration", func(t *testing.T) {
		if diagnostic := verifyFragmentScope(before, after, fragment, allowed); diagnostic != nil {
			t.Fatalf("diagnostic=%+v", diagnostic)
		}
	})

	t.Run("resolved kind outside allowlist", func(t *testing.T) {
		if diagnostic := verifyFragmentScope(before, after, fragment, map[SubjectKind]bool{}); diagnostic == nil {
			t.Fatal("kind escape was accepted")
		}
	})

	t.Run("resolved owner outside insertion owner", func(t *testing.T) {
		wrongOwner := root + ":layer:other"
		escaped := after
		escaped.SourceMap.Subjects = append([]index.SourceSubjectRecord(nil), after.SourceMap.Subjects...)
		escaped.SourceMap.Subjects[0].OwnerAddress = &wrongOwner
		if diagnostic := verifyFragmentScope(before, escaped, fragment, allowed); diagnostic == nil {
			t.Fatal("owner escape was accepted")
		}
	})
}

func TestCanonicalOrderingIsStableAcrossOriginsKindsAndRanges(t *testing.T) {
	addresses := []string{
		"ldl:pack:vendor:lib:entity-type:a",
		"ldl:project:p:layer:b",
		"ldl:project:p:entity-type:z",
		"ldl:project:p:layer:a:entity:api",
		"ldl:project:p:layer:a",
		"invalid",
	}
	for left := range addresses {
		for right := range addresses {
			if left == right {
				continue
			}
			forward := lessStableAddress(addresses[left], addresses[right])
			reverse := lessStableAddress(addresses[right], addresses[left])
			if forward == reverse {
				t.Fatalf("addresses are not strictly ordered: %q, %q", addresses[left], addresses[right])
			}
		}
	}

	ranges := []SourceRange{
		{ModulePath: "b.ldl", StartByte: 0, EndByte: 0},
		{ModulePath: "a.ldl", StartByte: 2, EndByte: 4},
		{ModulePath: "a.ldl", StartByte: 1, EndByte: 3},
		{ModulePath: "a.ldl", StartByte: 1, EndByte: 2},
	}
	sortRanges(ranges)
	wantRanges := []SourceRange{
		{ModulePath: "a.ldl", StartByte: 1, EndByte: 2},
		{ModulePath: "a.ldl", StartByte: 1, EndByte: 3},
		{ModulePath: "a.ldl", StartByte: 2, EndByte: 4},
		{ModulePath: "b.ldl", StartByte: 0, EndByte: 0},
	}
	if !reflect.DeepEqual(ranges, wantRanges) {
		t.Fatalf("ranges=%+v, want %+v", ranges, wantRanges)
	}

	address := StableAddress("ldl:project:p:entity-type:a")
	entries := []AuthoringImpactEntry{
		{SubjectAddress: &address, Capability: AuthoringCapabilitySchemaWrite, Action: AuthoringActionDelete},
		{SubjectAddress: &address, Capability: AuthoringCapabilityGraphWrite, Action: AuthoringActionUpdate},
		{SubjectAddress: &address, Capability: AuthoringCapabilityGraphWrite, Action: AuthoringActionCreate},
		{OwnerAddress: &address, Capability: AuthoringCapability("unknown"), Action: AuthoringActionUpdate},
	}
	for left := 0; left < len(entries); left++ {
		for right := left + 1; right < len(entries); right++ {
			if lessImpactEntry(entries[right], entries[left]) {
				entries[left], entries[right] = entries[right], entries[left]
			}
		}
	}
	if entries[0].Action != AuthoringActionCreate || entries[1].Capability != AuthoringCapabilityGraphWrite || entries[len(entries)-1].Capability != AuthoringCapability("unknown") {
		t.Fatalf("impact order=%+v", entries)
	}
}

func TestPlannerBoundaryConversionsPreservePackOriginAndOverflowSafety(t *testing.T) {
	origin := mapOrigin(resolve.SourceOrigin{Kind: resolve.OriginPack, PackAddress: "ldl:pack:vendor:lib"})
	if origin.Kind != OriginKindPack || origin.PackAddress == nil || *origin.PackAddress != "ldl:pack:vendor:lib" {
		t.Fatalf("origin=%+v", origin)
	}
	if _, ok := nextGeneration(Generation{Value: ^uint64(0)}); ok {
		t.Fatal("overflowing generation was accepted")
	}
	limit := &SourcePlannerLimitError{Resource: "max_items", Limit: 1, Observed: 2}
	if !strings.Contains(limit.Error(), "exceeds") {
		t.Fatalf("limit error=%q", limit.Error())
	}
}

func TestFragmentInsertionFramesModulesWithoutTrailingNewline(t *testing.T) {
	compiler := newTestCompiler()
	planner := NewSourcePlanner(compiler)
	input := testInput(map[string][]byte{"document.ldl": []byte("project p \"Project\" {}")})
	base, before := testBase(t, compiler, input)
	ref, blobs := testBlob("framed-fragment", []byte("entity_type t \"T\" {\n representation shape rect\n}"))
	plan, err := planner.PreviewFragment(context.Background(), base, PreviewFragmentInput{
		Limits: testLimits(), Preconditions: fullPreconditions(before, base.Generation),
		Fragment: FragmentInput{Intent: "insert", InsertionOwner: "ldl:project:p", AllowedKinds: []SubjectKind{SubjectKindEntityType}, FragmentBlob: ref},
	}, blobs)
	if err != nil || plan.Preview.Status != "valid" {
		t.Fatalf("preview=%+v err=%v", plan.Preview, err)
	}
	source := plan.Candidate.ProjectSourceTree["document.ldl"]
	if !bytes.Contains(source, []byte("{}\nentity_type")) || !bytes.HasSuffix(source, []byte("\n")) {
		t.Fatalf("framed source=%q", source)
	}
}

func TestFinalizeAndOutputLimitsRejectCancellationOverflowAndBytes(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := finalizePlan(cancelled, SourcePlanningBase{}, Snapshot{}, CompileInput{}, Snapshot{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("finalize cancellation=%v", err)
	}

	compiler := newTestCompiler()
	planner := NewSourcePlanner(compiler)
	input := testInput(map[string][]byte{"document.ldl": []byte("project p \"Project\" {}\n")})
	base, before := testBase(t, compiler, input)
	base.Generation.Value = ^uint64(0)
	preconditions := fullPreconditions(before, base.Generation)
	if _, err := planner.OrganizeWorkspace(context.Background(), base, OrganizeWorkspaceInput{Limits: testLimits(), Preconditions: preconditions, Strategy: "standard_layout"}); err == nil {
		t.Fatal("generation overflow was accepted")
	}

	base.Generation.Value = 1
	preconditions = fullPreconditions(before, base.Generation)
	limits := WorkbenchLimits{MaxItems: 1000, MaxOutputBytes: 1}
	if _, err := planner.OrganizeWorkspace(context.Background(), base, OrganizeWorkspaceInput{Limits: limits, Preconditions: preconditions, Strategy: "standard_layout"}); err == nil {
		t.Fatal("output byte limit was accepted")
	}
}

type scriptedCompiler struct {
	results []CompileResult
	errors  []error
	calls   int
}

func (c *scriptedCompiler) Compile(context.Context, CompileInput) (CompileResult, error) {
	index := c.calls
	c.calls++
	if index < len(c.errors) && c.errors[index] != nil {
		return CompileResult{}, c.errors[index]
	}
	if index < len(c.results) {
		return c.results[index], nil
	}
	return CompileResult{Output: Snapshot{}}, nil
}

func clonePreconditions(value EngineEditPreconditions) EngineEditPreconditions {
	result := value
	result.ExpectedSubjectHashes = append([]ExpectedHash(nil), value.ExpectedSubjectHashes...)
	result.ExpectedSubtreeHashes = append([]ExpectedHash(nil), value.ExpectedSubtreeHashes...)
	result.ExpectedChildSets = append([]ExpectedChildSet(nil), value.ExpectedChildSets...)
	if value.ExpectedSourceDigests != nil {
		items := append([]ExpectedSourceDigest(nil), (*value.ExpectedSourceDigests)...)
		result.ExpectedSourceDigests = &items
	}
	return result
}

func changedBlobRef(value BlobRef, change func(*BlobRef)) BlobRef {
	change(&value)
	return value
}

func assertMissingGuard(t *testing.T, plan SourcePlan, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Preview.Status != "invalid" || len(plan.Preview.Conflicts) != 1 || plan.Preview.Conflicts[0].Kind != "stale_revision" {
		t.Fatalf("preview = %+v", plan.Preview)
	}
}

func repeatedHash(character string) string       { return "sha256:" + strings.Repeat(character, 64) }
func repeatedDigest(character string) Digest     { return Digest(repeatedHash(character)) }
func pointerToModule(value ModuleRef) *ModuleRef { return &value }

func syntheticSnapshot(root string, hashes []materialize.SubjectHash, owners map[string]string) Snapshot {
	subjects := make([]index.SourceSubjectRecord, 0, len(hashes))
	canonical := map[string]any{"address": root, "display_name": "Project"}
	for _, item := range hashes {
		owner := owners[item.Address]
		subjects = append(subjects, index.SourceSubjectRecord{Address: item.Address, Kind: item.Kind, OwnerAddress: &owner})
		canonical[item.Address] = map[string]any{"address": item.Address, "display_name": item.Address}
	}
	encoded, _ := json.Marshal(canonical)
	return Snapshot{CanonicalJSON: encoded, SubjectSemanticHashes: hashes, SourceMap: index.SourceMapV1{Subjects: subjects}}
}

func projectRange(path string, start, end int) *resolve.SourceRange {
	return &resolve.SourceRange{Origin: resolve.SourceOrigin{Kind: resolve.OriginProject}, ModulePath: path, StartByte: start, EndByte: end}
}
