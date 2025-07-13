package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
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
		sigs <- syscall.SIGTERM
	} else {
		for i := range config.GetCameras() {
			var cfg *config.ConfigCamera = &config.GetCameras()[i]
			if s := streamer.NewStreamer(cfg); s != nil {
				log.Printf("Start streaming for %s", cfg.Name)
				go s.Start()
			} else {
				log.Printf("Failed to start streaming for %s", cfg.Name)
			}
		}
	}

	go func() {
		cfg := config.GetConfigGlobal()
		log.Println(http.ListenAndServe(fmt.Sprintf("%s:%d", cfg.WShost, cfg.WSPort), nil))
		sigs <- syscall.SIGTERM
	}()

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
