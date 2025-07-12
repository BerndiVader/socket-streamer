package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	origin     string = "http://localhost"
	selfIP     string = "111.111.111.111"
	cameraIP   string = "111.111.111.111"
	username   string = "user"
	password   string = "password"
	rtspURL    string
	ffmpegPath string = "ffmpeg"
	quality    string = "sub"
	port       int    = 1510
	CamPath    string = "/"
)

type Config struct {
	Origin     string
	SelfIP     string
	CameraIP   string
	Username   string
	Password   string
	FFmpegPath string
	Quality    string
	Port       int
	RTSPURL    string
	CamPath    string
}

var cfg Config

func NewConfig() *Config {

	cfg = Config{
		Origin:     origin,
		SelfIP:     selfIP,
		CameraIP:   cameraIP,
		Username:   username,
		Password:   password,
		FFmpegPath: ffmpegPath,
		Quality:    quality,
		Port:       port,
		RTSPURL:    rtspURL,
	}

	exe, _ := os.Executable()
	conf := filepath.Join(filepath.Dir(exe), "ws-streamer.conf")
	conf = strings.ReplaceAll(conf, "\\", "/")

	f, err := os.Open(conf)
	if err == nil {
		defer f.Close()
		decoder := json.NewDecoder(f)
		_ = decoder.Decode(&cfg)

		flag.StringVar(&cfg.Origin, "origin", cfg.Origin, "domain origin name")
		flag.StringVar(&cfg.CameraIP, "camera_ip", cfg.CameraIP, "camera IP address")
		flag.StringVar(&cfg.Username, "user", cfg.Username, "camera login name")
		flag.StringVar(&cfg.Password, "pass", cfg.Password, "camera user password")
		flag.StringVar(&cfg.SelfIP, "host_ip", cfg.SelfIP, "host IP address")
		flag.IntVar(&cfg.Port, "port", cfg.Port, "websocket server port")
		flag.StringVar(&cfg.FFmpegPath, "ffmpeg_path", cfg.FFmpegPath, "path to ffmpeg")
		flag.StringVar(&cfg.Quality, "quality", cfg.Quality, "quality: high/low")
		flag.StringVar(&cfg.CamPath, "campath", cfg.CamPath, "path to camera")
		flag.Parse()
	}

	if strings.ToLower(cfg.Quality) == "high" {
		cfg.Quality = "main"
	} else {
		cfg.Quality = "sub"
	}

	cfg.RTSPURL = fmt.Sprintf("rtsp://%s:%s@%s:554/h264Preview_01_%s", cfg.Username, cfg.Password, cfg.CameraIP, cfg.Quality)

	return &cfg
}
