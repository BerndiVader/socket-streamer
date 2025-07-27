package streamer

import (
	"bv-streamer/alarm"
	"bv-streamer/config"
	"bv-streamer/log"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	maxRestarts  int           = 5
	restartDelay time.Duration = 3 * time.Second
)

var (
	mutex     sync.Mutex
	handlers  = make(map[string]http.HandlerFunc)
	Streamers = make([]*Streamer, 0)
)

type Streamer struct {
	cfg   *config.ConfigCamera
	alarm *alarm.Alarm

	upgrader websocket.Upgrader

	mutex        sync.Mutex
	ffmpegCmd    *exec.Cmd
	restartCount int
	clients      map[*websocket.Conn]bool
	done         chan struct{}
}

func (s *Streamer) registerHandler() bool {
	if _, found := handlers[s.cfg.WSPath]; !found {
		mutex.Lock()
		handlers[s.cfg.WSPath] = s.handler
		mutex.Unlock()
		http.HandleFunc(s.cfg.WSPath, s.handler)
		return true
	}
	return false
}

func (s *Streamer) unregisterHandler() {
	if _, found := handlers[s.cfg.WSPath]; found {
		mutex.Lock()
		delete(handlers, s.cfg.WSPath)
		mutex.Unlock()
	}
}

func NewStreamer(c *config.ConfigCamera) *Streamer {
	s := &Streamer{cfg: c}

	if !s.registerHandler() {
		return nil
	}
	s.shutdownHandler()
	s.upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return strings.ToLower(r.Header.Get("Origin")) == s.cfg.Origin
		},
	}

	if s.cfg.Tracking {
		s.alarm = alarm.NewAlarm(s.cfg)
		go s.alarm.Run()
	}

	Streamers = append(Streamers, s)
	return s

}

func (s *Streamer) Start() {
	log.Infof("[%s] Starting streamer...\n", s.cfg.Name)

	s.clients = make(map[*websocket.Conn]bool)
	go s.ffmpegRunner()
	<-s.done
}

func (s *Streamer) Close() {
	log.Infof("[%s] Closing streamer...\n", s.cfg.Name)
	s.unregisterHandler()

	s.mutex.Lock()
	for c := range s.clients {
		c.Close()
		delete(s.clients, c)
	}
	s.clients = nil
	s.mutex.Unlock()

	s.destroyFFmpeg()
	close(s.done)
}

func (s *Streamer) handler(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "WebSocket upgrade failed", http.StatusBadRequest)
		log.Errorf("[%s] Upgrade error: %v", s.cfg.Name, err)
		if conn != nil {
			conn.Close()
		}
		return
	}

	s.mutex.Lock()
	s.clients[conn] = true
	s.mutex.Unlock()

	go s.clientRunner(conn)
}

func (s *Streamer) clientRunner(conn *websocket.Conn) {
	defer func() {
		conn.Close()
		s.mutex.Lock()
		delete(s.clients, conn)
		s.mutex.Unlock()
	}()

	addr := conn.RemoteAddr().String()
	log.Infof("[%s] Client-runner start. [%s]", s.cfg.Name, addr)
	for {
		if _, _, err := conn.NextReader(); err != nil {
			log.Errorf("[%s] Client-runner stop by error - %v - [%s]", s.cfg.Name, err, addr)
			return
		}
		select {
		case <-s.done:
			log.Infof("[%s] Client-runner stop. [%s]", s.cfg.Name, addr)
			return
		default:
			continue
		}
	}
}

func (s *Streamer) ffmpegRunner() {

	log.Infof("[%s] FFmpeg-runner start.", s.cfg.Name)
	ffmpegDone := make(chan error, 1)
	var ffmpegRunning bool

	for {

		s.mutex.Lock()
		hasClients := len(s.clients) > 0
		s.mutex.Unlock()

		select {
		case <-s.done:
			log.Infof("[%s] FFmpeg-runner stop (shutdown signal).", s.cfg.Name)
			s.destroyFFmpeg()
			if ffmpegRunning {
				<-ffmpegDone
			}
			return
		case err := <-ffmpegDone:
			log.Errorf("[%s] FFmpeg exited: %v", s.cfg.Name, err)
			ffmpegRunning = false
			s.mutex.Lock()
			s.ffmpegCmd = nil
			s.mutex.Unlock()
			if hasClients {
				log.Infof("[%s] Restarting ffmpeg in %v...", s.cfg.Name, restartDelay)
				time.Sleep(restartDelay)
			}
		case <-time.After(1 * time.Second):
			switch {
			case !ffmpegRunning && hasClients:
				stdout, stderr, err := s.createFFmpeg()
				if err != nil {
					log.Errorf("[%s] Failed to start ffmpeg: %v", s.cfg.Name, err)
					time.Sleep(restartDelay)
					continue
				}
				go s.pipeRunner(stdout, stderr)
				s.mutex.Lock()
				cmd := s.ffmpegCmd
				s.mutex.Unlock()
				ffmpegRunning = true
				go func() {
					ffmpegDone <- cmd.Wait()
				}()
				log.Debugf("[%s] FFmpeg started.", s.cfg.Name)
			case ffmpegRunning && !hasClients:
				log.Infof("[%s] No clients left, stopping ffmpeg...", s.cfg.Name)
				s.destroyFFmpeg()
				ffmpegRunning = false
			}
		}
	}
}

func (s *Streamer) createFFmpeg() (io.ReadCloser, io.ReadCloser, error) {
	s.ffmpegCmd = exec.Command(s.cfg.FFmpegPath,
		"-loglevel", "warning",
		"-rtsp_transport", "tcp",
		"-fflags", "+genpts",
		"-analyzeduration", "500000",
		"-probesize", "512k",
		"-i", s.cfg.RTSPURL,
		"-map", "0:v",
		"-c:v", "copy",
		"-f", "mpegts",
		"pipe:1",
	)

	stdout, err := s.ffmpegCmd.StdoutPipe()
	stderr, _ := s.ffmpegCmd.StderrPipe()
	if err != nil {
		log.Errorf("[%s] Error creating StdoutPipe: %v", s.cfg.Name, err)
		s.ffmpegCmd = nil
		return nil, nil, err
	}

	log.Infof("[%s] Starting ffmpeg (PIPE mode)...", s.cfg.Name)
	if err := s.ffmpegCmd.Start(); err != nil {
		log.Errorf("[%s] Error starting ffmpeg: %v", s.cfg.Name, err)
		s.ffmpegCmd = nil
		return nil, nil, err
	}
	return stdout, stderr, nil
}

func (s *Streamer) pipeRunner(streampipe io.ReadCloser, errpipe io.ReadCloser) {
	defer func() {
		streampipe.Close()
		errpipe.Close()
	}()

	buf := make([]byte, 8*1024)
	log.Infof("[%s] Pipe-runner started...", s.cfg.Name)

	go func() {
		errbuf := make([]byte, 8*1024)

		log.Infof("[%s] FFmpeg-errorpipe-runner start.", s.cfg.Name)
		for {
			select {
			case <-s.done:
				log.Infof("[%s] FFmpeg-errorpipe-runner stop.", s.cfg.Name)
				return
			default:
				n, err := errpipe.Read(errbuf)
				if err == nil {
					if n > 0 {
						log.Infof("[%s] FFmpeg stderr: %s", s.cfg.Name, string(errbuf[:n]))
					}
				} else {
					log.Debugf("[%s] FFmpeg-errorpipe-runner stop.", s.cfg.Name)
					return
				}
			}
		}

	}()

	log.Infof("[%s] FFmpeg-streampipe-runner start.", s.cfg.Name)
	for {
		select {
		case <-s.done:
			log.Infof("[%s] FFmpeg-streampipe-runner stop.", s.cfg.Name)
			return
		default:
			n, err := streampipe.Read(buf)
			if err != nil {
				log.Errorf("[%s] Error reading from ffmpeg PIPE: %v", s.cfg.Name, err)
				s.mutex.Lock()
				for client := range s.clients {
					client.Close()
					delete(s.clients, client)
				}
				s.mutex.Unlock()
				log.Infof("[%s] FFmpeg-streampipe-runner stop.", s.cfg.Name)
				return
			}

			s.mutex.Lock()
			conns := make([]*websocket.Conn, 0, len(s.clients))
			for client := range s.clients {
				conns = append(conns, client)
			}
			s.mutex.Unlock()

			for _, conn := range conns {
				if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
					s.mutex.Lock()
					conn.Close()
					delete(s.clients, conn)
					s.mutex.Unlock()
				}
			}

		}
	}

}

func (s *Streamer) destroyFFmpeg() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.ffmpegCmd != nil {
		log.Infof("[%s] Destroy ffmpeg process...", s.cfg.Name)
		if err := s.ffmpegCmd.Process.Kill(); err != nil {
			log.Errorf("[%s] Failed to terminate ffmpeg-process: %v", s.cfg.Name, err)
		}
		s.ffmpegCmd = nil
		s.restartCount = 0
	}
}

func (s *Streamer) shutdownHandler() {
	s.done = make(chan struct{})

	go func() {
		<-config.SigShutdown
		s.Close()
	}()
}
