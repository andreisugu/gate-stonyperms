package command

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"go.minekube.com/gate/pkg/command"

	"github.com/andreisugu/gate-stonyperms/engine"
	"github.com/andreisugu/gate-stonyperms/manager"
	"github.com/andreisugu/gate-stonyperms/model"
)

// HandleRoot displays StonyPerms version, stats, and quick overview or specific help page.
func HandleRoot(mgr *manager.Manager, src command.Source) error {
	return HandleHelp(mgr, src, nil)
}

// HandleHelp displays paginated, comprehensive LuckPerms-style command documentation.
func HandleHelp(mgr *manager.Manager, src command.Source, args []string) error {
	page := 1
	if len(args) > 0 {
		if p, err := strconv.Atoi(args[0]); err == nil && p > 0 {
			page = p
		}
	}
	if page > 4 {
		page = 4
	}

	uCount, gCount, tCount, _ := mgr.Stats()

	var lines []string
	switch page {
	case 1:
		lines = []string{
			Header("StonyPerms Commands (Page 1/4 - General)"),
			fmt.Sprintf("&7Proxy:&r &bMinekube Gate&r  &7•&r  &7Loaded:&r &e%d users&7, &e%d groups&7, &e%d tracks&r", uCount, gCount, tCount),
			Separator(),
			"  &b/sp info&7 - Display system diagnostic statistics & storage health&r",
			"  &b/sp sync&7 - Hot-reload all permissions data from disk&r",
			"  &b/sp check <user> <perm> [context...]&7 - Evaluate permission with trace&r",
			"  &b/sp verbose <on|off|record> [filter]&7 - Stream live permission checks to chat&r",
			"  &b/sp creategroup <group> [weight] [displayName]&7 - Create a group&r",
			"  &b/sp deletegroup <group>&7 - Delete an existing group&r",
			"  &b/sp listgroups&7 - List all configured groups ordered by weight&r",
			"  &b/sp createtrack <track>&7 - Create a new promotion track&r",
			"  &b/sp deletetrack <track>&7 - Delete a promotion track&r",
			"  &b/sp listtracks&7 - List all configured promotion tracks&r",
			Separator(),
			"&7Type &e/sp help 2&7 for User Commands, &e/sp help 3&7 for Groups, &e/sp help 4&7 for Tracks/Logs.&r",
		}

	case 2:
		lines = []string{
			Header("StonyPerms Commands (Page 2/4 - User Management)"),
			"&eBase Syntax:&r &b/sp user <user> <subcommand>&r",
			Separator(),
			"  &b... info&7 - Display user UUID, primary group, parents, and nodes&r",
			"  &b... permission set <node> [true/false] [context...]&7 - Set permission node&r",
			"  &b... permission unset <node> [context...]&7 - Remove direct node&r",
			"  &b... permission settemp <node> <val> <duration> [context...]&7 - Temp perm&r",
			"  &b... permission unsettemp <node> [context...]&7 - Remove temp perm&r",
			"  &b... parent add <group> [context...]&7 - Add inherited parent group&r",
			"  &b... parent set <group> [context...]&7 - Set sole parent & primary group&r",
			"  &b... parent remove <group> [context...]&7 - Remove inherited parent group&r",
			"  &b... parent switchprimarygroup <group>&7 - Switch primary group&r",
			"  &b... parent addtemp <group> <duration> [context...]&7 - Add temp parent&r",
			"  &b... meta set <key> <value>&7 - Set custom metadata key-value&r",
			"  &b... meta setprefix [priority] <prefix>&7 - Set player chat/tab prefix&r",
			"  &b... meta setsuffix [priority] <suffix>&7 - Set player chat/tab suffix&r",
			"  &b... promote <track>&7 / &b... demote <track>&7 - Progress along track&r",
			"  &b... showtracks&7 - Show user position in all tracks&r",
			"  &b... clone <target>&7 - Clone user permissions & parents to another user&r",
			"  &b... clear&7 - Clear all permissions, parents, and metadata&r",
			Separator(),
			"&7Type &e/sp help 3&7 for Group Commands, &e/sp help 4&7 for Tracks/Logs.&r",
		}

	case 3:
		lines = []string{
			Header("StonyPerms Commands (Page 3/4 - Group Management)"),
			"&eBase Syntax:&r &b/sp group <group> <subcommand>&r",
			Separator(),
			"  &b... info&7 - Display group details, display name, weight, and nodes&r",
			"  &b... setweight <weight>&7 - Set group weight priority&r",
			"  &b... setdisplayname <name>&7 - Set colored display name&r",
			"  &b... permission set <node> [true/false] [context...]&7 - Set group perm&r",
			"  &b... permission unset <node> [context...]&7 - Remove group perm&r",
			"  &b... permission settemp <node> <val> <duration> [context...]&7 - Temp perm&r",
			"  &b... parent add <parentGroup> [context...]&7 - Inherit from parent group&r",
			"  &b... parent remove <parentGroup> [context...]&7 - Remove parent group&r",
			"  &b... meta setprefix [priority] <prefix>&7 - Set group prefix&r",
			"  &b... meta setsuffix [priority] <suffix>&7 - Set group suffix&r",
			"  &b... rename <newName>&7 - Rename group&r",
			"  &b... clone <newName>&7 - Clone group to a new group&r",
			"  &b... listmembers&7 - List all users belonging to this group&r",
			"  &b... showtracks&7 - Show tracks containing this group&r",
			"  &b... clear&7 - Clear all nodes and parents on this group&r",
			Separator(),
			"&7Type &e/sp help 4&7 for Track & Log Commands.&r",
		}

	case 4:
		lines = []string{
			Header("StonyPerms Commands (Page 4/4 - Tracks & Audit Logs)"),
			"&eTrack Commands:&r &b/sp track <track> <subcommand>&r",
			"  &b... info&7 - View progression sequence of groups in track&r",
			"  &b... append <group>&7 - Append a group to the end of the track&r",
			"  &b... insert <group> <pos>&7 - Insert a group at position (1-based)&r",
			"  &b... remove <group>&7 - Remove a group from the track&r",
			"  &b... rename <newName>&7 - Rename the track&r",
			"  &b... clear&7 - Clear all groups from the track&r",
			Separator(),
			"&eAudit Log Commands:&r &b/sp log <subcommand>&r",
			"  &b/sp log recent [page]&7 - View recent administrative actions&r",
			"  &b/sp log search <query> [page]&7 - Search audit action logs&r",
			"  &b/sp log userhistory <user> [page]&7 - View changes for specific user&r",
			"  &b/sp log grouphistory <group> [page]&7 - View changes for specific group&r",
			"  &b/sp log trackhistory <track> [page]&7 - View changes for specific track&r",
			Separator(),
		}
	}

	for _, line := range lines {
		_ = src.SendMessage(MsgPlain(line))
	}
	return nil
}

// HandleInfo displays detailed system diagnostic information.
func HandleInfo(mgr *manager.Manager, src command.Source) error {
	uCount, gCount, tCount, lCount := mgr.Stats()

	lines := []string{
		Header("StonyPerms System Info"),
		fmt.Sprintf("&bStorage Type:&r &fJSON Flat-File&r"),
		fmt.Sprintf("&bStorage Status:&r &aConnected & Healthy&r"),
		fmt.Sprintf("&bActive Users (Cached):&r &e%d&r", uCount),
		fmt.Sprintf("&bLoaded Groups:&r &e%d&r", gCount),
		fmt.Sprintf("&bLoaded Tracks:&r &e%d&r", tCount),
		fmt.Sprintf("&bAudit Log Entries:&r &e%d&r", lCount),
		fmt.Sprintf("&bVerbose Logging:&r %s&r", FormatBool(mgr.Engine().IsVerboseActive())),
		Separator(),
	}

	for _, line := range lines {
		_ = src.SendMessage(MsgPlain(line))
	}
	return nil
}

// HandleSync reloads data from disk.
func HandleSync(mgr *manager.Manager, src command.Source) error {
	_ = src.SendMessage(Msg(Prefix + "&7Reloading permissions from storage...&r"))
	if err := mgr.Reload(); err != nil {
		return src.SendMessage(Msg(Prefix + "&cFailed to reload data: %v&r", err))
	}
	uCount, gCount, tCount, _ := mgr.Stats()
	mgr.LogAction(getActorName(src), "general", "system", "Reloaded storage data")
	return src.SendMessage(Msg(Prefix+"&aSuccessfully reloaded! &7(&e%d&7 users, &e%d&7 groups, &e%d&7 tracks)&r", uCount, gCount, tCount))
}

// HandleCheck evaluates a permission for a user and displays the decision trace.
func HandleCheck(mgr *manager.Manager, src command.Source, args []string) error {
	if len(args) < 2 {
		return src.SendMessage(Msg(Prefix + "&cUsage: /sp check <user> <permission> [context...]&r"))
	}

	userName := args[0]
	perm := args[1]
	contexts, _ := ParseContexts(args[2:])

	user := mgr.GetUserByName(userName)
	if user == nil {
		return src.SendMessage(Msg(Prefix + "&cUser '&e%s&c' not found in cache or storage.&r", userName))
	}

	groups := make(map[string]*model.Group)
	for _, g := range mgr.GetGroups() {
		groups[g.Name] = g
	}

	res := mgr.Engine().Evaluate(user, groups, perm, contexts)

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
		Header("Permission Check"),
		fmt.Sprintf("&bTarget User:&r &e%s&r &8(%s)&r", user.Username, user.UUID),
		fmt.Sprintf("&bPermission:&r &f%s&r", perm),
		fmt.Sprintf("&bContexts:&r %s", FormatContexts(contexts)),
		fmt.Sprintf("&bResult:&r %s", valStr),
		fmt.Sprintf("&bDecision Source:&r &d%s&r", res.Source),
		Separator(),
	}

	for _, line := range lines {
		_ = src.SendMessage(MsgPlain(line))
	}
	return nil
}

// HandleVerbose toggles or filters real-time permission check logging.
func HandleVerbose(mgr *manager.Manager, src command.Source, args []string) error {
	if len(args) == 0 {
		status := "&cOFF&r"
		if mgr.Engine().IsVerboseActive() {
			status = "&aON&r"
		}
		return src.SendMessage(Msg(Prefix+"&bVerbose mode is currently %s&b. Use &e/sp verbose <on|off> [filter]&b.&r", status))
	}

	action := strings.ToLower(args[0])
	switch action {
	case "on", "enable", "record":
		filterTarget := ""
		filterQuery := ""
		if len(args) > 1 {
			filterQuery = args[1]
		}
		if len(args) > 2 {
			filterTarget = args[1]
			filterQuery = args[2]
		}

		mgr.Engine().SetVerbose(true, engine.VerboseFilter{
			Target: filterTarget,
			Query:  filterQuery,
		}, func(subject string, permission string, result model.Tristate, source string) {
			resColor := "&cFAIL"
			if result == model.True {
				resColor = "&aPASS"
			}
			msg := fmt.Sprintf("&7[SP:V] &e%s &7checked &b%s &7-> %s &8(%s)&r", subject, permission, resColor, source)
			_ = src.SendMessage(MsgPlain(msg))
		})

		return src.SendMessage(Msg(Prefix + "&aVerbose logging enabled! Permission checks will now stream to your chat.&r"))

	case "off", "disable":
		mgr.Engine().SetVerbose(false, engine.VerboseFilter{}, nil)
		return src.SendMessage(Msg(Prefix + "&cVerbose logging disabled.&r"))

	default:
		return src.SendMessage(Msg(Prefix + "&cUsage: /sp verbose <on|off> [filter]&r"))
	}
}

// HandleCreateGroup creates a new group.
func HandleCreateGroup(mgr *manager.Manager, src command.Source, args []string) error {
	if len(args) < 1 {
		return src.SendMessage(Msg(Prefix + "&cUsage: /sp creategroup <name> [weight] [displayName]&r"))
	}

	name := strings.ToLower(args[0])
	weight := 0
	displayName := ""

	if len(args) > 1 {
		if w, err := strconv.Atoi(args[1]); err == nil {
			weight = w
		} else {
			displayName = args[1]
		}
	}
	if len(args) > 2 {
		displayName = strings.Join(args[2:], " ")
	}

	g, err := mgr.CreateGroup(name, weight, displayName)
	if err != nil {
		return src.SendMessage(Msg(Prefix+"&cFailed to create group: %v&r", err))
	}

	mgr.LogAction(getActorName(src), "group", name, fmt.Sprintf("Created group '%s' (weight: %d)", name, weight))
	return src.SendMessage(Msg(Prefix+"&aSuccessfully created group &e%s&a (weight: &e%d&a)!&r", g.Name, g.Weight))
}

// HandleDeleteGroup deletes an existing group.
func HandleDeleteGroup(mgr *manager.Manager, src command.Source, args []string) error {
	if len(args) < 1 {
		return src.SendMessage(Msg(Prefix + "&cUsage: /sp deletegroup <name>&r"))
	}

	name := strings.ToLower(args[0])
	if err := mgr.DeleteGroup(name); err != nil {
		return src.SendMessage(Msg(Prefix+"&cFailed to delete group: %v&r", err))
	}

	mgr.LogAction(getActorName(src), "group", name, fmt.Sprintf("Deleted group '%s'", name))
	return src.SendMessage(Msg(Prefix+"&aSuccessfully deleted group &e%s&a!&r", name))
}

// HandleListGroups lists all groups sorted by weight.
func HandleListGroups(mgr *manager.Manager, src command.Source) error {
	groups := mgr.GetGroups()
	if len(groups) == 0 {
		return src.SendMessage(Msg(Prefix + "&7No groups configured.&r"))
	}

	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Weight > groups[j].Weight
	})

	lines := []string{
		Header(fmt.Sprintf("Groups (%d)", len(groups))),
	}

	for _, g := range groups {
		parents := make([]string, 0, len(g.Parents))
		for _, p := range g.Parents {
			parents = append(parents, p.Group)
		}
		parentStr := ""
		if len(parents) > 0 {
			parentStr = fmt.Sprintf(" &8[inherits: %s]&r", strings.Join(parents, ", "))
		}

		lines = append(lines, fmt.Sprintf("  &6• &e%s &7(weight: &b%d&7, nodes: &a%d&7)%s&r", g.Name, g.Weight, len(g.Nodes), parentStr))
	}

	lines = append(lines, Separator())
	for _, line := range lines {
		_ = src.SendMessage(MsgPlain(line))
	}
	return nil
}

// HandleCreateTrack creates a new promotion track.
func HandleCreateTrack(mgr *manager.Manager, src command.Source, args []string) error {
	if len(args) < 1 {
		return src.SendMessage(Msg(Prefix + "&cUsage: /sp createtrack <name>&r"))
	}

	name := strings.ToLower(args[0])
	t, err := mgr.CreateTrack(name)
	if err != nil {
		return src.SendMessage(Msg(Prefix+"&cFailed to create track: %v&r", err))
	}

	mgr.LogAction(getActorName(src), "track", name, fmt.Sprintf("Created track '%s'", name))
	return src.SendMessage(Msg(Prefix+"&aSuccessfully created track &e%s&a!&r", t.Name))
}

// HandleDeleteTrack deletes a promotion track.
func HandleDeleteTrack(mgr *manager.Manager, src command.Source, args []string) error {
	if len(args) < 1 {
		return src.SendMessage(Msg(Prefix + "&cUsage: /sp deletetrack <name>&r"))
	}

	name := strings.ToLower(args[0])
	if err := mgr.DeleteTrack(name); err != nil {
		return src.SendMessage(Msg(Prefix+"&cFailed to delete track: %v&r", err))
	}

	mgr.LogAction(getActorName(src), "track", name, fmt.Sprintf("Deleted track '%s'", name))
	return src.SendMessage(Msg(Prefix+"&aSuccessfully deleted track &e%s&a!&r", name))
}

// HandleListTracks lists all configured promotion tracks.
func HandleListTracks(mgr *manager.Manager, src command.Source) error {
	tracks := mgr.GetTracks()
	if len(tracks) == 0 {
		return src.SendMessage(Msg(Prefix + "&7No tracks configured.&r"))
	}

	lines := []string{
		Header(fmt.Sprintf("Tracks (%d)", len(tracks))),
	}

	for _, t := range tracks {
		chain := "&7empty&r"
		if len(t.Groups) > 0 {
			chain = "&e" + strings.Join(t.Groups, " &7-> &e") + "&r"
		}
		lines = append(lines, fmt.Sprintf("  &6• &b%s&7: %s", t.Name, chain))
	}

	lines = append(lines, Separator())
	for _, line := range lines {
		_ = src.SendMessage(MsgPlain(line))
	}
	return nil
}

func getActorName(src command.Source) string {
	if src == nil {
		return "Console"
	}
	if p, ok := src.(interface{ Username() string }); ok {
		return p.Username()
	}
	return "Console"
}
