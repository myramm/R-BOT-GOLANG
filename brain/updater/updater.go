// Package updater menarik update dari GitHub via git lalu membangun ulang biner.
// Port lib/updater.js. Beda dengan Node (yang bisa re-require file command tanpa
// restart), biner Go harus di-`go build` ulang lalu proses di-exec ulang; jadi
// tak ada konsep "command-only reload" maupun `npm install` di sini — setiap
// update = git pull/reset → go build → restart (go build otomatis menarik deps).
package updater

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// git menjalankan `git <args...>` di direktori kerja (root repo, sama seperti
// tempat config.json dibaca) dengan timeout. Mengembalikan stdout terpangkas dan
// error yang sudah membawa baris stderr yang relevan.
func git(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", &gitError{err: err, stderr: errb.String()}
	}
	return out.String(), nil
}

// gitError membungkus error exec + stderr agar firstLine bisa menyaring pesan.
type gitError struct {
	err    error
	stderr string
}

func (e *gitError) Error() string {
	if msg := firstLine(e.stderr); msg != "" {
		return msg
	}
	return e.err.Error()
}

// IsGitRepo true bila folder bot adalah repo git (ada .git).
func IsGitRepo() bool {
	info, err := os.Stat(".git")
	return err == nil && info != nil
}

// Branch mengembalikan branch aktif (rev-parse), fallback "main" bila detached
// atau git gagal.
func Branch(ctx context.Context) string {
	out, err := git(ctx, 15*time.Second, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "main"
	}
	b := strings.TrimSpace(out)
	if b == "" || b == "HEAD" {
		return "main"
	}
	return b
}

// firstLine menyaring keluaran git jadi satu baris paling informatif (port
// firstLine di updater.js): buang baris progress/noise, utamakan baris fatal/error.
func firstLine(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var lines []string
	for _, l := range strings.Split(raw, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return raw
	}
	reErr := regexp.MustCompile(`(?i)fatal:|error:|not possible|overwritten|local changes|rejected|Aborting`)
	for _, l := range lines {
		if reErr.MatchString(l) {
			return l
		}
	}
	reNoise := regexp.MustCompile(`(?i)^From\s|^\*\s|->\s*FETCH_HEAD|^Receiving|^Resolving|^remote:|^Unpacking|^Counting|^Compressing`)
	for _, l := range lines {
		if !reNoise.MatchString(l) {
			return l
		}
	}
	return lines[0]
}

// Commit adalah satu commit yang tertinggal dari remote.
type Commit struct {
	Hash    string
	Subject string
}

// CheckResult adalah hasil CheckUpdate. Err terisi bila OK==false.
type CheckResult struct {
	OK           bool
	Err          string
	UpToDate     bool
	Branch       string
	Ahead        int
	Behind       int
	Commits      []Commit
	ChangedFiles []string
}

// CheckUpdate membandingkan HEAD lokal dengan origin/<branch>: fetch dulu, hitung
// ahead/behind, lalu (bila tertinggal) kumpulkan daftar commit & file berubah.
func CheckUpdate(ctx context.Context) CheckResult {
	if !IsGitRepo() {
		return CheckResult{Err: "Folder bot belum jadi repo git. Setup GitHub dulu."}
	}
	branch := Branch(ctx)
	if _, err := git(ctx, 60*time.Second, "fetch", "origin", branch); err != nil {
		return CheckResult{Err: "git fetch gagal: " + err.Error()}
	}
	remoteRef := "origin/" + branch

	counts, err := git(ctx, 30*time.Second, "rev-list", "--left-right", "--count", "HEAD..."+remoteRef)
	if err != nil {
		return CheckResult{Err: "gagal membandingkan versi: " + err.Error()}
	}
	ahead, behind := parseCounts(counts)
	if behind == 0 {
		return CheckResult{OK: true, UpToDate: true, Branch: branch, Ahead: ahead}
	}

	logOut, err := git(ctx, 30*time.Second, "log", "--pretty=%h\t%s", "HEAD.."+remoteRef)
	if err != nil {
		return CheckResult{Err: "gagal membaca daftar commit: " + err.Error()}
	}
	var commits []Commit
	for _, line := range strings.Split(strings.TrimSpace(logOut), "\n") {
		if line == "" {
			continue
		}
		hash, subject, _ := strings.Cut(line, "\t")
		commits = append(commits, Commit{Hash: hash, Subject: subject})
	}

	filesOut, err := git(ctx, 30*time.Second, "diff", "--name-only", "HEAD.."+remoteRef)
	if err != nil {
		return CheckResult{Err: "gagal membaca file berubah: " + err.Error()}
	}
	var changed []string
	for _, f := range strings.Split(strings.TrimSpace(filesOut), "\n") {
		if f != "" {
			changed = append(changed, f)
		}
	}

	return CheckResult{OK: true, Branch: branch, Ahead: ahead, Behind: behind, Commits: commits, ChangedFiles: changed}
}

// parseCounts membaca keluaran `rev-list --left-right --count` ("ahead\tbehind").
func parseCounts(s string) (ahead, behind int) {
	f := strings.Fields(strings.TrimSpace(s))
	if len(f) >= 1 {
		ahead, _ = strconv.Atoi(f[0])
	}
	if len(f) >= 2 {
		behind, _ = strconv.Atoi(f[1])
	}
	return ahead, behind
}

// ApplyResult adalah hasil ApplyUpdate/ForceUpdate.
type ApplyResult struct {
	OK     bool
	Err    string
	Branch string
	Forced bool
}

// ApplyUpdate menarik update dengan fast-forward (git pull --ff-only).
func ApplyUpdate(ctx context.Context) ApplyResult {
	if !IsGitRepo() {
		return ApplyResult{Err: "Folder bot belum jadi repo git."}
	}
	branch := Branch(ctx)
	if _, err := git(ctx, 120*time.Second, "pull", "--ff-only", "origin", branch); err != nil {
		msg := err.Error()
		if regexp.MustCompile(`(?i)local changes|not possible to fast-forward|overwritten`).MatchString(msg) {
			msg += "\nAda perubahan lokal yang menghalangi. Ketik *update force* untuk buang perubahan lokal & samakan dengan GitHub."
		}
		return ApplyResult{Err: "git pull gagal: " + msg}
	}
	return ApplyResult{OK: true, Branch: branch}
}

// ForceUpdate membuang perubahan lokal & menyamakan dengan remote (fetch + reset
// --hard). Dipakai saat pull ff-only tertahan perubahan lokal.
func ForceUpdate(ctx context.Context) ApplyResult {
	if !IsGitRepo() {
		return ApplyResult{Err: "Folder bot belum jadi repo git."}
	}
	branch := Branch(ctx)
	if _, err := git(ctx, 60*time.Second, "fetch", "origin", branch); err != nil {
		return ApplyResult{Err: "git fetch gagal: " + err.Error()}
	}
	if _, err := git(ctx, 60*time.Second, "reset", "--hard", "origin/"+branch); err != nil {
		return ApplyResult{Err: "git reset gagal: " + err.Error()}
	}
	return ApplyResult{OK: true, Branch: branch, Forced: true}
}

// Rebuild membangun ulang biner ke path proses saat ini (`go build -o <exe> .`).
// Dipanggil setelah pull/reset, sebelum restart. os.Executable() bisa berakhiran
// " (deleted)" bila biner sudah pernah ditimpa build sebelumnya di proses ini,
// jadi suffix itu dipangkas agar target build benar.
func Rebuild(ctx context.Context) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe = strings.TrimSuffix(exe, " (deleted)")
	exe, _ = filepath.Abs(exe)

	cctx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "go", "build", "-o", exe, ".")
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		if msg := firstLine(errb.String()); msg != "" {
			return &gitError{err: err, stderr: msg}
		}
		return err
	}
	return nil
}
