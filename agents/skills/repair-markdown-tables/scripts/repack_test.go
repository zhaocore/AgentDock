package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhaocore/agentdock/internal/domain"
)

// localLibraryFixture 创建含断行表格和附件的真实本地文件库。
func localLibraryFixture(t *testing.T) (string, domain.ConfluenceArchiveManifest) {
	t.Helper()
	root := t.TempDir()
	base := "https://confluence.example.com"
	id := domain.ConfluenceSpaceID(base, "GAMES")
	page := domain.ConfluenceArchivePage{
		ID: "123", Title: "规则", URL: base + "/pages/viewpage.action?pageId=123", Version: 2,
		Path: "父目录/规则", ModifiedAt: time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC),
	}
	for _, name := range []string{"page.md", "assets/asset-0123456789abcdef01234567.png"} {
		content := []byte("attachment bytes")
		if name == "page.md" {
			content = []byte("---\nconfluence_page_id: \"123\"\nconfluence_version: 2\n---\n\n| \nID\n\n | \nRule\n\n |\n| --- | --- |\n| 1 | 检查 |\n")
		}
		filename, err := domain.ConfluenceArchivePath(id, "GAMES", page.Path, page.Title, name)
		if err != nil {
			t.Fatal(err)
		}
		local := filepath.Join(root, filepath.FromSlash(filename))
		if err := os.MkdirAll(filepath.Dir(local), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(local, content, 0600); err != nil {
			t.Fatal(err)
		}
		page.Files = append(page.Files, domain.ConfluenceArchiveFile{
			Name: name, Path: filename, Size: int64(len(content)), SHA256: domain.ConfluenceFileHash(content),
		})
	}
	manifest := domain.NewConfluenceManifest(time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC))
	manifest.Spaces = []domain.ConfluenceArchiveSpace{{ID: id, BaseURL: base, Key: "GAMES", Pages: []domain.ConfluenceArchivePage{page}}}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, domain.ConfluenceManifestName), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return root, manifest
}

func TestRepairAndPackImportRoundTrip(t *testing.T) {
	root, original := localLibraryFixture(t)
	before, err := os.ReadFile(filepath.Join(root, domain.ConfluenceManifestName))
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "repaired.zip")
	command := exec.Command("python3", "repair_and_pack.py", root, output)
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("repair and pack: %v: %s", err, result)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	manifest, files, err := domain.ReadConfluenceArchive(data)
	if err != nil {
		t.Fatal(err)
	}
	page := manifest.Spaces[0].Pages[0]
	want := original.Spaces[0].Pages[0]
	if page.ID != want.ID || page.Title != want.Title || page.URL != want.URL || page.Version != want.Version || !page.ModifiedAt.Equal(want.ModifiedAt) {
		t.Fatal("source metadata changed")
	}
	markdown := page.Files[0]
	if len(files) != 2 || !strings.Contains(string(files[markdown.Path]), "| ID | Rule |") || markdown.SHA256 == want.Files[0].SHA256 || markdown.Size == want.Files[0].Size {
		t.Fatal("repaired content or refreshed manifest invalid")
	}
	if string(files[page.Files[1].Path]) != "attachment bytes" || !manifest.ExportedAt.After(original.ExportedAt) {
		t.Fatal("attachment or export timestamp invalid")
	}
	backup, err := os.ReadFile(filepath.Join(root, want.Files[0].Path+".bak"))
	if err != nil || domain.ConfluenceFileHash(backup) != want.Files[0].SHA256 {
		t.Fatalf("original backup missing: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(root, domain.ConfluenceManifestName))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("original manifest changed")
	}
	if _, err := repackLibrary(root, output); err == nil {
		t.Fatal("existing ZIP overwritten")
	}
	afterZIP, err := os.ReadFile(output)
	if err != nil || !bytes.Equal(data, afterZIP) {
		t.Fatal("existing ZIP changed")
	}
}

func TestRepackFailuresDoNotPublishZIP(t *testing.T) {
	for _, failure := range []string{"missing-manifest", "invalid-manifest", "missing-file", "symlink", "parent-symlink", "identity"} {
		t.Run(failure, func(t *testing.T) {
			root, manifest := localLibraryFixture(t)
			manifestPath := filepath.Join(root, domain.ConfluenceManifestName)
			filename := filepath.Join(root, manifest.Spaces[0].Pages[0].Files[0].Path)
			var err error
			switch failure {
			case "missing-manifest":
				err = os.Remove(manifestPath)
			case "invalid-manifest":
				err = os.WriteFile(manifestPath, []byte("{}"), 0600)
			case "missing-file":
				err = os.Remove(filename)
			case "symlink":
				if err := os.Remove(filename); err != nil {
					t.Fatal(err)
				}
				err = os.Symlink(manifestPath, filename)
			case "parent-symlink":
				parent := filepath.Dir(filename)
				if err := os.Rename(parent, parent+"-original"); err != nil {
					t.Fatal(err)
				}
				err = os.Symlink(parent+"-original", parent)
			case "identity":
				err = os.WriteFile(filename, []byte("---\nconfluence_page_id: \"999\"\nconfluence_version: 2\n---\n"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(t.TempDir(), "invalid.zip")
			if _, err := repackLibrary(root, output); err == nil {
				t.Fatal("invalid library accepted")
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatalf("failed ZIP published: %v", err)
			}
			entries, err := os.ReadDir(filepath.Dir(output))
			if err != nil || len(entries) != 0 {
				t.Fatalf("temporary archive leaked: %v", err)
			}
		})
	}
}

func TestRepairAndPackExistingOutputDoesNotRepairFiles(t *testing.T) {
	root, manifest := localLibraryFixture(t)
	filename := filepath.Join(root, manifest.Spaces[0].Pages[0].Files[0].Path)
	before, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "existing.zip")
	if err := os.WriteFile(output, []byte("existing archive"), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("python3", "repair_and_pack.py", root, output)
	if result, err := command.CombinedOutput(); err == nil || !strings.Contains(string(result), "输出已存在") {
		t.Fatalf("expected output conflict: %v: %s", err, result)
	}
	after, err := os.ReadFile(filename)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("source repaired despite output conflict")
	}
	if _, err := os.Lstat(filename + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("unexpected backup: %v", err)
	}
}
