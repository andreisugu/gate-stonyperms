package command

import (
	"strconv"
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

func hasPerm(src command.Source, required ...string) bool {
	if src.HasPermission("stonyperms.admin") || src.HasPermission("stonyperms.*") || src.HasPermission("*") {
		return true
	}
	for _, req := range required {
		if src.HasPermission(req) {
			return true
		}
	}
	return false
}

func checkPerm(src command.Source, required ...string) bool {
	if hasPerm(src, required...) {
		return true
	}
	permName := "stonyperms.admin"
	if len(required) > 0 {
		permName = required[0]
	}
	_ = src.SendMessage(Msg(Prefix+"&cYou do not have permission to execute this command (&e%s&c).", permName))
	return false
}

func buildCommandTree(alias string, p *proxy.Proxy, mgr *manager.Manager) brigodier.LiteralNodeBuilder {
	return brigodier.Literal(alias).
		Requires(command.Requires(func(ctx *command.RequiresContext) bool {
			return hasPerm(ctx.Source, "stonyperms.use", "stonyperms.info", "stonyperms.sync", "stonyperms.check",
				"stonyperms.verbose", "stonyperms.user", "stonyperms.group", "stonyperms.track", "stonyperms.log",
				"stonyperms.listgroups", "stonyperms.creategroup", "stonyperms.deletegroup",
				"stonyperms.listtracks", "stonyperms.createtrack", "stonyperms.deletetrack",
				"stonyperms.editor", "stonyperms.applyedits", "stonyperms.tree")
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
	case "help", "?":
		if !checkPerm(src, "stonyperms.use") {
			return nil
		}
		return HandleHelp(mgr, src, rest)
	case "info":
		if !checkPerm(src, "stonyperms.info") {
			return nil
		}
		return HandleInfo(mgr, src)
	case "sync", "reload", "reloadconfig":
		if !checkPerm(src, "stonyperms.sync") {
			return nil
		}
		return HandleSync(mgr, src)
	case "check":
		if !checkPerm(src, "stonyperms.check") {
			return nil
		}
		return HandleCheck(mgr, src, rest)
	case "verbose", "v":
		if !checkPerm(src, "stonyperms.verbose") {
			return nil
		}
		return HandleVerbose(mgr, src, rest)
	case "creategroup":
		if !checkPerm(src, "stonyperms.creategroup", "stonyperms.group") {
			return nil
		}
		return HandleCreateGroup(mgr, src, rest)
	case "deletegroup":
		if !checkPerm(src, "stonyperms.deletegroup", "stonyperms.group") {
			return nil
		}
		return HandleDeleteGroup(mgr, src, rest)
	case "listgroups", "groups":
		if !checkPerm(src, "stonyperms.listgroups", "stonyperms.group") {
			return nil
		}
		return HandleListGroups(mgr, src)
	case "createtrack":
		if !checkPerm(src, "stonyperms.createtrack", "stonyperms.track") {
			return nil
		}
		return HandleCreateTrack(mgr, src, rest)
	case "deletetrack":
		if !checkPerm(src, "stonyperms.deletetrack", "stonyperms.track") {
			return nil
		}
		return HandleDeleteTrack(mgr, src, rest)
	case "listtracks", "tracks":
		if !checkPerm(src, "stonyperms.listtracks", "stonyperms.track") {
			return nil
		}
		return HandleListTracks(mgr, src)
	case "user", "u":
		if !checkPerm(src, "stonyperms.user") {
			return nil
		}
		return HandleUserDispatch(mgr, src, rest)
	case "group", "g":
		if !checkPerm(src, "stonyperms.group") {
			return nil
		}
		return HandleGroupDispatch(mgr, src, rest)
	case "track", "t":
		if !checkPerm(src, "stonyperms.track") {
			return nil
		}
		return HandleTrackDispatch(mgr, src, rest)
	case "log", "logs", "history":
		if !checkPerm(src, "stonyperms.log") {
			return nil
		}
		return HandleLogDispatch(mgr, src, rest)
	case "editor", "ed":
		return HandleEditor(mgr, src, rest)
	case "applyedits", "applyedit":
		return HandleApplyEdits(mgr, src, rest)
	case "tree":
		return HandleTree(mgr, src, rest)
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
		input := builder.Input
		remaining := builder.Remaining
		args := strings.Fields(remaining)

		// Check if input ends with a space (meaning user finished a token and wants suggestions for next token)
		trailingSpace := strings.HasSuffix(input, " ") || strings.HasSuffix(remaining, " ")

		var wordStart int
		var currentWordPrefix string

		if len(args) == 0 {
			// User typed "/sp " -> ready for root subcommand at the cursor
			wordStart = len(input)
			currentWordPrefix = ""
		} else if trailingSpace {
			// User finished previous word and is starting the next word
			wordStart = len(input)
			currentWordPrefix = ""
		} else {
			// User is typing/completing current word
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

		// Helper to fetch server context suggestions
		serverContextSuggestions := func() []string {
			var list []string
			if p != nil {
				for _, s := range p.Servers() {
					list = append(list, "server="+s.ServerInfo().Name())
				}
			}
			if len(list) == 0 {
				list = append(list, "server=lobby", "server=survival", "world=hub")
			}
			return list
		}

		// Helper to fetch common permission nodes
		commonPermNodes := []string{
			"gate.command.server",
			"gate.command.send",
			"gate.command.glist",
			"gate.command.ping",
			"gate.command.alert",
			"gate.command.find",
			"gate.*",
			"stonyperms.admin",
			"stonyperms.use",
			"stonyperms.info",
			"stonyperms.sync",
			"stonyperms.check",
			"stonyperms.editor",
			"stonyperms.applyedits",
			"stonyperms.tree",
			"stonyperms.verbose",
			"stonyperms.user",
			"stonyperms.group",
			"stonyperms.track",
			"stonyperms.log",
			"stonyperms.creategroup",
			"stonyperms.deletegroup",
			"stonyperms.listgroups",
			"stonyperms.createtrack",
			"stonyperms.deletetrack",
			"stonyperms.listtracks",
			"stonyperms.*",
			"simplewhitelist.admin",
			"simplewhitelist.use",
			"simplewhitelist.add",
			"simplewhitelist.remove",
			"simplewhitelist.list",
			"simplewhitelist.toggle",
			"simplewhitelist.reload",
			"simplewhitelist.bypass",
			"bettertab.admin",
			"bettertab.reload",
			"bettertab.name",
			"bettertab.name.color",
			"bettertab.toggle",
			"bettertab.info",
			"bettertab.reset",
			"smartlimbo.admin",
			"smartlimbo.reload",
			"smartlimbo.status",
			"smartlimbo.reconnect",
			"smartlimbo.queue",
			"smartlimbo.priority",
			"smartlimbo.bypass",
			"discord4gate.admin",
			"discord4gate.reload",
			"discord4gate.status",
			"discord4gate.discord",
			"hybridforwarding.admin",
			"hybridforwarding.reload",
			"skinschanger.admin",
			"skinschanger.use",
			"skinschanger.set",
			"skinschanger.setfor",
			"skinschanger.url",
			"skinschanger.urlfor",
			"skinschanger.clear",
			"skinschanger.clearfor",
			"skinschanger.update",
			"skinschanger.info",
			"skinschanger.reload",
			"skinschanger.bypass.cooldown",
			"minecraft.command.teleport",
			"minecraft.command.gamemode",
			"*",
		}

		durations := []string{"10m", "30m", "1h", "6h", "12h", "1d", "3d", "7d", "14d", "30d", "90d", "1y"}
		priorities := []string{"10", "25", "50", "100", "500", "1000"}

		switch argCount {
		case 1:
			candidates = []string{
				"help", "info", "sync", "check", "verbose", "editor", "applyedits", "tree",
				"creategroup", "deletegroup", "listgroups", "createtrack", "deletetrack", "listtracks",
				"user", "group", "track", "log",
			}

		case 2:
			root := strings.ToLower(args[0])
			switch root {
			case "help", "?":
				candidates = []string{"1", "2", "3", "4"}
			case "user", "u", "check":
				if p != nil {
					for _, pl := range p.Players() {
						candidates = append(candidates, pl.Username())
					}
				}
				for _, u := range mgr.GetGroupMembers("default") {
					candidates = append(candidates, u.Username)
				}
				for _, u := range mgr.GetGroupMembers("admin") {
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
				candidates = []string{"on", "off", "record", "paste", "upload"}
			case "tree":
				candidates = []string{".", "gate", "stonyperms", "bettertab", "simplewhitelist", "smartlimbo", "skinschanger", "discord4gate"}
			case "log", "logs":
				candidates = []string{"recent", "search", "userhistory", "grouphistory", "trackhistory"}
			}

		case 3:
			root := strings.ToLower(args[0])
			switch root {
			case "user", "u":
				candidates = []string{"info", "permission", "parent", "meta", "promote", "demote", "showtracks", "editor", "clone", "clear"}
			case "group", "g":
				candidates = []string{"info", "setweight", "setdisplayname", "permission", "parent", "meta", "rename", "clone", "listmembers", "showtracks", "editor", "clear"}
			case "track", "t":
				candidates = []string{"info", "append", "insert", "remove", "rename", "clear"}
			case "check":
				candidates = commonPermNodes
			case "creategroup":
				candidates = priorities
			case "log", "logs":
				sub := strings.ToLower(args[1])
				switch sub {
				case "recent":
					candidates = []string{"1", "2", "3", "4", "5"}
				case "userhistory", "user":
					if p != nil {
						for _, pl := range p.Players() {
							candidates = append(candidates, pl.Username())
						}
					}
					for _, u := range mgr.GetGroupMembers("default") {
						candidates = append(candidates, u.Username)
					}
				case "grouphistory", "group":
					for _, g := range mgr.GetGroups() {
						candidates = append(candidates, g.Name)
					}
				case "trackhistory", "track":
					for _, t := range mgr.GetTracks() {
						candidates = append(candidates, t.Name)
					}
				}
			}

		case 4:
			root := strings.ToLower(args[0])
			sub := strings.ToLower(args[2])
			target := args[1]

			if root == "user" || root == "u" {
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
				case "clone":
					if p != nil {
						for _, pl := range p.Players() {
							if !strings.EqualFold(pl.Username(), target) {
								candidates = append(candidates, pl.Username())
							}
						}
					}
				}
			} else if root == "group" || root == "g" {
				switch sub {
				case "permission", "perm", "p":
					candidates = []string{"set", "unset", "settemp", "unsettemp", "check", "clear"}
				case "parent", "parents":
					candidates = []string{"info", "add", "set", "remove", "addtemp", "clear"}
				case "meta", "m":
					candidates = []string{"info", "set", "unset", "setprefix", "setsuffix", "removeprefix", "removesuffix", "clear"}
				case "setweight", "weight":
					candidates = priorities
				}
			} else if root == "track" || root == "t" {
				track := mgr.GetTrack(target)
				switch sub {
				case "append", "insert":
					for _, g := range mgr.GetGroups() {
						if track == nil || !containsStr(track.Groups, g.Name) {
							candidates = append(candidates, g.Name)
						}
					}
				case "remove":
					if track != nil {
						candidates = append(candidates, track.Groups...)
					}
				}
			} else if root == "check" {
				candidates = serverContextSuggestions()
			} else if root == "log" || root == "logs" {
				candidates = []string{"1", "2", "3", "4", "5"}
			}

		case 5:
			root := strings.ToLower(args[0])
			sub := strings.ToLower(args[2])
			action := strings.ToLower(args[3])
			target := args[1]

			if root == "user" || root == "u" {
				u := mgr.GetUserByName(target)
				if sub == "permission" || sub == "perm" || sub == "p" {
					switch action {
					case "set", "settemp", "check":
						candidates = commonPermNodes
					case "unset", "unsettemp":
						if u != nil {
							for _, n := range u.Nodes {
								candidates = append(candidates, n.Key)
							}
						}
						if len(candidates) == 0 {
							candidates = commonPermNodes
						}
					case "clear":
						candidates = serverContextSuggestions()
					}
				} else if sub == "parent" || sub == "parents" {
					switch action {
					case "add", "set", "switchprimarygroup", "addtemp":
						for _, g := range mgr.GetGroups() {
							candidates = append(candidates, g.Name)
						}
					case "remove":
						if u != nil {
							for _, p := range u.Parents {
								candidates = append(candidates, p.Group)
							}
						}
						for _, g := range mgr.GetGroups() {
							candidates = append(candidates, g.Name)
						}
					case "clear":
						candidates = serverContextSuggestions()
					}
				} else if sub == "meta" || sub == "m" {
					switch action {
					case "set":
						candidates = []string{"prefix", "suffix", "primary-group", "rank-color", "custom-tag"}
					case "unset":
						if u != nil {
							for k := range u.Meta {
								candidates = append(candidates, k)
							}
						}
					case "setprefix", "setsuffix":
						candidates = priorities
					case "removeprefix":
						if u != nil {
							for _, p := range u.Prefixes {
								candidates = append(candidates, strconv.Itoa(p.Priority))
							}
						}
						candidates = append(candidates, priorities...)
					case "removesuffix":
						if u != nil {
							for _, s := range u.Suffixes {
								candidates = append(candidates, strconv.Itoa(s.Priority))
							}
						}
						candidates = append(candidates, priorities...)
					}
				}
			} else if root == "group" || root == "g" {
				grp := mgr.GetGroup(target)
				if sub == "permission" || sub == "perm" || sub == "p" {
					switch action {
					case "set", "settemp", "check":
						candidates = commonPermNodes
					case "unset", "unsettemp":
						if grp != nil {
							for _, n := range grp.Nodes {
								candidates = append(candidates, n.Key)
							}
						}
						if len(candidates) == 0 {
							candidates = commonPermNodes
						}
					case "clear":
						candidates = serverContextSuggestions()
					}
				} else if sub == "parent" || sub == "parents" {
					switch action {
					case "add", "set", "addtemp":
						for _, g := range mgr.GetGroups() {
							if !strings.EqualFold(g.Name, target) {
								candidates = append(candidates, g.Name)
							}
						}
					case "remove":
						if grp != nil {
							for _, p := range grp.Parents {
								candidates = append(candidates, p.Group)
							}
						}
					}
				} else if sub == "meta" || sub == "m" {
					switch action {
					case "set":
						candidates = []string{"prefix", "suffix", "display-name", "color"}
					case "unset":
						if grp != nil {
							for k := range grp.Meta {
								candidates = append(candidates, k)
							}
						}
					case "setprefix", "setsuffix":
						candidates = priorities
					case "removeprefix":
						if grp != nil {
							for _, p := range grp.Prefixes {
								candidates = append(candidates, strconv.Itoa(p.Priority))
							}
						}
						candidates = append(candidates, priorities...)
					case "removesuffix":
						if grp != nil {
							for _, s := range grp.Suffixes {
								candidates = append(candidates, strconv.Itoa(s.Priority))
							}
						}
						candidates = append(candidates, priorities...)
					}
				}
			} else if root == "track" || root == "t" {
				if sub == "insert" {
					candidates = []string{"1", "2", "3", "4", "5"}
				}
			}

		case 6:
			root := strings.ToLower(args[0])
			sub := strings.ToLower(args[2])
			action := strings.ToLower(args[3])

			if (root == "user" || root == "u" || root == "group" || root == "g") && (sub == "permission" || sub == "perm" || sub == "p") {
				if action == "set" || action == "settemp" {
					candidates = []string{"true", "false"}
				} else if action == "unset" || action == "unsettemp" {
					candidates = serverContextSuggestions()
				}
			} else if (root == "user" || root == "u" || root == "group" || root == "g") && (sub == "parent" || sub == "parents") {
				if action == "addtemp" {
					candidates = durations
				} else if action == "add" || action == "set" || action == "remove" {
					candidates = serverContextSuggestions()
				}
			} else if (root == "user" || root == "u" || root == "group" || root == "g") && (sub == "meta" || sub == "m") {
				if action == "setprefix" {
					candidates = []string{"\"&7[Member] \"", "\"&a[VIP] \"", "\"&9[Mod] \"", "\"&c[Admin] \""}
				} else if action == "setsuffix" {
					candidates = []string{"\" &7[PRO]\"", "\" &e★\"", "\" &b[MVP]\""}
				}
			}

		case 7:
			root := strings.ToLower(args[0])
			sub := strings.ToLower(args[2])
			action := strings.ToLower(args[3])

			if (root == "user" || root == "u" || root == "group" || root == "g") && (sub == "permission" || sub == "perm" || sub == "p") {
				if action == "settemp" {
					candidates = durations
				} else if action == "set" {
					candidates = serverContextSuggestions()
				}
			} else if (root == "user" || root == "u" || root == "group" || root == "g") && (sub == "parent" || sub == "parents") {
				if action == "addtemp" {
					candidates = serverContextSuggestions()
				}
			}

		case 8:
			root := strings.ToLower(args[0])
			sub := strings.ToLower(args[2])
			action := strings.ToLower(args[3])

			if (root == "user" || root == "u" || root == "group" || root == "g") && (sub == "permission" || sub == "perm" || sub == "p") {
				if action == "settemp" {
					candidates = serverContextSuggestions()
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

func containsStr(slice []string, val string) bool {
	for _, s := range slice {
		if strings.EqualFold(s, val) {
			return true
		}
	}
	return false
}
