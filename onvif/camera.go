package onvif

import (
	"log"
	"time"
)

type Camera struct {
	Address    string
	User       string
	Pass       string
	quit       chan struct{}
	runnerstop chan struct{}
}

func (camera *Camera) runner() {
	defer close(camera.runnerstop)
	for {
		select {
		case <-camera.quit:
			log.Println("camera-runner stop.")
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (camera *Camera) Start() {
	if camera.quit != nil {
		return
	}
	camera.quit = make(chan struct{})
	camera.runnerstop = make(chan struct{})
	go camera.runner()
}

func (camera *Camera) Stop() {
	if camera.quit != nil {
		close(camera.quit)
		camera.quit = nil
		<-camera.runnerstop
	}
}
