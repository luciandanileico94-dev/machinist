package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/owainlewis/machinist/internal/factory"
	"github.com/owainlewis/machinist/internal/fakegh"
)

func TestInitWritesFactoryManifestAndFactoryIsCurrent(t *testing.T) {
	directory := inTempDirectory(t)
	var stdout, stderr bytes.Buffer
	if exitCode := Execute(t.Context(), []string{"init"}, strings.NewReader(""), &stdout, &stderr, "test"); exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	factoryDir := filepath.Join(directory, "factory")
	info, err := os.Stat(filepath.Join(factoryDir, factory.ManifestName))
	if err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("manifest mode = %o, want 600", info.Mode().Perm())
	}
	updates, err := factory.Check(factoryDir)
	if err != nil || len(updates) != 0 {
		t.Fatalf("updates = %+v, err = %v", updates, err)
	}
}

func TestStartLoadsCustomFactoryWithoutTemplateWarnings(t *testing.T) {
	fakegh.Install(t, "machinist-bot")
	directory := inTempDirectory(t)
	if exitCode := Execute(t.Context(), []string{"init"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, "test"); exitCode != 0 {
		t.Fatalf("init exit code = %d", exitCode)
	}
	writeConfigFile(t, directory, "config.yaml", "server:\n  listen: 127.0.0.1:0\n  database: .machinist/tickets.db\n  factory: factory\n\ngithub:\n  handle: machinist-bot\n")

	// Freshly initialized factory emits no factory warning.
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	freshStderr := &cancelOnListen{cancel: cancel}
	if exitCode := Execute(ctx, []string{"start"}, strings.NewReader(""), &bytes.Buffer{}, freshStderr, "test"); exitCode != 0 {
		t.Fatalf("fresh start exit = %d, stderr = %q", exitCode, freshStderr.body.String())
	}
	if strings.Contains(freshStderr.body.String(), "factory file") {
		t.Fatalf("unexpected factory warning on fresh install: %q", freshStderr.body.String())
	}

	// Simulate an older shipped version: unedited publish.sh, edited publish.md, missing revise.sh.
	factoryDir := filepath.Join(directory, "factory")
	manifest, _, err := factory.LoadManifest(factoryDir)
	if err != nil {
		t.Fatal(err)
	}
	oldScript := []byte("#!/bin/sh\necho old publish\n")
	if err := os.WriteFile(filepath.Join(factoryDir, "scripts", "publish.sh"), oldScript, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest.Files["scripts/publish.sh"] = factory.HashBytes(oldScript)

	if err := os.WriteFile(filepath.Join(factoryDir, "agents", "publish.md"), []byte("---\nharness: claude\n---\nTeam custom publish.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest.Files["agents/publish.md"] = factory.HashBytes([]byte("---\nharness: claude\n---\nOlder shipped publish.\n"))

	if err := os.Remove(filepath.Join(factoryDir, "scripts", "revise.sh")); err != nil {
		t.Fatal(err)
	}
	if err := factory.WriteManifest(factoryDir, manifest); err != nil {
		t.Fatal(err)
	}

	ctx2, cancel2 := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel2()
	staleStderr := &cancelOnListen{cancel: cancel2}
	if exitCode := Execute(ctx2, []string{"start"}, strings.NewReader(""), &bytes.Buffer{}, staleStderr, "test"); exitCode != 0 {
		t.Fatalf("stale start exit = %d, stderr = %q", exitCode, staleStderr.body.String())
	}
	output := staleStderr.body.String()
	if strings.Contains(output, "out of date") || strings.Contains(output, "factory upgrade") {
		t.Fatalf("custom factory produced a template warning: %q", output)
	}
}

func TestCustomFactoryUpgradeRefusesEvenWithForce(t *testing.T) {
	directory := inTempDirectory(t)
	if exitCode := Execute(t.Context(), []string{"init"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, "test"); exitCode != 0 {
		t.Fatalf("init exit = %d", exitCode)
	}
	file := filepath.Join(directory, "factory", "agents", "publish.md")
	custom := []byte("---\nharness: pi\n---\nTeam custom publishing.\n")
	if err := os.WriteFile(file, custom, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"factory", "upgrade"}, {"factory", "upgrade", "--force"}} {
		var stderr bytes.Buffer
		if exitCode := Execute(t.Context(), args, strings.NewReader(""), &bytes.Buffer{}, &stderr, "test"); exitCode == 0 ||
			!strings.Contains(stderr.String(), "своя фабрика — upgrade выключен") {
			t.Fatalf("args = %v, exit = %d, stderr = %q", args, exitCode, stderr.String())
		}
		if actual, err := os.ReadFile(file); err != nil || !bytes.Equal(actual, custom) {
			t.Fatalf("custom agent changed: %q, err = %v", actual, err)
		}
	}
}
