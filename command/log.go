package command

import (
	"fmt"
	"strconv"
	"strings"

	"go.minekube.com/gate/pkg/command"

	"github.com/andreisugu/gate-stonyperms/manager"
	"github.com/andreisugu/gate-stonyperms/model"
)

const pageSize = 8

// HandleLogDispatch routes /sp log <subcommand> calls.
func HandleLogDispatch(mgr *manager.Manager, src command.Source, args []string) error {
	if len(args) > 0 && strings.EqualFold(args[0], "notify") {
		if !checkPerm(src, "stonyperms.log.notify") {
			return nil
		}
		if len(args) < 2 {
			state := "on"
			if !mgr.LogNotifyEnabled(getActorName(src)) {
				state = "off"
			}
			return src.SendMessage(Msg(Prefix+"&7Editor change notifications are &e%s&7. Usage: &b/sp log notify <on|off>&r", state))
		}
		switch strings.ToLower(args[1]) {
		case "on", "true", "enable", "enabled":
			mgr.SetLogNotify(getActorName(src), true)
			return src.SendMessage(Msg(Prefix + "&aEditor change notifications enabled.&r"))
		case "off", "false", "disable", "disabled":
			mgr.SetLogNotify(getActorName(src), false)
			return src.SendMessage(Msg(Prefix + "&eEditor change notifications muted.&r"))
		default:
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp log notify <on|off>&r"))
		}
	}
	if len(args) == 0 || strings.EqualFold(args[0], "recent") {
		page := 1
		if len(args) > 1 {
			if p, err := strconv.Atoi(args[1]); err == nil && p > 0 {
				page = p
			}
		}
		return renderLogEntries(src, "Recent Action Log", mgr.GetLogs(100), page)
	}

	subCmd := strings.ToLower(args[0])
	rest := args[1:]

	switch subCmd {
	case "search":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp log search <query> [page]&r"))
		}
		query := strings.ToLower(rest[0])
		page := 1
		if len(rest) > 1 {
			if p, err := strconv.Atoi(rest[1]); err == nil && p > 0 {
				page = p
			}
		}
		var filtered []model.LogEntry
		for _, l := range mgr.GetLogs(500) {
			if strings.Contains(strings.ToLower(l.Action), query) || strings.Contains(strings.ToLower(l.Actor), query) || strings.Contains(strings.ToLower(l.Target), query) {
				filtered = append(filtered, l)
			}
		}
		return renderLogEntries(src, fmt.Sprintf("Search Log: '%s'", query), filtered, page)

	case "userhistory", "user":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp log userhistory <user> [page]&r"))
		}
		userQuery := strings.ToLower(rest[0])
		page := 1
		if len(rest) > 1 {
			if p, err := strconv.Atoi(rest[1]); err == nil && p > 0 {
				page = p
			}
		}
		var filtered []model.LogEntry
		for _, l := range mgr.GetLogs(500) {
			if strings.EqualFold(l.TargetType, "user") && strings.Contains(strings.ToLower(l.Target), userQuery) {
				filtered = append(filtered, l)
			}
		}
		return renderLogEntries(src, fmt.Sprintf("User History: %s", userQuery), filtered, page)

	case "grouphistory", "group":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp log grouphistory <group> [page]&r"))
		}
		groupQuery := strings.ToLower(rest[0])
		page := 1
		if len(rest) > 1 {
			if p, err := strconv.Atoi(rest[1]); err == nil && p > 0 {
				page = p
			}
		}
		var filtered []model.LogEntry
		for _, l := range mgr.GetLogs(500) {
			if strings.EqualFold(l.TargetType, "group") && strings.Contains(strings.ToLower(l.Target), groupQuery) {
				filtered = append(filtered, l)
			}
		}
		return renderLogEntries(src, fmt.Sprintf("Group History: %s", groupQuery), filtered, page)

	case "trackhistory", "track":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp log trackhistory <track> [page]&r"))
		}
		trackQuery := strings.ToLower(rest[0])
		page := 1
		if len(rest) > 1 {
			if p, err := strconv.Atoi(rest[1]); err == nil && p > 0 {
				page = p
			}
		}
		var filtered []model.LogEntry
		for _, l := range mgr.GetLogs(500) {
			if strings.EqualFold(l.TargetType, "track") && strings.Contains(strings.ToLower(l.Target), trackQuery) {
				filtered = append(filtered, l)
			}
		}
		return renderLogEntries(src, fmt.Sprintf("Track History: %s", trackQuery), filtered, page)

	default:
		return src.SendMessage(Msg(Prefix + "&cUnknown log subcommand. Available: recent, search, userhistory, grouphistory, trackhistory.&r"))
	}
}

func renderLogEntries(src command.Source, title string, logs []model.LogEntry, page int) error {
	if len(logs) == 0 {
		return src.SendMessage(Msg(Prefix + "&7No audit logs recorded.&r"))
	}

	totalPages := (len(logs) + pageSize - 1) / pageSize
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}

	// Logs are newest at the end, so reverse slice for display
	reversed := make([]model.LogEntry, len(logs))
	for i, l := range logs {
		reversed[len(logs)-1-i] = l
	}

	start := (page - 1) * pageSize
	end := start + pageSize
	if end > len(reversed) {
		end = len(reversed)
	}

	lines := []string{
		Header(fmt.Sprintf("%s (Page %d/%d)", title, page, totalPages)),
	}

	for _, l := range reversed[start:end] {
		timeStr := l.Time().Format("01/02 15:04:05")
		lines = append(lines, fmt.Sprintf("  &8[%s]&r &e%s&r &7->&r &f%s&r", timeStr, l.Actor, l.Action))
	}

	lines = append(lines, Separator())
	for _, l := range lines {
		_ = src.SendMessage(MsgPlain(l))
	}
	return nil
}
