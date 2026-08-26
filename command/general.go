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

// HandleRoot displays StonyPerms version, stats, and quick-help.
func HandleRoot(mgr *manager.Manager, src command.Source) error {
	uCount, gCount, tCount, _ := mgr.Stats()

	lines := []string{
		Header("StonyPerms v1.0.0"),
		fmt.Sprintf("&7Running on &bGate Proxy&7. High performance LuckPerms-style permissions.&r"),
		fmt.Sprintf("&7Loaded: &e%d users&7, &e%d groups&7, &e%d tracks&7.&r", uCount, gCount, tCount),
		Separator(),
		"&eCommand Overview:&r",
		"  &b/sp user <user>&7 - Manage user permissions & parents",
		"  &b/sp group <group>&7 - Manage group permissions & inheritance",
		"  &b/sp track <track>&7 - Manage promotion tracks",
		"  &b/sp check <user> <perm>&7 - Evaluate permission with trace",
		"  &b/sp verbose <on|off>&7 - Toggle live permission logging",
		"  &b/sp creategroup <group> [weight]&7 - Create a group",
		"  &b/sp listgroups&7 / &b/sp listtracks&7 - List entities",
		"  &b/sp sync&7 - Reload data from storage disk",
		Separator(),
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
