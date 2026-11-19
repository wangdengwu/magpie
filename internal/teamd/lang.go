package teamd

import (
	"errors"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"strconv"
	"strings"
)

// The portal's two languages, chosen from the request's Accept-Language.
//
// Every page is drawn on the server from a template and a word list, never
// in a script: a sentence is one entry with named arguments, so a language
// can put its words and its quantities in its own order. English is the
// original — the templates' own text — and a language with no entry for a
// sentence says the English one rather than a key or a blank.
//
// There is no cookie and no switcher: the same link reads Chinese in a
// Chinese browser and English in another.

// lang is a language the portal speaks.
type lang string

const (
	langEN lang = "en"
	langZH lang = "zh"
)

// languages are the ones this build has words for, English first.
var languages = []lang{langEN, langZH}

// zh-TW is Traditional Chinese, and this build has no words for it: it
// answers Simplified, which is a reading of the language a Traditional
// reader can follow, where English is not. Writing Traditional is a second
// word list (words.go), not a change here.

// langOf is the language a request asks for, from its Accept-Language.
//
// The header is a list of tags with weights: `zh-CN,zh;q=0.9,en;q=0.8`. The
// highest-weighted tag this build speaks decides, not the first one
// written; a tag with no weight counts as 1, and a more specific tag beats
// a less specific one at the same weight (`zh-CN` over `zh`). A header with
// nothing this build speaks — `*`, `fr`, absent — is English, which is also
// what a client sending no header at all gets.
func langOf(header string) lang {
	best, bestQ, bestLen := langEN, -1.0, -1
	for _, part := range strings.Split(header, ",") {
		tag, q, ok := parseLanguageRange(part)
		if !ok {
			continue
		}
		l, speaks := speak(tag)
		if !speaks {
			continue
		}
		if weight, tagLen := q, len(tag); weight > bestQ || weight == bestQ && tagLen > bestLen {
			best, bestQ, bestLen = l, weight, tagLen
		}
	}
	return best
}

// parseLanguageRange reads one comma-separated part of Accept-Language: its
// tag, in lower case, and its weight. The weight is 1 when the part gives
// none; a part whose weight can't be read is dropped rather than guessed at.
func parseLanguageRange(part string) (tag string, q float64, ok bool) {
	tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
	tag = strings.ToLower(strings.TrimSpace(tag))
	if tag == "" {
		return "", 0, false
	}
	q = 1
	for _, p := range strings.Split(params, ";") {
		k, v, has := strings.Cut(strings.TrimSpace(p), "=")
		if !has || !strings.EqualFold(strings.TrimSpace(k), "q") {
			continue
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil || f < 0 || f > 1 {
			return "", 0, false
		}
		q = f
	}
	return tag, q, true
}

// speak is the language a tag names, and whether this build has words for
// it. The primary subtag decides: `zh`, `zh-CN` and `zh-Hans` are all
// Chinese, though this build writes Simplified.
func speak(tag string) (lang, bool) {
	if tag == "*" {
		return langEN, true
	}
	primary, _, _ := strings.Cut(tag, "-")
	for _, l := range languages {
		if primary == string(l) {
			return l, true
		}
	}
	return langEN, false
}

// words is a language's sentences, by key. A key says where a sentence
// sits, not what it says: English's list is the original's, and a language
// that leaves one out falls back to it.
type words map[string]string

// trans is a language's sentences and the ones it falls back to.
type trans struct {
	lang lang
	own  words // the language's own
	en   words // English's, which every lookup ends at
}

// langOf is the language, for the page's view.
func (t trans) Lang() lang { return t.lang }

// HTML is the language's tag for the page's <html lang>, which is what a
// browser reads a page's language for from: its spell check, its
// hyphenation, and a screen reader's voice.
func (t trans) HTML() string { return string(t.lang) }

// T is the sentence at key, with its arguments put where its placeholders
// are: `{{.On}} of {{.Total}} {{.Accounts}} in use`.
//
// The sentence's own text is the portal's and goes out as written; each
// argument is escaped here, since it is a name, an id or a number that
// magpie gave and never markup. That is what the templates do for their
// own values, so a name holding `<script>` reads as text in either
// language.
//
// A key the language has no sentence for says English's, and one neither
// has says the key itself: a page with a missing entry is readable, never a
// blank or a word like "accounts.in_use".
func (t trans) T(key string, args ...any) string {
	s, ok := t.own[key]
	if !ok {
		s, ok = t.en[key]
	}
	if !ok {
		return key
	}
	return fill(s, args)
}

// fill substitutes a sentence's placeholders with its arguments, escaping
// each one.
//
// A placeholder is `{{.Name}}`, as html/template writes one, and it takes
// the argument of that name from an argsOf; a sentence written with
// positional arguments takes them in order. A placeholder with neither is
// left as it is — visible when a page is read, which is where a wrong
// argument should show rather than silently blank.
func fill(s string, args []any) string {
	named, positional := argsOf{}, args
	if len(args) > 0 {
		if m, ok := args[0].(argsOf); ok {
			named, positional = m, args[1:]
		}
	}
	var b strings.Builder
	for i := 0; ; {
		j := strings.Index(s[i:], "{{.")
		if j < 0 {
			b.WriteString(s[i:])
			return b.String()
		}
		b.WriteString(s[i : i+j])
		k := strings.Index(s[i+j:], "}}")
		if k < 0 {
			b.WriteString(s[i+j:])
			return b.String()
		}
		name := strings.TrimSpace(s[i+j+3 : i+j+k])
		if v, ok := valueOf(named, positional, name); ok {
			b.WriteString(html.EscapeString(v))
		} else {
			b.WriteString(s[i+j : i+j+k+2])
		}
		i += j + k + 2
	}
}

// argsOf is a sentence's arguments by name, which is how a template passes
// them: one map, so a sentence can name them rather than count them.
type argsOf map[string]any

// valueOf is one placeholder's argument.
func valueOf(named argsOf, positional []any, name string) (string, bool) {
	if v, ok := named[name]; ok {
		return text(v), true
	}
	if i, err := strconv.Atoi(name); err == nil && i >= 0 && i < len(positional) {
		return text(positional[i]), true
	}
	return "", false
}

// text is one argument as a sentence needs it.
func text(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}

// pageErr is an error whose text is the page's, not magpie's: a word key and
// its arguments, said in the request's language where it is shown.
//
// It is how a failure the portal decides on — a form with no address, a
// sign-in that finished without an account — reaches a page translated,
// while a failure magpie answered stays in magpie's own words.
type pageErr struct {
	key  string
	args argsOf
	// why is magpie's own text where the sentence has a place for it.
	why string
}

func (e *pageErr) Error() string { return e.key }

// pageErrs are the ones the add flow and its sign-in page answer with.
var (
	errNoSignInID  = &pageErr{key: "signin.err.no_id"}
	errPaste       = &pageErr{key: "signin.err.paste"}
	errNoUser      = &pageErr{key: "signin.err.no_user"}
	errNoGroupName = &pageErr{key: "signin.err.name"}
)

// said is err in the request's language: the portal's own sentence where it
// made one, and its own text where the words are magpie's.
func said(t trans, err error) string {
	var e *pageErr
	if errors.As(err, &e) {
		args := e.args
		if e.key == "signin.err.save" {
			args = argsOf{"Why": e.why}
		}
		return t.T(e.key, args)
	}
	return err.Error()
}

// transOf is the language a request's page is drawn in.
func transOf(r *http.Request) trans { return transFor(langOf(r.Header.Get("Accept-Language"))) }

// funcs are what a template says a sentence with: `T` for one whose
// arguments it names, and `args` to name them.
//
//	{{T "accounts.empty.why" (args "Name" .Group.Name)}}
//
// A sentence is text, and so is each argument: the whole is marked safe for
// html/template only here, which is the one place that knows a sentence's
// text is the portal's own and its arguments were escaped by fill.
func (t trans) funcs() template.FuncMap {
	safe := func(key string, args ...any) template.HTML { return template.HTML(t.T(key, args...)) }
	return template.FuncMap{
		"T": safe,
		// Tn is T for a sentence with a count in it: English says "1
		// account" and "3 accounts" as two sentences, and every language
		// writes the `key.one` entry beside `key` so both are its own.
		"Tn": func(key string, n int, args ...any) template.HTML {
			if n == 1 {
				return safe(key+".one", args...)
			}
			return safe(key, args...)
		},
		"args": func(kv ...any) argsOf {
			m := argsOf{}
			for i := 0; i+1 < len(kv); i += 2 {
				if k, ok := kv[i].(string); ok {
					m[k] = kv[i+1]
				}
			}
			return m
		},
	}
}
