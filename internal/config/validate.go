package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/takets/tcc-local-connector/internal/constants"
)

const (
	codeParseError               = "parse_error"
	codeUnsupportedConfigVersion = "unsupported_config_version"
	codeMissingRequiredField     = "missing_required_field"
	codeUnknownField             = "unknown_field"
	codeUnknownActionType        = "unknown_action_type"
	codeUnsupportedAction        = "unsupported_action"
	codeDuplicateRuleID          = "duplicate_rule_id"
	codeDuplicateProcessID       = "duplicate_process_id"
	codeValueOutOfRange          = "value_out_of_range"
	codeTimeoutExceedsInterval   = "timeout_exceeds_interval"
	codeUnsupportedValue         = "unsupported_value"
	codeExecutableNotFound       = "executable_not_found"
	codeInvalidIdentifier        = "invalid_identifier"
	codeShellNotAllowed          = "shell_not_allowed"
	codeForbiddenSafetyFlag      = "forbidden_safety_flag"
	codeRuleConflictSamePriority = "rule_conflict_same_priority"
)

const (
	codeBrowserBlockRequiresEnsure = "browser_block_requires_ensure"
	codeBrowserDomainsRequired     = "browser_domains_required"
	codeBrowserDomainsLimit        = "browser_domains_limit"
	codeInvalidBrowserDomain       = "invalid_browser_domain"
)

var (
	reRuleID    = regexp.MustCompile(constants.RuleIDPattern)
	reBundleID  = regexp.MustCompile(constants.BundleIDPattern)
	reProcessID = regexp.MustCompile(constants.ProcessIDPattern)
	reEnvKey    = regexp.MustCompile(constants.EnvKeyPattern)
)

type ValidationError struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

func (e ValidationError) Error() string { return e.Code + ": " + e.Path }
func validation(code, path string) ValidationError {
	return ValidationError{Code: code, Path: path, Message: code}
}
func Validate(cfg *Config) (*Config, []ValidationError) {
	var errors []ValidationError
	if cfg.Version != constants.ConfigSchemaVersion {
		errors = append(errors, validation(codeUnsupportedConfigVersion, "version"))
	}
	if cfg.TaskSource.Executable == "" {
		errors = append(errors, validation(codeMissingRequiredField, "task_source.executable"))
	}
	if cfg.TaskSource.Type != "tcc2_mcp" {
		errors = append(errors, validation(codeUnsupportedValue, "task_source.type"))
	}
	if !executable(cfg.TaskSource.Executable) {
		errors = append(errors, validation(codeExecutableNotFound, "task_source.executable"))
	}
	if outOfRange(cfg.Polling.IntervalSeconds, constants.MinPollIntervalSeconds, constants.MaxPollIntervalSeconds) ||
		outOfRange(cfg.Polling.TimeoutSeconds, constants.MinPollTimeoutSeconds, constants.MaxPollTimeoutSeconds) ||
		outOfRange(cfg.Polling.FailureGraceSeconds, constants.MinFailureGraceSeconds, constants.MaxFailureGraceSeconds) {
		errors = append(errors, validation(codeValueOutOfRange, "polling"))
	}
	if cfg.Polling.TimeoutSeconds >= cfg.Polling.IntervalSeconds {
		errors = append(errors, validation(codeTimeoutExceedsInterval, "polling.timeout_seconds"))
	}
	if cfg.Polling.FailurePolicy != "release_controls" {
		errors = append(errors, validation(codeUnsupportedValue, "polling.failure_policy"))
	}
	if cfg.Safety.AllowExternalProcessControl || cfg.Safety.AllowForceTerminate {
		errors = append(errors, validation(codeForbiddenSafetyFlag, "safety"))
	}
	if !oneOf(cfg.Logging.Level, "debug", "info", "warn", "error") {
		errors = append(errors, validation(codeUnsupportedValue, "logging.level"))
	}
	if outOfRange(cfg.Logging.RetainDays, constants.MinLogRetainDays, constants.MaxLogRetainDays) || len(cfg.Rules) > constants.MaxRules {
		errors = append(errors, validation(codeValueOutOfRange, "logging.retain_days"))
	}
	ruleIDs, processIDs, conflicts := map[string]bool{}, map[string]bool{}, map[string]string{}
	for i := range cfg.Rules {
		errors = append(errors, validateRule(&cfg.Rules[i], i, cfg.Safety.AllowShell, ruleIDs, processIDs, conflicts)...)
	}
	if len(errors) > 0 {
		return nil, errors
	}
	return cfg, nil
}
func validateRule(rule *Rule, index int, allowShell bool, ruleIDs, processIDs map[string]bool, conflicts map[string]string) []ValidationError {
	var errors []ValidationError
	base := "rules[" + strconvItoa(index) + "]"
	if rule.ID == "" {
		errors = append(errors, validation(codeMissingRequiredField, base+".id"))
	} else if !reRuleID.MatchString(rule.ID) {
		errors = append(errors, validation(codeInvalidIdentifier, base+".id"))
	} else if ruleIDs[rule.ID] {
		errors = append(errors, validation(codeDuplicateRuleID, base+".id"))
	} else {
		ruleIDs[rule.ID] = true
	}
	if outOfRange(rule.Priority, 0, 1000) ||
		outOfRange(len(rule.Ensure), 0, constants.MaxActionsPerRule) ||
		outOfRange(len(rule.OnEnter), 0, constants.MaxActionsPerRule) ||
		outOfRange(len(rule.OnExit), 0, constants.MaxActionsPerRule) {
		errors = append(errors, validation(codeValueOutOfRange, base))
	}
	for _, values := range [][]string{rule.Match.TaskNameContains, rule.Match.TaskNameNotContains} {
		for _, value := range values {
			if value == "" {
				errors = append(errors, validation(codeValueOutOfRange, base+".match"))
			}
		}
	}
	for group, actions := range map[string][]Action{"ensure": rule.Ensure, "on_enter": rule.OnEnter, "on_exit": rule.OnExit} {
		for i := range actions {
			errors = append(errors, validateAction(&actions[i], base+"."+group+"["+strconvItoa(i)+"]", group, allowShell, processIDs, conflicts, rule.Priority)...)
		}
	}
	return errors
}
func validateAction(action *Action, path, group string, allowShell bool, processIDs map[string]bool, conflicts map[string]string, priority int) []ValidationError {
	var errors []ValidationError
	switch action.Type {
	case constants.BrowserBlockActionType:
		if group != "ensure" {
			errors = append(errors, validation(codeBrowserBlockRequiresEnsure, path+".type"))
		}
		normalized, domainErrors := validateBrowserDomains(action.Domains, path+".domains")
		errors = append(errors, domainErrors...)
		if len(domainErrors) == 0 {
			action.Domains = normalized
		}
	case "browser.redirect":
		return []ValidationError{validation(codeUnsupportedAction, path+".type")}
	case "app.start":
		if len(action.BundleIDs) != 0 {
			errors = append(errors, validation(codeUnsupportedValue, path+".bundle_ids"))
		}
		if action.BundleID == "" {
			errors = append(errors, validation(codeMissingRequiredField, path+".bundle_id"))
		} else if !reBundleID.MatchString(action.BundleID) {
			errors = append(errors, validation(codeInvalidIdentifier, path+".bundle_id"))
		} else {
			recordAppConflict(conflicts, priority, action.Type, action.BundleID, path, &errors)
		}
	case "app.stop":
		ids, idErrors := validateAppStopIDs(action, path)
		errors = append(errors, idErrors...)
		if action.GraceSeconds != 0 && outOfRange(action.GraceSeconds, constants.MinGraceSeconds, constants.MaxGraceSeconds) {
			errors = append(errors, validation(codeValueOutOfRange, path+".grace_seconds"))
		}
		if len(idErrors) == 0 {
			action.BundleIDs = ids
			for _, id := range ids {
				recordAppConflict(conflicts, priority, action.Type, id, path, &errors)
			}
		}
	case "process.start":
		if action.ProcessID == "" {
			errors = append(errors, validation(codeMissingRequiredField, path+".process_id"))
		} else if !reProcessID.MatchString(action.ProcessID) {
			errors = append(errors, validation(codeInvalidIdentifier, path+".process_id"))
		} else if processIDs[action.ProcessID] {
			errors = append(errors, validation(codeDuplicateProcessID, path+".process_id"))
		} else {
			processIDs[action.ProcessID] = true
		}
		if !executable(action.Executable) {
			errors = append(errors, validation(codeExecutableNotFound, path+".executable"))
		}
		if action.WorkingDir != "" {
			if !filepath.IsAbs(action.WorkingDir) {
				errors = append(errors, validation(codeValueOutOfRange, path+".working_dir"))
			} else if info, err := os.Stat(action.WorkingDir); err != nil || !info.IsDir() {
				errors = append(errors, validation(codeValueOutOfRange, path+".working_dir"))
			}
		}
		for key := range action.Env {
			if !reEnvKey.MatchString(key) {
				errors = append(errors, validation(codeInvalidIdentifier, path+".env"))
			}
		}
	case "process.stop":
		if action.ProcessID == "" {
			errors = append(errors, validation(codeMissingRequiredField, path+".process_id"))
		}
		if action.GraceSeconds != 0 && outOfRange(action.GraceSeconds, constants.MinGraceSeconds, constants.MaxGraceSeconds) {
			errors = append(errors, validation(codeValueOutOfRange, path+".grace_seconds"))
		}
	case "command.run":
		if !executable(action.Executable) {
			errors = append(errors, validation(codeExecutableNotFound, path+".executable"))
		}
		if action.Shell && !allowShell {
			errors = append(errors, validation(codeShellNotAllowed, path+".shell"))
		}
		if action.TimeoutSeconds != 0 && outOfRange(action.TimeoutSeconds, constants.MinActionTimeoutSeconds, constants.MaxActionTimeoutSeconds) {
			errors = append(errors, validation(codeValueOutOfRange, path+".timeout_seconds"))
		}
	case "notify":
		if action.Title == "" || action.Message == "" {
			errors = append(errors, validation(codeMissingRequiredField, path))
		}
		if utf8.RuneCountInString(action.Title) > constants.NotifyTitleMaxRunes || utf8.RuneCountInString(action.Message) > constants.NotifyMessageMaxRunes || !oneOf(action.Level, "", "info", "warn", "error") {
			errors = append(errors, validation(codeValueOutOfRange, path))
		}
	default:
		errors = append(errors, validation(codeUnknownActionType, path+".type"))
	}
	return errors
}

func recordAppConflict(conflicts map[string]string, priority int, verb, bundleID, path string, errors *[]ValidationError) {
	key := strconvItoa(priority) + ":app:" + bundleID
	if previous := conflicts[key]; previous != "" && previous != verb {
		*errors = append(*errors, validation(codeRuleConflictSamePriority, path))
	}
	conflicts[key] = verb
}

func validateAppStopIDs(action *Action, path string) ([]string, []ValidationError) {
	raw := make([]string, 0, 1+len(action.BundleIDs))
	if action.BundleID != "" {
		raw = append(raw, action.BundleID)
	}
	raw = append(raw, action.BundleIDs...)
	if len(raw) == 0 {
		field := path + ".bundle_id"
		if action.BundleIDs != nil {
			field = path + ".bundle_ids"
		}
		return nil, []ValidationError{validation(codeMissingRequiredField, field)}
	}
	if len(raw) > constants.MaxActionsPerRule {
		return nil, []ValidationError{validation(codeValueOutOfRange, path+".bundle_ids")}
	}
	seen := map[string]struct{}{}
	ids := make([]string, 0, len(raw))
	var errors []ValidationError
	if action.BundleID != "" && !reBundleID.MatchString(action.BundleID) {
		errors = append(errors, validation(codeInvalidIdentifier, path+".bundle_id"))
	}
	for i, id := range action.BundleIDs {
		if !reBundleID.MatchString(id) {
			errors = append(errors, validation(codeInvalidIdentifier, path+".bundle_ids["+strconvItoa(i)+"]"))
		}
	}
	if len(errors) > 0 {
		return nil, errors
	}
	for _, id := range raw {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

func validateBrowserDomains(domains []string, path string) ([]string, []ValidationError) {
	if len(domains) == 0 {
		return nil, []ValidationError{validation(codeBrowserDomainsRequired, path)}
	}
	if len(domains) > constants.BrowserPolicyMaxDomains {
		return nil, []ValidationError{validation(codeBrowserDomainsLimit, path)}
	}

	normalized := make([]string, len(domains))
	var errors []ValidationError
	for i, domain := range domains {
		value := strings.TrimSuffix(strings.ToLower(domain), ".")
		if !validBrowserDomain(value) {
			errors = append(errors, validation(codeInvalidBrowserDomain, path+"["+strconvItoa(i)+"]"))
			continue
		}
		normalized[i] = value
	}
	return normalized, errors
}

func validBrowserDomain(domain string) bool {
	if domain == "" || len(domain) > constants.BrowserDomainMaxBytes || !isASCII(domain) || net.ParseIP(domain) != nil {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
				return false
			}
		}
	}
	return true
}

func isASCII(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] > 0x7f {
			return false
		}
	}
	return true
}
func executable(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}
func oneOf(value string, allowed ...string) bool {
	for _, v := range allowed {
		if value == v {
			return true
		}
	}
	return false
}
func strconvItoa(value int) string {
	return strings.TrimPrefix(strings.TrimSpace(fmt.Sprintf("%d", value)), "+")
}

func outOfRange(value, min, max int) bool {
	return value < min || value > max
}
