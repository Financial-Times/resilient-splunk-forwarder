package main

import (
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	requestCounter   prometheus.Counter
	errorCounter     prometheus.Counter
	discardedCounter prometheus.Counter
	postTime         prometheus.Observer
)

type Forwarder interface {
	Healthy
	forward(s string, callback func(string, error))
}

type splunkClient struct {
	sync.Mutex
	config      appConfig
	client      *http.Client
	latestError error
}

func NewSplunkForwarder(config appConfig) Forwarder {
	initMetrics()
	tlsConfig := &tls.Config{InsecureSkipVerify: true}
	transport := &http.Transport{
		TLSClientConfig:     tlsConfig,
		MaxIdleConnsPerHost: config.workers,
	}
	client := &http.Client{Transport: transport}

	return &splunkClient{
		client: client,
		config: config,
	}
}

func (splunk *splunkClient) forward(s string, callback func(string, error)) {
	prometheusTimer := prometheus.NewTimer(postTime)
	defer prometheusTimer.ObserveDuration()

	req, err := http.NewRequest("POST", splunk.config.fwdURL, strings.NewReader(s))
	if err != nil {
		splunk.config.UPPLogger.Infof(err.Error())
	}
	tokenWithKeyword := strings.Join([]string{"Splunk", splunk.config.token}, " ") //join strings "Splunk" and value of -token argument
	req.Header.Set("Authorization", tokenWithKeyword)
	requestCounter.Inc()
	r, err := splunk.client.Do(req)
	if err != nil {
		errorCounter.Inc()
		splunk.config.UPPLogger.Infof(err.Error())
	} else {
		defer r.Body.Close()
		_, _ = io.Copy(io.Discard, r.Body)
		if r.StatusCode != 200 {
			errorCounter.Inc()
			splunk.config.UPPLogger.Infof("Unexpected status code %v (%v) when sending %v to %v\n", r.StatusCode, r.Status, s, splunk.config.fwdURL)
			if r.StatusCode != 400 {
				err = errors.New(r.Status)
			} else {
				discardedCounter.Inc()
				splunk.config.UPPLogger.Infof("Discarding malformed message\n")
			}
		}
	}
	splunk.setHealth(err)
	callback(s, err)
}

func (splunk *splunkClient) getHealth() error {
	splunk.Lock()
	defer splunk.Unlock()
	return splunk.latestError
}

func (splunk *splunkClient) setHealth(err error) {
	splunk.Lock()
	defer splunk.Unlock()
	splunk.latestError = err
}

func initMetrics() {
	postTime = registerHistogram("post_time", "HTTP Post time", []float64{.002, .003, .0035, .004, .0045, .005, .006, .007, .008, .009})
	errorCounter = registerCounter("error_count", "Number of errors connecting to splunk")
	requestCounter = registerCounter("request_count", "Number of requests to splunk")
	discardedCounter = registerCounter("discarded_count", "Number of discarded messages")
}
