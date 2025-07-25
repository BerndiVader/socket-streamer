package config

type ConfigGlobal struct {
	LogLevel    LogLevel
	LoglevelStr string          `json:"loglevel"`
	WShost      string          `json:"ws_host"`
	WSPort      int             `json:"ws_port"`
	Cameras     []*ConfigCamera `json:"cameras"`
}
