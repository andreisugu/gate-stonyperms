package command

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.minekube.com/gate/pkg/command"

	"github.com/andreisugu/gate-stonyperms/manager"
	"github.com/andreisugu/gate-stonyperms/model"
)

type permSubject interface {
	IsUser() bool
	Kind() string
	Name() string
	Save() error
	Nodes() []model.Node
	SetNodes([]model.Node)
	Parents() []model.Inheritance
	SetParents([]model.Inheritance)
	PrimaryGroup() string
	SetPrimaryGroup(string)
	MetaMap() map[string]string
	MetaEntries() []model.MetaEntry
	Prefixes() []model.PriorityMeta
	Suffixes() []model.PriorityMeta
	ClearMeta(scope string)
	CleanExpired(time.Time) int
	AddNodeWithModifier(model.Node, model.TempModifier, time.Time) (model.Node, error)
	RemoveNode(string, map[string]string) bool
	RemoveTempNode(string, map[string]string) bool
	AddParentWithModifier(model.Inheritance, model.TempModifier, time.Time) (model.Inheritance, error)
	RemoveParent(string, map[string]string) bool
	RemoveTempParent(string, map[string]string) bool
	SetMetaEntry(model.MetaEntry, model.TempModifier, time.Time) (model.MetaEntry, error)
	UnsetMetaEntry(string, map[string]string, bool) bool
	AddAffixWithModifier(bool, model.PriorityMeta, model.TempModifier, time.Time) (model.PriorityMeta, error)
	SetAffixInContext(bool, model.PriorityMeta)
	RemoveAffixMatch(bool, int, string, map[string]string, bool) bool
	CheckPermission(*manager.Manager, command.Source, string, []string) error
	SetTrackGroup(*manager.Manager, string, string, map[string]string, string) error
	ClearTrack(*manager.Manager, string, map[string]string, string) (bool, error)
	ResolvedAffixes(*manager.Manager) (string, string)
}

type userSubject struct {
	u   *model.User
	mgr *manager.Manager
}

func (s userSubject) IsUser() bool                     { return true }
func (s userSubject) Kind() string                     { return "user" }
func (s userSubject) Name() string                     { return s.u.Username }
func (s userSubject) Save() error                      { return s.mgr.SaveUser(s.u) }
func (s userSubject) Nodes() []model.Node              { return s.u.Nodes }
func (s userSubject) SetNodes(n []model.Node)          { s.u.Nodes = n }
func (s userSubject) Parents() []model.Inheritance     { return s.u.Parents }
func (s userSubject) SetParents(p []model.Inheritance) { s.u.Parents = p }
func (s userSubject) PrimaryGroup() string             { return s.u.PrimaryGroup }
func (s userSubject) SetPrimaryGroup(g string)         { s.u.PrimaryGroup = g }
func (s userSubject) MetaMap() map[string]string       { return s.u.Meta }
func (s userSubject) MetaEntries() []model.MetaEntry   { return s.u.MetaEntries }
func (s userSubject) Prefixes() []model.PriorityMeta   { return s.u.Prefixes }
func (s userSubject) Suffixes() []model.PriorityMeta   { return s.u.Suffixes }
func (s userSubject) CleanExpired(t time.Time) int     { return s.u.CleanExpired(t) }
func (s userSubject) AddNodeWithModifier(n model.Node, mod model.TempModifier, now time.Time) (model.Node, error) {
	return s.u.AddNodeWithModifier(n, mod, now)
}
func (s userSubject) RemoveNode(k string, c map[string]string) bool { return s.u.RemoveNode(k, c) }
func (s userSubject) RemoveTempNode(k string, c map[string]string) bool {
	return s.u.RemoveTempNode(k, c)
}
func (s userSubject) AddParentWithModifier(i model.Inheritance, mod model.TempModifier, now time.Time) (model.Inheritance, error) {
	return s.u.AddParentWithModifier(i, mod, now)
}
func (s userSubject) RemoveParent(g string, c map[string]string) bool { return s.u.RemoveParent(g, c) }
func (s userSubject) RemoveTempParent(g string, c map[string]string) bool {
	return s.u.RemoveTempParent(g, c)
}
func (s userSubject) SetMetaEntry(m model.MetaEntry, mod model.TempModifier, now time.Time) (model.MetaEntry, error) {
	return s.u.SetMetaEntry(m, mod, now)
}
func (s userSubject) UnsetMetaEntry(k string, c map[string]string, tempOnly bool) bool {
	return s.u.UnsetMetaEntry(k, c, tempOnly)
}
func (s userSubject) AddAffixWithModifier(isSuffix bool, pm model.PriorityMeta, mod model.TempModifier, now time.Time) (model.PriorityMeta, error) {
	if isSuffix {
		return s.u.AddSuffixWithModifier(pm, mod, now)
	}
	return s.u.AddPrefixWithModifier(pm, mod, now)
}
func (s userSubject) SetAffixInContext(isSuffix bool, pm model.PriorityMeta) {
	if isSuffix {
		s.u.SetSuffixInContext(pm)
		return
	}
	s.u.SetPrefixInContext(pm)
}
func (s userSubject) RemoveAffixMatch(isSuffix bool, p int, v string, c map[string]string, tempOnly bool) bool {
	if isSuffix {
		return s.u.RemoveSuffixMatch(p, v, c, tempOnly)
	}
	return s.u.RemovePrefixMatch(p, v, c, tempOnly)
}
func (s userSubject) ClearMeta(scope string) {
	switch scope {
	case "prefix", "prefixes":
		s.u.Prefixes = make([]model.PriorityMeta, 0)
	case "suffix", "suffixes":
		s.u.Suffixes = make([]model.PriorityMeta, 0)
	case "meta", "keys":
		s.u.Meta = make(map[string]string)
		s.u.MetaEntries = make([]model.MetaEntry, 0)
	default:
		s.u.Meta = make(map[string]string)
		s.u.MetaEntries = make([]model.MetaEntry, 0)
		s.u.Prefixes = make([]model.PriorityMeta, 0)
		s.u.Suffixes = make([]model.PriorityMeta, 0)
	}
}
func (s userSubject) CheckPermission(mgr *manager.Manager, src command.Source, perm string, extra []string) error {
	checkArgs := append([]string{s.u.Username, perm}, extra...)
	return HandleCheck(mgr, src, checkArgs)
}
func (s userSubject) SetTrackGroup(mgr *manager.Manager, trackName, targetGroup string, ctx map[string]string, actor string) error {
	_, err := mgr.SetUserTrackGroup(s.u.UUID, trackName, targetGroup, ctx, actor)
	return err
}
func (s userSubject) ClearTrack(mgr *manager.Manager, trackName string, ctx map[string]string, actor string) (bool, error) {
	_, removed, err := mgr.ClearUserTrack(s.u.UUID, trackName, ctx, actor)
	return removed, err
}
func (s userSubject) ResolvedAffixes(mgr *manager.Manager) (string, string) {
	return mgr.GetPrefix(s.u, nil), mgr.GetSuffix(s.u, nil)
}

type groupSubject struct {
	g   *model.Group
	mgr *manager.Manager
}

func (s groupSubject) IsUser() bool                     { return false }
func (s groupSubject) Kind() string                     { return "group" }
func (s groupSubject) Name() string                     { return s.g.Name }
func (s groupSubject) Save() error                      { return s.mgr.SaveGroup(s.g) }
func (s groupSubject) Nodes() []model.Node              { return s.g.Nodes }
func (s groupSubject) SetNodes(n []model.Node)          { s.g.Nodes = n }
func (s groupSubject) Parents() []model.Inheritance     { return s.g.Parents }
func (s groupSubject) SetParents(p []model.Inheritance) { s.g.Parents = p }
func (s groupSubject) PrimaryGroup() string             { return "" }
func (s groupSubject) SetPrimaryGroup(string)           {}
func (s groupSubject) MetaMap() map[string]string       { return s.g.Meta }
func (s groupSubject) MetaEntries() []model.MetaEntry   { return s.g.MetaEntries }
func (s groupSubject) Prefixes() []model.PriorityMeta   { return s.g.Prefixes }
func (s groupSubject) Suffixes() []model.PriorityMeta   { return s.g.Suffixes }
func (s groupSubject) CleanExpired(t time.Time) int     { return s.g.CleanExpired(t) }
func (s groupSubject) AddNodeWithModifier(n model.Node, mod model.TempModifier, now time.Time) (model.Node, error) {
	return s.g.AddNodeWithModifier(n, mod, now)
}
func (s groupSubject) RemoveNode(k string, c map[string]string) bool { return s.g.RemoveNode(k, c) }
func (s groupSubject) RemoveTempNode(k string, c map[string]string) bool {
	return s.g.RemoveTempNode(k, c)
}
func (s groupSubject) AddParentWithModifier(i model.Inheritance, mod model.TempModifier, now time.Time) (model.Inheritance, error) {
	return s.g.AddParentWithModifier(i, mod, now)
}
func (s groupSubject) RemoveParent(g string, c map[string]string) bool {
	return s.g.RemoveParent(g, c)
}
func (s groupSubject) RemoveTempParent(g string, c map[string]string) bool {
	return s.g.RemoveTempParent(g, c)
}
func (s groupSubject) SetMetaEntry(m model.MetaEntry, mod model.TempModifier, now time.Time) (model.MetaEntry, error) {
	return s.g.SetMetaEntry(m, mod, now)
}
func (s groupSubject) UnsetMetaEntry(k string, c map[string]string, tempOnly bool) bool {
	return s.g.UnsetMetaEntry(k, c, tempOnly)
}
func (s groupSubject) AddAffixWithModifier(isSuffix bool, pm model.PriorityMeta, mod model.TempModifier, now time.Time) (model.PriorityMeta, error) {
	if isSuffix {
		return s.g.AddSuffixWithModifier(pm, mod, now)
	}
	return s.g.AddPrefixWithModifier(pm, mod, now)
}
func (s groupSubject) SetAffixInContext(isSuffix bool, pm model.PriorityMeta) {
	if isSuffix {
		s.g.SetSuffixInContext(pm)
		return
	}
	s.g.SetPrefixInContext(pm)
}
func (s groupSubject) RemoveAffixMatch(isSuffix bool, p int, v string, c map[string]string, tempOnly bool) bool {
	if isSuffix {
		return s.g.RemoveSuffixMatch(p, v, c, tempOnly)
	}
	return s.g.RemovePrefixMatch(p, v, c, tempOnly)
}
func (s groupSubject) ClearMeta(scope string) {
	switch scope {
	case "prefix", "prefixes":
		s.g.Prefixes = make([]model.PriorityMeta, 0)
	case "suffix", "suffixes":
		s.g.Suffixes = make([]model.PriorityMeta, 0)
	case "meta", "keys":
		s.g.Meta = make(map[string]string)
		s.g.MetaEntries = make([]model.MetaEntry, 0)
	default:
		s.g.Meta = make(map[string]string)
		s.g.MetaEntries = make([]model.MetaEntry, 0)
		s.g.Prefixes = make([]model.PriorityMeta, 0)
		s.g.Suffixes = make([]model.PriorityMeta, 0)
	}
}
func (s groupSubject) CheckPermission(mgr *manager.Manager, src command.Source, perm string, extra []string) error {
	contexts, _ := ParseContexts(extra)
	groups := make(map[string]*model.Group)
	for _, grp := range mgr.GetGroups() {
		groups[grp.Name] = grp
	}
	res := mgr.Engine().GroupPermissionValue(s.g, groups, perm, contexts)

	valStr := "&7&lUNDEFINED (false)&r"
	switch res.Result {
	case model.True:
		valStr = "&a&lTRUE&r"
	case model.False:
		valStr = "&c&lFALSE&r"
	}

	lines := []string{
		Header(fmt.Sprintf("Group Permission Check: %s", s.g.Name)),
		fmt.Sprintf("&bPermission:&r &f%s&r", perm),
		fmt.Sprintf("&bResult:&r %s", valStr),
		fmt.Sprintf("&bTrace:&r &7%s&r", res.Source),
		Separator(),
	}
	for _, l := range lines {
		_ = src.SendMessage(MsgPlain(l))
	}
	return nil
}
func (s groupSubject) SetTrackGroup(mgr *manager.Manager, trackName, targetGroup string, ctx map[string]string, actor string) error {
	_, err := mgr.SetGroupTrackParent(s.g.Name, trackName, targetGroup, ctx, actor)
	return err
}
func (s groupSubject) ClearTrack(mgr *manager.Manager, trackName string, ctx map[string]string, actor string) (bool, error) {
	_, removed, err := mgr.ClearGroupTrack(s.g.Name, trackName, ctx, actor)
	return removed, err
}
func (s groupSubject) ResolvedAffixes(*manager.Manager) (string, string) {
	bestP, bestS := "", ""
	maxP, maxS := -1<<31, -1<<31
	for _, p := range s.g.Prefixes {
		if p.Priority >= maxP {
			maxP = p.Priority
			bestP = p.Value
		}
	}
	for _, sf := range s.g.Suffixes {
		if sf.Priority >= maxS {
			maxS = sf.Priority
			bestS = sf.Value
		}
	}
	return bestP, bestS
}

func isContextToken(tok string) bool {
	idx := strings.IndexByte(tok, '=')
	if idx <= 0 || idx == len(tok)-1 {
		return false
	}
	k := strings.TrimSpace(tok[:idx])
	v := strings.TrimSpace(tok[idx+1:])
	return k != "" && v != ""
}

func splitTrailingContexts(args []string) (map[string]string, []string) {
	cut := len(args)
	for cut > 0 && isContextToken(args[cut-1]) {
		cut--
	}
	if cut == len(args) {
		return map[string]string{}, args
	}
	ctxs, _ := ParseContexts(args[cut:])
	return ctxs, args[:cut]
}

func parseTempDurationAndMod(tokens []string, idx int) (time.Duration, model.TempModifier, []string, error) {
	if idx >= len(tokens) {
		return 0, model.TempModifierReplace, nil, errors.New("duration required (e.g. 10m, 2h, 7d)")
	}
	dur, err := ParseDuration(tokens[idx])
	if err != nil {
		return 0, model.TempModifierReplace, nil, fmt.Errorf("invalid duration %q: %w", tokens[idx], err)
	}
	rem := tokens[idx+1:]
	mod := model.TempModifierReplace
	if len(rem) > 0 {
		if parsed, ok := model.ParseTempModifier(rem[0]); ok {
			mod = parsed
			rem = rem[1:]
		}
	}
	return dur, mod, rem, nil
}

func handleSubjectPermission(mgr *manager.Manager, src command.Source, subj permSubject, args []string) error {
	if len(args) < 1 {
		return src.SendMessage(Msg(Prefix+"&cUsage: /sp %s <%s> permission <info|set|unset|settemp|unsettemp|check|clear> [args...]&r", subj.Kind(), subj.Kind()))
	}

	action := strings.ToLower(args[0])
	rest := args[1:]

	switch action {
	case "info":
		subj.CleanExpired(time.Now())
		page := 1
		if len(rest) > 0 {
			if p, err := strconv.Atoi(rest[0]); err == nil && p > 0 {
				page = p
			}
		}
		nodes := subj.Nodes()
		const perPage = 15
		totalPages := (len(nodes) + perPage - 1) / perPage
		if totalPages == 0 {
			totalPages = 1
		}
		if page > totalPages {
			page = totalPages
		}
		lines := []string{
			Header(fmt.Sprintf("Permissions (%s: %s - Page %d/%d)", subj.Kind(), subj.Name(), page, totalPages)),
		}
		if len(nodes) == 0 {
			lines = append(lines, "  &7(no direct permission nodes)&r")
		} else {
			start := (page - 1) * perPage
			end := start + perPage
			if end > len(nodes) {
				end = len(nodes)
			}
			for _, n := range nodes[start:end] {
				sign := "&a+"
				if !n.Value {
					sign = "&c-"
				}
				exp := ""
				if n.Expiry > 0 {
					exp = fmt.Sprintf(" &e(expires in %s)&r", FormatDuration(time.Until(time.Unix(n.Expiry, 0))))
				}
				lines = append(lines, fmt.Sprintf("  &6• %s &f%s&r%s %s", sign, n.Key, exp, FormatContexts(n.Contexts)))
			}
		}
		lines = append(lines, Separator())
		for _, l := range lines {
			_ = src.SendMessage(MsgPlain(l))
		}
		return nil

	case "set":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix+"&cUsage: /sp %s <%s> permission set <node> [true/false] [context...]&r", subj.Kind(), subj.Kind()))
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
		_, _ = subj.AddNodeWithModifier(node, model.TempModifierReplace, time.Now())
		if err := subj.Save(); err != nil {
			return src.SendMessage(Msg(Prefix+"&cFailed to save: %v&r", err))
		}
		mgr.LogAction(getActorName(src), subj.Kind(), subj.Name(), fmt.Sprintf("Set permission '%s' to %v %s", nodeKey, value, FormatContexts(contexts)))
		return src.SendMessage(Msg(Prefix+"&aSet &e%s&a to %s &afor %s &e%s&a %s&r", nodeKey, FormatBool(value), subj.Kind(), subj.Name(), FormatContexts(contexts)))

	case "unset":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix+"&cUsage: /sp %s <%s> permission unset <node> [context...]&r", subj.Kind(), subj.Kind()))
		}
		nodeKey := rest[0]
		contexts, _ := ParseContexts(rest[1:])
		if !subj.RemoveNode(nodeKey, contexts) {
			return src.SendMessage(Msg(Prefix+"&c%s &e%s&c does not have direct node '&e%s&c'.&r", strings.ToUpper(subj.Kind()[:1])+subj.Kind()[1:], subj.Name(), nodeKey))
		}
		if err := subj.Save(); err != nil {
			return src.SendMessage(Msg(Prefix+"&cFailed to save: %v&r", err))
		}
		mgr.LogAction(getActorName(src), subj.Kind(), subj.Name(), fmt.Sprintf("Unset permission '%s'", nodeKey))
		return src.SendMessage(Msg(Prefix+"&aUnset permission &e%s&a for %s &e%s&a.&r", nodeKey, subj.Kind(), subj.Name()))

	case "settemp":
		if len(rest) < 2 {
			return src.SendMessage(Msg(Prefix+"&cUsage: /sp %s <%s> permission settemp <node> [true/false] <duration> [accumulate|replace|deny] [context...]&r", subj.Kind(), subj.Kind()))
		}
		nodeKey := rest[0]
		valIdx := 1
		value := true
		if strings.EqualFold(rest[1], "true") || strings.EqualFold(rest[1], "false") {
			value = strings.EqualFold(rest[1], "true")
			valIdx = 2
		}
		dur, mod, ctxRem, err := parseTempDurationAndMod(rest, valIdx)
		if err != nil {
			return src.SendMessage(Msg(Prefix+"&c%v&r", err))
		}
		contexts, _ := ParseContexts(ctxRem)

		now := time.Now()
		node := model.NewTempNode(nodeKey, value, dur)
		node.Expiry = now.Add(dur).Unix()
		node.Contexts = contexts
		savedNode, err := subj.AddNodeWithModifier(node, mod, now)
		if err != nil {
			return src.SendMessage(Msg(Prefix+"&cCannot set temporary permission '&e%s&c': %v&r", nodeKey, err))
		}
		if err := subj.Save(); err != nil {
			return src.SendMessage(Msg(Prefix+"&cFailed to save: %v&r", err))
		}
		actualDur := time.Until(time.Unix(savedNode.Expiry, 0))
		mgr.LogAction(getActorName(src), subj.Kind(), subj.Name(), fmt.Sprintf("Set temp permission '%s' to %v (%s, modifier=%s)", nodeKey, value, FormatDuration(actualDur), mod))
		return src.SendMessage(Msg(Prefix+"&aSet temporary permission &e%s&a to %s &afor %s &e%s&a (duration: &e%s&a, modifier: &b%s&a) %s&r", nodeKey, FormatBool(value), subj.Kind(), subj.Name(), FormatDuration(actualDur), mod, FormatContexts(contexts)))

	case "unsettemp":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix+"&cUsage: /sp %s <%s> permission unsettemp <node> [context...]&r", subj.Kind(), subj.Kind()))
		}
		nodeKey := rest[0]
		contexts, _ := ParseContexts(rest[1:])
		if !subj.RemoveTempNode(nodeKey, contexts) {
			return src.SendMessage(Msg(Prefix+"&c%s &e%s&c does not have a matching temporary node '&e%s&c'.&r", strings.ToUpper(subj.Kind()[:1])+subj.Kind()[1:], subj.Name(), nodeKey))
		}
		if err := subj.Save(); err != nil {
			return src.SendMessage(Msg(Prefix+"&cFailed to save: %v&r", err))
		}
		mgr.LogAction(getActorName(src), subj.Kind(), subj.Name(), fmt.Sprintf("Unset temp permission '%s'", nodeKey))
		return src.SendMessage(Msg(Prefix+"&aUnset temporary permission &e%s&a for %s &e%s&a.&r", nodeKey, subj.Kind(), subj.Name()))

	case "check":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix+"&cUsage: /sp %s <%s> permission check <node> [context...]&r", subj.Kind(), subj.Kind()))
		}
		return subj.CheckPermission(mgr, src, rest[0], rest[1:])

	case "clear":
		contexts, _ := ParseContexts(rest)
		if len(contexts) > 0 {
			var remaining []model.Node
			for _, n := range subj.Nodes() {
				if !model.SameContexts(n.Contexts, contexts) {
					remaining = append(remaining, n)
				}
			}
			subj.SetNodes(remaining)
		} else {
			subj.SetNodes(make([]model.Node, 0))
		}
		if err := subj.Save(); err != nil {
			return src.SendMessage(Msg(Prefix+"&cFailed to save: %v&r", err))
		}
		mgr.LogAction(getActorName(src), subj.Kind(), subj.Name(), "Cleared permissions")
		return src.SendMessage(Msg(Prefix+"&aCleared permissions for %s &e%s&a.&r", subj.Kind(), subj.Name()))

	default:
		return src.SendMessage(Msg(Prefix+"&cUnknown permission action: %s. Available: info, set, unset, settemp, unsettemp, check, clear.&r", action))
	}
}

func handleSubjectParent(mgr *manager.Manager, src command.Source, subj permSubject, args []string) error {
	if len(args) < 1 {
		return src.SendMessage(Msg(Prefix+"&cUsage: /sp %s <%s> parent <info|add|set|remove|addtemp|removetemp|settrack|cleartrack|clear> [args...]&r", subj.Kind(), subj.Kind()))
	}

	action := strings.ToLower(args[0])
	rest := args[1:]

	switch action {
	case "info":
		subj.CleanExpired(time.Now())
		lines := []string{Header(fmt.Sprintf("Parents: %s", subj.Name()))}
		if subj.IsUser() {
			lines = append(lines, fmt.Sprintf("&bPrimary Group:&r &e%s&r", subj.PrimaryGroup()))
		}
		for _, p := range subj.Parents() {
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
			return src.SendMessage(Msg(Prefix+"&cUsage: /sp %s <%s> parent add <group> [context...]&r", subj.Kind(), subj.Kind()))
		}
		groupName := strings.ToLower(rest[0])
		if mgr.GetGroup(groupName) == nil {
			return src.SendMessage(Msg(Prefix+"&cGroup '&e%s&c' does not exist.&r", groupName))
		}
		if !subj.IsUser() && strings.EqualFold(subj.Name(), groupName) {
			return src.SendMessage(Msg(Prefix + "&cA group cannot inherit from itself.&r"))
		}
		contexts, _ := ParseContexts(rest[1:])
		inh := model.NewInheritance(groupName)
		inh.Contexts = contexts
		_, _ = subj.AddParentWithModifier(inh, model.TempModifierReplace, time.Now())
		if err := subj.Save(); err != nil {
			return src.SendMessage(Msg(Prefix+"&cFailed to save: %v&r", err))
		}
		mgr.LogAction(getActorName(src), subj.Kind(), subj.Name(), fmt.Sprintf("Added parent group '%s'", groupName))
		return src.SendMessage(Msg(Prefix+"&aAdded parent group &e%s&a to %s &e%s&a!&r", groupName, subj.Kind(), subj.Name()))

	case "set":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix+"&cUsage: /sp %s <%s> parent set <group> [context...]&r", subj.Kind(), subj.Kind()))
		}
		groupName := strings.ToLower(rest[0])
		if mgr.GetGroup(groupName) == nil {
			return src.SendMessage(Msg(Prefix+"&cGroup '&e%s&c' does not exist.&r", groupName))
		}
		if !subj.IsUser() && strings.EqualFold(subj.Name(), groupName) {
			return src.SendMessage(Msg(Prefix + "&cA group cannot inherit from itself.&r"))
		}
		contexts, _ := ParseContexts(rest[1:])
		subj.SetParents([]model.Inheritance{{
			Group:    groupName,
			Contexts: contexts,
		}})
		subj.SetPrimaryGroup(groupName)
		if err := subj.Save(); err != nil {
			return src.SendMessage(Msg(Prefix+"&cFailed to save: %v&r", err))
		}
		mgr.LogAction(getActorName(src), subj.Kind(), subj.Name(), fmt.Sprintf("Set parent group to '%s'", groupName))
		return src.SendMessage(Msg(Prefix+"&aSet sole parent group to &e%s&a for %s &e%s&a!&r", groupName, subj.Kind(), subj.Name()))

	case "remove":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix+"&cUsage: /sp %s <%s> parent remove <group> [context...]&r", subj.Kind(), subj.Kind()))
		}
		groupName := rest[0]
		contexts, _ := ParseContexts(rest[1:])
		if !subj.RemoveParent(groupName, contexts) {
			return src.SendMessage(Msg(Prefix+"&c%s &e%s&c does not inherit from '&e%s&c'.&r", strings.ToUpper(subj.Kind()[:1])+subj.Kind()[1:], subj.Name(), groupName))
		}
		if subj.IsUser() && strings.EqualFold(subj.PrimaryGroup(), groupName) {
			subj.SetPrimaryGroup("default")
		}
		if err := subj.Save(); err != nil {
			return src.SendMessage(Msg(Prefix+"&cFailed to save: %v&r", err))
		}
		mgr.LogAction(getActorName(src), subj.Kind(), subj.Name(), fmt.Sprintf("Removed parent group '%s'", groupName))
		return src.SendMessage(Msg(Prefix+"&aRemoved parent group &e%s&a from %s &e%s&a.&r", groupName, subj.Kind(), subj.Name()))

	case "switchprimarygroup":
		if !subj.IsUser() {
			return src.SendMessage(Msg(Prefix + "&cGroups do not have a primary group.&r"))
		}
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix + "&cUsage: /sp user <user> parent switchprimarygroup <group>&r"))
		}
		groupName := strings.ToLower(rest[0])
		if mgr.GetGroup(groupName) == nil {
			return src.SendMessage(Msg(Prefix+"&cGroup '&e%s&c' does not exist.&r", groupName))
		}
		now := time.Now()
		hasGlobalParent := false
		for _, p := range subj.Parents() {
			if strings.EqualFold(p.Group, groupName) && len(p.Contexts) == 0 && !p.IsExpired(now) {
				hasGlobalParent = true
				break
			}
		}
		if !hasGlobalParent {
			_, _ = subj.AddParentWithModifier(model.NewInheritance(groupName), model.TempModifierReplace, now)
		}
		subj.SetPrimaryGroup(groupName)
		if err := subj.Save(); err != nil {
			return src.SendMessage(Msg(Prefix+"&cFailed to save: %v&r", err))
		}
		mgr.LogAction(getActorName(src), "user", subj.Name(), fmt.Sprintf("Switched primary group to '%s'", groupName))
		return src.SendMessage(Msg(Prefix+"&aSwitched primary group for &e%s&a to &e%s&a!&r", subj.Name(), groupName))

	case "addtemp":
		if len(rest) < 2 {
			return src.SendMessage(Msg(Prefix+"&cUsage: /sp %s <%s> parent addtemp <group> <duration> [accumulate|replace|deny] [context...]&r", subj.Kind(), subj.Kind()))
		}
		groupName := strings.ToLower(rest[0])
		if mgr.GetGroup(groupName) == nil {
			return src.SendMessage(Msg(Prefix+"&cGroup '&e%s&c' does not exist.&r", groupName))
		}
		if !subj.IsUser() && strings.EqualFold(subj.Name(), groupName) {
			return src.SendMessage(Msg(Prefix + "&cA group cannot inherit from itself.&r"))
		}
		dur, mod, ctxRem, err := parseTempDurationAndMod(rest, 1)
		if err != nil {
			return src.SendMessage(Msg(Prefix+"&c%v&r", err))
		}
		contexts, _ := ParseContexts(ctxRem)

		now := time.Now()
		inh := model.NewTempInheritance(groupName, dur)
		inh.Expiry = now.Add(dur).Unix()
		inh.Contexts = contexts
		savedInh, err := subj.AddParentWithModifier(inh, mod, now)
		if err != nil {
			return src.SendMessage(Msg(Prefix+"&cCannot add temporary parent '&e%s&c': %v&r", groupName, err))
		}
		if err := subj.Save(); err != nil {
			return src.SendMessage(Msg(Prefix+"&cFailed to save: %v&r", err))
		}
		actualDur := time.Until(time.Unix(savedInh.Expiry, 0))
		mgr.LogAction(getActorName(src), subj.Kind(), subj.Name(), fmt.Sprintf("Added temp parent '%s' for %s (modifier=%s)", groupName, FormatDuration(actualDur), mod))
		return src.SendMessage(Msg(Prefix+"&aAdded temporary parent &e%s&a to %s &e%s&a (duration: &e%s&a, modifier: &b%s&a)!&r", groupName, subj.Kind(), subj.Name(), FormatDuration(actualDur), mod))

	case "removetemp":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix+"&cUsage: /sp %s <%s> parent removetemp <group> [context...]&r", subj.Kind(), subj.Kind()))
		}
		groupName := rest[0]
		contexts, _ := ParseContexts(rest[1:])
		if !subj.RemoveTempParent(groupName, contexts) {
			return src.SendMessage(Msg(Prefix+"&c%s &e%s&c has no matching temporary parent '&e%s&c'.&r", strings.ToUpper(subj.Kind()[:1])+subj.Kind()[1:], subj.Name(), groupName))
		}
		if err := subj.Save(); err != nil {
			return src.SendMessage(Msg(Prefix+"&cFailed to save: %v&r", err))
		}
		mgr.LogAction(getActorName(src), subj.Kind(), subj.Name(), fmt.Sprintf("Removed temporary parent '%s'", groupName))
		return src.SendMessage(Msg(Prefix+"&aRemoved temporary parent &e%s&a from %s &e%s&a.&r", groupName, subj.Kind(), subj.Name()))

	case "settrack":
		if len(rest) < 2 {
			return src.SendMessage(Msg(Prefix+"&cUsage: /sp %s <%s> parent settrack <track> <group> [context...]&r", subj.Kind(), subj.Kind()))
		}
		trackName := rest[0]
		targetGroup := rest[1]
		contexts, _ := ParseContexts(rest[2:])
		if err := subj.SetTrackGroup(mgr, trackName, targetGroup, contexts, getActorName(src)); err != nil {
			return src.SendMessage(Msg(Prefix+"&cFailed to set track group: %v&r", err))
		}
		return src.SendMessage(Msg(Prefix+"&aSet %s &e%s&a on track &b%s&a to &e%s&a %s&r", subj.Kind(), subj.Name(), trackName, strings.ToLower(targetGroup), FormatContexts(contexts)))

	case "cleartrack":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix+"&cUsage: /sp %s <%s> parent cleartrack <track> [context...]&r", subj.Kind(), subj.Kind()))
		}
		trackName := rest[0]
		contexts, _ := ParseContexts(rest[1:])
		removed, err := subj.ClearTrack(mgr, trackName, contexts, getActorName(src))
		if err != nil {
			return src.SendMessage(Msg(Prefix+"&cFailed to clear track: %v&r", err))
		}
		if !removed {
			return src.SendMessage(Msg(Prefix+"&7%s &e%s&7 has no parent groups on track &b%s&7 %s&r", subj.Kind(), subj.Name(), trackName, FormatContexts(contexts)))
		}
		return src.SendMessage(Msg(Prefix+"&aCleared track &b%s&a groups for %s &e%s&a %s&r", trackName, subj.Kind(), subj.Name(), FormatContexts(contexts)))

	case "clear":
		contexts, _ := ParseContexts(rest)
		if len(contexts) > 0 {
			var remaining []model.Inheritance
			for _, p := range subj.Parents() {
				if !model.SameContexts(p.Contexts, contexts) {
					remaining = append(remaining, p)
				}
			}
			subj.SetParents(remaining)
		} else {
			subj.SetParents(make([]model.Inheritance, 0))
			subj.SetPrimaryGroup("default")
		}
		if err := subj.Save(); err != nil {
			return src.SendMessage(Msg(Prefix+"&cFailed to save: %v&r", err))
		}
		mgr.LogAction(getActorName(src), subj.Kind(), subj.Name(), "Cleared parent groups")
		return src.SendMessage(Msg(Prefix+"&aCleared parent groups for %s &e%s&a.&r", subj.Kind(), subj.Name()))

	default:
		return src.SendMessage(Msg(Prefix + "&cUnknown parent action. Available: info, add, set, remove, addtemp, removetemp, settrack, cleartrack, clear.&r"))
	}
}

func handleSubjectMeta(mgr *manager.Manager, src command.Source, subj permSubject, args []string) error {
	if len(args) < 1 {
		return src.SendMessage(Msg(Prefix+"&cUsage: /sp %s <%s> meta <info|set|unset|settemp|unsettemp|addprefix|setprefix|addtempprefix|settempprefix|removeprefix|removetempprefix|addsuffix|setsuffix|addtempsuffix|settempsuffix|removesuffix|removetempsuffix|clear> [args...]&r", subj.Kind(), subj.Kind()))
	}

	action := strings.ToLower(args[0])
	rest := args[1:]

	switch action {
	case "info":
		subj.CleanExpired(time.Now())
		prefixVal, suffixVal := subj.ResolvedAffixes(mgr)
		lines := []string{
			Header(fmt.Sprintf("Meta: %s", subj.Name())),
			fmt.Sprintf("&bPrefix:&r &f%s&r", prefixVal),
			fmt.Sprintf("&bSuffix:&r &f%s&r", suffixVal),
			Separator(),
		}

		entries := subj.MetaEntries()
		metaMap := subj.MetaMap()
		coveredGlobal := make(map[string]bool)
		var customLines []string
		for _, me := range entries {
			if me.Expiry == 0 && len(me.Contexts) == 0 {
				coveredGlobal[strings.ToLower(me.Key)] = true
			}
			exp := ""
			if me.Expiry > 0 {
				exp = fmt.Sprintf(" &e(expires in %s)&r", FormatDuration(time.Until(time.Unix(me.Expiry, 0))))
			}
			customLines = append(customLines, fmt.Sprintf("  &6• &e%s&7: &f%s&r%s %s", me.Key, me.Value, exp, FormatContexts(me.Contexts)))
		}
		for k, v := range metaMap {
			if !coveredGlobal[strings.ToLower(k)] {
				customLines = append(customLines, fmt.Sprintf("  &6• &e%s&7: &f%s&r", k, v))
			}
		}
		lines = append(lines, SubHeader(fmt.Sprintf("Custom Keys (%d)", len(customLines))))
		if len(customLines) == 0 {
			lines = append(lines, "  &7(none)&r")
		} else {
			lines = append(lines, customLines...)
		}

		lines = append(lines, SubHeader(fmt.Sprintf("Prefixes (%d)", len(subj.Prefixes()))))
		for _, p := range subj.Prefixes() {
			exp := ""
			if p.Expiry > 0 {
				exp = fmt.Sprintf(" &e(expires in %s)&r", FormatDuration(time.Until(time.Unix(p.Expiry, 0))))
			}
			lines = append(lines, fmt.Sprintf("  &6• &b(priority %d)&r &f%s&r%s %s", p.Priority, p.Value, exp, FormatContexts(p.Contexts)))
		}

		lines = append(lines, SubHeader(fmt.Sprintf("Suffixes (%d)", len(subj.Suffixes()))))
		for _, sf := range subj.Suffixes() {
			exp := ""
			if sf.Expiry > 0 {
				exp = fmt.Sprintf(" &e(expires in %s)&r", FormatDuration(time.Until(time.Unix(sf.Expiry, 0))))
			}
			lines = append(lines, fmt.Sprintf("  &6• &b(priority %d)&r &f%s&r%s %s", sf.Priority, sf.Value, exp, FormatContexts(sf.Contexts)))
		}

		lines = append(lines, Separator())
		for _, l := range lines {
			_ = src.SendMessage(MsgPlain(l))
		}
		return nil

	case "set":
		if len(rest) < 2 {
			return src.SendMessage(Msg(Prefix+"&cUsage: /sp %s <%s> meta set <key> <value> [context...]&r", subj.Kind(), subj.Kind()))
		}
		key := rest[0]
		contexts, core := splitTrailingContexts(rest[1:])
		if len(core) == 0 {
			return src.SendMessage(Msg(Prefix+"&cUsage: /sp %s <%s> meta set <key> <value> [context...]&r", subj.Kind(), subj.Kind()))
		}
		val := strings.Join(core, " ")
		_, _ = subj.SetMetaEntry(model.MetaEntry{Key: key, Value: val, Contexts: contexts}, model.TempModifierReplace, time.Now())
		if err := subj.Save(); err != nil {
			return src.SendMessage(Msg(Prefix+"&cFailed to save: %v&r", err))
		}
		mgr.LogAction(getActorName(src), subj.Kind(), subj.Name(), fmt.Sprintf("Set meta '%s' = '%s' %s", key, val, FormatContexts(contexts)))
		return src.SendMessage(Msg(Prefix+"&aSet meta &e%s&a to '&f%s&a' for %s &e%s&a %s&r", key, val, subj.Kind(), subj.Name(), FormatContexts(contexts)))

	case "settemp":
		if len(rest) < 3 {
			return src.SendMessage(Msg(Prefix+"&cUsage: /sp %s <%s> meta settemp <key> <value> <duration> [accumulate|replace|deny] [context...]&r", subj.Kind(), subj.Kind()))
		}
		key := rest[0]
		contexts, core := splitTrailingContexts(rest[1:])
		mod := model.TempModifierReplace
		if len(core) >= 3 {
			if parsed, ok := model.ParseTempModifier(core[len(core)-1]); ok {
				mod = parsed
				core = core[:len(core)-1]
			}
		}
		if len(core) < 2 {
			return src.SendMessage(Msg(Prefix+"&cUsage: /sp %s <%s> meta settemp <key> <value> <duration> [accumulate|replace|deny] [context...]&r", subj.Kind(), subj.Kind()))
		}
		durStr := core[len(core)-1]
		dur, err := ParseDuration(durStr)
		if err != nil {
			return src.SendMessage(Msg(Prefix+"&cInvalid duration '%s': %v&r", durStr, err))
		}
		val := strings.Join(core[:len(core)-1], " ")
		now := time.Now()
		resEntry, err := subj.SetMetaEntry(model.MetaEntry{
			Key:      key,
			Value:    val,
			Expiry:   now.Add(dur).Unix(),
			Contexts: contexts,
		}, mod, now)
		if err != nil {
			return src.SendMessage(Msg(Prefix+"&cCannot set temporary meta '&e%s&c': %v&r", key, err))
		}
		if err := subj.Save(); err != nil {
			return src.SendMessage(Msg(Prefix+"&cFailed to save: %v&r", err))
		}
		actualDur := time.Until(time.Unix(resEntry.Expiry, 0))
		mgr.LogAction(getActorName(src), subj.Kind(), subj.Name(), fmt.Sprintf("Set temp meta '%s' = '%s' (%s, modifier=%s)", key, val, FormatDuration(actualDur), mod))
		return src.SendMessage(Msg(Prefix+"&aSet temp meta &e%s&a to '&f%s&a' for %s &e%s&a (duration: &e%s&a, modifier: &b%s&a) %s&r", key, val, subj.Kind(), subj.Name(), FormatDuration(actualDur), mod, FormatContexts(contexts)))

	case "unset":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix+"&cUsage: /sp %s <%s> meta unset <key> [context...]&r", subj.Kind(), subj.Kind()))
		}
		contexts, _ := ParseContexts(rest[1:])
		if !subj.UnsetMetaEntry(rest[0], contexts, false) {
			return src.SendMessage(Msg(Prefix+"&cMeta key '&e%s&c' is not set for %s &e%s&c.&r", rest[0], subj.Kind(), subj.Name()))
		}
		if err := subj.Save(); err != nil {
			return src.SendMessage(Msg(Prefix+"&cFailed to save: %v&r", err))
		}
		mgr.LogAction(getActorName(src), subj.Kind(), subj.Name(), fmt.Sprintf("Unset meta '%s'", rest[0]))
		return src.SendMessage(Msg(Prefix+"&aUnset meta key &e%s&a for %s &e%s&a.&r", rest[0], subj.Kind(), subj.Name()))

	case "unsettemp":
		if len(rest) < 1 {
			return src.SendMessage(Msg(Prefix+"&cUsage: /sp %s <%s> meta unsettemp <key> [context...]&r", subj.Kind(), subj.Kind()))
		}
		contexts, _ := ParseContexts(rest[1:])
		if !subj.UnsetMetaEntry(rest[0], contexts, true) {
			return src.SendMessage(Msg(Prefix+"&cNo temporary meta key '&e%s&c' set for %s &e%s&c.&r", rest[0], subj.Kind(), subj.Name()))
		}
		if err := subj.Save(); err != nil {
			return src.SendMessage(Msg(Prefix+"&cFailed to save: %v&r", err))
		}
		mgr.LogAction(getActorName(src), subj.Kind(), subj.Name(), fmt.Sprintf("Unset temp meta '%s'", rest[0]))
		return src.SendMessage(Msg(Prefix+"&aUnset temporary meta key &e%s&a for %s &e%s&a.&r", rest[0], subj.Kind(), subj.Name()))

	case "addprefix":
		return handleSubjectAffixSet(mgr, src, subj, false, false, false, rest)
	case "setprefix":
		return handleSubjectAffixSet(mgr, src, subj, false, true, false, rest)
	case "addtempprefix":
		return handleSubjectAffixSet(mgr, src, subj, false, false, true, rest)
	case "settempprefix":
		return handleSubjectAffixSet(mgr, src, subj, false, true, true, rest)
	case "removeprefix":
		return handleSubjectAffixRemove(mgr, src, subj, false, false, rest)
	case "removetempprefix":
		return handleSubjectAffixRemove(mgr, src, subj, false, true, rest)

	case "addsuffix":
		return handleSubjectAffixSet(mgr, src, subj, true, false, false, rest)
	case "setsuffix":
		return handleSubjectAffixSet(mgr, src, subj, true, true, false, rest)
	case "addtempsuffix":
		return handleSubjectAffixSet(mgr, src, subj, true, false, true, rest)
	case "settempsuffix":
		return handleSubjectAffixSet(mgr, src, subj, true, true, true, rest)
	case "removesuffix":
		return handleSubjectAffixRemove(mgr, src, subj, true, false, rest)
	case "removetempsuffix":
		return handleSubjectAffixRemove(mgr, src, subj, true, true, rest)

	case "clear":
		scope := "all"
		if len(rest) > 0 {
			scope = strings.ToLower(rest[0])
		}
		subj.ClearMeta(scope)
		if err := subj.Save(); err != nil {
			return src.SendMessage(Msg(Prefix+"&cFailed to save: %v&r", err))
		}
		mgr.LogAction(getActorName(src), subj.Kind(), subj.Name(), fmt.Sprintf("Cleared metadata (scope=%s)", scope))
		return src.SendMessage(Msg(Prefix+"&aCleared metadata (%s) for %s &e%s&a.&r", scope, subj.Kind(), subj.Name()))

	default:
		return src.SendMessage(Msg(Prefix + "&cUnknown meta action. Available: info, set, unset, settemp, unsettemp, addprefix, setprefix, addtempprefix, settempprefix, removeprefix, removetempprefix, addsuffix, setsuffix, addtempsuffix, settempsuffix, removesuffix, removetempsuffix, clear.&r"))
	}
}

func handleSubjectAffixSet(mgr *manager.Manager, src command.Source, subj permSubject, isSuffix, replaceCtx, temporary bool, rest []string) error {
	affixName := "prefix"
	if isSuffix {
		affixName = "suffix"
	}
	if len(rest) < 1 {
		return src.SendMessage(Msg(Prefix+"&cUsage: /sp %s <%s> meta %s [priority] <%s> ...&r", subj.Kind(), subj.Kind(), affixName, affixName))
	}

	contexts, core := splitTrailingContexts(rest)
	var dur time.Duration
	mod := model.TempModifierReplace
	if temporary {
		if len(core) >= 3 {
			if parsed, ok := model.ParseTempModifier(core[len(core)-1]); ok {
				mod = parsed
				core = core[:len(core)-1]
			}
		}
		if len(core) < 2 {
			return src.SendMessage(Msg(Prefix+"&cDuration and %s are required.&r", affixName))
		}
		parsedDur, err := ParseDuration(core[len(core)-1])
		if err != nil {
			return src.SendMessage(Msg(Prefix+"&cInvalid duration '%s': %v&r", core[len(core)-1], err))
		}
		dur = parsedDur
		core = core[:len(core)-1]
	}

	if len(core) == 0 {
		return src.SendMessage(Msg(Prefix+"&cMissing %s value.&r", affixName))
	}

	priority := 100
	value := strings.Join(core, " ")
	if len(core) > 1 {
		if p, err := strconv.Atoi(core[0]); err == nil {
			priority = p
			value = strings.Join(core[1:], " ")
		}
	}

	now := time.Now()
	pm := model.PriorityMeta{
		Priority: priority,
		Value:    value,
		Contexts: contexts,
	}
	if temporary {
		pm.Expiry = now.Add(dur).Unix()
	}

	if replaceCtx {
		if temporary && mod != model.TempModifierReplace {
			existing := subj.Prefixes()
			if isSuffix {
				existing = subj.Suffixes()
			}
			for _, cur := range existing {
				if cur.Priority == priority && !cur.IsExpired(now) && model.SameContexts(cur.Contexts, contexts) {
					if mod == model.TempModifierDeny {
						return src.SendMessage(Msg(Prefix+"&cCannot set temp %s: %v&r", affixName, model.ErrTempEntryExists))
					}
					if mod == model.TempModifierAccumulate && cur.Expiry > 0 {
						pm.Expiry = cur.Expiry + int64(dur.Seconds())
					}
					break
				}
			}
		}
		subj.SetAffixInContext(isSuffix, pm)
	} else {
		savedPM, err := subj.AddAffixWithModifier(isSuffix, pm, mod, now)
		if err != nil {
			return src.SendMessage(Msg(Prefix+"&cCannot add temp %s: %v&r", affixName, err))
		}
		pm = savedPM
	}

	if err := subj.Save(); err != nil {
		return src.SendMessage(Msg(Prefix+"&cFailed to save: %v&r", err))
	}

	if temporary {
		actualDur := time.Until(time.Unix(pm.Expiry, 0))
		mgr.LogAction(getActorName(src), subj.Kind(), subj.Name(), fmt.Sprintf("Set temp %s '%s' (priority %d, duration %s, mod %s)", affixName, value, priority, FormatDuration(actualDur), mod))
		return src.SendMessage(Msg(Prefix+"&aSet temp %s to '&f%s&a' (priority: &e%d&a, duration: &e%s&a) for %s &e%s&a %s&r", affixName, value, priority, FormatDuration(actualDur), subj.Kind(), subj.Name(), FormatContexts(contexts)))
	}
	mgr.LogAction(getActorName(src), subj.Kind(), subj.Name(), fmt.Sprintf("Set %s '%s' (priority %d)", affixName, value, priority))
	return src.SendMessage(Msg(Prefix+"&aSet %s to '&f%s&a' (priority: &e%d&a) for %s &e%s&a %s&r", affixName, value, priority, subj.Kind(), subj.Name(), FormatContexts(contexts)))
}

func handleSubjectAffixRemove(mgr *manager.Manager, src command.Source, subj permSubject, isSuffix, tempOnly bool, rest []string) error {
	affixName := "prefix"
	if isSuffix {
		affixName = "suffix"
	}
	if len(rest) < 1 {
		return src.SendMessage(Msg(Prefix+"&cUsage: /sp %s <%s> meta remove%s <priority> [value] [context...]&r", subj.Kind(), subj.Kind(), affixName))
	}
	contexts, core := splitTrailingContexts(rest)
	if len(core) < 1 {
		return src.SendMessage(Msg(Prefix + "&cPriority must be an integer.&r"))
	}
	priority, err := strconv.Atoi(core[0])
	if err != nil {
		return src.SendMessage(Msg(Prefix + "&cPriority must be an integer.&r"))
	}
	value := ""
	if len(core) > 1 {
		value = strings.Join(core[1:], " ")
	}
	if !subj.RemoveAffixMatch(isSuffix, priority, value, contexts, tempOnly) {
		return src.SendMessage(Msg(Prefix+"&cNo matching %s with priority &e%d&c found for %s &e%s&c.&r", affixName, priority, subj.Kind(), subj.Name()))
	}
	if err := subj.Save(); err != nil {
		return src.SendMessage(Msg(Prefix+"&cFailed to save: %v&r", err))
	}
	mgr.LogAction(getActorName(src), subj.Kind(), subj.Name(), fmt.Sprintf("Removed %s priority %d", affixName, priority))
	return src.SendMessage(Msg(Prefix+"&aRemoved %s with priority &e%d&a for %s &e%s&a.&r", affixName, priority, subj.Kind(), subj.Name()))
}
