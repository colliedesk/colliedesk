package helpcenter

import (
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// codeLanguages are the chroma lexers, by exact name, that colour a help center's code blocks. A
// short list and not all of chroma: lexers.Get falls back to matching an unknown name as a file name
// and then against every lexer (3 to 15 ms a block), and the lexers that look up another one by text
// from the code (Markdown, Svelte, Org and the SQL dialects with an embedded language) reach that
// fallback from inside a block. Each one here is still bound by highlightTime.
var codeLanguages = []string{
	"Bash", "Bash Session", "Batchfile", "C", "C#", "C++", "CSS", "Dart", "Diff", "Docker",
	"Elixir", "GraphQL", "Go", "Groovy", "HTML", "HTTP", "Haskell", "INI", "Java", "JavaScript",
	"JSON", "Kotlin", "Lua", "Makefile", "Nginx configuration file", "Objective-C", "PHP", "Perl",
	"PowerShell", "Protocol Buffer", "Python", "R", "Ruby", "Rust", "SCSS", "SQL", "Scala", "Swift",
	"TOML", "Terraform", "TypeScript", "XML", "YAML", "properties", "react", "vue",
}

// codeLexers maps the lowercase name, every alias and every plain file extension ("yml" of "*.yml")
// of codeLanguages to its lexer, so a block's language is found by a map lookup and never by
// chroma's fallback. A name or alias wins over another lexer's extension.
var codeLexers = sync.OnceValue(func() map[string]chroma.Lexer {
	byName := map[string]chroma.Lexer{}
	for _, name := range codeLanguages {
		lexer := lexers.Get(name)
		if lexer == nil || lexer.Config().Name != name {
			continue
		}
		byName[strings.ToLower(name)] = lexer
		for _, alias := range lexer.Config().Aliases {
			byName[strings.ToLower(alias)] = lexer
		}
	}
	for _, name := range codeLanguages {
		lexer := byName[strings.ToLower(name)]
		if lexer == nil {
			continue
		}
		for _, file := range lexer.Config().Filenames {
			extension, ok := strings.CutPrefix(strings.ToLower(file), "*.")
			if _, taken := byName[extension]; ok && !taken && !strings.ContainsAny(extension, "*.[") {
				byName[extension] = lexer
			}
		}
	}
	return byName
})
