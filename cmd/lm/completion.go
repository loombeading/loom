// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/loombeading/loom/internal/output"
)

func runCompletion(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		output.WriteError(stderr, "lm completion requires exactly one shell (zsh, bash)")
		return 1
	}
	switch args[0] {
	case "zsh":
		fmt.Fprint(stdout, zshCompletion())
	case "bash":
		fmt.Fprint(stdout, bashCompletion())
	default:
		output.WriteError(stderr, fmt.Sprintf("unknown lm completion shell %q (want zsh, bash)", args[0]))
		return 1
	}
	return 0
}

func zshCompletion() string {
	var b strings.Builder
	b.WriteString("#compdef lm\n\n")
	b.WriteString("_lm() {\n")
	b.WriteString("  local -a commands\n")
	b.WriteString("  commands=(\n")
	for _, c := range registry {
		fmt.Fprintf(&b, "    '%s:%s'\n", c.name, zshEscape(c.desc))
	}
	b.WriteString("    'help:Show command help'\n")
	b.WriteString("    'completion:Generate a shell completion script'\n")
	b.WriteString("  )\n\n")

	b.WriteString("  if (( CURRENT == 2 )); then\n")
	b.WriteString("    _describe 'command' commands\n")
	b.WriteString("    return\n")
	b.WriteString("  fi\n\n")

	b.WriteString("  local cmd=${words[2]}\n")
	b.WriteString("  case $cmd in\n")
	for _, c := range registry {
		if len(c.sub) == 0 {
			if len(c.flags) == 0 {
				continue
			}
			fmt.Fprintf(&b, "    %s)\n", c.name)
			fmt.Fprintf(&b, "      _arguments %s\n", zshFlagArgs(c.flags))
			b.WriteString("      ;;\n")
			continue
		}
		fmt.Fprintf(&b, "    %s)\n", c.name)
		b.WriteString("      if (( CURRENT == 3 )); then\n")
		b.WriteString("        local -a subs\n")
		b.WriteString("        subs=(\n")
		for _, s := range c.sub {
			fmt.Fprintf(&b, "          '%s:%s'\n", s.name, zshEscape(s.desc))
		}
		for _, f := range c.flags {
			fmt.Fprintf(&b, "          '--%s:%s'\n", f, zshEscape(f))
		}
		b.WriteString("        )\n")
		b.WriteString("        _describe 'subcommand' subs\n")
		b.WriteString("        return\n")
		b.WriteString("      fi\n")
		b.WriteString("      local sub=${words[3]}\n")
		b.WriteString("      case $sub in\n")
		for _, s := range c.sub {
			if len(s.flags) == 0 {
				continue
			}
			fmt.Fprintf(&b, "        %s)\n", s.name)
			fmt.Fprintf(&b, "          _arguments %s\n", zshFlagArgs(s.flags))
			b.WriteString("          ;;\n")
		}
		b.WriteString("      esac\n")
		b.WriteString("      ;;\n")
	}
	b.WriteString("    help)\n")
	b.WriteString("      if (( CURRENT == 3 )); then\n")
	b.WriteString("        _describe 'command' commands\n")
	b.WriteString("      fi\n")
	b.WriteString("      ;;\n")
	b.WriteString("    completion)\n")
	b.WriteString("      if (( CURRENT == 3 )); then\n")
	b.WriteString("        _values 'shell' zsh bash\n")
	b.WriteString("      fi\n")
	b.WriteString("      ;;\n")
	b.WriteString("  esac\n")
	b.WriteString("}\n\n")
	b.WriteString("_lm \"$@\"\n")
	return b.String()
}

func zshFlagArgs(flags []string) string {
	parts := make([]string, len(flags))
	for i, f := range flags {
		parts[i] = fmt.Sprintf("'--%s[%s]'", f, f)
	}
	return strings.Join(parts, " ")
}

func zshEscape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "'\\''")
	s = strings.ReplaceAll(s, ":", "\\:")
	s = strings.ReplaceAll(s, "[", "\\[")
	s = strings.ReplaceAll(s, "]", "\\]")
	return s
}

func bashCompletion() string {
	var b strings.Builder
	b.WriteString("_lm() {\n")
	b.WriteString("  local cur prev words cword\n")
	b.WriteString("  COMPREPLY=()\n")
	b.WriteString("  cur=${COMP_WORDS[COMP_CWORD]}\n")
	b.WriteString("  cmd=${COMP_WORDS[1]}\n")
	b.WriteString("  sub=${COMP_WORDS[2]}\n\n")

	reg := registry
	commandNames := make([]string, 0, len(reg)+2)
	for _, c := range reg {
		commandNames = append(commandNames, c.name)
	}
	commandNames = append(commandNames, "help", "completion")

	b.WriteString("  if (( COMP_CWORD == 1 )); then\n")
	fmt.Fprintf(&b, "    COMPREPLY=( $(compgen -W \"%s\" -- \"$cur\") )\n", strings.Join(commandNames, " "))
	b.WriteString("    return\n")
	b.WriteString("  fi\n\n")

	b.WriteString("  case $cmd in\n")
	for _, c := range registry {
		if len(c.sub) == 0 {
			if len(c.flags) == 0 {
				continue
			}
			fmt.Fprintf(&b, "    %s)\n", c.name)
			fmt.Fprintf(&b, "      COMPREPLY=( $(compgen -W \"%s\" -- \"$cur\") )\n", bashFlagWords(c.flags))
			b.WriteString("      ;;\n")
			continue
		}
		subNames := make([]string, 0, len(c.sub)+len(c.flags))
		for _, s := range c.sub {
			subNames = append(subNames, s.name)
		}
		for _, f := range c.flags {
			subNames = append(subNames, "--"+f)
		}
		fmt.Fprintf(&b, "    %s)\n", c.name)
		b.WriteString("      if (( COMP_CWORD == 2 )); then\n")
		fmt.Fprintf(&b, "        COMPREPLY=( $(compgen -W \"%s\" -- \"$cur\") )\n", strings.Join(subNames, " "))
		b.WriteString("        return\n")
		b.WriteString("      fi\n")
		b.WriteString("      case $sub in\n")
		for _, s := range c.sub {
			if len(s.flags) == 0 {
				continue
			}
			fmt.Fprintf(&b, "        %s)\n", s.name)
			fmt.Fprintf(&b, "          COMPREPLY=( $(compgen -W \"%s\" -- \"$cur\") )\n", bashFlagWords(s.flags))
			b.WriteString("          ;;\n")
		}
		b.WriteString("      esac\n")
		b.WriteString("      ;;\n")
	}
	b.WriteString("    help)\n")
	fmt.Fprintf(&b, "      COMPREPLY=( $(compgen -W \"%s\" -- \"$cur\") )\n", strings.Join(commandNames, " "))
	b.WriteString("      ;;\n")
	b.WriteString("    completion)\n")
	b.WriteString("      COMPREPLY=( $(compgen -W \"zsh bash\" -- \"$cur\") )\n")
	b.WriteString("      ;;\n")
	b.WriteString("  esac\n")
	b.WriteString("}\n")
	b.WriteString("complete -F _lm lm\n")
	return b.String()
}

func bashFlagWords(flags []string) string {
	parts := make([]string, len(flags))
	for i, f := range flags {
		parts[i] = "--" + f
	}
	return strings.Join(parts, " ")
}
