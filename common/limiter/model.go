package limiter

type GlobalDeviceLimitConfig struct {
	Enable           bool   `mapstructure:"Enable"`
	RedisNetwork     string `mapstructure:"RedisNetwork"`     // tcp or unix
	RedisAddr        string `mapstructure:"RedisAddr"`        // host:port, or /path/to/unix.sock
	RedisUsername    string `mapstructure:"RedisUsername"`
	RedisPassword    string `mapstructure:"RedisPassword"`
	RedisDB          int    `mapstructure:"RedisDB"`
	Timeout          int    `mapstructure:"Timeout"`
	Expiry           int    `mapstructure:"Expiry"`           // second
	EnableTLS        bool   `mapstructure:"EnableTLS"`        // Enable TLS connection
	TLSSkipVerify    bool   `mapstructure:"TLSSkipVerify"`    // Skip TLS certificate verification
	TLSCertFile      string `mapstructure:"TLSCertFile"`      // Path to TLS certificate file
	TLSKeyFile       string `mapstructure:"TLSKeyFile"`       // Path to TLS key file
	TLSCAFile        string `mapstructure:"TLSCAFile"`        // Path to TLS CA file
	TLSServerName    string `mapstructure:"TLSServerName"`    // Server name for TLS verification
}
