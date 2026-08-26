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

		// Calculate exact replacement range for the current word being typed/completed
		var wordStart int
		var currentWordPrefix string

		if trailingSpace {
			wordStart = len(builder.Input)
			currentWordPrefix = ""
		} else {
			lastSpace := strings.LastIndex(remaining, " ")
			if lastSpace >= 0 {
				wordStart = builder.Start + lastSpace + 1
				currentWordPrefix = strings.ToLower(remaining[lastSpace+1:])
			} else {
				wordStart = builder.Start
				currentWordPrefix = strings.ToLower(remaining)
			}
		}

		argCount := len(args)
		if trailingSpace {
			argCount++
		}

		var candidates []string

		switch argCount {
		case 1:
			candidates = []string{
				"user", "group", "track", "info", "sync", "check", "verbose",
				"creategroup", "deletegroup", "listgroups", "createtrack", "deletetrack", "listtracks", "log",
			}

		case 2:
			root := strings.ToLower(args[0])
			switch root {
			case "user", "u":
				if p != nil {
					for _, pl := range p.Players() {
						candidates = append(candidates, pl.Username())
					}
				}
				for _, u := range mgr.GetGroupMembers("default") {
					candidates = append(candidates, u.Username)
				}
			case "group", "g", "deletegroup":
				for _, g := range mgr.GetGroups() {
					candidates = append(candidates, g.Name)
				}
			case "track", "t", "deletetrack":
				for _, t := range mgr.GetTracks() {
					candidates = append(candidates, t.Name)
				}
			case "verbose", "v":
				candidates = []string{"on", "off", "record"}
			case "log", "logs":
				candidates = []string{"recent", "search", "userhistory", "grouphistory", "trackhistory"}
			case "check":
				if p != nil {
					for _, pl := range p.Players() {
						candidates = append(candidates, pl.Username())
					}
				}
			}

		case 3:
			root := strings.ToLower(args[0])
			switch root {
			case "user", "u":
				candidates = []string{"info", "permission", "parent", "meta", "promote", "demote", "showtracks", "clear", "clone"}
			case "group", "g":
				candidates = []string{"info", "permission", "parent", "meta", "setweight", "setdisplayname", "rename", "clone", "listmembers", "showtracks", "clear"}
			case "track", "t":
				candidates = []string{"info", "append", "insert", "remove", "clear", "rename"}
			case "check":
				candidates = []string{"gate.command.server", "gate.command.send", "gate.command.glist", "gate.*", "*", "stonyperms.admin"}
			}

		case 4:
			root := strings.ToLower(args[0])
			sub := strings.ToLower(args[2])
			if root == "user" || root == "u" || root == "group" || root == "g" {
				switch sub {
				case "permission", "perm", "p":
					candidates = []string{"set", "unset", "settemp", "unsettemp", "check", "clear"}
				case "parent", "parents":
					candidates = []string{"info", "add", "set", "remove", "switchprimarygroup", "addtemp", "clear"}
				case "meta", "m":
					candidates = []string{"info", "set", "unset", "setprefix", "setsuffix", "removeprefix", "removesuffix", "clear"}
				case "promote", "demote":
					for _, t := range mgr.GetTracks() {
						candidates = append(candidates, t.Name)
					}
				}
			} else if root == "track" || root == "t" {
				if sub == "append" || sub == "insert" || sub == "remove" {
					for _, g := range mgr.GetGroups() {
						candidates = append(candidates, g.Name)
					}
				}
			}

		case 5:
			root := strings.ToLower(args[0])
			sub := strings.ToLower(args[2])
			action := strings.ToLower(args[3])
			if (root == "user" || root == "group") && (sub == "permission" || sub == "perm" || sub == "p") {
				switch action {
				case "set", "settemp", "unset", "unsettemp", "check":
					candidates = []string{
						"gate.command.server",
						"gate.command.send",
						"gate.command.glist",
						"gate.command.ping",
						"gate.*",
						"stonyperms.admin",
						"stonyperms.user",
						"stonyperms.group",
						"*",
					}
				}
			} else if (root == "user" || root == "group") && (sub == "parent" || sub == "parents") {
				switch action {
				case "add", "set", "remove", "switchprimarygroup", "addtemp":
					for _, g := range mgr.GetGroups() {
						candidates = append(candidates, g.Name)
					}
				}
			}

		case 6:
			root := strings.ToLower(args[0])
			sub := strings.ToLower(args[2])
			action := strings.ToLower(args[3])
			if (root == "user" || root == "group") && (sub == "permission" || sub == "perm" || sub == "p") {
				if action == "set" || action == "settemp" {
					candidates = []string{"true", "false"}
				}
			} else if (root == "user" || root == "group") && (sub == "parent" || sub == "parents") {
				if action == "addtemp" {
					candidates = []string{"10m", "1h", "12h", "1d", "7d", "30d"}
				}
			}

		case 7:
			root := strings.ToLower(args[0])
			sub := strings.ToLower(args[2])
			action := strings.ToLower(args[3])
			if (root == "user" || root == "group") && (sub == "permission" || sub == "perm" || sub == "p") {
				if action == "settemp" {
					candidates = []string{"10m", "1h", "12h", "1d", "7d", "30d"}
				} else if action == "set" {
					if p != nil {
						for _, s := range p.Servers() {
							candidates = append(candidates, "server="+s.ServerInfo().Name())
						}
					}
				}
			}
		}

		// Filter candidates and build suggestions with the exact word replacement range
		var results []*brigodier.Suggestion
		seen := make(map[string]struct{})
		for _, c := range candidates {
			if _, exists := seen[c]; exists {
				continue
			}
			if strings.HasPrefix(strings.ToLower(c), currentWordPrefix) {
				seen[c] = struct{}{}
				results = append(results, &brigodier.Suggestion{
					Range: brigodier.StringRange{
						Start: wordStart,
						End:   len(builder.Input),
					},
					Text: c,
				})
			}
		}

		return brigodier.CreateSuggestion(builder.Input, results)
	}
}
