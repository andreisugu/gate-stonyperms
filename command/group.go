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
	case "editor", "ed":
		return openGroupEditor(mgr, src, group)
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
		group.MetaEntries = make([]model.MetaEntry, 0)
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
	return handleSubjectPermission(mgr, src, groupSubject{g: g, mgr: mgr}, args)
}

func handleGroupParent(mgr *manager.Manager, src command.Source, g *model.Group, args []string) error {
	return handleSubjectParent(mgr, src, groupSubject{g: g, mgr: mgr}, args)
}

func handleGroupMeta(mgr *manager.Manager, src command.Source, g *model.Group, args []string) error {
	return handleSubjectMeta(mgr, src, groupSubject{g: g, mgr: mgr}, args)
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
