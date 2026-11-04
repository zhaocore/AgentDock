package config

import (
	"github.com/zhaocore/agentdock/internal/domain"
	"testing"
)

func TestCrawlTaskProfiles(t *testing.T) {
	profiles := []CrawlProfile{{ID: "a", Origin: "https://a.example", UserIDs: []string{"owner"}}, {ID: "b", Origin: "https://b.example", UserIDs: []string{"owner"}}, {ID: "a2", Origin: "https://a.example", UserIDs: []string{"owner"}}, {ID: "private", Origin: "https://private.example", UserIDs: []string{"other"}}}
	for _, mode := range []string{"anonymous", "multiple", "legacy", "legacy-repeated", "denied", "missing", "same-origin", "duplicate-id", "bad-hops", "negative-hops", "bad-regex", "no-seed", "bad-seed", "too-many-profiles"} {
		t.Run(mode, func(t *testing.T) {
			task := domain.CrawlTask{UserID: "owner", URLs: []string{"https://public.example/a"}, Hops: 1}
			want := 0
			invalid := false
			switch mode {
			case "multiple":
				task.ProfileIDs = []string{"a", "b"}
				want = 2
			case "legacy":
				task.ProfileID = "a"
				want = 1
			case "legacy-repeated":
				task.ProfileID = "a"
				task.ProfileIDs = []string{"a"}
				want = 1
			case "denied":
				task.ProfileIDs = []string{"private"}
				invalid = true
			case "missing":
				task.ProfileIDs = []string{"missing"}
				invalid = true
			case "same-origin":
				task.ProfileIDs = []string{"a", "a2"}
				invalid = true
			case "duplicate-id":
				task.ProfileIDs = []string{"a", "a"}
				invalid = true
			case "bad-hops":
				task.Hops = 6
				invalid = true
			case "negative-hops":
				task.Hops = -1
				invalid = true
			case "bad-regex":
				task.IncludePattern = "["
				invalid = true
			case "no-seed":
				task.URLs = nil
				invalid = true
			case "bad-seed":
				task.URLs = []string{"http://bad.example"}
				invalid = true
			case "too-many-profiles":
				task.ProfileIDs = make([]string, 101)
				invalid = true
			}
			got, err := CrawlTaskProfiles(profiles, task)
			if (err != nil) != invalid || (!invalid && len(got) != want) {
				t.Fatal(got, err)
			}
		})
	}
}
