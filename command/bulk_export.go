package command

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.minekube.com/gate/pkg/command"

	"github.com/andreisugu/gate-stonyperms/manager"
)

const bulkConfirmTTL = 60 * time.Second

type pendingBulkEntry struct {
	opts      manager.BulkUpdateOptions
	summary   manager.BulkUpdateSummary
	expiresAt time.Time
}

var (
	pendingBulkMu      sync.Mutex
	pendingBulkUpdates = make(map[string]pendingBulkEntry)
)

func storePendingBulk(opts manager.BulkUpdateOptions, summary manager.BulkUpdateSummary) string {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		b[0] = byte(time.Now().UnixNano())
		b[1] = byte(time.Now().UnixNano() >> 8)
		b[2] = byte(time.Now().UnixNano() >> 16)
	}
	code := strings.ToLower(hex.EncodeToString(b[:]))

	now := time.Now()
	pendingBulkMu.Lock()
	for k, v := range pendingBulkUpdates {
		if !now.Before(v.expiresAt) {
			delete(pendingBulkUpdates, k)
		}
	}
	pendingBulkUpdates[code] = pendingBulkEntry{
		opts:      opts,
		summary:   summary,
		expiresAt: now.Add(bulkConfirmTTL),
	}
	pendingBulkMu.Unlock()
	return code
}

func takePendingBulk(code string) (pendingBulkEntry, bool) {
	key := strings.ToLower(strings.TrimSpace(code))
	now := time.Now()
	pendingBulkMu.Lock()
	defer pendingBulkMu.Unlock()
	for k, v := range pendingBulkUpdates {
		if !now.Before(v.expiresAt) {
			delete(pendingBulkUpdates, k)
		}
	}
	entry, ok := pendingBulkUpdates[key]
	if ok {
		delete(pendingBulkUpdates, key)
	}
	return entry, ok
}

// HandleSearch executes /sp search <permission|group.<group>> [page].
func HandleSearch(mgr *manager.Manager, src command.Source, args []string) error {
	if len(args) < 1 {
		return src.SendMessage(Msg(Prefix + "&cUsage: /sp search <permission|group.<group>> [page]&r"))
	}

	page := 1
	query := args[0]
	if len(args) >= 2 {
		if p, err := strconv.Atoi(args[len(args)-1]); err == nil && p > 0 {
			page = p
			if len(args) > 2 {
				query = strings.Join(args[:len(args)-1], " ")
			}
		} else {
			query = strings.Join(args, " ")
		}
	}

	res := mgr.Search(query)
	hits := make([]manager.SearchHit, 0, len(res.GroupHits)+len(res.UserHits))
	hits = append(hits, res.GroupHits...)
	hits = append(hits, res.UserHits...)

	const perPage = 15
	total := len(hits)
	totalPages := (total + perPage - 1) / perPage
	if totalPages == 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}

	lines := []string{
		Header(fmt.Sprintf("Search: %s (Page %d/%d - %d hits)", res.Query, page, totalPages, total)),
	}
	if total == 0 {
		lines = append(lines, "  &7(no matching permissions or parents found)&r")
	} else {
		start := (page - 1) * perPage
		end := start + perPage
		if end > total {
			end = total
		}
		for _, h := range hits[start:end] {
			tag := "&e[G] " + h.Target
			if h.TargetType == "user" {
				tag = "&b[U] " + h.Target
			}
			sign := "&a+"
			if !h.Value {
				sign = "&c-"
			}
			exp := ""
			if h.Expiry > 0 {
				exp = fmt.Sprintf(" &e(expires in %s)&r", FormatDuration(time.Until(time.Unix(h.Expiry, 0))))
			}
			lines = append(lines, fmt.Sprintf("  %s&r &8->&r %s &f%s&r%s %s", tag, sign, h.NodeKey, exp, FormatContexts(h.Contexts)))
		}
	}
	lines = append(lines, Separator())
	for _, l := range lines {
		_ = src.SendMessage(MsgPlain(l))
	}

	if totalPages > 1 {
		if page > 1 {
			_ = src.SendMessage(PageButton("&8[&b◀ Prev Search Page&8]&r", fmt.Sprintf("/sp search %s %d", res.Query, page-1), "&eView previous search page"))
		}
		if page < totalPages {
			_ = src.SendMessage(PageButton("&8[&bNext Search Page ▶&8]&r", fmt.Sprintf("/sp search %s %d", res.Query, page+1), "&eView next search page"))
		}
	}
	return nil
}

// HandleBulkUpdate executes /sp bulkupdate <all|users|groups> <update|delete> [field value] [constraints...] [--confirm]
// or /sp bulkupdate confirm <code>.
func HandleBulkUpdate(mgr *manager.Manager, src command.Source, args []string) error {
	if len(args) < 1 {
		return sendBulkUsage(src)
	}

	if strings.EqualFold(args[0], "confirm") {
		if len(args) < 2 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp bulkupdate confirm <code>&r"))
		}
		entry, ok := takePendingBulk(args[1])
		if !ok {
			return src.SendMessage(Msg(Prefix + "&cInvalid or expired confirmation code (codes expire after 60s).&r"))
		}
		summary, err := mgr.BulkUpdate(entry.opts, getActorName(src))
		if err != nil {
			return src.SendMessage(Msg(Prefix+"&cBulk update failed: %v&r", err))
		}
		return sendBulkSummary(src, summary)
	}

	if len(args) < 2 {
		return sendBulkUsage(src)
	}

	scope := strings.ToLower(args[0])
	action := strings.ToLower(args[1])
	rest := args[2:]

	confirmNow := false
	filtered := make([]string, 0, len(rest))
	for _, tok := range rest {
		if strings.EqualFold(tok, "--confirm") {
			confirmNow = true
			continue
		}
		filtered = append(filtered, tok)
	}

	var field, value string
	constraintTokens := filtered
	if action == "update" {
		if len(filtered) < 2 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp bulkupdate <all|users|groups> update <field> <value> [constraints...] [--confirm]&r"))
		}
		field = filtered[0]
		value = filtered[1]
		constraintTokens = filtered[2:]
	}

	constraints, err := parseBulkConstraints(constraintTokens)
	if err != nil {
		return src.SendMessage(Msg(Prefix+"&c%v&r", err))
	}

	opts := manager.BulkUpdateOptions{
		Scope:       scope,
		Action:      action,
		Field:       field,
		Value:       value,
		Constraints: constraints,
	}

	if confirmNow {
		summary, err := mgr.BulkUpdate(opts, getActorName(src))
		if err != nil {
			return src.SendMessage(Msg(Prefix+"&cBulk update failed: %v&r", err))
		}
		return sendBulkSummary(src, summary)
	}

	preview, err := mgr.PreviewBulkUpdate(opts)
	if err != nil {
		return src.SendMessage(Msg(Prefix+"&cInvalid bulk update: %v&r", err))
	}
	if preview.NodesAffected == 0 {
		return src.SendMessage(Msg(Prefix + "&7Bulk update preview: &e0&7 matching nodes found; nothing to change.&r"))
	}

	code := storePendingBulk(opts, preview)
	_ = src.SendMessage(Msg(Prefix+"&eBulk update preview (expires in 60s): &b%d&e groups, &b%d&e users, &a%d&e nodes matched.&r", preview.GroupsChanged, preview.UsersChanged, preview.NodesAffected))
	_ = src.SendMessage(ClickableRunCmd(fmt.Sprintf("&8[&aConfirm Bulk Update: &f/sp bulkupdate confirm %s&8]&r", code), "/sp bulkupdate confirm "+code, "&eClick to apply this bulk update"))
	return nil
}

func sendBulkUsage(src command.Source) error {
	return src.SendMessage(Msg(Prefix + "&cUsage: /sp bulkupdate <all|users|groups> <update <field> <value>|delete> [\"<field> <==|!=|~~|!~> <pattern>\"...] [--confirm] | confirm <code>&r"))
}

func sendBulkSummary(src command.Source, s manager.BulkUpdateSummary) error {
	return src.SendMessage(Msg(Prefix+"&aBulk update complete: &e%d&a groups, &e%d&a users, &b%d&a nodes updated &7(rev=%d)&a.&r", s.GroupsChanged, s.UsersChanged, s.NodesAffected, s.Revision))
}

func parseBulkConstraints(tokens []string) ([]manager.BulkConstraint, error) {
	var out []manager.BulkConstraint
	for i := 0; i < len(tokens); {
		parts := strings.Fields(tokens[i])
		if len(parts) == 3 {
			out = append(out, manager.BulkConstraint{
				Field:    parts[0],
				Operator: parts[1],
				Pattern:  parts[2],
			})
			i++
			continue
		}
		if len(parts) == 1 && i+2 < len(tokens) {
			out = append(out, manager.BulkConstraint{
				Field:    tokens[i],
				Operator: tokens[i+1],
				Pattern:  tokens[i+2],
			})
			i += 3
			continue
		}
		return nil, fmt.Errorf("invalid constraint near %q (expected '<field> <==|!=|~~|!~> <pattern>')", tokens[i])
	}
	return out, nil
}

// HandleExport executes /sp export <filename>.
func HandleExport(mgr *manager.Manager, src command.Source, args []string) error {
	if len(args) < 1 {
		return src.SendMessage(Msg(Prefix + "&cUsage: /sp export <filename.json|filename.json.gz>&r"))
	}
	path, bundle, err := mgr.ExportToFile(args[0], getActorName(src))
	if err != nil {
		return src.SendMessage(Msg(Prefix+"&cExport failed: %v&r", err))
	}
	return src.SendMessage(Msg(Prefix+"&aExported &e%d&a groups, &e%d&a tracks, &e%d&a users to &b%s&a.&r", len(bundle.Groups), len(bundle.Tracks), len(bundle.Users), filepath.Base(path)))
}

// HandleImport executes /sp import <filename> [--replace].
func HandleImport(mgr *manager.Manager, src command.Source, args []string) error {
	if len(args) < 1 {
		return src.SendMessage(Msg(Prefix + "&cUsage: /sp import <filename> [--replace]&r"))
	}
	replace := false
	filename := ""
	for _, a := range args {
		if strings.EqualFold(a, "--replace") {
			replace = true
			continue
		}
		if filename == "" {
			filename = a
		}
	}
	if filename == "" {
		return src.SendMessage(Msg(Prefix + "&cUsage: /sp import <filename> [--replace]&r"))
	}
	summary, err := mgr.ImportFromFile(filename, replace, getActorName(src))
	if err != nil {
		return src.SendMessage(Msg(Prefix+"&cImport failed: %v&r", err))
	}
	return src.SendMessage(Msg(Prefix+"&aImported &e%d&a groups, &e%d&a tracks, &e%d&a users &7(replace=%v, rev=%d)&a.&r", summary.Groups, summary.Tracks, summary.Users, summary.Replaced, summary.Revision))
}
