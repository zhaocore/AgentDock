package rustfs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

const archiveChunkSize = 16 * 1024 * 1024

var ErrArchiveTooLarge = errors.New("archive upload exceeds size limit")

type archiveMetadata struct {
	Size int64 `json:"size"`
}

// PutArchive 将上传流分块写入私有存储，全部成功后才发布长度标记。
func PutArchive(ctx context.Context, objects Objects, key string, input io.Reader, limit int64) error {
	reader := io.LimitReader(input, limit+1)
	buffer := make([]byte, archiveChunkSize)
	var total int64
	for part := 0; ; part++ {
		n, err := io.ReadFull(reader, buffer)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return fmt.Errorf("read archive upload: %w", err)
		}
		total += int64(n)
		if total > limit {
			return fmt.Errorf("%w: ZIP limit %d GiB", ErrArchiveTooLarge, limit/(1024*1024*1024))
		}
		if n > 0 {
			if err := objects.Put(ctx, archiveChunkKey(key, int64(part)), buffer[:n], "application/octet-stream"); err != nil {
				return err
			}
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
	}
	metadata, err := json.Marshal(archiveMetadata{Size: total})
	if err != nil {
		return err
	}
	return objects.Put(ctx, key+"/metadata.json", metadata, "application/json")
}

func archiveChunkKey(key string, part int64) string {
	return fmt.Sprintf("%s/%06d", key, part)
}

// OpenArchive 用分块读取实现 ZIP 所需的随机访问，最多缓存一个块。
func OpenArchive(ctx context.Context, objects Objects, key string, limit int64) (io.ReaderAt, int64, error) {
	data, err := objects.Get(ctx, key+"/metadata.json")
	if err != nil {
		return nil, 0, fmt.Errorf("read staged ZIP metadata; upload ZIP again: %w", err)
	}
	var metadata archiveMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, 0, fmt.Errorf("decode staged ZIP size: %w", err)
	}
	if metadata.Size <= 0 || metadata.Size > limit {
		return nil, 0, fmt.Errorf("invalid staged ZIP size %d", metadata.Size)
	}
	return &archiveReader{ctx: ctx, objects: objects, key: key, size: metadata.Size, cachedPart: -1}, metadata.Size, nil
}

type archiveReader struct {
	ctx        context.Context
	objects    Objects
	key        string
	size       int64
	mu         sync.Mutex
	cachedPart int64
	cache      []byte
}

// ReadAt 按块读取并验证长度，支持跨块和 ZIP64 的大偏移。
func (r *archiveReader) ReadAt(output []byte, offset int64) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if offset < 0 {
		return 0, fmt.Errorf("negative ZIP offset %d", offset)
	}
	n := 0
	for n < len(output) {
		if err := r.ctx.Err(); err != nil {
			return n, err
		}
		if offset >= r.size {
			return n, io.EOF
		}
		part := offset / archiveChunkSize
		if part != r.cachedPart {
			data, err := r.objects.Get(r.ctx, archiveChunkKey(r.key, part))
			if err != nil {
				return n, err
			}
			expected := min(int64(archiveChunkSize), r.size-part*archiveChunkSize)
			if int64(len(data)) != expected {
				return n, fmt.Errorf("staged ZIP chunk %d has %d bytes, expected %d", part, len(data), expected)
			}
			r.cache, r.cachedPart = data, part
		}
		copied := copy(output[n:], r.cache[offset%archiveChunkSize:])
		n += copied
		offset += int64(copied)
	}
	return n, nil
}
