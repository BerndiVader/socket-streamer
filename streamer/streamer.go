package streamer

import (
	"io"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
	"ws-streamer/alarm"
	"ws-streamer/config"

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
	log.Printf("[%s]Starting streamer...\n", s.cfg.Name)

	s.clients = make(map[*websocket.Conn]bool)
	go s.ffmpegRunner()
	<-s.done
}

func (s *Streamer) Close() {
	log.Printf("[%s]Closing streamer...\n", s.cfg.Name)
	s.unregisterHandler()

	s.mutex.Lock()
	for c := range s.clients {
		c.Close()
		delete(s.clients, c)
	}
	s.clients = nil
	s.mutex.Unlock()

	if s.ffmpegCmd != nil {
		log.Printf("[%s]Terminate ffmpeg-process.\n", s.cfg.Name)
		s.mutex.Lock()
		err := s.ffmpegCmd.Process.Kill()
		s.mutex.Unlock()
		if err != nil {
			log.Printf("[%s]Failed to terminate ffmpeg-process: %v\n", s.cfg.Name, err)
		}

	}
	close(s.done)
}

func (s *Streamer) handler(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "WebSocket upgrade failed", http.StatusBadRequest)
		log.Printf("[%s]Upgrade error: %v", s.cfg.Name, err)
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
	log.Printf("[%s]client-runner start. [%s]\n", s.cfg.Name, addr)
	for {
		select {
		case <-s.done:
			log.Printf("[%s]client-runner stop. [%s]\n", s.cfg.Name, addr)
			return
		default:
			if _, _, err := conn.NextReader(); err != nil {
				log.Printf("[%s]client-runner stop by error - %v - [%s]\n", s.cfg.Name, err, addr)
				return
			}

		}
	}
}

func (s *Streamer) ffmpegRunner() {

	log.Printf("[%s]ffmpeg-runner start.\n", s.cfg.Name)
	ffmpegDone := make(chan error, 1)
	var ffmpegRunning bool

	for {

		s.mutex.Lock()
		hasClients := len(s.clients) > 0
		s.mutex.Unlock()

		select {
		case <-s.done:
			log.Printf("[%s]ffmpeg-runner stop (shutdown signal).\n", s.cfg.Name)
			s.destroyFFmpeg()
			if ffmpegRunning {
				<-ffmpegDone
			}
			return
		case err := <-ffmpegDone:
			log.Printf("[%s]ffmpeg exited: %v\n", s.cfg.Name, err)
			ffmpegRunning = false
			s.mutex.Lock()
			s.ffmpegCmd = nil
			s.mutex.Unlock()
			if hasClients {
				log.Printf("[%s]Restarting ffmpeg in %v...\n", s.cfg.Name, restartDelay)
				time.Sleep(restartDelay)
			}
		default:
			switch {
			case !ffmpegRunning && hasClients:
				stdout, stderr, err := s.createFFmpeg()
				if err != nil {
					log.Printf("[%s]Failed to start ffmpeg: %v\n", s.cfg.Name, err)
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
				log.Printf("[%s]ffmpeg started.\n", s.cfg.Name)
			case ffmpegRunning && !hasClients:
				log.Printf("[%s]No clients left, stopping ffmpeg...\n", s.cfg.Name)
				s.destroyFFmpeg()
				ffmpegRunning = false
			default:
				time.Sleep(500 * time.Millisecond)
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
		log.Printf("[%s]Error creating StdoutPipe: %v\n", s.cfg.Name, err)
		s.ffmpegCmd = nil
		return nil, nil, err
	}

	log.Printf("[%s]Starting ffmpeg (PIPE mode)...\n", s.cfg.Name)
	if err := s.ffmpegCmd.Start(); err != nil {
		log.Printf("[%s]Error starting ffmpeg: %v\n", s.cfg.Name, err)
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
	log.Printf("[%s]pipe-runner started...\n", s.cfg.Name)

	go func() {
		errbuf := make([]byte, 8*1024)

		log.Printf("[%s]ffmpeg-errorpipe-runner start.\n", s.cfg.Name)
		for {
			select {
			case <-s.done:
				log.Printf("[%s]ffmpeg-errorpipe-runner stop.\n", s.cfg.Name)
				return
			default:
				n, err := errpipe.Read(errbuf)
				if err == nil {
					if n > 0 {
						log.Printf("[%s]ffmpeg stderr: %s\n", s.cfg.Name, string(errbuf[:n]))
					}
				} else {
					log.Printf("[%s]ffmpeg-errorpipe-runner stop.\n", s.cfg.Name)
					return
				}
			}
		}

	}()

	log.Printf("[%s]ffmpeg-streampipe-runner start.\n", s.cfg.Name)
	for {
		select {
		case <-s.done:
			log.Printf("[%s]ffmpeg-streampipe-runner stop.\n", s.cfg.Name)
			return
		default:
			n, err := streampipe.Read(buf)
			if err != nil {
				log.Printf("[%s]Error reading from ffmpeg PIPE: %s\n", s.cfg.Name, err)
				s.mutex.Lock()
				for client := range s.clients {
					client.Close()
					delete(s.clients, client)
				}
				s.mutex.Unlock()
				log.Printf("[%s]ffmpeg-streampipe-runner stop.\n", s.cfg.Name)
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
	if s.ffmpegCmd != nil {
		log.Printf("[%s]Destroy ffmpeg process...\n", s.cfg.Name)
		s.ffmpegCmd.Process.Kill()
		s.ffmpegCmd = nil
		s.restartCount = 0
	}
	s.mutex.Unlock()
}

func (s *Streamer) shutdownHandler() {
	s.done = make(chan struct{})

	go func() {
		<-config.SigShutdown
		s.Close()
	}()
}
