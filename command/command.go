package command

import (
	"strings"

	"go.minekube.com/brigodier"
	"go.minekube.com/gate/pkg/command"
	"go.minekube.com/gate/pkg/edition/java/proxy"

	"github.com/andreisugu/gate-stonyperms/manager"
)

// SuggestionFunc adapts a function into a brigodier.SuggestionProvider.
type SuggestionFunc func(ctx *brigodier.CommandContext, builder *brigodier.SuggestionsBuilder) *brigodier.Suggestions

// Suggestions implements brigodier.SuggestionProvider.
func (f SuggestionFunc) Suggestions(ctx *brigodier.CommandContext, builder *brigodier.SuggestionsBuilder) *brigodier.Suggestions {
	return f(ctx, builder)
}

// RegisterCommands registers /sp, /stonyperms, /spb, /spv, /perm, and /perms into Gate's command registry.
func RegisterCommands(p *proxy.Proxy, mgr *manager.Manager) {
	aliases := []string{"sp", "stonyperms", "spb", "spv", "perm", "perms"}

	for _, alias := range aliases {
		p.Command().Register(buildCommandTree(alias, p, mgr))
	}
}

func buildCommandTree(alias string, p *proxy.Proxy, mgr *manager.Manager) brigodier.LiteralNodeBuilder {
	return brigodier.Literal(alias).
		Requires(command.Requires(func(ctx *command.RequiresContext) bool {
			return true // Handled dynamically in Dispatch
		})).
		Executes(command.Command(func(cmdCtx *command.Context) error {
			return HandleRoot(mgr, cmdCtx.Source)
		})).
		Then(
			brigodier.Argument("args", brigodier.StringPhrase).
				Suggests(buildSuggestions(p, mgr)).
				Executes(command.Command(func(cmdCtx *command.Context) error {
					raw := cmdCtx.String("args")
					args := ParseCommandLine(raw)
					return Dispatch(mgr, cmdCtx.Source, args)
				})),
		)
}

// Dispatch routes parsed command line arguments to the appropriate handlers.
func Dispatch(mgr *manager.Manager, src command.Source, args []string) error {
	if len(args) == 0 {
		return HandleRoot(mgr, src)
	}

	root := strings.ToLower(args[0])
	rest := args[1:]

	switch root {
	case "help":
		return HandleRoot(mgr, src)
	case "info":
		return HandleInfo(mgr, src)
	case "sync", "reload", "reloadconfig":
		return HandleSync(mgr, src)
	case "check":
		return HandleCheck(mgr, src, rest)
	case "verbose", "v":
		return HandleVerbose(mgr, src, rest)
	case "creategroup":
		return HandleCreateGroup(mgr, src, rest)
	case "deletegroup":
		return HandleDeleteGroup(mgr, src, rest)
	case "listgroups", "groups":
		return HandleListGroups(mgr, src)
	case "createtrack":
		return HandleCreateTrack(mgr, src, rest)
	case "deletetrack":
		return HandleDeleteTrack(mgr, src, rest)
	case "listtracks", "tracks":
		return HandleListTracks(mgr, src)
	case "user", "u":
		return HandleUserDispatch(mgr, src, rest)
	case "group", "g":
		return HandleGroupDispatch(mgr, src, rest)
	case "track", "t":
		return HandleTrackDispatch(mgr, src, rest)
	case "log", "logs", "history":
		return HandleLogDispatch(mgr, src, rest)
	default:
		return src.SendMessage(Msg(Prefix+"&cUnknown subcommand '&e%s&c'. Type &b/sp&c for help.&r", root))
	}
}

// ParseCommandLine splits a command string preserving quotes.
func ParseCommandLine(line string) []string {
	var args []string
	var curr strings.Builder
	inQuotes := false
	var quoteChar rune

	for _, r := range line {
		if (r == '"' || r == '\'') && !inQuotes {
			inQuotes = true
			quoteChar = r
			continue
		}
		if inQuotes && r == quoteChar {
			inQuotes = false
			continue
		}
		if (r == ' ' || r == '\t') && !inQuotes {
			if curr.Len() > 0 {
				args = append(args, curr.String())
				curr.Reset()
			}
			continue
		}
		curr.WriteRune(r)
	}

	if curr.Len() > 0 {
		args = append(args, curr.String())
	}
	return args
}

func buildSuggestions(p *proxy.Proxy, mgr *manager.Manager) SuggestionFunc {
	return func(ctx *brigodier.CommandContext, builder *brigodier.SuggestionsBuilder) *brigodier.Suggestions {
		remaining := builder.Remaining
		args := strings.Fields(remaining)
		trailingSpace := strings.HasSuffix(remaining, " ")

		argCount := len(args)
		if trailingSpace {
			argCount++
		}

		switch argCount {
		case 1:
			// Suggest root subcommands
			roots := []string{
				"user", "group", "track", "info", "sync", "check", "verbose",
				"creategroup", "deletegroup", "listgroups", "createtrack", "deletetrack", "listtracks", "log",
			}
			prefix := ""
			if len(args) > 0 && !trailingSpace {
				prefix = strings.ToLower(args[0])
			}
			for _, r := range roots {
				if strings.HasPrefix(r, prefix) {
					builder.Suggest(r)
				}
			}

		case 2:
			root := strings.ToLower(args[0])
			prefix := ""
			if len(args) > 1 && !trailingSpace {
				prefix = strings.ToLower(args[1])
			}

			if root == "user" || root == "u" {
				// Suggest online players
				if p != nil {
					for _, pl := range p.Players() {
						name := pl.Username()
						if strings.HasPrefix(strings.ToLower(name), prefix) {
							builder.Suggest(name)
						}
					}
				}
			} else if root == "group" || root == "g" || root == "deletegroup" {
				for _, g := range mgr.GetGroups() {
					if strings.HasPrefix(g.Name, prefix) {
						builder.Suggest(g.Name)
					}
				}
			} else if root == "track" || root == "t" || root == "deletetrack" {
				for _, t := range mgr.GetTracks() {
					if strings.HasPrefix(t.Name, prefix) {
						builder.Suggest(t.Name)
					}
				}
			} else if root == "verbose" || root == "v" {
				for _, v := range []string{"on", "off", "record"} {
					if strings.HasPrefix(v, prefix) {
						builder.Suggest(v)
					}
				}
			} else if root == "log" || root == "logs" {
				for _, l := range []string{"recent", "search", "userhistory", "grouphistory", "trackhistory"} {
					if strings.HasPrefix(l, prefix) {
						builder.Suggest(l)
					}
				}
			}

		case 3:
			root := strings.ToLower(args[0])
			prefix := ""
			if len(args) > 2 && !trailingSpace {
				prefix = strings.ToLower(args[2])
			}

			if root == "user" || root == "u" {
				subs := []string{"info", "permission", "parent", "meta", "promote", "demote", "showtracks", "clear", "clone"}
				for _, s := range subs {
					if strings.HasPrefix(s, prefix) {
						builder.Suggest(s)
					}
				}
			} else if root == "group" || root == "g" {
				subs := []string{"info", "permission", "parent", "meta", "setweight", "setdisplayname", "rename", "clone", "listmembers", "showtracks", "clear"}
				for _, s := range subs {
					if strings.HasPrefix(s, prefix) {
						builder.Suggest(s)
					}
				}
			} else if root == "track" || root == "t" {
				subs := []string{"info", "append", "insert", "remove", "clear", "rename"}
				for _, s := range subs {
					if strings.HasPrefix(s, prefix) {
						builder.Suggest(s)
					}
				}
			}

		case 4:
			root := strings.ToLower(args[0])
			sub := strings.ToLower(args[2])
			prefix := ""
			if len(args) > 3 && !trailingSpace {
				prefix = strings.ToLower(args[3])
			}

			if (root == "user" || root == "group") && (sub == "permission" || sub == "perm" || sub == "p") {
				for _, a := range []string{"set", "unset", "settemp", "unsettemp", "check", "clear"} {
					if strings.HasPrefix(a, prefix) {
						builder.Suggest(a)
					}
				}
			} else if (root == "user" || root == "group") && (sub == "parent" || sub == "parents") {
				for _, a := range []string{"info", "add", "set", "remove", "switchprimarygroup", "addtemp", "clear"} {
					if strings.HasPrefix(a, prefix) {
						builder.Suggest(a)
					}
				}
			} else if (root == "user" || root == "group") && (sub == "meta" || sub == "m") {
				for _, a := range []string{"info", "set", "unset", "setprefix", "setsuffix", "removeprefix", "removesuffix", "clear"} {
					if strings.HasPrefix(a, prefix) {
						builder.Suggest(a)
					}
				}
			} else if root == "user" && (sub == "promote" || sub == "demote") {
				for _, t := range mgr.GetTracks() {
					if strings.HasPrefix(t.Name, prefix) {
						builder.Suggest(t.Name)
					}
				}
			}
		}

		return builder.Build()
	}
}
