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

	var c = Alarm{
		cfg:             &conf,
		state:           STATE_IDLE,
		aiCooldown:      12 * time.Second,
		aiCheckInterval: 4 * time.Second,
		mdCheckInterval: 2 * time.Second,
		recCooldown:     10 * time.Second,
	}

	c.Run(t)

}

func (c *Alarm) Run(t *testing.T) {

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-time.After(5 * time.Minute):
			c.stopRec(t)
			return
		case <-sigs:
			c.stopRec(t)
			return
		default:
			motion := c.isMotion(t)
			now := time.Now()

			switch c.state {
			case STATE_IDLE:
				if motion && now.Sub(c.lastAICheck) > c.aiCheckInterval && now.Sub(c.lastAIAlarm) > c.aiCooldown {
					if c.isHuman(t) {
						t.Log("Human detected! -> change to ALARM.")
						c.state = STATE_ALARM
						c.alarmStart = now
						c.lastAIAlarm = now
						c.lastMotion = now
						c.startRec(t)
					}
					c.lastAICheck = now
				}
			case STATE_ALARM:
				if c.isHuman(t) {
					c.lastMotion = now
					t.Log("Still on ALARM.")
				} else if now.Sub(c.lastMotion) > c.recCooldown {
					t.Log("No human detected for cooldown -> back to IDLE.")
					c.state = STATE_IDLE
					c.stopRec(t)
				} else {
					t.Log("Cooldown running, still recording...")
				}
			}

			t.Logf("motion: %v, state: %v", motion, c.state)
			time.Sleep(c.mdCheckInterval)
		}
	}
}

func (a *Alarm) startRec(t *testing.T) {
	t.Log("starting....")
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
	t.Log("stopping....")
	if a.ffmpeg != nil && a.ffmpeg.Process != nil {
		a.ffmpeg.Process.Kill()
		a.ffmpeg = nil
		t.Log("Recording stopped")
	}
}

func (c *Alarm) isMotion(t *testing.T) bool {

	resp, err := http.DefaultClient.Get(fmt.Sprintf("http://%s/api.cgi?cmd=GetMdState&channel=0&rs=random123&user=%s&password=%s", c.cfg.Address, c.cfg.User, c.cfg.Password))
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

func (c *Alarm) isHuman(t *testing.T) bool {

	resp, err := http.DefaultClient.Get(fmt.Sprintf("http://%s/api.cgi?cmd=GetAiState&channel=0&rs=random123&user=%s&password=%s", c.cfg.Address, c.cfg.User, c.cfg.Password))
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
