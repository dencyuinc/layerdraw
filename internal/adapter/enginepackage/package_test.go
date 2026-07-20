// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package enginepackage

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine"
)

func TestRoundTripAndRedactedExportCannotReuseDerivedArtifacts(t *testing.T) {
	service := New(engine.New(engine.BuildInfo{}))
	input := projectInput()
	preview := []byte("preview")
	archive, err := service.Export(context.Background(), engine.LayerdrawWriteInput{CompileInput: input, Artifacts: map[string][]byte{"previews/overview.txt": preview}})
	if err != nil {
		t.Fatal(err)
	}
	document, err := service.Import(context.Background(), archive, engine.LayerdrawLimits{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(document.Artifacts["previews/overview.txt"], preview) || document.CompileInput.EntryPath != input.EntryPath {
		t.Fatal("container round trip lost source or artifacts")
	}
	tree, err := ExportLDLTree(document.CompileInput)
	if err != nil || !bytes.Equal(tree["document.ldl"], input.ProjectSourceTree["document.ldl"]) {
		t.Fatalf("LDL tree: %v", err)
	}
	tree["document.ldl"][0] = 'X'
	if bytes.Equal(tree["document.ldl"], input.ProjectSourceTree["document.ldl"]) {
		t.Fatal("LDL export aliases Engine input")
	}
	receipt := ProjectionReceipt{PolicyID: "share/public", CommittedRevisionHash: "sha256:revision", AccessDecisionDigest: "sha256:decision"}
	if _, err := service.ExportRedacted(context.Background(), RedactedInput{CompileInput: input, PolicyID: "share/public", Projection: receipt, DerivedArtifacts: map[string][]byte{"exports/old.json": []byte("unredacted")}}); !errors.Is(err, ErrInvalidProjection) {
		t.Fatalf("unredacted artifact reused: %v", err)
	}
	redacted, err := service.ExportRedacted(context.Background(), RedactedInput{CompileInput: input, PolicyID: "share/public", Projection: receipt})
	if err != nil {
		t.Fatal(err)
	}
	manifest := readManifest(t, redacted)
	if manifest.Redaction == nil || manifest.Redaction.PolicyID != "share/public" {
		t.Fatalf("redaction missing: %+v", manifest)
	}
	if _, ok := manifest.Files["previews/overview.txt"]; ok {
		t.Fatal("old derived preview copied into redacted package")
	}
}

func TestInvalidProjectionAndLDLTreeFailClosed(t *testing.T) {
	service := New(engine.New(engine.BuildInfo{}))
	if _, err := service.ExportRedacted(context.Background(), RedactedInput{}); !errors.Is(err, ErrInvalidProjection) {
		t.Fatalf("empty projection=%v", err)
	}
	if _, err := ExportLDLTree(engine.CompileInput{}); !errors.Is(err, ErrInvalidProjection) {
		t.Fatalf("empty LDL tree=%v", err)
	}
}

func projectInput() engine.CompileInput {
	return engine.CompileInput{Mode: engine.CompileProject, EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte("project demo \"Demo\" {}\n")}, ResolvedDependencies: engine.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1}}
}

func readManifest(t *testing.T, archive []byte) engine.LayerdrawManifest {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range reader.File {
		if file.Name != "manifest.json" {
			continue
		}
		stream, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Close()
		var value engine.LayerdrawManifest
		if err := json.NewDecoder(stream).Decode(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	t.Fatal("manifest missing")
	return engine.LayerdrawManifest{}
}
