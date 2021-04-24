// Copyright Contributors to the Open Cluster Management project

package main

import (
	"context"
	"flag"
	stdlog "log"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"

	"github.com/open-cluster-management/alerts-collector/pkg/forwarder"
	"github.com/open-cluster-management/alerts-collector/pkg/webhook"
)

func main() {
	// default configuration for webhook server
	whOpts := &webhook.Options{
		Port:     8443,
		CertFile: "/etc/alerts-collector/certs/tls.crt",
		KeyFile:  "/etc/alerts-collector/certs/tls.key",
	}

	// default log level: info
	logLevel := "info"

	// default config file for upstream alertmanagers
	amConfigFile := "/etc/alerts-collector/config/alertmanager-config/config.yaml"

	// init command line parameters
	flag.IntVar(&whOpts.Port, "port", whOpts.Port, "port for the alerts collector.")
	flag.StringVar(&logLevel, "log-level", logLevel, "Log filtering level. e.g info, debug, warn, error.")
	flag.StringVar(&whOpts.CertFile, "tls-cert", whOpts.CertFile, "File containing the x509 Certificate for HTTPS.")
	flag.StringVar(&whOpts.KeyFile, "tls-key", whOpts.KeyFile, "File containing the x509 private key to --tlsCertFile.")
	flag.StringVar(&amConfigFile, "alertmanagers.config-file", amConfigFile, "YAML format file containing the configuration of upstream alertmanagers.")
	flag.Parse()

	// setup logger
	l := log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	l = level.NewFilter(l, logLevelFromString(logLevel))
	l = log.WithPrefix(l, "ts", log.DefaultTimestampUTC)
	l = log.WithPrefix(l, "caller", log.DefaultCaller)
	stdlog.SetOutput(log.NewStdlibAdapter(l))
	whOpts.Logger = l

	// create new alerts forwarder with alertmanager configuration file
	fwder, err := forwarder.NewForwarder(l, amConfigFile)
	if err != nil {
		level.Error(l).Log("msg", "failed to create alert forwarder", "err", err)
		os.Exit(1)
	}

	whOpts.Forwarder = fwder
	webhookSvr, err := webhook.NewWebhook(whOpts)
	if err != nil {
		level.Error(l).Log("msg", "failed to create webhook server", "err", err)
		os.Exit(1)
	}

	// start webhook server in new rountine
	go func() {
		if err := webhookSvr.Run(); err != nil {
			level.Error(l).Log("msg", "failed to start webhook server", "err", err)
			os.Exit(1)
		}
	}()

	level.Info(l).Log("msg", "alerts collector initialized")

	// listening OS shutdown singal
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	<-signalChan

	level.Info(l).Log("msg", "got OS shutdown signal, shutting down webhook server gracefully...")
	if err = webhookSvr.Shutdown(context.TODO()); err != nil {
		level.Error(l).Log("msg", "failed to shut down the webhook server gracefully", "err", err)
	}
}

// logLevelFromString determines log level to string, defaults to all
func logLevelFromString(l string) level.Option {
	switch l {
	case "debug":
		return level.AllowDebug()
	case "info":
		return level.AllowInfo()
	case "warn":
		return level.AllowWarn()
	case "error":
		return level.AllowError()
	default:
		return level.AllowAll()
	}
}
