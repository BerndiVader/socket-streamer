package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
)

var SigShutdown = make(chan struct{})

type ConfigGlobal struct {
	LogLevel string          `json:"loglevel"`
	WShost   string          `json:"ws_host"`
	WSPort   int             `json:"ws_port"`
	Cameras  []*ConfigCamera `json:"cameras"`
}

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
}

var global *ConfigGlobal

func Init(path *string) error {

	global = &ConfigGlobal{
		LogLevel: "info",
		Cameras:  make([]*ConfigCamera, 0),
	}

	exe, err := os.Executable()
	if err == nil || *path != "" {
		var conf string
		if *path == "" {
			conf = filepath.Join(filepath.Dir(exe), "ws-streamer.conf")
			conf = strings.ReplaceAll(conf, "\\", "/")
		} else {
			conf = *path
		}

		file, err := os.Open(conf)
		if err == nil {
			defer file.Close()
			decoder := json.NewDecoder(file)
			err = decoder.Decode(&global)
			if err != nil {
				log.Printf("%v - Error decoding config file.\n", err)
			}
		} else {
			log.Printf("%v - Error opening config file.\n", err)
		}
	} else {
		log.Printf("%v - Error getting filepath. Use -config to set path to config.\n", err)
	}

	if err == nil {
		log.Println("Config loaded ok.")
	}

	return err
}

func GetConfigGlobal() *ConfigGlobal {
	return global
}

func GetCamera(name string) *ConfigCamera {
	name = strings.ToLower(name)
	for _, cam := range global.Cameras {
		if strings.ToLower(cam.Name) == name {
			return cam
		}

	}
	return nil
}

func GetCameras() []*ConfigCamera {
	return global.Cameras
}
