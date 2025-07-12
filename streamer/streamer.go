package streamer

import (
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
	"ws-streamer/config"
	"ws-streamer/onvif"

	"github.com/gorilla/websocket"
)

const (
	maxRestarts  int           = 5
	restartDelay time.Duration = 3 * time.Second
)

var (
	Clients = map[*websocket.Conn]bool{}
)

type Streamer struct {
	cfg      *config.Config
	upgrader websocket.Upgrader

	mutex sync.Mutex

	ffmpegCmd    *exec.Cmd
	restartCount int
	ofiv         *onvif.Camera
}

func NewStreamer(c *config.Config) *Streamer {
	s := &Streamer{cfg: c}
	s.upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return strings.ToLower(r.Header.Get("Origin")) == s.cfg.Origin
		},
	}

	http.HandleFunc(s.cfg.CamPath, s.wsHandler)

	return s
}

func (s *Streamer) wsHandler(w http.ResponseWriter, r *http.Request) {

}
