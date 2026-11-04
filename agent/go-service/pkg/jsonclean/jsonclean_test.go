package jsonclean

import (
	"encoding/json"
	"testing"
)

func TestCleanParsesJSONCWithoutTouchingStrings(t *testing.T) {
	raw := []byte("\ufeff{\n" +
		"  \"url\": \"https://example.test/a//b\", // keep string contents\n" +
		"  /* block comment */\n" +
		"  \"items\": [1, 2,],\n" +
		"}\n")

	var got struct {
		URL   string `json:"url"`
		Items []int  `json:"items"`
	}
	if err := json.Unmarshal(Clean(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got.URL != "https://example.test/a//b" {
		t.Fatalf("url = %q", got.URL)
	}
	if len(got.Items) != 2 || got.Items[0] != 1 || got.Items[1] != 2 {
		t.Fatalf("items = %v", got.Items)
	}
}

func TestCleanPreservesUnterminatedBlockCommentAsInvalidJSON(t *testing.T) {
	if err := json.Unmarshal(Clean([]byte("{\"value\": 1 /*")), &map[string]int{}); err == nil {
		t.Fatal("unterminated block comment must remain invalid")
	}
}
