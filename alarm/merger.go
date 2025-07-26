package alarm

import (
	"bv-streamer/config"
	"bv-streamer/log"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const DATE_FORMAT string = "2006-01-02"

func (a *Alarm) dailyMerger() {
	if entries, err := os.ReadDir(a.cfg.RecPath); err == nil {

		entries = filter(entries, func(e os.DirEntry) bool {
			info, err := e.Info()
			if err == nil {
				if info.ModTime().Format(DATE_FORMAT) == time.Now().Format(DATE_FORMAT) {
					return false
				}
			}
			return !e.IsDir() && strings.HasSuffix(e.Name(), ".mp4")
		})

		sort.Slice(entries, func(a, b int) bool {
			if timeA, err := entries[a].Info(); err == nil {
				if timeB, err := entries[b].Info(); err == nil {
					x := timeA.ModTime().Compare(timeB.ModTime())
					return x < 0
				}
			}
			return entries[a].Name() < entries[b].Name()
		})

		for len(entries) > 0 {
			entries = merge(entries, a.cfg)
		}

	}

}

func filter(entries []os.DirEntry, keep func(os.DirEntry) bool) []os.DirEntry {
	var filtred []os.DirEntry

	for _, de := range entries {
		if keep(de) {
			filtred = append(filtred, de)
		}

	}

	return filtred
}

func merge(entries []os.DirEntry, cfg *config.ConfigCamera) []os.DirEntry {
	var curr time.Time
	var merges []string
	var filtred []os.DirEntry

	for _, merger := range entries {
		if info, err := merger.Info(); err == nil {
			if curr.IsZero() {
				curr = info.ModTime()
			}
			switch curr.Format(DATE_FORMAT) == info.ModTime().Format(DATE_FORMAT) {
			case true:
				if info.Size() > 0 {
					merges = append(merges, merger.Name())
				}
			case false:
				filtred = append(filtred, merger)
			}
		}
	}

	arcPath := filepath.Join(cfg.RecPath, "archive")
	if _, err := os.Stat(arcPath); err != nil {
		if err := os.MkdirAll(arcPath, 0775); err != nil {
			log.Errorln("[REC] Error making archive dir.")
		} else {
			log.Debugln("[REC] Created archive dir ok.")
		}
	}

	outPath := filepath.Join(arcPath, fmt.Sprintf("%s_%s.mp4", cfg.Name, curr.Format(DATE_FORMAT)))

	switch len(merges) {
	case 0:
	case 1:
		src := filepath.Join(cfg.RecPath, merges[0])
		if err := cp(src, outPath); err != nil {
			log.Errorf("Copy error: %s", err)
		}
	default:
		lpath := filepath.Join(cfg.RecPath, "merges.txt")
		if lfile, err := os.Create(lpath); err == nil {
			for _, fname := range merges {
				fmt.Fprintf(lfile, "file '%s'\n", filepath.Join(cfg.RecPath, fname))
			}
			lfile.Close()
			cmd := exec.Command(cfg.FFmpegPath, "-f", "concat", "-safe", "0", "-i", lpath, "-c", "copy", outPath)
			if err := cmd.Run(); err != nil {
				fmt.Println(err)
			}
			cmd.Process.Kill()
			os.Remove(lpath)
		}
	}

	return filtred
}

func cp(src, dst string) error {
	from, err := os.Open(src)
	if err != nil {
		return err
	}
	defer from.Close()
	to, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer to.Close()
	_, err = io.Copy(to, from)
	return err
}
