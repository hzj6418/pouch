package main

import (
	"fmt"
	"os"
	"os/signal"
	"path"
	"syscall"

	"github.com/alibaba/pouch/daemon"
	"github.com/alibaba/pouch/daemon/config"
	"github.com/alibaba/pouch/pkg/exec"
	"github.com/alibaba/pouch/pkg/utils"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	cfg        config.Config
	sigHandles []func() error
)

func main() {
	var cmdServe = &cobra.Command{
		Use:          "pouchd",
		Args:         cobra.NoArgs,
		SilenceUsage: true,

		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemon()
		},
	}

	setupFlags(cmdServe)

	if err := cmdServe.Execute(); err != nil {
		os.Exit(1)
	}
}

// runDaemon prepares configs, setups essential details and runs pouchd daemon.
func runDaemon() error {
	// initialize log.
	initLog()

	// initialize home dir.
	dir := cfg.HomeDir

	if dir == "" || !path.IsAbs(dir) {
		return fmt.Errorf("invalid pouchd's home dir: %s", dir)
	}
	if _, err := os.Stat(dir); err != nil && os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0666); err != nil {
			return fmt.Errorf("failed to mkdir: %v", err)
		}
	}

	// define and start all required processes.
	if _, err := os.Stat(cfg.ContainerdAddr); err == nil {
		os.RemoveAll(cfg.ContainerdAddr)
	}
	var processes exec.Processes = []*exec.Process{
		{
			Path: cfg.ContainerdPath,
			Args: []string{
				"-c", cfg.ContainerdConfig,
				"-a", cfg.ContainerdAddr,
				"--root", path.Join(cfg.HomeDir, "containerd/root"),
				"--state", path.Join(cfg.HomeDir, "containerd/state"),
				"-l", utils.If(cfg.Debug, "debug", "info").(string),
			},
		},
	}
	defer processes.StopAll()

	if err := processes.RunAll(); err != nil {
		return err
	}
	sigHandles = append(sigHandles, processes.StopAll)

	// initialize signal and handle method.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		sig := <-signals
		logrus.Warnf("received signal: %s", sig)

		for _, handle := range sigHandles {
			if err := handle(); err != nil {
				logrus.Errorf("failed to handle signal: %v", err)
			}
		}
		os.Exit(1)
	}()

	// new daemon instance, this is core.
	d := daemon.NewDaemon(cfg)
	if d == nil {
		return fmt.Errorf("failed to new daemon")
	}

	sigHandles = append(sigHandles, d.Shutdown)

	return d.Run()
}

// setupFlags setups flags for command line.
func setupFlags(cmd *cobra.Command) {
	flagSet := cmd.Flags()

	flagSet.StringVar(
		&cfg.HomeDir,
		"home-dir",
		"/etc/pouchd",
		"The pouchd's home directory")

	flagSet.StringArrayVarP(
		&cfg.Listen,
		"listen",
		"l",
		[]string{"unix:///var/run/pouchd.sock"},
		"which address to listen on")

	flagSet.BoolVarP(
		&cfg.Debug,
		"debug",
		"D",
		false,
		"switch debug level")

	flagSet.StringVarP(
		&cfg.ContainerdAddr,
		"containerd",
		"c",
		"/var/run/containerd.sock",
		"where does containerd listened on")

	flagSet.StringVar(
		&cfg.ContainerdPath,
		"containerd-path",
		"/usr/local/bin/containerd",
		"Specify the path of Containerd binary")

	flagSet.StringVar(
		&cfg.ContainerdConfig,
		"containerd-config",
		"/etc/containerd/config.toml",
		"Specify the path of Containerd configuration file")

	utils.SetupTLSFlag(flagSet, &cfg.TLS)
}

// initLog initializes log Level and log format of daemon.
func initLog() {
	if cfg.Debug {
		logrus.SetLevel(logrus.DebugLevel)
		logrus.Infof("start daemon at debug level")
	}

	formatter := &logrus.TextFormatter{
		ForceColors:     true,
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05.000000000",
	}
	logrus.SetFormatter(formatter)
}
