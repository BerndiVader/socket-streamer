package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"ws-streamer/config"
	"ws-streamer/streamer"
)

var sigs = make(chan os.Signal, 1)

func main() {
	log.SetPrefix("[ws-streamer]")
	log.SetFlags(log.LstdFlags)
	ShutdownHandler()

	var path string
	flag.StringVar(&path, "config", "", "Path to config file.")
	flag.Parse()

	var err error
	if err = config.Init(&path); err != nil {
		log.Fatalf("Config-Error: %v", err)
	}

	if len(config.GetCameras()) == 0 {
		log.Println("No cameras found in config.")
		close(sigs)
	} else {
		for i := range config.GetCameras() {
			var cfg *config.ConfigCamera = &config.GetCameras()[i]
			if s := streamer.NewStreamer(cfg); s != nil {
				log.Printf("Start streaming for %s", cfg.Name)
				s.Start()
			} else {
				log.Printf("Failed to start streaming for %s", cfg.Name)
			}
		}
	}

	<-config.SigShutdown

}

func ShutdownHandler() {

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigs
		log.Println("Shutting down ws-streamer-go...")
		log.Println("Close all streamer...")
		close(config.SigShutdown)

	}()
}
