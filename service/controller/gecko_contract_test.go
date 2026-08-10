package controller_test

import (
	"encoding/json"
	"testing"

	"github.com/xtls/xray-core/infra/conf"
	"github.com/xtls/xray-core/transport/internet/finalmask/salamander"
)

// The Gecko support in InboundBuilder rests on one non-obvious xray-core
// behaviour: there is no "gecko" mask type. Both obfuscators are configured
// through the "salamander" mask, and Salamander.Build() returns a GeckoConfig
// instead of a plain Config purely because packetSize is set.
//
// That is an assumption about a pinned upstream, invisible from XrayR's own
// code, and it silently degrades rather than failing if it ever changes: a
// Gecko node would come up running plain Salamander and no Gecko client could
// connect, with nothing logged. So assert the contract directly against the
// vendored parser, using the exact JSON shape InboundBuilder emits.

func buildMask(t *testing.T, settings string) interface{} {
	t.Helper()
	var s conf.Salamander
	if err := json.Unmarshal([]byte(settings), &s); err != nil {
		t.Fatalf("xray-core rejected our settings JSON %s: %v", settings, err)
	}
	msg, err := s.Build()
	if err != nil {
		t.Fatalf("Build() failed for %s: %v", settings, err)
	}
	return msg
}

func TestSalamanderSettingsBuildSalamanderConfig(t *testing.T) {
	msg := buildMask(t, `{"password":"s3cret"}`)

	cfg, ok := msg.(*salamander.Config)
	if !ok {
		t.Fatalf("without packetSize xray-core must build a plain Salamander config, got %T", msg)
	}
	if cfg.Password != "s3cret" {
		t.Errorf("Password = %q, want %q", cfg.Password, "s3cret")
	}
}

func TestGeckoSettingsBuildGeckoConfig(t *testing.T) {
	msg := buildMask(t, `{"password":"s3cret","packetSize":"600-1300"}`)

	cfg, ok := msg.(*salamander.GeckoConfig)
	if !ok {
		t.Fatalf("with packetSize xray-core must build a Gecko config, got %T", msg)
	}
	if cfg.Password != "s3cret" {
		t.Errorf("Password = %q, want %q", cfg.Password, "s3cret")
	}
	if cfg.MinPacketSize != 600 || cfg.MaxPacketSize != 1300 {
		t.Errorf("packet size = %d-%d, want 600-1300", cfg.MinPacketSize, cfg.MaxPacketSize)
	}
}

// Our own parse layer caps the range at 2048 so the operator gets a readable
// panel-side error. Pin the upstream bound that cap mirrors.
func TestGeckoPacketSizeUpperBoundIsStill2048(t *testing.T) {
	var s conf.Salamander
	if err := json.Unmarshal([]byte(`{"password":"s3cret","packetSize":"600-2049"}`), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, err := s.Build(); err == nil {
		t.Fatal("xray-core used to reject a packetSize above 2048; it no longer does, " +
			"so geckoMaxPacketSizeCap in api/sspanel is now wrong")
	}
}
