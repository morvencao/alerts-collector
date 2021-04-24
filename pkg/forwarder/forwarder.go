// Copyright Contributors to the Open Cluster Management project

package forwarder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/go-openapi/strfmt"
	"github.com/prometheus/alertmanager/api/v2/models"
	"github.com/prometheus/alertmanager/template"
	"go.uber.org/atomic"
)

// Alertmanager is an HTTP client that can send alerts to an alertmanager endpoint
type Alertmanager struct {
	logger    log.Logger
	endpoints []*url.URL
	client    *http.Client
	timeout   time.Duration
	version   APIVersion
}

// NewAlertmanager construct new Alertmanager client
func NewAlertmanager(l log.Logger, amcfg AlertmanagerConfig) (*Alertmanager, error) {
	client, err := createHTTPClient(amcfg.HTTPClientConfig, "alerts-collector")
	if err != nil {
		return nil, fmt.Errorf("failed to create http client for upstream alertmanager: %v", err)
	}

	// TODO(morvencao): support dynamic service discovery
	if amcfg.EndpointsConfig == nil || amcfg.EndpointsConfig.StaticAddresses == nil {
		return nil, fmt.Errorf("failed to get endpoint addresses")
	}

	var urls []*url.URL
	for _, addr := range amcfg.EndpointsConfig.StaticAddresses {
		urls = append(urls,
			&url.URL{
				Scheme: amcfg.EndpointsConfig.Scheme,
				Host:   addr,
				Path:   path.Join("/", amcfg.EndpointsConfig.PathPrefix),
			},
		)
	}

	return &Alertmanager{
		logger:    l,
		endpoints: urls,
		client:    client,
		timeout:   time.Duration(amcfg.Timeout),
		version:   amcfg.APIVersion,
	}, nil
}

// postAlerts post the alert to upstream alertmanager
func (am *Alertmanager) postAlerts(ctx context.Context, u url.URL, r io.Reader) error {
	req, err := http.NewRequest("POST", u.String(), r)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, am.timeout)
	defer cancel()
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")

	resp, err := am.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request to %q: %v", u.String(), err)
	}
	defer resp.Body.Close()
	level.Info(am.logger).Log("msg", "post an alert")

	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("bad response status %v from %q", resp.Status, u.String())
	}
	return nil
}

// Forwarder forwards alerts to a dynamic set of upstream alertmanagers
type Forwarder struct {
	logger        log.Logger
	alertmanagers []*Alertmanager
	versions      []APIVersion
}

// NewForwarder returns a new forwarder
func NewForwarder(l log.Logger, amConfigFile string) (*Forwarder, error) {
	alertCfg, err := loadAlertingConfig(amConfigFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load configurations of upstream alertmanagers: %v", err)
	}

	if len(alertCfg.Alertmanagers) == 0 {
		level.Info(l).Log("msg", "no alertmanager configured")
	}

	var alertmanagers []*Alertmanager
	for _, amcfg := range alertCfg.Alertmanagers {
		am, err := NewAlertmanager(l, amcfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create alertmanager client from configuration: %v", err)
		}
		alertmanagers = append(alertmanagers, am)
	}

	var (
		versions       []APIVersion
		versionPresent map[APIVersion]bool
	)
	for _, am := range alertmanagers {
		if _, found := versionPresent[am.version]; found {
			continue
		}
		versionPresent[am.version] = true
		versions = append(versions, am.version)
	}

	return &Forwarder{
		logger:        l,
		alertmanagers: alertmanagers,
		versions:      versions,
	}, nil
}

// Forward an alert batch to all given Alertmanager
func (fwder *Forwarder) Forward(ctx context.Context, alerts template.Alerts) error {
	if len(alerts) == 0 {
		level.Warn(fwder.logger).Log("msg", "no alert to forward")
		return nil
	}

	payload := make(map[APIVersion][]byte)
	for _, version := range fwder.versions {
		var (
			b   []byte
			err error
		)
		switch version {
		case APIv1:
			if b, err = json.Marshal(alerts); err != nil {
				level.Warn(fwder.logger).Log("msg", "encoding alerts for v1 API failed", "err", err)
				return err
			}
		case APIv2:
			pAlerts := make(models.PostableAlerts, 0, len(alerts))
			for _, alt := range alerts {
				pAlerts = append(pAlerts, &models.PostableAlert{
					Annotations: kvToLabelSet(alt.Annotations),
					EndsAt:      strfmt.DateTime(alt.EndsAt),
					StartsAt:    strfmt.DateTime(alt.StartsAt),
					Alert: models.Alert{
						GeneratorURL: strfmt.URI(alt.GeneratorURL),
						Labels:       kvToLabelSet(alt.Labels),
					},
				})
			}
			if b, err = json.Marshal(pAlerts); err != nil {
				level.Warn(fwder.logger).Log("msg", "encoding alerts for v2 API failed", "err", err)
				return err
			}
		}
		payload[version] = b
	}

	var (
		wg         sync.WaitGroup
		numSuccess atomic.Uint64
	)
	for _, am := range fwder.alertmanagers {
		for _, u := range am.endpoints {
			wg.Add(1)
			go func(am *Alertmanager, u url.URL) {
				defer wg.Done()

				level.Debug(fwder.logger).Log("msg", "forward alerts", "alertmanager", u.Host, "numAlerts", len(alerts))
				u.Path = path.Join(u.Path, fmt.Sprintf("/api/%s/alerts", string(am.version)))

				if err := am.postAlerts(ctx, u, bytes.NewReader(payload[am.version])); err != nil {
					level.Warn(fwder.logger).Log(
						"msg", "forwarding alerts failed",
						"alertmanager", u.Host,
						"alerts", string(payload[am.version]),
						"err", err,
					)
					return
				}
				numSuccess.Inc()
			}(am, *u)
		}
	}
	wg.Wait()

	if numSuccess.Load() > 0 {
		return nil
	}
	level.Warn(fwder.logger).Log("msg", "failed to send alerts to all alertmanagers", "numAlerts", len(alerts))
	return fmt.Errorf("failed to send %d alerts to all alertmanagers", len(alerts))
}

// kvToLabelSet translate KC to LabelSet
func kvToLabelSet(kvs template.KV) models.LabelSet {
	ls := make(models.LabelSet, len(kvs))
	for k, v := range kvs {
		ls[k] = v
	}
	return ls
}
