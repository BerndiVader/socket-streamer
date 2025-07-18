package alarm

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"time"
	"ws-streamer/config"
)

const (
	STATE_IDLE    = 0
	STATE_ALARM   = 1
	ALARM_TIMEOUT = 5 * time.Minute
)

type Alarm struct {
	cfg             *config.ConfigCamera
	state           int
	lastAICheck     time.Time
	lastAIAlarm     time.Time
	alarmStart      time.Time
	aiCooldown      time.Duration
	aiCheckInterval time.Duration
	mdCheckInterval time.Duration
	stopRec         chan struct{}
	done            chan struct{}
	ffmpegCmd       *exec.Cmd
}

func NewAlarm(conf *config.ConfigCamera) *Alarm {

	var a = Alarm{
		cfg:             conf,
		state:           STATE_IDLE,
		aiCooldown:      15 * time.Second,
		aiCheckInterval: 10 * time.Second,
		mdCheckInterval: 5 * time.Second,
	}
	return &a

}

func (a *Alarm) Run() {
	for {
		select {
		case <-a.done:
			a.Stop()
			return
		default:
			motion := a.isMotion()
			now := time.Now()

			switch a.state {
			case STATE_IDLE:
				if motion && now.Sub(a.lastAICheck) > a.aiCheckInterval && now.Sub(a.lastAIAlarm) > a.aiCooldown {
					if a.isHuman() {
						log.Println("Human detected! -> change to ALARM.")
						a.state = STATE_ALARM
						a.alarmStart = now
						a.lastAIAlarm = now
					}
					a.lastAICheck = now
				}
			case STATE_ALARM:
				if !a.isHuman() {
					log.Println("No human detected! -> back to IDLE.")
					a.state = STATE_IDLE
				} else if now.Sub(a.alarmStart) > ALARM_TIMEOUT {
					log.Println("Human detected but alarm timeout! -> back to IDLE.")
					a.state = STATE_IDLE
				} else {
					log.Println("Still on ALARM.")
				}
			}

			if config.GetConfigGlobal().LogLevel == "debug" {
				log.Printf("motion: %v, state: %v\n", motion, a.state)
			}
			time.Sleep(a.mdCheckInterval)
		}
	}
}

func (a *Alarm) Stop() {
	close(a.stopRec)
}

func (a *Alarm) recorderStop() {
	if a.ffmpegCmd != nil {
		if err := a.ffmpegCmd.Process.Kill(); err != nil {
			log.Println(err.Error())
		}
	}
}

func (a *Alarm) recorderStart() {
	if a.ffmpegCmd != nil {
		if err := a.ffmpegCmd.Process.Kill(); err != nil {
			log.Println(err.Error())
		}
	}
}

func (a *Alarm) isMotion() bool {

	resp, err := http.DefaultClient.Get(fmt.Sprintf("http://%s/api.cgi?cmd=GetMdState&channel=0&rs=random123&user=%s&password=%s", a.cfg.Address, a.cfg.User, a.cfg.Password))
	if err != nil {
		log.Println(err.Error())
		return false
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Println(err.Error())
		return false
	}
	var states MdStateEnvelope
	if err := json.Unmarshal(body, &states); err != nil {
		log.Println(err.Error())
		return false
	}
	if len(states) > 0 {
		state := states[0]
		return state.Value.State == 1
	}
	return false

}

func (a *Alarm) isHuman() bool {

	resp, err := http.DefaultClient.Get(fmt.Sprintf("http://%s/api.cgi?cmd=GetAiState&channel=0&rs=random123&user=%s&password=%s", a.cfg.Address, a.cfg.User, a.cfg.Password))
	if err != nil {
		log.Println(err.Error())
		return false
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Println(err.Error())
		return false
	}
	var states AiStateEnvelope
	if err := json.Unmarshal(body, &states); err != nil {
		log.Println(err.Error())
		return false
	}
	if len(states) > 0 {
		state := states[0]
		return state.Value.People.AlarmState == 1
	}
	return false

}
