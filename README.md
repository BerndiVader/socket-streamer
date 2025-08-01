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
  "loglevel": "info",             // debug,info,warn,error,verborse
  "ws_host": "111.111.111.111",   // IP for winsocket server
  "ws_port": 1510,                // Port for winsocket server
  "cameras": [                    // List of IP cams for streaming, tracking and recording
    {
      "name": "UNKNOWN", // Camera description
      "origin": "https://origin.url", // Allowed origin to access the the livestreams
      "ws_path": "/garage_low", // Winsocket path for the livestream & recording
      "ffmpeg_path": "/absolute/path/to/ffmpeg", // Absolute path to ffmpeg executable
      "ffmpeg_params": [          // For custom settings or generic IP cams.
        "-loglevel","warning",
        "-rtsp_transport","tcp",
        "-i","rtsp://user:pass@192.168.x.x:554/h264Preview_01_sub",
        "-map","0:v",
        "-c:v","copy",
        "-f","mpegts"
      ],
      "rtsp_url": "rtsp://user:pass@123.123.123.123:554/h264Preview_01_sub", // RTSP link to cam livestream
      "addr": "123.123.123.123",  // IP adress of cam
      "user": "user",             // Login credentials used for the api calls
      "pass": "pass",             // 
      "tracking": true, // Use tracking and recording on this cam
      "rec_path": "/absolute/path/to/recordings", // Absolute path where the recordings should be stored.
      "md_interval":1, // Interval for simple motion check, must be smaller or equal AI interval
      "ai_interval":2, // Interval to check if the motion is a human/pet/etc...
      "ai_cooldown":3, // Warmup for next AI check if there was a positiv ai check.
      "rec_cooldown":8 // Cooldown for record stop if ai check is false.
    }
  ]
}
```

## Notes
- The program is written for OpenWrt also runs on Linux and Windows
- ffmpeg must be executable and support the mpegts
- Streaming uses the gorilla/websocket library
