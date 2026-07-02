package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// NewWarpCompletionPluginCmd emits the native Warp completion plugin.
// It is intentionally hidden because users should get it through install.sh.
func NewWarpCompletionPluginCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "__warp_completion_plugin",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			plugin, err := WarpCompletionPlugin(cmd.Root())
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), plugin)
			return err
		},
	}
}

// WarpCompletionPlugin returns the JS source for Warp's native completions API.
func WarpCompletionPlugin(root *cobra.Command) (string, error) {
	if root == nil {
		return "", errors.New("warp completion: nil root command")
	}
	root.InitDefaultCompletionCmd("completion")

	signature := warpCommandSignature{
		Command: warpCommandFromCobra(root, nil),
		ParseOptions: warpParseOptions{
			OptionArgumentDelimiters: []string{"=", " "},
		},
	}
	body, err := json.MarshalIndent(signature, "", "  ")
	if err != nil {
		return "", fmt.Errorf("warp completion: marshal signature: %w", err)
	}
	return warpCompletionPluginHeader + "\nconst quarkSignature = " +
		string(body) + ";\n" + warpCompletionPluginFooter, nil
}

type warpCommandSignature struct {
	Command      warpCommand      `json:"command"`
	ParseOptions warpParseOptions `json:"parseOptions,omitempty"`
}

type warpParseOptions struct {
	OptionArgumentDelimiters []string `json:"optionArgumentDelimiters,omitempty"`
}

type warpCommand struct {
	Name        string        `json:"name"`
	Alias       any           `json:"alias,omitempty"`
	InsertValue string        `json:"insertValue,omitempty"`
	Description string        `json:"description,omitempty"`
	Arguments   any           `json:"arguments,omitempty"`
	Subcommands []warpCommand `json:"subcommands,omitempty"`
	Options     []warpOption  `json:"options,omitempty"`
}

type warpArgument struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Values      []warpDynamicValue `json:"values,omitempty"`
	Optional    bool               `json:"optional,omitempty"`
}

type warpOption struct {
	Name        any    `json:"name"`
	Description string `json:"description,omitempty"`
	Arguments   any    `json:"arguments,omitempty"`
	Deprecated  bool   `json:"deprecated,omitempty"`
}

type warpDynamicValue struct {
	QuarkCobraGenerator *warpCobraGenerator `json:"quarkCobraGenerator,omitempty"`
}

type warpCobraGenerator struct {
	Prefix                 []string `json:"prefix"`
	CompletedBeforeCurrent int      `json:"completedBeforeCurrent,omitempty"`
}

func warpCommandFromCobra(cmd *cobra.Command, path []string) warpCommand {
	commandPath := path
	if cmd.HasParent() {
		commandPath = append(append([]string{}, path...), cmd.Name())
	}

	out := warpCommand{
		Name:        cmd.Name(),
		Alias:       warpAlias(cmd.Aliases),
		Description: cmd.Short,
		Arguments:   warpArgumentsFromUse(cmd.Use, commandPath, cmd.ValidArgsFunction != nil),
		Options:     warpOptionsFromCommand(cmd, commandPath),
	}

	for _, sub := range cmd.Commands() {
		if skipWarpCommand(sub) {
			continue
		}
		out.Subcommands = append(out.Subcommands, warpCommandFromCobra(sub, commandPath))
	}
	return out
}

func warpAlias(aliases []string) any {
	switch len(aliases) {
	case 0:
		return nil
	case 1:
		return aliases[0]
	default:
		return aliases
	}
}

func skipWarpCommand(cmd *cobra.Command) bool {
	if cmd.Hidden {
		return true
	}
	switch cmd.Name() {
	case "help", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
		return true
	default:
		return false
	}
}

func warpArgumentsFromUse(use string, path []string, dynamic bool) any {
	fields := strings.Fields(use)
	if len(fields) <= 1 {
		return nil
	}

	args := make([]warpArgument, 0, len(fields)-1)
	for i, field := range fields[1:] {
		arg := warpArgument{
			Name:     cleanWarpArgumentName(field),
			Optional: strings.HasPrefix(field, "["),
		}
		if dynamic {
			arg.Values = []warpDynamicValue{{
				QuarkCobraGenerator: &warpCobraGenerator{
					Prefix:                 path,
					CompletedBeforeCurrent: i,
				},
			}}
		}
		args = append(args, arg)
	}
	return compactWarpArguments(args)
}

func cleanWarpArgumentName(value string) string {
	value = strings.Trim(value, "[]<>")
	value = strings.TrimSuffix(value, "...")
	return value
}

func compactWarpArguments(args []warpArgument) any {
	switch len(args) {
	case 0:
		return nil
	case 1:
		return args[0]
	default:
		return args
	}
}

func warpOptionsFromCommand(cmd *cobra.Command, path []string) []warpOption {
	seen := map[string]struct{}{}
	var out []warpOption
	appendFlags := func(flags *pflag.FlagSet) {
		if flags == nil {
			return
		}
		flags.VisitAll(func(flag *pflag.Flag) {
			if flag.Hidden {
				return
			}
			if _, ok := seen[flag.Name]; ok {
				return
			}
			seen[flag.Name] = struct{}{}
			out = append(out, warpOptionFromFlag(cmd, flag, path))
		})
	}

	appendFlags(cmd.LocalNonPersistentFlags())
	appendFlags(cmd.PersistentFlags())
	appendFlags(cmd.InheritedFlags())
	if _, ok := seen["help"]; !ok {
		out = append(out, warpOption{
			Name:        []string{"-h", "--help"},
			Description: "help for " + cmd.Name(),
		})
	}
	return out
}

func warpOptionFromFlag(cmd *cobra.Command, flag *pflag.Flag, path []string) warpOption {
	opt := warpOption{
		Name:        warpFlagName(flag),
		Description: flag.Usage,
		Deprecated:  flag.Deprecated != "",
	}
	if flag.Value != nil && flag.Value.Type() != "bool" {
		arg := warpArgument{Name: flag.Name}
		if _, ok := cmd.GetFlagCompletionFunc(flag.Name); ok {
			arg.Values = []warpDynamicValue{{
				QuarkCobraGenerator: &warpCobraGenerator{
					Prefix: append(append([]string{}, path...), "--"+flag.Name),
				},
			}}
		}
		opt.Arguments = arg
	}
	return opt
}

func warpFlagName(flag *pflag.Flag) any {
	long := "--" + flag.Name
	if flag.Shorthand == "" {
		return long
	}
	return []string{"-" + flag.Shorthand, long}
}

const warpCompletionPluginHeader = `function suggestion(value, description) {
  return {
    value,
    displayValue: value,
    insertValue: shellInsertValue(value),
    description,
  };
}

function shellInsertValue(value) {
  return String(value)
    .replace(/([\\\s"'$])/g, "\\$1")
    .replace(new RegExp(String.fromCharCode(96), "g"), "\\" + String.fromCharCode(96));
}

function shellQuote(value) {
  return "'" + String(value).replace(/'/g, "'\\''") + "'";
}

function parseCobraCompletion(output) {
  return String(output || "")
    .split(/\r?\n/)
    .filter((line) => {
      return line !== "" &&
        !line.startsWith(":") &&
        !line.startsWith("Completion ended with directive:");
    })
    .map((line) => {
      const tab = line.indexOf("\t");
      if (tab === -1) {
        return suggestion(line, undefined);
      }
      return suggestion(line.slice(0, tab), line.slice(tab + 1));
    });
}

function withoutRootToken(tokens) {
  const out = Array.isArray(tokens) ? tokens.slice() : [];
  if (out[0] === "quark") {
    out.shift();
  }
  return out;
}

function prefixIndex(tokens, prefix) {
  for (let start = 0; start <= tokens.length - prefix.length; start += 1) {
    if (prefix.every((token, index) => tokens[start + index] === token)) {
      return start;
    }
  }
  return -1;
}

function cobraArgs(ctx, prefix, completedBeforeCurrent) {
  let args = withoutRootToken(ctx.tokens);
  let start = prefixIndex(args, prefix);
  if (start === -1) {
    args = prefix.slice();
    start = 0;
  }

  const currentIndex = start + prefix.length + completedBeforeCurrent;
  if (args.length === currentIndex) {
    args.push("");
  }
  return args;
}

function cobraGenerator(prefix, completedBeforeCurrent = 0) {
  return {
    generateSuggestionsFn: async (ctx) => {
      const args = cobraArgs(ctx, prefix, completedBeforeCurrent);
      const quotedArgs = args.map(shellQuote).join(" ");
      const pwd = shellQuote(ctx.pwd || ".");
      const result = await ctx.executeShellCommand("cd " + pwd + " && quark __complete " + quotedArgs);
      if (!result || !result.success) {
        return { suggestions: [] };
      }
      return { suggestions: parseCobraCompletion(result.output), is_ordered: true };
    },
  };
}

function hydrateDynamicValues(node) {
  if (Array.isArray(node)) {
    node.forEach(hydrateDynamicValues);
    return;
  }
  if (!node || typeof node !== "object") {
    return;
  }
  if (node.quarkCobraGenerator) {
    const spec = node.quarkCobraGenerator;
    delete node.quarkCobraGenerator;
    Object.assign(node, cobraGenerator(spec.prefix || [], spec.completedBeforeCurrent || 0));
  }
  Object.values(node).forEach(hydrateDynamicValues);
}
`

const warpCompletionPluginFooter = `export function activate(warp) {
  hydrateDynamicValues(quarkSignature);
  warp.completions.registerCommandSignature(quarkSignature);
}
`
