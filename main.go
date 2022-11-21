package main

import (
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	cli "github.com/jawher/mow.cli"

	health "github.com/Financial-Times/go-fthealth/v1_1"
	"github.com/Financial-Times/go-logger/v2"
	status "github.com/Financial-Times/service-status-go/httphandlers"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	namespace      = "upp"
	subsystem      = "splunk_forwarder"
	appDescription = "Forwards logs cached in S3 to Splunk"
)

var (
	labelNames = []string{"environment"}
	envLabel   prometheus.Labels
)

type appConfig struct {
	appSystemCode string
	appName       string
	port          string
	fwdURL        string
	env           string
	workers       int
	chanBuffer    int
	token         string
	bucket        string
	awsRegion     string
	UPPLogger     *logger.UPPLogger
}

func main() {

	app := initApp()
	err := app.Run(os.Args)
	if err != nil {
		log.Fatalf("App could not start, error=[%s]\n", err)
		return
	}
}

func initApp() *cli.Cli {
	app := cli.App("resilient-splunk-forwarder", appDescription)

	appSystemCode := app.String(cli.StringOpt{
		Name:   "app-system-code",
		Value:  "resilient-splunk-forwarder",
		Desc:   "System Code of the application",
		EnvVar: "APP_SYSTEM_CODE",
	})
	appName := app.String(cli.StringOpt{
		Name:   "app-name",
		Value:  "Resilient Splunk Forwarder",
		Desc:   "Application name",
		EnvVar: "APP_NAME",
	})
	port := app.String(cli.StringOpt{
		Name:   "port",
		Value:  "8080",
		Desc:   "Port to listen on",
		EnvVar: "APP_PORT",
	})
	fwdURL := app.String(cli.StringOpt{
		Name:   "url",
		Value:  "",
		Desc:   "The url to forward to",
		EnvVar: "FORWARD_URL",
	})
	env := app.String(cli.StringOpt{
		Name:   "env",
		Value:  "dummy",
		Desc:   "environment_tag value",
		EnvVar: "ENV",
	})
	workers := app.Int(cli.IntOpt{
		Name:   "workers",
		Value:  8,
		Desc:   "Number of concurrent workers",
		EnvVar: "WORKERS",
	})
	chanBuffer := app.Int(cli.IntOpt{
		Name:   "buffer",
		Value:  256,
		Desc:   "Channel buffer size",
		EnvVar: "CHAN_BUFFER",
	})
	token := app.String(cli.StringOpt{
		Name:   "token",
		Value:  "",
		Desc:   "Splunk HEC Authorization token",
		EnvVar: "TOKEN",
	})
	bucket := app.String(cli.StringOpt{
		Name:   "bucketName",
		Value:  "",
		Desc:   "S3 bucket for caching failed events",
		EnvVar: "BUCKET_NAME",
	})
	awsRegion := app.String(cli.StringOpt{
		Name:   "awsRegion",
		Value:  "",
		Desc:   "AWS region for S3",
		EnvVar: "AWS_REGION",
	})

	logLevel := app.String(cli.StringOpt{
		Name:   "logLevel",
		Value:  "INFO",
		Desc:   "Logging level (DEBUG, INFO, WARN, ERROR, PANIC)",
		EnvVar: "LOG_LEVEL",
	})

	app.Action = func() {

		config := appConfig{
			appSystemCode: *appSystemCode,
			appName:       *appName,
			port:          *port,
			fwdURL:        *fwdURL,
			env:           *env,
			workers:       *workers,
			chanBuffer:    *chanBuffer,
			token:         *token,
			bucket:        *bucket,
			awsRegion:     *awsRegion,
			UPPLogger:     logger.NewUPPLogger(*appSystemCode, *logLevel),
		}

		config.UPPLogger.Infof("[Startup] resilient-splunk-forwarder is starting ")

		config.UPPLogger.Infof("System code: %s, App Name: %s, Port: %s", *appSystemCode, *appName, *port)
		err := validateParams(config)
		if err != nil {
			config.UPPLogger.Fatal(err)
		}

		defer config.UPPLogger.Infof("Resilient Splunk forwarder: Stopped\n")

		s3, err := NewS3Service(config.bucket, config.awsRegion, config.env, config.UPPLogger)
		if err != nil {
			config.UPPLogger.Fatalf(err.Error())
		}
		envLabel = prometheus.Labels{"environment": config.env}

		splunkForwarder := NewSplunkForwarder(config)
		logProcessor := NewLogProcessor(splunkForwarder, s3, config)

		logProcessor.Start()

		healthService := newHealthService(
			&healthConfig{
				appSystemCode: *appSystemCode,
				appName:       *appName,
				port:          *port,
			},
			[]health.Check{
				{
					BusinessImpact:   "Logs are not reaching Splunk therefore monitoring may be affected",
					Name:             "Splunk healthcheck",
					PanicGuide:       "https://runbooks.in.ft.com/resilient-splunk-forwarder",
					Severity:         1,
					TechnicalSummary: "Latest request to Splunk HEC has returned an error - check journal file",
					Checker: func() (string, error) {
						err := splunkForwarder.getHealth()
						if err != nil {
							return "Splunk is not healthy", err
						}
						return "Splunk is healthy", nil
					},
				},
				{
					BusinessImpact:   "Logs can not be read from S3 and will probably be indexed with delay",
					Name:             "S3 healthcheck",
					PanicGuide:       "https://runbooks.in.ft.com/resilient-splunk-forwarder",
					Severity:         1,
					TechnicalSummary: "Latest request to S3 has returned an error - check journal file",
					Checker: func() (string, error) {
						err := s3.getHealth()
						if err != nil {
							return "S3 is not healthy", err
						}
						return "S3 is healthy", nil
					},
				},
			},
		)

		go func() {
			serveEndpoints(healthService, *appSystemCode, *appName, *port, config.UPPLogger)
		}()

		config.UPPLogger.Infof("Resilient Splunk forwarder (workers %v): Started\n", workers)
		waitForSignal()
	}

	return app
}

func serveEndpoints(healthService *healthService, appSystemCode string, appName string, port string, uppLogger *logger.UPPLogger) {

	serveMux := http.NewServeMux()

	hc := health.TimedHealthCheck{
		HealthCheck: health.HealthCheck{
			SystemCode: appSystemCode,
			Name:       appName, Description: appDescription,
			Checks: healthService.checks},
		Timeout: 10 * time.Second,
	}
	serveMux.HandleFunc(healthPath, health.Handler(hc))
	serveMux.HandleFunc(status.GTGPath, status.NewGoodToGoHandler(healthService.GTG))
	serveMux.HandleFunc(status.BuildInfoPath, status.BuildInfoHandler)
	serveMux.Handle("/metrics", promhttp.Handler())

	server := &http.Server{
		Addr:    ":" + port,
		Handler: serveMux,
	}

	wg := sync.WaitGroup{}

	wg.Add(1)
	go func() {
		if err := server.ListenAndServe(); err != nil {
			uppLogger.Infof("HTTP server closing with message: %v", err)
		}
		wg.Done()
	}()

	waitForSignal()
	uppLogger.Infof("[Shutdown] resilient-splunk-forwarder is shutting down")

	if err := server.Close(); err != nil {
		uppLogger.Errorf("Unable to stop http server: %v", err)
	}

	wg.Wait()
}

func waitForSignal() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
}

func validateParams(config appConfig) error {
	if len(config.fwdURL) == 0 { //Check whether -url parameter value was provided
		return errors.New("forwarder URL must be provided")
	}
	if len(config.token) == 0 { //Check whether -token parameter value was provided
		return errors.New("splunk token must be provided")
	}
	if len(config.bucket) == 0 { //Check whether -bucket parameter value was provided
		return errors.New("s3 bucket name must be provided")
	}

	return nil
}

func registerCounter(name, help string) prometheus.Counter {
	c := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      name,
			Help:      help,
		},
		labelNames)
	prometheus.MustRegister(c)
	if envLabel == nil {
		envLabel = prometheus.Labels{"environment": "dummy"}
	}
	return c.With(envLabel)
}

func registerHistogram(name, help string, buckets []float64) prometheus.Observer {
	h := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      name,
			Help:      help,
			Buckets:   buckets,
		},
		labelNames)
	prometheus.Register(h)
	if envLabel == nil {
		envLabel = prometheus.Labels{"environment": "dummy"}
	}
	return h.With(envLabel)
}
