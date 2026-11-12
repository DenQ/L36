// Command load starts bin/l36 and runs vegeta against it.
//
// The pages scenario cycles 36 page ids, so most answers are 409.
// The versions scenario appends a new body to 100 pages created first.
// SAVE_INTERVAL_SECONDS is 1, and the attack is longer than that, so a flush
// happens while requests are in flight. Results go to load/history.jsonl.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const historyFile = "load/history.jsonl"

func main() {
	duration := flag.Duration("duration", 10*time.Second, "how long each scenario runs")
	workers := flag.Uint("workers", 100, "vegeta workers for both scenarios")
	port := flag.Int("port", 12360, "port for the temporary server")
	binary := flag.String("binary", "bin/l36", "path to the l36 binary")
	saveEvery := flag.Int("save-interval", 1, "SAVE_INTERVAL_SECONDS for the temporary server")
	flag.Parse()

	if *duration <= 0 {
		fmt.Fprintln(os.Stderr, "duration must be positive")
		os.Exit(2)
	}
	if *workers == 0 {
		fmt.Fprintln(os.Stderr, "workers must be positive")
		os.Exit(2)
	}
	if *saveEvery <= 0 {
		fmt.Fprintln(os.Stderr, "save-interval must be positive")
		os.Exit(2)
	}
	if _, err := exec.LookPath("vegeta"); err != nil {
		fmt.Fprintln(os.Stderr, "vegeta not found. Install it with: brew install vegeta")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, options{
		duration:  *duration,
		workers:   *workers,
		port:      *port,
		binary:    *binary,
		saveEvery: *saveEvery,
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type options struct {
	duration  time.Duration
	workers   uint
	port      int
	binary    string
	saveEvery int
}

func run(ctx context.Context, opt options) error {
	bin, err := filepath.Abs(opt.binary)
	if err != nil {
		return err
	}
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("%s: %w (run make build)", opt.binary, err)
	}

	base := "http://127.0.0.1:" + strconv.Itoa(opt.port)
	srv, err := startServer(ctx, bin, opt.port, opt.saveEvery)
	if err != nil {
		return err
	}
	defer srv.close()

	fmt.Printf("l36 load  %s  save every %ds  %s x %d workers\n", base, opt.saveEvery, opt.duration, opt.workers)

	pages, err := attack(ctx, "Pages", opt, func(i int) target { return pageTarget(base, i) })
	if err != nil {
		return err
	}
	fmt.Println(formatResult("Pages", pages))
	if err := appendHistory("Pages", opt, pages); err != nil {
		return err
	}

	if err := createVersionPages(ctx, base); err != nil {
		return err
	}
	versions, err := attack(ctx, "Versions", opt, func(i int) target { return versionTarget(base, i) })
	if err != nil {
		return err
	}
	fmt.Println(formatResult("Versions", versions))
	if err := appendHistory("Versions", opt, versions); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", historyFile)
	return nil
}

func attack(ctx context.Context, name string, opt options, next func(i int) target) (vegetaReport, error) {
	if err := ctx.Err(); err != nil {
		return vegetaReport{}, err
	}
	pr, pw := io.Pipe()
	go func() {
		err := streamTargets(ctx, pw, next)
		pw.CloseWithError(err)
	}()

	workers := strconv.FormatUint(uint64(opt.workers), 10)
	hit := exec.CommandContext(ctx, "vegeta", "attack",
		"-duration="+opt.duration.String(),
		"-rate=0",
		"-workers="+workers,
		"-max-workers="+workers,
		"-format=json",
		"-lazy",
		"-max-body=0",
		"-targets=stdin",
		"-name="+name,
	)
	hit.Stdin = pr
	hit.Stderr = os.Stderr

	var stdout, stderr bytes.Buffer
	reportCmd := exec.CommandContext(ctx, "vegeta", "report", "--type=json")
	reportCmd.Stdin, _ = hit.StdoutPipe()
	reportCmd.Stdout = &stdout
	reportCmd.Stderr = &stderr

	if err := reportCmd.Start(); err != nil {
		pr.Close()
		return vegetaReport{}, err
	}
	if err := hit.Start(); err != nil {
		pr.Close()
		_ = reportCmd.Wait()
		return vegetaReport{}, err
	}
	hitErr := hit.Wait()
	pr.Close()
	reportErr := reportCmd.Wait()
	if ctx.Err() != nil {
		return vegetaReport{}, ctx.Err()
	}
	if hitErr != nil {
		return vegetaReport{}, fmt.Errorf("vegeta attack %s: %w", name, hitErr)
	}
	if reportErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = reportErr.Error()
		}
		return vegetaReport{}, fmt.Errorf("vegeta report %s: %s", name, msg)
	}
	report, err := parseReport(stdout.Bytes())
	if err != nil {
		return vegetaReport{}, fmt.Errorf("%s: %w", name, err)
	}
	return report, nil
}

func createVersionPages(ctx context.Context, base string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	for i := 0; i < versionPages; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		body := fmt.Sprintf(`{"pageId":"load-ver-%d","content":"v0","userId":"load"}`, i)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/pages", strings.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("create load-ver-%d: %w", i, err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusConflict {
			return fmt.Errorf("create load-ver-%d: %s", i, resp.Status)
		}
	}
	return nil
}

type runningServer struct {
	cmd    *exec.Cmd
	exited chan struct{}
	dir    string
	log    *os.File
}

func startServer(ctx context.Context, bin string, port, saveEvery int) (*runningServer, error) {
	dir, err := os.MkdirTemp("", "l36-load-")
	if err != nil {
		return nil, err
	}
	logFile, err := os.Create(filepath.Join(dir, "server.log"))
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	cmd := exec.Command(bin)
	cmd.Dir = dir
	cmd.Env = childEnv(port, saveEvery)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	srv := &runningServer{cmd: cmd, exited: make(chan struct{}), dir: dir, log: logFile}
	if err := cmd.Start(); err != nil {
		logFile.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	go func() {
		_ = cmd.Wait()
		close(srv.exited)
	}()

	addr := "127.0.0.1:" + strconv.Itoa(port)
	if err := waitReady(ctx, srv, addr); err != nil {
		tail := srv.tail()
		srv.close()
		if tail != "" {
			err = fmt.Errorf("%w\n%s", err, tail)
		}
		return nil, err
	}
	return srv, nil
}

func waitReady(ctx context.Context, srv *runningServer, addr string) error {
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-srv.exited:
			return errors.New("server exited before listening")
		case <-deadline.C:
			return fmt.Errorf("server did not listen on %s", addr)
		default:
		}
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (s *runningServer) close() {
	if s == nil || s.cmd.Process == nil {
		return
	}
	_ = s.cmd.Process.Signal(os.Interrupt)
	select {
	case <-s.exited:
	case <-time.After(20 * time.Second):
		_ = s.cmd.Process.Kill()
		<-s.exited
	}
	s.log.Close()
	os.RemoveAll(s.dir)
}

func (s *runningServer) tail() string {
	if _, err := s.log.Seek(0, io.SeekStart); err != nil {
		return ""
	}
	raw, err := io.ReadAll(s.log)
	if err != nil || len(raw) == 0 {
		return ""
	}
	text := string(raw)
	if len(text) > 2000 {
		text = text[len(text)-2000:]
	}
	return strings.TrimSpace(text)
}

func childEnv(port, saveEvery int) []string {
	out := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "PORT=") || strings.HasPrefix(kv, "SAVE_INTERVAL_SECONDS=") {
			continue
		}
		out = append(out, kv)
	}
	out = append(out,
		"PORT="+strconv.Itoa(port),
		"SAVE_INTERVAL_SECONDS="+strconv.Itoa(saveEvery),
	)
	return out
}

type historyRecord struct {
	Time        string         `json:"time"`
	Scenario    string         `json:"scenario"`
	Requests    uint64         `json:"requests"`
	DurationNs  int64          `json:"duration_ns"`
	RPS         float64        `json:"rps"`
	SuccessRPS  float64        `json:"success_rps"`
	Success     float64        `json:"success"`
	P99Ns       int64          `json:"p99_ns"`
	Status      map[string]int `json:"status,omitempty"`
	Commit      string         `json:"commit,omitempty"`
	Dirty       bool           `json:"dirty,omitempty"`
	Go          string         `json:"go"`
	GOOS        string         `json:"goos"`
	GOARCH      string         `json:"goarch"`
	CPUs        int            `json:"cpus"`
	Workers     uint           `json:"workers"`
	SaveSeconds int            `json:"save_interval_seconds"`
}

func appendHistory(scenario string, opt options, report vegetaReport) error {
	commit, dirty := gitCommit()
	rec := historyRecord{
		Time:        time.Now().UTC().Format(time.RFC3339),
		Scenario:    scenario,
		Requests:    report.Requests,
		DurationNs:  report.Duration.Nanoseconds(),
		RPS:         math.Round(report.Rate*100) / 100,
		SuccessRPS:  math.Round(report.Throughput*100) / 100,
		Success:     math.Round(report.Success*1e6) / 1e6,
		P99Ns:       report.Latencies.P99.Nanoseconds(),
		Status:      report.StatusCodes,
		Commit:      commit,
		Dirty:       dirty,
		Go:          runtime.Version(),
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
		CPUs:        runtime.NumCPU(),
		Workers:     opt.workers,
		SaveSeconds: opt.saveEvery,
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	f, err := os.OpenFile(historyFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(raw)
	return err
}

func gitCommit() (string, bool) {
	commit, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "", false
	}
	status, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		return strings.TrimSpace(string(commit)), false
	}
	return strings.TrimSpace(string(commit)), len(bytes.TrimSpace(status)) > 0
}
