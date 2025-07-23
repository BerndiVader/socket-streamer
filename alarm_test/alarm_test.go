package alarm_test

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

var RecPath string = "//keller/c/temp1"

func Test_merge(t *testing.T) {
	if entries, err := os.ReadDir(RecPath); err == nil {
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
			entries = merge(entries)
		}

	}
}

func filter(entries []os.DirEntry, keep func(os.DirEntry) bool) []os.DirEntry {
	var keeps []os.DirEntry

	for _, de := range entries {
		if keep(de) {
			keeps = append(keeps, de)
		}

	}

	return keeps
}

func merge(entries []os.DirEntry) []os.DirEntry {
	var curr time.Time
	var merges []string
	var filtred []os.DirEntry

	for _, merger := range entries {
		if info, err := merger.Info(); err == nil {
			if curr.IsZero() {
				curr = info.ModTime()
			}
			switch curr.Format("2006-01-02") == info.ModTime().Format("2006-01-02") {
			case true:
				if info.Size() > 0 {
					merges = append(merges, merger.Name())
				}
			case false:
				filtred = append(filtred, merger)
			}
		}
	}

	outPath := filepath.Join(RecPath, "archive", "test_"+curr.Format("2006-01-02")+".mp4")

	switch len(merges) {
	case 0:
	case 1:
		src := filepath.Join(RecPath, merges[0])
		if err := cp(src, outPath); err != nil {
			fmt.Println("Copy error:", err)
		}
	default:
		lpath := filepath.Join(RecPath, "merges.txt")
		if lfile, err := os.Create(lpath); err == nil {
			for _, fname := range merges {
				fmt.Fprintf(lfile, "file '%s'\n", filepath.Join(RecPath, fname))
			}
			lfile.Close()
			cmd := exec.Command("ffmpeg.exe", "-f", "concat", "-safe", "0", "-i", lpath, "-c", "copy", outPath)
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
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
