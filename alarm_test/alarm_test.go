package alarm_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"ws-streamer/alarm"
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
	lastMotion      time.Time
	aiCooldown      time.Duration
	recCooldown     time.Duration
	aiCheckInterval time.Duration
	mdCheckInterval time.Duration

	ffmpeg *exec.Cmd
	mu     sync.Mutex
}

func Test_DetectHuman(t *testing.T) {

	data, err := os.ReadFile("camera.conf.test")
	if err != nil {
		t.Fatal(err)
	}

	var conf config.ConfigCamera
	if err = json.Unmarshal(data, &conf); err != nil {
		t.Fatal(err)
	}

	var a = Alarm{
		cfg:             &conf,
		state:           STATE_IDLE,
		aiCooldown:      14 * time.Second,
		aiCheckInterval: 7 * time.Second,
		mdCheckInterval: 3 * time.Second,
		recCooldown:     12 * time.Second,
	}

	a.Run(t)

}

func (a *Alarm) Run(t *testing.T) {

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-time.After(ALARM_TIMEOUT):
			a.stopRec(t)
			return
		case <-sigs:
			a.stopRec(t)
			return
		default:
			motion := a.isMotion(t)
			now := time.Now()

			switch a.state {
			case STATE_IDLE:
				if motion && now.Sub(a.lastAICheck) > a.aiCheckInterval && now.Sub(a.lastAIAlarm) > a.aiCooldown {
					if a.isHuman(t) {
						t.Log("Human detected! -> Change to ALARM.")
						a.state = STATE_ALARM
						a.alarmStart = now
						a.lastAIAlarm = now
						a.lastMotion = now
						a.stopRec(t)
						a.startRec(t)
					}
					a.lastAICheck = now
				}
			case STATE_ALARM:
				if a.isHuman(t) {
					a.lastMotion = now
					t.Log("Still on ALARM.")
				} else if now.Sub(a.lastMotion) > a.recCooldown {
					t.Log("No human detected for cooldown -> back to IDLE.")
					a.state = STATE_IDLE
					a.stopRec(t)
				} else {
					t.Log("Cooldown running, still recording...")
				}
			}

			t.Logf("motion: %v, state: %v", motion, a.state)
			time.Sleep(a.mdCheckInterval)
		}
	}

}

func (a *Alarm) startRec(t *testing.T) {
	a.mu.Lock()
	defer a.mu.Unlock()

	t.Log("Start recording...")
	if a.ffmpeg != nil {
		t.Log("Recorder already run")
		return
	}

	output := fmt.Sprintf("%s/rec_%s_%d.mp4", a.cfg.RecPath, a.cfg.Name, time.Now().Unix())
	output = strings.ReplaceAll(output, "\\", "/")

	t.Log(output)

	a.ffmpeg = exec.Command(
		a.cfg.FFmpegPath,
		"-rtsp_transport", "tcp",
		"-i", a.cfg.RTSPURL,
		"-c", "copy",
		"-f", "mpegts",
		output,
	)

	if err := a.ffmpeg.Start(); err != nil {
		t.Log(err.Error())
	} else {
		t.Log("ffmpeg record started.")
	}

}

func (a *Alarm) stopRec(t *testing.T) {
	a.mu.Lock()
	defer a.mu.Unlock()

	t.Log("Stop recording....")
	if a.ffmpeg != nil && a.ffmpeg.Process != nil {
		a.ffmpeg.Process.Kill()
		done := make(chan error, 1)
		go func() { done <- a.ffmpeg.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				t.Logf("Recorder Wait error: %s", err.Error())
			}
			a.ffmpeg = nil
			t.Log("Recording stopped.")
		case <-time.After(5 * time.Second):
			t.Log("Timeout waiting for recorder exit.")
			a.ffmpeg = nil
		}
	}
}

func (a *Alarm) isMotion(t *testing.T) bool {

	resp, err := http.DefaultClient.Get(fmt.Sprintf("http://%s/api.cgi?cmd=GetMdState&channel=0&user=%s&password=%s", a.cfg.Address, a.cfg.User, a.cfg.Password))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var states alarm.MdStateEnvelope
	if err := json.Unmarshal(body, &states); err != nil {
		t.Fatal(err)
	}
	if len(states) > 0 {
		state := states[0]
		return state.Value.State == 1
	}
	return false

}

func (a *Alarm) isHuman(t *testing.T) bool {

	resp, err := http.DefaultClient.Get(fmt.Sprintf("http://%s/api.cgi?cmd=GetAiState&channel=0&user=%s&password=%s", a.cfg.Address, a.cfg.User, a.cfg.Password))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var states alarm.AiStateEnvelope
	if err := json.Unmarshal(body, &states); err != nil {
		t.Fatal(err)
	}
	if len(states) > 0 {
		state := states[0]
		return state.Value.People.AlarmState == 1
	}
	return false

}
