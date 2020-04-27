package pmapi

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// ErrTLSMismatch indicates that no TLS fingerprint match could be found.
var ErrTLSMismatch = errors.New("no TLS fingerprint match found")

// TrustedAPIPins contains trusted public keys of the protonmail API and proxies.
// NOTE: the proxy pins are the same for all proxy servers, guaranteed by infra team ;)
var TrustedAPIPins = []string{ // nolint[gochecknoglobals]
	`pin-sha256="drtmcR2kFkM8qJClsuWgUzxgBkePfRCkRpqUesyDmeE="`, // current
	`pin-sha256="YRGlaY0jyJ4Jw2/4M8FIftwbDIQfh8Sdro96CeEel54="`, // hot
	`pin-sha256="AfMENBVvOS8MnISprtvyPsjKlPooqh8nMB/pvCrpJpw="`, // cold
	`pin-sha256="EU6TS9MO0L/GsDHvVc9D5fChYLNy5JdGYpJw0ccgetM="`, // proxy main
	`pin-sha256="iKPIHPnDNqdkvOnTClQ8zQAIKG0XavaPkcEo0LBAABA="`, // proxy backup 1
	`pin-sha256="MSlVrBCdL0hKyczvgYVSRNm88RicyY04Q2y5qrBt0xA="`, // proxy backup 2
	`pin-sha256="C2UxW0T1Ckl9s+8cXfjXxlEqwAfPM4HiW2y3UdtBeCw="`, // proxy backup 3
}

// TLSReportURI is the address where TLS reports should be sent.
const TLSReportURI = "https://reports.protonmail.ch/reports/tls"

// TLSReport is inspired by https://tools.ietf.org/html/rfc7469#section-3.
// When a TLS key mismatch is detected, a TLSReport is posted to TLSReportURI.
type TLSReport struct {
	//  DateTime of observed pin validation in time.RFC3339 format.
	DateTime string `json:"date-time"`

	// Hostname to which the UA made original request that failed pin validation.
	Hostname string `json:"hostname"`

	// Port to which the UA made original request that failed pin validation.
	Port int `json:"port"`

	// EffectiveExpirationDate for noted pins in time.RFC3339 format.
	EffectiveExpirationDate string `json:"effective-expiration-date"`

	// IncludeSubdomains indicates whether or not the UA has noted the
	// includeSubDomains directive for the Known Pinned Host.
	IncludeSubdomains bool `json:"include-subdomains"`

	// NotedHostname indicates the hostname that the UA noted when it noted
	// the Known Pinned Host. This field allows operators to understand why
	// Pin Validation was performed for, e.g., foo.example.com when the
	// noted Known Pinned Host was example.com with includeSubDomains set.
	NotedHostname string `json:"noted-hostname"`

	// ServedCertificateChain is the certificate chain, as served by
	// the Known Pinned Host during TLS session setup.  It is provided as an
	// array of strings; each string pem1, ... pemN is the Privacy-Enhanced
	// Mail (PEM) representation of each X.509 certificate as described in
	// [RFC7468].
	ServedCertificateChain []string `json:"served-certificate-chain"`

	// ValidatedCertificateChain is the certificate chain, as
	// constructed by the UA during certificate chain verification.  (This
	// may differ from the served-certificate-chain.)  It is provided as an
	// array of strings; each string pem1, ... pemN is the PEM
	// representation of each X.509 certificate as described in [RFC7468].
	// UAs that build certificate chains in more than one way during the
	// validation process SHOULD send the last chain built.  In this way,
	// they can avoid keeping too much state during the validation process.
	ValidatedCertificateChain []string `json:"validated-certificate-chain"`

	// The known-pins are the Pins that the UA has noted for the Known
	// Pinned Host.  They are provided as an array of strings with the
	// syntax: known-pin = token "=" quoted-string
	// e.g.:
	// ```
	// "known-pins": [
	//   'pin-sha256="d6qzRu9zOECb90Uez27xWltNsj0e1Md7GkYYkVoZWmM="',
	//   "pin-sha256=\"E9CZ9INDbd+2eRQozYqqbQ2yXLVKB9+xcprMF+44U1g=\""
	// ]
	// ```
	KnownPins []string `json:"known-pins"`

	// AppVersion is used to set `x-pm-appversion` json format from datatheorem/TrustKit.
	AppVersion string `json:"app-version"`
}

// NewTLSReport constructs a new TLSreport configured with the given app version and known pinned public keys.
func NewTLSReport(host, port, server string, certChain, knownPins []string, appVersion string) (report TLSReport) {
	// If we can't parse the port for whatever reason, it doesn't really matter; we should report anyway.
	intPort, _ := strconv.Atoi(port)

	report = TLSReport{
		Hostname:                  host,
		Port:                      intPort,
		EffectiveExpirationDate:   time.Now().Add(365 * 24 * 60 * 60 * time.Second).Format(time.RFC3339),
		IncludeSubdomains:         false,
		NotedHostname:             server,
		ValidatedCertificateChain: []string{},
		ServedCertificateChain:    certChain,
		KnownPins:                 knownPins,
		AppVersion:                appVersion,
	}

	return
}

// postCertIssueReport posts the given TLS report to the standard TLS Report URI.
func postCertIssueReport(report TLSReport, userAgent string) {
	b, err := json.Marshal(report)
	if err != nil {
		logrus.WithError(err).Error("Failed to marshal TLS report")
		return
	}

	req, err := http.NewRequest("POST", TLSReportURI, bytes.NewReader(b))
	if err != nil {
		logrus.WithError(err).Error("Failed to create http request")
		return
	}

	req.Header.Add("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("x-pm-apiversion", strconv.Itoa(Version))
	req.Header.Set("x-pm-appversion", report.AppVersion)

	logrus.WithField("request", req).Warn("Reporting TLS mismatch")
	res, err := (&http.Client{}).Do(req)
	if err != nil {
		logrus.WithError(err).Error("Failed to report TLS mismatch")
		return
	}

	logrus.WithField("response", res).Error("Reported TLS mismatch")

	if res.StatusCode != http.StatusOK {
		logrus.WithField("status", http.StatusOK).Error("StatusCode was not OK")
	}

	_, _ = ioutil.ReadAll(res.Body)
	_ = res.Body.Close()
}
