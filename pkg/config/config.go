package config

type Config struct {
	ServerURL       string
	AllowHTTP       bool
	Debug           bool
	Transport       string
	Headers         map[string]string
	ConfigDir       string
	ProtocolVersion string
}