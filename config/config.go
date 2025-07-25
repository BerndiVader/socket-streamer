package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type LogLevel int

const (
	LOG_DEBUG LogLevel = iota
	LOG_INFO
	LOG_WARN
	LOG_ERROR
	LOG_VERB
)

var SigShutdown = make(chan struct{})

var global *ConfigGlobal

func Init(path *string) error {

	global = &ConfigGlobal{
		LoglevelStr: "info",
		Cameras:     make([]*ConfigCamera, 0),
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

	switch strings.ToLower(global.LoglevelStr) {
	case "info":
		global.LogLevel = LOG_INFO
	case "warn":
		global.LogLevel = LOG_WARN
	case "error":
		global.LogLevel = LOG_ERROR
	case "verb":
		global.LogLevel = LOG_VERB
	default:
		global.LogLevel = LOG_INFO
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
