package reo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

func Test_DetectHumanLoop(t *testing.T) {

	data, err := os.ReadFile("camera.conf.test")
	if err != nil {
		t.Fatal(err)
	}

	var c Camera
	if err = json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}

	var (
		aiCooldown      = 90 * time.Second
		lastAICheck     time.Time
		lastAIAlarm     time.Time
		aiCheckInterval = 15 * time.Second
	)

	for {
		motion := c.IsMotion(t)
		now := time.Now()

		if motion {
			if now.Sub(lastAICheck) > aiCheckInterval && now.Sub(lastAIAlarm) > aiCooldown {
				if c.IsHuman(t) {
					fmt.Println("Person erkannt!")
					lastAIAlarm = now
				}
				lastAICheck = now
			}
		}

		t.Log(motion)

		time.Sleep(7 * time.Second)
	}

}

func (c *Camera) IsMotion(t *testing.T) bool {

	resp, err := http.DefaultClient.Get(fmt.Sprintf("http://%s/api.cgi?cmd=GetMdState&channel=0&rs=random123&user=%s&password=%s", c.Address, c.User, c.Pass))
	if err != nil {
		t.Fatal(err)
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}

	var states MdStateEnvelope
	if err := json.Unmarshal(body, &states); err != nil {
		t.Fatal(err)
	}

	if len(states) > 0 {
		state := states[0]
		return state.Value.State == 1
	}
	return false

}

func (c *Camera) IsHuman(t *testing.T) bool {

	return true

}
