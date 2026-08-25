package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/takets/tcc-local-connector/internal/constants"
)

type helloMessage struct {
	Type    string `json:"type"`
	Version int    `json:"version"`
}

type policyFile struct {
	Version        int      `json:"version"`
	Generation     uint64   `json:"generation"`
	Enforce        bool     `json:"enforce"`
	DryRun         bool     `json:"dry_run"`
	Domains        []string `json:"domains"`
	PlannedDomains []string `json:"planned_domains"`
	UpdatedAt      string   `json:"updated_at"`
}

type ownerHeartbeatFile struct {
	Version   int    `json:"version"`
	UpdatedAt string `json:"updated_at"`
}

type policyMessage struct {
	Type       string   `json:"type"`
	Version    int      `json:"version"`
	Generation uint64   `json:"generation"`
	Enforce    bool     `json:"enforce"`
	DryRun     bool     `json:"dry_run"`
	Domains    []string `json:"domains"`
}

type effectivePolicy struct {
	message policyMessage
	valid   bool
}

type connectionPolicyState struct {
	initialized bool
	watermark   uint64
	adopted     policyMessage
	sent        policyMessage
}

func main() {
	stateDir, err := resolveBrowserStateDir()
	if err == nil {
		err = run(os.Stdin, os.Stdout, stateDir, time.Now, time.Duration(constants.NativeHostPollIntervalMilliseconds)*time.Millisecond)
	}
	if err != nil {
		writeJSONError(os.Stderr, err)
		os.Exit(1)
	}
}

func run(in io.Reader, out io.Writer, stateDir string, now func() time.Time, pollInterval time.Duration) error {
	if err := readHello(in); err != nil {
		return err
	}

	state := connectionPolicyState{}
	message, send := pollPolicy(&state, readEffectivePolicy(stateDir, now()))
	if send {
		if err := writeNativeMessage(out, message); err != nil {
			return fmt.Errorf("write initial policy: %w", err)
		}
	}

	// Why: A read goroutine is used instead of polling stdin. Native Messaging keeps
	// the pipe open while connected, so blocking read is the reliable disconnect signal.
	disconnected := make(chan error, 1)
	go func() {
		_, err := readNativeMessage(in)
		if err == nil {
			err = errors.New("unexpected message after hello")
		}
		disconnected <- err
	}()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case err := <-disconnected:
			return err
		case <-ticker.C:
			message, send := pollPolicy(&state, readEffectivePolicy(stateDir, now()))
			if send {
				if err := writeNativeMessage(out, message); err != nil {
					return fmt.Errorf("write policy: %w", err)
				}
			}
		}
	}
}

func readNativeMessage(reader io.Reader) ([]byte, error) {
	header := make([]byte, constants.NativeMessageHeaderBytes)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, err
	}
	length := binary.LittleEndian.Uint32(header)
	if length == 0 || length > constants.NativeMessageMaxPayloadBytes {
		return nil, fmt.Errorf("invalid native message length: %d", length)
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	if !utf8.Valid(payload) {
		return nil, errors.New("native message payload is not UTF-8")
	}
	return payload, nil
}

func writeNativeMessage(writer io.Writer, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(payload) == 0 || len(payload) > constants.NativeMessageMaxPayloadBytes {
		return fmt.Errorf("invalid native message payload length: %d", len(payload))
	}
	frame := make([]byte, constants.NativeMessageHeaderBytes+len(payload))
	binary.LittleEndian.PutUint32(frame, uint32(len(payload)))
	copy(frame[constants.NativeMessageHeaderBytes:], payload)
	for len(frame) > 0 {
		n, err := writer.Write(frame)
		if err != nil {
			return err
		}
		if n <= 0 || n > len(frame) {
			return io.ErrShortWrite
		}
		frame = frame[n:]
	}
	return nil
}

func readHello(reader io.Reader) error {
	payload, err := readNativeMessage(reader)
	if err != nil {
		return fmt.Errorf("read hello: %w", err)
	}
	var hello helloMessage
	if err := decodeExactJSON(payload, []string{"type", "version"}, &hello); err != nil {
		return fmt.Errorf("decode hello: %w", err)
	}
	return validateHello(hello)
}

func validateHello(hello helloMessage) error {
	if hello.Type != "hello" || hello.Version != constants.BrowserPolicyVersion {
		return errors.New("invalid native messaging hello")
	}
	return nil
}

func readPolicyFile(path string) (policyFile, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return policyFile{}, err
	}
	var policy policyFile
	keys := []string{"version", "generation", "enforce", "dry_run", "domains", "planned_domains", "updated_at"}
	if err := decodeExactJSON(payload, keys, &policy); err != nil {
		return policyFile{}, err
	}
	return policy, nil
}

func readOwnerHeartbeatFile(path string) (ownerHeartbeatFile, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return ownerHeartbeatFile{}, err
	}
	var heartbeat ownerHeartbeatFile
	if err := decodeExactJSON(payload, []string{"version", "updated_at"}, &heartbeat); err != nil {
		return ownerHeartbeatFile{}, err
	}
	return heartbeat, nil
}

func validatePolicy(policy policyFile, now time.Time) error {
	if policy.Version != constants.BrowserPolicyVersion || policy.Generation == 0 {
		return errors.New("invalid browser policy version or generation")
	}
	if policy.Domains == nil || policy.PlannedDomains == nil {
		return errors.New("browser policy arrays must not be null")
	}
	if err := validateFreshTimestamp(policy.UpdatedAt, now); err != nil {
		return err
	}
	if err := validateDomains(policy.Domains); err != nil {
		return err
	}
	if err := validateDomains(policy.PlannedDomains); err != nil {
		return err
	}
	if policy.DryRun && (policy.Enforce || len(policy.Domains) != 0) {
		return errors.New("dry-run browser policy must not enforce domains")
	}
	if !policy.Enforce && !policy.DryRun && (len(policy.Domains) != 0 || len(policy.PlannedDomains) != 0) {
		return errors.New("disabled browser policy must be empty")
	}
	if policy.Enforce && !slices.Equal(policy.Domains, policy.PlannedDomains) {
		return errors.New("enforced browser policy domains must equal planned_domains")
	}
	return nil
}

func validateOwnerHeartbeat(heartbeat ownerHeartbeatFile, now time.Time) error {
	if heartbeat.Version != constants.BrowserPolicyVersion {
		return errors.New("invalid owner heartbeat version")
	}
	return validateFreshTimestamp(heartbeat.UpdatedAt, now)
}

func resolveBrowserStateDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, constants.BrowserStateDirRelative), nil
}

func readEffectivePolicy(stateDir string, now time.Time) effectivePolicy {
	policy, err := readPolicyFile(filepath.Join(stateDir, constants.BrowserPolicyFileName))
	if err != nil || validatePolicy(policy, now) != nil {
		return effectivePolicy{message: emptyPolicyMessage(0)}
	}
	heartbeat, err := readOwnerHeartbeatFile(filepath.Join(stateDir, constants.BrowserOwnerHeartbeatFileName))
	if err != nil || validateOwnerHeartbeat(heartbeat, now) != nil {
		return effectivePolicy{message: emptyPolicyMessage(0)}
	}
	message := policyMessage{
		Type: "policy", Version: constants.BrowserPolicyVersion, Generation: policy.Generation,
		Enforce: policy.Enforce, DryRun: policy.DryRun,
		Domains: append([]string(nil), policy.Domains...),
	}
	if !policy.Enforce || policy.DryRun || len(policy.Domains) == 0 {
		message = emptyPolicyMessage(policy.Generation)
	}
	return effectivePolicy{message: message, valid: true}
}

func emptyPolicyMessage(generation uint64) policyMessage {
	return policyMessage{Type: "policy", Version: constants.BrowserPolicyVersion, Generation: generation, Domains: []string{}}
}

func pollPolicy(state *connectionPolicyState, candidate effectivePolicy) (policyMessage, bool) {
	if !state.initialized {
		state.initialized = true
		if candidate.valid {
			state.watermark = candidate.message.Generation
			state.adopted = clonePolicyMessage(candidate.message)
		}
		state.sent = clonePolicyMessage(candidate.message)
		return candidate.message, true
	}

	if !candidate.valid || !candidate.message.Enforce || candidate.message.DryRun || len(candidate.message.Domains) == 0 {
		if candidate.valid && candidate.message.Generation > state.watermark {
			state.watermark = candidate.message.Generation
			state.adopted = clonePolicyMessage(candidate.message)
		}
		if len(state.sent.Domains) == 0 && !state.sent.Enforce {
			return policyMessage{}, false
		}
		empty := emptyPolicyMessage(state.watermark)
		state.sent = clonePolicyMessage(empty)
		return empty, true
	}

	if candidate.message.Generation < state.watermark {
		if len(state.sent.Domains) == 0 && !state.sent.Enforce {
			return policyMessage{}, false
		}
		empty := emptyPolicyMessage(state.watermark)
		state.sent = clonePolicyMessage(empty)
		return empty, true
	}
	if candidate.message.Generation == state.watermark {
		if !equalPolicyMessage(candidate.message, state.adopted) {
			return policyMessage{}, false
		}
		if equalPolicyMessage(candidate.message, state.sent) {
			return policyMessage{}, false
		}
		state.sent = clonePolicyMessage(candidate.message)
		return candidate.message, true
	}

	state.watermark = candidate.message.Generation
	state.adopted = clonePolicyMessage(candidate.message)
	state.sent = clonePolicyMessage(candidate.message)
	return candidate.message, true
}

func writeJSONError(writer io.Writer, err error) {
	_ = json.NewEncoder(writer).Encode(map[string]string{"error": err.Error()})
}

func decodeExactJSON(payload []byte, keys []string, target any) error {
	if !utf8.Valid(payload) {
		return errors.New("JSON is not UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return errors.New("JSON value must be an object")
	}
	raw := make(map[string]json.RawMessage, len(keys))
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := keyToken.(string)
		if !ok {
			return errors.New("JSON object key must be a string")
		}
		if _, exists := raw[key]; exists {
			return fmt.Errorf("duplicate JSON key %q", key)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		raw[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return err
	}
	if len(raw) != len(keys) {
		return errors.New("JSON object has an incorrect key count")
	}
	for _, key := range keys {
		value, exists := raw[key]
		if !exists || bytes.Equal(value, []byte("null")) {
			return fmt.Errorf("JSON key %q is missing or null", key)
		}
	}
	strict := json.NewDecoder(bytes.NewReader(payload))
	strict.DisallowUnknownFields()
	if err := strict.Decode(target); err != nil {
		return err
	}
	return ensureJSONEOF(strict)
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

const rfc3339NanoUTCFixedFraction = "2006-01-02T15:04:05.000000000Z"

func validateFreshTimestamp(value string, now time.Time) error {
	updatedAt, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || updatedAt.Location() != time.UTC {
		return errors.New("updated_at must be canonical RFC3339Nano UTC")
	}
	// Why: Swift writes a fixed nine-digit fraction (including trailing zeros).
	// Go RFC3339Nano omits those zeros, so exact Format equality would fail-open live heartbeats.
	if value != updatedAt.Format(time.RFC3339Nano) && value != updatedAt.Format(rfc3339NanoUTCFixedFraction) {
		return errors.New("updated_at must be canonical RFC3339Nano UTC")
	}
	if updatedAt.After(now) || now.Sub(updatedAt) > time.Duration(constants.BrowserLivenessTTLSeconds)*time.Second {
		return errors.New("updated_at is outside the liveness window")
	}
	return nil
}

func validateDomains(domains []string) error {
	if len(domains) > constants.BrowserPolicyMaxDomains {
		return errors.New("too many browser policy domains")
	}
	if !sort.StringsAreSorted(domains) {
		return errors.New("browser policy domains are not sorted")
	}
	for index, domain := range domains {
		if index > 0 && domains[index-1] == domain {
			return errors.New("browser policy domains contain duplicates")
		}
		if domain == "" || domain != strings.ToLower(domain) || strings.HasSuffix(domain, ".") || len(domain) > constants.BrowserDomainMaxBytes || net.ParseIP(domain) != nil {
			return fmt.Errorf("invalid browser policy domain %q", domain)
		}
		for _, char := range domain {
			if char > 0x7f {
				return fmt.Errorf("invalid browser policy domain %q", domain)
			}
		}
		for _, label := range strings.Split(domain, ".") {
			if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return fmt.Errorf("invalid browser policy domain %q", domain)
			}
			for _, char := range label {
				if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
					return fmt.Errorf("invalid browser policy domain %q", domain)
				}
			}
		}
	}
	return nil
}

func equalPolicyMessage(left, right policyMessage) bool {
	if left.Type != right.Type || left.Version != right.Version || left.Generation != right.Generation || left.Enforce != right.Enforce || left.DryRun != right.DryRun {
		return false
	}
	return slices.Equal(left.Domains, right.Domains)
}

func clonePolicyMessage(message policyMessage) policyMessage {
	message.Domains = append([]string(nil), message.Domains...)
	return message
}
