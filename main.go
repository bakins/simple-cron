package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/robfig/cron"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// TODO: we could expose a statsd server - like statsd exporter
// for processes to push metrics to?

// todoay, allow override?
const metricNamespace = "cron"

var (
	schedule string
	address  string
	jobName  string
	logger   *zap.Logger

	runCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricNamespace,
			Name:      "runs_total",
			Help:      "Number of job runs.",
		},
		[]string{"status", "name"},
	)

	runTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "run_time",
			Help:      "job run time",
		},
		[]string{"status", "name"},
	)

// TODO:  add histogram as well?
// TODO: add last runtime?
// TODO: add https://golang.org/pkg/syscall/#Rusage from process state?
// should only be exposed when was a success?
)

func main() {
	prometheus.MustRegister(runCount)
	prometheus.MustRegister(runTime)
	prometheus.Unregister(prometheus.NewProcessCollector(os.Getpid(), ""))
	prometheus.Unregister(prometheus.NewGoCollector())

	rootCmd := &cobra.Command{
		Use:   "simple-cron",
		Short: "single process job scheduler",
		Run:   runSimpleCron,
	}

	f := rootCmd.Flags()
	f.StringVarP(&schedule, "schedule", "s", "15 * * * *", "cron expression of desired schedule.")
	f.StringVarP(&address, "address", "a", ":2766", "address for HTTP listener for metrics")
	f.StringVarP(&jobName, "name", "n", "", "name of job for metrics. If unset, the command is used")

	var err error
	logger, err = newLogger()

	if err != nil {
		panic(err)
	}

	if err := rootCmd.Execute(); err != nil {
		logger.Fatal("failed to start", zap.Error(err))
	}
}

// cron package uses a extended cron format, but we just want "normal"
func parseSchedule(s string) (string, error) {
	parts := strings.Fields(s)
	if len(parts) != 5 {
		return "", errors.Errorf("invalid cron schedule")
	}

	// convert to "extended". use 0 for seconds
	a := append([]string{"0"}, parts...)
	return strings.Join(a, " "), nil
}

func runSimpleCron(cmd *cobra.Command, args []string) {
	if len(args) == 0 {
		logger.Fatal("command is required")
	}

	cronExp, err := parseSchedule(schedule)
	if err != nil {
		logger.Fatal("failed to parse schedule", zap.Error(err))
	}
	c := cron.New()
	w, err := newWrappedCommand(args)
	if err != nil {
		logger.Fatal("failed to create job", zap.Error(err))
	}

	if err := c.AddFunc(cronExp, w.run); err != nil {
		logger.Fatal("failed to schedule job", zap.Error(err))
	}

	srv := http.Server{
		Addr: address,
	}

	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "OK\n")
	})

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		err := srv.ListenAndServe()
		switch err {
		case nil:
		// normal
		case http.ErrServerClosed:
		// server was stopped
		default:
			logger.Fatal("failed to start HTTP server", zap.Error(err))
		}
	}()

	c.Start()
	// block waiting on signals
	<-sigs

	w.stop()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	srv.Shutdown(ctx)
}

type wrappedCommand struct {
	sync.Mutex
	name      string
	path      string
	args      []string
	cmd       *exec.Cmd
	logger    *zap.Logger
	isRunning bool
}

func (w *wrappedCommand) setRunning(running bool) {
	w.Lock()
	defer w.Unlock()
	w.isRunning = running
}

func (w *wrappedCommand) getRunning() bool {
	w.Lock()
	defer w.Unlock()
	return w.isRunning
}

func (w *wrappedCommand) setCmd(cmd *exec.Cmd) {
	w.Lock()
	defer w.Unlock()
	w.cmd = cmd
}

func (w *wrappedCommand) getCmd() (cmd *exec.Cmd) {
	w.Lock()
	defer w.Unlock()
	return w.cmd
}

// wrapper so that the locked region is smaller/easier
func (w *wrappedCommand) runCommand(cmd *exec.Cmd) error {
	w.setRunning(true)
	defer w.setRunning(false)

	if err := cmd.Start(); err != nil {
		return errors.Wrap(err, "failed to start process")
	}

	w.setCmd(cmd)
	defer w.setCmd(nil)

	if err := cmd.Wait(); err != nil {
		return errors.Wrap(err, "failed to run process")
	}

	return nil
}

func (w *wrappedCommand) run() {
	if w.getRunning() {
		logger.Info("already running", zap.String("jobname", w.name))
		// TODO: metric for concurrent attempts
	}

	// TODO: use a timeout?
	// https://golang.org/pkg/os/exec/#CommandContext

	cmd := exec.Command(w.path, w.args...)

	// XXX : Wrap log lines?
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	status := "success"
	start := time.Now()
	err := w.runCommand(cmd)
	diff := time.Since(start)
	if err != nil {
		status = "error"
		logger.Warn("job failed", zap.Error(err))
	}

	labels := prometheus.Labels{"status": status, "name": w.name}
	runCount.With(labels).Inc()
	runTime.With(labels).Set(diff.Seconds())
}

func (w *wrappedCommand) stop() {
	if !w.isRunning {
		return
	}

	cmd := w.getCmd()
	if cmd == nil {
		return
	}

	if cmd.Process == nil {
		return
	}

	//TODO: check if process is running?

	// is probably fine to kill as there will always be a race
	// even if we check if runnign
	if err := cmd.Process.Kill(); err != nil {
		w.logger.Error("failed to kill process", zap.Error(err))
	}

	// use a timeout and send kill -9?
}

func newWrappedCommand(cmd []string) (*wrappedCommand, error) {
	proc, args := cmd[0], cmd[1:]
	path, err := exec.LookPath(proc)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to find '%s' in PATH", proc)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to determine absolute path of '%s'", proc)
	}

	if jobName == "" {
		jobName = proc
	}

	w := &wrappedCommand{
		name:   jobName,
		path:   absPath,
		args:   args,
		logger: logger.With(zap.String("jobname", jobName)),
	}

	return w, nil
}

func newLogger() (*zap.Logger, error) {
	config := zap.Config{
		Development:       false,
		DisableCaller:     true,
		DisableStacktrace: true,
		EncoderConfig:     zap.NewProductionEncoderConfig(),
		Encoding:          "json",
		ErrorOutputPaths:  []string{"stdout"},
		Level:             zap.NewAtomicLevel(),
		OutputPaths:       []string{"stdout"},
	}
	l, err := config.Build()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create logger")
	}
	return l, nil
}
