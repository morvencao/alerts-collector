// Copyright Contributors to the Open Cluster Management project

package forwarder

import (
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"gopkg.in/yaml.v2"
)

// APIVersion represents the API version of the alertmanager endpoint
type APIVersion string

const (
	APIv1 APIVersion = "v1"
	APIv2 APIVersion = "v2"
)

type AlertingConfig struct {
	Alertmanagers []AlertmanagerConfig `yaml:"alertmanagers"`
}

// AlertmanagerConfig represents a client to a cluster of Alertmanager endpoints.
type AlertmanagerConfig struct {
	HTTPClientConfig ClientConfig    `yaml:"http_config"`
	EndpointsConfig  EndpointsConfig `yaml:",inline"`
	Timeout          model.Duration  `yaml:"timeout"`
	APIVersion       APIVersion      `yaml:"api_version"`
}

// ClientConfig configures an HTTP client.
type ClientConfig struct {
	// The HTTP basic authentication credentials for the targets.
	BasicAuth BasicAuth `yaml:"basic_auth"`
	// The bearer token for the targets.
	BearerToken string `yaml:"bearer_token"`
	// The bearer token file for the targets.
	BearerTokenFile string `yaml:"bearer_token_file"`
	// HTTP proxy server to use to connect to the targets.
	ProxyURL string `yaml:"proxy_url"`
	// TLSConfig to use to connect to the targets.
	TLSConfig TLSConfig `yaml:"tls_config"`
}

// TLSConfig configures TLS connections.
type TLSConfig struct {
	// The CA cert to use for the targets.
	CAFile string `yaml:"ca_file"`
	// The client cert file for the targets.
	CertFile string `yaml:"cert_file"`
	// The client key file for the targets.
	KeyFile string `yaml:"key_file"`
	// Used to verify the hostname for the targets.
	ServerName string `yaml:"server_name"`
	// Disable target certificate validation.
	InsecureSkipVerify bool `yaml:"insecure_skip_verify"`
}

// BasicAuth configures basic authentication for HTTP clients.
type BasicAuth struct {
	Username     string `yaml:"username"`
	Password     string `yaml:"password"`
	PasswordFile string `yaml:"password_file"`
}

// IsZero returns false if basic authentication isn't enabled.
func (b BasicAuth) IsZero() bool {
	return b.Username == "" && b.Password == "" && b.PasswordFile == ""
}

// EndpointsConfig configures a cluster of HTTP endpoints from static addresses and
// file service discovery.
type EndpointsConfig struct {
	// List of addresses with DNS prefixes.
	StaticAddresses []string `yaml:"static_configs"`

	// The URL scheme to use when talking to targets.
	Scheme string `yaml:"scheme"`

	// Path prefix to add in front of the endpoint path.
	PathPrefix string `yaml:"path_prefix"`
}

// loadAlertingConfig loads configuraration about upstream alertmanagers from YAML format file
func loadAlertingConfig(configFile string) (*AlertingConfig, error) {
	configYAML, err := ioutil.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load configurations from file %s: %v", configFile, err)
	}

	alertingCfg := &AlertingConfig{}
	if err := yaml.UnmarshalStrict(configYAML, alertingCfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal configurations: %v", err)
	}
	return alertingCfg, nil
}

// createHTTPClient returns a new HTTP client based on alertmanager configuration
func createHTTPClient(clientCfg ClientConfig, name string) (*http.Client, error) {
	httpClientConfig := config.HTTPClientConfig{
		BearerToken:     config.Secret(clientCfg.BearerToken),
		BearerTokenFile: clientCfg.BearerTokenFile,
		TLSConfig: config.TLSConfig{
			CAFile:             clientCfg.TLSConfig.CAFile,
			CertFile:           clientCfg.TLSConfig.CertFile,
			KeyFile:            clientCfg.TLSConfig.KeyFile,
			ServerName:         clientCfg.TLSConfig.ServerName,
			InsecureSkipVerify: clientCfg.TLSConfig.InsecureSkipVerify,
		},
	}
	if clientCfg.ProxyURL != "" {
		var proxy config.URL
		err := yaml.Unmarshal([]byte(clientCfg.ProxyURL), &proxy)
		if err != nil {
			return nil, err
		}
		httpClientConfig.ProxyURL = proxy
	}
	if !clientCfg.BasicAuth.IsZero() {
		httpClientConfig.BasicAuth = &config.BasicAuth{
			Username:     clientCfg.BasicAuth.Username,
			Password:     config.Secret(clientCfg.BasicAuth.Password),
			PasswordFile: clientCfg.BasicAuth.PasswordFile,
		}
	}
	if err := httpClientConfig.Validate(); err != nil {
		return nil, err
	}

	client, err := config.NewClientFromConfig(httpClientConfig, name, false, false)
	if err != nil {
		return nil, err
	}
	return client, nil
}
