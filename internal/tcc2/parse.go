package tcc2

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/takets/tcc-local-connector/internal/constants"
)

type RunningTask struct {
	Name     string   `json:"name"`
	TaskID   string   `json:"task_id"`
	Date     string   `json:"date"`
	Start    string   `json:"start"`
	Meta     string   `json:"meta"`
	Section  string   `json:"section,omitempty"`
	Project  string   `json:"project,omitempty"`
	Mode     string   `json:"mode,omitempty"`
	Routine  string   `json:"routine,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}
type TaskChuteResult struct {
	RunningTasks []RunningTask  `json:"running_tasks"`
	StatusCounts map[string]int `json:"status_counts"`
	Warnings     []string       `json:"warnings,omitempty"`
}
type ParseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *ParseError) Error() string { return e.Code + ": " + e.Message }

var dateHeaderRE = regexp.MustCompile(constants.DateHeaderPattern)
var taskIDRE = regexp.MustCompile(constants.TaskIDPattern)
var metaHeadRE = regexp.MustCompile(constants.MetaHeadPattern)
var userFieldRE = regexp.MustCompile(constants.UserFieldPattern)

// ParseTaskChuteText deliberately distinguishes an empty task list from an
// unrecognised response, so callers never release controls on format drift.
func ParseTaskChuteText(text string, isError bool) (TaskChuteResult, error) {
	if isError {
		return TaskChuteResult{}, parseError("tcc2_api_error")
	}
	if text == "" {
		return TaskChuteResult{}, parseError("empty_content")
	}
	result := TaskChuteResult{StatusCounts: map[string]int{}}
	var currentDate string
	var taskLines int
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if matches := dateHeaderRE.FindStringSubmatch(line); len(matches) == 2 {
			currentDate = matches[1]
			continue
		}
		if !strings.HasPrefix(line, "- [") {
			continue
		}
		taskLines++
		if !strings.HasPrefix(line, constants.InProgressLinePrefix) {
			if end := strings.Index(line, "]"); end > 3 {
				result.StatusCounts[line[3:end]]++
			}
			continue
		}
		result.StatusCounts["In Progress"]++
		task := parseRunningTask(line[len(constants.InProgressLinePrefix):], currentDate)
		result.RunningTasks = append(result.RunningTasks, task)
		if len(result.RunningTasks) > constants.MaxRunningTasks {
			return TaskChuteResult{}, parseError("too_many_running_tasks")
		}
	}
	if taskLines == 0 {
		return TaskChuteResult{}, parseError("unrecognized_format")
	}
	return result, nil
}
func parseError(code string) *ParseError {
	message := code
	if len(message) > constants.MaxErrorMessageBytes {
		message = message[:constants.MaxErrorMessageBytes]
	}
	return &ParseError{Code: code, Message: message}
}
func parseRunningTask(rest, date string) RunningTask {
	task := RunningTask{Date: date}
	rest, task.TaskID, task.Section, task.Project, task.Mode, task.Routine, task.Warnings = splitIDBlock(rest)
	rest, task.Start, task.Meta = splitMetaGroup(rest)
	task.Name = trimTaskName(rest)
	if task.Name == "" {
		task.Warnings = append(task.Warnings, "empty_name")
	}
	return task
}
func splitIDBlock(rest string) (string, string, string, string, string, string, []string) {
	var warnings []string
	if !strings.HasSuffix(rest, "]") {
		return rest, "", "", "", "", "", warnings
	}
	index := strings.LastIndex(rest, constants.IDBlockDelimiter)
	if index < 0 {
		return rest, "", "", "", "", "", warnings
	}
	parts := strings.Split(rest[index+len(constants.IDBlockDelimiter):len(rest)-1], ", ")
	if len(parts) == 0 || !taskIDRE.MatchString(parts[0]) {
		warnings = append(warnings, "task_id_format")
	}
	var id, section, project, mode, routine string
	if len(parts) > 0 && taskIDRE.MatchString(parts[0]) {
		id = parts[0]
	}
	for _, part := range parts[1:] {
		key, value, ok := strings.Cut(part, ": ")
		if !ok {
			warnings = append(warnings, "unknown_id_key:"+part)
			continue
		}
		switch key {
		case "Section":
			section = value
		case "Project":
			project = value
		case "Mode":
			mode = value
		case "Routine":
			routine = value
		case "Tags":
		default:
			warnings = append(warnings, "unknown_id_key:"+key)
		}
	}
	return rest[:index], id, section, project, mode, routine, warnings
}
func splitMetaGroup(rest string) (string, string, string) {
	if !strings.HasSuffix(rest, ")") {
		return rest, "", ""
	}
	index := strings.LastIndex(rest, " (")
	if index < 0 {
		return rest, "", ""
	}
	meta := rest[index+2 : len(rest)-1]
	if !metaHeadRE.MatchString(meta) {
		return rest, "", ""
	}
	start := ""
	if len(meta) >= 5 {
		start = meta[:5]
	}
	return rest[:index], start, meta
}
func trimTaskName(value string) string {
	value = strings.TrimRight(value, " ")
	if len(value) <= constants.MaxTaskNameBytes {
		return value
	}
	cut := constants.MaxTaskNameBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}

type UserInfo struct {
	Timezone      string `json:"timezone"`
	StartOfDay    string `json:"start_of_day"`
	DefaultViewID string `json:"default_view_id"`
}

func parseUserText(text string) UserInfo {
	var info UserInfo
	for _, line := range strings.Split(text, "\n") {
		match := userFieldRE.FindStringSubmatch(line)
		if len(match) != 3 {
			continue
		}
		switch match[1] {
		case "Timezone":
			info.Timezone = match[2]
		case "Start of Day":
			info.StartOfDay = match[2]
		case "Default View ID":
			info.DefaultViewID = match[2]
		}
	}
	return info
}
