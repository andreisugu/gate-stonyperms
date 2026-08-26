package command

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.minekube.com/gate/pkg/command"

	"github.com/andreisugu/gate-stonyperms/manager"
	"github.com/andreisugu/gate-stonyperms/model"
)

// HandleUserDispatch routes /sp user <user> <subcommand> calls.
func HandleUserDispatch(mgr *manager.Manager, src command.Source, args []string) error {
	if len(args) < 1 {
		return src.SendMessage(Msg(Prefix + "&cUsage: /sp user <username|uuid> <info|permission|parent|meta|promote|demote|clear|clone>&r"))
	}

	userQuery := args[0]
	subArgs := args[1:]

	// Find or resolve user
	user := mgr.GetUserByName(userQuery)
	if user == nil {
		// If query is a valid UUID, create new profile on the fly
		if parsedUUID, err := uuid.Parse(userQuery); err == nil {
			user = mgr.GetOrCreateUser(parsedUUID, "")
		} else {
			// Auto-provision offline user by offline UUID
			offlineUUID := uuid.NewMD5(uuid.NameSpaceDNS, []byte("OfflinePlayer:"+strings.ToLower(userQuery)))
			user = mgr.GetOrCreateUser(offlineUUID, userQuery)
		}
	}

	if len(subArgs) == 0 || strings.EqualFold(subArgs[0], "info") {
		return handleUserInfo(mgr, src, user)
	}

	subCmd := strings.ToLower(subArgs[0])
	rest := subArgs[1:]

	switch subCmd {
	case "permission", "perm", "p":
		return handleUserPermission(mgr, src, user, rest)
	case "parent", "parents", "group", "groups":
		return handleUserParent(mgr, src, user, rest)
	case "meta", "m":
		return handleUserMeta(mgr, src, user, rest)
	case "promote":
		return handleUserPromote(mgr, src, user, rest)
	case "demote":
		return handleUserDemote(mgr, src, user, rest)
	case "showtracks":
		return handleUserShowTracks(mgr, src, user)
	case "clear":
		user.ClearAll()
		_ = mgr.SaveUser(user)
		mgr.LogAction(getActorName(src), "user", user.Username, "Cleared all data")
		return src.SendMessage(Msg(Prefix+"&aCleared all permissions, parents, and metadata for user &e%s&a.&r", user.Username))
	case "clone":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp user <user> clone <target>&r"))
		}
		targetName := rest[0]
		targetUser := mgr.GetUserByName(targetName)
		if targetUser == nil {
			targetUUID := uuid.NewMD5(uuid.NameSpaceDNS, []byte("OfflinePlayer:"+strings.ToLower(targetName)))
			targetUser = mgr.GetOrCreateUser(targetUUID, targetName)
		}
		cloned := user.Clone(targetUser.UUID, targetUser.Username)
		_ = mgr.SaveUser(cloned)
		mgr.LogAction(getActorName(src), "user", targetUser.Username, fmt.Sprintf("Cloned data from %s", user.Username))
		return src.SendMessage(Msg(Prefix+"&aSuccessfully cloned data from &e%s&a to &e%s&a!&r", user.Username, targetUser.Username))
	default:
		return src.SendMessage(Msg(Prefix + "&cUnknown user subcommand '&e%s&c'. Available: info, permission, parent, meta, promote, demote, clear, clone.&r", subCmd))
	}
}

func handleUserInfo(mgr *manager.Manager, src command.Source, u *model.User) error {
	now := time.Now()
	u.CleanExpired(now)

	lines := []string{
		Header(fmt.Sprintf("User Info: %s", u.Username)),
		fmt.Sprintf("&bUUID:&r &7%s&r", u.UUID),
		fmt.Sprintf("&bPrimary Group:&r &e%s&r", u.PrimaryGroup),
		fmt.Sprintf("&bPrefix:&r &f%s&r", mgr.GetPrefix(u, nil)),
		fmt.Sprintf("&bSuffix:&r &f%s&r", mgr.GetSuffix(u, nil)),
		Separator(),
		SubHeader(fmt.Sprintf("Parents (%d)", len(u.Parents))),
	}

	if len(u.Parents) == 0 {
		lines = append(lines, "  &7(none - inherits primary group only)&r")
	} else {
		for _, p := range u.Parents {
			exp := ""
			if p.Expiry > 0 {
				exp = fmt.Sprintf(" &e(expires in %s)&r", FormatDuration(time.Until(time.Unix(p.Expiry, 0))))
			}
			ctx := FormatContexts(p.Contexts)
			lines = append(lines, fmt.Sprintf("  &6• &e%s&r%s %s", p.Group, exp, ctx))
		}
	}

	lines = append(lines, SubHeader(fmt.Sprintf("Direct Nodes (%d)", len(u.Nodes))))
	if len(u.Nodes) == 0 {
		lines = append(lines, "  &7(none)&r")
	} else {
		for _, n := range u.Nodes {
			valSign := "&a+"
			if !n.Value {
				valSign = "&c-"
			}
			exp := ""
			if n.Expiry > 0 {
				exp = fmt.Sprintf(" &e(expires in %s)&r", FormatDuration(time.Until(time.Unix(n.Expiry, 0))))
			}
			ctx := FormatContexts(n.Contexts)
			lines = append(lines, fmt.Sprintf("  &6• %s &f%s&r%s %s", valSign, n.Key, exp, ctx))
		}
	}

	lines = append(lines, Separator())
	for _, line := range lines {
		_ = src.SendMessage(MsgPlain(line))
	}
	return nil
}

func handleUserPermission(mgr *manager.Manager, src command.Source, u *model.User, args []string) error {
	if len(args) < 1 {
		return src.SendMessage(Msg(Prefix + "&cUsage: /sp user <user> permission <set|unset|settemp|unsettemp|check|clear> [args...]&r"))
	}

	action := strings.ToLower(args[0])
	rest := args[1:]

	switch action {
	case "set":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp user <user> permission set <node> [true/false] [context...]&r"))
		}
		nodeKey := rest[0]
		value := true
		ctxArgs := rest[1:]
		if len(rest) > 1 && (strings.EqualFold(rest[1], "true") || strings.EqualFold(rest[1], "false")) {
			value = strings.EqualFold(rest[1], "true")
			ctxArgs = rest[2:]
		}
		contexts, _ := ParseContexts(ctxArgs)

		node := model.NewNode(nodeKey, value)
		node.Contexts = contexts
		u.AddNode(node)
		_ = mgr.SaveUser(u)

		mgr.LogAction(getActorName(src), "user", u.Username, fmt.Sprintf("Set permission '%s' to %v %s", nodeKey, value, FormatContexts(contexts)))
		return src.SendMessage(Msg(Prefix+"&aSet &e%s&a to %s &afor &e%s&a %s&r", nodeKey, FormatBool(value), u.Username, FormatContexts(contexts)))

	case "unset":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp user <user> permission unset <node> [context...]&r"))
		}
		nodeKey := rest[0]
		contexts, _ := ParseContexts(rest[1:])

		if !u.RemoveNode(nodeKey, contexts) {
			return src.SendMessage(Msg(Prefix+"&cUser &e%s&c does not have direct node '&e%s&c'.&r", u.Username, nodeKey))
		}
		_ = mgr.SaveUser(u)
		mgr.LogAction(getActorName(src), "user", u.Username, fmt.Sprintf("Unset permission '%s'", nodeKey))
		return src.SendMessage(Msg(Prefix+"&aUnset permission &e%s&a for &e%s&a.&r", nodeKey, u.Username))

	case "settemp":
		if len(rest) < 2 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp user <user> permission settemp <node> <true/false> <duration> [context...]&r"))
		}
		nodeKey := rest[0]
		valIdx := 1
		value := true
		if strings.EqualFold(rest[1], "true") || strings.EqualFold(rest[1], "false") {
			value = strings.EqualFold(rest[1], "true")
			valIdx = 2
		}
		if len(rest) <= valIdx {
			return src.SendMessage(Msg(Prefix + "&cDuration required (e.g. 10m, 2h, 7d).&r"))
		}
		durationStr := rest[valIdx]
		dur, err := ParseDuration(durationStr)
		if err != nil {
			return src.SendMessage(Msg(Prefix+"&cInvalid duration '%s': %v&r", durationStr, err))
		}
		contexts, _ := ParseContexts(rest[valIdx+1:])

		node := model.NewTempNode(nodeKey, value, dur)
		node.Contexts = contexts
		u.AddNode(node)
		_ = mgr.SaveUser(u)

		mgr.LogAction(getActorName(src), "user", u.Username, fmt.Sprintf("Set temp permission '%s' to %v for %s", nodeKey, value, durationStr))
		return src.SendMessage(Msg(Prefix+"&aSet temporary permission &e%s&a to %s &afor &e%s&a (duration: &e%s&a) %s&r", nodeKey, FormatBool(value), u.Username, FormatDuration(dur), FormatContexts(contexts)))

	case "unsettemp":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp user <user> permission unsettemp <node> [context...]&r"))
		}
		nodeKey := rest[0]
		contexts, _ := ParseContexts(rest[1:])
		if !u.RemoveNode(nodeKey, contexts) {
			return src.SendMessage(Msg(Prefix+"&cUser &e%s&c does not have node '&e%s&c'.&r", u.Username, nodeKey))
		}
		_ = mgr.SaveUser(u)
		mgr.LogAction(getActorName(src), "user", u.Username, fmt.Sprintf("Unset temp permission '%s'", nodeKey))
		return src.SendMessage(Msg(Prefix+"&aUnset temporary permission &e%s&a for &e%s&a.&r", nodeKey, u.Username))

	case "check":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp user <user> permission check <node>&r"))
		}
		return HandleCheck(mgr, src, []string{u.Username, rest[0]})

	case "clear":
		contexts, _ := ParseContexts(rest)
		if len(contexts) > 0 {
			var remaining []model.Node
			for _, n := range u.Nodes {
				if !model.SameContexts(n.Contexts, contexts) {
					remaining = append(remaining, n)
				}
			}
			u.Nodes = remaining
		} else {
			u.Nodes = make([]model.Node, 0)
		}
		_ = mgr.SaveUser(u)
		mgr.LogAction(getActorName(src), "user", u.Username, "Cleared permissions")
		return src.SendMessage(Msg(Prefix+"&aCleared permissions for &e%s&a.&r", u.Username))

	default:
		return src.SendMessage(Msg(Prefix + "&cUnknown permission action: %s. Available: set, unset, settemp, unsettemp, check, clear.&r", action))
	}
}

func handleUserParent(mgr *manager.Manager, src command.Source, u *model.User, args []string) error {
	if len(args) < 1 {
		return src.SendMessage(Msg(Prefix + "&cUsage: /sp user <user> parent <info|add|set|remove|switchprimarygroup|addtemp|clear> [args...]&r"))
	}

	action := strings.ToLower(args[0])
	rest := args[1:]

	switch action {
	case "info":
		lines := []string{
			Header(fmt.Sprintf("Parents: %s", u.Username)),
			fmt.Sprintf("&bPrimary Group:&r &e%s&r", u.PrimaryGroup),
		}
		for _, p := range u.Parents {
			exp := ""
			if p.Expiry > 0 {
				exp = fmt.Sprintf(" &e(expires in %s)&r", FormatDuration(time.Until(time.Unix(p.Expiry, 0))))
			}
			lines = append(lines, fmt.Sprintf("  &6• &e%s%s %s", p.Group, exp, FormatContexts(p.Contexts)))
		}
		lines = append(lines, Separator())
		for _, l := range lines {
			_ = src.SendMessage(MsgPlain(l))
		}
		return nil

	case "add":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp user <user> parent add <group> [context...]&r"))
		}
		groupName := rest[0]
		if mgr.GetGroup(groupName) == nil {
			return src.SendMessage(Msg(Prefix+"&cGroup '&e%s&c' does not exist.&r", groupName))
		}
		contexts, _ := ParseContexts(rest[1:])

		inh := model.NewInheritance(groupName)
		inh.Contexts = contexts
		u.AddParent(inh)
		_ = mgr.SaveUser(u)

		mgr.LogAction(getActorName(src), "user", u.Username, fmt.Sprintf("Added parent group '%s'", groupName))
		return src.SendMessage(Msg(Prefix+"&aAdded parent group &e%s&a to &e%s&a!&r", groupName, u.Username))

	case "set":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp user <user> parent set <group> [context...]&r"))
		}
		groupName := rest[0]
		if mgr.GetGroup(groupName) == nil {
			return src.SendMessage(Msg(Prefix+"&cGroup '&e%s&c' does not exist.&r", groupName))
		}
		contexts, _ := ParseContexts(rest[1:])

		u.Parents = []model.Inheritance{{
			Group:    groupName,
			Contexts: contexts,
		}}
		u.PrimaryGroup = groupName
		_ = mgr.SaveUser(u)

		mgr.LogAction(getActorName(src), "user", u.Username, fmt.Sprintf("Set parent group to '%s'", groupName))
		return src.SendMessage(Msg(Prefix+"&aSet sole parent and primary group to &e%s&a for &e%s&a!&r", groupName, u.Username))

	case "remove":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp user <user> parent remove <group> [context...]&r"))
		}
		groupName := rest[0]
		contexts, _ := ParseContexts(rest[1:])
		if !u.RemoveParent(groupName, contexts) {
			return src.SendMessage(Msg(Prefix+"&cUser &e%s&c does not inherit from group '&e%s&c'.&r", u.Username, groupName))
		}
		if strings.EqualFold(u.PrimaryGroup, groupName) {
			u.PrimaryGroup = "default"
		}
		_ = mgr.SaveUser(u)
		mgr.LogAction(getActorName(src), "user", u.Username, fmt.Sprintf("Removed parent group '%s'", groupName))
		return src.SendMessage(Msg(Prefix+"&aRemoved parent group &e%s&a from &e%s&a.&r", groupName, u.Username))

	case "switchprimarygroup":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp user <user> parent switchprimarygroup <group>&r"))
		}
		groupName := strings.ToLower(rest[0])
		if mgr.GetGroup(groupName) == nil {
			return src.SendMessage(Msg(Prefix+"&cGroup '&e%s&c' does not exist.&r", groupName))
		}
		u.PrimaryGroup = groupName
		_ = mgr.SaveUser(u)
		mgr.LogAction(getActorName(src), "user", u.Username, fmt.Sprintf("Switched primary group to '%s'", groupName))
		return src.SendMessage(Msg(Prefix+"&aSwitched primary group for &e%s&a to &e%s&a!&r", u.Username, groupName))

	case "addtemp":
		if len(rest) < 2 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp user <user> parent addtemp <group> <duration> [context...]&r"))
		}
		groupName := rest[0]
		if mgr.GetGroup(groupName) == nil {
			return src.SendMessage(Msg(Prefix+"&cGroup '&e%s&c' does not exist.&r", groupName))
		}
		durStr := rest[1]
		dur, err := ParseDuration(durStr)
		if err != nil {
			return src.SendMessage(Msg(Prefix+"&cInvalid duration '%s': %v&r", durStr, err))
		}
		contexts, _ := ParseContexts(rest[2:])

		inh := model.NewTempInheritance(groupName, dur)
		inh.Contexts = contexts
		u.AddParent(inh)
		_ = mgr.SaveUser(u)

		mgr.LogAction(getActorName(src), "user", u.Username, fmt.Sprintf("Added temp parent '%s' for %s", groupName, durStr))
		return src.SendMessage(Msg(Prefix+"&aAdded temporary parent &e%s&a to &e%s&a (duration: &e%s&a)!&r", groupName, u.Username, FormatDuration(dur)))

	case "clear":
		u.Parents = make([]model.Inheritance, 0)
		u.PrimaryGroup = "default"
		_ = mgr.SaveUser(u)
		mgr.LogAction(getActorName(src), "user", u.Username, "Cleared all parent groups")
		return src.SendMessage(Msg(Prefix+"&aCleared all parent groups for &e%s&a.&r", u.Username))

	default:
		return src.SendMessage(Msg(Prefix + "&cUnknown parent action. Available: info, add, set, remove, switchprimarygroup, addtemp, clear.&r"))
	}
}

func handleUserMeta(mgr *manager.Manager, src command.Source, u *model.User, args []string) error {
	if len(args) < 1 {
		return src.SendMessage(Msg(Prefix + "&cUsage: /sp user <user> meta <info|set|unset|setprefix|setsuffix|removeprefix|removesuffix|clear> [args...]&r"))
	}

	action := strings.ToLower(args[0])
	rest := args[1:]

	switch action {
	case "info":
		lines := []string{
			Header(fmt.Sprintf("Meta: %s", u.Username)),
			fmt.Sprintf("&bPrefix:&r &f%s&r", mgr.GetPrefix(u, nil)),
			fmt.Sprintf("&bSuffix:&r &f%s&r", mgr.GetSuffix(u, nil)),
			Separator(),
			SubHeader(fmt.Sprintf("Custom Keys (%d)", len(u.Meta))),
		}
		for k, v := range u.Meta {
			lines = append(lines, fmt.Sprintf("  &6• &e%s&7: &f%s&r", k, v))
		}
		lines = append(lines, Separator())
		for _, l := range lines {
			_ = src.SendMessage(MsgPlain(l))
		}
		return nil

	case "set":
		if len(rest) < 2 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp user <user> meta set <key> <value>&r"))
		}
		k := rest[0]
		v := strings.Join(rest[1:], " ")
		u.SetMeta(k, v)
		_ = mgr.SaveUser(u)
		mgr.LogAction(getActorName(src), "user", u.Username, fmt.Sprintf("Set meta '%s' = '%s'", k, v))
		return src.SendMessage(Msg(Prefix+"&aSet meta &e%s&a to '&f%s&a' for &e%s&a.&r", k, v, u.Username))

	case "unset":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp user <user> meta unset <key>&r"))
		}
		k := rest[0]
		if !u.UnsetMeta(k) {
			return src.SendMessage(Msg(Prefix+"&cMeta key '&e%s&c' is not set for &e%s&c.&r", k, u.Username))
		}
		_ = mgr.SaveUser(u)
		mgr.LogAction(getActorName(src), "user", u.Username, fmt.Sprintf("Unset meta '%s'", k))
		return src.SendMessage(Msg(Prefix+"&aUnset meta key &e%s&a for &e%s&a.&r", k, u.Username))

	case "setprefix":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp user <user> meta setprefix [priority] <prefix>&r"))
		}
		priority := 100
		prefix := rest[0]
		if len(rest) > 1 {
			if p, err := strconv.Atoi(rest[0]); err == nil {
				priority = p
				prefix = strings.Join(rest[1:], " ")
			} else {
				prefix = strings.Join(rest, " ")
			}
		}
		u.AddPrefix(model.PriorityMeta{Priority: priority, Value: prefix})
		_ = mgr.SaveUser(u)
		mgr.LogAction(getActorName(src), "user", u.Username, fmt.Sprintf("Set prefix to '%s' (priority %d)", prefix, priority))
		return src.SendMessage(Msg(Prefix+"&aSet prefix to '&f%s&a' (priority: &e%d&a) for &e%s&a!&r", prefix, priority, u.Username))

	case "setsuffix":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp user <user> meta setsuffix [priority] <suffix>&r"))
		}
		priority := 100
		suffix := rest[0]
		if len(rest) > 1 {
			if p, err := strconv.Atoi(rest[0]); err == nil {
				priority = p
				suffix = strings.Join(rest[1:], " ")
			} else {
				suffix = strings.Join(rest, " ")
			}
		}
		u.AddSuffix(model.PriorityMeta{Priority: priority, Value: suffix})
		_ = mgr.SaveUser(u)
		mgr.LogAction(getActorName(src), "user", u.Username, fmt.Sprintf("Set suffix to '%s' (priority %d)", suffix, priority))
		return src.SendMessage(Msg(Prefix+"&aSet suffix to '&f%s&a' (priority: &e%d&a) for &e%s&a!&r", suffix, priority, u.Username))

	case "removeprefix":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp user <user> meta removeprefix <priority>&r"))
		}
		priority, err := strconv.Atoi(rest[0])
		if err != nil {
			return src.SendMessage(Msg(Prefix + "&cPriority must be an integer.&r"))
		}
		if !u.RemovePrefix(priority) {
			return src.SendMessage(Msg(Prefix+"&cNo prefix with priority &e%d&c found for &e%s&c.&r", priority, u.Username))
		}
		_ = mgr.SaveUser(u)
		mgr.LogAction(getActorName(src), "user", u.Username, fmt.Sprintf("Removed prefix with priority %d", priority))
		return src.SendMessage(Msg(Prefix+"&aRemoved prefix with priority &e%d&a for &e%s&a.&r", priority, u.Username))

	case "removesuffix":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp user <user> meta removesuffix <priority>&r"))
		}
		priority, err := strconv.Atoi(rest[0])
		if err != nil {
			return src.SendMessage(Msg(Prefix + "&cPriority must be an integer.&r"))
		}
		if !u.RemoveSuffix(priority) {
			return src.SendMessage(Msg(Prefix+"&cNo suffix with priority &e%d&c found for &e%s&c.&r", priority, u.Username))
		}
		_ = mgr.SaveUser(u)
		mgr.LogAction(getActorName(src), "user", u.Username, fmt.Sprintf("Removed suffix with priority %d", priority))
		return src.SendMessage(Msg(Prefix+"&aRemoved suffix with priority &e%d&a for &e%s&a.&r", priority, u.Username))

	case "clear":
		u.Meta = make(map[string]string)
		u.Prefixes = make([]model.PriorityMeta, 0)
		u.Suffixes = make([]model.PriorityMeta, 0)
		_ = mgr.SaveUser(u)
		mgr.LogAction(getActorName(src), "user", u.Username, "Cleared metadata, prefixes, and suffixes")
		return src.SendMessage(Msg(Prefix+"&aCleared all metadata for &e%s&a.&r", u.Username))

	default:
		return src.SendMessage(Msg(Prefix + "&cUnknown meta action. Available: info, set, unset, setprefix, setsuffix, removeprefix, removesuffix, clear.&r"))
	}
}

func handleUserPromote(mgr *manager.Manager, src command.Source, u *model.User, args []string) error {
	if len(args) < 1 {
		return src.SendMessage(Msg(Prefix + "&cUsage: /sp user <user> promote <track>&r"))
	}
	trackName := args[0]
	fromGroup, toGroup, err := mgr.PromoteUser(u, trackName, getActorName(src))
	if err != nil {
		return src.SendMessage(Msg(Prefix+"&cFailed to promote: %v&r", err))
	}
	return src.SendMessage(Msg(Prefix+"&aPromoted &e%s&a on track &b%s&a: &7%s &a-> &e&l%s&a!&r", u.Username, trackName, fromGroup, toGroup))
}

func handleUserDemote(mgr *manager.Manager, src command.Source, u *model.User, args []string) error {
	if len(args) < 1 {
		return src.SendMessage(Msg(Prefix + "&cUsage: /sp user <user> demote <track>&r"))
	}
	trackName := args[0]
	fromGroup, toGroup, err := mgr.DemoteUser(u, trackName, getActorName(src))
	if err != nil {
		return src.SendMessage(Msg(Prefix+"&cFailed to demote: %v&r", err))
	}
	return src.SendMessage(Msg(Prefix+"&aDemoted &e%s&a on track &b%s&a: &7%s &a-> &e&l%s&a.&r", u.Username, trackName, fromGroup, toGroup))
}

func handleUserShowTracks(mgr *manager.Manager, src command.Source, u *model.User) error {
	tracks := mgr.GetTracks()
	lines := []string{
		Header(fmt.Sprintf("Tracks for User: %s", u.Username)),
	}

	for _, t := range tracks {
		pos := -1
		for idx, gName := range t.Groups {
			if strings.EqualFold(u.PrimaryGroup, gName) {
				pos = idx
				break
			}
		}
		var parts []string
		for idx, gName := range t.Groups {
			if idx == pos {
				parts = append(parts, fmt.Sprintf("&a&l[%s]&r", gName))
			} else {
				parts = append(parts, fmt.Sprintf("&7%s&r", gName))
			}
		}
		chain := strings.Join(parts, " &8-> ")
		lines = append(lines, fmt.Sprintf("  &6• &b%s&7: %s", t.Name, chain))
	}

	lines = append(lines, Separator())
	for _, l := range lines {
		_ = src.SendMessage(MsgPlain(l))
	}
	return nil
}
