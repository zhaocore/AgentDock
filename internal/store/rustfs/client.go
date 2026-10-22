// Package rustfs 提供私有 S3 对象的内存读写接缝。
package rustfs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// Objects 是后台导出和鉴权读取共用的对象存储契约。
type Objects interface {
	Put(context.Context, string, []byte, string) error
	Get(context.Context, string) ([]byte, error)
}

// Client 使用官方 SDK 访问 RustFS，不公开桶。
type Client struct {
	api    *s3.Client
	bucket string
}

// New 创建路径式 S3 客户端，重试由官方 SDK 执行。
func New(endpoint, region, bucket, accessKey, secretKey string) *Client {
	return &Client{api: s3.New(s3.Options{Region: region, BaseEndpoint: aws.String(endpoint), UsePathStyle: true, Credentials: credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")}), bucket: bucket}
}

// Put 缺桶时自动创建私有桶；完整对象上传成功后才能推进业务检查点。
func (c *Client) Put(ctx context.Context, key string, data []byte, contentType string) error {
	input := &s3.PutObjectInput{Bucket: &c.bucket, Key: &key, Body: bytes.NewReader(data), ContentType: &contentType}
	_, err := c.api.PutObject(ctx, input)
	var apiError smithy.APIError
	if errors.As(err, &apiError) && apiError.ErrorCode() == "NoSuchBucket" {
		slog.Warn("RustFS bucket missing; creating before retry", "bucket", c.bucket, "key", key)
		create := &s3.CreateBucketInput{Bucket: &c.bucket}
		if region := c.api.Options().Region; region != "us-east-1" {
			create.CreateBucketConfiguration = &types.CreateBucketConfiguration{LocationConstraint: types.BucketLocationConstraint(region)}
		}
		_, createErr := c.api.CreateBucket(ctx, create)
		var owned *types.BucketAlreadyOwnedByYou
		if createErr != nil && !errors.As(createErr, &owned) {
			return fmt.Errorf("RustFS create bucket=%s for key=%s: %w", c.bucket, key, createErr)
		}
		input.Body = bytes.NewReader(data)
		_, err = c.api.PutObject(ctx, input)
	}
	if err != nil {
		return fmt.Errorf("RustFS put key=%s: %w", key, err)
	}
	return nil
}

// Get 有界读取对象，避免异常对象耗尽服务内存。
func (c *Client) Get(ctx context.Context, key string) ([]byte, error) {
	response, err := c.api.GetObject(ctx, &s3.GetObjectInput{Bucket: &c.bucket, Key: &key})
	if err != nil {
		return nil, fmt.Errorf("RustFS get key=%s: %w", key, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 128*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 128*1024*1024 {
		return nil, fmt.Errorf("RustFS object exceeds 128 MiB key=%s", key)
	}
	return data, nil
}

// List 列出私有前缀下全部对象，分页结果只返回键名。
func (c *Client) List(ctx context.Context, prefix string) ([]string, error) {
	paginator := s3.NewListObjectsV2Paginator(c.api, &s3.ListObjectsV2Input{Bucket: &c.bucket, Prefix: &prefix})
	keys := []string{}
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("RustFS list prefix=%s: %w", prefix, err)
		}
		for _, object := range page.Contents {
			if object.Key == nil || !bytes.HasPrefix([]byte(*object.Key), []byte(prefix)) {
				return nil, fmt.Errorf("RustFS list returned key outside prefix")
			}
			keys = append(keys, *object.Key)
		}
	}
	return keys, nil
}
