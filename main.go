package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	maxRestarts  = 5
	restartDelay = 3 * time.Second
)

var (
	origin     string = "http://localhost"
	selfIP     string = "111.111.111.111"
	cameraIP   string = "111.111.111.111"
	username   string = "user"
	password   string = "password"
	rtspURL    string
	ffmpegPath string = "ffmpeg"
	quality    string = "sub"
	port       int    = 1510

	mutex   sync.Mutex
	clients = map[*websocket.Conn]bool{}

	ffmpegCmd    *exec.Cmd
	restartCount int

	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return strings.ToLower(r.Header.Get("Origin")) == origin
		},
	}
)

type (
	config struct {
		Origin     string `json:"origin"`
		SelfIP     string `json:"host_ip"`
		CameraIP   string `json:"camera_ip"`
		Username   string `json:"user"`
		Password   string `json:"pass"`
		FFmpegpath string `json:"ffmpeg_path"`
		Quality    string `json:"quality"`
		Port       int    `json:"port"`
	}
)

func main() {
	if cfg, err := loadConfig(); err == nil {
		origin = cfg.Origin
		cameraIP = cfg.CameraIP
		username = cfg.Username
		password = cfg.Password
		ffmpegPath = cfg.FFmpegpath
		quality = cfg.Quality
		selfIP = cfg.SelfIP
		port = cfg.Port
	}

	flag.StringVar(&origin, "origin", origin, "domain origin name")
	flag.StringVar(&cameraIP, "camera_ip", cameraIP, "camera IP address")
	flag.StringVar(&username, "user", username, "camera login name")
	flag.StringVar(&password, "pass", password, "camera user password")
	flag.StringVar(&selfIP, "host_ip", selfIP, "host IP address")
	flag.IntVar(&port, "port", 1510, "websocket server port")
	flag.StringVar(&ffmpegPath, "ffmpeg_path", ffmpegPath, "path to ffmpeg")
	flag.StringVar(&quality, "quality", quality, "quality: high/low")
	flag.Parse()

	if strings.ToLower(quality) == "high" {
		quality = "main"
	} else {
		quality = "sub"
	}

	rtspURL = fmt.Sprintf("rtsp://%s:%s@%s:554/h264Preview_01_%s", username, password, cameraIP, quality)

	http.HandleFunc("/", wsHandler)
	go runFFmpeg()
	println("ws-streamer-go started...")
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
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

	for {
		if _, _, err := conn.NextReader(); err != nil {
			break
		}
	}
}

func runFFmpeg() {
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
			return
		}

		restartCount = 0

		cmd.Wait()

		mutex.Lock()
		needRestart := len(clients) > 0
		mutex.Unlock()

		if needRestart {
			restartCount++
			log.Println("ffmpeg crashed – restart in 3 seconds...")
			time.Sleep(restartDelay)
			continue
		}
	}
}

func startFFmpeg() (io.ReadCloser, io.ReadCloser, error) {
	ffmpegCmd = exec.Command(ffmpegPath,
		"-loglevel", "warning",
		"-rtsp_transport", "tcp",
		"-fflags", "+genpts",
		"-analyzeduration", "500000",
		"-probesize", "512k",
		"-i", rtspURL,
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

	fmt.Printf("ffmpegCmd: %v\n", ffmpegCmd)
	fmt.Println("Starting ffmpeg (PIPE mode)...")
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
	println("PIPE stream started...")

	go func() {
		errbuf := make([]byte, 8*1024)

		for {
			n, err := errpipe.Read(errbuf)
			if err == nil {
				if n > 0 {
					log.Printf("ffmpeg stderr: %s", string(errbuf[:n]))
				}
			}
		}

	}()

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

func loadConfig() (*config, error) {
	exe, _ := os.Executable()
	conf := filepath.Join(filepath.Dir(exe), "ws-streamer.conf")
	conf = strings.ReplaceAll(conf, "\\", "/")

	f, err := os.Open(conf)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var cfg config
	decoder := json.NewDecoder(f)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
