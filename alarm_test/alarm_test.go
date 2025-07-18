package alarm_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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
	aiCooldown      time.Duration
	aiCheckInterval time.Duration
	mdCheckInterval time.Duration
	test_Timeout    chan time.Time
	StopRecorder    chan struct{}
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
		aiCooldown:      20 * time.Second,
		aiCheckInterval: 10 * time.Second,
		mdCheckInterval: 5 * time.Second,
	}

	c.Run(t)

}

func (c *Alarm) Run(t *testing.T) {
	for {
		select {
		case <-c.test_Timeout:
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
					}
					c.lastAICheck = now
				}
			case STATE_ALARM:
				if !c.isHuman(t) {
					t.Log("No human detected! -> back to IDLE.")
					c.state = STATE_IDLE
				} else if now.Sub(c.alarmStart) > ALARM_TIMEOUT {
					t.Log("Human detected but alarm timeout! -> back to IDLE.")
					c.state = STATE_IDLE
				} else {
					t.Log("Still on ALARM.")
				}
			}

			t.Logf("motion: %v, state: %v", motion, c.state)
			time.Sleep(c.mdCheckInterval)
		}
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
