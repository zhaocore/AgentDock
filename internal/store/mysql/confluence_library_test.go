package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/zhaocore/dutybotgo/internal/domain"
)

func TestConfluenceLibraryStorage(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	ctx := context.Background()
	taskCols := []string{"id", "user_id", "url", "status", "stage", "completed", "total", "error", "created_at", "updated_at"}
	mock.ExpectQuery("SELECT .* FROM confluence_tasks WHERE user_id").WithArgs("owner").WillReturnRows(sqlmock.NewRows(taskCols).AddRow("task", "owner", "url", "failed", "export", 1, 2, "", time.Now(), time.Now()))
	sources, err := ListConfluenceSources(ctx, db, "owner")
	if err != nil || len(sources) != 1 {
		t.Fatal(sources, err)
	}
	report := domain.ConfluenceImportReport{ID: "import", Added: 1, Items: []domain.ConfluenceImportItem{}}
	record := domain.ConfluenceImport{ID: "import", UserID: "owner", Status: "preview", BaseDigest: "digest", Spaces: []domain.ConfluenceLibrarySpace{}, Report: report, CreatedAt: time.Now()}
	manifest, _ := json.Marshal(record.Spaces)
	reportData, _ := json.Marshal(report)
	mock.ExpectExec("INSERT INTO confluence_library_imports").WithArgs("import", "owner", "digest", manifest, reportData).WillReturnResult(sqlmock.NewResult(1, 1))
	if err = CreateConfluenceImport(ctx, db, record); err != nil {
		t.Fatal(err)
	}
	columns := []string{"id", "user_id", "status", "base_digest", "manifest", "report", "created_at"}
	rows := func() *sqlmock.Rows {
		return sqlmock.NewRows(columns).AddRow("import", "owner", "preview", "digest", manifest, reportData, record.CreatedAt)
	}
	mock.ExpectQuery("SELECT .* WHERE id=\\? AND user_id=\\?").WithArgs("import", "owner").WillReturnRows(rows())
	current, err := GetConfluenceImport(ctx, db, "import", "owner")
	if err != nil || current.Report.Added != 1 {
		t.Fatal(current, err)
	}
	mock.ExpectQuery("SELECT .* WHERE user_id=\\?").WithArgs("owner").WillReturnRows(rows())
	records, err := ListConfluenceImports(ctx, db, "owner")
	if err != nil || len(records) != 1 {
		t.Fatal(records, err)
	}
	mock.ExpectExec("UPDATE confluence_library_imports").WithArgs("import", "owner").WillReturnResult(sqlmock.NewResult(0, 1))
	if err = ApplyConfluenceImport(ctx, db, "import", "owner"); err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec("UPDATE confluence_library_imports").WillReturnResult(sqlmock.NewResult(0, 0))
	if err = ApplyConfluenceImport(ctx, db, "import", "other"); err == nil {
		t.Fatal("unscoped apply accepted")
	}
	mock.ExpectQuery("SELECT GET_LOCK").WithArgs("confluence-library:owner").WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(1))
	connection, err := LockConfluenceLibrary(ctx, db, "owner")
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT RELEASE_LOCK").WithArgs("confluence-library:owner").WillReturnRows(sqlmock.NewRows([]string{"released"}).AddRow(1))
	if err = UnlockConfluenceLibrary(ctx, connection, "owner"); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT GET_LOCK").WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(0))
	if _, err = LockConfluenceLibrary(ctx, db, "owner"); err == nil {
		t.Fatal("busy lock accepted")
	}
	mock.ExpectQuery("SELECT GET_LOCK").WillReturnError(errors.New("db down"))
	if _, err = LockConfluenceLibrary(ctx, db, "owner"); err == nil {
		t.Fatal("lock error ignored")
	}
	mock.ExpectQuery("SELECT .* WHERE user_id").WillReturnError(errors.New("db down"))
	if _, err = ListConfluenceSources(ctx, db, "owner"); err == nil {
		t.Fatal("query error ignored")
	}
	mock.ExpectQuery("SELECT .* WHERE user_id").WillReturnRows(sqlmock.NewRows(columns).AddRow("import", "owner", "preview", "digest", []byte("{"), reportData, time.Now()))
	if _, err = ListConfluenceImports(ctx, db, "owner"); err == nil {
		t.Fatal("invalid manifest ignored")
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
