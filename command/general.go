package command

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	c "go.minekube.com/common/minecraft/component"
	"go.minekube.com/gate/pkg/command"

	"github.com/andreisugu/gate-stonyperms/engine"
	"github.com/andreisugu/gate-stonyperms/manager"
	"github.com/andreisugu/gate-stonyperms/model"
)

// HandleRoot displays StonyPerms version, stats, and quick overview or specific help page.
func HandleRoot(mgr *manager.Manager, src command.Source) error {
	return HandleHelp(mgr, src, nil)
}

// HandleHelp displays paginated, comprehensive LuckPerms-style command documentation with clickable commands.
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

	var components []c.Component

	switch page {
	case 1:
		components = append(components,
			MsgPlain(Header("StonyPerms Commands (Page 1/4 - General)")),
			MsgPlain(fmt.Sprintf("&7Proxy:&r &bMinekube Gate&r  &7•&r  &7Loaded:&r &e%d users&7, &e%d groups&7, &e%d tracks&r", uCount, gCount, tCount)),
			MsgPlain(Separator()),
			ClickableRunCmd("&b/sp info&r", "/sp info", "Display system diagnostic statistics & storage health"),
			ClickableRunCmd("&b/sp sync&r", "/sp sync", "Hot-reload all permissions data from disk"),
			ClickableCmd("&b/sp check <user> <perm> [context...]&r", "/sp check ", "Evaluate permission with trace"),
			ClickableRunCmd("&b/sp editor [filter]&r", "/sp editor", "Open the interactive StonyPermsWeb editor"),
			ClickableCmd("&b/sp applyedits <code>&r", "/sp applyedits ", "Apply saved edits from the web editor"),
			ClickableRunCmd("&b/sp tree [root]&r", "/sp tree", "Open the interactive Permission Tree viewer"),
			ClickableCmd("&b/sp verbose <on|off|record|paste> [filter]&r", "/sp verbose ", "Stream or record permission checks for web viewer"),
			ClickableCmd("&b/sp creategroup <group> [weight] [displayName]&r", "/sp creategroup ", "Create a new permission group"),
			ClickableCmd("&b/sp deletegroup <group>&r", "/sp deletegroup ", "Delete an existing group"),
			ClickableRunCmd("&b/sp listgroups&r", "/sp listgroups", "List all configured groups ordered by weight"),
			ClickableCmd("&b/sp createtrack <track>&r", "/sp createtrack ", "Create a new promotion track"),
			ClickableCmd("&b/sp deletetrack <track>&r", "/sp deletetrack ", "Delete a promotion track"),
			ClickableRunCmd("&b/sp listtracks&r", "/sp listtracks", "List all configured promotion tracks"),
			MsgPlain(Separator()),
			makeHelpNavFooter(1, 4),
		)

	case 2:
		components = append(components,
			MsgPlain(Header("StonyPerms Commands (Page 2/4 - Users)")),
			MsgPlain("&eBase Syntax:&r &b/sp user <user> <subcommand>&r  &7(Click to insert)&r"),
			MsgPlain(Separator()),
			ClickableCmd("&b/sp user <user> info&r", "/sp user ", "Display user UUID, primary group, parents, and nodes"),
			ClickableCmd("&b/sp user <user> permission set <node> [true/false] [context...]&r", "/sp user ", "Set permission node"),
			ClickableCmd("&b/sp user <user> permission unset <node> [context...]&r", "/sp user ", "Remove direct node"),
			ClickableCmd("&b/sp user <user> permission settemp <node> <val> <duration> [context...]&r", "/sp user ", "Set temporary permission"),
			ClickableCmd("&b/sp user <user> permission unsettemp <node> [context...]&r", "/sp user ", "Remove temporary permission"),
			ClickableCmd("&b/sp user <user> parent add <group> [context...]&r", "/sp user ", "Add inherited parent group"),
			ClickableCmd("&b/sp user <user> parent set <group> [context...]&r", "/sp user ", "Set sole parent & primary group"),
			ClickableCmd("&b/sp user <user> parent remove <group> [context...]&r", "/sp user ", "Remove inherited parent group"),
			ClickableCmd("&b/sp user <user> parent switchprimarygroup <group>&r", "/sp user ", "Switch primary group"),
			ClickableCmd("&b/sp user <user> parent addtemp <group> <duration> [context...]&r", "/sp user ", "Add temporary parent"),
			ClickableCmd("&b/sp user <user> meta set <key> <value>&r", "/sp user ", "Set custom metadata key-value"),
			ClickableCmd("&b/sp user <user> meta setprefix [priority] <prefix>&r", "/sp user ", "Set player chat/tab prefix"),
			ClickableCmd("&b/sp user <user> meta setsuffix [priority] <suffix>&r", "/sp user ", "Set player chat/tab suffix"),
			ClickableCmd("&b/sp user <user> promote <track>&r", "/sp user ", "Progress player up a track"),
			ClickableCmd("&b/sp user <user> demote <track>&r", "/sp user ", "Demote player down a track"),
			ClickableCmd("&b/sp user <user> showtracks&r", "/sp user ", "Show user position in all tracks"),
			ClickableCmd("&b/sp user <user> clone <target>&r", "/sp user ", "Clone user data to another user"),
			ClickableCmd("&b/sp user <user> clear&r", "/sp user ", "Clear all permissions, parents, and metadata"),
			MsgPlain(Separator()),
			makeHelpNavFooter(2, 4),
		)

	case 3:
		components = append(components,
			MsgPlain(Header("StonyPerms Commands (Page 3/4 - Groups)")),
			MsgPlain("&eBase Syntax:&r &b/sp group <group> <subcommand>&r  &7(Click to insert)&r"),
			MsgPlain(Separator()),
			ClickableCmd("&b/sp group <group> info&r", "/sp group ", "Display group details, display name, weight, and nodes"),
			ClickableCmd("&b/sp group <group> setweight <weight>&r", "/sp group ", "Set group weight priority"),
			ClickableCmd("&b/sp group <group> setdisplayname <name>&r", "/sp group ", "Set colored display name"),
			ClickableCmd("&b/sp group <group> permission set <node> [true/false] [context...]&r", "/sp group ", "Set group permission"),
			ClickableCmd("&b/sp group <group> permission unset <node> [context...]&r", "/sp group ", "Remove group permission"),
			ClickableCmd("&b/sp group <group> permission settemp <node> <val> <duration> [context...]&r", "/sp group ", "Set temporary group permission"),
			ClickableCmd("&b/sp group <group> parent add <parentGroup> [context...]&r", "/sp group ", "Inherit from parent group"),
			ClickableCmd("&b/sp group <group> parent remove <parentGroup> [context...]&r", "/sp group ", "Remove parent group"),
			ClickableCmd("&b/sp group <group> meta setprefix [priority] <prefix>&r", "/sp group ", "Set group prefix"),
			ClickableCmd("&b/sp group <group> meta setsuffix [priority] <suffix>&r", "/sp group ", "Set group suffix"),
			ClickableCmd("&b/sp group <group> rename <newName>&r", "/sp group ", "Rename group"),
			ClickableCmd("&b/sp group <group> clone <newName>&r", "/sp group ", "Clone group to a new group"),
			ClickableCmd("&b/sp group <group> listmembers&r", "/sp group ", "List all users in this group"),
			ClickableCmd("&b/sp group <group> showtracks&r", "/sp group ", "Show tracks containing this group"),
			ClickableCmd("&b/sp group <group> clear&r", "/sp group ", "Clear all nodes and parents on this group"),
			MsgPlain(Separator()),
			makeHelpNavFooter(3, 4),
		)

	case 4:
		components = append(components,
			MsgPlain(Header("StonyPerms Commands (Page 4/4 - Tracks/Logs)")),
			MsgPlain("&eTrack Commands:&r &b/sp track <track> <subcommand>&r"),
			ClickableCmd("&b/sp track <track> info&r", "/sp track ", "View progression sequence of groups in track"),
			ClickableCmd("&b/sp track <track> append <group>&r", "/sp track ", "Append a group to end of track"),
			ClickableCmd("&b/sp track <track> insert <group> <pos>&r", "/sp track ", "Insert a group at position (1-based)"),
			ClickableCmd("&b/sp track <track> remove <group>&r", "/sp track ", "Remove a group from track"),
			ClickableCmd("&b/sp track <track> rename <newName>&r", "/sp track ", "Rename the track"),
			ClickableCmd("&b/sp track <track> clear&r", "/sp track ", "Clear all groups from track"),
			MsgPlain(Separator()),
			MsgPlain("&eAudit Log Commands:&r &b/sp log <subcommand>&r"),
			ClickableRunCmd("&b/sp log recent [page]&r", "/sp log recent", "View recent administrative actions"),
			ClickableCmd("&b/sp log search <query> [page]&r", "/sp log search ", "Search audit action logs"),
			ClickableCmd("&b/sp log userhistory <user> [page]&r", "/sp log userhistory ", "View changes for specific user"),
			ClickableCmd("&b/sp log grouphistory <group> [page]&r", "/sp log grouphistory ", "View changes for specific group"),
			ClickableCmd("&b/sp log trackhistory <track> [page]&r", "/sp log trackhistory ", "View changes for specific track"),
			MsgPlain(Separator()),
			makeHelpNavFooter(4, 4),
		)
	}

	for _, comp := range components {
		_ = src.SendMessage(comp)
	}
	return nil
}

func makeHelpNavFooter(currentPage, totalPages int) c.Component {
	var extra []c.Component

	// Prev button
	if currentPage > 1 {
		prevBtn := PageButton("&8[&b◀ Prev&8]&r", fmt.Sprintf("/sp help %d", currentPage-1), fmt.Sprintf("&eClick to go to Page %d", currentPage-1))
		extra = append(extra, prevBtn, Msg("  "))
	} else {
		extra = append(extra, Msg("&8[◀ Prev]&r  "))
	}

	// Direct Page numbers: [1] [2] [3] [4]
	for p := 1; p <= totalPages; p++ {
		if p == currentPage {
			extra = append(extra, Msg(fmt.Sprintf("&e&l[%d]&r ", p)))
		} else {
			btn := PageButton(fmt.Sprintf("&b[%d]&r ", p), fmt.Sprintf("/sp help %d", p), fmt.Sprintf("&eClick to open Page %d", p))
			extra = append(extra, btn)
		}
	}

	// Next button
	if currentPage < totalPages {
		nextBtn := PageButton("&8[&bNext ▶&8]&r", fmt.Sprintf("/sp help %d", currentPage+1), fmt.Sprintf("&eClick to go to Page %d", currentPage+1))
		extra = append(extra, Msg(" "), nextBtn)
	} else {
		extra = append(extra, Msg(" &8[Next ▶]&r"))
	}

	return &c.Text{
		Content: "",
		Extra:   extra,
	}
}

// HandleInfo displays detailed system diagnostic information.
func HandleInfo(mgr *manager.Manager, src command.Source) error {
	uCount, gCount, tCount, lCount := mgr.Stats()

	lines := []string{
		Header("StonyPerms System Info"),
		"&bStorage Type:&r &fJSON Flat-File&r",
		"&bStorage Status:&r &aConnected & Healthy&r",
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
		return src.SendMessage(Msg(Prefix+"&cFailed to reload data: %v&r", err))
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
		return src.SendMessage(Msg(Prefix+"&cUser '&e%s&c' not found in cache or storage.&r", userName))
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
		} else if mgr.Engine().IsVerboseRecording() {
			status = "&aRECORDING&r"
		}
		return src.SendMessage(Msg(Prefix+"&bVerbose mode is currently %s&b. Use &e/sp verbose <on|off|record|paste> [filter]&b.&r", status))
	}

	parseVerboseFilter := func(rest []string) engine.VerboseFilter {
		var filter engine.VerboseFilter
		if len(rest) == 1 {
			filter.Query = rest[0]
		} else if len(rest) > 1 {
			filter.Target = rest[0]
			filter.Query = rest[1]
		}
		return filter
	}

	action := strings.ToLower(args[0])
	switch action {
	case "on", "enable":
		mgr.Engine().SetVerbose(true, parseVerboseFilter(args[1:]), func(subject string, permission string, result model.Tristate, source string) {
			resColor := "&cFAIL"
			if result == model.True {
				resColor = "&aPASS"
			}
			msg := fmt.Sprintf("&7[SP:V] &e%s &7checked &b%s &7-> %s &8(%s)&r", subject, permission, resColor, source)
			_ = src.SendMessage(MsgPlain(msg))
		})

		return src.SendMessage(Msg(Prefix + "&aVerbose logging enabled! Permission checks will now stream to your chat.&r"))

	case "record":
		mgr.Engine().StartVerboseRecording(getActorName(src), parseVerboseFilter(args[1:]))
		return src.SendMessage(Msg(Prefix + "&aVerbose recording started! Run &e/sp verbose paste&a to upload to the web viewer.&r"))

	case "paste", "upload":
		rec := mgr.Engine().StopVerboseRecording()
		created, err := mgr.CreateVerboseSession(rec, getActorName(src), "sp")
		if err != nil {
			return src.SendMessage(Msg(Prefix+"&cFailed to upload verbose log: %v&r", err))
		}
		if err := src.SendMessage(MsgPlain(Prefix + fmt.Sprintf("&aUploaded &e%d&a recorded permission checks:&r", created.Count))); err != nil {
			return err
		}
		return src.SendMessage(ClickableURL("&a&n"+created.URL+"&r", created.URL, "Open the recorded checks in Verbose Viewer"))

	case "off", "disable":
		mgr.Engine().SetVerbose(false, engine.VerboseFilter{}, nil)
		mgr.Engine().StopVerboseRecording()
		return src.SendMessage(Msg(Prefix + "&cVerbose logging and recording disabled.&r"))

	default:
		return src.SendMessage(Msg(Prefix + "&cUsage: /sp verbose <on|off|record|paste> [filter]&r"))
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
