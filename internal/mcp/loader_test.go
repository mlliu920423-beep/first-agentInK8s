package mcp

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/bigmay/first-agentink8s/internal/tools"
)

// fakeDriver is registered once (via sync.Once — RegisterDriver panics on
// duplicate) and its behaviour is reconfigured per test by clearing its
// state field. This mirrors how a real driver is wired: init-time
// registration, per-invocation Start.
type fakeDriver struct {
	transport string

	mu       sync.Mutex
	started  []Config
	failWith error
	closers  []*fakeCloser
}

func (f *fakeDriver) Name() string { return f.transport }

func (f *fakeDriver) Start(_ context.Context, cfg Config, _ *tools.Registry) (io.Closer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, cfg)
	if f.failWith != nil {
		return nil, f.failWith
	}
	c := &fakeCloser{}
	f.closers = append(f.closers, c)
	return c, nil
}

func (f *fakeDriver) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = nil
	f.failWith = nil
	f.closers = nil
}

type fakeCloser struct {
	closed int
}

func (f *fakeCloser) Close() error {
	f.closed++
	return nil
}

// The transport name has to be one that Config.validate() accepts
// ("inproc" or "stdio"). Config.validate is the outer schema check; loader
// dispatches by whichever driver is registered under that transport name.
// Since this test package doesn't import internal/mcp/inproc, no real
// driver claims the "inproc" slot and we can register a fake there.
var (
	fake     = &fakeDriver{transport: "inproc"}
	fakeOnce sync.Once
)

func registerFake(t *testing.T) *fakeDriver {
	t.Helper()
	fakeOnce.Do(func() {
		RegisterDriver(fake)
	})
	fake.reset()
	return fake
}

func writeYAML(t *testing.T, dir, name, body string) {
	t.Helper()
	// 0o600 satisfies gosec G306; these are temp-dir test fixtures where
	// permissions don't matter functionally.
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAll_Empty(t *testing.T) {
	registerFake(t)
	dir := t.TempDir()
	reg := tools.NewRegistry()
	closers, err := LoadAll(context.Background(), dir, reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(closers) != 0 {
		t.Fatalf("want 0 closers, got %d", len(closers))
	}
}

func TestLoadAll_EnabledSkip(t *testing.T) {
	f := registerFake(t)
	dir := t.TempDir()
	writeYAML(t, dir, "a.yaml", "name: a\ntransport: inproc\nprovider: p\nenabled_if: env:NEVER_SET_XYZ=1\n")
	closers, err := LoadAll(context.Background(), dir, tools.NewRegistry())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(closers) != 0 {
		t.Fatalf("want 0 closers, got %d", len(closers))
	}
	if len(f.started) != 0 {
		t.Fatalf("driver should not have been called, got %d starts", len(f.started))
	}
}

func TestLoadAll_EnabledStart(t *testing.T) {
	f := registerFake(t)
	dir := t.TempDir()
	writeYAML(t, dir, "a.yaml", "name: a\ntransport: inproc\nprovider: p\nenabled_if: always\n")
	closers, err := LoadAll(context.Background(), dir, tools.NewRegistry())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(closers) != 1 {
		t.Fatalf("want 1 closer, got %d", len(closers))
	}
	if len(f.started) != 1 {
		t.Fatalf("want 1 start, got %d", len(f.started))
	}
	if f.started[0].Name != "a" {
		t.Fatalf("wrong cfg passed: %+v", f.started[0])
	}
}

func TestLoadAll_UnknownTransport(t *testing.T) {
	registerFake(t)
	dir := t.TempDir()
	writeYAML(t, dir, "a.yaml", "name: a\ntransport: nonexistent-xyz\n")
	closers, err := LoadAll(context.Background(), dir, tools.NewRegistry())
	if err == nil {
		t.Fatal("want error, got nil")
	}
	// The unknown-transport error is raised inside validate() (config.go)
	// because Config.Transport is constrained there. Either that or the
	// loader's "no driver registered" branch is acceptable — both refuse
	// to boot the pod, which is the load-bearing property.
	if closers != nil {
		t.Fatalf("want nil closers on failure, got %d", len(closers))
	}
}

func TestLoadAll_TypoInEnabledIf(t *testing.T) {
	registerFake(t)
	dir := t.TempDir()
	writeYAML(t, dir, "a.yaml", "name: a\ntransport: inproc\nprovider: p\nenabled_if: enev:X\n")
	closers, err := LoadAll(context.Background(), dir, tools.NewRegistry())
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if closers != nil {
		t.Fatalf("want nil closers, got %d", len(closers))
	}
	if !strings.Contains(err.Error(), "enabled_if") && !strings.Contains(err.Error(), "unknown condition") {
		t.Fatalf("error should mention enabled_if: %v", err)
	}
}

func TestLoadAll_DuplicateName(t *testing.T) {
	registerFake(t)
	dir := t.TempDir()
	writeYAML(t, dir, "a.yaml", "name: dup\ntransport: inproc\nprovider: p\n")
	writeYAML(t, dir, "b.yaml", "name: dup\ntransport: inproc\nprovider: p\n")
	_, err := LoadAll(context.Background(), dir, tools.NewRegistry())
	if err == nil {
		t.Fatal("want duplicate error")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("want duplicate mention: %v", err)
	}
}

func TestLoadAll_DriverStartFails(t *testing.T) {
	f := registerFake(t)
	f.failWith = &startError{msg: "boom"}
	dir := t.TempDir()
	writeYAML(t, dir, "a.yaml", "name: a\ntransport: inproc\nprovider: p\n")
	closers, err := LoadAll(context.Background(), dir, tools.NewRegistry())
	if err == nil {
		t.Fatal("want driver error")
	}
	if closers != nil {
		t.Fatalf("want nil closers on failure, got %d", len(closers))
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("driver error should bubble up: %v", err)
	}
}

func TestLoadAll_CleanupOnFailure(t *testing.T) {
	f := registerFake(t)
	dir := t.TempDir()
	// a.yaml starts fine, b.yaml triggers a hard error (unknown transport
	// at validate() time). Expectation: a's closer got Close()'d before
	// the error returned.
	writeYAML(t, dir, "a.yaml", "name: a\ntransport: inproc\nprovider: p\n")
	writeYAML(t, dir, "b.yaml", "name: b\ntransport: nonexistent-xyz\n")

	_, err := LoadAll(context.Background(), dir, tools.NewRegistry())
	if err == nil {
		t.Fatal("want error")
	}
	if len(f.closers) != 1 {
		t.Fatalf("want 1 closer created by fake driver, got %d", len(f.closers))
	}
	if f.closers[0].closed != 1 {
		t.Fatalf("started closer must have been closed once on rollback, got %d", f.closers[0].closed)
	}
}

// startError is a trivial error type so we don't pull in fmt.Errorf for a
// single sentinel.
type startError struct{ msg string }

func (e *startError) Error() string { return e.msg }
