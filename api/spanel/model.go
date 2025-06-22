package spanel

import "encoding/json"

type (
	ServerCommon struct {
		Protocol  string `form:"protocol" json:"protocol"`
		ServerId  int64  `form:"server_id" json:"server_id"`
		SecretKey string `form:"secret_key" json:"secret_key"`
	}

	ServerBasic struct {
		PushInterval int64 `json:"push_interval"`
		PullInterval int64 `json:"pull_interval"`
	}

	GetServerConfigResponse struct {
		Basic    ServerBasic      `json:"basic"`
		Protocol string           `json:"protocol"`
		Config   *json.RawMessage `json:"config"`
	}

	Vmess struct {
		Port            int             `json:"port"`
		Transport       string          `json:"transport"`
		TransportConfig TransportConfig `json:"transport_config"`
		Security        string          `json:"security"`
		SecurityConfig  SecurityConfig  `json:"security_config"`
	}
	Vless struct {
		Port            int             `json:"port"`
		Flow            string          `json:"flow"`
		Transport       string          `json:"transport"`
		TransportConfig TransportConfig `json:"transport_config"`
		Security        string          `json:"security"`
		SecurityConfig  SecurityConfig  `json:"security_config"`
	}
	Trojan struct {
		Port            int             `json:"port"`
		Transport       string          `json:"transport"`
		TransportConfig TransportConfig `json:"transportConfig"`
		Security        string          `json:"security"`
		SecurityConfig  SecurityConfig  `json:"securityConfig"`
	}

	Shadowsocks struct {
		Method    string `json:"method"`
		Port      int    `json:"port"`
		ServerKey string `json:"server_key"`
	}

	TransportConfig struct {
		Path        string `json:"path"`
		Host        string `json:"host"`
		ServiceName string `json:"service_name"`
	}
	SecurityConfig struct {
		SNI               string `json:"sni"`
		AllowInsecure     bool   `json:"allow_insecure"`
		Fingerprint       string `json:"fingerprint"`
		RealityPrivateKey string `json:"reality_private_key"`
		RealityPublicKey  string `json:"reality_public_key"`
		RealityShortId    string `json:"reality_short_id"`
	}

	GetServerUserListResponse struct {
		Users []ServerUser `json:"users"`
	}
	ServerUser struct {
		Id          int64  `json:"id"`
		UUID        string `json:"uuid"`
		SpeedLimit  int64  `json:"speed_limit"`
		DeviceLimit int64  `json:"device_limit"`
	}
	ServerPushStatusRequest struct {
		Cpu       float64 `json:"cpu"`
		Mem       float64 `json:"mem"`
		Disk      float64 `json:"disk"`
		UpdatedAt int64   `json:"updated_at"`
	}
	ServerPushUserTrafficRequest struct {
		ServerCommon
		Traffic []UserTraffic `json:"traffic"`
	}
	UserTraffic struct {
		UID      int64 `json:"uid"`
		Upload   int64 `json:"upload"`
		Download int64 `json:"download"`
	}
	OnlineUser struct {
		UID int64  `json:"uid"`
		IP  string `json:"ip"`
	}
	OnlineUsersRequest struct {
		Users []OnlineUser `json:"users"`
	}
)
