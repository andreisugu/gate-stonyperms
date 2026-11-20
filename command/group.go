package command

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.minekube.com/gate/pkg/command"

	"github.com/andreisugu/gate-stonyperms/manager"
	"github.com/andreisugu/gate-stonyperms/model"
)

// HandleGroupDispatch routes /sp group <group> <subcommand> calls.
func HandleGroupDispatch(mgr *manager.Manager, src command.Source, args []string) error {
	if len(args) < 1 {
		return src.SendMessage(Msg(Prefix + "&cUsage: /sp group <group> <info|permission|parent|meta|setweight|setdisplayname|rename|clone|listmembers|clear>&r"))
	}

	groupName := strings.ToLower(args[0])
	subArgs := args[1:]

	// Command handlers mutate a copy, never the published record.
	group := mgr.GetGroupCopy(groupName)
	if group == nil {
		return src.SendMessage(Msg(Prefix+"&cGroup '&e%s&c' does not exist. Use &b/sp creategroup %s&c to create it.&r", groupName, groupName))
	}

	if len(subArgs) == 0 || strings.EqualFold(subArgs[0], "info") {
		return handleGroupInfo(mgr, src, group)
	}

	subCmd := strings.ToLower(subArgs[0])
	rest := subArgs[1:]

	switch subCmd {
	case "permission", "perm", "p":
		return handleGroupPermission(mgr, src, group, rest)
	case "parent", "parents":
		return handleGroupParent(mgr, src, group, rest)
	case "meta", "m":
		return handleGroupMeta(mgr, src, group, rest)
	case "setweight", "weight":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp group <group> setweight <weight>&r"))
		}
		weight, err := strconv.Atoi(rest[0])
		if err != nil {
			return src.SendMessage(Msg(Prefix + "&cWeight must be an integer.&r"))
		}
		group.Weight = weight
		_ = mgr.SaveGroup(group)
		mgr.LogAction(getActorName(src), "group", group.Name, fmt.Sprintf("Set weight to %d", weight))
		return src.SendMessage(Msg(Prefix+"&aSet weight for group &e%s&a to &e%d&a!&r", group.Name, weight))

	case "setdisplayname", "displayname":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp group <group> setdisplayname <name>&r"))
		}
		displayName := strings.Join(rest, " ")
		group.DisplayName = displayName
		_ = mgr.SaveGroup(group)
		mgr.LogAction(getActorName(src), "group", group.Name, fmt.Sprintf("Set display name to '%s'", displayName))
		return src.SendMessage(Msg(Prefix+"&aSet display name for group &e%s&a to '&r%s&a'!&r", group.Name, displayName))

	case "rename":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp group <group> rename <newName>&r"))
		}
		newName := strings.ToLower(rest[0])
		if group.Name == "default" {
			return src.SendMessage(Msg(Prefix + "&cCannot rename the default group.&r"))
		}
		if mgr.GetGroup(newName) != nil {
			return src.SendMessage(Msg(Prefix+"&cGroup '&e%s&c' already exists.&r", newName))
		}
		oldName := group.Name
		_ = mgr.DeleteGroup(oldName)
		group.Name = newName
		_ = mgr.SaveGroup(group)
		mgr.LogAction(getActorName(src), "group", newName, fmt.Sprintf("Renamed group from '%s' to '%s'", oldName, newName))
		return src.SendMessage(Msg(Prefix+"&aRenamed group &e%s&a to &e%s&a!&r", oldName, newName))

	case "clone":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp group <group> clone <newName>&r"))
		}
		newName := strings.ToLower(rest[0])
		if mgr.GetGroup(newName) != nil {
			return src.SendMessage(Msg(Prefix+"&cGroup '&e%s&c' already exists.&r", newName))
		}
		cloned := group.Clone(newName)
		_ = mgr.SaveGroup(cloned)
		mgr.LogAction(getActorName(src), "group", newName, fmt.Sprintf("Cloned group from '%s'", group.Name))
		return src.SendMessage(Msg(Prefix+"&aCloned group &e%s&a to &e%s&a!&r", group.Name, newName))

	case "listmembers":
		return handleGroupListMembers(mgr, src, group)

	case "showtracks":
		return handleGroupShowTracks(mgr, src, group)

	case "clear":
		group.Nodes = make([]model.Node, 0)
		group.Parents = make([]model.Inheritance, 0)
		group.Meta = make(map[string]string)
		group.Prefixes = make([]model.PriorityMeta, 0)
		group.Suffixes = make([]model.PriorityMeta, 0)
		_ = mgr.SaveGroup(group)
		mgr.LogAction(getActorName(src), "group", group.Name, "Cleared all group data")
		return src.SendMessage(Msg(Prefix+"&aCleared all data for group &e%s&a.&r", group.Name))

	default:
		return src.SendMessage(Msg(Prefix+"&cUnknown group subcommand: %s. Available: info, permission, parent, meta, setweight, setdisplayname, rename, clone, listmembers, clear.&r", subCmd))
	}
}

func handleGroupInfo(mgr *manager.Manager, src command.Source, g *model.Group) error {
	now := time.Now()
	g.CleanExpired(now)

	lines := []string{
		Header(fmt.Sprintf("Group Info: %s", g.Name)),
		fmt.Sprintf("&bDisplay Name:&r &f%s&r", g.GetDisplayName()),
		fmt.Sprintf("&bWeight:&r &e%d&r", g.Weight),
		Separator(),
		SubHeader(fmt.Sprintf("Parents (%d)", len(g.Parents))),
	}

	if len(g.Parents) == 0 {
		lines = append(lines, "  &7(none)&r")
	} else {
		for _, p := range g.Parents {
			exp := ""
			if p.Expiry > 0 {
				exp = fmt.Sprintf(" &e(expires in %s)&r", FormatDuration(time.Until(time.Unix(p.Expiry, 0))))
			}
			lines = append(lines, fmt.Sprintf("  &6• &e%s%s %s", p.Group, exp, FormatContexts(p.Contexts)))
		}
	}

	lines = append(lines, SubHeader(fmt.Sprintf("Direct Nodes (%d)", len(g.Nodes))))
	if len(g.Nodes) == 0 {
		lines = append(lines, "  &7(none)&r")
	} else {
		for _, n := range g.Nodes {
			valSign := "&a+"
			if !n.Value {
				valSign = "&c-"
			}
			exp := ""
			if n.Expiry > 0 {
				exp = fmt.Sprintf(" &e(expires in %s)&r", FormatDuration(time.Until(time.Unix(n.Expiry, 0))))
			}
			lines = append(lines, fmt.Sprintf("  &6• %s &f%s&r%s %s", valSign, n.Key, exp, FormatContexts(n.Contexts)))
		}
	}

	lines = append(lines, Separator())
	for _, line := range lines {
		_ = src.SendMessage(MsgPlain(line))
	}
	return nil
}

func handleGroupPermission(mgr *manager.Manager, src command.Source, g *model.Group, args []string) error {
	if len(args) < 1 {
		return src.SendMessage(Msg(Prefix + "&cUsage: /sp group <group> permission <set|unset|settemp|unsettemp|check|clear> [args...]&r"))
	}

	action := strings.ToLower(args[0])
	rest := args[1:]

	switch action {
	case "set":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp group <group> permission set <node> [true/false] [context...]&r"))
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
		g.AddNode(node)
		_ = mgr.SaveGroup(g)

		mgr.LogAction(getActorName(src), "group", g.Name, fmt.Sprintf("Set permission '%s' to %v %s", nodeKey, value, FormatContexts(contexts)))
		return src.SendMessage(Msg(Prefix+"&aSet &e%s&a to %s &afor group &e%s&a %s&r", nodeKey, FormatBool(value), g.Name, FormatContexts(contexts)))

	case "unset":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp group <group> permission unset <node> [context...]&r"))
		}
		nodeKey := rest[0]
		contexts, _ := ParseContexts(rest[1:])

		if !g.RemoveNode(nodeKey, contexts) {
			return src.SendMessage(Msg(Prefix+"&cGroup &e%s&c does not have direct node '&e%s&c'.&r", g.Name, nodeKey))
		}
		_ = mgr.SaveGroup(g)
		mgr.LogAction(getActorName(src), "group", g.Name, fmt.Sprintf("Unset permission '%s'", nodeKey))
		return src.SendMessage(Msg(Prefix+"&aUnset permission &e%s&a for group &e%s&a.&r", nodeKey, g.Name))

	case "settemp":
		if len(rest) < 2 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp group <group> permission settemp <node> <true/false> <duration> [context...]&r"))
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
		g.AddNode(node)
		_ = mgr.SaveGroup(g)

		mgr.LogAction(getActorName(src), "group", g.Name, fmt.Sprintf("Set temp permission '%s' to %v for %s", nodeKey, value, durationStr))
		return src.SendMessage(Msg(Prefix+"&aSet temporary permission &e%s&a to %s &afor group &e%s&a (duration: &e%s&a) %s&r", nodeKey, FormatBool(value), g.Name, FormatDuration(dur), FormatContexts(contexts)))

	case "unsettemp":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp group <group> permission unsettemp <node> [context...]&r"))
		}
		nodeKey := rest[0]
		contexts, _ := ParseContexts(rest[1:])
		if !g.RemoveNode(nodeKey, contexts) {
			return src.SendMessage(Msg(Prefix+"&cGroup &e%s&c does not have node '&e%s&c'.&r", g.Name, nodeKey))
		}
		_ = mgr.SaveGroup(g)
		mgr.LogAction(getActorName(src), "group", g.Name, fmt.Sprintf("Unset temp permission '%s'", nodeKey))
		return src.SendMessage(Msg(Prefix+"&aUnset temporary permission &e%s&a for group &e%s&a.&r", nodeKey, g.Name))

	case "check":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp group <group> permission check <node>&r"))
		}
		perm := rest[0]
		contexts, _ := ParseContexts(rest[1:])
		groups := make(map[string]*model.Group)
		for _, grp := range mgr.GetGroups() {
			groups[grp.Name] = grp
		}
		res := mgr.Engine().GroupPermissionValue(g, groups, perm, contexts)

		var valStr string
		switch res.Result {
		case model.True:
			valStr = "&a&lTRUE&r"
		case model.False:
			valStr = "&c&lFALSE&r"
		default:
			valStr = "&7&lUNDEFINED (false)&r"
		}

		lines := []string{
			Header("Group Permission Check"),
			fmt.Sprintf("&bGroup:&r &e%s&r", g.Name),
			fmt.Sprintf("&bPermission:&r &f%s&r", perm),
			fmt.Sprintf("&bContexts:&r %s", FormatContexts(contexts)),
			fmt.Sprintf("&bResult:&r %s", valStr),
			fmt.Sprintf("&bDecision Source:&r &d%s&r", res.Source),
			Separator(),
		}
		for _, l := range lines {
			_ = src.SendMessage(MsgPlain(l))
		}
		return nil

	case "clear":
		g.Nodes = make([]model.Node, 0)
		_ = mgr.SaveGroup(g)
		mgr.LogAction(getActorName(src), "group", g.Name, "Cleared all permissions")
		return src.SendMessage(Msg(Prefix+"&aCleared permissions for group &e%s&a.&r", g.Name))

	default:
		return src.SendMessage(Msg(Prefix+"&cUnknown permission action: %s. Available: set, unset, settemp, unsettemp, check, clear.&r", action))
	}
}

func handleGroupParent(mgr *manager.Manager, src command.Source, g *model.Group, args []string) error {
	if len(args) < 1 {
		return src.SendMessage(Msg(Prefix + "&cUsage: /sp group <group> parent <info|add|set|remove|addtemp|clear> [args...]&r"))
	}

	action := strings.ToLower(args[0])
	rest := args[1:]

	switch action {
	case "info":
		lines := []string{
			Header(fmt.Sprintf("Parents: %s", g.Name)),
		}
		for _, p := range g.Parents {
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
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp group <group> parent add <parentGroup> [context...]&r"))
		}
		parentName := rest[0]
		if mgr.GetGroup(parentName) == nil {
			return src.SendMessage(Msg(Prefix+"&cGroup '&e%s&c' does not exist.&r", parentName))
		}
		contexts, _ := ParseContexts(rest[1:])

		inh := model.NewInheritance(parentName)
		inh.Contexts = contexts
		g.AddParent(inh)
		_ = mgr.SaveGroup(g)

		mgr.LogAction(getActorName(src), "group", g.Name, fmt.Sprintf("Added parent group '%s'", parentName))
		return src.SendMessage(Msg(Prefix+"&aAdded parent group &e%s&a to group &e%s&a!&r", parentName, g.Name))

	case "set":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp group <group> parent set <parentGroup> [context...]&r"))
		}
		parentName := rest[0]
		if mgr.GetGroup(parentName) == nil {
			return src.SendMessage(Msg(Prefix+"&cGroup '&e%s&c' does not exist.&r", parentName))
		}
		contexts, _ := ParseContexts(rest[1:])

		g.Parents = []model.Inheritance{{
			Group:    parentName,
			Contexts: contexts,
		}}
		_ = mgr.SaveGroup(g)

		mgr.LogAction(getActorName(src), "group", g.Name, fmt.Sprintf("Set sole parent group to '%s'", parentName))
		return src.SendMessage(Msg(Prefix+"&aSet sole parent group for &e%s&a to &e%s&a!&r", g.Name, parentName))

	case "remove":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp group <group> parent remove <parentGroup> [context...]&r"))
		}
		parentName := rest[0]
		contexts, _ := ParseContexts(rest[1:])
		if !g.RemoveParent(parentName, contexts) {
			return src.SendMessage(Msg(Prefix+"&cGroup &e%s&c does not inherit from '&e%s&c'.&r", g.Name, parentName))
		}
		_ = mgr.SaveGroup(g)
		mgr.LogAction(getActorName(src), "group", g.Name, fmt.Sprintf("Removed parent group '%s'", parentName))
		return src.SendMessage(Msg(Prefix+"&aRemoved parent group &e%s&a from &e%s&a.&r", parentName, g.Name))

	case "addtemp":
		if len(rest) < 2 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp group <group> parent addtemp <parentGroup> <duration> [context...]&r"))
		}
		parentName := rest[0]
		if mgr.GetGroup(parentName) == nil {
			return src.SendMessage(Msg(Prefix+"&cGroup '&e%s&c' does not exist.&r", parentName))
		}
		durStr := rest[1]
		dur, err := ParseDuration(durStr)
		if err != nil {
			return src.SendMessage(Msg(Prefix+"&cInvalid duration '%s': %v&r", durStr, err))
		}
		contexts, _ := ParseContexts(rest[2:])

		inh := model.NewTempInheritance(parentName, dur)
		inh.Contexts = contexts
		g.AddParent(inh)
		_ = mgr.SaveGroup(g)

		mgr.LogAction(getActorName(src), "group", g.Name, fmt.Sprintf("Added temp parent '%s' for %s", parentName, durStr))
		return src.SendMessage(Msg(Prefix+"&aAdded temporary parent &e%s&a to &e%s&a (duration: &e%s&a)!&r", parentName, g.Name, FormatDuration(dur)))

	case "clear":
		g.Parents = make([]model.Inheritance, 0)
		_ = mgr.SaveGroup(g)
		mgr.LogAction(getActorName(src), "group", g.Name, "Cleared all parent groups")
		return src.SendMessage(Msg(Prefix+"&aCleared all parent groups for &e%s&a.&r", g.Name))

	default:
		return src.SendMessage(Msg(Prefix + "&cUnknown parent action. Available: info, add, set, remove, addtemp, clear.&r"))
	}
}

func handleGroupMeta(mgr *manager.Manager, src command.Source, g *model.Group, args []string) error {
	if len(args) < 1 {
		return src.SendMessage(Msg(Prefix + "&cUsage: /sp group <group> meta <info|set|unset|setprefix|setsuffix|removeprefix|removesuffix|clear> [args...]&r"))
	}

	action := strings.ToLower(args[0])
	rest := args[1:]

	switch action {
	case "info":
		lines := []string{
			Header(fmt.Sprintf("Meta: %s", g.Name)),
			SubHeader(fmt.Sprintf("Custom Keys (%d)", len(g.Meta))),
		}
		for k, v := range g.Meta {
			lines = append(lines, fmt.Sprintf("  &6• &e%s&7: &f%s&r", k, v))
		}
		lines = append(lines, SubHeader(fmt.Sprintf("Prefixes (%d)", len(g.Prefixes))))
		for _, p := range g.Prefixes {
			lines = append(lines, fmt.Sprintf("  &6• &b(priority %d)&r &f%s", p.Priority, p.Value))
		}
		lines = append(lines, SubHeader(fmt.Sprintf("Suffixes (%d)", len(g.Suffixes))))
		for _, s := range g.Suffixes {
			lines = append(lines, fmt.Sprintf("  &6• &b(priority %d)&r &f%s", s.Priority, s.Value))
		}
		lines = append(lines, Separator())
		for _, l := range lines {
			_ = src.SendMessage(MsgPlain(l))
		}
		return nil

	case "set":
		if len(rest) < 2 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp group <group> meta set <key> <value>&r"))
		}
		k := rest[0]
		v := strings.Join(rest[1:], " ")
		g.SetMeta(k, v)
		_ = mgr.SaveGroup(g)
		mgr.LogAction(getActorName(src), "group", g.Name, fmt.Sprintf("Set meta '%s' = '%s'", k, v))
		return src.SendMessage(Msg(Prefix+"&aSet meta &e%s&a to '&f%s&a' for group &e%s&a.&r", k, v, g.Name))

	case "unset":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp group <group> meta unset <key>&r"))
		}
		k := rest[0]
		if !g.UnsetMeta(k) {
			return src.SendMessage(Msg(Prefix+"&cMeta key '&e%s&c' is not set for &e%s&c.&r", k, g.Name))
		}
		_ = mgr.SaveGroup(g)
		mgr.LogAction(getActorName(src), "group", g.Name, fmt.Sprintf("Unset meta '%s'", k))
		return src.SendMessage(Msg(Prefix+"&aUnset meta key &e%s&a for group &e%s&a.&r", k, g.Name))

	case "setprefix":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp group <group> meta setprefix [priority] <prefix>&r"))
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
		g.AddPrefix(model.PriorityMeta{Priority: priority, Value: prefix})
		_ = mgr.SaveGroup(g)
		mgr.LogAction(getActorName(src), "group", g.Name, fmt.Sprintf("Set prefix to '%s' (priority %d)", prefix, priority))
		return src.SendMessage(Msg(Prefix+"&aSet prefix to '&f%s&a' (priority: &e%d&a) for group &e%s&a!&r", prefix, priority, g.Name))

	case "setsuffix":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp group <group> meta setsuffix [priority] <suffix>&r"))
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
		g.AddSuffix(model.PriorityMeta{Priority: priority, Value: suffix})
		_ = mgr.SaveGroup(g)
		mgr.LogAction(getActorName(src), "group", g.Name, fmt.Sprintf("Set suffix to '%s' (priority %d)", suffix, priority))
		return src.SendMessage(Msg(Prefix+"&aSet suffix to '&f%s&a' (priority: &e%d&a) for group &e%s&a!&r", suffix, priority, g.Name))

	case "removeprefix":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp group <group> meta removeprefix <priority>&r"))
		}
		priority, err := strconv.Atoi(rest[0])
		if err != nil {
			return src.SendMessage(Msg(Prefix + "&cPriority must be an integer.&r"))
		}
		if !g.RemovePrefix(priority) {
			return src.SendMessage(Msg(Prefix+"&cNo prefix with priority &e%d&c found for group &e%s&c.&r", priority, g.Name))
		}
		_ = mgr.SaveGroup(g)
		mgr.LogAction(getActorName(src), "group", g.Name, fmt.Sprintf("Removed prefix with priority %d", priority))
		return src.SendMessage(Msg(Prefix+"&aRemoved prefix with priority &e%d&a for group &e%s&a.&r", priority, g.Name))

	case "removesuffix":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp group <group> meta removesuffix <priority>&r"))
		}
		priority, err := strconv.Atoi(rest[0])
		if err != nil {
			return src.SendMessage(Msg(Prefix + "&cPriority must be an integer.&r"))
		}
		if !g.RemoveSuffix(priority) {
			return src.SendMessage(Msg(Prefix+"&cNo suffix with priority &e%d&c found for group &e%s&c.&r", priority, g.Name))
		}
		_ = mgr.SaveGroup(g)
		mgr.LogAction(getActorName(src), "group", g.Name, fmt.Sprintf("Removed suffix with priority %d", priority))
		return src.SendMessage(Msg(Prefix+"&aRemoved suffix with priority &e%d&a for group &e%s&a.&r", priority, g.Name))

	case "clear":
		g.Meta = make(map[string]string)
		g.Prefixes = make([]model.PriorityMeta, 0)
		g.Suffixes = make([]model.PriorityMeta, 0)
		_ = mgr.SaveGroup(g)
		mgr.LogAction(getActorName(src), "group", g.Name, "Cleared all meta, prefixes, and suffixes")
		return src.SendMessage(Msg(Prefix+"&aCleared all metadata for group &e%s&a.&r", g.Name))

	default:
		return src.SendMessage(Msg(Prefix + "&cUnknown meta action. Available: info, set, unset, setprefix, setsuffix, removeprefix, removesuffix, clear.&r"))
	}
}

func handleGroupListMembers(mgr *manager.Manager, src command.Source, g *model.Group) error {
	members := mgr.GetGroupMembers(g.Name)

	lines := []string{
		Header(fmt.Sprintf("Members of: %s (%d)", g.Name, len(members))),
	}

	if len(members) == 0 {
		lines = append(lines, "  &7(no users in this group)&r")
	} else {
		for _, u := range members {
			rel := "&7(parent)&r"
			if strings.EqualFold(u.PrimaryGroup, g.Name) {
				rel = "&e(primary)&r"
			}
			lines = append(lines, fmt.Sprintf("  &6• &e%s &7[%s] %s", u.Username, u.UUID, rel))
		}
	}

	lines = append(lines, Separator())
	for _, l := range lines {
		_ = src.SendMessage(MsgPlain(l))
	}
	return nil
}

func handleGroupShowTracks(mgr *manager.Manager, src command.Source, g *model.Group) error {
	tracks := mgr.GetTracks()
	lines := []string{
		Header(fmt.Sprintf("Tracks containing: %s", g.Name)),
	}

	found := false
	for _, t := range tracks {
		if pos, ok := t.IndexOf(g.Name); ok {
			found = true
			var parts []string
			for idx, name := range t.Groups {
				if idx == pos {
					parts = append(parts, fmt.Sprintf("&a&l[%s]&r", name))
				} else {
					parts = append(parts, fmt.Sprintf("&7%s&r", name))
				}
			}
			chain := strings.Join(parts, " &8-> ")
			lines = append(lines, fmt.Sprintf("  &6• &b%s&7: %s", t.Name, chain))
		}
	}

	if !found {
		lines = append(lines, "  &7(group is not part of any track)&r")
	}

	lines = append(lines, Separator())
	for _, l := range lines {
		_ = src.SendMessage(MsgPlain(l))
	}
	return nil
}
