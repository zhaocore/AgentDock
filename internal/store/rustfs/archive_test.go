package rustfs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 官方 SDK 通过 HTTP 验证分块写入、跨块读取及缓存，正文只用合成字节。
func TestArchiveSDKRoundTrip(t *testing.T) {
	objects := map[string][]byte{}
	gets := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PUT":
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			objects[r.URL.Path] = data
		case "GET":
			gets++
			data, ok := objects[r.URL.Path]
			if !ok {
				w.WriteHeader(404)
				return
			}
			w.Write(data)
		default:
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(400)
		}
	}))
	defer server.Close()
	client := New(server.URL, "us-east-1", "private", "access", "secret")
	data := bytes.Repeat([]byte("archive!"), archiveChunkSize/8+8)
	ctx := context.Background()
	if err := PutArchive(ctx, client, "staged", bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatal(err)
	}
	reader, size, err := OpenArchive(ctx, client, "staged", int64(len(data)))
	if err != nil || size != int64(len(data)) {
		t.Fatal(size, err)
	}
	output := make([]byte, 16)
	if n, err := reader.ReadAt(output, archiveChunkSize-8); n != len(output) || err != nil || !bytes.Equal(output, data[archiveChunkSize-8:archiveChunkSize+8]) {
		t.Fatal(n, err)
	}
	before := gets
	if _, err := reader.ReadAt(output[:8], archiveChunkSize); err != nil || gets != before {
		t.Fatal("chunk cache missed", err)
	}
	if n, err := reader.ReadAt(output, size-8); n != 8 || err != io.EOF {
		t.Fatal(n, err)
	}
	if _, err := reader.ReadAt(output, -1); err == nil {
		t.Fatal("negative offset accepted")
	}
	if n, err := reader.ReadAt(nil, size); n != 0 || err != nil {
		t.Fatal(n, err)
	}
}

type archiveTestObjects struct {
	data    map[string][]byte
	failPut string
}

func (m *archiveTestObjects) Put(_ context.Context, key string, data []byte, _ string) error {
	if key == m.failPut {
		return errors.New("write denied")
	}
	m.data[key] = bytes.Clone(data)
	return nil
}

func (m *archiveTestObjects) Get(_ context.Context, key string) ([]byte, error) {
	data, ok := m.data[key]
	if !ok {
		return nil, errors.New("object missing")
	}
	return data, nil
}

type failedArchiveInput struct{}

func (failedArchiveInput) Read([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestArchiveRejectsIncompleteUpload(t *testing.T) {
	for _, mode := range []string{"oversized", "read-error", "write-error", "metadata-error", "exact-block"} {
		t.Run(mode, func(t *testing.T) {
			objects := &archiveTestObjects{data: map[string][]byte{}}
			var input io.Reader = strings.NewReader("123456789")
			limit := int64(9)
			switch mode {
			case "oversized":
				limit = 8
			case "read-error":
				input = failedArchiveInput{}
			case "write-error":
				objects.failPut = "staged/000000"
			case "metadata-error":
				objects.failPut = "staged/metadata.json"
			case "exact-block":
				input = bytes.NewReader(make([]byte, archiveChunkSize))
				limit = archiveChunkSize
			}
			err := PutArchive(context.Background(), objects, "staged", input, limit)
			if mode == "exact-block" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil {
				t.Fatal("invalid upload succeeded")
			}
			if mode == "oversized" && !errors.Is(err, ErrArchiveTooLarge) {
				t.Fatal(err)
			}
			if _, ok := objects.data["staged/metadata.json"]; ok {
				t.Fatal("incomplete ZIP published")
			}
		})
	}
}

func TestArchiveReaderRejectsInvalidStorage(t *testing.T) {
	for _, mode := range []string{"missing-metadata", "bad-metadata", "empty", "oversized", "missing-chunk", "short-chunk", "cancelled", "large-offset"} {
		t.Run(mode, func(t *testing.T) {
			objects := &archiveTestObjects{data: map[string][]byte{"staged/metadata.json": []byte(`{"size":9}`)}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "missing-metadata":
				delete(objects.data, "staged/metadata.json")
			case "bad-metadata":
				objects.data["staged/metadata.json"] = []byte("bad")
			case "empty":
				objects.data["staged/metadata.json"] = []byte(`{"size":0}`)
			case "oversized":
				objects.data["staged/metadata.json"] = []byte(`{"size":10737418241}`)
			case "short-chunk":
				objects.data["staged/000000"] = []byte("short")
			case "cancelled":
				cancel()
			case "large-offset":
				objects.data["staged/metadata.json"] = []byte(`{"size":10737418240}`)
				objects.data["staged/000639"] = bytes.Repeat([]byte("x"), archiveChunkSize)
			}
			reader, size, err := OpenArchive(ctx, objects, "staged", 10*1024*1024*1024)
			if mode == "missing-metadata" || mode == "bad-metadata" || mode == "empty" || mode == "oversized" {
				if err == nil {
					t.Fatal("invalid metadata accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			buffer := make([]byte, 8)
			offset := int64(0)
			if mode == "large-offset" {
				offset = size - 8
			}
			_, err = reader.ReadAt(buffer, offset)
			if mode == "large-offset" {
				if err != nil || string(buffer) != "xxxxxxxx" {
					t.Fatal(err, string(buffer))
				}
			} else if err == nil {
				t.Fatal("invalid chunk accepted")
			}
		})
	}
}
