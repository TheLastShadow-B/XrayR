package sspanel

import (
	"strings"
	"testing"
)

// Gecko is Salamander's PSK obfuscation plus randomised re-framing of QUIC
// long-header (handshake) packets. xray-core exposes both through the same
// "salamander" mask and picks Gecko as soon as packetSize is present, so the
// panel has to be able to say which one it means and, for Gecko, how big the
// fragments should be.

func TestParseHysteria2NodeInfo_Gecko(t *testing.T) {
	c := newHy2APIClient(t)

	nodeInfo, err := c.ParseSSPanelNodeInfo(&NodeInfoResponse{
		Sort: 15,
		CustomConfig: newCustomConfigRaw(t, map[string]any{
			"offset_port_node": "443",
			"Hy2Opts": map[string]any{
				"obfs":                 "gecko",
				"obfs_password":        "s3cret",
				"obfs_min_packet_size": 600,
				"obfs_max_packet_size": 1300,
			},
		}),
	})
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if nodeInfo.Obfs != "gecko" {
		t.Errorf("Obfs = %q, want \"gecko\"", nodeInfo.Obfs)
	}
	if nodeInfo.ObfsPassword != "s3cret" {
		t.Errorf("ObfsPassword = %q", nodeInfo.ObfsPassword)
	}
	if nodeInfo.ObfsMinPacketSize != 600 || nodeInfo.ObfsMaxPacketSize != 1300 {
		t.Errorf("packet size range = %d-%d, want 600-1300",
			nodeInfo.ObfsMinPacketSize, nodeInfo.ObfsMaxPacketSize)
	}
}

// Omitting the sizes is the common case for an operator who just wants Gecko
// on; fall back to a range that stays clear of the path MTU rather than
// erroring or handing xray-core a zero range (which would silently degrade to
// plain Salamander and leave Gecko-configured clients unable to connect).
func TestParseHysteria2NodeInfo_GeckoDefaultsPacketSize(t *testing.T) {
	c := newHy2APIClient(t)

	nodeInfo, err := c.ParseSSPanelNodeInfo(&NodeInfoResponse{
		Sort: 15,
		CustomConfig: newCustomConfigRaw(t, map[string]any{
			"offset_port_node": "443",
			"Hy2Opts": map[string]any{
				"obfs":          "gecko",
				"obfs_password": "s3cret",
			},
		}),
	})
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if nodeInfo.ObfsMinPacketSize != geckoDefaultMinPacketSize ||
		nodeInfo.ObfsMaxPacketSize != geckoDefaultMaxPacketSize {
		t.Errorf("defaults = %d-%d, want %d-%d",
			nodeInfo.ObfsMinPacketSize, nodeInfo.ObfsMaxPacketSize,
			geckoDefaultMinPacketSize, geckoDefaultMaxPacketSize)
	}
}

func TestParseHysteria2NodeInfo_GeckoMissingPassword(t *testing.T) {
	c := newHy2APIClient(t)

	_, err := c.ParseSSPanelNodeInfo(&NodeInfoResponse{
		Sort: 15,
		CustomConfig: newCustomConfigRaw(t, map[string]any{
			"offset_port_node": "443",
			"Hy2Opts":          map[string]any{"obfs": "gecko"},
		}),
	})
	if err == nil {
		t.Fatal("expected an error when obfs=gecko has no obfs_password")
	}
	if !strings.Contains(err.Error(), "obfs_password") || !strings.Contains(err.Error(), "gecko") {
		t.Errorf("error must name both obfs_password and gecko: %q", err)
	}
}

// xray-core rejects a range whose upper bound exceeds 2048
// (infra/conf/transport_finalmask.go). Catching it here turns a node that
// fails to build into a readable panel-side config error.
func TestParseHysteria2NodeInfo_GeckoPacketSizeOutOfRange(t *testing.T) {
	for name, sizes := range map[string][2]int{
		"max above xray-core's 2048 cap": {600, 4096},
		"min greater than max":           {1400, 600},
		"zero min with non-zero max":     {0, 1300},
		"negative min":                   {-1, 1300},
	} {
		t.Run(name, func(t *testing.T) {
			c := newHy2APIClient(t)
			_, err := c.ParseSSPanelNodeInfo(&NodeInfoResponse{
				Sort: 15,
				CustomConfig: newCustomConfigRaw(t, map[string]any{
					"offset_port_node": "443",
					"Hy2Opts": map[string]any{
						"obfs":                 "gecko",
						"obfs_password":        "s3cret",
						"obfs_min_packet_size": sizes[0],
						"obfs_max_packet_size": sizes[1],
					},
				}),
			})
			if err == nil {
				t.Fatalf("expected an error for packet size range %d-%d", sizes[0], sizes[1])
			}
		})
	}
}

// Salamander must keep working exactly as before, and must not pick up a
// packet size — that is what would silently turn it into Gecko and break
// every Salamander client.
func TestParseHysteria2NodeInfo_SalamanderHasNoPacketSize(t *testing.T) {
	c := newHy2APIClient(t)

	nodeInfo, err := c.ParseSSPanelNodeInfo(&NodeInfoResponse{
		Sort: 15,
		CustomConfig: newCustomConfigRaw(t, map[string]any{
			"offset_port_node": "443",
			"Hy2Opts": map[string]any{
				"obfs":                 "salamander",
				"obfs_password":        "s3cret",
				"obfs_min_packet_size": 600,
				"obfs_max_packet_size": 1300,
			},
		}),
	})
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if nodeInfo.ObfsMinPacketSize != 0 || nodeInfo.ObfsMaxPacketSize != 0 {
		t.Errorf("salamander must not carry a packet size, got %d-%d",
			nodeInfo.ObfsMinPacketSize, nodeInfo.ObfsMaxPacketSize)
	}
}

// An unrecognised obfs used to be ignored in silence, which is the worst
// possible outcome: the inbound comes up with no obfuscation while every
// client is configured to use some, so nothing connects and nothing is logged.
func TestParseHysteria2NodeInfo_UnknownObfsRejected(t *testing.T) {
	c := newHy2APIClient(t)

	_, err := c.ParseSSPanelNodeInfo(&NodeInfoResponse{
		Sort: 15,
		CustomConfig: newCustomConfigRaw(t, map[string]any{
			"offset_port_node": "443",
			"Hy2Opts": map[string]any{
				"obfs":          "salamandar", // typo
				"obfs_password": "s3cret",
			},
		}),
	})
	if err == nil {
		t.Fatal("expected an error for an unknown obfs type")
	}
	if !strings.Contains(err.Error(), "salamandar") {
		t.Errorf("error should quote the offending value: %q", err)
	}
}
