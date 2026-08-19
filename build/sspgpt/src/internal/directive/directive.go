package directive

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// Registry is the single editable authority for cognition directives. Runtime
// uses it only to recognize user intent; Bridge uses the same rule IDs to
// resolve cognition semantics/documents. No directive may directly execute OS
// commands or accept an arbitrary user-supplied filesystem path.
type Registry struct {
	FormatVersion int                     `json:"format_version"`
	Enabled       bool                    `json:"enabled"`
	Documents     map[string]DocumentSpec `json:"documents,omitempty"`
	Directives    map[string]Rule         `json:"directives,omitempty"`
}

type DocumentSpec struct {
	Label            string `json:"label,omitempty"`
	Path             string `json:"path"`
	MaxContextTokens int    `json:"max_context_tokens,omitempty"`
}

type Rule struct {
	Kind            string   `json:"kind"`            // document_query|semantic_alias
	Match           string   `json:"match,omitempty"` // prefix|exact
	Aliases         []string `json:"aliases"`
	FallbackAliases []string `json:"fallback_aliases,omitempty"`
	Document        string   `json:"document,omitempty"`
	Culture         string   `json:"culture,omitempty"`
	Meaning         string   `json:"meaning,omitempty"`
	Instruction     string   `json:"instruction,omitempty"`
}

type Match struct {
	ID        string
	Rule      Rule
	InvokedAs string
	Argument  string
}

func Parse(b []byte) (Registry, error) {
	var r Registry
	if err := json.Unmarshal(b, &r); err != nil {
		return Registry{}, err
	}
	if r.Documents == nil {
		r.Documents = map[string]DocumentSpec{}
	}
	if r.Directives == nil {
		r.Directives = map[string]Rule{}
	}
	return r, nil
}

func Load(path string) (Registry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Registry{}, err
	}
	return Parse(b)
}

// MatchText recognizes only explicitly registered directives. Canonical aliases
// and optional compatibility/fallback aliases share the same semantic rule.
// Unknown command-like text remains ordinary chat. Prefix rules must occur at
// the beginning of the input. For prefix rules, only ASCII command-identifier
// continuation characters block a match. This keeps \readmanualfoo,
// \readmanual123 and \readmanual_test distinct while allowing natural CJK
// adjacency such as \readmanual問題 without requiring a space. Exact rules are
// useful for cultural expressions such as えんいー / \えんいー / \e.
func (r Registry) MatchText(text string) (Match, bool) {
	if !r.Enabled {
		return Match{}, false
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return Match{}, false
	}
	type candidate struct {
		id    string
		rule  Rule
		alias string
		mode  string
	}
	cs := []candidate{}
	for id, rule := range r.Directives {
		kind := strings.ToLower(strings.TrimSpace(rule.Kind))
		if kind != "document_query" && kind != "semantic_alias" {
			continue
		}
		mode := strings.ToLower(strings.TrimSpace(rule.Match))
		if mode == "" {
			if kind == "document_query" {
				mode = "prefix"
			} else {
				mode = "exact"
			}
		}
		if mode != "prefix" && mode != "exact" {
			continue
		}
		allAliases := make([]string, 0, len(rule.Aliases)+len(rule.FallbackAliases))
		allAliases = append(allAliases, rule.Aliases...)
		allAliases = append(allAliases, rule.FallbackAliases...)
		for _, alias := range allAliases {
			alias = strings.TrimSpace(alias)
			if alias == "" {
				continue
			}
			cs = append(cs, candidate{id: id, rule: rule, alias: alias, mode: mode})
		}
	}
	sort.SliceStable(cs, func(i, j int) bool {
		// Prefer exact matches and then longest aliases, so editable aliases
		// remain deterministic even when one is a prefix of another.
		if cs[i].mode != cs[j].mode {
			return cs[i].mode == "exact"
		}
		if len([]rune(cs[i].alias)) != len([]rune(cs[j].alias)) {
			return len([]rune(cs[i].alias)) > len([]rune(cs[j].alias))
		}
		if cs[i].id != cs[j].id {
			return cs[i].id < cs[j].id
		}
		return cs[i].alias < cs[j].alias
	})
	for _, c := range cs {
		switch c.mode {
		case "exact":
			if equalFold(text, c.alias) {
				return Match{ID: c.id, Rule: c.rule, InvokedAs: c.alias}, true
			}
		case "prefix":
			if arg, ok := prefixArgument(text, c.alias); ok {
				return Match{ID: c.id, Rule: c.rule, InvokedAs: c.alias, Argument: arg}, true
			}
		}
	}
	return Match{}, false
}

func equalFold(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func prefixArgument(text, alias string) (string, bool) {
	rt, ra := []rune(text), []rune(alias)
	if len(rt) < len(ra) || !strings.EqualFold(string(rt[:len(ra)]), string(ra)) {
		return "", false
	}
	if len(rt) == len(ra) {
		return "", true
	}
	next := rt[len(ra)]
	if isASCIICommandIdentifierRune(next) {
		return "", false
	}
	rest := rt[len(ra):]
	// Space and the two common command separators are syntax, not part of the
	// semantic question. Other punctuation/CJK text is preserved as argument.
	for len(rest) > 0 && unicode.IsSpace(rest[0]) {
		rest = rest[1:]
	}
	if len(rest) > 0 && (rest[0] == ':' || rest[0] == '：') {
		rest = rest[1:]
		for len(rest) > 0 && unicode.IsSpace(rest[0]) {
			rest = rest[1:]
		}
	}
	return strings.TrimSpace(string(rest)), true
}

func isASCIICommandIdentifierRune(r rune) bool {
	return r >= 'a' && r <= 'z' ||
		r >= 'A' && r <= 'Z' ||
		r >= '0' && r <= '9' ||
		r == '_' || r == '-'
}

func (r Registry) Rule(id string) (Rule, bool) {
	x, ok := r.Directives[id]
	return x, ok
}

func (r Registry) Document(id string) (DocumentSpec, bool) {
	x, ok := r.Documents[id]
	return x, ok
}

// SafeDocumentRelativePath constrains directive-readable documents to the
// character/ tree. The user command supplies only a registered document ID;
// it can never supply a filesystem path directly.
func SafeDocumentRelativePath(rel string) (string, error) {
	rel = strings.TrimSpace(strings.ReplaceAll(rel, "\\", "/"))
	if rel == "" || strings.HasPrefix(rel, "/") {
		return "", errors.New("empty or absolute directive document path")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, ":") {
		return "", errors.New("unsafe directive document path")
	}
	if !strings.HasPrefix(clean, "character/") {
		return "", errors.New("directive documents must live under character/")
	}
	ext := strings.ToLower(filepath.Ext(clean))
	if ext != ".md" && ext != ".txt" {
		return "", errors.New("directive document must be .md or .txt")
	}
	return clean, nil
}
