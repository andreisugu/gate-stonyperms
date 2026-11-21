package command

import (
	"fmt"
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

	// Command handlers mutate a copy, never the published record.
	user := mgr.GetUserByNameCopy(userQuery)
	if user == nil {
		// If query is a valid UUID, create new profile on the fly
		if parsedUUID, err := uuid.Parse(userQuery); err == nil {
			user = mgr.GetOrCreateUser(parsedUUID, "")
		} else {
			// Auto-provision offline user by offline UUID
			offlineUUID := uuid.NewMD5(uuid.NameSpaceDNS, []byte("OfflinePlayer:"+strings.ToLower(userQuery)))
			user = mgr.GetOrCreateUser(offlineUUID, userQuery)
		}
		user = mgr.GetUserCopy(user.UUID)
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
	case "editor", "ed":
		return openUserEditor(mgr, src, user)
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
		targetUser := mgr.GetUserByNameCopy(targetName)
		if targetUser == nil {
			targetUUID := uuid.NewMD5(uuid.NameSpaceDNS, []byte("OfflinePlayer:"+strings.ToLower(targetName)))
			targetUser = mgr.GetOrCreateUser(targetUUID, targetName)
			targetUser = mgr.GetUserCopy(targetUser.UUID)
		}
		cloned := user.Clone(targetUser.UUID, targetUser.Username)
		_ = mgr.SaveUser(cloned)
		mgr.LogAction(getActorName(src), "user", targetUser.Username, fmt.Sprintf("Cloned data from %s", user.Username))
		return src.SendMessage(Msg(Prefix+"&aSuccessfully cloned data from &e%s&a to &e%s&a!&r", user.Username, targetUser.Username))
	default:
		return src.SendMessage(Msg(Prefix+"&cUnknown user subcommand '&e%s&c'. Available: info, permission, parent, meta, promote, demote, editor, clear, clone.&r", subCmd))
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
	return handleSubjectPermission(mgr, src, userSubject{u: u, mgr: mgr}, args)
}

func handleUserParent(mgr *manager.Manager, src command.Source, u *model.User, args []string) error {
	return handleSubjectParent(mgr, src, userSubject{u: u, mgr: mgr}, args)
}

func handleUserMeta(mgr *manager.Manager, src command.Source, u *model.User, args []string) error {
	return handleSubjectMeta(mgr, src, userSubject{u: u, mgr: mgr}, args)
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
