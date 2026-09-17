package sspanel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/XrayR-project/XrayR/api"
	"github.com/XrayR-project/XrayR/common/tcpprobe"
)

func TestTCPProbeAPIContract(t *testing.T) {
	var posted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mod_mu/nodes/7/tcp-probe" || r.URL.Query().Get("key") != "test-key" {
			t.Error("wrong route or credentials")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" {
			fmt.Fprintf(w, `{"ret":1,"data":{"enabled":true,"config_hash":"%s","targets":[{"id":1,"carrier":"telecom","label":"test","ip":"1.1.1.1","port":443}]}}`, strings.Repeat("a", 64))
			return
		}
		var report tcpprobe.Report
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil || report.MeasuredAt != 123 || len(report.Results) != 1 {
			t.Error("wrong report body")
		}
		posted = true
		fmt.Fprint(w, `{"ret":1}`)
	}))
	defer server.Close()
	c := New(&api.Config{APIHost: server.URL, Key: "test-key", NodeID: 7})
	config, err := c.GetTCPProbeConfig(context.Background())
	if err != nil || !config.Enabled || config.Targets[0].ID != 1 {
		t.Fatalf("%+v %v", config, err)
	}
	err = c.ReportTCPProbe(context.Background(), tcpprobe.Report{Hash: config.Hash, MeasuredAt: 123, Results: []tcpprobe.Result{{TargetID: 1}}})
	if err != nil || !posted {
		t.Fatal(err)
	}
}

func TestTCPProbeOldPanelAndRejectedReports(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(422)
	}))
	defer server.Close()
	c := New(&api.Config{APIHost: server.URL, Key: "secret", NodeID: 7})
	config, err := c.GetTCPProbeConfig(context.Background())
	if err != nil || config.Enabled {
		t.Fatal("old panel must silently disable probe")
	}
	err = c.ReportTCPProbe(context.Background(), tcpprobe.Report{})
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("rejected report not handled safely")
	}
}
