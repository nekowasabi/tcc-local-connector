// Package constants contains the configuration values shared by the backend.
package constants

const (
	ConfigSchemaVersion = 2
	ConfigRelPath       = ".config/tcc-local-connector/config.yml"
	StateDirRelPath     = ".local/state/tcc-local-connector"
	PauseFileName       = "pause.json"
	LedgerFileName      = "managed-processes.json"
	LogFileName         = "backend.log"

	StateFileMode           = 0o600
	ConfigForbiddenModeMask = 0o022
	ConfigStrictModeMask    = 0o077

	MinPollIntervalSeconds        = 10
	DefaultPollIntervalSeconds    = 60
	MaxPollIntervalSeconds        = 3600
	MinPollTimeoutSeconds         = 1
	DefaultPollTimeoutSeconds     = 20
	MaxPollTimeoutSeconds         = 120
	MinFailureGraceSeconds        = 0
	DefaultFailureGraceSeconds    = 180
	MaxFailureGraceSeconds        = 3600
	WakeReevaluateThresholdFactor = 2

	UserInfoTTLSeconds   = 21600
	TaskChuteQueryDays   = 2
	MaxRunningTasks      = 32
	MaxTaskNameBytes     = 512
	MaxErrorMessageBytes = 200
	MaxRules             = 100
	MaxActionsPerRule    = 20

	MinActionTimeoutSeconds        = 1
	DefaultActionTimeoutSeconds    = 30
	MaxActionTimeoutSeconds        = 300
	MinGraceSeconds                = 1
	DefaultAppStopGraceSeconds     = 10
	DefaultProcessStopGraceSeconds = 10
	MaxGraceSeconds                = 120
	CommandRunMaxOutputBytes       = 65536
	NotifyTitleMaxRunes            = 200
	NotifyMessageMaxRunes          = 500

	PauseStateVersion       = 1
	LedgerVersion           = 1
	PauseMinSeconds         = 60
	PauseMaxSeconds         = 86400
	PausePresetShortSeconds = 900
	PausePresetLongSeconds  = 3600
	PauseNextDayStartHour   = 5
	MinLogRetainDays        = 1
	DefaultLogRetainDays    = 14
	MaxLogRetainDays        = 365
	DefaultLogLevel         = "info"
	LogRedactBearerPrefix   = "Bearer "
	LogRedactTokenExpires   = "Token expires"
	MCPWaitDelaySeconds     = 2
	MCPStderrLimitBytes     = 4096
	MCPMaxResponseBytes     = 1048576

	InProgressLinePrefix = "- [In Progress] "
	IDBlockDelimiter     = " [ID: "
	DateHeaderPattern    = `^##\s+.*?(\d{4}-\d{2}-\d{2})\s*$`
	MetaHeadPattern      = `^\d{2}:\d{2}(,|$)`
	TaskIDPattern        = `^task_[0-9a-f]{32}$`
	UserFieldPattern     = `^- \*\*([^*]+):\*\* (.*)$`
	RuleIDPattern        = `^[a-z0-9][a-z0-9-]{0,63}$`
	ProcessIDPattern     = `^[a-z0-9][a-z0-9-]{0,63}$`
	BundleIDPattern      = `^[A-Za-z0-9][A-Za-z0-9._-]*$`
	EnvKeyPattern        = `^[A-Za-z_][A-Za-z0-9_]*$`
)
