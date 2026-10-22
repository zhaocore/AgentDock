package rustfs

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 官方 S3 SDK 真实签名与路径式读写，拒绝无凭据的公开读取。
func TestPrivateObjects(t *testing.T) {
	var stored string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=access/") {
			t.Error("missing signature")
		}
		if r.URL.Path != "/private/confluence/user/task/page.md" {
			t.Error(r.URL.Path)
		}
		if r.Method == "PUT" {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			stored = string(body)
			w.WriteHeader(200)
		} else {
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(stored))
		}
	}))
	defer server.Close()
	client := New(server.URL, "us-east-1", "private", "access", "secret")
	if err := client.Put(context.Background(), "confluence/user/task/page.md", []byte("# Hello"), "text/plain"); err != nil {
		t.Fatal(err)
	}
	data, err := client.Get(context.Background(), "confluence/user/task/page.md")
	if err != nil || string(data) != "# Hello" {
		t.Fatalf("%q %v", data, err)
	}
}

// 缺桶只允许在明确的 NoSuchBucket 后创建，其他错误必须原样传播。
func TestPutCreatesMissingBucket(t *testing.T) {
	type response struct {
		path   string
		status int
		code   string
	}
	objectPath := "/private/confluence/user/task/tree.json"
	for _, tc := range []struct {
		name, region, wantError string
		steps                   []response
	}{
		{name: "missing bucket", region: "us-east-1", steps: []response{{objectPath, 404, "NoSuchBucket"}, {"/private", 200, ""}, {objectPath, 200, ""}}},
		{name: "regional bucket", region: "eu-west-1", steps: []response{{objectPath, 404, "NoSuchBucket"}, {"/private", 200, ""}, {objectPath, 200, ""}}},
		{name: "concurrent creation", region: "us-east-1", steps: []response{{objectPath, 404, "NoSuchBucket"}, {"/private", 409, "BucketAlreadyOwnedByYou"}, {objectPath, 200, ""}}},
		{name: "object access denied", region: "us-east-1", wantError: "AccessDenied", steps: []response{{objectPath, 403, "AccessDenied"}}},
		{name: "missing object", region: "us-east-1", wantError: "NoSuchKey", steps: []response{{objectPath, 404, "NoSuchKey"}}},
		{name: "unclassified not found", region: "us-east-1", wantError: "NotFound", steps: []response{{objectPath, 404, "NotFound"}}},
		{name: "create denied", region: "us-east-1", wantError: "RustFS create bucket=private", steps: []response{{objectPath, 404, "NoSuchBucket"}, {"/private", 403, "AccessDenied"}}},
		{name: "bucket belongs to another account", region: "us-east-1", wantError: "BucketAlreadyExists", steps: []response{{objectPath, 404, "NoSuchBucket"}, {"/private", 409, "BucketAlreadyExists"}}},
		{name: "retry still fails", region: "us-east-1", wantError: "NoSuchBucket", steps: []response{{objectPath, 404, "NoSuchBucket"}, {"/private", 200, ""}, {objectPath, 404, "NoSuchBucket"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				index := calls
				calls++
				if index >= len(tc.steps) {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(400)
					return
				}
				step := tc.steps[index]
				if r.Method != "PUT" || r.URL.Path != step.path {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=access/") {
					t.Error("missing signature")
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				if r.URL.Path == objectPath {
					if string(body) != `{"nodes":[]}` || r.Header.Get("Content-Type") != "application/json" {
						t.Errorf("object body/type changed: %q", body)
					}
				} else {
					if r.Header.Get("X-Amz-Acl") != "" {
						t.Error("unexpected ACL change")
					}
					if tc.region != "us-east-1" && !strings.Contains(string(body), "<LocationConstraint>"+tc.region+"</LocationConstraint>") {
						t.Errorf("missing bucket region: %s", body)
					}
				}
				if step.code != "" {
					w.Header().Set("Content-Type", "application/xml")
				}
				w.WriteHeader(step.status)
				if step.code != "" {
					io.WriteString(w, "<Error><Code>"+step.code+"</Code><Message>test failure</Message></Error>")
				}
			}))
			defer server.Close()
			client := New(server.URL, tc.region, "private", "access", "secret")
			err := client.Put(context.Background(), "confluence/user/task/tree.json", []byte(`{"nodes":[]}`), "application/json")
			if tc.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("want %q got %v", tc.wantError, err)
			}
			if calls != len(tc.steps) {
				t.Fatalf("requests=%d want=%d", calls, len(tc.steps))
			}
		})
	}
}

// 列举只接受目标前缀，完整处理分页，不暴露其他任务对象。
func TestListPrivatePrefix(t *testing.T) {
	for _, mode := range []string{"pages", "outside", "denied"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			prefix := "confluence/user/task/1/"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/private" || r.URL.Query().Get("prefix") != prefix {
					t.Error("invalid list request")
				}
				w.Header().Set("Content-Type", "application/xml")
				if mode == "denied" {
					w.WriteHeader(403)
					io.WriteString(w, "<Error><Code>AccessDenied</Code></Error>")
					return
				}
				key := prefix + "page.md"
				truncated := "false"
				token := ""
				if mode == "outside" {
					key = "another-user/private.md"
				} else if calls == 1 {
					truncated = "true"
					token = "<NextContinuationToken>next</NextContinuationToken>"
				} else {
					if r.URL.Query().Get("continuation-token") != "next" {
						t.Error("missing pagination token")
					}
					key = prefix + "assets/asset-0123456789abcdef01234567.png"
				}
				io.WriteString(w, "<ListBucketResult><IsTruncated>"+truncated+"</IsTruncated>"+token+"<Contents><Key>"+key+"</Key></Contents></ListBucketResult>")
			}))
			defer server.Close()
			client := New(server.URL, "us-east-1", "private", "access", "secret")
			keys, err := client.List(context.Background(), prefix)
			if mode == "pages" {
				if err != nil || len(keys) != 2 || calls != 2 {
					t.Fatal(keys, err, calls)
				}
			} else if err == nil {
				t.Fatal("unsafe list accepted")
			}
		})
	}
}
