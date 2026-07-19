// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package local

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

func TestExternalFileStoreProjectPublishesCompleteTreeIdempotentlyAcrossRestart(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	writeExternalFixture(t, project, map[string][]byte{"document.ldl": []byte("old\n"), "removed.ldl": []byte("remove\n")})
	writeExternalFixture(t, project, map[string][]byte{".git/config": []byte("git config\n"), "assets/logo.svg": []byte("<svg/>\n"), "project.json": []byte("{}\n")})
	store, err := NewExternalFileStore(filepath.Join(root, "adapter"), ExternalFileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	scope := testScope()
	if err := store.Bind(ctx, ExternalFileBinding{Scope: scope, Kind: port.ExternalFileKindProject, Locator: project}); err != nil {
		t.Fatal(err)
	}
	head, err := store.GetExternalHead(ctx, port.GetExternalFileHeadInput{Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	materialization := port.ExternalMaterialization{Kind: port.ExternalFileKindProject, ProjectFiles: []port.ExternalProjectFile{{Path: "document.ldl", Contents: []byte("new\n")}, {Path: "created/module.ldl", Contents: []byte("created\n")}}}
	stage, err := store.Prepare(ctx, port.PrepareExternalFileInput{Scope: scope, OperationID: "external_project", IdempotencyKey: "external_project_key", RevisionID: "revision_project", ExpectedProviderVersion: head.ProviderVersion, Materialization: materialization})
	if err != nil {
		t.Fatal(err)
	}
	stagedInspection, err := store.Inspect(ctx, port.InspectExternalFileInput{Scope: scope, OperationID: "external_project", IdempotencyKey: "external_project_key"})
	if err != nil || stagedInspection.Stage == nil || !reflect.DeepEqual(*stagedInspection.Stage, stage) {
		t.Fatalf("staged inspection=%+v err=%v", stagedInspection, err)
	}
	receipt, err := store.Publish(ctx, port.PublishExternalFileInput{Scope: scope, OperationID: "external_project", IdempotencyKey: "external_project_key", StageID: stage.StageID, ExpectedProviderVersion: head.ProviderVersion})
	if err != nil {
		t.Fatal(err)
	}
	stagePath, backupPath := externalSiblingPaths(externalBindingDisk{Kind: port.ExternalFileKindProject, Locator: project}, stage.StageID)
	for _, cleaned := range []string{stagePath, backupPath} {
		if _, statErr := os.Lstat(cleaned); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("owned artifact was not durably cleaned: %s err=%v", cleaned, statErr)
		}
	}
	assertExternalFixture(t, project, map[string][]byte{"document.ldl": []byte("new\n"), "created/module.ldl": []byte("created\n")})
	for path, expected := range map[string]string{".git/config": "git config\n", "assets/logo.svg": "<svg/>\n", "project.json": "{}\n"} {
		contents, err := os.ReadFile(filepath.Join(project, filepath.FromSlash(path)))
		if err != nil || string(contents) != expected {
			t.Fatalf("unrelated %s=%q err=%v", path, contents, err)
		}
	}
	again, err := store.Publish(ctx, port.PublishExternalFileInput{Scope: scope, OperationID: "external_project", IdempotencyKey: "external_project_key", StageID: stage.StageID, ExpectedProviderVersion: head.ProviderVersion})
	if err != nil || !reflect.DeepEqual(again, receipt) {
		t.Fatalf("idempotent receipt=%+v want=%+v err=%v", again, receipt, err)
	}
	restarted, err := NewExternalFileStore(filepath.Join(root, "adapter"), ExternalFileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	inspected, err := restarted.Inspect(ctx, port.InspectExternalFileInput{Scope: scope, OperationID: "external_project", IdempotencyKey: "external_project_key"})
	if err != nil || inspected.Receipt == nil || !reflect.DeepEqual(*inspected.Receipt, receipt) {
		t.Fatalf("inspection=%+v err=%v", inspected, err)
	}
}

func TestExternalFileStoreAbortRejectsReplacedStageDirectory(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	writeExternalFixture(t, project, map[string][]byte{"document.ldl": []byte("base\n")})
	store, err := NewExternalFileStore(filepath.Join(root, "adapter"), ExternalFileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	scope := testScope()
	if err := store.Bind(ctx, ExternalFileBinding{Scope: scope, Kind: port.ExternalFileKindProject, Locator: project}); err != nil {
		t.Fatal(err)
	}
	head, _ := store.GetExternalHead(ctx, port.GetExternalFileHeadInput{Scope: scope})
	stage, err := store.Prepare(ctx, port.PrepareExternalFileInput{Scope: scope, OperationID: "external_abort_swap", IdempotencyKey: "external_abort_swap_key", RevisionID: "revision_abort_swap", ExpectedProviderVersion: head.ProviderVersion, Materialization: port.ExternalMaterialization{Kind: port.ExternalFileKindProject, ProjectFiles: []port.ExternalProjectFile{{Path: "document.ldl", Contents: []byte("next\n")}}}})
	if err != nil {
		t.Fatal(err)
	}
	stagePath, _ := externalSiblingPaths(externalBindingDisk{Kind: port.ExternalFileKindProject, Locator: project}, stage.StageID)
	if err := os.RemoveAll(stagePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(stagePath, 0o700); err != nil {
		t.Fatal(err)
	}
	alien := filepath.Join(stagePath, "alien.txt")
	if err := os.WriteFile(alien, []byte("third party"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Abort(ctx, port.AbortExternalFileInput{Scope: scope, StageID: stage.StageID}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("abort replacement error=%v", err)
	}
	if contents, err := os.ReadFile(alien); err != nil || string(contents) != "third party" {
		t.Fatalf("abort deleted replacement contents=%q err=%v", contents, err)
	}
}

func TestExternalFileStoreDurableReceiptSurvivesUnsafeCleanupReplacement(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	writeExternalFixture(t, project, map[string][]byte{"document.ldl": []byte("base\n")})
	operation, key := runtimeprotocol.OperationID("external_cleanup_swap"), runtimeprotocol.IdempotencyKey("external_cleanup_swap_key")
	stageID := externalIdentity(operation, key)
	_, backupPath := externalSiblingPaths(externalBindingDisk{Kind: port.ExternalFileKindProject, Locator: project}, stageID)
	swapped := false
	store, err := NewExternalFileStore(filepath.Join(root, "adapter"), ExternalFileOptions{Fault: func(point string) error {
		if point != "before_external_receipt" || swapped {
			return nil
		}
		swapped = true
		if err := os.RemoveAll(backupPath); err != nil {
			return err
		}
		if err := os.Mkdir(backupPath, 0o700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(backupPath, "alien.txt"), []byte("third party"), 0o600)
	}})
	if err != nil {
		t.Fatal(err)
	}
	scope := testScope()
	if err := store.Bind(ctx, ExternalFileBinding{Scope: scope, Kind: port.ExternalFileKindProject, Locator: project}); err != nil {
		t.Fatal(err)
	}
	head, _ := store.GetExternalHead(ctx, port.GetExternalFileHeadInput{Scope: scope})
	stage, err := store.Prepare(ctx, port.PrepareExternalFileInput{Scope: scope, OperationID: operation, IdempotencyKey: key, RevisionID: "revision_cleanup_swap", ExpectedProviderVersion: head.ProviderVersion, Materialization: port.ExternalMaterialization{Kind: port.ExternalFileKindProject, ProjectFiles: []port.ExternalProjectFile{{Path: "document.ldl", Contents: []byte("next\n")}}}})
	if err != nil {
		t.Fatal(err)
	}
	publish := port.PublishExternalFileInput{Scope: scope, OperationID: operation, IdempotencyKey: key, StageID: stage.StageID, ExpectedProviderVersion: head.ProviderVersion}
	receipt, err := store.Publish(ctx, publish)
	if err != nil || receipt.ReceiptDigest == "" {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	if contents, err := os.ReadFile(filepath.Join(backupPath, "alien.txt")); err != nil || string(contents) != "third party" {
		t.Fatalf("cleanup deleted replacement contents=%q err=%v", contents, err)
	}
	again, err := store.Publish(ctx, publish)
	if err != nil || !reflect.DeepEqual(again, receipt) {
		t.Fatalf("durable receipt retry=%+v want=%+v err=%v", again, receipt, err)
	}
	if _, err := os.Stat(filepath.Join(backupPath, "alien.txt")); err != nil {
		t.Fatalf("retry deleted unsafe replacement: %v", err)
	}
}

func TestExternalFileStorePrepareRejectsUnownedSiblingCollision(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	writeExternalFixture(t, project, map[string][]byte{"document.ldl": []byte("base\n")})
	store, err := NewExternalFileStore(filepath.Join(root, "adapter"), ExternalFileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	scope := testScope()
	if err := store.Bind(ctx, ExternalFileBinding{Scope: scope, Kind: port.ExternalFileKindProject, Locator: project}); err != nil {
		t.Fatal(err)
	}
	head, _ := store.GetExternalHead(ctx, port.GetExternalFileHeadInput{Scope: scope})
	operation, key := runtimeprotocol.OperationID("external_collision"), runtimeprotocol.IdempotencyKey("external_collision_key")
	stagePath := filepath.Join(root, ".layerdraw-external-"+externalIdentity(operation, key)+".stage")
	if err := os.Mkdir(stagePath, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(stagePath, "do-not-delete")
	if err := os.WriteFile(sentinel, []byte("owned elsewhere"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = store.Prepare(ctx, port.PrepareExternalFileInput{Scope: scope, OperationID: operation, IdempotencyKey: key, RevisionID: "revision_collision", ExpectedProviderVersion: head.ProviderVersion, Materialization: port.ExternalMaterialization{Kind: port.ExternalFileKindProject, ProjectFiles: []port.ExternalProjectFile{{Path: "document.ldl", Contents: []byte("candidate\n")}}}})
	if !errors.Is(err, port.ErrConflict) {
		t.Fatalf("collision error=%v", err)
	}
	if contents, readErr := os.ReadFile(sentinel); readErr != nil || string(contents) != "owned elsewhere" {
		t.Fatalf("collision sentinel=%q err=%v", contents, readErr)
	}
}

func TestExternalFileStorePublishRejectsManagedParentSymlinkSwap(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	writeExternalFixture(t, project, map[string][]byte{"document.ldl": []byte("base\n")})
	store, err := NewExternalFileStore(filepath.Join(root, "adapter"), ExternalFileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	scope := testScope()
	if err := store.Bind(ctx, ExternalFileBinding{Scope: scope, Kind: port.ExternalFileKindProject, Locator: project}); err != nil {
		t.Fatal(err)
	}
	head, _ := store.GetExternalHead(ctx, port.GetExternalFileHeadInput{Scope: scope})
	stage, err := store.Prepare(ctx, port.PrepareExternalFileInput{Scope: scope, OperationID: "external_symlink", IdempotencyKey: "external_symlink_key", RevisionID: "revision_symlink", ExpectedProviderVersion: head.ProviderVersion, Materialization: port.ExternalMaterialization{Kind: port.ExternalFileKindProject, ProjectFiles: []port.ExternalProjectFile{{Path: "document.ldl", Contents: []byte("next\n")}, {Path: "created/module.ldl", Contents: []byte("escaped\n")}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(project, "created")); err != nil {
		t.Fatal(err)
	}
	_, err = store.Publish(ctx, port.PublishExternalFileInput{Scope: scope, OperationID: "external_symlink", IdempotencyKey: "external_symlink_key", StageID: stage.StageID, ExpectedProviderVersion: head.ProviderVersion})
	if !errors.Is(err, port.ErrConflict) {
		t.Fatalf("symlink publication error=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(outside, "module.ldl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("publication escaped root: %v", err)
	}
}

func TestExternalFileStoreRecoversProjectSwapAndRejectsExternalConflict(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	writeExternalFixture(t, project, map[string][]byte{"document.ldl": []byte("base\n")})
	fault := errors.New("injected crash")
	store, err := NewExternalFileStore(filepath.Join(root, "adapter"), ExternalFileOptions{Fault: func(point string) error {
		if point == "project_after_backup" {
			return fault
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	scope := testScope()
	if err := store.Bind(ctx, ExternalFileBinding{Scope: scope, Kind: port.ExternalFileKindProject, Locator: project}); err != nil {
		t.Fatal(err)
	}
	head, _ := store.GetExternalHead(ctx, port.GetExternalFileHeadInput{Scope: scope})
	stage, err := store.Prepare(ctx, port.PrepareExternalFileInput{Scope: scope, OperationID: "external_crash", IdempotencyKey: "external_crash_key", RevisionID: "revision_crash", ExpectedProviderVersion: head.ProviderVersion, Materialization: port.ExternalMaterialization{Kind: port.ExternalFileKindProject, ProjectFiles: []port.ExternalProjectFile{{Path: "document.ldl", Contents: []byte("recovered\n")}}}})
	if err != nil {
		t.Fatal(err)
	}
	input := port.PublishExternalFileInput{Scope: scope, OperationID: "external_crash", IdempotencyKey: "external_crash_key", StageID: stage.StageID, ExpectedProviderVersion: head.ProviderVersion}
	if _, err := store.Publish(ctx, input); !errors.Is(err, fault) {
		t.Fatalf("publish error=%v", err)
	}
	restarted, err := NewExternalFileStore(filepath.Join(root, "adapter"), ExternalFileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Publish(ctx, input); err != nil {
		t.Fatal(err)
	}
	assertExternalFixture(t, project, map[string][]byte{"document.ldl": []byte("recovered\n")})

	nextHead, _ := restarted.GetExternalHead(ctx, port.GetExternalFileHeadInput{Scope: scope})
	next, err := restarted.Prepare(ctx, port.PrepareExternalFileInput{Scope: scope, OperationID: "external_conflict", IdempotencyKey: "external_conflict_key", RevisionID: "revision_conflict", ExpectedProviderVersion: nextHead.ProviderVersion, Materialization: port.ExternalMaterialization{Kind: port.ExternalFileKindProject, ProjectFiles: []port.ExternalProjectFile{{Path: "document.ldl", Contents: []byte("candidate\n")}}}})
	if err != nil {
		t.Fatal(err)
	}
	writeExternalFixture(t, project, map[string][]byte{"document.ldl": []byte("external\n")})
	if _, err := restarted.Publish(ctx, port.PublishExternalFileInput{Scope: scope, OperationID: "external_conflict", IdempotencyKey: "external_conflict_key", StageID: next.StageID, ExpectedProviderVersion: nextHead.ProviderVersion}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("conflict error=%v", err)
	}
}

func TestExternalFileStoreContainerPublicationIsConditionalAndRestartSafe(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	container := filepath.Join(root, "document.layerdraw")
	if err := os.WriteFile(container, []byte("old container"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewExternalFileStore(filepath.Join(root, "adapter"), ExternalFileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	scope := testScope()
	if err := store.Bind(ctx, ExternalFileBinding{Scope: scope, Kind: port.ExternalFileKindContainer, Locator: container}); err != nil {
		t.Fatal(err)
	}
	head, _ := store.GetExternalHead(ctx, port.GetExternalFileHeadInput{Scope: scope})
	stage, err := store.Prepare(ctx, port.PrepareExternalFileInput{Scope: scope, OperationID: "external_container", IdempotencyKey: "external_container_key", RevisionID: runtimeprotocol.RevisionID("revision_container"), ExpectedProviderVersion: head.ProviderVersion, Materialization: port.ExternalMaterialization{Kind: port.ExternalFileKindContainer, Container: []byte("new container")}})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := store.Publish(ctx, port.PublishExternalFileInput{Scope: scope, OperationID: "external_container", IdempotencyKey: "external_container_key", StageID: stage.StageID, ExpectedProviderVersion: head.ProviderVersion})
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(container)
	if err != nil || string(bytes) != "new container" || receipt.ProviderVersion == head.ProviderVersion {
		t.Fatalf("bytes=%q receipt=%+v err=%v", bytes, receipt, err)
	}
}

func TestExternalFileStoreValidationCancellationAndAbortBranches(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	other := filepath.Join(root, "other")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}
	writeExternalFixture(t, project, map[string][]byte{"document.ldl": []byte("base\n")})
	writeExternalFixture(t, other, map[string][]byte{"document.ldl": []byte("other\n")})
	store, err := NewExternalFileStore(filepath.Join(root, "adapter"), ExternalFileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	scope := testScope()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := store.Bind(cancelled, ExternalFileBinding{Scope: scope, Kind: port.ExternalFileKindProject, Locator: project}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled bind=%v", err)
	}
	if err := store.Bind(ctx, ExternalFileBinding{Scope: scope, Kind: port.ExternalFileKindProject, Locator: project}); err != nil {
		t.Fatal(err)
	}
	if err := store.Bind(ctx, ExternalFileBinding{Scope: scope, Kind: port.ExternalFileKindProject, Locator: project}); err != nil {
		t.Fatalf("idempotent bind=%v", err)
	}
	if err := store.Bind(ctx, ExternalFileBinding{Scope: scope, Kind: port.ExternalFileKindProject, Locator: other}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("binding replacement=%v", err)
	}
	if err := store.Bind(ctx, ExternalFileBinding{Scope: scope, Kind: port.ExternalFileKind("unknown"), Locator: project}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("unknown binding kind=%v", err)
	}
	if err := store.Bind(ctx, ExternalFileBinding{Scope: scope, Kind: port.ExternalFileKindContainer, Locator: project}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("directory container binding=%v", err)
	}
	if _, err := store.GetExternalHead(cancelled, port.GetExternalFileHeadInput{Scope: scope}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled head=%v", err)
	}
	missingScope := scope
	missingScope.DocumentID = "doc_external_missing"
	if _, err := store.GetExternalHead(ctx, port.GetExternalFileHeadInput{Scope: missingScope}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("unbound head=%v", err)
	}
	head, err := store.GetExternalHead(ctx, port.GetExternalFileHeadInput{Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	base := port.PrepareExternalFileInput{Scope: scope, OperationID: "validation_operation", IdempotencyKey: "validation_idempotency", RevisionID: "revision_validation", ExpectedProviderVersion: head.ProviderVersion, Materialization: port.ExternalMaterialization{Kind: port.ExternalFileKindProject, ProjectFiles: []port.ExternalProjectFile{{Path: "document.ldl", Contents: []byte("next\n")}}}}
	if _, err := store.Prepare(cancelled, base); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled prepare=%v", err)
	}
	wrongKind := base
	wrongKind.Materialization.Kind = port.ExternalFileKindContainer
	wrongKind.Materialization.ProjectFiles = nil
	wrongKind.Materialization.Container = []byte("container")
	if _, err := store.Prepare(ctx, wrongKind); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("wrong materialization kind=%v", err)
	}
	stale := base
	stale.ExpectedProviderVersion = "stale"
	if _, err := store.Prepare(ctx, stale); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("stale prepare=%v", err)
	}
	for name, materialization := range map[string]port.ExternalMaterialization{
		"empty project":   {Kind: port.ExternalFileKindProject},
		"traversal":       {Kind: port.ExternalFileKindProject, ProjectFiles: []port.ExternalProjectFile{{Path: "../escape.ldl", Contents: []byte("x")}}},
		"non ldl":         {Kind: port.ExternalFileKindProject, ProjectFiles: []port.ExternalProjectFile{{Path: "note.txt", Contents: []byte("x")}}},
		"duplicate":       {Kind: port.ExternalFileKindProject, ProjectFiles: []port.ExternalProjectFile{{Path: "a.ldl", Contents: []byte("a")}, {Path: "a.ldl", Contents: []byte("b")}}},
		"empty container": {Kind: port.ExternalFileKindContainer},
		"mixed container": {Kind: port.ExternalFileKindContainer, Container: []byte("x"), ProjectFiles: []port.ExternalProjectFile{{Path: "a.ldl", Contents: []byte("a")}}},
	} {
		t.Run(name, func(t *testing.T) {
			input := base
			input.OperationID = runtimeprotocol.OperationID("invalid_" + strings.ReplaceAll(name, " ", "_"))
			input.IdempotencyKey = runtimeprotocol.IdempotencyKey("invalid_key_" + strings.ReplaceAll(name, " ", "_"))
			input.Materialization = materialization
			if _, err := store.Prepare(ctx, input); !errors.Is(err, port.ErrConflict) {
				t.Fatalf("invalid materialization=%v", err)
			}
		})
	}
	stage, err := store.Prepare(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if repeated, err := store.Prepare(ctx, base); err != nil || !reflect.DeepEqual(repeated, stage) {
		t.Fatalf("idempotent prepare=%+v err=%v", repeated, err)
	}
	changed := base
	changed.RevisionID = "revision_changed"
	if _, err := store.Prepare(ctx, changed); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("changed prepare identity=%v", err)
	}
	if _, err := store.Inspect(cancelled, port.InspectExternalFileInput{Scope: scope, OperationID: base.OperationID, IdempotencyKey: base.IdempotencyKey}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled inspect=%v", err)
	}
	if _, err := store.Publish(cancelled, port.PublishExternalFileInput{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled publish=%v", err)
	}
	if err := store.Abort(cancelled, port.AbortExternalFileInput{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled abort=%v", err)
	}
	if err := store.Abort(ctx, port.AbortExternalFileInput{Scope: scope, StageID: "missing-stage"}); err != nil {
		t.Fatalf("missing abort=%v", err)
	}
	if err := store.Abort(ctx, port.AbortExternalFileInput{Scope: scope, StageID: stage.StageID}); err != nil {
		t.Fatalf("abort=%v", err)
	}
	if err := store.Abort(ctx, port.AbortExternalFileInput{Scope: scope, StageID: stage.StageID}); err != nil {
		t.Fatalf("idempotent abort=%v", err)
	}
	bounded, err := NewExternalFileStore(filepath.Join(root, "bounded-adapter"), ExternalFileOptions{MaxFiles: 1, MaxBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	if err := bounded.Bind(ctx, ExternalFileBinding{Scope: scope, Kind: port.ExternalFileKindProject, Locator: project}); err != nil {
		t.Fatal(err)
	}
	boundedHead, _ := bounded.GetExternalHead(ctx, port.GetExternalFileHeadInput{Scope: scope})
	tooLarge := base
	tooLarge.ExpectedProviderVersion = boundedHead.ProviderVersion
	if _, err := bounded.Prepare(ctx, tooLarge); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("byte bound=%v", err)
	}
	tooMany := tooLarge
	tooMany.Materialization.ProjectFiles = []port.ExternalProjectFile{{Path: "a.ldl", Contents: []byte("a")}, {Path: "b.ldl", Contents: []byte("b")}}
	if _, err := bounded.Prepare(ctx, tooMany); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("file bound=%v", err)
	}
}

func TestExternalFileStoreContainerCrashResumesAndAbortRejectsPublishedBackup(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	container := filepath.Join(root, "document.layerdraw")
	if err := os.WriteFile(container, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	fault := errors.New("container crash")
	store, err := NewExternalFileStore(filepath.Join(root, "adapter"), ExternalFileOptions{Fault: func(point string) error {
		if point == "container_after_backup" {
			return fault
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	scope := testScope()
	if err := store.Bind(ctx, ExternalFileBinding{Scope: scope, Kind: port.ExternalFileKindContainer, Locator: container}); err != nil {
		t.Fatal(err)
	}
	head, _ := store.GetExternalHead(ctx, port.GetExternalFileHeadInput{Scope: scope})
	stage, err := store.Prepare(ctx, port.PrepareExternalFileInput{Scope: scope, OperationID: "container_crash", IdempotencyKey: "container_crash_key", RevisionID: "revision_container_crash", ExpectedProviderVersion: head.ProviderVersion, Materialization: port.ExternalMaterialization{Kind: port.ExternalFileKindContainer, Container: []byte("new")}})
	if err != nil {
		t.Fatal(err)
	}
	publish := port.PublishExternalFileInput{Scope: scope, OperationID: "container_crash", IdempotencyKey: "container_crash_key", StageID: stage.StageID, ExpectedProviderVersion: head.ProviderVersion}
	if _, err := store.Publish(ctx, publish); !errors.Is(err, fault) {
		t.Fatalf("container crash=%v", err)
	}
	if err := store.Abort(ctx, port.AbortExternalFileInput{Scope: scope, StageID: stage.StageID}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("abort after backup=%v", err)
	}
	restarted, err := NewExternalFileStore(filepath.Join(root, "adapter"), ExternalFileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Publish(ctx, publish); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(container)
	if err != nil || string(contents) != "new" {
		t.Fatalf("resumed container=%q err=%v", contents, err)
	}
}

func TestExternalPathSafetyHelpersCoverClosedFilesystemShapes(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(root, "project")
	container := filepath.Join(root, "document.layerdraw")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(container, []byte("container"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := canonicalExternalLocator(port.ExternalFileKindProject, project); err != nil || got == "" {
		t.Fatalf("project locator=%q err=%v", got, err)
	}
	if got, err := canonicalExternalLocator(port.ExternalFileKindContainer, container); err != nil || got == "" {
		t.Fatalf("container locator=%q err=%v", got, err)
	}
	for _, test := range []struct {
		name string
		kind port.ExternalFileKind
		path string
	}{{"missing", port.ExternalFileKindProject, filepath.Join(root, "missing")}, {"project file", port.ExternalFileKindProject, container}, {"container directory", port.ExternalFileKindContainer, project}, {"unknown kind", port.ExternalFileKind("unknown"), project}} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := canonicalExternalLocator(test.kind, test.path); err == nil {
				t.Fatal("unsafe locator was accepted")
			}
		})
	}
	if clean, err := cleanManagedPath("nested/module.ldl"); err != nil || clean != "nested/module.ldl" {
		t.Fatalf("clean path=%q err=%v", clean, err)
	}
	for _, path := range []string{"note.txt", "../escape.ldl", "/absolute.ldl", "."} {
		if _, err := cleanManagedPath(path); !errors.Is(err, port.ErrConflict) {
			t.Fatalf("unsafe managed path %q=%v", path, err)
		}
	}
	if err := requireExternalAbsent(container); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("existing path absent check=%v", err)
	}
	if err := requireExternalAbsent(filepath.Join(root, "absent")); err != nil {
		t.Fatal(err)
	}
	if err := ensureExternalRoot(project, true); err != nil {
		t.Fatal(err)
	}
	if err := ensureExternalRoot(container, false); err != nil {
		t.Fatal(err)
	}
	if err := ensureExternalRoot(project, false); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("directory accepted as file=%v", err)
	}
	if err := ensureExternalRoot(container, true); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("file accepted as directory=%v", err)
	}
	target := filepath.Join(project, "nested", "module.ldl")
	if err := ensureExternalManagedParent(project, target, true); err != nil {
		t.Fatal(err)
	}
	if err := ensureExternalManagedParent(project, target, false); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("missing managed parent=%v", err)
	}
	if err := mkdirExternalManagedParents(project, target); err != nil {
		t.Fatal(err)
	}
	if err := mkdirExternalManagedParents(project, target); err != nil {
		t.Fatalf("idempotent managed parents=%v", err)
	}
	if err := os.WriteFile(target, []byte("module"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureExternalManagedParent(project, target, false); err != nil {
		t.Fatal(err)
	}
	if err := ensureExternalManagedFile(project, target); err != nil {
		t.Fatal(err)
	}
	if err := ensureExternalManagedParent(project, project, false); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("root accepted as child=%v", err)
	}
	if err := ensureExternalManagedParent(project, filepath.Join(root, "outside.ldl"), true); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("outside parent=%v", err)
	}
	if err := mkdirExternalManagedParents(project, filepath.Join(root, "outside", "file.ldl")); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("outside mkdir=%v", err)
	}
	if err := ensureExternalManagedFile(project, filepath.Join(project, "nested")); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("directory accepted as managed file=%v", err)
	}
	link := filepath.Join(project, "linked")
	if err := os.Symlink(root, link); err == nil {
		if err := ensureExternalManagedParent(project, filepath.Join(link, "escaped.ldl"), false); !errors.Is(err, port.ErrConflict) {
			t.Fatalf("symlink parent=%v", err)
		}
	}
	if err := ensureOwnedExternalPath(project, true); err != nil {
		t.Fatal(err)
	}
	if err := ensureOwnedExternalPath(container, false); err != nil {
		t.Fatal(err)
	}
	if err := ensureOwnedExternalPath(project, false); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("owned directory accepted as file=%v", err)
	}
	if err := ensureOwnedExternalPath(filepath.Join(root, "missing-owned"), true); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("missing owned path=%v", err)
	}
	if err := syncExternalRename(container, filepath.Join(root, "same-parent")); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(root, "other")
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syncExternalRename(container, filepath.Join(other, "cross-parent")); err != nil {
		t.Fatal(err)
	}
	if err := syncExternalDir(filepath.Join(root, "missing-dir")); err == nil {
		t.Fatal("missing directory sync succeeded")
	}
}

func TestExternalOwnerMarkerRejectsMissingDuplicateAndAlteredOwnership(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	owner := externalOwner("stage_owner", externalDigest("owned tree"))
	if err := validateExternalOwnerMarker(root, owner); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("missing marker=%v", err)
	}
	if err := writeExternalOwnerMarker(root, owner); err != nil {
		t.Fatal(err)
	}
	if err := validateExternalOwnerMarker(root, owner); err != nil {
		t.Fatal(err)
	}
	if err := writeExternalOwnerMarker(root, owner); err == nil {
		t.Fatal("duplicate owner marker was overwritten")
	}
	if err := validateExternalOwnerMarker(root, externalOwner("other_stage", externalDigest("owned tree"))); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("altered owner=%v", err)
	}
	if err := os.Remove(filepath.Join(root, externalOwnerMarker)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, externalOwnerMarker), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := validateExternalOwnerMarker(root, owner); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("directory marker=%v", err)
	}
}

func TestExternalFileStoreAbortRejectsAlteredStagedProject(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	writeExternalFixture(t, project, map[string][]byte{"document.ldl": []byte("base\n")})
	store, err := NewExternalFileStore(filepath.Join(root, "adapter"), ExternalFileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	scope := testScope()
	if err := store.Bind(ctx, ExternalFileBinding{Scope: scope, Kind: port.ExternalFileKindProject, Locator: project}); err != nil {
		t.Fatal(err)
	}
	head, err := store.GetExternalHead(ctx, port.GetExternalFileHeadInput{Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	stage, err := store.Prepare(ctx, port.PrepareExternalFileInput{Scope: scope, OperationID: "external_abort_altered", IdempotencyKey: "external_abort_altered_key", RevisionID: "revision_abort_altered", ExpectedProviderVersion: head.ProviderVersion, Materialization: port.ExternalMaterialization{Kind: port.ExternalFileKindProject, ProjectFiles: []port.ExternalProjectFile{{Path: "document.ldl", Contents: []byte("next\n")}}}})
	if err != nil {
		t.Fatal(err)
	}
	stagePath, _ := externalSiblingPaths(externalBindingDisk{Kind: port.ExternalFileKindProject, Locator: project}, stage.StageID)
	if err := os.WriteFile(filepath.Join(stagePath, "document.ldl"), []byte("altered by another process\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Abort(ctx, port.AbortExternalFileInput{Scope: scope, StageID: stage.StageID}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("altered stage abort=%v", err)
	}
	if contents, err := os.ReadFile(filepath.Join(stagePath, "document.ldl")); err != nil || string(contents) != "altered by another process\n" {
		t.Fatalf("altered stage was removed: %q err=%v", contents, err)
	}
}

func TestExternalFileStoreRejectsMismatchedBindingAndStageIdentity(t *testing.T) {
	ctx := context.Background()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(root, "project")
	other := filepath.Join(root, "other")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}
	writeExternalFixture(t, project, map[string][]byte{"document.ldl": []byte("base\n")})
	store, err := NewExternalFileStore(filepath.Join(root, "adapter"), ExternalFileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	scope := testScope()
	base := port.PrepareExternalFileInput{Scope: scope, OperationID: "external_identity", IdempotencyKey: "external_identity_key", RevisionID: "revision_identity", Materialization: port.ExternalMaterialization{Kind: port.ExternalFileKindProject, ProjectFiles: []port.ExternalProjectFile{{Path: "document.ldl", Contents: []byte("next\n")}}}}
	if _, err := store.Prepare(ctx, base); err == nil {
		t.Fatal("prepare without a binding succeeded")
	}
	if err := store.Bind(ctx, ExternalFileBinding{Scope: scope, Kind: port.ExternalFileKindProject, Locator: project}); err != nil {
		t.Fatal(err)
	}
	if err := store.Bind(ctx, ExternalFileBinding{Scope: scope, Kind: port.ExternalFileKindProject, Locator: project}); err != nil {
		t.Fatalf("idempotent binding=%v", err)
	}
	if err := store.Bind(ctx, ExternalFileBinding{Scope: scope, Kind: port.ExternalFileKindProject, Locator: other}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("replacement binding=%v", err)
	}
	head, err := store.GetExternalHead(ctx, port.GetExternalFileHeadInput{Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	base.ExpectedProviderVersion = head.ProviderVersion
	wrongVersion := base
	wrongVersion.ExpectedProviderVersion = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := store.Prepare(ctx, wrongVersion); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("wrong provider version=%v", err)
	}
	mismatched := base
	mismatched.Materialization.Kind = port.ExternalFileKindContainer
	if _, err := store.Prepare(ctx, mismatched); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("mismatched materialization=%v", err)
	}
	stage, err := store.Prepare(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	changed := base
	changed.RevisionID = "revision_identity_changed"
	if _, err := store.Prepare(ctx, changed); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("changed idempotency input=%v", err)
	}
	if _, err := store.Publish(ctx, port.PublishExternalFileInput{Scope: scope, OperationID: "wrong_operation", IdempotencyKey: base.IdempotencyKey, StageID: stage.StageID, ExpectedProviderVersion: head.ProviderVersion}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("mismatched stage identity=%v", err)
	}
	if _, err := store.Publish(ctx, port.PublishExternalFileInput{Scope: scope, OperationID: base.OperationID, IdempotencyKey: base.IdempotencyKey, StageID: stage.StageID, ExpectedProviderVersion: wrongVersion.ExpectedProviderVersion}); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("mismatched expected version=%v", err)
	}
	if _, err := store.Publish(ctx, port.PublishExternalFileInput{Scope: scope, OperationID: "missing_operation", IdempotencyKey: "missing_key", StageID: "missing_stage", ExpectedProviderVersion: head.ProviderVersion}); err == nil {
		t.Fatal("missing publication stage succeeded")
	}
	if _, err := store.Inspect(ctx, port.InspectExternalFileInput{Scope: scope, OperationID: "missing_operation", IdempotencyKey: "missing_key"}); err == nil {
		t.Fatal("missing inspection succeeded")
	}
	if err := store.Abort(ctx, port.AbortExternalFileInput{Scope: scope, StageID: "missing_stage"}); err != nil {
		t.Fatalf("missing stage abort=%v", err)
	}
	dir, err := store.scopeDir(scope)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.stageMetadataPath(dir, stage.StageID), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Prepare(ctx, base); err == nil {
		t.Fatal("corrupt stage metadata was accepted by prepare")
	}
	if err := store.Abort(ctx, port.AbortExternalFileInput{Scope: scope, StageID: stage.StageID}); err == nil {
		t.Fatal("corrupt stage metadata was accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "external", "binding.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Bind(ctx, ExternalFileBinding{Scope: scope, Kind: port.ExternalFileKindProject, Locator: project}); err == nil {
		t.Fatal("corrupt binding metadata was accepted")
	}
}

func TestNewExternalFileStoreRejectsFileAsRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(root, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewExternalFileStore(root, ExternalFileOptions{}); err == nil {
		t.Fatal("file root was accepted")
	}
}

func writeExternalFixture(t *testing.T, root string, files map[string][]byte) {
	t.Helper()
	for path, contents := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func assertExternalFixture(t *testing.T, root string, expected map[string][]byte) {
	t.Helper()
	actual := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if filepath.Ext(relative) != ".ldl" {
			return nil
		}
		actual[relative], err = os.ReadFile(path)
		return err
	})
	if err != nil || !reflect.DeepEqual(actual, expected) {
		t.Fatalf("tree=%q want=%q err=%v", actual, expected, err)
	}
}
