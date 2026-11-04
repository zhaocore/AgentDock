package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/rand"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/zhaocore/agentdock/internal/domain"
	"github.com/zhaocore/agentdock/internal/store/rustfs"
)

// 大于旧上限的 ZIP 直接写入丢弃流，不在测试中缓存压缩包。
func TestConfluenceLibraryExportAbove100MiB(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	objects := libraryObjects()
	asset := make([]byte, 52*1024*1024)
	if _, err := rand.New(rand.NewSource(42)).Read(asset); err != nil {
		t.Fatal(err)
	}
	objects.data["confluence/owner/task/1/assets/asset-0123456789abcdef01234567.bin"] = asset
	objects.data["confluence/owner/task/1/assets/asset-0123456789abcdef01234568.bin"] = asset
	expectLibraryCatalog(mock, "owner", librarySourceRows(), sqlmock.NewRows(libraryImportColumns))
	output := &confluenceZIPWriter{output: io.Discard}
	service := ConfluenceLibrary{DB: db, Objects: objects}
	if err := service.Export(context.Background(), "owner", "", "", output); err != nil {
		t.Fatal(err)
	}
	if output.written <= 100*1024*1024 {
		t.Fatalf("ZIP did not exceed old limit: %d", output.written)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 用计数验证 10 GiB 边界，不分配同等大小的内存。
func TestConfluenceZIPWriterLimit(t *testing.T) {
	output := &confluenceZIPWriter{output: io.Discard, written: domain.ConfluenceExportZIPLimit - 3}
	if n, err := output.Write([]byte("zip")); n != 3 || err != nil {
		t.Fatalf("boundary write: %d %v", n, err)
	}
	if n, err := output.Write([]byte("x")); n != 0 || err == nil || !strings.Contains(err.Error(), "10 GiB") {
		t.Fatalf("oversized write: %d %v", n, err)
	}
}

// 大包通过管道导出并分块导入，确认阶段也不能整包读取。
func TestConfluenceLibraryLargeImportAndApply(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	source := libraryObjects()
	asset := make([]byte, 52*1024*1024)
	if _, err := rand.New(rand.NewSource(43)).Read(asset); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"asset-0123456789abcdef01234567.bin", "asset-0123456789abcdef01234568.bin"} {
		source.data["confluence/owner/task/1/assets/"+name] = asset
	}
	target := &memoryObjects{data: map[string][]byte{}}
	exporter := ConfluenceLibrary{DB: db, Objects: source}
	importer := ConfluenceLibrary{DB: db, Objects: target}
	expectLibraryCatalog(mock, "owner", librarySourceRows(), sqlmock.NewRows(libraryImportColumns))
	expectLibraryCatalog(mock, "other", sqlmock.NewRows(taskColumns), sqlmock.NewRows(libraryImportColumns))
	mock.ExpectExec("INSERT INTO confluence_library_imports").WillReturnResult(sqlmock.NewResult(1, 1))
	reader, writer := io.Pipe()
	defer reader.Close()
	done := make(chan error, 1)
	go func() {
		err := exporter.Export(context.Background(), "owner", "", "", writer)
		writer.CloseWithError(err)
		done <- err
	}()
	report, err := importer.Preview(context.Background(), "other", reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	spaceID := domain.ConfluenceSpaceID("https://example.com", "TEAM")
	prefix := "confluence-library/other/" + report.ID + "/" + spaceID + "/1/"
	record := domain.ConfluenceImport{ID: report.ID, UserID: "other", Status: "preview", BaseDigest: domain.ConfluenceFileHash([]byte("[]")), Report: report, CreatedAt: time.Now(), Spaces: []domain.ConfluenceLibrarySpace{{ID: spaceID, BaseURL: "https://example.com", Key: "TEAM", Pages: []domain.ConfluenceLibraryPage{{ID: "1", ObjectPrefix: prefix}}}}}
	mock.ExpectQuery("SELECT GET_LOCK").WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(1))
	mock.ExpectQuery("SELECT .* FROM confluence_library_imports WHERE id").WillReturnRows(importRows(record))
	expectLibraryCatalog(mock, "other", sqlmock.NewRows(taskColumns), sqlmock.NewRows(libraryImportColumns))
	expectLibraryCatalog(mock, "other", sqlmock.NewRows(taskColumns), sqlmock.NewRows(libraryImportColumns))
	mock.ExpectExec("UPDATE confluence_library_imports SET status='applied'").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT RELEASE_LOCK").WillReturnRows(sqlmock.NewRows([]string{"released"}).AddRow(1))
	if _, err := importer.Apply(context.Background(), "other", report.ID); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(target.data[prefix+"assets/asset-0123456789abcdef01234567.bin"], asset) {
		t.Fatal("large attachment changed")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestConfluenceLibraryUsesSelectedPlan(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	objects := libraryObjects()
	var tree domain.ConfluenceTree
	if err := json.Unmarshal([]byte(libraryTreeFixture), &tree); err != nil {
		t.Fatal(err)
	}
	node, page := tree.Nodes[0], tree.Pages[0]
	node.ID, node.Title = "2", "Two"
	page.ID, page.Title, page.Path = "2", "Two", "Root/Two"
	tree.Nodes = append(tree.Nodes, node)
	tree.Pages = append(tree.Pages, page)
	data, err := json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	objects.data["confluence/owner/task/tree.json"] = data
	objects.data["confluence/owner/task/selection.json"] = []byte(`{"node_id":"2"}`)
	rows := sqlmock.NewRows(taskColumns).AddRow("task", "owner", "https://example.com/page", "completed", "markdown", 1, 1, "", time.Now(), time.Now())
	expectLibraryCatalog(mock, "owner", rows, sqlmock.NewRows(libraryImportColumns))
	service := ConfluenceLibrary{DB: db, Objects: objects}
	catalog, err := service.Catalog(context.Background(), "owner")
	if err != nil || len(catalog.Spaces) != 1 || len(catalog.Spaces[0].Pages) != 1 || catalog.Spaces[0].Pages[0].ID != "2" {
		t.Fatal(catalog, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func (m *memoryObjects) List(_ context.Context, prefix string) ([]string, error) {
	keys := []string{}
	for key := range m.data {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

var libraryImportColumns = []string{"id", "user_id", "status", "base_digest", "manifest", "report", "created_at"}

const libraryTreeFixture = `{"snapshot":{"source":{"base_url":"https://example.com","space_key":"TEAM"},"captured_at":"2026-10-22T00:00:00Z"},"nodes":[{"id":"1","title":"One","url":"https://example.com/pages/viewpage.action?pageId=1","version":1,"last_modified_at":"2026-10-21T00:00:00Z","children":[]}],"pages":[{"id":"1","title":"One","url":"https://example.com/pages/viewpage.action?pageId=1","version":1,"path":"Root/One"}]}`
const libraryMarkdown = "---\nconfluence_page_id: \"1\"\nconfluence_version: 1\n---\n\n# One\n"

func librarySourceRows() *sqlmock.Rows {
	return sqlmock.NewRows(taskColumns).AddRow("task", "owner", "https://example.com/page", "running", "export", 1, 2, "", time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC), time.Now())
}
func libraryObjects() *memoryObjects {
	return &memoryObjects{data: map[string][]byte{"confluence/owner/task/tree.json": []byte(libraryTreeFixture), "confluence/owner/task/1/page.md": []byte(libraryMarkdown)}}
}
func expectLibraryCatalog(mock sqlmock.Sqlmock, user string, tasks, imports *sqlmock.Rows) {
	mock.ExpectQuery("SELECT .* FROM confluence_tasks WHERE user_id=\\?").WithArgs(user).WillReturnRows(tasks)
	mock.ExpectQuery("SELECT .* FROM confluence_library_imports WHERE user_id=\\?").WithArgs(user).WillReturnRows(imports)
}
func importRows(record domain.ConfluenceImport) *sqlmock.Rows {
	manifest, _ := json.Marshal(record.Spaces)
	report, _ := json.Marshal(record.Report)
	return sqlmock.NewRows(libraryImportColumns).AddRow(record.ID, record.UserID, record.Status, record.BaseDigest, manifest, report, record.CreatedAt)
}

// 覆盖历史任务汇集、目录选择、ZIP 往返、用户隔离与整批确认后才可见。
func TestConfluenceLibraryZIPRoundTripAndApply(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	objects := libraryObjects()
	service := ConfluenceLibrary{DB: db, Objects: objects}
	ctx := context.Background()
	expectLibraryCatalog(mock, "owner", librarySourceRows(), sqlmock.NewRows(libraryImportColumns))
	var output bytes.Buffer
	err := service.Export(ctx, "owner", "", "Root", &output)
	if err == nil {
		t.Fatal("directory without Space accepted")
	}
	// 参数拒绝发生在读取数据库之前。
	err = service.Export(ctx, "owner", domain.ConfluenceSpaceID("https://example.com", "TEAM"), "Root", &output)
	if err != nil {
		t.Fatal(err)
	}
	zipData := output.Bytes()
	manifest, files, err := domain.ReadConfluenceArchive(zipData)
	if err != nil || len(manifest.Spaces) != 1 || len(files) != 1 {
		t.Fatal(manifest, err)
	}
	expectLibraryCatalog(mock, "other", sqlmock.NewRows(taskColumns), sqlmock.NewRows(libraryImportColumns))
	mock.ExpectExec("INSERT INTO confluence_library_imports").WithArgs(sqlmock.AnyArg(), "other", domain.ConfluenceFileHash([]byte("[]")), sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	report, err := service.Preview(ctx, "other", bytes.NewReader(zipData))
	if err != nil || report.Added != 1 {
		t.Fatal(report, err)
	}
	spaceID := manifest.Spaces[0].ID
	sourcePage := manifest.Spaces[0].Pages[0]
	importedPage := domain.ConfluenceLibraryPage{ID: sourcePage.ID, Title: sourcePage.Title, URL: sourcePage.URL, Version: sourcePage.Version, Path: sourcePage.Path, ModifiedAt: sourcePage.ModifiedAt, ObjectPrefix: "confluence-library/other/" + report.ID + "/" + spaceID + "/1/"}
	record := domain.ConfluenceImport{ID: report.ID, UserID: "other", Status: "preview", BaseDigest: domain.ConfluenceFileHash([]byte("[]")), Spaces: []domain.ConfluenceLibrarySpace{{ID: spaceID, BaseURL: "https://example.com", Key: "TEAM", Pages: []domain.ConfluenceLibraryPage{importedPage}}}, Report: report, CreatedAt: time.Now()}
	if _, ok := objects.data[importedPage.ObjectPrefix+"page.md"]; ok {
		t.Fatal("preview published files")
	}
	mock.ExpectQuery("SELECT GET_LOCK").WithArgs("confluence-library:other").WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(1))
	mock.ExpectQuery("SELECT .* FROM confluence_library_imports WHERE id=\\? AND user_id=\\?").WithArgs(report.ID, "other").WillReturnRows(importRows(record))
	expectLibraryCatalog(mock, "other", sqlmock.NewRows(taskColumns), sqlmock.NewRows(libraryImportColumns))
	expectLibraryCatalog(mock, "other", sqlmock.NewRows(taskColumns), sqlmock.NewRows(libraryImportColumns))
	mock.ExpectExec("UPDATE confluence_library_imports SET status='applied'").WithArgs(report.ID, "other").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT RELEASE_LOCK").WithArgs("confluence-library:other").WillReturnRows(sqlmock.NewRows([]string{"released"}).AddRow(1))
	applied, err := service.Apply(ctx, "other", report.ID)
	if err != nil || applied.Added != 1 {
		t.Fatal(applied, err)
	}
	if !bytes.Equal(objects.data[importedPage.ObjectPrefix+"page.md"], []byte(libraryMarkdown)) {
		t.Fatal("import changed Markdown")
	}
	record.Status = "applied"
	expectLibraryCatalog(mock, "other", sqlmock.NewRows(taskColumns), importRows(record))
	catalog, err := service.Catalog(ctx, "other")
	if err != nil || len(catalog.Spaces) != 1 {
		t.Fatal(catalog, err)
	}
	expectLibraryCatalog(mock, "other", sqlmock.NewRows(taskColumns), importRows(record))
	body, err := service.File(ctx, "other", spaceID, "1", "page.md")
	if err != nil || string(body) != libraryMarkdown {
		t.Fatal(err)
	}
	if _, err := FindConfluenceLibraryPage(catalog, spaceID, "999"); err == nil {
		t.Fatal("unknown page accepted")
	}
	if _, err := service.File(ctx, "other", spaceID, "1", "../secret"); err == nil {
		t.Fatal("unsafe file accepted")
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func buildLibraryZIP(t *testing.T, modified time.Time, content, pathValue, id string) []byte {
	t.Helper()
	spaceID := domain.ConfluenceSpaceID("https://example.com", "TEAM")
	filename, err := domain.ConfluenceArchivePath(spaceID, "TEAM", pathValue, "One", "page.md")
	if err != nil {
		t.Fatal(err)
	}
	manifest := domain.NewConfluenceManifest(time.Now())
	manifest.Spaces = []domain.ConfluenceArchiveSpace{{ID: spaceID, BaseURL: "https://example.com", Key: "TEAM", Pages: []domain.ConfluenceArchivePage{{ID: id, Title: "One", URL: "https://example.com/pages/viewpage.action?pageId=" + id, Version: 1, Path: pathValue, ModifiedAt: modified, Files: []domain.ConfluenceArchiveFile{{Name: "page.md", Path: filename, Size: int64(len(content)), SHA256: domain.ConfluenceFileHash([]byte(content))}}}}}}
	var buffer bytes.Buffer
	if err = domain.WriteConfluenceArchive(&buffer, manifest, map[string][]byte{filename: []byte(content)}); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestConfluenceLibraryMergePreview(t *testing.T) {
	for _, mode := range []string{"newer", "older", "same", "different-content", "different-path", "new-page", "directory-conflict"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			service := ConfluenceLibrary{DB: db, Objects: libraryObjects()}
			modified := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
			content := libraryMarkdown
			directory := "Root/One"
			id := "1"
			switch mode {
			case "newer":
				modified = modified.Add(time.Hour)
			case "older":
				modified = modified.Add(-time.Hour)
			case "different-content":
				content += "changed"
			case "different-path":
				directory = "Moved/One"
			case "new-page":
				id = "2"
				directory = "Root/Two"
				content = strings.ReplaceAll(content, "\"1\"", "\"2\"")
			case "directory-conflict":
				id = "2"
				content = strings.ReplaceAll(content, "\"1\"", "\"2\"")
			}
			zipData := buildLibraryZIP(t, modified, content, directory, id)
			expectLibraryCatalog(mock, "owner", librarySourceRows(), sqlmock.NewRows(libraryImportColumns))
			mock.ExpectExec("INSERT INTO confluence_library_imports").WillReturnResult(sqlmock.NewResult(1, 1))
			report, err := service.Preview(context.Background(), "owner", bytes.NewReader(zipData))
			if err != nil {
				t.Fatal(err)
			}
			want := "kept"
			switch mode {
			case "newer":
				want = "updated"
			case "new-page":
				want = "added"
			case "different-content", "different-path", "directory-conflict":
				want = "conflict"
			}
			if len(report.Items) != 1 || report.Items[0].Action != want {
				t.Fatalf("want %s got %+v", want, report)
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConfluenceLibraryCatalogNewestAndErrors(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	objects := libraryObjects()
	service := ConfluenceLibrary{DB: db, Objects: objects}
	rows := librarySourceRows().AddRow("new-task", "owner", "https://example.com/page", "completed", "export", 1, 1, "", time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC), time.Now())
	objects.data["confluence/owner/new-task/tree.json"] = []byte(strings.ReplaceAll(libraryTreeFixture, "2026-10-21T00:00:00Z", "2026-10-22T00:00:00Z"))
	expectLibraryCatalog(mock, "owner", rows, sqlmock.NewRows(libraryImportColumns))
	catalog, err := service.Catalog(context.Background(), "owner")
	if err != nil || catalog.Spaces[0].Pages[0].ObjectPrefix != "confluence/owner/new-task/1/" {
		t.Fatal(catalog, err)
	}
	for _, mode := range []string{"missing-space", "missing-time", "invalid-path", "bad-checkpoint"} {
		tree := libraryTreeFixture
		switch mode {
		case "missing-space":
			tree = strings.ReplaceAll(tree, `"space_key":"TEAM"`, `"space_key":""`)
		case "missing-time":
			tree = strings.ReplaceAll(tree, "2026-10-21T00:00:00Z", "")
		case "invalid-path":
			tree = strings.ReplaceAll(tree, "Root/One", "../One")
		}
		objects.data["confluence/owner/task/tree.json"] = []byte(tree)
		tasks := librarySourceRows()
		if mode == "bad-checkpoint" {
			tasks = sqlmock.NewRows(taskColumns).AddRow("task", "owner", "url", "running", "export", 3, 3, "", time.Now(), time.Now())
		}
		expectLibraryCatalog(mock, "owner", tasks, sqlmock.NewRows(libraryImportColumns))
		if _, err = service.Catalog(context.Background(), "owner"); err == nil {
			t.Fatal("invalid source accepted", mode)
		}
	}
	if _, err = service.Preview(context.Background(), "owner", strings.NewReader("bad zip")); !errors.Is(err, ErrInvalidArchive) {
		t.Fatal(err)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestConfluenceImportRejectsStalePreviewAndPartialPublish(t *testing.T) {
	for _, mode := range []string{"stale", "upload-failure", "already-applied", "unknown-user"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			objects := libraryObjects()
			service := ConfluenceLibrary{DB: db, Objects: objects}
			record := domain.ConfluenceImport{ID: "import", UserID: "owner", Status: "preview", BaseDigest: "outdated", Spaces: []domain.ConfluenceLibrarySpace{}, Report: domain.ConfluenceImportReport{}, CreatedAt: time.Now()}
			if mode == "already-applied" {
				record.Status = "applied"
			}
			if mode == "upload-failure" {
				record.BaseDigest = domain.ConfluenceFileHash([]byte("[]"))
				id := domain.ConfluenceSpaceID("https://example.com", "TEAM")
				record.Spaces = []domain.ConfluenceLibrarySpace{{ID: id, BaseURL: "https://example.com", Key: "TEAM", Pages: []domain.ConfluenceLibraryPage{{ID: "1", ObjectPrefix: "confluence-library/owner/import/1/"}}}}
				if err := rustfs.PutArchive(context.Background(), objects, "confluence-library/owner/import/archive.zip", bytes.NewReader(buildLibraryZIP(t, time.Now(), libraryMarkdown, "Root/One", "1")), domain.ConfluenceZIPLimit); err != nil {
					t.Fatal(err)
				}
				objects.fail = true
			}
			mock.ExpectQuery("SELECT GET_LOCK").WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(1))
			row := mock.ExpectQuery("SELECT .* FROM confluence_library_imports WHERE id").WithArgs("import", "owner")
			if mode == "unknown-user" {
				row.WillReturnRows(sqlmock.NewRows(libraryImportColumns))
			} else {
				row.WillReturnRows(importRows(record))
			}
			if mode == "stale" || mode == "upload-failure" {
				expectLibraryCatalog(mock, "owner", sqlmock.NewRows(taskColumns), sqlmock.NewRows(libraryImportColumns))
			}
			mock.ExpectQuery("SELECT RELEASE_LOCK").WillReturnRows(sqlmock.NewRows([]string{"released"}).AddRow(1))
			if _, err := service.Apply(context.Background(), "owner", "import"); err == nil {
				t.Fatal("invalid apply succeeded")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
