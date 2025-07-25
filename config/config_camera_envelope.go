package config

type ConfigCamera struct {
	Name       string `json:"name"`
	RTSPURL    string `json:"rtsp_url"`
	WSPath     string `json:"ws_path"`
	Origin     string `json:"origin"`
	FFmpegPath string `json:"ffmpeg_path"`
	Address    string `json:"addr"`
	User       string `json:"user"`
	Password   string `json:"pass"`
	Tracking   bool   `json:"tracking"`
	RecPath    string `json:"rec_path"`
	MdInterval int    `json:"md_interval"`
	AiInterval int    `json:"ai_interval"`
	AiCooldown int    `json:"ai_cooldown"`
	ReCooldown int    `json:"rec_cooldown"`
}
