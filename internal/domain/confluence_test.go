package domain

import (
	"reflect"
	"testing"
)

func TestSelectConfluencePages(t *testing.T) {
	tree := ConfluenceTree{Nodes: []ConfluenceNode{{ID: "10", Children: []ConfluenceNode{{ID: "1"}, {ID: "20", Children: []ConfluenceNode{{ID: "2"}}}}}, {ID: "3"}}, Pages: []ConfluencePage{{ID: "3", Path: "Other"}, {ID: "1", Path: "Root/One"}, {ID: "2", Path: "Root/Branch/Two"}}}
	for _, tc := range []struct {
		node string
		ids  []string
	}{{"", []string{"3", "1", "2"}}, {"10", []string{"1", "2"}}, {"20", []string{"2"}}, {"2", []string{"2"}}} {
		pages, err := SelectConfluencePages(tree, tc.node)
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, page := range pages {
			ids = append(ids, page.ID)
		}
		if !reflect.DeepEqual(ids, tc.ids) {
			t.Fatal(tc.node, ids)
		}
	}
	if tree.Pages[0].ID != "3" || tree.Pages[2].Path != "Root/Branch/Two" {
		t.Fatal("source tree mutated")
	}
	if _, err := SelectConfluencePages(tree, "999"); err == nil {
		t.Fatal("unknown node accepted")
	}
	if _, err := SelectConfluencePages(ConfluenceTree{Nodes: []ConfluenceNode{{ID: "10"}}}, "10"); err == nil {
		t.Fatal("empty subtree accepted")
	}
}
