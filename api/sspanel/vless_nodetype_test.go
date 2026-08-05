package sspanel_test

import (
	"encoding/json"
	"testing"

	"github.com/XrayR-project/XrayR/api"
	"github.com/XrayR-project/XrayR/api/sspanel"
)

// parseWithNodeType drives ParseSSPanelNodeInfo directly so the switch on
// c.NodeType can be exercised without a live panel.
func parseWithNodeType(t *testing.T, nodeType string, customConfig string) *api.NodeInfo {
	t.Helper()

	client := sspanel.New(&api.Config{
		APIHost:  "http://127.0.0.1:667",
		Key:      "123",
		NodeID:   1,
		NodeType: nodeType,
	})

	nodeInfo, err := client.ParseSSPanelNodeInfo(&sspanel.NodeInfoResponse{
		Sort:            12,
		RawServerString: "example.com",
		CustomConfig:    json.RawMessage(customConfig),
	})
	if err != nil {
		t.Fatalf("NodeType=%s: %v", nodeType, err)
	}

	return nodeInfo
}

// NodeType: Vless must enable VLESS on its own. Previously the switch had no
// "Vless" arm, so it fell through leaving TransportProtocol empty and
// EnableVless false — a silently broken inbound.
func TestParseNodeTypeVlessEnablesVless(t *testing.T) {
	nodeInfo := parseWithNodeType(t, "Vless", `{
		"offset_port_node": "443",
		"network": "tcp",
		"security": "reality",
		"flow": "xtls-rprx-vision"
	}`)

	if !nodeInfo.EnableVless {
		t.Error("EnableVless should be true for NodeType=Vless")
	}
	if nodeInfo.TransportProtocol != "tcp" {
		t.Errorf("TransportProtocol = %q, want \"tcp\"", nodeInfo.TransportProtocol)
	}
	if nodeInfo.VlessFlow != "xtls-rprx-vision" {
		t.Errorf("VlessFlow = %q, want \"xtls-rprx-vision\"", nodeInfo.VlessFlow)
	}
}

// NodeType: Vless must not need custom_config.enable_vless — that flag exists
// so a V2ray-typed node can opt in, not as a second switch for an explicit type.
func TestParseNodeTypeVlessIgnoresEnableVlessFlag(t *testing.T) {
	nodeInfo := parseWithNodeType(t, "Vless", `{
		"offset_port_node": "443",
		"network": "tcp",
		"enable_vless": "0"
	}`)

	if !nodeInfo.EnableVless {
		t.Error("NodeType=Vless must win over enable_vless=\"0\"")
	}
}

// The V2ray arm must keep its existing behaviour: VLESS only when the panel
// says so, via the string "1".
func TestParseNodeTypeV2rayStillHonoursEnableVlessFlag(t *testing.T) {
	on := parseWithNodeType(t, "V2ray", `{"offset_port_node":"443","network":"ws","enable_vless":"1"}`)
	if !on.EnableVless {
		t.Error("V2ray + enable_vless=\"1\" should enable VLESS")
	}

	off := parseWithNodeType(t, "V2ray", `{"offset_port_node":"443","network":"ws"}`)
	if off.EnableVless {
		t.Error("V2ray without enable_vless should stay VMess")
	}
}

// TLS handling must be identical across the two arms.
func TestParseNodeTypeVlessTLS(t *testing.T) {
	tls := parseWithNodeType(t, "Vless", `{"offset_port_node":"443","network":"tcp","security":"tls"}`)
	if !tls.EnableTLS {
		t.Error("security=tls should set EnableTLS for NodeType=Vless")
	}

	reality := parseWithNodeType(t, "Vless", `{"offset_port_node":"443","network":"tcp","security":"reality"}`)
	if reality.EnableTLS {
		t.Error("security=reality must not set EnableTLS — REALITY replaces the TLS layer")
	}
}
