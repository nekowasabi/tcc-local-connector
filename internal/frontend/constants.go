package frontend

const (
	SupportedProtocolVersion   = 1
	MenuRefreshIntervalSeconds = 5
	PauseNextDayStartHour      = 5
	PausePresetShortSeconds    = 900
	PausePresetLongSeconds     = 3600
	FrontendMaxLineBytes       = 262144
)

var RequiredCapabilities = []string{
	"status",
	"reload_config",
	"pause",
	"resume",
	"refresh_now",
	"config_paths",
	"report_actions",
}
