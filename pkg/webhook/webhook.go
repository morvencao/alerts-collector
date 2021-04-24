// Copyright Contributors to the Open Cluster Management project

package webhook

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/alertmanager/template"

	"github.com/open-cluster-management/alerts-collector/pkg/forwarder"
)

// webhook server options
type Options struct {
	Port      int                  // webhook server port
	CertFile  string               // path to the x509 certificate for https
	KeyFile   string               // path to the x509 private key matching `CertFile`
	Logger    log.Logger           // logger for the webhook server
	Forwarder *forwarder.Forwarder // alert forwarder for the the webhook server
}

// webhook server
type Webhook struct {
	logger    log.Logger           // logger for the webhook server
	forwarder *forwarder.Forwarder // alert forwarder for the the webhook server
	server    *http.Server         // http server for the webhook
}

// NewWebhook construct the new webhook server
func NewWebhook(opts *Options) (*Webhook, error) {
	pair, err := tls.LoadX509KeyPair(opts.CertFile, opts.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load key pair: %v", err)
	}

	return &Webhook{
		logger:    opts.Logger,
		forwarder: opts.Forwarder,
		server: &http.Server{
			Addr:      fmt.Sprintf(":%v", opts.Port),
			TLSConfig: &tls.Config{Certificates: []tls.Certificate{pair}},
		},
	}, nil
}

// Run method register the handler functions and starts the webhook server
func (wh *Webhook) Run() error {
	// define http server and server handler
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", wh.Serve)
	mux.HandleFunc("/healthz", wh.Healthz)
	wh.server.Handler = mux

	if err := wh.server.ListenAndServeTLS("", ""); err != nil {
		return fmt.Errorf("failed to listen and serve webhook server: %v", err)
	}
	return nil
}

// Shutdown method starts the webhook server
func (wh *Webhook) Shutdown(ctx context.Context) error {
	return wh.server.Shutdown(ctx)
}

// Serve handler for the webhook server
func (wh *Webhook) Serve(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	data := &template.Data{}
	if err := json.NewDecoder(r.Body).Decode(data); err != nil {
		asJson(w, http.StatusBadRequest, err.Error())
		return
	}

	level.Info(wh.logger).Log("alert", fmt.Sprintf("GroupLabels=%v, CommonLabels=%v", data.GroupLabels, data.CommonLabels))
	for _, alert := range data.Alerts {
		level.Debug(wh.logger).Log("alert", fmt.Sprintf("status=%s,Labels=%v,Annotations=%v,StartsAt=%v,EndsAt=%v", alert.Status, alert.Labels, alert.Annotations, alert.StartsAt, alert.EndsAt))
		severity := alert.Labels["severity"]
		switch strings.ToUpper(severity) {
		case "CRITICAL":
			level.Debug(wh.logger).Log("alert", fmt.Sprintf("action on severity: %s", severity))
			// TODO(morvencao): forward alerts according to the alert severity
		case "WARNING":
			level.Debug(wh.logger).Log("alert", fmt.Sprintf("action on severity: %s", severity))
			// TODO(morvencao): forward alerts according to the alert severity
		default:
			level.Debug(wh.logger).Log("alert", fmt.Sprintf("no action on severity: %s", severity))
			// TODO(morvencao): forward alerts according to the alert severity
		}
	}

	level.Info(wh.logger).Log("msg", "prepare to forward alerts to upstream alertmanagers")
	// forward the alerts
	// TODO(morvencao): forward alerts according to the alert severity
	if err := wh.forwarder.Forward(context.TODO(), data.Alerts); err != nil {
		asJson(w, http.StatusInternalServerError, err.Error())
	}
	asJson(w, http.StatusOK, "success")
}

// Healthz method for webhook server to return healthy status
func (wh *Webhook) Healthz(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "OK!")
}

type response struct {
	Status  int
	Message string
}

// asJson write json response
func asJson(w http.ResponseWriter, status int, message string) {
	data := response{
		Status:  status,
		Message: message,
	}
	bytes, _ := json.Marshal(data)
	json := string(bytes[:])

	w.WriteHeader(status)
	fmt.Fprint(w, json)
}
