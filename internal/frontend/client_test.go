package frontend

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAcceptReadySuccessAndFailures(t *testing.T) {
	client := NewClient()
	client.AcceptReady(SupportedProtocolVersion, RequiredCapabilities)
	if client.State() != StateRunning {
		t.Fatalf("state = %q", client.State())
	}

	client.AcceptReady(SupportedProtocolVersion+1, RequiredCapabilities)
	if client.State() != StateBackendIncompatible {
		t.Fatalf("version mismatch state = %q", client.State())
	}

	client.AcceptReady(SupportedProtocolVersion, []string{"status"})
	if client.State() != StateBackendIncompatible {
		t.Fatalf("capability mismatch state = %q", client.State())
	}
}

func TestConsumeStdoutDecodesPlanAndNotify(t *testing.T) {
	client := NewClient()
	plan := `{"version":1,"event":"event.plan","data":{"cycle_id":7,"dry_run":true,"actions":[],"enforce_stop_bundle_ids":["com.example.App"]}}` + "\n"
	client.ConsumeStdout([]byte(plan[:17]))
	client.ConsumeStdout([]byte(plan[17:]))
	notify := `{"version":1,"event":"event.notify","data":{"level":"warn","code":"action_refused","title":"x","message":"y"}}` + "\n"
	client.ConsumeStdout([]byte(notify))
	plans := client.TakePlans()
	notifications := client.TakeNotifications()
	if len(plans) != 1 || plans[0].CycleID != 7 || !plans[0].DryRun || len(plans[0].EnforceStopBundleIDs) != 1 {
		t.Fatalf("plans = %#v", plans)
	}
	if len(notifications) != 1 || notifications[0].Code != "action_refused" {
		t.Fatalf("notifications = %#v", notifications)
	}
}

func TestReadyEventSetsRunning(t *testing.T) {
	client := NewClient()
	payload := `{"version":1,"event":"ready","data":{"protocol_version":1,"capabilities":[{"name":"status"},{"name":"reload_config"},{"name":"pause"},{"name":"resume"},{"name":"refresh_now"},{"name":"config_paths"},{"name":"report_actions"}]}}` + "\n"
	client.ConsumeStdout([]byte(payload))
	if client.State() != StateRunning {
		t.Fatalf("state = %q", client.State())
	}
}

func TestPauseParamsEncodeIntegerSeconds(t *testing.T) {
	request := Request{Version: 1, ID: "pause-1", Method: "pause", Params: map[string]any{"duration_seconds": 900}}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"duration_seconds":900`) {
		t.Fatalf("encoded pause = %s", body)
	}
}
