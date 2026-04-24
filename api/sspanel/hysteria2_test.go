package sspanel

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/XrayR-project/XrayR/api"
)

// newHy2APIClient returns a minimal APIClient configured for Hysteria2
// NodeType dispatch. It does not make network calls; tests feed
// NodeInfoResponse directly to ParseSSPanelNodeInfo.
func newHy2APIClient(t *testing.T) *APIClient {
	t.Helper()
	return New(&api.Config{
		APIHost:  "http://127.0.0.1:0",
		Key:      "test",
		NodeID:   1,
		NodeType: "Hysteria2",
	})
}

func newCustomConfigRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal custom_config: %v", err)
	}
	return b
}

// ---------- Happy path: full Hy2Opts fixture ----------

func TestParseHysteria2NodeInfo_FullFixture(t *testing.T) {
	c := newHy2APIClient(t)

	custom := map[string]any{
		"offset_port_node": "443",
		"offset_port_user": "443",
		"host":             "example.com",
		"Hy2Opts": map[string]any{
			"up_mbps":       500,
			"down_mbps":     1000,
			"obfs":          "salamander",
			"obfs_password": "node_obfs_secret",
			"masquerade": map[string]any{
				"type":         "url",
				"url":          "https://example.com/",
				"rewrite_host": true,
				"insecure":     false,
				"status_code":  404,
			},
		},
	}
	resp := &NodeInfoResponse{
		Sort:         15,
		CustomConfig: newCustomConfigRaw(t, custom),
	}

	nodeInfo, err := c.ParseSSPanelNodeInfo(resp)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if nodeInfo.NodeType != "Hysteria2" {
		t.Errorf("NodeType: got %q want Hysteria2", nodeInfo.NodeType)
	}
	if nodeInfo.Port != 443 {
		t.Errorf("Port: got %d want 443", nodeInfo.Port)
	}
	if nodeInfo.UpMbps != 500 {
		t.Errorf("UpMbps: got %d want 500", nodeInfo.UpMbps)
	}
	if nodeInfo.DownMbps != 1000 {
		t.Errorf("DownMbps: got %d want 1000", nodeInfo.DownMbps)
	}
	if nodeInfo.Obfs != "salamander" {
		t.Errorf("Obfs: got %q want salamander", nodeInfo.Obfs)
	}
	if nodeInfo.ObfsPassword != "node_obfs_secret" {
		t.Errorf("ObfsPassword: got %q want node_obfs_secret", nodeInfo.ObfsPassword)
	}
	if nodeInfo.Host != "example.com" {
		t.Errorf("Host: got %q want example.com", nodeInfo.Host)
	}
	if !nodeInfo.EnableTLS {
		t.Errorf("EnableTLS: got false, Hy2 must always be true")
	}
	if nodeInfo.Hy2Masquerade == nil {
		t.Fatal("Hy2Masquerade: nil, want non-nil")
	}
	if nodeInfo.Hy2Masquerade.Type != "url" {
		t.Errorf("Hy2Masquerade.Type: got %q want url", nodeInfo.Hy2Masquerade.Type)
	}
	if nodeInfo.Hy2Masquerade.URL != "https://example.com/" {
		t.Errorf("Hy2Masquerade.URL: got %q", nodeInfo.Hy2Masquerade.URL)
	}
	if !nodeInfo.Hy2Masquerade.RewriteHost {
		t.Error("Hy2Masquerade.RewriteHost: got false want true")
	}
	if nodeInfo.Hy2Masquerade.StatusCode != 404 {
		t.Errorf("Hy2Masquerade.StatusCode: got %d want 404", nodeInfo.Hy2Masquerade.StatusCode)
	}
}

// ---------- Happy path: minimal fixture, Hy2Opts absent ----------

func TestParseHysteria2NodeInfo_MinimalFixture(t *testing.T) {
	c := newHy2APIClient(t)

	custom := map[string]any{
		"offset_port_node": "443",
	}
	resp := &NodeInfoResponse{
		Sort:         15,
		CustomConfig: newCustomConfigRaw(t, custom),
	}

	nodeInfo, err := c.ParseSSPanelNodeInfo(resp)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if nodeInfo.Port != 443 {
		t.Errorf("Port: got %d want 443", nodeInfo.Port)
	}
	if nodeInfo.UpMbps != 0 || nodeInfo.DownMbps != 0 {
		t.Errorf("bandwidth: got up=%d down=%d, want 0/0", nodeInfo.UpMbps, nodeInfo.DownMbps)
	}
	if nodeInfo.Obfs != "" || nodeInfo.ObfsPassword != "" {
		t.Errorf("obfs: got %q/%q, want empty/empty", nodeInfo.Obfs, nodeInfo.ObfsPassword)
	}
	if nodeInfo.Hy2Masquerade != nil {
		t.Errorf("Hy2Masquerade: got non-nil %+v, want nil", nodeInfo.Hy2Masquerade)
	}
	if !nodeInfo.EnableTLS {
		t.Error("EnableTLS: got false, Hy2 must always be true")
	}
	if nodeInfo.Host != "" {
		t.Errorf("Host: got %q, want empty (fallback signal for inbound builder)", nodeInfo.Host)
	}
}

// ---------- Happy path: obfs explicitly empty ----------

func TestParseHysteria2NodeInfo_NoObfs(t *testing.T) {
	c := newHy2APIClient(t)

	custom := map[string]any{
		"offset_port_node": "443",
		"Hy2Opts": map[string]any{
			"up_mbps":   100,
			"down_mbps": 200,
			"obfs":      "",
		},
	}
	resp := &NodeInfoResponse{
		Sort:         15,
		CustomConfig: newCustomConfigRaw(t, custom),
	}

	nodeInfo, err := c.ParseSSPanelNodeInfo(resp)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if nodeInfo.Obfs != "" {
		t.Errorf("Obfs: got %q, want empty", nodeInfo.Obfs)
	}
}

// ---------- Edge case: salamander obfs without password ----------

func TestParseHysteria2NodeInfo_ObfsMissingPassword(t *testing.T) {
	c := newHy2APIClient(t)

	custom := map[string]any{
		"offset_port_node": "443",
		"Hy2Opts": map[string]any{
			"obfs": "salamander",
			// obfs_password intentionally missing
		},
	}
	resp := &NodeInfoResponse{
		Sort:         15,
		CustomConfig: newCustomConfigRaw(t, custom),
	}

	_, err := c.ParseSSPanelNodeInfo(resp)
	if err == nil {
		t.Fatal("expected error when obfs=salamander but obfs_password missing")
	}
	msg := err.Error()
	if !strings.Contains(msg, "obfs_password") || !strings.Contains(msg, "salamander") {
		t.Errorf("error message must mention both 'obfs_password' and 'salamander': %q", msg)
	}
}

// ---------- Edge case: invalid masquerade type ----------

func TestParseHysteria2NodeInfo_InvalidMasqueradeType(t *testing.T) {
	c := newHy2APIClient(t)

	custom := map[string]any{
		"offset_port_node": "443",
		"Hy2Opts": map[string]any{
			"masquerade": map[string]any{
				"type": "invalid_type_xyz",
			},
		},
	}
	resp := &NodeInfoResponse{
		Sort:         15,
		CustomConfig: newCustomConfigRaw(t, custom),
	}

	_, err := c.ParseSSPanelNodeInfo(resp)
	if err == nil {
		t.Fatal("expected error for invalid masquerade.type")
	}
	msg := err.Error()
	if !strings.Contains(msg, "masquerade") {
		t.Errorf("error message should mention 'masquerade': %q", msg)
	}
}

// ---------- Regression: non-Hy2 node does not pick up Hy2 fields ----------

func TestParseSSPanelNodeInfo_V2rayDoesNotLeakHy2Fields(t *testing.T) {
	// Use a V2ray-configured APIClient; custom_config stray obfs_password
	// value must not propagate into NodeInfo.Hy2* fields.
	c := New(&api.Config{
		APIHost:  "http://127.0.0.1:0",
		Key:      "test",
		NodeID:   1,
		NodeType: "V2ray",
	})

	custom := map[string]any{
		"offset_port_node": "443",
		"host":             "vm.example.com",
		"network":          "ws",
		"security":         "tls",
		"enable_vless":     "0",
		// stray Hy2-looking field; should not surface in NodeInfo
		"obfs_password": "shouldNotLeak",
	}
	resp := &NodeInfoResponse{
		Sort:         11,
		CustomConfig: newCustomConfigRaw(t, custom),
	}

	nodeInfo, err := c.ParseSSPanelNodeInfo(resp)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if nodeInfo.ObfsPassword != "" {
		t.Errorf("V2ray NodeInfo must not expose ObfsPassword, got %q", nodeInfo.ObfsPassword)
	}
	if nodeInfo.UpMbps != 0 || nodeInfo.DownMbps != 0 {
		t.Errorf("V2ray NodeInfo must not populate Hy2 bandwidth fields, got %d/%d", nodeInfo.UpMbps, nodeInfo.DownMbps)
	}
	if nodeInfo.Hy2Masquerade != nil {
		t.Errorf("V2ray NodeInfo must not populate Hy2Masquerade, got %+v", nodeInfo.Hy2Masquerade)
	}
}

// ---------- obfs_password redaction in error path ----------

func TestObfsPasswordRedaction(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "simple",
			in:   `{"obfs_password":"super_secret"}`,
			want: `{"obfs_password":"[REDACTED]"}`,
		},
		{
			name: "nested in Hy2Opts",
			in:   `{"host":"a.com","Hy2Opts":{"obfs_password":"leakme","up_mbps":500}}`,
			want: `{"host":"a.com","Hy2Opts":{"obfs_password":"[REDACTED]","up_mbps":500}}`,
		},
		{
			name: "spaces around colon",
			in:   `{"obfs_password" : "abc"}`,
			want: `{"obfs_password":"[REDACTED]"}`,
		},
		{
			name: "no obfs_password present",
			in:   `{"host":"a.com","up_mbps":500}`,
			want: `{"host":"a.com","up_mbps":500}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(obfsPasswordRedactRe.ReplaceAll([]byte(tc.in), []byte(`"obfs_password":"[REDACTED]"`)))
			if got != tc.want {
				t.Errorf("redact mismatch:\n got:  %s\n want: %s", got, tc.want)
			}
			if strings.Contains(got, "super_secret") || strings.Contains(got, "leakme") || strings.Contains(got, `"abc"`) {
				t.Errorf("original password leaked: %s", got)
			}
		})
	}
}
