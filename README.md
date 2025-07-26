# bv-streamer

## Overview

**bv-streamer** is a lightweight auto-recording and auto-streaming app for IP cams on OpenWrt or Pi's. Used on autark 12V systems with a consumption of 11Watts (2 cameras, router and modem). It detects motion and AI events (person, vehicle, animal, face) and records by fly. Saves are corrupt and fault safe. There is also a listener on WS for livestreaming.

## Installation
2. Requires ffmpeg installed.
3. Clone the project:
   ```
   git clone
   cd bv-streamer
   go build -o bv-streamer
   ```
4. Adjust configuration (`bv-streamer.conf`)

## Usage
- Start:
  ```
  ./bv-streamer
  ```
  Or run it as service.
- Connect WebSocket client:
  - e.g. with a frontend or `websocat`
- Records are saved in the configured dir

## Configuration
The file `bv-streamer.conf` contains all relevant settings:
- Camera IP, RTSP URL, user/password
- Recording directory
- ffmpeg path
- Cooldown and interval times

## Example configuration
```json
{
  "loglevel": "info",
  "ws_host": "192.168.x.x",
  "ws_port": 1510,
  "cameras": [
    {
      "name": "Cam1_High",
      "origin": "https://accepted.origin",
      "ws_path": "/cam1_high",
      "ffmpeg_path": "absolute/path/to/ffmpeg.exe",
      "rtsp_url": "rtsp://user:pass@192.168.x.x:554/h264Preview_01_sub",
      "addr": "192.168.x.x",
      "user": "user",
      "pass": "pass",
      "tracking": true,
      "rec_path": "absolute/path/to/records/cam1_high"
    }
  ]
}
```

## Notes
- The program is written for OpenWrt also runs on Linux and Windows
- ffmpeg must be executable and support the mpegts
- Streaming uses the gorilla/websocket library
