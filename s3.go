package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Financial-Times/go-logger/v2"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/pborman/uuid"
	"go.uber.org/multierr"
)

const maxKeys = int64(100)

type Cache interface {
	Healthy
	ListAndDelete() ([]string, error)
	Put(obj string) error
}

type s3Interface interface {
	ListObjectsV2(input *s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error)
	DeleteObjects(input *s3.DeleteObjectsInput) (*s3.DeleteObjectsOutput, error)
	PutObject(input *s3.PutObjectInput) (*s3.PutObjectOutput, error)
	GetObject(input *s3.GetObjectInput) (*s3.GetObjectOutput, error)
}

type s3Service struct {
	bucketName  string
	prefix      string
	svc         s3Interface
	latestError error
}

var NewS3Service = func(bucketName, bucketRegion, prefix string, log *logger.UPPLogger) (Cache, error) {
	wrks := 8
	spareWorkers := 1

	hc := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          wrks + spareWorkers,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConnsPerHost:   wrks + spareWorkers,
			TLSHandshakeTimeout:   3 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
	sess, err := session.NewSession(
		&aws.Config{
			Region:     aws.String(bucketRegion),
			MaxRetries: aws.Int(1),
			HTTPClient: hc,
		})
	if err != nil {
		return nil, fmt.Errorf("Failed to create AWS session: %v", err)
	}

	v, err := sess.Config.Credentials.Get()
	if err != nil {
		return nil, fmt.Errorf("failed to obtain AWS authentication: %w", err)
	}
	log.WithField("ProviderName", v.ProviderName).Info("Establishing AWS session using authentication provider")

	svc := s3.New(sess)
	return &s3Service{bucketName: bucketName, prefix: prefix, svc: svc}, nil
}

func (s *s3Service) ListAndDelete() ([]string, error) {
	out, err := s.svc.ListObjectsV2(&s3.ListObjectsV2Input{
		Bucket:  aws.String(s.bucketName),
		Prefix:  aws.String(s.prefix),
		MaxKeys: aws.Int64(maxKeys),
	})
	if err != nil {
		return nil, err
	}
	s.latestError = err
	ids := []*s3.ObjectIdentifier{}
	vals := []string{}
	mutex := sync.Mutex{}
	wg := sync.WaitGroup{}
	getErr := error(nil)
	for _, obj := range out.Contents {
		ids = append(ids, &s3.ObjectIdentifier{Key: obj.Key})
		wg.Add(1)
		go func(o s3.Object) {
			defer wg.Done()
			val, err := s.Get(*o.Key)
			if err != nil {
				// don't capture latest error in case another instance has deleted them first
				mutex.Lock()
				getErr = err
				mutex.Unlock()
				return
			}

			mutex.Lock()
			vals = append(vals, val)
			mutex.Unlock()
		}(*obj)
	}
	wg.Wait()
	if getErr != nil {
		return nil, getErr
	}

	if *out.KeyCount > 0 {
		delOutput, err := s.svc.DeleteObjects(&s3.DeleteObjectsInput{
			Bucket: aws.String(s.bucketName),
			Delete: &s3.Delete{
				Objects: ids,
			},
		})
		if err != nil {
			// don't capture latest error in case another instance has deleted them first
			return nil, err
		}
		if delOutput != nil && len(delOutput.Errors) > 0 {
			var merr error
			for _, e := range delOutput.Errors {
				merr = multierr.Append(merr, fmt.Errorf("deleting %q: %s", *e.Key, *e.Code))
			}

			return nil, err
		}

		s.latestError = err
		return vals, nil
	}
	return nil, nil
}

func (s *s3Service) Put(obj string) error {
	uuid := fmt.Sprintf("%v/%v_%v", s.prefix, time.Now().UnixNano(), uuid.New())
	_, err := s.svc.PutObject(&s3.PutObjectInput{
		Bucket: aws.String(s.bucketName),
		Body:   strings.NewReader(obj),
		Key:    aws.String(uuid),
	})
	s.latestError = err
	return err
}

func (s *s3Service) Get(key string) (string, error) {
	val, err := s.svc.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(key),
	})
	if err != nil {
		return "", err
	}

	defer val.Body.Close()
	buf, err := io.ReadAll(val.Body)
	return string(buf), err
}

func (s *s3Service) getHealth() error {
	return s.latestError
}
