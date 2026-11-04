package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/zhaocore/agentdock/internal/domain"
)

func TestCrawlSourceMarkdown(t *testing.T) {
	for _, mode := range []string{"rss", "escaped-rss", "legacy-html", "query-error", "scan-error", "empty", "too-many", "bad-position", "object-error", "bad-json", "oversized-object", "bad-format", "missing", "oversized-page", "oversized-document"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			page := domain.CrawlPage{URL: "https://feed.example/rss.xml", Title: "RSS", CapturedAt: time.Now(), Markdown: strings.Repeat("完整正文", 7000), Format: "rss"}
			count := 1
			switch mode {
			case "escaped-rss":
				page.Markdown = strings.Repeat("<>&", 70000)
			case "legacy-html":
				page.Format = ""
				page.Markdown = "HTML fact"
			case "empty":
				count = 0
			case "too-many":
				count = 101
			case "bad-format":
				page.Format = "executable"
			case "missing":
				page.Markdown = ""
			case "oversized-page":
				page.Markdown = strings.Repeat("文", 240001)
			case "oversized-document":
				count = 70
				page.Markdown = strings.Repeat("x", 240000)
			}
			rows := sqlmock.NewRows([]string{"position"})
			objects := &crawlMarkdownObjects{data: map[string][]byte{}}
			if mode == "object-error" {
				objects.err = errors.New("storage unavailable")
			}
			for index := 0; index < count; index++ {
				position := index
				if mode == "bad-position" {
					position = -1
				}
				if mode == "scan-error" {
					rows.AddRow("invalid")
				} else {
					rows.AddRow(position)
				}
				data, _ := json.Marshal(page)
				if mode == "escaped-rss" && len(data) <= 1024*1024 {
					t.Fatal("test must cover JSON escaping beyond browser protocol limit")
				}
				if mode == "bad-json" {
					data = []byte("{")
				}
				if mode == "oversized-object" {
					data = []byte(strings.Repeat("x", 6*1024*1024+1))
				}
				objects.data[fmt.Sprintf("crawl/owner/run/pages/%d.json", index)] = data
			}
			query := mock.ExpectQuery("SELECT p.position FROM crawl_pages.*r.user_id").WithArgs("run", "owner")
			if mode == "query-error" {
				query.WillReturnError(errors.New("database unavailable"))
			} else {
				query.WillReturnRows(rows)
			}
			w := &CrawlWorker{DB: db, Objects: objects}
			document, err := w.SourceMarkdown(context.Background(), domain.CrawlRun{ID: "run", UserID: "owner"})
			if mode == "rss" || mode == "escaped-rss" || mode == "legacy-html" {
				if err != nil || !strings.Contains(document, page.Markdown) || !strings.Contains(document, page.URL) {
					t.Fatal(len(document), err)
				}
			} else if err == nil {
				t.Fatal("invalid source accepted", mode)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type crawlMarkdownObjects struct {
	data map[string][]byte
	err  error
}

func (o *crawlMarkdownObjects) Put(context.Context, string, []byte, string) error { return o.err }
func (o *crawlMarkdownObjects) Get(_ context.Context, key string) ([]byte, error) {
	return o.data[key], o.err
}

func TestCrawlDocumentLimits(t *testing.T) {
	page := domain.CrawlPage{URL: "https://feed.example/", Title: "Feed", Markdown: strings.Repeat("文", 24001), CapturedAt: time.Now(), Format: "rss"}
	if err := validateCrawlPage(page); err != nil {
		t.Fatal(err)
	}
	page.Format = "html"
	if err := validateCrawlPage(page); err == nil {
		t.Fatal("HTML limit expanded")
	}
}
