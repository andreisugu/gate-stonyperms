package command

import (
	"fmt"
	"strings"

	c "go.minekube.com/common/minecraft/component"
	"go.minekube.com/gate/pkg/command"

	"github.com/andreisugu/gate-stonyperms/manager"
	"github.com/andreisugu/gate-stonyperms/model"
)

// HandleEditor creates a web permissions editor session and sends a clickable link to chat.
func HandleEditor(mgr *manager.Manager, src command.Source, args []string) error {
	filter := strings.TrimSpace(strings.Join(args, " "))
	return openEditorSession(mgr, src, manager.WebSessionCreateOptions{
		Actor:        getActorName(src),
		CommandAlias: "sp",
		TargetType:   "all",
		Filter:       filter,
	})
}

func openEditorSession(mgr *manager.Manager, src command.Source, opts manager.WebSessionCreateOptions) error {
	if !checkPerm(src, "stonyperms.editor") {
		return nil
	}
	created, err := mgr.CreateEditorSession(opts)
	if err != nil {
		return src.SendMessage(Msg(Prefix+"&cFailed to create web editor session: %v&r", err))
	}

	var lines []c.Component
	lines = append(lines,
		MsgPlain(Prefix+"&aOpening the StonyPerms Web Permissions Editor...&r"),
		ClickableURL("&a&n"+created.URL+"&r", created.URL, "Open the StonyPerms Web Editor in your browser"),
	)
	if !created.LiveSave {
		lines = append(lines, MsgPlain("&7HTTP API is disabled — after clicking &eSave&7, run &b/sp applyedits <code>&7 in chat.&r"))
	} else {
		lines = append(lines, MsgPlain("&7Click &aSave&7 in the editor to apply changes live, or copy &b/sp applyedits <code>&7.&r"))
	}
	for _, line := range lines {
		if sendErr := src.SendMessage(line); sendErr != nil {
			return sendErr
		}
	}
	return nil
}

// HandleApplyEdits applies changes exported or staged from StonyPermsWeb.
func HandleApplyEdits(mgr *manager.Manager, src command.Source, args []string) error {
	if !checkPerm(src, "stonyperms.applyedits") {
		return nil
	}
	if len(args) < 1 {
		return src.SendMessage(Msg(Prefix + "&cUsage: /sp applyedits <code|file <name>>&r"))
	}
	if strings.EqualFold(strings.TrimSpace(args[0]), "file") {
		if len(args) < 2 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp applyedits file <name>&r"))
		}
		summary, err := mgr.ApplyEditFile(getActorName(src), strings.TrimSpace(args[1]))
		if err != nil {
			return src.SendMessage(Msg(Prefix+"&cFailed to apply %s: %v&r", args[1], err))
		}
		return sendApplySummary(src, summary)
	}
	raw := strings.TrimSpace(args[0])
	if strings.EqualFold(raw, "applyedits") && len(args) > 1 {
		raw = strings.TrimSpace(args[1])
	}
	summary, err := mgr.ApplyEditCode(getActorName(src), raw)
	if err != nil {
		return src.SendMessage(Msg(Prefix+"&cFailed to apply web edits: %v&r", err))
	}
	return sendApplySummary(src, summary)
}

func sendApplySummary(src command.Source, summary manager.EditorApplySummary) error {
	if len(summary.Diff) > 0 {
		_ = src.SendMessage(Msg(Prefix+"&aApplied web edits (rev &e#%d&a):&r", summary.Revision))
		for i, line := range summary.Diff {
			if i >= 12 {
				_ = src.SendMessage(MsgPlain("&7  ... and more changes in the audit log&r"))
				break
			}
			_ = src.SendMessage(MsgPlain("&7  " + line + "&r"))
		}
		return nil
	}
	return src.SendMessage(Msg(
		Prefix+"&aApplied web edits (rev &e#%d&a): &e%d&7 groups (&c-%d&7), &e%d&7 tracks (&c-%d&7), &e%d&7 users (&c-%d&7).&r",
		summary.Revision,
		summary.GroupsUpdated, summary.GroupsDeleted,
		summary.TracksUpdated, summary.TracksDeleted,
		summary.UsersUpdated, summary.UsersDeleted,
	))
}

// HandleTree generates a web permission tree session across registered proxy & storage permissions.
func HandleTree(mgr *manager.Manager, src command.Source, args []string) error {
	if !checkPerm(src, "stonyperms.tree") {
		return nil
	}
	root := "."
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		root = strings.TrimSpace(args[0])
	}
	created, err := mgr.CreateTreeSession(root, getActorName(src), "sp", nil)
	if err != nil {
		return src.SendMessage(Msg(Prefix+"&cFailed to create permission tree session: %v&r", err))
	}
	if sendErr := src.SendMessage(MsgPlain(Prefix + fmt.Sprintf("&aPermission tree prepared (&e%d&a nodes under &b%s&a):&r", created.Count, root))); sendErr != nil {
		return sendErr
	}
	return src.SendMessage(ClickableURL("&a&n"+created.URL+"&r", created.URL, "Open the interactive Permission Tree Viewer"))
}

func openUserEditor(mgr *manager.Manager, src command.Source, u *model.User) error {
	if u == nil {
		return src.SendMessage(Msg(Prefix + "&cUser not found.&r"))
	}
	return openEditorSession(mgr, src, manager.WebSessionCreateOptions{
		Actor:        getActorName(src),
		CommandAlias: "sp",
		TargetType:   "user",
		Target:       u.UUID.String(),
	})
}

func openGroupEditor(mgr *manager.Manager, src command.Source, g *model.Group) error {
	if g == nil {
		return src.SendMessage(Msg(Prefix + "&cGroup not found.&r"))
	}
	return openEditorSession(mgr, src, manager.WebSessionCreateOptions{
		Actor:        getActorName(src),
		CommandAlias: "sp",
		TargetType:   "group",
		Target:       g.Name,
	})
}
