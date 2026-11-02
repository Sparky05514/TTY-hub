package daemon

import "testing"

func TestSplitSend(t *testing.T) {
	known := map[string]int{
		"Ada":          1,
		"Ada Lovelace": 1,
		"Sam":          2,
	}
	matches := func(q string) (int, error) {
		return known[q], nil
	}
	query, text, err := splitSend([]string{"Ada", "Lovelace", "on", "my", "way"}, matches)
	if err != nil {
		t.Fatal(err)
	}
	if query != "Ada Lovelace" || text != "on my way" {
		t.Fatalf("query=%q text=%q", query, text)
	}
	query, text, err = splitSend([]string{"Ada", "hello"}, matches)
	if err != nil {
		t.Fatal(err)
	}
	if query != "Ada" || text != "hello" {
		t.Fatalf("query=%q text=%q", query, text)
	}
	if _, _, err := splitSend([]string{"Sam", "hello"}, matches); err == nil {
		t.Fatal("expected an ambiguous name to fail")
	}
	if _, _, err := splitSend([]string{"missing", "hello"}, matches); err == nil {
		t.Fatal("expected a missing chat to fail")
	}
	if _, _, err := splitSend([]string{"Ada"}, matches); err == nil {
		t.Fatal("expected a missing message to fail")
	}
}
