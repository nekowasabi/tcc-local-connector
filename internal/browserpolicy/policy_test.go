package browserpolicy

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// MergeDomains was removed from production; test the same behavior through NormalizeDomains.
func mergeDomainsForTest(lists ...[]string) ([]string, error) {
	var all []string
	for _, list := range lists {
		all = append(all, list...)
	}
	return NormalizeDomains(all)
}

func TestPolicyDecodeStrictSchema(t *testing.T) {
	valid := `{"version":1,"generation":1,"enforce":true,"dry_run":false,"domains":["example.com"],"planned_domains":["example.com"],"updated_at":"2026-08-12T01:02:03.000000004Z"}`
	if _, err := Decode([]byte(valid)); err != nil {
		t.Fatalf("Decode(valid) = %v", err)
	}
	tests := map[string]string{
		"unknown":       `{"version":1,"generation":1,"enforce":false,"dry_run":false,"domains":[],"planned_domains":[],"updated_at":"2026-08-12T01:02:03Z","extra":true}`,
		"duplicate":     `{"version":1,"version":1,"generation":1,"enforce":false,"dry_run":false,"domains":[],"planned_domains":[],"updated_at":"2026-08-12T01:02:03Z"}`,
		"missing":       `{"version":1,"generation":1,"enforce":false,"dry_run":false,"domains":[],"planned_domains":[]}`,
		"null":          `{"version":1,"generation":1,"enforce":false,"dry_run":false,"domains":null,"planned_domains":[],"updated_at":"2026-08-12T01:02:03Z"}`,
		"wrong type":    `{"version":"1","generation":1,"enforce":false,"dry_run":false,"domains":[],"planned_domains":[],"updated_at":"2026-08-12T01:02:03Z"}`,
		"non UTC":       `{"version":1,"generation":1,"enforce":false,"dry_run":false,"domains":[],"planned_domains":[],"updated_at":"2026-08-12T10:02:03+09:00"}`,
		"non canonical": `{"version":1,"generation":1,"enforce":false,"dry_run":false,"domains":[],"planned_domains":[],"updated_at":"2026-08-12t01:02:03Z"}`,
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode([]byte(input)); err == nil {
				t.Fatal("Decode() succeeded, want error")
			}
		})
	}
}

func TestPolicyNormalizeMergeAndFingerprint(t *testing.T) {
	got, err := NormalizeDomains([]string{"B.Example.", "a.example", "b.example"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.example", "b.example"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeDomains() = %#v, want %#v", got, want)
	}
	merged, err := mergeDomainsForTest([]string{"b.example"}, []string{"a.example", "b.example"})
	if err != nil || !reflect.DeepEqual(merged, want) {
		t.Fatalf("mergeDomainsForTest() = %#v, %v", merged, err)
	}
	if Fingerprint(want) != Fingerprint([]string{"B.EXAMPLE.", "a.example"}) {
		t.Fatal("fingerprint differs for the same normalized set")
	}
	if _, err := NormalizeDomains([]string{"https://example.com"}); err == nil {
		t.Fatal("NormalizeDomains accepted a URL")
	}
	if _, err := NormalizeDomains([]string{"127.0.0.1"}); err == nil {
		t.Fatal("NormalizeDomains accepted an IP address")
	}
}

func TestPolicyValidateDryRunAndEmpty(t *testing.T) {
	now := "2026-08-12T01:02:03Z"
	dryRun := Policy{Version: 1, Generation: 1, DryRun: true, Domains: []string{}, PlannedDomains: []string{"example.com"}, UpdatedAt: now}
	if err := Validate(dryRun); err != nil {
		t.Fatalf("Validate(dry-run) = %v", err)
	}
	empty := Policy{Version: 1, Generation: 2, Domains: []string{}, PlannedDomains: []string{}, UpdatedAt: now}
	if err := Validate(empty); err != nil {
		t.Fatalf("Validate(empty) = %v", err)
	}
	dryRun.Domains = []string{"example.com"}
	if err := Validate(dryRun); err == nil {
		t.Fatal("Validate accepted enforced domains in dry-run")
	}
}

func TestStorePublishGenerationAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy", "firefox-browser-policy.json")
	store := NewStore(path)
	times := []time.Time{
		time.Date(2026, 8, 12, 1, 2, 3, 4, time.UTC),
		time.Date(2026, 8, 12, 1, 2, 8, 9, time.UTC),
	}
	store.now = func() time.Time {
		result := times[0]
		times = times[1:]
		return result
	}
	first, err := store.Publish(true, false, []string{"example.com"}, []string{"example.com"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Publish(true, false, []string{"example.com"}, []string{"example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Generation != 1 || second.Generation != 2 || first.UpdatedAt == second.UpdatedAt {
		t.Fatalf("publications = %#v, %#v", first, second)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("policy permissions = %v, %v", info, err)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil || dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("directory permissions = %v, %v", dirInfo, err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(payload, &keys); err != nil || len(keys) != 7 {
		t.Fatalf("policy keys = %v, %v", keys, err)
	}
}

func TestStorePublishEmptyAndPreserveOnWriteFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	store := NewStore(path)
	if _, err := store.PublishEmpty(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	store.write = func(string, []byte) error { return errors.New("injected write failure") }
	if _, err := store.Publish(true, false, []string{"example.com"}, []string{"example.com"}); err == nil {
		t.Fatal("Publish succeeded, want error")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("failed publication changed the current policy")
	}
}

func TestStoreDoesNotReuseGenerationAfterCommittedWriteFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	store := NewStore(path)
	store.write = func(string, []byte) error {
		return committedWriteError{err: errors.New("injected post-rename failure")}
	}
	if _, err := store.PublishEmpty(); err == nil {
		t.Fatal("Publish succeeded, want error")
	}
	store.write = writeAtomic
	policy, err := store.PublishEmpty()
	if err != nil {
		t.Fatal(err)
	}
	if policy.Generation != 2 {
		t.Fatalf("generation = %d, want 2", policy.Generation)
	}
}

func TestStoreRestoresGeneration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	first := NewStore(path)
	if _, err := first.PublishEmpty(); err != nil {
		t.Fatal(err)
	}
	second := NewStore(path)
	policy, err := second.PublishEmpty()
	if err != nil {
		t.Fatal(err)
	}
	if policy.Generation != 2 {
		t.Fatalf("generation = %d, want 2", policy.Generation)
	}
}
