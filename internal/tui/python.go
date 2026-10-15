package tui

import (
	"path"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"memhogs.kjanat.dev/internal/collector"
)

func tableProcessSummary(app string, p collector.ProcDetail, width int) string {
	summary := processSummary(app, p)
	if p.Session != nil || ansi.StringWidth(summary) <= width {
		return summary
	}
	if role := pythonCommand(p.Arguments()); role != "" {
		return processSummaryContext(processIdentity(app, p), role, p, true)
	}
	return summary
}

// Python installations in uv caches and virtualenvs identify the interpreter,
// not the workload. Put the script/module and its leading action in the table;
// the inspector retains the complete observed command.
func pythonCommand(args []string) string {
	if len(args) == 0 || !pythonProgram(commandBasename(args[0])) {
		return ""
	}
	program := commandBasename(args[0])
	return pythonInvocation(program, args[1:])
}

func pythonInvocation(program string, args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-m":
			return pythonModule(program, args[i+1:])
		case strings.HasPrefix(arg, "-m"):
			return pythonModule(program, append([]string{arg[2:]}, args[i+1:]...))
		case strings.HasPrefix(arg, "-c"):
			return program + " -c (inline code)"
		case arg == "--":
			return pythonScript(program, args[i+1:])
		case arg == "-":
			return program + " (stdin)"
		case pythonValueOption(arg):
			i++
		case strings.HasPrefix(arg, "-"):
			continue
		default:
			return pythonScript(program, args[i:])
		}
	}
	return program
}

func pythonProgram(name string) bool {
	name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	for _, prefix := range []string{"python", "pythonw", "pypy"} {
		version, ok := strings.CutPrefix(name, prefix)
		if ok && strings.Trim(version, "0123456789.") == "" {
			return true
		}
	}
	return false
}

func pythonRuntimeLabel(application string, args []string) bool {
	return len(args) > 0 && pythonProgram(commandBasename(application)) && pythonProgram(commandBasename(args[0]))
}

func pythonValueOption(arg string) bool {
	return arg == "-W" || arg == "-X" || arg == "--check-hash-based-pycs" || arg == "--jit"
}

func pythonModule(program string, args []string) string {
	if len(args) == 0 {
		return program + " -m"
	}
	return program + " -m " + strings.Join(append([]string{args[0]}, commandPositionals(args[1:])...), " ")
}

func pythonScript(program string, args []string) string {
	if len(args) == 0 {
		return program
	}
	if args[0] == "-" {
		return program + " (stdin)"
	}
	script := commandBasename(args[0])
	switch script {
	case "__main__.py", "__init__.py", "main.py":
		if parent := path.Base(path.Dir(strings.ReplaceAll(args[0], "\\", "/"))); parent != "." && parent != "/" {
			script = parent + "/" + script
		}
	}
	return strings.Join(append([]string{script}, commandPositionals(args[1:])...), " ")
}

func commandBasename(arg string) string {
	return path.Base(strings.ReplaceAll(arg, "\\", "/"))
}
