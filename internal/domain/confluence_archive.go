package domain

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// ZIP 导入同时限制压缩大小、展开大小、文件大小与条目数。
const (
	ConfluenceZIPLimit            = 100 * 1024 * 1024 * 1024
	ConfluenceExportZIPLimit      = 100 * 1024 * 1024 * 1024
	ConfluenceExportExpandedLimit = 500 * 1024 * 1024 * 1024
	ConfluenceExpandedLimit       = 500 * 1024 * 1024 * 1024
	ConfluenceFileLimit           = 640 * 1024 * 1024
	ConfluenceEntryLimit          = 1000000
	ConfluenceManifestName        = "confluence-library.json"
)

// ConfluenceSpaceID 使用实例和 Space 标识避免同名空间互相覆盖。
func ConfluenceSpaceID(base, key string) string {
	sum := sha256.Sum256([]byte(strings.TrimRight(base, "/") + "\n" + key))
	return hex.EncodeToString(sum[:])
}

// ValidConfluencePath 拒绝路径穿越、绝对路径和跨平台歧义。
func ValidConfluencePath(value string) bool {
	if value == "" || strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\\:") || path.Clean(value) != value || len(value) > 4096 || strings.Count(value, "/") > 128 {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "." || part == ".." || part == "" {
			return false
		}
	}
	return !strings.ContainsFunc(value, unicode.IsControl)
}

// ConfluenceTitleName 与既有技能的标题命名约定保持一致。
func ConfluenceTitleName(title string) (string, error) {
	name := strings.Map(func(r rune) rune {
		if strings.ContainsRune("<>:\"/\\|?*", r) || unicode.IsControl(r) {
			return '-'
		}
		return r
	}, title)
	name = strings.TrimRight(strings.TrimSpace(name), ". ")
	if name == "" {
		return "", fmt.Errorf("empty Confluence title filename")
	}
	if regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[1-9]|lpt[1-9])(?:\.|$)`).MatchString(name) {
		name = "_" + name
	}
	return name, nil
}

// ConfluenceArchivePath 为可直接阅读的标题目录生成 ZIP 路径。
func ConfluenceArchivePath(spaceID, spaceKey, pagePath, title, name string) (string, error) {
	root, err := ConfluenceTitleName(spaceKey)
	if err != nil {
		return "", err
	}
	if len(spaceID) != 64 {
		return "", fmt.Errorf("invalid Space ID")
	}
	if name == "page.md" {
		titleName, err := ConfluenceTitleName(title)
		if err != nil {
			return "", err
		}
		name = titleName + ".md"
	}
	result := root + " [" + spaceID[:12] + "]/" + pagePath + "/" + name
	if !ValidConfluencePath(result) {
		return "", fmt.Errorf("invalid ZIP path %q", result)
	}
	return result, nil
}

// ConfluenceFileHash 校验完整字节，避免凭文件时间或文件名判断内容相同。
func ConfluenceFileHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ValidateConfluenceManifest 校验来源身份、文件清单和路径唯一性。
func ValidateConfluenceManifest(manifest ConfluenceArchiveManifest) error {
	if manifest.SchemaVersion != 1 || manifest.ExportedAt.IsZero() || len(manifest.Spaces) == 0 {
		return fmt.Errorf("ZIP requires supported Confluence manifest and exported_at")
	}
	spaces := map[string]bool{}
	paths := map[string]bool{}
	for _, space := range manifest.Spaces {
		base, err := url.Parse(space.BaseURL)
		if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || strings.TrimSpace(space.Key) == "" || space.ID != ConfluenceSpaceID(space.BaseURL, space.Key) || spaces[space.ID] {
			return fmt.Errorf("invalid or duplicate ZIP Space %q", space.Key)
		}
		spaces[space.ID] = true
		pages := map[string]bool{}
		pagePaths := map[string]bool{}
		for _, page := range space.Pages {
			source, err := url.Parse(page.URL)
			if err != nil || source.Scheme != base.Scheme || source.Host != base.Host || source.User != nil || !strings.HasPrefix(source.Path, strings.TrimRight(base.Path, "/")+"/") || !regexp.MustCompile(`^\d+$`).MatchString(page.ID) || pages[page.ID] || page.Version < 1 || strings.TrimSpace(page.Title) == "" || !ValidConfluencePath(page.Path) || pagePaths[strings.ToLower(page.Path)] || page.ModifiedAt.IsZero() {
				return fmt.Errorf("invalid or duplicate ZIP page %q", page.ID)
			}
			pages[page.ID] = true
			pagePaths[strings.ToLower(page.Path)] = true
			names := map[string]bool{}
			hasMarkdown := false
			for _, file := range page.Files {
				expected, err := ConfluenceArchivePath(space.ID, space.Key, page.Path, page.Title, file.Name)
				if err != nil || file.Path != expected || file.Size < 0 || file.Size > ConfluenceFileLimit || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(file.SHA256) || names[file.Name] || paths[strings.ToLower(file.Path)] {
					return fmt.Errorf("invalid ZIP file %q", file.Path)
				}
				if file.Name != "page.md" && !regexp.MustCompile(`^assets/asset-[a-f0-9]{24}\.[a-z0-9]{1,8}$`).MatchString(file.Name) {
					return fmt.Errorf("unsupported ZIP filename %q", file.Name)
				}
				if file.Name == "page.md" {
					hasMarkdown = true
				}
				names[file.Name] = true
				paths[strings.ToLower(file.Path)] = true
			}
			if !hasMarkdown {
				return fmt.Errorf("ZIP page %s lacks Markdown", page.ID)
			}
		}
	}
	for filename := range paths {
		for parent := path.Dir(filename); parent != "."; parent = path.Dir(parent) {
			if paths[parent] {
				return fmt.Errorf("ZIP file/directory collision %s", parent)
			}
		}
	}
	return nil
}

// ReadConfluenceArchive 为内存中的小型 ZIP 提供完整往返校验。
func ReadConfluenceArchive(data []byte) (ConfluenceArchiveManifest, map[string][]byte, error) {
	manifest, entries, err := OpenConfluenceArchive(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return manifest, nil, err
	}
	files := map[string][]byte{}
	for name, entry := range entries {
		content, err := ReadConfluenceArchiveEntry(entry)
		if err != nil {
			return manifest, nil, err
		}
		files[name] = content
	}
	return manifest, files, nil
}

// ReadConfluenceArchiveEntry 有界读取一个 ZIP 文件，校验完整长度与 CRC。
func ReadConfluenceArchiveEntry(file *zip.File) ([]byte, error) {
	stream, err := file.Open()
	if err != nil {
		return nil, err
	}
	content, readErr := io.ReadAll(io.LimitReader(stream, ConfluenceFileLimit+1))
	closeErr := stream.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read ZIP file %s: %w", file.Name, readErr)
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(content) > ConfluenceFileLimit || uint64(len(content)) != file.UncompressedSize64 {
		return nil, fmt.Errorf("ZIP file size mismatch %s", file.Name)
	}
	return content, nil
}

// OpenConfluenceArchive 随机读取 ZIP，逐文件校验后仅保留清单和文件索引。
func OpenConfluenceArchive(input io.ReaderAt, size int64) (ConfluenceArchiveManifest, map[string]*zip.File, error) {
	var manifest ConfluenceArchiveManifest
	if size > ConfluenceZIPLimit {
		return manifest, nil, fmt.Errorf("ZIP exceeds 10 GiB")
	}
	reader, err := zip.NewReader(input, size)
	if err != nil {
		return manifest, nil, fmt.Errorf("read ZIP: %w", err)
	}
	if len(reader.File) > ConfluenceEntryLimit {
		return manifest, nil, fmt.Errorf("ZIP exceeds %d entries (files, directories and manifest): got %d; import a smaller archive", ConfluenceEntryLimit, len(reader.File))
	}
	files := map[string]*zip.File{}
	seen := map[string]bool{}
	var expanded uint64
	for _, file := range reader.File {
		name := strings.TrimSuffix(file.Name, "/")
		if !ValidConfluencePath(name) || seen[strings.ToLower(name)] || (!file.FileInfo().IsDir() && !file.Mode().IsRegular()) {
			return manifest, nil, fmt.Errorf("unsafe or duplicate ZIP entry %q", file.Name)
		}
		seen[strings.ToLower(name)] = true
		if file.FileInfo().IsDir() {
			continue
		}
		if file.UncompressedSize64 > ConfluenceFileLimit || expanded+file.UncompressedSize64 > ConfluenceExpandedLimit {
			return manifest, nil, fmt.Errorf("ZIP exceeds file or expanded size limit")
		}
		expanded += file.UncompressedSize64
		files[file.Name] = file
	}
	metadata, ok := files[ConfluenceManifestName]
	if !ok {
		return manifest, nil, fmt.Errorf("ZIP requires confluence-library.json manifest")
	}
	raw, err := ReadConfluenceArchiveEntry(metadata)
	if err != nil {
		return manifest, nil, err
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return manifest, nil, fmt.Errorf("invalid ZIP manifest: %w", err)
	}
	if err := ValidateConfluenceManifest(manifest); err != nil {
		return manifest, nil, err
	}
	expected := 1
	for _, space := range manifest.Spaces {
		for _, page := range space.Pages {
			for _, file := range page.Files {
				entry, ok := files[file.Path]
				if !ok || entry.UncompressedSize64 != uint64(file.Size) {
					return manifest, nil, fmt.Errorf("ZIP missing or corrupt file %s", file.Path)
				}
				content, err := ReadConfluenceArchiveEntry(entry)
				if err != nil {
					return manifest, nil, err
				}
				if ConfluenceFileHash(content) != file.SHA256 {
					return manifest, nil, fmt.Errorf("ZIP missing or corrupt file %s", file.Path)
				}
				if file.Name == "page.md" {
					identifier := regexp.MustCompile(`(?m)^confluence_page_id: "([0-9]+)"$`).FindSubmatch(content)
					version := regexp.MustCompile(`(?m)^confluence_version: ([0-9]+)$`).FindSubmatch(content)
					if len(identifier) != 2 || string(identifier[1]) != page.ID || len(version) != 2 || string(version[1]) != fmt.Sprint(page.Version) {
						return manifest, nil, fmt.Errorf("ZIP Markdown identity/version mismatch page_id=%s", page.ID)
					}
				}
				expected++
			}
		}
	}
	if len(files) != expected {
		return manifest, nil, fmt.Errorf("ZIP contains files absent from manifest")
	}
	delete(files, ConfluenceManifestName)
	return manifest, files, nil
}

// WriteConfluenceArchive 写出清单和可直接阅读的目录，不修改输入。
func WriteConfluenceArchive(output io.Writer, manifest ConfluenceArchiveManifest, files map[string][]byte) error {
	return StreamConfluenceArchive(output, manifest, func(filename string) ([]byte, error) {
		content, ok := files[filename]
		if !ok {
			return nil, fmt.Errorf("missing export file %s", filename)
		}
		return content, nil
	})
}

// StreamConfluenceArchive 逐文件读取并校验后压缩，内存不随 ZIP 总量增长。
func StreamConfluenceArchive(output io.Writer, manifest ConfluenceArchiveManifest, load func(string) ([]byte, error)) error {
	if err := ValidateConfluenceManifest(manifest); err != nil {
		return err
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if len(raw) > ConfluenceFileLimit {
		return fmt.Errorf("manifest exceeds %d MiB (size %d bytes); export a smaller directory", ConfluenceFileLimit/(1024*1024), len(raw))
	}
	writer := zip.NewWriter(output)
	entry, err := writer.Create(ConfluenceManifestName)
	if err != nil {
		return err
	}
	if _, err = entry.Write(raw); err != nil {
		return err
	}
	directories := map[string]bool{}
	entries := 1
	for _, space := range manifest.Spaces {
		for _, page := range space.Pages {
			for _, file := range page.Files {
				content, err := load(file.Path)
				if err != nil {
					return fmt.Errorf("read export file %s: %w", file.Path, err)
				}
				if int64(len(content)) != file.Size || ConfluenceFileHash(content) != file.SHA256 {
					return fmt.Errorf("missing or corrupt export file %s", file.Path)
				}
				var ancestors []string
				for parent := path.Dir(file.Path); parent != "."; parent = path.Dir(parent) {
					ancestors = append(ancestors, parent)
				}
				for index := len(ancestors) - 1; index >= 0; index-- {
					directory := ancestors[index] + "/"
					if !directories[directory] {
						entries++
						if entries > ConfluenceEntryLimit {
							return fmt.Errorf("ZIP exceeds %d entries (files, directories and manifest); export a smaller directory", ConfluenceEntryLimit)
						}
						if _, err = writer.Create(directory); err != nil {
							return err
						}
						directories[directory] = true
					}
				}
				entries++
				if entries > ConfluenceEntryLimit {
					return fmt.Errorf("ZIP exceeds %d entries (files, directories and manifest); export a smaller directory", ConfluenceEntryLimit)
				}
				header := &zip.FileHeader{Name: file.Path, Method: zip.Deflate, Modified: page.ModifiedAt.UTC()}
				header.SetMode(0600)
				entry, err = writer.CreateHeader(header)
				if err != nil {
					return err
				}
				if _, err = entry.Write(content); err != nil {
					return err
				}
			}
		}
	}
	return writer.Close()
}

// NewConfluenceManifest 初始化可跨环境往返的清单。
func NewConfluenceManifest(now time.Time) ConfluenceArchiveManifest {
	return ConfluenceArchiveManifest{SchemaVersion: 1, ExportedAt: now, Spaces: []ConfluenceArchiveSpace{}}
}
