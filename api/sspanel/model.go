package sspanel

import "encoding/json"

// NodeInfoResponse is the response of node
type NodeInfoResponse struct {
	Group           int             `json:"node_group"`
	Class           int             `json:"node_class"`
	SpeedLimit      float64         `json:"node_speedlimit"`
	TrafficRate     float64         `json:"traffic_rate"`
	Sort            int             `json:"sort"`
	RawServerString string          `json:"server"`
	Type            string          `json:"type"`
	CustomConfig    json.RawMessage `json:"custom_config"`
	Version         string          `json:"version"`
}

type CustomConfig struct {
	OffsetPortNode string          `json:"offset_port_node"`
	Host           string          `json:"host"`
	Method         string          `json:"method"`
	ServerKey      string          `json:"server_key"`
	TLS            string          `json:"tls"`
	EnableVless    string          `json:"enable_vless"`
	Network        string          `json:"network"`
	Security       string          `json:"security"`
	Path           string          `json:"path"`
	VerifyCert     bool            `json:"verify_cert"`
	Obfs           string          `json:"obfs"`
	Header         json.RawMessage `json:"header"`
	AllowInsecure  string          `json:"allow_insecure"`
	Servicename    string          `json:"servicename"`
	EnableXtls     string          `json:"enable_xtls"`
	Flow           string          `json:"flow"`
	EnableREALITY  bool            `json:"enable_reality"`
	RealityOpts    *REALITYConfig  `json:"reality-opts"`
	// Hy2Opts bundles Hysteria 2-specific knobs; non-nil only for sort=15 nodes.
	Hy2Opts *Hy2OptsStruct `json:"Hy2Opts,omitempty"`
}

// Hy2OptsStruct carries Hysteria 2 per-node parameters from the panel.
// Mirrors the "Hy2Opts" nested bundle in custom_config JSON.
type Hy2OptsStruct struct {
	UpMbps       uint32             `json:"up_mbps"`
	DownMbps     uint32             `json:"down_mbps"`
	Obfs         string             `json:"obfs"`          // "" or "salamander"
	ObfsPassword string             `json:"obfs_password"` // required when Obfs != ""
	Masquerade   *Hy2MasqueradeOpts `json:"masquerade,omitempty"`
}

// Hy2MasqueradeOpts maps to Xray-core's infra/conf.Masquerade facade.
// Field names use snake_case on the panel side; the backend maps them to
// the camelCase names Xray-core expects (rewriteHost, statusCode).
type Hy2MasqueradeOpts struct {
	Type        string `json:"type"` // "url" | "file" | "string"
	URL         string `json:"url"`
	RewriteHost bool   `json:"rewrite_host"`
	Insecure    bool   `json:"insecure"`
	Dir         string `json:"dir"` // file mode directory (Xray-core calls this "dir", not "file")
	Content     string `json:"content"`
	StatusCode  int32  `json:"status_code"`
}

// UserResponse is the response of user
type UserResponse struct {
	ID          int     `json:"id"`
	Email       string  `json:"email"`
	Passwd      string  `json:"passwd"`
	Port        uint32  `json:"port"`
	Method      string  `json:"method"`
	SpeedLimit  float64 `json:"node_speedlimit"`
	DeviceLimit int     `json:"node_iplimit"`
	UUID        string  `json:"uuid"`
	AliveIP     int     `json:"alive_ip"`
}

// Response is the common response
type Response struct {
	Ret  uint            `json:"ret"`
	Data json.RawMessage `json:"data"`
}

// PostData is the data structure of post data
type PostData struct {
	Data interface{} `json:"data"`
}

// SystemLoad is the data structure of system load
type SystemLoad struct {
	Uptime string `json:"uptime"`
	Load   string `json:"load"`
}

// OnlineUser is the data structure of online user
type OnlineUser struct {
	UID int    `json:"user_id"`
	IP  string `json:"ip"`
}

// UserTraffic is the data structure of traffic
type UserTraffic struct {
	UID      int   `json:"user_id"`
	Upload   int64 `json:"u"`
	Download int64 `json:"d"`
}

type RuleItem struct {
	ID      int    `json:"id"`
	Content string `json:"regex"`
}

type IllegalItem struct {
	ID  int `json:"list_id"`
	UID int `json:"user_id"`
}

type REALITYConfig struct {
	Dest             string   `json:"dest,omitempty"`
	ProxyProtocolVer uint64   `json:"proxy_protocol_ver,omitempty"`
	ServerNames      []string `json:"server_names,omitempty"`
	PrivateKey       string   `json:"private_key,omitempty"`
	MinClientVer     string   `json:"min_client_ver,omitempty"`
	MaxClientVer     string   `json:"max_client_ver,omitempty"`
	MaxTimeDiff      uint64   `json:"max_time_diff,omitempty"`
	ShortIds         []string `json:"short_ids,omitempty"`
}
