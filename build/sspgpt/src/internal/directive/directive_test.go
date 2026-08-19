package directive

import "testing"

func testRegistry() Registry {
	return Registry{Enabled: true, Directives: map[string]Rule{
		"readmanual":   {Kind: "document_query", Match: "prefix", Aliases: []string{"\\readmanual"}, FallbackAliases: []string{"/readmanual"}, Document: "manual"},
		"ukagaka_en_i": {Kind: "semantic_alias", Match: "exact", Aliases: []string{"えんいー", "\\えんいー", "\\e"}, Meaning: "closing"},
	}}
}

func TestMatchDocumentDirectiveCanonicalAndFallback(t *testing.T) {
	m, ok := testRegistry().MatchText("\\readmanual： 這個功能怎麼用？")
	if !ok || m.ID != "readmanual" || m.Argument != "這個功能怎麼用？" || m.InvokedAs != "\\readmanual" {
		t.Fatalf("unexpected canonical match: %#v ok=%t", m, ok)
	}
	legacy, ok := testRegistry().MatchText("/readmanual 這個功能怎麼用？")
	if !ok || legacy.ID != "readmanual" || legacy.Argument != "這個功能怎麼用？" || legacy.InvokedAs != "/readmanual" {
		t.Fatalf("legacy typo fallback must disambiguate to readmanual: %#v ok=%t", legacy, ok)
	}
	for _, input := range []struct {
		text string
		arg  string
	}{
		{"\\readmanual這個功能怎麼用？", "這個功能怎麼用？"},
		{"\\readmanual問題", "問題"},
		{"/readmanual防呆", "防呆"},
		{"\\readmanual？", "？"},
	} {
		m, ok := testRegistry().MatchText(input.text)
		if !ok || m.ID != "readmanual" || m.Argument != input.arg {
			t.Fatalf("natural CJK-adjacent directive did not match: input=%q match=%#v ok=%t", input.text, m, ok)
		}
	}
	for _, bad := range []string{
		"\\readmanualfoo nope", "/readmanualfoo nope",
		"\\readmanual123 nope", "\\readmanual_test nope", "\\readmanual-next nope",
	} {
		if _, ok := testRegistry().MatchText(bad); ok {
			t.Fatalf("prefix directive matched an ASCII command-identifier continuation: %q", bad)
		}
	}
}

func TestUnifiedUkagakaClosingAliases(t *testing.T) {
	for _, input := range []string{"えんいー", "\\えんいー", "\\e"} {
		m, ok := testRegistry().MatchText(input)
		if !ok || m.ID != "ukagaka_en_i" || m.Argument != "" {
			t.Fatalf("unexpected unified cultural alias match for %q: %#v ok=%t", input, m, ok)
		}
	}
	if _, ok := testRegistry().MatchText("/えんいー"); ok {
		t.Fatal("slash-en-i is invalid syntax and must not be registered")
	}
	if _, ok := testRegistry().MatchText("えんいーって何？"); ok {
		t.Fatal("exact semantic alias must not hijack ordinary prose")
	}
}

func TestSafeDocumentRelativePath(t *testing.T) {
	if got, err := SafeDocumentRelativePath("character/manual.md"); err != nil || got != "character/manual.md" {
		t.Fatalf("valid document rejected: got=%q err=%v", got, err)
	}
	for _, bad := range []string{"../secret.txt", "profile/secrets.txt", "/character/manual.md", "character/manual.json"} {
		if _, err := SafeDocumentRelativePath(bad); err == nil {
			t.Fatalf("unsafe document path accepted: %q", bad)
		}
	}
}
