// Package controller Package generate the InboundConfig used by add inbound
package controller

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sagernet/sing-shadowsocks/shadowaead_2022"
	C "github.com/sagernet/sing/common"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"

	"github.com/XrayR-project/XrayR/api"
	"github.com/XrayR-project/XrayR/common/mylego"
)

// InboundBuilder build Inbound config for different protocol
func InboundBuilder(config *Config, nodeInfo *api.NodeInfo, tag string) (*core.InboundHandlerConfig, error) {
	inboundDetourConfig := &conf.InboundDetourConfig{}
	// Build Listen IP address
	if nodeInfo.NodeType == "Shadowsocks-Plugin" {
		// Shdowsocks listen in 127.0.0.1 for safety
		inboundDetourConfig.ListenOn = &conf.Address{Address: net.ParseAddress("127.0.0.1")}
	} else if config.ListenIP != "" {
		ipAddress := net.ParseAddress(config.ListenIP)
		inboundDetourConfig.ListenOn = &conf.Address{Address: ipAddress}
	}

	// Build Port
	portList := &conf.PortList{
		Range: []conf.PortRange{{From: nodeInfo.Port, To: nodeInfo.Port}},
	}
	inboundDetourConfig.PortList = portList
	// Build Tag
	inboundDetourConfig.Tag = tag
	// SniffingConfig
	// Xray-core v26.x changed SniffingConfig.DestOverride from *StringList to StringList (value type).
	sniffingConfig := &conf.SniffingConfig{
		Enabled:      true,
		DestOverride: conf.StringList{"http", "tls", "quic", "fakedns"},
	}
	if config.DisableSniffing {
		sniffingConfig.Enabled = false
	}
	inboundDetourConfig.SniffingConfig = sniffingConfig

	var (
		protocol      string
		streamSetting *conf.StreamConfig
		setting       json.RawMessage
	)

	var proxySetting any
	// Build Protocol and Protocol setting
	switch nodeInfo.NodeType {
	case "V2ray", "Vmess", "Vless":
		if nodeInfo.EnableVless || (nodeInfo.NodeType == "Vless" && nodeInfo.NodeType != "Vmess") {
			protocol = "vless"
			// Enable fallback
			if config.EnableFallback {
				fallbackConfigs, err := buildVlessFallbacks(config.FallBackConfigs)
				if err == nil {
					proxySetting = &conf.VLessInboundConfig{
						Decryption: "none",
						Fallbacks:  fallbackConfigs,
					}
				} else {
					return nil, err
				}
			} else {
				proxySetting = &conf.VLessInboundConfig{
					Decryption: "none",
				}
			}
		} else {
			protocol = "vmess"
			proxySetting = &conf.VMessInboundConfig{}
		}
	case "Trojan":
		protocol = "trojan"
		// Enable fallback
		if config.EnableFallback {
			fallbackConfigs, err := buildTrojanFallbacks(config.FallBackConfigs)
			if err == nil {
				proxySetting = &conf.TrojanServerConfig{
					Fallbacks: fallbackConfigs,
				}
			} else {
				return nil, err
			}
		} else {
			proxySetting = &conf.TrojanServerConfig{}
		}
	case "Hysteria2":
		// Hysteria 2 protocol settings are minimal at the proxy layer —
		// only Version + Users. Users are added later via AddUser per sync cycle.
		// Bandwidth (BrutalUp/Down), obfuscation (Salamander), and masquerade
		// live at the transport/finalmask layer, wired below in the stream
		// settings block for networkType == "hysteria".
		protocol = "hysteria"
		proxySetting = &conf.HysteriaServerConfig{
			Version: 2,
			Users:   nil, // filled dynamically
		}

	case "Shadowsocks", "Shadowsocks-Plugin":
		protocol = "shadowsocks"
		cipher := strings.ToLower(nodeInfo.CypherMethod)

		proxySetting = &conf.ShadowsocksServerConfig{
			Cipher:   cipher,
			Password: nodeInfo.ServerKey, // shadowsocks2022 shareKey
		}

		proxySetting, _ := proxySetting.(*conf.ShadowsocksServerConfig)
		// shadowsocks must have a random password
		// shadowsocks2022's password == user PSK, thus should a length of string >= 32 and base64 encoder
		b := make([]byte, 32)
		rand.Read(b)
		randPasswd := hex.EncodeToString(b)
		if C.Contains(shadowaead_2022.List, cipher) {
			proxySetting.Users = append(proxySetting.Users, &conf.ShadowsocksUserConfig{
				Password: base64.StdEncoding.EncodeToString(b),
			})
		} else {
			proxySetting.Password = randPasswd
		}

		proxySetting.NetworkList = &conf.NetworkList{"tcp", "udp"}
		// IVCheck field was removed from ShadowsocksServerConfig in Xray-core v26.x.
		// The legacy Shadowsocks IV collision check no longer applies; modern 2022-blake3
		// ciphers handle replay protection internally. config.DisableIVCheck is now a no-op.

	case "dokodemo-door":
		protocol = "dokodemo-door"
		proxySetting = struct {
			Host        string   `json:"address"`
			NetworkList []string `json:"network"`
		}{
			Host:        "v1.mux.cool",
			NetworkList: []string{"tcp", "udp"},
		}
	default:
		return nil, fmt.Errorf("unsupported node type: %s, Only support: V2ray, Trojan, Shadowsocks, and Shadowsocks-Plugin", nodeInfo.NodeType)
	}

	setting, err := json.Marshal(proxySetting)
	if err != nil {
		return nil, fmt.Errorf("marshal proxy %s config failed: %s", nodeInfo.NodeType, err)
	}
	inboundDetourConfig.Protocol = protocol
	inboundDetourConfig.Settings = &setting

	// Build streamSettings
	streamSetting = new(conf.StreamConfig)
	transportProtocol := conf.TransportProtocol(nodeInfo.TransportProtocol)
	networkType, err := transportProtocol.Build()
	if err != nil {
		return nil, fmt.Errorf("convert TransportProtocol failed: %s", err)
	}

	switch networkType {
	case "tcp":
		tcpSetting := &conf.TCPConfig{
			AcceptProxyProtocol: config.EnableProxyProtocol,
			HeaderConfig:        nodeInfo.Header,
		}
		streamSetting.TCPSettings = tcpSetting
	case "websocket":
		headers := make(map[string]string)
		headers["Host"] = nodeInfo.Host
		wsSettings := &conf.WebSocketConfig{
			AcceptProxyProtocol: config.EnableProxyProtocol,
			Host:                nodeInfo.Host,
			Path:                nodeInfo.Path,
			Headers:             headers,
		}
		streamSetting.WSSettings = wsSettings
	case "grpc":
		grpcSettings := &conf.GRPCConfig{
			ServiceName: nodeInfo.ServiceName,
			Authority:   nodeInfo.Authority,
		}
		streamSetting.GRPCSettings = grpcSettings
	case "httpupgrade":
		httpupgradeSettings := &conf.HttpUpgradeConfig{
			Headers:             nodeInfo.Headers,
			Path:                nodeInfo.Path,
			Host:                nodeInfo.Host,
			AcceptProxyProtocol: nodeInfo.AcceptProxyProtocol,
		}
		streamSetting.HTTPUPGRADESettings = httpupgradeSettings
	case "splithttp", "xhttp":
		splithttpSetting := &conf.SplitHTTPConfig{
			Path: nodeInfo.Path,
			Host: nodeInfo.Host,
		}
		streamSetting.SplitHTTPSettings = splithttpSetting
	case "hysteria":
		if err := buildHysteria2StreamSettings(streamSetting, nodeInfo); err != nil {
			return nil, err
		}
	}
	streamSetting.Network = &transportProtocol

	// Build TLS and REALITY settings
	var isREALITY bool
	if config.DisableLocalREALITYConfig {
		if nodeInfo.REALITYConfig != nil && nodeInfo.EnableREALITY {
			isREALITY = true
			streamSetting.Security = "reality"

			r := nodeInfo.REALITYConfig
			streamSetting.REALITYSettings = &conf.REALITYConfig{
				Show:         config.REALITYConfigs.Show,
				Dest:         []byte(`"` + r.Dest + `"`),
				Xver:         r.ProxyProtocolVer,
				ServerNames:  r.ServerNames,
				PrivateKey:   r.PrivateKey,
				MinClientVer: r.MinClientVer,
				MaxClientVer: r.MaxClientVer,
				MaxTimeDiff:  r.MaxTimeDiff,
				ShortIds:     r.ShortIds,
			}
		}
	} else if config.EnableREALITY && config.REALITYConfigs != nil {
		isREALITY = true
		streamSetting.Security = "reality"

		streamSetting.REALITYSettings = &conf.REALITYConfig{
			Show:         config.REALITYConfigs.Show,
			Dest:         []byte(`"` + config.REALITYConfigs.Dest + `"`),
			Xver:         config.REALITYConfigs.ProxyProtocolVer,
			ServerNames:  config.REALITYConfigs.ServerNames,
			PrivateKey:   config.REALITYConfigs.PrivateKey,
			MinClientVer: config.REALITYConfigs.MinClientVer,
			MaxClientVer: config.REALITYConfigs.MaxClientVer,
			MaxTimeDiff:  config.REALITYConfigs.MaxTimeDiff,
			ShortIds:     config.REALITYConfigs.ShortIds,
		}
	}

	if !isREALITY && nodeInfo.EnableTLS && config.CertConfig.CertMode != "none" {
		streamSetting.Security = "tls"
		certFile, keyFile, err := getCertFile(config.CertConfig)
		if err != nil {
			return nil, err
		}
		tlsSettings := &conf.TLSConfig{
			RejectUnknownSNI: config.CertConfig.RejectUnknownSni,
		}
		tlsSettings.Certs = append(tlsSettings.Certs, &conf.TLSCertConfig{CertFile: certFile, KeyFile: keyFile, OcspStapling: 3600})
		// Hysteria 2 mandates ALPN "h3" in the QUIC handshake. Without this,
		// xray-core's GetTLSConfig falls back to ["h2","http/1.1"], which has
		// no overlap with what every Hy2 client offers — the server then
		// answers the first Initial with a CRYPTO_ERROR / TLS alert 120
		// (no_application_protocol) and the connection never establishes.
		if networkType == "hysteria" {
			alpn := conf.StringList{"h3"}
			tlsSettings.ALPN = &alpn
		}
		streamSetting.TLSSettings = tlsSettings
	}

	// Support ProxyProtocol for any transport protocol
	if networkType != "tcp" && networkType != "ws" && config.EnableProxyProtocol {
		sockoptConfig := &conf.SocketConfig{
			AcceptProxyProtocol: config.EnableProxyProtocol,
		}
		streamSetting.SocketSettings = sockoptConfig
	}
	inboundDetourConfig.StreamSetting = streamSetting

	return inboundDetourConfig.Build()
}

// buildHysteria2StreamSettings wires the Hy2-specific transport + finalmask
// blocks on a StreamConfig. Xray-core v26.x splits Hy2 config across three
// places:
//
//   - StreamConfig.HysteriaSettings: transport-level (version, masquerade,
//     udp_idle_timeout). Up/Down/Congestion/UdpHop fields on this struct are
//     deprecated — we do not set them.
//   - StreamConfig.FinalMask.QuicParams: BBR bandwidth (brutalUp/brutalDown)
//   - congestion algo ("brutal"). The active location for bandwidth.
//   - StreamConfig.FinalMask.Udp: Salamander obfuscation is registered as a
//     UDP mask (type: "salamander"), not a direct FinalMask field.
//
// TLS settings are applied by the caller's shared TLS/REALITY block; we only
// wire the hysteria-specific pieces here.
func buildHysteria2StreamSettings(streamSetting *conf.StreamConfig, nodeInfo *api.NodeInfo) error {
	// Transport layer: Version + Masquerade.
	hyConfig := &conf.HysteriaConfig{
		Version: 2,
	}
	if nodeInfo.Hy2Masquerade != nil {
		m := nodeInfo.Hy2Masquerade
		hyConfig.Masquerade = conf.Masquerade{
			Type:        m.Type,
			Url:         m.URL, // xray-core uses Url (not URL)
			RewriteHost: m.RewriteHost,
			Insecure:    m.Insecure,
			Dir:         m.Dir,
			Content:     m.Content,
			StatusCode:  m.StatusCode,
		}
	} else {
		// D4: safe default — masq_type "string" with 404 empty body.
		// Does not emit outbound traffic (url mode would fetch remote).
		hyConfig.Masquerade = conf.Masquerade{
			Type:       "string",
			StatusCode: 404,
			Content:    "",
		}
	}
	streamSetting.HysteriaSettings = hyConfig

	// Finalmask: QuicParams (receive windows + bandwidth) + optional Salamander obfs.
	finalMask := &conf.FinalMask{}

	// QUIC receive windows: override xray-core's defaults (stream 8MiB /
	// conn 20MiB) with larger values to better fill high-BDP links.
	quicParams := &conf.QuicParamsConfig{
		InitStreamReceiveWindow:     26843545,
		MaxStreamReceiveWindow:      26843545,
		InitConnectionReceiveWindow: 67108864,
		MaxConnectionReceiveWindow:  67108864,
	}

	if nodeInfo.UpMbps > 0 || nodeInfo.DownMbps > 0 {
		quicParams.Congestion = "brutal"
		// Bandwidth is parsed from a string like "500 mbps"; the simplest
		// robust path is to marshal via JSON. See conf.Bandwidth.UnmarshalJSON.
		if nodeInfo.UpMbps > 0 {
			if err := json.Unmarshal(
				[]byte(fmt.Sprintf(`"%d mbps"`, nodeInfo.UpMbps)),
				&quicParams.BrutalUp,
			); err != nil {
				return fmt.Errorf("Hysteria2: parse up_mbps=%d: %w", nodeInfo.UpMbps, err)
			}
		}
		if nodeInfo.DownMbps > 0 {
			if err := json.Unmarshal(
				[]byte(fmt.Sprintf(`"%d mbps"`, nodeInfo.DownMbps)),
				&quicParams.BrutalDown,
			); err != nil {
				return fmt.Errorf("Hysteria2: parse down_mbps=%d: %w", nodeInfo.DownMbps, err)
			}
		}
	}
	finalMask.QuicParams = quicParams

	if nodeInfo.Obfs == "salamander" && nodeInfo.ObfsPassword != "" {
		salaRaw := json.RawMessage(fmt.Sprintf(`{"password":%q}`, nodeInfo.ObfsPassword))
		finalMask.Udp = []conf.Mask{
			{
				Type:     "salamander",
				Settings: &salaRaw,
			},
		}
	}

	// Only attach finalmask if we actually configured something on it.
	if finalMask.QuicParams != nil || len(finalMask.Udp) > 0 {
		streamSetting.FinalMask = finalMask
	}

	return nil
}

func getCertFile(certConfig *mylego.CertConfig) (certFile string, keyFile string, err error) {
	switch certConfig.CertMode {
	case "file":
		if certConfig.CertFile == "" || certConfig.KeyFile == "" {
			return "", "", fmt.Errorf("cert file path or key file path not exist")
		}
		return certConfig.CertFile, certConfig.KeyFile, nil
	case "dns":
		lego, err := mylego.New(certConfig)
		if err != nil {
			return "", "", err
		}
		certPath, keyPath, err := lego.DNSCert()
		if err != nil {
			return "", "", err
		}
		return certPath, keyPath, err
	case "http", "tls":
		lego, err := mylego.New(certConfig)
		if err != nil {
			return "", "", err
		}
		certPath, keyPath, err := lego.HTTPCert()
		if err != nil {
			return "", "", err
		}
		return certPath, keyPath, err
	default:
		return "", "", fmt.Errorf("unsupported certmode: %s", certConfig.CertMode)
	}
}

func buildVlessFallbacks(fallbackConfigs []*FallBackConfig) ([]*conf.VLessInboundFallback, error) {
	if fallbackConfigs == nil {
		return nil, fmt.Errorf("you must provide FallBackConfigs")
	}

	vlessFallBacks := make([]*conf.VLessInboundFallback, len(fallbackConfigs))
	for i, c := range fallbackConfigs {

		if c.Dest == "" {
			return nil, fmt.Errorf("dest is required for fallback failed")
		}

		var dest json.RawMessage
		dest, err := json.Marshal(c.Dest)
		if err != nil {
			return nil, fmt.Errorf("marshal dest %s config failed: %s", dest, err)
		}
		vlessFallBacks[i] = &conf.VLessInboundFallback{
			Name: c.SNI,
			Alpn: c.Alpn,
			Path: c.Path,
			Dest: dest,
			Xver: c.ProxyProtocolVer,
		}
	}
	return vlessFallBacks, nil
}

func buildTrojanFallbacks(fallbackConfigs []*FallBackConfig) ([]*conf.TrojanInboundFallback, error) {
	if fallbackConfigs == nil {
		return nil, fmt.Errorf("you must provide FallBackConfigs")
	}

	trojanFallBacks := make([]*conf.TrojanInboundFallback, len(fallbackConfigs))
	for i, c := range fallbackConfigs {

		if c.Dest == "" {
			return nil, fmt.Errorf("dest is required for fallback failed")
		}

		var dest json.RawMessage
		dest, err := json.Marshal(c.Dest)
		if err != nil {
			return nil, fmt.Errorf("marshal dest %s config failed: %s", dest, err)
		}
		trojanFallBacks[i] = &conf.TrojanInboundFallback{
			Name: c.SNI,
			Alpn: c.Alpn,
			Path: c.Path,
			Dest: dest,
			Xver: c.ProxyProtocolVer,
		}
	}
	return trojanFallBacks, nil
}
