package command

import (
	"fmt"
	"strconv"
	"strings"

	"go.minekube.com/gate/pkg/command"

	"github.com/andreisugu/gate-stonyperms/manager"
)

// HandleTrackDispatch routes /sp track <track> <subcommand> calls.
func HandleTrackDispatch(mgr *manager.Manager, src command.Source, args []string) error {
	if len(args) < 1 {
		return src.SendMessage(Msg(Prefix + "&cUsage: /sp track <track> <info|append|insert|remove|clear|rename|clone|editor>&r"))
	}

	trackName := strings.ToLower(args[0])
	subArgs := args[1:]

	// Command handlers mutate a copy, never the published record.
	track := mgr.GetTrackCopy(trackName)
	if track == nil {
		return src.SendMessage(Msg(Prefix+"&cTrack '&e%s&c' does not exist. Use &b/sp createtrack %s&c to create it.&r", trackName, trackName))
	}

	if len(subArgs) == 0 || strings.EqualFold(subArgs[0], "info") {
		chain := "&7(empty)&r"
		if len(track.Groups) > 0 {
			chain = "&e" + strings.Join(track.Groups, " &7-> &e") + "&r"
		}
		lines := []string{
			Header(fmt.Sprintf("Track Info: %s", track.Name)),
			fmt.Sprintf("&bPath:&r %s", chain),
			Separator(),
		}
		for _, l := range lines {
			_ = src.SendMessage(MsgPlain(l))
		}
		return nil
	}

	subCmd := strings.ToLower(subArgs[0])
	rest := subArgs[1:]

	switch subCmd {
	case "append", "add":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp track <track> append <group>&r"))
		}
		groupName := strings.ToLower(rest[0])
		if mgr.GetGroup(groupName) == nil {
			return src.SendMessage(Msg(Prefix+"&cGroup '&e%s&c' does not exist.&r", groupName))
		}
		if !track.Append(groupName) {
			return src.SendMessage(Msg(Prefix+"&cGroup '&e%s&c' is already in track '&e%s&c'.&r", groupName, track.Name))
		}
		_ = mgr.SaveTrack(track)
		mgr.LogAction(getActorName(src), "track", track.Name, fmt.Sprintf("Appended group '%s'", groupName))
		return src.SendMessage(Msg(Prefix+"&aAppended group &e%s&a to track &b%s&a!&r", groupName, track.Name))

	case "insert":
		if len(rest) < 2 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp track <track> insert <group> <position>&r"))
		}
		groupName := strings.ToLower(rest[0])
		pos, err := strconv.Atoi(rest[1])
		if err != nil {
			return src.SendMessage(Msg(Prefix + "&cPosition must be an integer (1-based or 0-based).&r"))
		}
		if pos > 0 {
			pos = pos - 1 // convert 1-based user input to 0-based index
		}
		if mgr.GetGroup(groupName) == nil {
			return src.SendMessage(Msg(Prefix+"&cGroup '&e%s&c' does not exist.&r", groupName))
		}
		if !track.Insert(groupName, pos) {
			return src.SendMessage(Msg(Prefix+"&cGroup '&e%s&c' is already in track '&e%s&c'.&r", groupName, track.Name))
		}
		_ = mgr.SaveTrack(track)
		mgr.LogAction(getActorName(src), "track", track.Name, fmt.Sprintf("Inserted group '%s' at position %d", groupName, pos+1))
		return src.SendMessage(Msg(Prefix+"&aInserted group &e%s&a into track &b%s&a at position &e%d&a!&r", groupName, track.Name, pos+1))

	case "remove":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp track <track> remove <group>&r"))
		}
		groupName := strings.ToLower(rest[0])
		if !track.Remove(groupName) {
			return src.SendMessage(Msg(Prefix+"&cGroup '&e%s&c' is not in track '&e%s&c'.&r", groupName, track.Name))
		}
		_ = mgr.SaveTrack(track)
		mgr.LogAction(getActorName(src), "track", track.Name, fmt.Sprintf("Removed group '%s'", groupName))
		return src.SendMessage(Msg(Prefix+"&aRemoved group &e%s&a from track &b%s&a.&r", groupName, track.Name))

	case "clear":
		track.Clear()
		_ = mgr.SaveTrack(track)
		mgr.LogAction(getActorName(src), "track", track.Name, "Cleared all groups from track")
		return src.SendMessage(Msg(Prefix+"&aCleared all groups from track &b%s&a.&r", track.Name))

	case "rename":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp track <track> rename <newName>&r"))
		}
		newName := strings.ToLower(strings.TrimSpace(rest[0]))
		renamed, _, err := mgr.RenameTrack(track.Name, newName, getActorName(src))
		if err != nil {
			return src.SendMessage(Msg(Prefix+"&cFailed to rename track: %v&r", err))
		}
		return src.SendMessage(Msg(Prefix+"&aRenamed track &b%s&a to &b%s&a!&r", track.Name, renamed.Name))

	case "clone":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp track <track> clone <newName>&r"))
		}
		newName := strings.ToLower(strings.TrimSpace(rest[0]))
		if mgr.GetTrack(newName) != nil {
			return src.SendMessage(Msg(Prefix+"&cTrack '&e%s&c' already exists.&r", newName))
		}
		cloned, _, err := mgr.CreateTrackWithActor(newName, track.Groups, getActorName(src))
		if err != nil {
			return src.SendMessage(Msg(Prefix+"&cFailed to clone track: %v&r", err))
		}
		return src.SendMessage(Msg(Prefix+"&aCloned track &b%s&a to &b%s&a (%d groups)!&r", track.Name, cloned.Name, len(cloned.Groups)))

	case "editor", "ed":
		return openEditorSession(mgr, src, manager.WebSessionCreateOptions{
			Actor:        getActorName(src),
			CommandAlias: "sp",
			TargetType:   "track",
			Target:       track.Name,
		})

	default:
		return src.SendMessage(Msg(Prefix + "&cUnknown track subcommand. Available: info, append, insert, remove, clear, rename, clone, editor.&r"))
	}
}
