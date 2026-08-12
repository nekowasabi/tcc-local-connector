package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/takets/tcc-local-connector/internal/constants"
)

type chunkReader struct {
	reader io.Reader
	size   int
}

func (reader *chunkReader) Read(payload []byte) (int, error) {
	if len(payload) > reader.size {
		payload = payload[:reader.size]
	}
	return reader.reader.Read(payload)
}

type chunkWriter struct {
	buffer bytes.Buffer
	size   int
}

func (writer *chunkWriter) Write(payload []byte) (int, error) {
	if len(payload) > writer.size {
		payload = payload[:writer.size]
	}
	return writer.buffer.Write(payload)
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("injected write failure")
}

func TestNativeMessageFramingAndBoundaries(t *testing.T) {
	payload := []byte(`{"type":"hello","version":1}`)
	frame := nativeFrame(payload)
	got, err := readNativeMessage(&chunkReader{reader: bytes.NewReader(frame), size: 2})
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("partial read = %q, %v", got, err)
	}

	writer := &chunkWriter{size: 3}
	if err := writeNativeMessage(writer, json.RawMessage(payload)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(writer.buffer.Bytes(), frame) {
		t.Fatalf("partial write = %v, want %v", writer.buffer.Bytes(), frame)
	}

	maximum := bytes.Repeat([]byte("a"), constants.NativeMessageMaxPayloadBytes)
	if got, err := readNativeMessage(bytes.NewReader(nativeFrame(maximum))); err != nil || len(got) != len(maximum) {
		t.Fatalf("maximum payload = %d, %v", len(got), err)
	}
	tooLargeHeader := make([]byte, constants.NativeMessageHeaderBytes)
	binary.LittleEndian.PutUint32(tooLargeHeader, constants.NativeMessageMaxPayloadBytes+1)
	if _, err := readNativeMessage(bytes.NewReader(tooLargeHeader)); err == nil {
		t.Fatal("oversized payload was accepted")
	}
	zeroHeader := make([]byte, constants.NativeMessageHeaderBytes)
	if _, err := readNativeMessage(bytes.NewReader(zeroHeader)); err == nil {
		t.Fatal("zero payload was accepted")
	}
	if _, err := readNativeMessage(bytes.NewReader(frame[:2])); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("partial header error = %v", err)
	}
	if _, err := readNativeMessage(bytes.NewReader(frame[:len(frame)-1])); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("partial payload error = %v", err)
	}
	invalidUTF8 := nativeFrame([]byte{0xff})
	if _, err := readNativeMessage(bytes.NewReader(invalidUTF8)); err == nil {
		t.Fatal("invalid UTF-8 was accepted")
	}
	rawMaximum := json.RawMessage(`"` + strings.Repeat("a", constants.NativeMessageMaxPayloadBytes-2) + `"`)
	if err := writeNativeMessage(io.Discard, rawMaximum); err != nil {
		t.Fatalf("maximum write failed: %v", err)
	}
	if err := writeNativeMessage(io.Discard, strings.Repeat("a", constants.NativeMessageMaxPayloadBytes)); err == nil {
		t.Fatal("oversized write was accepted")
	}
}

func TestMaximumValidPolicyFitsNativeMessage(t *testing.T) {
	domains := make([]string, constants.BrowserPolicyMaxDomains)
	for index := range domains {
		domains[index] = fmt.Sprintf("%061d%02x.%063d.%063d.%061d", 0, index, 0, 0, 0)
	}
	message := policyMessage{
		Type: "policy", Version: constants.BrowserPolicyVersion, Generation: 1,
		Enforce: true, Domains: domains,
	}
	if err := writeNativeMessage(io.Discard, message); err != nil {
		t.Fatalf("maximum valid policy did not fit native message: %v", err)
	}
}

func TestHelloStrictJSON(t *testing.T) {
	valid := nativeFrame([]byte(`{"type":"hello","version":1}`))
	if err := readHello(bytes.NewReader(valid)); err != nil {
		t.Fatalf("valid hello: %v", err)
	}
	tests := []string{
		`{"type":"hello"}`,
		`{"type":"hello","version":1,"extra":true}`,
		`{"type":"hello","type":"hello","version":1}`,
		`{"type":"other","version":1}`,
		`{"type":"hello","version":2}`,
		`{"type":"hello","version":"1"}`,
		`{"type":"hello","version":1} {}`,
	}
	for _, input := range tests {
		if err := readHello(bytes.NewReader(nativeFrame([]byte(input)))); err == nil {
			t.Errorf("invalid hello was accepted: %s", input)
		}
	}
}

func TestPolicyStrictSchemaAndValidation(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	path := filepath.Join(dir, constants.BrowserPolicyFileName)
	valid := policyJSON(4, true, false, []string{"example.com"}, []string{"example.com"}, now)
	writeFile(t, path, valid)
	policy, err := readPolicyFile(path)
	if err != nil || validatePolicy(policy, now) != nil {
		t.Fatalf("valid policy = %#v, %v", policy, err)
	}

	invalid := []string{
		strings.Replace(valid, `"updated_at":`, `"extra":true,"updated_at":`, 1),
		strings.Replace(valid, `"version":1`, `"version":1,"version":1`, 1),
		strings.Replace(valid, `,"updated_at":"`+now.Format(time.RFC3339Nano)+`"`, "", 1),
		strings.Replace(valid, `"generation":4`, `"generation":"4"`, 1),
		`{broken`,
	}
	for _, input := range invalid {
		writeFile(t, path, input)
		if _, err := readPolicyFile(path); err == nil {
			t.Errorf("invalid policy schema was accepted: %s", input)
		}
	}

	validationCases := []policyFile{
		{Version: 2, Generation: 1, Domains: []string{}, PlannedDomains: []string{}, UpdatedAt: now.Format(time.RFC3339Nano)},
		{Version: 1, Generation: 0, Domains: []string{}, PlannedDomains: []string{}, UpdatedAt: now.Format(time.RFC3339Nano)},
		{Version: 1, Generation: 1, Domains: nil, PlannedDomains: []string{}, UpdatedAt: now.Format(time.RFC3339Nano)},
		{Version: 1, Generation: 1, Domains: []string{"Example.com"}, PlannedDomains: []string{}, UpdatedAt: now.Format(time.RFC3339Nano)},
		{Version: 1, Generation: 1, Domains: []string{"b.com", "a.com"}, PlannedDomains: []string{}, UpdatedAt: now.Format(time.RFC3339Nano)},
		{Version: 1, Generation: 1, Domains: []string{"a.com", "a.com"}, PlannedDomains: []string{}, UpdatedAt: now.Format(time.RFC3339Nano)},
		{Version: 1, Generation: 1, Domains: []string{"h" + "ttps://example.com"}, PlannedDomains: []string{}, UpdatedAt: now.Format(time.RFC3339Nano)},
		{Version: 1, Generation: 1, Domains: []string{"127.0.0.1"}, PlannedDomains: []string{}, UpdatedAt: now.Format(time.RFC3339Nano)},
		{Version: 1, Generation: 1, DryRun: true, Enforce: true, Domains: []string{"a.com"}, PlannedDomains: []string{"a.com"}, UpdatedAt: now.Format(time.RFC3339Nano)},
		{Version: 1, Generation: 1, Domains: []string{"a.com"}, PlannedDomains: []string{"a.com"}, UpdatedAt: now.Format(time.RFC3339Nano)},
		{Version: 1, Generation: 1, Enforce: true, Domains: []string{"a.com"}, PlannedDomains: []string{"b.com"}, UpdatedAt: now.Format(time.RFC3339Nano)},
		{Version: 1, Generation: 1, Domains: []string{}, PlannedDomains: []string{}, UpdatedAt: now.Add(time.Nanosecond).Format(time.RFC3339Nano)},
		{Version: 1, Generation: 1, Domains: []string{}, PlannedDomains: []string{}, UpdatedAt: now.Add(-15*time.Second - time.Nanosecond).Format(time.RFC3339Nano)},
	}
	for index, candidate := range validationCases {
		if err := validatePolicy(candidate, now); err == nil {
			t.Errorf("validation case %d was accepted: %#v", index, candidate)
		}
	}
	tooMany := make([]string, constants.BrowserPolicyMaxDomains+1)
	for index := range tooMany {
		tooMany[index] = strings.Repeat("a", 3) + string(rune('a'+index/26)) + string(rune('a'+index%26)) + ".example"
	}
	sortStrings(tooMany)
	if err := validateDomains(tooMany); err == nil {
		t.Fatal("too many domains were accepted")
	}
	boundary := policyFile{Version: 1, Generation: 1, Domains: []string{}, PlannedDomains: []string{}, UpdatedAt: now.Add(-15 * time.Second).Format(time.RFC3339Nano)}
	if err := validatePolicy(boundary, now); err != nil {
		t.Fatalf("TTL boundary was rejected: %v", err)
	}
}

func TestHeartbeatStrictSchemaAndValidation(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), constants.BrowserOwnerHeartbeatFileName)
	valid := heartbeatJSON(now)
	writeFile(t, path, valid)
	heartbeat, err := readOwnerHeartbeatFile(path)
	if err != nil || validateOwnerHeartbeat(heartbeat, now) != nil {
		t.Fatalf("valid heartbeat = %#v, %v", heartbeat, err)
	}
	for _, input := range []string{
		strings.Replace(valid, `"updated_at":`, `"generation":1,"updated_at":`, 1),
		strings.Replace(valid, `"version":1`, `"version":1,"version":1`, 1),
		`{"version":1}`,
		`{"version":"1","updated_at":"` + now.Format(time.RFC3339Nano) + `"}`,
	} {
		writeFile(t, path, input)
		if _, err := readOwnerHeartbeatFile(path); err == nil {
			t.Errorf("invalid heartbeat schema was accepted: %s", input)
		}
	}
	for _, candidate := range []ownerHeartbeatFile{
		{Version: 2, UpdatedAt: now.Format(time.RFC3339Nano)},
		{Version: 1, UpdatedAt: now.Add(time.Nanosecond).Format(time.RFC3339Nano)},
		{Version: 1, UpdatedAt: now.Add(-15*time.Second - time.Nanosecond).Format(time.RFC3339Nano)},
		{Version: 1, UpdatedAt: now.In(time.FixedZone("offset", 3600)).Format(time.RFC3339Nano)},
	} {
		if err := validateOwnerHeartbeat(candidate, now); err == nil {
			t.Errorf("invalid heartbeat was accepted: %#v", candidate)
		}
	}
}

func TestPolicyEffectiveFailOpen(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	writeStates(t, dir, policyJSON(7, true, false, []string{"example.com"}, []string{"example.com"}, now), heartbeatJSON(now))
	got := readEffectivePolicy(dir, now)
	if !got.valid || !got.message.Enforce || !reflect.DeepEqual(got.message.Domains, []string{"example.com"}) {
		t.Fatalf("effective policy = %#v", got)
	}

	cases := []struct {
		name      string
		policy    string
		heartbeat string
	}{
		{"missing policy", "", heartbeatJSON(now)},
		{"broken policy", "{", heartbeatJSON(now)},
		{"missing heartbeat", policyJSON(7, true, false, []string{"example.com"}, []string{"example.com"}, now), ""},
		{"expired policy", policyJSON(7, true, false, []string{"example.com"}, []string{"example.com"}, now.Add(-16*time.Second)), heartbeatJSON(now)},
		{"future heartbeat", policyJSON(7, true, false, []string{"example.com"}, []string{"example.com"}, now), heartbeatJSON(now.Add(time.Second))},
		{"disabled", policyJSON(7, false, false, []string{"example.com"}, []string{"example.com"}, now), heartbeatJSON(now)},
		{"dry run", policyJSON(7, false, true, []string{}, []string{"example.com"}, now), heartbeatJSON(now)},
		{"empty", policyJSON(7, true, false, []string{}, []string{}, now), heartbeatJSON(now)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			caseDir := t.TempDir()
			if test.policy != "" {
				writeFile(t, filepath.Join(caseDir, constants.BrowserPolicyFileName), test.policy)
			}
			if test.heartbeat != "" {
				writeFile(t, filepath.Join(caseDir, constants.BrowserOwnerHeartbeatFileName), test.heartbeat)
			}
			got := readEffectivePolicy(caseDir, now)
			if got.message.Enforce || len(got.message.Domains) != 0 {
				t.Fatalf("fail-open policy = %#v", got)
			}
		})
	}
}

func TestGenerationWatermarkAndInvalidation(t *testing.T) {
	state := connectionPolicyState{}
	active2 := validEffective(2, "two.example")
	if got, send := pollPolicy(&state, active2); !send || !equalPolicyMessage(got, active2.message) {
		t.Fatalf("initial = %#v, %v", got, send)
	}
	if _, send := pollPolicy(&state, active2); send {
		t.Fatal("same generation and content was sent")
	}
	modified2 := validEffective(2, "changed.example")
	if _, send := pollPolicy(&state, modified2); send {
		t.Fatal("same generation modification was sent")
	}
	active3 := validEffective(3, "three.example")
	if got, send := pollPolicy(&state, active3); !send || got.Generation != 3 {
		t.Fatalf("new generation = %#v, %v", got, send)
	}
	if got, send := pollPolicy(&state, effectivePolicy{message: emptyPolicyMessage(0)}); !send || got.Enforce || len(got.Domains) != 0 || got.Generation != 3 {
		t.Fatalf("invalidation = %#v, %v", got, send)
	}
	if got, send := pollPolicy(&state, active3); !send || !got.Enforce {
		t.Fatalf("heartbeat recovery = %#v, %v", got, send)
	}
	if got, send := pollPolicy(&state, active2); !send || got.Enforce || got.Generation != 3 {
		t.Fatalf("stale generation fail-open = %#v, %v", got, send)
	}
	disabledOld := effectivePolicy{valid: true, message: emptyPolicyMessage(1)}
	if _, send := pollPolicy(&state, disabledOld); send {
		t.Fatal("repeated old-generation invalidation was sent")
	}
	if got, send := pollPolicy(&state, active3); !send || !got.Enforce {
		t.Fatalf("same adopted generation recovery = %#v, %v", got, send)
	}
}

func TestPollPolicyStateChange(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	writeStates(t, dir, policyJSON(1, true, false, []string{"one.example"}, []string{"one.example"}, now), heartbeatJSON(now))
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- run(inputReader, outputWriter, dir, func() time.Time { return now }, 5*time.Millisecond)
	}()
	if _, err := inputWriter.Write(nativeFrame([]byte(`{"type":"hello","version":1}`))); err != nil {
		t.Fatal(err)
	}
	first := readPolicyFrame(t, outputReader)
	if first.Generation != 1 || !reflect.DeepEqual(first.Domains, []string{"one.example"}) {
		t.Fatalf("initial frame = %#v", first)
	}
	writeStates(t, dir, policyJSON(2, true, false, []string{"two.example"}, []string{"two.example"}, now), heartbeatJSON(now))
	second := readPolicyFrame(t, outputReader)
	if second.Generation != 2 || !reflect.DeepEqual(second.Domains, []string{"two.example"}) {
		t.Fatalf("polled frame = %#v", second)
	}
	if err := inputWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, io.EOF) {
		t.Fatalf("disconnect error = %v", err)
	}
	_ = outputReader.Close()
	_ = outputWriter.Close()
}

func TestDisconnectAndWriteFailure(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	writeStates(t, dir, policyJSON(1, true, false, []string{"example.com"}, []string{"example.com"}, now), heartbeatJSON(now))
	hello := nativeFrame([]byte(`{"type":"hello","version":1}`))
	var output bytes.Buffer
	if err := run(bytes.NewReader(hello), &output, dir, func() time.Time { return now }, time.Hour); !errors.Is(err, io.EOF) {
		t.Fatalf("EOF error = %v", err)
	}
	if message := decodeOnlyPolicyFrame(t, output.Bytes()); message.Generation != 1 {
		t.Fatalf("output policy = %#v", message)
	}
	if err := run(bytes.NewReader(hello), failingWriter{}, dir, func() time.Time { return now }, time.Hour); err == nil || !strings.Contains(err.Error(), "injected write failure") {
		t.Fatalf("write failure = %v", err)
	}
}

func TestStdoutContainsOnlyNativeFrames(t *testing.T) {
	message := validEffective(9, "example.com").message
	var stdout bytes.Buffer
	if err := writeNativeMessage(&stdout, message); err != nil {
		t.Fatal(err)
	}
	decoded := decodeOnlyPolicyFrame(t, stdout.Bytes())
	if !equalPolicyMessage(decoded, message) {
		t.Fatalf("decoded frame = %#v, want %#v", decoded, message)
	}
	var stderr bytes.Buffer
	writeJSONError(&stderr, errors.New("diagnostic"))
	if !strings.Contains(stderr.String(), `"error":"diagnostic"`) {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func nativeFrame(payload []byte) []byte {
	frame := make([]byte, constants.NativeMessageHeaderBytes+len(payload))
	binary.LittleEndian.PutUint32(frame, uint32(len(payload)))
	copy(frame[constants.NativeMessageHeaderBytes:], payload)
	return frame
}

func policyJSON(generation uint64, enforce, dryRun bool, domains, planned []string, updatedAt time.Time) string {
	payload, err := json.Marshal(policyFile{
		Version: constants.BrowserPolicyVersion, Generation: generation, Enforce: enforce, DryRun: dryRun,
		Domains: domains, PlannedDomains: planned, UpdatedAt: updatedAt.Format(time.RFC3339Nano),
	})
	if err != nil {
		panic(err)
	}
	return string(payload)
}

func heartbeatJSON(updatedAt time.Time) string {
	payload, err := json.Marshal(ownerHeartbeatFile{Version: constants.BrowserPolicyVersion, UpdatedAt: updatedAt.Format(time.RFC3339Nano)})
	if err != nil {
		panic(err)
	}
	return string(payload)
}

func writeStates(t *testing.T, dir, policy, heartbeat string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, constants.BrowserPolicyFileName), policy)
	writeFile(t, filepath.Join(dir, constants.BrowserOwnerHeartbeatFileName), heartbeat)
}

func writeFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func validEffective(generation uint64, domain string) effectivePolicy {
	return effectivePolicy{valid: true, message: policyMessage{
		Type: "policy", Version: constants.BrowserPolicyVersion, Generation: generation, Enforce: true,
		Domains: []string{domain},
	}}
}

func readPolicyFrame(t *testing.T, reader io.Reader) policyMessage {
	t.Helper()
	payload, err := readNativeMessage(reader)
	if err != nil {
		t.Fatal(err)
	}
	var message policyMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatal(err)
	}
	return message
}

func decodeOnlyPolicyFrame(t *testing.T, frame []byte) policyMessage {
	t.Helper()
	reader := bytes.NewReader(frame)
	message := readPolicyFrame(t, reader)
	if reader.Len() != 0 {
		t.Fatalf("stdout contains %d bytes outside the native frame", reader.Len())
	}
	return message
}

func sortStrings(values []string) {
	for index := 1; index < len(values); index++ {
		for current := index; current > 0 && values[current] < values[current-1]; current-- {
			values[current], values[current-1] = values[current-1], values[current]
		}
	}
}
