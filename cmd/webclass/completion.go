package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/liquidcatmofu/webclass-cli/internal/state"
)

func completionScript(shell string) (string, error) {
	switch strings.ToLower(shell) {
	case "bash":
		return bashCompletion, nil
	case "zsh":
		return zshCompletion, nil
	case "fish":
		return fishCompletion, nil
	case "powershell", "pwsh":
		return powershellCompletion, nil
	default:
		return "", fmt.Errorf("unsupported shell %q; use bash, zsh, fish, or powershell", shell)
	}
}

// completionCandidates is intentionally local-only. Shell completion must not
// turn repeated Tab presses into WebClass requests. Course and material names
// are learned from the manifest produced by previous pulls.
func completionCandidates(words []string) []string {
	if len(words) == 0 {
		return []string{"auth", "courses", "pull", "completion", "help"}
	}

	current := words[len(words)-1]
	before := words[:len(words)-1]
	if len(before) == 0 {
		return prefixCandidates(current, []string{"auth", "courses", "pull", "completion", "help"})
	}

	command := before[0]
	if command == "completion" {
		return prefixCandidates(current, []string{"bash", "zsh", "fish", "powershell"})
	}

	var flags []string
	switch command {
	case "auth":
		flags = []string{"--base-url", "--browser"}
	case "courses":
		flags = []string{"--base-url"}
	case "pull":
		flags = []string{"--base-url", "--dir", "--group-dirs", "--interval", "--material"}
	default:
		return nil
	}

	if strings.HasPrefix(current, "-") {
		return prefixCandidates(current, flags)
	}
	if command != "pull" {
		return nil
	}

	if len(before) > 0 {
		prev := before[len(before)-1]
		if prev == "--material" {
			dir := completionDir(before[1:])
			courseID := completionCourse(before[1:])
			return manifestMaterials(dir, courseID, current)
		}
		if prev == "--base-url" || prev == "--dir" || prev == "--interval" {
			return nil
		}
	}

	if completionCourse(before[1:]) == "" {
		return manifestCourses(completionDir(before[1:]), current)
	}
	return nil
}

func completionDir(words []string) string {
	dir := "webclass"
	for i := 0; i < len(words); i++ {
		if words[i] == "--dir" && i+1 < len(words) {
			dir = words[i+1]
			i++
			continue
		}
		if strings.HasPrefix(words[i], "--dir=") {
			dir = strings.TrimPrefix(words[i], "--dir=")
		}
	}
	return dir
}

func completionCourse(words []string) string {
	expectsValue := map[string]bool{
		"--base-url": true,
		"--dir":      true,
		"--interval": true,
		"--material": true,
	}
	for i := 0; i < len(words); i++ {
		word := words[i]
		if expectsValue[word] {
			i++
			continue
		}
		if strings.HasPrefix(word, "--") {
			continue
		}
		return word
	}
	return ""
}

func manifestCourses(dir, prefix string) []string {
	manifest, err := state.Load(dir)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	for _, entry := range manifest.Entries {
		if entry.CourseID != "" {
			seen[entry.CourseID] = true
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		if strings.HasPrefix(strings.ToLower(id), strings.ToLower(prefix)) {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func manifestMaterials(dir, courseID, prefix string) []string {
	manifest, err := state.Load(dir)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	for _, entry := range manifest.Entries {
		if courseID != "" && entry.CourseID != courseID {
			continue
		}
		if entry.ResourceTitle != "" {
			seen[entry.ResourceTitle] = true
		}
	}
	out := make([]string, 0, len(seen))
	lowPrefix := strings.ToLower(prefix)
	for title := range seen {
		if strings.HasPrefix(strings.ToLower(title), lowPrefix) {
			out = append(out, title)
		}
	}
	sort.Strings(out)
	return out
}

func prefixCandidates(prefix string, candidates []string) []string {
	var out []string
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate, prefix) {
			out = append(out, candidate)
		}
	}
	return out
}

const bashCompletion = `# bash completion for webclass
_webclass_complete() {
    local -a candidates
    local cur
    cur="${COMP_WORDS[COMP_CWORD]}"
    mapfile -t candidates < <("${COMP_WORDS[0]}" __complete "${COMP_WORDS[@]:1:COMP_CWORD}" 2>/dev/null)
    COMPREPLY=()
    local candidate
    for candidate in "${candidates[@]}"; do
        [[ "$candidate" == "$cur"* ]] && COMPREPLY+=("$candidate")
    done
}
complete -o default -F _webclass_complete webclass
complete -o default -F _webclass_complete webclass.exe
`

const zshCompletion = `#compdef webclass webclass.exe
_webclass_complete() {
    local -a candidates args
    args=("${words[@]:2}")
    candidates=("${(@f)$($words[1] __complete "${args[@]}" 2>/dev/null)}")
    (( ${#candidates[@]} )) && compadd -Q -- "${candidates[@]}"
}
compdef _webclass_complete webclass webclass.exe
`

const fishCompletion = `function __webclass_complete
    set -l tokens (commandline -opc)
    set -l current (commandline -ct)
    set -l exe $tokens[1]
    set -e tokens[1]
    command $exe __complete $tokens $current 2>/dev/null
end
complete -c webclass -f -a '(__webclass_complete)'
complete -c webclass.exe -f -a '(__webclass_complete)'
`

const powershellCompletion = `Register-ArgumentCompleter -Native -CommandName webclass,webclass.exe,'.\webclass.exe','./webclass.exe' -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)

    $elements = @($commandAst.CommandElements)
    if ($elements.Count -eq 0) { return }
    $exe = $elements[0].Extent.Text
    $args = @()
    for ($i = 1; $i -lt $elements.Count; $i++) {
        $element = $elements[$i]
        if ($element -is [System.Management.Automation.Language.StringConstantExpressionAst]) {
            $args += $element.Value
        } else {
            $args += $element.Extent.Text
        }
    }
    if ($args.Count -eq 0 -or $args[-1] -ne $wordToComplete) {
        $args += $wordToComplete
    }

    & $exe __complete @args 2>$null | ForEach-Object {
        [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
    }
}
`
