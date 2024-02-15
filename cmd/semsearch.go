package main

import (
	"fmt"
	"gopkg.in/yaml.v2"
	"os"
	"os/exec"
	"strings"
)

var help string = `Usage: semsearch [options]

Pattern options:

  -p,   --pattern <pattern>                Pattern to search for
  -pi,  --pattern-inside <pattern>         Pattern to search for inside the matched pattern
  -pni, --pattern-not-inside <pattern>     Pattern to search for not inside the matched pattern
  -pr,  --pattern-regex <pattern>          Pattern to search for using a regex
  -pnr, --pattern-not-regex <pattern>      Pattern to search for not using a regex
  -mr,  --metavariable-regex <name=regex>  Metavariable to search for using a regex
  -fm,  --focus-metavariable <name>        Metavariable to focus on

Pattern group options:
  -ps,  --patterns                         Start a new pattern group
  -pe,  --pattern-either <pattern>         Pattern to search for either the matched pattern or the given pattern
  [                                        Start a pattern group
  ],    --pop                              End the pattern group

Search options:
  -l,   --language <language>              Language to search for (default: generic)
  -i,   --file, --path <path>              Evaluate the rule on the given path
  --eval <string>                          Evaluate the rule on the given string

Rule options:
  -f,  --format <format>                   Output format (json, text, sarif, vim)
  -c,  --config <config>                   Run additional rules
  -m,  --message <message>                 Message to display
  --id <id>                                Rule id
  --export                                 Output the YAML rule
`

var shortcuts = map[string]string{
	"p":   "pattern",
	"pi":  "pattern-inside",
	"pni": "pattern-not-inside",
	"pe":  "pattern-either",
	"pr":  "pattern-regex",
	"pnr": "pattern-not-regex",
	"ps":  "patterns",
	"l":   "language",
	"f":   "format",
	"mr":  "metavariable-regex",
	"fm":  "focus-metavariable",
	"c":   "config",
	"e":   "eval",
	"i":   "file",
	"m":   "message",
}

type Rule struct {
	Id        string         `yaml:"id"`
	Patterns  *[]interface{} `yaml:"patterns"`
	Severity  string         `yaml:"severity"`
	Message   string         `yaml:"message"`
	Languages []string       `yaml:"languages"`
}

type MetavariableRegex struct {
	Metavariable string `yaml:"metavariable,omitempty"`
	Regex        string `yaml:"regex,omitempty"`
}

type MetavariablePattern struct {
	Metavariable string      `yaml:"metavariable,omitempty"`
	Patterns     interface{} `yaml:"patterns,omitempty"`
}

type Condition struct {
	Pattern           string            `yaml:"pattern,omitempty"`
	PatternNot        string            `yaml:"pattern-not,omitempty"`
	PatternInside     string            `yaml:"pattern-inside,omitempty"`
	PatternNotInside  string            `yaml:"pattern-not-inside,omitempty"`
	PatternRegex      string            `yaml:"pattern-regex,omitempty"`
	PatternNotRegex   string            `yaml:"pattern-not-regex,omitempty"`
	FocusMetavariable string            `yaml:"focus-metavariable,omitempty"`
	MetavariableRegex MetavariableRegex `yaml:"metavariable-regex,omitempty"`

	Patterns            *[]interface{}      `yaml:"patterns,omitempty"`
	PatternEither       *[]interface{}      `yaml:"pattern-either,omitempty"`
	MetavariablePattern MetavariablePattern `yaml:"metavariable-pattern,omitempty"`
}

type State struct {
	Rule
	Stack     []*[]interface{}
	Paths     []string
	Configs   []string
	Format    string
	Debug     bool
	Export    bool
	Evals     []string
	Pairs     int
	Tempfiles []string
}

func metavar(value string) string {
	if value[0] != '$' {
		return "$" + value
	}

	return value
}

func NewState() *State {
	patterns := make([]interface{}, 0)
	return &State{Rule: Rule{
		Id:       "id",
		Patterns: &patterns,
		Severity: "WARNING",
	},
		Format: "text",
		Stack:  []*[]interface{}{&patterns},
	}
}

func (s *State) Args() []string {
	args := []string{
		"scan",
		"--quiet",
		"--no-rewrite-rule-ids",
		"--metrics=off",
		"--disable-version-check",
		fmt.Sprintf("--%s", s.Format),
	}

	for _, config := range s.Configs {
		args = append(args, "--config", config)
	}

	if len(s.Evals) > 0 {
		args = append(args, "--scan-unknown-extensions")
	}

	args = append(args, s.Paths...)
	return args
}

func (s *State) AddCondition(cond Condition) {
	head := s.Stack[len(s.Stack)-1]
	*head = append(*head, cond)
}

func (s *State) Build(args []string) {
	var cmd string
	var value string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if len(arg) > 2 && arg[0:2] == "--" {
			cmd = arg[2:]
		} else if arg[0] == '-' {
			cmd = shortcuts[arg[1:]]
		} else {
			cmd = arg
		}

		switch cmd {
		case "json", "vim", "emacs", "sarif", "text":
			s.Format = cmd
			continue
		case "[":
			s.Pairs += 1
			continue
		case "debug":
			s.Debug = true
			continue
		case "patterns":
			collection := make([]interface{}, 0)
			s.AddCondition(Condition{Patterns: &collection})
			s.Stack = append(s.Stack, &collection)
			continue
		case "pattern-either":
			collection := make([]interface{}, 0)
			s.AddCondition(Condition{PatternEither: &collection})
			s.Stack = append(s.Stack, &collection)
			continue
		case "pop", "]":
			if cmd == "]" {
				s.Pairs -= 1
			}
			if len(s.Stack) == 1 || s.Pairs < 0 {
				fmt.Fprintln(os.Stderr, "Error: stack underflow")
				continue
			}
			s.Stack = s.Stack[:len(s.Stack)-1]
			continue
		case "export":
			s.Export = true
			continue
		}

		i += 1
		if i < len(args) {
			value = args[i]
		}

		switch cmd {
		case "format":
			s.Format = value
		case "pattern":
			s.AddCondition(Condition{Pattern: value})
		case "pattern-not":
			s.AddCondition(Condition{PatternNot: value})
		case "pattern-inside":
			s.AddCondition(Condition{PatternInside: value})
		case "pattern-not-inside":
			s.AddCondition(Condition{PatternNotInside: value})
		case "pattern-regex":
			s.AddCondition(Condition{PatternRegex: value})
		case "pattern-not-regex":
			s.AddCondition(Condition{PatternNotRegex: value})
		case "metavariable-regex":
			parts := strings.Split(value, "=")
			s.AddCondition(Condition{
				MetavariableRegex: MetavariableRegex{
					Metavariable: metavar(parts[0]),
					Regex:        parts[1],
				},
			})
		case "focus-metavariable":
			s.AddCondition(Condition{
				FocusMetavariable: metavar(value),
			})
		case "message":
			s.Rule.Message = value
		case "language":
			s.Languages = append(s.Languages, value)
		case "config":
			s.Configs = append(s.Configs, value)
		case "path", "file":
			s.Paths = append(s.Paths, value)
		case "id":
			s.Rule.Id = value
		case "eval":
			s.Evals = append(s.Evals, value)
		case "severity":
			s.Severity = value
		default:
			fmt.Fprintf(os.Stderr, "Error: unknown cli option %s\n", cmd)
			os.Exit(1)
		}
	}

	if len(s.Languages) == 0 {
		s.Languages = append(s.Languages, "generic")
	}
}

func (s *State) Tempfile(name string) (*os.File, error) {
	file, err := os.CreateTemp("", name)
	if err != nil {
		return nil, err
	}
	s.Tempfiles = append(s.Tempfiles, file.Name())
	return file, nil
}

func (s *State) Cleanup() {
	for _, file := range s.Tempfiles {
		os.Remove(file)
	}
}

func (s *State) Prepare() {
	if s.Pairs != 0 {
		fmt.Fprintln(os.Stderr, "Error: unmatched brackets")
		os.Exit(1)
	}

	for i, eval := range s.Evals {
		input, err := s.Tempfile(fmt.Sprintf("semsearch-input-%d-", i+1))
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error: failed to create temporary input file")
			return
		}
		input.WriteString(eval)
		s.Paths = append(s.Paths, input.Name())
	}
}

func (s *State) Exec() {
	rulefile, err := s.Tempfile("semsearch-rule-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: failed to create temporary rule file")
		return
	}

	s.Configs = append(s.Configs, rulefile.Name())
	rule := s.Rule
	args := s.Args()
	rules := map[string]interface{}{
		"rules": []Rule{rule},
	}

	yaml.NewEncoder(rulefile).Encode(rules)
	rulefile.Close()

	if s.Debug {
		yaml.NewEncoder(os.Stderr).Encode(rules)
		fmt.Fprintf(os.Stderr, "command: semgrep %s\n", strings.Join(args, " "))
	}

	if s.Export {
		yaml.NewEncoder(os.Stdout).Encode(rules)
		return
	}

	cmd := exec.Command("semgrep", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}

func main() {
	if len(os.Args) == 1 {
		fmt.Println(help)
		return
	}

	state := NewState()
	defer state.Cleanup()

	state.Build(os.Args[1:])
	state.Prepare()
	state.Exec()
}
