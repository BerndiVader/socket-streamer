package alarm

import (
	"bv-streamer/config"
	"bv-streamer/log"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type State int

const (
	STATE_IDLE State = iota
	STATE_ALARM
)

const (
	ALARM_TIMEOUT = 5 * time.Minute
	FFMPEG_LOGS   = "ffmpeg_logs"
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
	stdin  io.WriteCloser
	mu     sync.Mutex
}

func NewAlarm(conf *config.ConfigCamera) *Alarm {

	a := Alarm{
		cfg:             conf,
		state:           STATE_IDLE,
		aiCooldown:      time.Second * 14,
		aiCheckInterval: time.Second * 7,
		mdCheckInterval: time.Second * 3,
		recCooldown:     time.Second * 12,
	}

	if conf.AiCooldown > 0 {
		a.aiCooldown = time.Duration(conf.AiCooldown) * time.Second
	}
	if conf.AiInterval > 0 {
		a.aiCheckInterval = time.Duration(conf.AiInterval) * time.Second
	}
	if conf.MdInterval > 0 {
		a.mdCheckInterval = time.Duration(conf.MdInterval) * time.Second
	}
	if conf.ReCooldown > 0 {
		a.recCooldown = time.Duration(conf.ReCooldown) * time.Second
	}

	return &a
}

func (a *Alarm) Run() {

	if _, err := os.Stat(a.cfg.RecPath); err != nil {
		if err := os.MkdirAll(a.cfg.RecPath, 0755); err != nil {
			log.Errorln("[REC] Failed to create recording dir.")
		} else {
			log.Debugf("[REC] Created recording dir %s.", a.cfg.RecPath)
		}
	}

	go func() {
		for {
			next := time.Now().Add(24 * time.Hour)
			wait := time.Until(time.Date(next.Year(), next.Month(), next.Day(), 0, 30, 0, 0, next.Location()))
			select {
			case <-config.SigShutdown:
				return
			case <-time.After(wait):
				a.dailyMerger()
			}
		}
	}()

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
						log.Infof("[REC] Human detected! -> Change to ALARM.")
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
					log.Debugln("[REC] Still on ALARM.")
				} else if now.Sub(a.lastMotion) > a.recCooldown {
					log.Infoln("[REC] No human detected for cooldown -> back to IDLE.")
					a.state = STATE_IDLE
					a.stopRec()
				} else {
					log.Debugln("[REC] Cooldown running, still recording...")
				}
			}
			log.Debugf("[REC] motion: %v, state: %v", motion, a.state)
		}
	}

}

func (a *Alarm) startRec() {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()
	output := fmt.Sprintf("%s/rec_%s_%d.mp4", a.cfg.RecPath, a.cfg.Name, now.Unix())
	output = strings.ReplaceAll(output, "\\", "/")

	log.Debugf("[REC] Start recording: %s at %s", output, now.Format(time.RFC3339))
	if a.ffmpeg != nil {
		log.Debugln("[REC] Recorder already run")
		return
	}

	a.ffmpeg = exec.Command(
		a.cfg.FFmpegPath,
		"-rtsp_transport", "tcp",
		"-i", a.cfg.RTSPURL,
		"-c", "copy",
		"-f", "mpegts",
		output,
	)

	path := fmt.Sprintf("%s/%s", a.cfg.RecPath, FFMPEG_LOGS)
	if _, err := os.Stat(path); err != nil {
		if err := os.MkdirAll(path, 0755); err != nil {
			log.Errorln(err)
		}
	}

	logfile := fmt.Sprintf("%s/ffmpeg_%s_%d.log", path, a.cfg.Name, now.Unix())
	f, err := os.Create(logfile)
	if err == nil {
		a.ffmpeg.Stderr = f
		defer f.Close()
	} else {
		log.Errorf("[REC] Could not create ffmpeg log file: %s", err)
	}

	a.stdin, _ = a.ffmpeg.StdinPipe()

	if err := a.ffmpeg.Start(); err != nil {
		log.Errorf("[REC] ffmpeg start error: %s", err.Error())
	} else {
		log.Debugln("[REC] ffmpeg record started.")
	}

}

func (a *Alarm) stopRec() {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()
	log.Debugf("[REC] Stop recording at %s", now.Format(time.RFC3339))
	if a.ffmpeg != nil && a.ffmpeg.Process != nil {
		if a.stdin != nil {
			defer a.stdin.Close()
			if _, err := a.stdin.Write([]byte("q\n")); err != nil {
				a.ffmpeg.Process.Kill()
				log.Errorln(err)
			}
		} else {
			a.ffmpeg.Process.Kill()
			log.Errorln("[REC] stdin was not set.")
		}
		done := make(chan error, 1)
		go func() { done <- a.ffmpeg.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				log.Errorf("[REC] Recorder Wait error: %s", err.Error())
				a.ffmpeg.Process.Kill()
			}
			a.ffmpeg = nil
			log.Debugln("[REC] Recording stopped.")
		case <-time.After(5 * time.Second):
			log.Debugln("[REC] Timeout waiting for recorder exit.")
			a.ffmpeg.Process.Kill()
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
