package main

import (
	"math"
	"sync"
	"time"

	"github.com/Financial-Times/go-logger/v2"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	sleepTime  = 100
	maxBackoff = 9
	minBackoff = 0
)

type LogRetry interface {
	Enqueue(s string)
}

type LogProcessor interface {
	LogRetry
	Start()
	Stop()
	Dequeue() ([]string, error)
}

type logProcessor struct {
	sync.Mutex
	forwarder  Forwarder
	cache      Cache
	stopped    bool
	inChan     chan string
	outChan    chan string
	wg         sync.WaitGroup
	chanBuffer int
	workers    int
	uppLogger  *logger.UPPLogger
}

var queueLatency prometheus.Observer

func NewLogProcessor(forwarder Forwarder, cache Cache, config appConfig) LogProcessor {
	if queueLatency == nil {
		queueLatency = registerHistogram("queue_latency", "Post queue latency", []float64{.00001, .000015, .00002, .000025, .00003, .00004, .00005, .00006})
	}
	return &logProcessor{
		forwarder:  forwarder,
		cache:      cache,
		wg:         sync.WaitGroup{},
		chanBuffer: config.chanBuffer,
		workers:    config.workers,
		uppLogger:  config.UPPLogger,
	}
}

func (logProcessor *logProcessor) Start() {
	mutex := sync.Mutex{}
	level := 0
	levelUp := false

	logProcessor.outChan = make(chan string, logProcessor.chanBuffer)

	for i := 0; i < logProcessor.workers; i++ {
		logProcessor.wg.Add(1)
		go func() {
			defer logProcessor.wg.Done()
			for msg := range logProcessor.outChan {
				logProcessor.forwarder.forward(msg, func(s string, err error) {
					if err != nil {
						// cache again and retry later
						logProcessor.Enqueue(s)

						mutex.Lock()
						if level < maxBackoff {
							levelUp = true
						}
						mutex.Unlock()
					}
				})
			}
		}()
	}

	logProcessor.inChan = make(chan string, logProcessor.chanBuffer)
	for i := 0; i < logProcessor.workers; i++ {
		logProcessor.wg.Add(1)
		go func() {
			defer logProcessor.wg.Done()
			for msg := range logProcessor.inChan {
				err := logProcessor.cache.Put(msg)
				if err != nil {
					logProcessor.uppLogger.Errorf("Unexpected error when caching messages: %v\n", err)
				}
			}
		}()
	}

	go func() {
		logProcessor.wg.Add(1)
		defer logProcessor.wg.Done()
		for !logProcessor.isStopped() {
			entries, err := logProcessor.Dequeue()
			if err != nil {
				logProcessor.uppLogger.Errorf("Failure retrieving logs from S3 %v\n", err)
			} else if len(entries) > 0 {
				logProcessor.uppLogger.Debugf("Read %v messages from S3\n", len(entries))
			}
			for _, entry := range entries {
				mutex.Lock()
				if levelUp {
					if level < maxBackoff {
						level++
					}
					levelUp = false
				} else if level > minBackoff {
					level--
				}
				mutex.Unlock()
				if level > 0 {
					sleepDuration := time.Duration((0.2*math.Pow(2, float64(level))-0.2)*1000) * time.Millisecond

					logProcessor.uppLogger.Infof("Sleeping for %v\n", sleepDuration)
					time.Sleep(sleepDuration)
				}
				logProcessor.uppLogger.Debug("Sending document to channel")
				prometheusTimer := prometheus.NewTimer(queueLatency)
				logProcessor.outChan <- entry
				prometheusTimer.ObserveDuration()
			}

			// don't overwhelm S3 when it's empty
			if len(entries) == 0 {
				time.Sleep(sleepTime * time.Millisecond)
			}
		}
	}()
}

func (logProcessor *logProcessor) Stop() {
	logProcessor.Lock()
	logProcessor.stopped = true
	logProcessor.Unlock()
	logProcessor.uppLogger.Infof("Waiting buffered channel consumer to finish processing messages\n")
	logProcessor.wg.Wait()
	close(logProcessor.outChan)
	close(logProcessor.inChan)
}

func (logProcessor *logProcessor) Enqueue(s string) {
	logProcessor.inChan <- s
}

func (logProcessor *logProcessor) Dequeue() ([]string, error) {
	return logProcessor.cache.ListAndDelete()
}
func (logProcessor *logProcessor) isStopped() bool {
	logProcessor.Lock()
	defer logProcessor.Unlock()
	return logProcessor.stopped
}
