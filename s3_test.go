package main

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/Financial-Times/go-logger/v2"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockS3Interface struct {
	mock.Mock
}

var sampleErr = errors.New("sample error")
var successResponse = "Ohai Mark"

func (m *mockS3Interface) ListObjectsV2(input *s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error) {
	if *input.Bucket == "simulated-error" {
		return nil, sampleErr
	}

	if *input.Bucket == "empty-response" {
		int64Val := int64(0)
		obj := &s3.ListObjectsV2Output{
			KeyCount: &int64Val,
		}
		return obj, nil
	}

	if *input.Bucket == "simulated-error-response" {
		int64Val := int64(1)
		key := "simulated-error-response"
		obj := &s3.ListObjectsV2Output{
			KeyCount: &int64Val,
			Prefix:   input.Prefix,
			Contents: []*s3.Object{
				{
					ETag: nil,
					Key:  &key,
				},
			},
		}
		return obj, nil
	}

	int64Val := int64(1)
	key := "test-key"
	obj := &s3.ListObjectsV2Output{
		KeyCount: &int64Val,
		Prefix:   input.Prefix,
		Contents: []*s3.Object{
			{
				ETag: nil,
				Key:  &key,
			},
		},
	}
	return obj, nil
}

func (m *mockS3Interface) DeleteObjects(input *s3.DeleteObjectsInput) (*s3.DeleteObjectsOutput, error) {
	if *input.Bucket == "simulated-delete-error" {
		return nil, sampleErr
	}

	return nil, nil
}
func (m *mockS3Interface) PutObject(input *s3.PutObjectInput) (*s3.PutObjectOutput, error) {
	return nil, nil
}

func (m *mockS3Interface) GetObject(input *s3.GetObjectInput) (*s3.GetObjectOutput, error) {
	if *input.Key == "simulated-error-response" {
		return nil, sampleErr
	}

	output := &s3.GetObjectOutput{
		Body: io.NopCloser(bytes.NewBufferString(successResponse)),
	}

	return output, nil
}

var _ s3Interface = (*mockS3Interface)(nil)

func Test_S3_failServiceCreation(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "MOCK_ID")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "MOCK_SECRET")
	s3service, errServiceCreation := NewS3Service("", "no-region", "", logger.NewUnstructuredLogger())

	assert.Equal(t, nil, errServiceCreation)
	assert.NotEqual(t, nil, s3service)
}

func Test_S3_success(t *testing.T) {
	s3InterfaceMock := &mockS3Interface{}
	s3service := &s3Service{
		bucketName:  "test-bucket",
		prefix:      "test-prefix",
		latestError: nil,
		svc:         s3InterfaceMock,
	}

	s3service.Put(`{event:"127.0.0.1 - - [21/Apr/2015:12:15:34 +0000] \"GET /eom-file/all/e09b49d6-e1fa-11e4-bb7f-00144feab7de HTTP/1.1\" 200 53706 919 919"}`)

	result, errListAndDelete := s3service.ListAndDelete()

	assert.Nil(t, s3service.getHealth())
	assert.Contains(t, result, successResponse)
	assert.Nil(t, errListAndDelete)
	assert.NotEqual(t, nil, s3service)
}

func Test_S3_error_delete(t *testing.T) {
	s3InterfaceMock := &mockS3Interface{}
	s3service := &s3Service{
		bucketName:  "simulated-delete-error",
		prefix:      "test-prefix",
		latestError: nil,
		svc:         s3InterfaceMock,
	}

	s3service.Put(`{event:"127.0.0.1 - - [21/Apr/2015:12:15:34 +0000] \"GET /eom-file/all/e09b49d6-e1fa-11e4-bb7f-00144feab7de HTTP/1.1\" 200 53706 919 919"}`)

	result, errListAndDelete := s3service.ListAndDelete()

	assert.Empty(t, result)
	assert.Equal(t, sampleErr, errListAndDelete)
	assert.NotEqual(t, nil, s3service)
}

func Test_S3_error_get(t *testing.T) {
	s3InterfaceMock := &mockS3Interface{}
	s3InterfaceMock.On("GetObject").
		Return(nil, sampleErr).
		Once()
	s3service := &s3Service{
		bucketName:  "simulated-error-response",
		prefix:      "test-prefix",
		latestError: nil,
		svc:         s3InterfaceMock,
	}

	//s3, _ := NewS3Service("test-bucket", "test-region", "test-prefix")

	s3service.Put(`{event:"127.0.0.1 - - [21/Apr/2015:12:15:34 +0000] \"GET /eom-file/all/e09b49d6-e1fa-11e4-bb7f-00144feab7de HTTP/1.1\" 200 53706 919 919"}`)

	result, errListAndDelete := s3service.ListAndDelete()

	assert.Empty(t, result)
	assert.Equal(t, sampleErr, errListAndDelete)
	assert.NotEqual(t, nil, s3service)
}

func Test_S3_empty(t *testing.T) {
	s3InterfaceMock := &mockS3Interface{}
	s3InterfaceMock.On("GetObject", mock.Anything).
		Return(nil, sampleErr).
		Once()
	s3service := &s3Service{
		bucketName:  "empty-response",
		prefix:      "test-prefix",
		latestError: nil,
		svc:         s3InterfaceMock,
	}

	result, errListAndDelete := s3service.ListAndDelete()

	assert.Empty(t, result)
	assert.Nil(t, errListAndDelete)
	assert.NotEqual(t, nil, s3service)
}
