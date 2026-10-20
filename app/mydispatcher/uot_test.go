package mydispatcher_test

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/sagernet/sing-shadowsocks/shadowaead_2022"
	B "github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/uot"
	"github.com/xtls/xray-core/app/proxyman"
	_ "github.com/xtls/xray-core/app/proxyman/inbound"
	_ "github.com/xtls/xray-core/app/proxyman/outbound"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/routing"
	"github.com/xtls/xray-core/infra/conf"
	_ "github.com/xtls/xray-core/proxy/freedom"
	_ "github.com/xtls/xray-core/proxy/shadowsocks_2022"

	"github.com/XrayR-project/XrayR/api"
	"github.com/XrayR-project/XrayR/app/mydispatcher"
)

const uotTestTag = "ss2022-uot"

func uotTestKey(t *testing.T) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func uotTestFreePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// uotTestServer starts a Shadowsocks 2022 multi-user inbound and a freedom outbound wired
// through mydispatcher, the way panel.go builds the core. freedom blocks private targets by
// default, so the loopback echo server is allowed explicitly.
func uotTestServer(t *testing.T, port int, serverKey, userKey, email string) *mydispatcher.DefaultDispatcher {
	inboundJSON := fmt.Sprintf(`{
		"tag": %q, "listen": "127.0.0.1", "port": %d, "protocol": "shadowsocks",
		"settings": {"method": "2022-blake3-aes-128-gcm", "password": %q, "network": "tcp,udp",
			"clients": [{"password": %q, "email": %q}]}
	}`, uotTestTag, port, serverKey, userKey, email)
	var inbound conf.InboundDetourConfig
	if err := json.Unmarshal([]byte(inboundJSON), &inbound); err != nil {
		t.Fatal(err)
	}
	inboundConfig, err := inbound.Build()
	if err != nil {
		t.Fatal(err)
	}
	var outbound conf.OutboundDetourConfig
	if err := json.Unmarshal([]byte(`{"protocol": "freedom", "settings": {"finalRules": [{"action": "allow", "ip": ["127.0.0.1"]}]}}`), &outbound); err != nil {
		t.Fatal(err)
	}
	outboundConfig, err := outbound.Build()
	if err != nil {
		t.Fatal(err)
	}

	server, err := core.New(&core.Config{
		App: []*serial.TypedMessage{
			serial.ToTypedMessage(&mydispatcher.Config{}),
			serial.ToTypedMessage(&proxyman.InboundConfig{}),
			serial.ToTypedMessage(&proxyman.OutboundConfig{}),
		},
		Inbound:  []*core.InboundHandlerConfig{inboundConfig},
		Outbound: []*core.OutboundHandlerConfig{outboundConfig},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })

	return server.GetFeature(routing.DispatcherType()).(*mydispatcher.DefaultDispatcher)
}

// uotTestEcho answers every UDP packet with "echo:" + payload.
func uotTestEcho(t *testing.T) *net.UDPAddr {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		b := make([]byte, 2048)
		for {
			n, addr, err := pc.ReadFrom(b)
			if err != nil {
				return
			}
			pc.WriteTo(append([]byte("echo:"), b[:n]...), addr)
		}
	}()
	return pc.LocalAddr().(*net.UDPAddr)
}

func TestUDPOverTCP(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version uint8
	}{
		{"v2", uot.Version},
		{"legacy, mihomo's default", uot.LegacyVersion},
	} {
		t.Run(tc.name, func(t *testing.T) {
			serverKey, userKey := uotTestKey(t), uotTestKey(t)
			port := uotTestFreePort(t)
			const uid = 7
			// Inbound users are tagged "<inbound tag>|<email>|<uid>", as buildUserTag does.
			dispatcher := uotTestServer(t, port, serverKey, userKey, fmt.Sprintf("%s|u@test|%d", uotTestTag, uid))
			if err := dispatcher.Limiter.AddInboundLimiter(uotTestTag, 0, &[]api.UserInfo{{UID: uid, Email: "u@test"}}, nil); err != nil {
				t.Fatal(err)
			}
			echo := uotTestEcho(t)

			method, err := shadowaead_2022.NewWithPassword("2022-blake3-aes-128-gcm", serverKey+":"+userKey, time.Now)
			if err != nil {
				t.Fatal(err)
			}
			tcp, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer tcp.Close()
			tcp.SetDeadline(time.Now().Add(10 * time.Second))
			ssConn, err := method.DialConn(tcp, uot.RequestDestination(tc.version))
			if err != nil {
				t.Fatal(err)
			}
			client := &uot.Client{Version: tc.version}
			packetConn, err := client.DialConn(ssConn, false, M.SocksaddrFromNet(echo))
			if err != nil {
				t.Fatal(err)
			}

			for _, payload := range []string{"first", "second"} {
				if err := packetConn.WritePacket(B.As([]byte(payload)), M.SocksaddrFromNet(echo)); err != nil {
					t.Fatal(err)
				}
				reply := B.NewPacket()
				from, err := packetConn.ReadPacket(reply)
				if err != nil {
					t.Fatal(err)
				}
				if got, want := string(reply.Bytes()), "echo:"+payload; got != want {
					t.Fatalf("reply = %q, want %q", got, want)
				}
				if from.Port != uint16(echo.Port) {
					t.Fatalf("reply from port %d, want %d", from.Port, echo.Port)
				}
				reply.Release()
			}

			// The UDP flow is counted against the user at the TCP connection's source address.
			online, err := dispatcher.Limiter.GetOnlineDevice(uotTestTag)
			if err != nil {
				t.Fatal(err)
			}
			if len(*online) != 1 || (*online)[0].IP != "127.0.0.1" || (*online)[0].UID != uid {
				t.Fatalf("online devices = %+v, want uid %d at 127.0.0.1", *online, uid)
			}
		})
	}
}
