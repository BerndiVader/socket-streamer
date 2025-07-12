package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
	"ws-streamer/config"
	"ws-streamer/onvif"

	"github.com/gorilla/websocket"
)

const (
	maxRestarts  = 5
	restartDelay = 3 * time.Second
)

var (
	cfg    *config.Config
	camera *onvif.Camera

	mutex   sync.Mutex
	clients = map[*websocket.Conn]bool{}

	ffmpegCmd    *exec.Cmd
	restartCount int

	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return strings.ToLower(r.Header.Get("Origin")) == cfg.Origin
		},
	}
	done = make(chan struct{})
)

func main() {
	log.SetPrefix("[ws-streamer]")
	log.SetFlags(log.LstdFlags)
	ShutdownHandler()

	cfg = config.NewConfig()

	http.HandleFunc("/", wsHandler)
	go runFFmpeg()
	log.Println("ws-streamer-go started...")
	go func() {
		log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", cfg.Port), nil))
	}()

	<-done
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "WebSocket upgrade failed", http.StatusBadRequest)
		log.Println("Upgrade error:", err)
		if conn != nil {
			conn.Close()
		}
		return
	}

	mutex.Lock()
	clients[conn] = true
	mutex.Unlock()

	go handleClient(conn)
}

func handleClient(conn *websocket.Conn) {
	defer func() {
		conn.Close()
		mutex.Lock()
		delete(clients, conn)
		mutex.Unlock()
	}()

	addr := conn.RemoteAddr().String()
	log.Printf("[%s]client-runner start.\n", addr)
	for {
		if _, _, err := conn.NextReader(); err != nil {
			break
		}
	}
	log.Printf("[%s]client-runner stop.\n", addr)
}

func runFFmpeg() {
	log.Println("ffmpeg-runner start.")
	for {
		mutex.Lock()
		needStart := len(clients) != 0 && ffmpegCmd == nil
		needStop := len(clients) == 0 && ffmpegCmd != nil
		mutex.Unlock()

		if needStart {
			stdout, stderr, err := startFFmpeg()
			if err != nil {
				time.Sleep(restartDelay)
				continue
			}
			go runPipeStream(stdout, stderr)
		} else if needStop {
			stopFFmpeg()
		}

		mutex.Lock()
		cmd := ffmpegCmd
		mutex.Unlock()

		if cmd == nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		if restartCount >= maxRestarts {
			break
		}

		restartCount = 0

		err := cmd.Wait()

		mutex.Lock()
		needRestart := len(clients) > 0
		mutex.Unlock()

		if needRestart {
			restartCount++
			if err != nil {
				log.Printf("ffmpeg crashed %s – restart in 3 seconds...\n", err.Error())
			} else {
				log.Println("ffmpeg exited for unkown reason – restart in 3 seconds...")
			}
			time.Sleep(restartDelay)
			continue
		}
	}
	log.Println("ffmpeg-runner stop.")
}

func startFFmpeg() (io.ReadCloser, io.ReadCloser, error) {
	ffmpegCmd = exec.Command(cfg.FFmpegPath,
		"-loglevel", "warning",
		"-rtsp_transport", "tcp",
		"-fflags", "+genpts",
		"-analyzeduration", "500000",
		"-probesize", "512k",
		"-i", cfg.RTSPURL,
		"-map", "0:v",
		"-c:v", "copy",
		"-f", "mpegts",
		"pipe:1",
	)

	stdout, err := ffmpegCmd.StdoutPipe()
	stderr, _ := ffmpegCmd.StderrPipe()
	if err != nil {
		log.Println("Error creating StdoutPipe:", err)
		ffmpegCmd = nil
		return nil, nil, err
	}

	log.Println("Starting ffmpeg (PIPE mode)...")
	if err := ffmpegCmd.Start(); err != nil {
		log.Println("Error starting ffmpeg:", err)
		ffmpegCmd = nil
		return nil, nil, err
	}
	return stdout, stderr, nil
}

func runPipeStream(pipe io.ReadCloser, errpipe io.ReadCloser) {
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
			n, err := errpipe.Read(errbuf)
			if err == nil {
				if n > 0 {
					log.Printf("ffmpeg stderr: %s", string(errbuf[:n]))
				}
			} else {
				break
			}
		}

		log.Println("ffmpeg-errorpipe-runner stop.")

	}()

	log.Println("ffmpeg-streampipe-runner start.")
	for {
		n, err := pipe.Read(buf)
		if err != nil {
			log.Println("Error reading from ffmpeg PIPE:", err)
			mutex.Lock()
			for client := range clients {
				client.Close()
				delete(clients, client)
			}
			mutex.Unlock()
			break
		}

		mutex.Lock()
		conns := make([]*websocket.Conn, 0, len(clients))
		for client := range clients {
			conns = append(conns, client)
		}
		mutex.Unlock()

		for _, conn := range conns {
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				mutex.Lock()
				conn.Close()
				delete(clients, conn)
				mutex.Unlock()
			}
		}

		mutex.Lock()
		killffmpeg := len(conns) == 0 && ffmpegCmd != nil
		mutex.Unlock()

		if killffmpeg {
			stopFFmpeg()
		}
	}

	log.Println("ffmpeg-streampipe-runner stop.")
}

func stopFFmpeg() {
	mutex.Lock()
	if ffmpegCmd != nil {
		log.Println("Stopping ffmpeg (no clients left)...")
		ffmpegCmd.Process.Kill()
		ffmpegCmd = nil
		restartCount = 0
	}
	mutex.Unlock()
}

func ShutdownHandler() {

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigs
		log.Println("Shutting down ws-streamer-go...")
		log.Println("Close all connected clients...")
		mutex.Lock()
		for c := range clients {
			delete(clients, c)
		}
		if ffmpegCmd != nil {
			log.Println("Shutting down ffmpeg...")
			ffmpegCmd.Process.Kill()
		}
		mutex.Unlock()
		if camera != nil {
			camera.Stop()
		}
		close(done)
	}()
}
