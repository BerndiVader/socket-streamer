package log

import (
	"log"
	"ws-streamer/config"
)

func Debugln(s string) {
	Logln(config.LOG_DEBUG, s)
}
func Debugf(s string, args ...any) {
	Logf(config.LOG_DEBUG, s, args...)
}

func Infoln(s string, args ...any) {
	Logln(config.LOG_INFO, s)
}
func Infof(s string, args ...any) {
	Logf(config.LOG_INFO, s, args...)
}

func Warnln(s string) {
	Logln(config.LOG_WARN, s)
}
func Warnf(s string, args ...any) {
	Logf(config.LOG_WARN, s, args...)
}

func Errorln(s any) {
	Logln(config.LOG_ERROR, s)
}
func Errorf(s string, args ...any) {
	Logf(config.LOG_ERROR, s, args...)
}

func Logf(level config.LogLevel, format string, args ...any) {
	if config.GetConfigGlobal().LogLevel <= level {
		log.Printf(format, args...)
	}
}
func Logln(level config.LogLevel, s any) {
	if config.GetConfigGlobal().LogLevel <= level {
		log.Println(s)
	}
}
