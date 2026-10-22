package domain

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestStreamConfluenceArchivePropagatesReadFailure(t *testing.T) {
	manifest, _ := archiveFixture()
	failure := errors.New("object stream disconnected")
	err := StreamConfluenceArchive(io.Discard, manifest, func(string) ([]byte, error) { return nil, failure })
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
}

func archiveFixture() (ConfluenceArchiveManifest, map[string][]byte) {
	manifest := NewConfluenceManifest(time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC))
	files := map[string][]byte{}
	for _, key := range []string{"GAMES", "TEAM"} {
		id := ConfluenceSpaceID("https://example.com/wiki", key)
		page := ConfluenceArchivePage{ID: "123", Title: "页面", URL: "https://example.com/wiki/pages/viewpage.action?pageId=123", Version: 2, Path: "父目录/页面", ModifiedAt: time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC), Files: []ConfluenceArchiveFile{}}
		for _, name := range []string{"page.md", "assets/asset-0123456789abcdef01234567.png"} {
			data := []byte("image bytes")
			if name == "page.md" {
				data = []byte("---\nconfluence_page_id: \"123\"\nconfluence_version: 2\n---\n\n# Hello\n![image](./assets/asset-0123456789abcdef01234567.png)")
			}
			filename, _ := ConfluenceArchivePath(id, key, page.Path, page.Title, name)
			page.Files = append(page.Files, ConfluenceArchiveFile{Name: name, Path: filename, Size: int64(len(data)), SHA256: ConfluenceFileHash(data)})
			files[filename] = data
		}
		manifest.Spaces = append(manifest.Spaces, ConfluenceArchiveSpace{ID: id, BaseURL: "https://example.com/wiki", Key: key, Pages: []ConfluenceArchivePage{page}})
	}
	return manifest, files
}

func rawArchive(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, data := range entries {
		entry, err := writer.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestConfluenceArchiveRoundTrip(t *testing.T) {
	manifest, files := archiveFixture()
	original, _ := json.Marshal(manifest)
	var buffer bytes.Buffer
	if err := WriteConfluenceArchive(&buffer, manifest, files); err != nil {
		t.Fatal(err)
	}
	restored, contents, err := ReadConfluenceArchive(buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	actual, _ := json.Marshal(restored)
	if !bytes.Equal(original, actual) {
		t.Fatal("manifest changed")
	}
	for name, data := range files {
		if !bytes.Equal(data, contents[name]) {
			t.Fatal("bytes changed", name)
		}
	}
	reader, _ := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	directories := 0
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			directories++
		}
	}
	if directories < 6 {
		t.Fatal("missing ancestor directories")
	}
}

func TestConfluenceArchiveRejectsCorruptAndUnsafeInputs(t *testing.T) {
	for _, mode := range []string{"missing-manifest", "invalid-manifest", "missing-file", "extra-file", "checksum", "wrong-identity", "traversal", "absolute", "backslash", "crc"} {
		t.Run(mode, func(t *testing.T) {
			manifest, files := archiveFixture()
			raw, _ := json.Marshal(manifest)
			entries := map[string][]byte{ConfluenceManifestName: raw}
			for name, data := range files {
				entries[name] = data
			}
			filename := manifest.Spaces[0].Pages[0].Files[0].Path
			switch mode {
			case "missing-manifest":
				delete(entries, ConfluenceManifestName)
			case "invalid-manifest":
				entries[ConfluenceManifestName] = []byte("{")
			case "missing-file":
				delete(entries, filename)
			case "extra-file":
				entries["unknown.txt"] = []byte("unknown")
			case "checksum":
				entries[filename] = []byte("changed")
			case "wrong-identity":
				content := bytes.ReplaceAll(entries[filename], []byte("123"), []byte("999"))
				entries[filename] = content
				manifest.Spaces[0].Pages[0].Files[0].SHA256 = ConfluenceFileHash(content)
				raw, _ = json.Marshal(manifest)
				entries[ConfluenceManifestName] = raw
			case "traversal":
				entries["../secret"] = []byte("x")
			case "absolute":
				entries["/secret"] = []byte("x")
			case "backslash":
				entries["a\\b"] = []byte("x")
			}
			data := rawArchive(t, entries)
			if mode == "crc" {
				index := bytes.Index(data, []byte("image bytes"))
				if index < 0 {
					t.Fatal("fixture missing")
				}
				data[index] = 'X'
			}
			if _, _, err := ReadConfluenceArchive(data); err == nil {
				t.Fatal("invalid archive accepted")
			}
		})
	}
	for _, mode := range []string{"symlink", "duplicate", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			var buffer bytes.Buffer
			writer := zip.NewWriter(&buffer)
			switch mode {
			case "symlink":
				header := &zip.FileHeader{Name: "link", Method: zip.Store}
				header.SetMode(os.ModeSymlink | 0600)
				entry, _ := writer.CreateHeader(header)
				entry.Write([]byte("target"))
			case "duplicate":
				writer.Create("same")
				writer.Create("SAME")
			case "oversize":
				writer.CreateRaw(&zip.FileHeader{Name: "large", Method: zip.Store, UncompressedSize64: ConfluenceFileLimit + 1})
			}
			writer.Close()
			if _, _, err := ReadConfluenceArchive(buffer.Bytes()); err == nil {
				t.Fatal("unsafe entry accepted")
			}
		})
	}
	if _, _, err := ReadConfluenceArchive([]byte("not zip")); err == nil {
		t.Fatal("invalid zip accepted")
	}
}

func TestConfluenceManifestValidation(t *testing.T) {
	for _, mode := range []string{"schema", "time", "space-id", "space-url", "space-duplicate", "page-id", "page-path", "page-time", "page-version", "page-url", "page-duplicate", "file-hash", "file-name", "file-path", "file-size", "file-duplicate", "missing-markdown"} {
		t.Run(mode, func(t *testing.T) {
			manifest, _ := archiveFixture()
			space := &manifest.Spaces[0]
			page := &space.Pages[0]
			file := &page.Files[0]
			switch mode {
			case "schema":
				manifest.SchemaVersion = 2
			case "time":
				manifest.ExportedAt = time.Time{}
			case "space-id":
				space.ID = "bad"
			case "space-url":
				space.BaseURL = "http://example.com/wiki"
			case "space-duplicate":
				manifest.Spaces = append(manifest.Spaces, *space)
			case "page-id":
				page.ID = "../1"
			case "page-path":
				page.Path = "../page"
			case "page-time":
				page.ModifiedAt = time.Time{}
			case "page-version":
				page.Version = 0
			case "page-url":
				page.URL = "https://other.example.com/pages/123"
			case "page-duplicate":
				space.Pages = append(space.Pages, *page)
			case "file-hash":
				file.SHA256 = "bad"
			case "file-name":
				file.Name = "unknown.txt"
				file.Path, _ = ConfluenceArchivePath(space.ID, space.Key, page.Path, page.Title, file.Name)
			case "file-path":
				file.Path = "other.md"
			case "file-size":
				file.Size = -1
			case "file-duplicate":
				page.Files = append(page.Files, *file)
			case "missing-markdown":
				page.Files = page.Files[1:]
			}
			if err := ValidateConfluenceManifest(manifest); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
}

type archiveErrorWriter struct{}

func (archiveErrorWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestConfluenceArchivePathsAndWriteErrors(t *testing.T) {
	for _, value := range []string{"", "../x", "/x", "a//b", "a/./b", "C:/file", "a\\b", "a\x00b", strings.Repeat("x", 4097)} {
		if ValidConfluencePath(value) {
			t.Fatal(value)
		}
	}
	if !ValidConfluencePath("中文/目录") {
		t.Fatal("unicode rejected")
	}
	for title, want := range map[string]string{"页面/标题": "页面-标题", "CON": "_CON", " Name. ": "Name"} {
		got, err := ConfluenceTitleName(title)
		if err != nil || got != want {
			t.Fatal(got, err)
		}
	}
	if _, err := ConfluenceTitleName(" . "); err == nil {
		t.Fatal("empty title accepted")
	}
	if _, err := ConfluenceArchivePath("bad", "SPACE", "dir", "title", "page.md"); err == nil {
		t.Fatal("bad ID accepted")
	}
	manifest, files := archiveFixture()
	if err := WriteConfluenceArchive(archiveErrorWriter{}, manifest, files); err == nil {
		t.Fatal("writer error ignored")
	}
	delete(files, manifest.Spaces[0].Pages[0].Files[0].Path)
	if err := WriteConfluenceArchive(io.Discard, manifest, files); err == nil {
		t.Fatal("missing file ignored")
	}
}
