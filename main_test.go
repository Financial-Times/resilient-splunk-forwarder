package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Financial-Times/go-logger/v2"

	"github.com/stretchr/testify/assert"

	"github.com/prometheus/client_golang/prometheus"
)

var config appConfig

func TestMain(m *testing.M) {
	splunkTestServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bytes := make([]byte, r.ContentLength)
		r.Body.Read(bytes)
		defer r.Body.Close()
		body := string(bytes)
		if strings.Contains(body, "simulated_retry") {
			splunk.incErrors()
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			splunk.append(body)
			w.WriteHeader(http.StatusOK)
		}
	}))

	defer splunkTestServer.Close()

	graphiteTestServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	defer graphiteTestServer.Close()

	config = appConfig{}
	config.fwdURL = splunkTestServer.URL
	config.env = "dummy"
	config.workers = 8
	config.chanBuffer = 256
	config.token = "secret"
	config.bucket = "testbucket"
	config.UPPLogger = logger.NewUPPLogger("PANIC", "app-system-code")

	os.Setenv("TOKEN", config.token)
	os.Setenv("BUCKET_NAME", config.bucket)
	os.Setenv("AWS_BUCKET_REGION", "eu")
	os.Setenv("FORWARDER_URL", config.fwdURL)

	app := initApp()

	go func() {
		app.Run(os.Args)
	}()

	os.Exit(m.Run())
}

func Test_successValidateParams(t *testing.T) {
	validationConfig := config
	validationConfig.fwdURL = "test-fwdURL"

	validateParams(validationConfig)
}

func Test_failValidateParams(t *testing.T) {
	brokenConfig := config
	brokenConfig.fwdURL = ""

	err := validateParams(brokenConfig)

	if err == nil {
		t.Error("validation of the input parameters has failed")
	}
}

func Test_RegisterCounter(t *testing.T) {
	name := "fooCounter"
	help := "barDescription"
	registerCounter(name, help)
	duplicateCounter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      name,
			Help:      help,
		},
		labelNames)

	err := prometheus.Register(duplicateCounter)
	assert.NotNil(t, err, "Counter should've been registered already.")
	_, ok := err.(prometheus.AlreadyRegisteredError)
	assert.True(t, ok, "Expecting an 'AlreadyRegisteredError'.")
}

func Test_RegisterHistogram(t *testing.T) {
	name := "fooHistogram"
	help := "barDescription"
	registerHistogram(name, help, []float64{})
	duplicateHistogram := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      name,
			Help:      help,
		},
		labelNames)

	err := prometheus.Register(duplicateHistogram)
	assert.NotNil(t, err, "Histogram should've been registered already.")
	_, ok := err.(prometheus.AlreadyRegisteredError)
	assert.True(t, ok, "Expecting an 'AlreadyRegisteredError'.")
}
