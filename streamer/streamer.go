package streamer

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
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
	cfg *config.ConfigCamera

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
		handlers[s.cfg.WSPath] = s.wsHandler
		mutex.Unlock()
		http.HandleFunc(s.cfg.WSPath, s.wsHandler)
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

	Streamers = append(Streamers, s)
	return s

}

func (s *Streamer) Start() {
	s.clients = make(map[*websocket.Conn]bool)

	go s.runFFmpeg()
	log.Println("ws-streamer-go started...")
	go func() {
		log.Println(http.ListenAndServe(fmt.Sprintf("%s:%d", s.cfg.WShost, s.cfg.WSPort), nil))
	}()

	<-s.done
}

func (s *Streamer) Close() {
	log.Printf("Close streamer for cam %s", s.cfg.Name)
	s.unregisterHandler()

	s.mutex.Lock()
	for c := range s.clients {
		c.Close()
		delete(s.clients, c)
	}
	s.clients = nil
	if s.ffmpegCmd != nil {
		log.Println("Close ffmpeg...")
		s.ffmpegCmd.Process.Kill()
	}
	s.mutex.Unlock()
	close(s.done)
}

func (s *Streamer) wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "WebSocket upgrade failed", http.StatusBadRequest)
		log.Println("Upgrade error:", err)
		if conn != nil {
			conn.Close()
		}
		return
	}

	s.mutex.Lock()
	s.clients[conn] = true
	s.mutex.Unlock()

	go s.runner(conn)
}

func (s *Streamer) runner(conn *websocket.Conn) {
	defer func() {
		conn.Close()
		s.mutex.Lock()
		delete(s.clients, conn)
		s.mutex.Unlock()
	}()

	addr := conn.RemoteAddr().String()
	log.Printf("[%s]client-runner start.\n", addr)
	for {
		select {
		case <-s.done:
			log.Printf("[%s]client-runner stop.\n", addr)
			return
		default:
			if _, _, err := conn.NextReader(); err != nil {
				log.Printf("[%s]client-runner stop.\n", addr)
				return
			}

		}
	}
}

func (s *Streamer) runFFmpeg() {
	log.Println("ffmpeg-runner start.")
	for {
		select {
		case <-s.done:
			log.Println("ffmpeg-runner stop (shutdown signal).")
			return
		default:
			s.mutex.Lock()
			needStart := len(s.clients) != 0 && s.ffmpegCmd == nil
			needStop := len(s.clients) == 0 && s.ffmpegCmd != nil
			s.mutex.Unlock()

			if needStart {
				stdout, stderr, err := s.startFFmpeg()
				if err != nil {
					time.Sleep(restartDelay)
					continue
				}
				go s.runPipeStream(stdout, stderr)
			} else if needStop {
				s.stopFFmpeg()
			}

			s.mutex.Lock()
			cmd := s.ffmpegCmd
			s.mutex.Unlock()

			if cmd == nil {
				time.Sleep(500 * time.Millisecond)
				continue
			}

			if s.restartCount >= maxRestarts {
				break
			}
			s.restartCount = 0

			err := cmd.Wait()

			s.mutex.Lock()
			needRestart := len(s.clients) > 0
			s.mutex.Unlock()

			if needRestart {
				s.restartCount++
				if err != nil {
					log.Printf("ffmpeg crashed %s – restart in 3 seconds...\n", err.Error())
				} else {
					log.Println("ffmpeg exited for unkown reason – restart in 3 seconds...")
				}
				time.Sleep(restartDelay)
				continue
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (s *Streamer) startFFmpeg() (io.ReadCloser, io.ReadCloser, error) {
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
		log.Println("Error creating StdoutPipe:", err)
		s.ffmpegCmd = nil
		return nil, nil, err
	}

	log.Println("Starting ffmpeg (PIPE mode)...")
	if err := s.ffmpegCmd.Start(); err != nil {
		log.Println("Error starting ffmpeg:", err)
		s.ffmpegCmd = nil
		return nil, nil, err
	}
	return stdout, stderr, nil
}

func (s *Streamer) runPipeStream(pipe io.ReadCloser, errpipe io.ReadCloser) {
	defer func() {
		pipe.Close()
		errpipe.Close()
	}()

	buf := make([]byte, 8*1024)
	log.Println("PIPE stream started...")

	go func() {
		errbuf := make([]byte, 8*1024)

		log.Println("ffmpeg-errorpipe-runner start.")
		for {
			select {
			case <-s.done:
				log.Println("ffmpeg-errorpipe-runner stop.")
				return
			default:
				n, err := errpipe.Read(errbuf)
				if err == nil {
					if n > 0 {
						log.Printf("ffmpeg stderr: %s", string(errbuf[:n]))
					}
				} else {
					log.Println("ffmpeg-errorpipe-runner stop.")
					return
				}
			}
		}

	}()

	log.Println("ffmpeg-streampipe-runner start.")
	for {
		select {
		case <-s.done:
			log.Println("ffmpeg-streampipe-runner stop.")
			return
		default:
			n, err := pipe.Read(buf)
			if err != nil {
				log.Println("Error reading from ffmpeg PIPE:", err)
				s.mutex.Lock()
				for client := range s.clients {
					client.Close()
					delete(s.clients, client)
				}
				s.mutex.Unlock()
				log.Println("ffmpeg-streampipe-runner stop.")
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

			s.mutex.Lock()
			killffmpeg := len(conns) == 0 && s.ffmpegCmd != nil
			s.mutex.Unlock()

			if killffmpeg {
				s.stopFFmpeg()
			}

		}
	}

}

func (s *Streamer) stopFFmpeg() {
	s.mutex.Lock()
	if s.ffmpegCmd != nil {
		log.Println("Stopping ffmpeg (no clients left)...")
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
