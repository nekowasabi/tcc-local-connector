package browserpolicy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/takets/tcc-local-connector/internal/constants"
)

type Policy struct {
	Version        int      `json:"version"`
	Generation     uint64   `json:"generation"`
	Enforce        bool     `json:"enforce"`
	DryRun         bool     `json:"dry_run"`
	Domains        []string `json:"domains"`
	PlannedDomains []string `json:"planned_domains"`
	UpdatedAt      string   `json:"updated_at"`
}

func NormalizeDomains(domains []string) ([]string, error) {
	unique := make(map[string]struct{}, len(domains))
	for _, domain := range domains {
		domain = strings.TrimSuffix(strings.ToLower(domain), ".")
		if err := validateDomain(domain); err != nil {
			return nil, err
		}
		unique[domain] = struct{}{}
	}
	if len(unique) > constants.BrowserPolicyMaxDomains {
		return nil, fmt.Errorf("too many browser policy domains: %d", len(unique))
	}
	result := make([]string, 0, len(unique))
	for domain := range unique {
		result = append(result, domain)
	}
	sort.Strings(result)
	return result, nil
}

func Fingerprint(domains []string) string {
	normalized, err := NormalizeDomains(domains)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join(normalized, "\n")))
	return hex.EncodeToString(sum[:])
}

func Validate(policy Policy) error {
	if policy.Version != constants.BrowserPolicyVersion {
		return errors.New("invalid browser policy version")
	}
	if policy.Generation == 0 {
		return errors.New("browser policy generation must be positive")
	}
	if policy.Domains == nil || policy.PlannedDomains == nil {
		return errors.New("browser policy domain arrays must not be null")
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, policy.UpdatedAt)
	if err != nil || updatedAt.Location() != time.UTC || updatedAt.Format(time.RFC3339Nano) != policy.UpdatedAt {
		return errors.New("browser policy updated_at must be canonical RFC3339Nano UTC")
	}
	domains, err := NormalizeDomains(policy.Domains)
	if err != nil || !equalDomains(domains, policy.Domains) {
		return errors.New("browser policy domains must be normalized, unique, and sorted")
	}
	planned, err := NormalizeDomains(policy.PlannedDomains)
	if err != nil || !equalDomains(planned, policy.PlannedDomains) {
		return errors.New("browser policy planned_domains must be normalized, unique, and sorted")
	}
	if policy.DryRun && (policy.Enforce || len(policy.Domains) != 0) {
		return errors.New("dry-run browser policy must not enforce domains")
	}
	if !policy.Enforce && !policy.DryRun && (len(policy.Domains) != 0 || len(policy.PlannedDomains) != 0) {
		return errors.New("disabled browser policy must be empty")
	}
	if policy.Enforce && !equalDomains(policy.Domains, policy.PlannedDomains) {
		return errors.New("enforced browser policy domains must equal planned_domains")
	}
	return nil
}

func Decode(data []byte) (Policy, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	raw, err := decodeRawObject(decoder)
	if err != nil {
		return Policy{}, err
	}
	want := []string{"version", "generation", "enforce", "dry_run", "domains", "planned_domains", "updated_at"}
	if len(raw) != len(want) {
		return Policy{}, errors.New("browser policy must contain exactly seven keys")
	}
	for _, key := range want {
		value, ok := raw[key]
		if !ok || bytes.Equal(value, []byte("null")) {
			return Policy{}, fmt.Errorf("browser policy key %q is missing or null", key)
		}
	}
	var policy Policy
	strict := json.NewDecoder(bytes.NewReader(data))
	strict.DisallowUnknownFields()
	if err := strict.Decode(&policy); err != nil {
		return Policy{}, err
	}
	if err := ensureJSONEOF(strict); err != nil {
		return Policy{}, err
	}
	if err := Validate(policy); err != nil {
		return Policy{}, err
	}
	return policy, nil
}

func decodeRawObject(decoder *json.Decoder) (map[string]json.RawMessage, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return nil, errors.New("browser policy must be a JSON object")
	}
	result := map[string]json.RawMessage{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, errors.New("browser policy key must be a string")
		}
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("duplicate browser policy key %q", key)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		result[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	return result, ensureJSONEOF(decoder)
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

func validateDomain(domain string) error {
	if domain == "" || len(domain) > constants.BrowserDomainMaxBytes || net.ParseIP(domain) != nil {
		return fmt.Errorf("invalid browser policy domain %q", domain)
	}
	for i := 0; i < len(domain); i++ {
		if domain[i] > 0x7f {
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
	return nil
}

func equalDomains(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
