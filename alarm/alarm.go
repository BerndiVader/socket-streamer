package alarm

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
	"ws-streamer/config"
	"ws-streamer/log"
)

type State int

const (
	STATE_IDLE State = iota
	STATE_ALARM
)

const (
	ALARM_TIMEOUT = 5 * time.Minute
)

type Alarm struct {
	cfg             *config.ConfigCamera
	state           State
	lastAICheck     time.Time
	lastAIAlarm     time.Time
	alarmStart      time.Time
	lastMotion      time.Time
	aiCooldown      time.Duration
	recCooldown     time.Duration
	aiCheckInterval time.Duration
	mdCheckInterval time.Duration

	ffmpeg *exec.Cmd
	mu     sync.Mutex
}

func NewAlarm(conf *config.ConfigCamera) *Alarm {

	var a = Alarm{
		cfg:             conf,
		state:           STATE_IDLE,
		aiCooldown:      14 * time.Second,
		aiCheckInterval: 7 * time.Second,
		mdCheckInterval: 3 * time.Second,
		recCooldown:     12 * time.Second,
	}

	return &a
}

func (a *Alarm) Run() {

	for {
		select {
		case <-config.SigShutdown:
			a.stopRec()
			return
		case <-time.After(a.mdCheckInterval):
			motion := a.isMotion()
			now := time.Now()

			switch a.state {
			case STATE_IDLE:
				if motion && now.Sub(a.lastAICheck) > a.aiCheckInterval && now.Sub(a.lastAIAlarm) > a.aiCooldown {
					if a.isHuman() {
						log.Infof("Human detected! -> Change to ALARM.")
						a.state = STATE_ALARM
						a.alarmStart = now
						a.lastAIAlarm = now
						a.lastMotion = now
						a.stopRec()
						a.startRec()
					}
					a.lastAICheck = now
				}
			case STATE_ALARM:
				if a.isHuman() {
					a.lastMotion = now
					log.Debugln("Still on ALARM.")
				} else if now.Sub(a.lastMotion) > a.recCooldown {
					log.Infoln("No human detected for cooldown -> back to IDLE.")
					a.state = STATE_IDLE
					a.stopRec()
				} else {
					log.Debugln("Cooldown running, still recording...")
				}
			}
			log.Debugf("motion: %v, state: %v\n", motion, a.state)
		}
	}

}

func (a *Alarm) startRec() {
	a.mu.Lock()
	defer a.mu.Unlock()

	log.Debugln("Start recording...")
	if a.ffmpeg != nil {
		log.Debugln("Recorder already run")
		return
	}

	output := fmt.Sprintf("%s/rec_%s_%d.mp4", a.cfg.RecPath, a.cfg.Name, time.Now().Unix())
	output = strings.ReplaceAll(output, "\\", "/")

	a.ffmpeg = exec.Command(
		a.cfg.FFmpegPath,
		"-rtsp_transport", "tcp",
		"-i", a.cfg.RTSPURL,
		"-c", "copy",
		"-f", "mpegts",
		output,
	)

	if err := a.ffmpeg.Start(); err != nil {
		log.Errorln(err.Error())
	} else {
		log.Debugln("ffmpeg record started.")
	}

}

func (a *Alarm) stopRec() {
	a.mu.Lock()
	defer a.mu.Unlock()

	log.Debugln("Stop recording....")
	if a.ffmpeg != nil && a.ffmpeg.Process != nil {
		a.ffmpeg.Process.Kill()
		done := make(chan error, 1)
		go func() { done <- a.ffmpeg.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				log.Errorf("Recorder Wait error: %s\n", err.Error())
			}
			a.ffmpeg = nil
			log.Debugln("Recording stopped.")
		case <-time.After(5 * time.Second):
			log.Debugln("Timeout waiting for recorder exit.")
			a.ffmpeg = nil
		}
	}
}

func (a *Alarm) isMotion() bool {

	resp, err := http.DefaultClient.Get(fmt.Sprintf("http://%s/api.cgi?cmd=GetMdState&channel=0&user=%s&password=%s", a.cfg.Address, a.cfg.User, a.cfg.Password))
	if err != nil {
		log.Errorln(err)
		return false
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Errorln(err)
		return false
	}
	var states MdStateEnvelope
	if err := json.Unmarshal(body, &states); err != nil {
		log.Errorln(err)
		return false
	}
	if len(states) > 0 {
		state := states[0]
		return state.Value.State == 1
	}
	return false

}

func (a *Alarm) isHuman() bool {

	resp, err := http.DefaultClient.Get(fmt.Sprintf("http://%s/api.cgi?cmd=GetAiState&channel=0&user=%s&password=%s", a.cfg.Address, a.cfg.User, a.cfg.Password))
	if err != nil {
		log.Errorln(err)
		return false
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Errorln(err)
		return false
	}
	var states AiStateEnvelope
	if err := json.Unmarshal(body, &states); err != nil {
		log.Errorln(err)
		return false
	}
	if len(states) > 0 {
		state := states[0]
		return state.Value.People.AlarmState == 1
	}
	return false

}
