package command

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	c "go.minekube.com/common/minecraft/component"
	"go.minekube.com/common/minecraft/component/codec/legacy"
)

var legacyCodec = &legacy.Legacy{Char: legacy.AmpersandChar}

// Msg converts a legacy ampersand-colored string to a Component, with optional sprintf formatting.
func Msg(msg string, a ...any) c.Component {
	if len(a) > 0 {
		msg = fmt.Sprintf(msg, a...)
	}
	comp, err := legacyCodec.Unmarshal([]byte(msg))
	if err != nil {
		return &c.Text{Content: msg}
	}
	return comp
}

// MsgPlain converts a pre-formatted string directly to a Component.
func MsgPlain(msg string) c.Component {
	comp, err := legacyCodec.Unmarshal([]byte(msg))
	if err != nil {
		return &c.Text{Content: msg}
	}
	return comp
}

// Msgf formats a legacy ampersand-colored string using fmt.Sprintf and converts it to a Component.
func Msgf(msg string, a ...any) c.Component {
	return Msg(msg, a...)
}

// ClickableCmd creates a text component with a hover tooltip and click event to insert the command into chat.
func ClickableCmd(displayCmd, insertCmd, description string) c.Component {
	cmdComp := MsgPlain(displayCmd)
	hoverText := MsgPlain(fmt.Sprintf("&eClick to insert into chat:\n&b%s\n&7%s", insertCmd, description))

	applyStyle := func(target c.Component) {
		if st := target.Style(); st != nil {
			st.ClickEvent = c.SuggestCommand(insertCmd)
			st.HoverEvent = c.ShowText(hoverText)
		}
	}

	applyStyle(cmdComp)
	for _, child := range cmdComp.Children() {
		applyStyle(child)
	}

	return &c.Text{
		Extra: []c.Component{
			MsgPlain("  "),
			cmdComp,
			MsgPlain("&7 - " + description + "&r"),
		},
	}
}

// ClickableRunCmd creates a text component with a hover tooltip and click event to directly execute the command on click.
func ClickableRunCmd(displayCmd, runCmd, description string) c.Component {
	cmdComp := MsgPlain(displayCmd)
	hoverText := MsgPlain(fmt.Sprintf("&aClick to execute:\n&b%s\n&7%s", runCmd, description))

	applyStyle := func(target c.Component) {
		if st := target.Style(); st != nil {
			st.ClickEvent = c.RunCommand(runCmd)
			st.HoverEvent = c.ShowText(hoverText)
		}
	}

	applyStyle(cmdComp)
	for _, child := range cmdComp.Children() {
		applyStyle(child)
	}

	return &c.Text{
		Extra: []c.Component{
			MsgPlain("  "),
			cmdComp,
			MsgPlain("&7 - " + description + "&r"),
		},
	}
}

// PageButton creates a styled clickable button to jump between help pages.
func PageButton(label, command, hoverText string) c.Component {
	btn := MsgPlain(label)
	applyStyle := func(target c.Component) {
		if st := target.Style(); st != nil {
			st.ClickEvent = c.RunCommand(command)
			st.HoverEvent = c.ShowText(MsgPlain(hoverText))
		}
	}
	applyStyle(btn)
	for _, child := range btn.Children() {
		applyStyle(child)
	}
	return btn
}

// Prefix returns the standard StonyPerms [SP] message prefix.
const Prefix = "&6[SP]&r "

// Header formats a LuckPerms-style section header.
func Header(title string) string {
	line := strings.Repeat("=", 16)
	return fmt.Sprintf("&b%s &e&l%s &b%s&r", line, title, line)
}

// SubHeader formats a subsection line.
func SubHeader(title string) string {
	return fmt.Sprintf("&3> &b&l%s:&r", title)
}

// Separator returns a horizontal divider bar.
func Separator() string {
	return "&8" + strings.Repeat("-", 42) + "&r"
}

// FormatBool returns colored true/false string.
func FormatBool(b bool) string {
	if b {
		return "&atrue&r"
	}
	return "&cfalse&r"
}

// FormatDuration formats a time.Duration into a clean human-readable string like "2d 4h 15m".
func FormatDuration(d time.Duration) string {
	if d <= 0 {
		return "expired"
	}
	d = d.Round(time.Second)
	days := d / (24 * time.Hour)
	d -= days * 24 * time.Hour
	hours := d / time.Hour
	d -= hours * time.Hour
	minutes := d / time.Minute
	d -= minutes * time.Minute
	seconds := d / time.Second

	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	if seconds > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%ds", seconds))
	}
	return strings.Join(parts, " ")
}

// ParseDuration parses human duration strings like "10s", "5m", "2h", "7d", "30d", "1y".
func ParseDuration(input string) (time.Duration, error) {
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return 0, fmt.Errorf("empty duration")
	}

	if strings.HasSuffix(input, "d") {
		daysStr := strings.TrimSuffix(input, "d")
		days, err := strconv.Atoi(daysStr)
		if err != nil {
			return 0, fmt.Errorf("invalid day duration: %s", input)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	if strings.HasSuffix(input, "w") {
		weeksStr := strings.TrimSuffix(input, "w")
		weeks, err := strconv.Atoi(weeksStr)
		if err != nil {
			return 0, fmt.Errorf("invalid week duration: %s", input)
		}
		return time.Duration(weeks) * 7 * 24 * time.Hour, nil
	}
	if strings.HasSuffix(input, "mo") {
		moStr := strings.TrimSuffix(input, "mo")
		mo, err := strconv.Atoi(moStr)
		if err != nil {
			return 0, fmt.Errorf("invalid month duration: %s", input)
		}
		return time.Duration(mo) * 30 * 24 * time.Hour, nil
	}
	if strings.HasSuffix(input, "y") {
		yStr := strings.TrimSuffix(input, "y")
		y, err := strconv.Atoi(yStr)
		if err != nil {
			return 0, fmt.Errorf("invalid year duration: %s", input)
		}
		return time.Duration(y) * 365 * 24 * time.Hour, nil
	}

	return time.ParseDuration(input)
}

// ParseContexts extracts key=value context pairs from command arguments.
func ParseContexts(args []string) (map[string]string, []string) {
	contexts := make(map[string]string)
	var remaining []string

	for _, arg := range args {
		if strings.Contains(arg, "=") {
			parts := strings.SplitN(arg, "=", 2)
			k := strings.TrimSpace(strings.ToLower(parts[0]))
			v := strings.TrimSpace(parts[1])
			if k != "" && v != "" {
				contexts[k] = v
				continue
			}
		}
		remaining = append(remaining, arg)
	}

	return contexts, remaining
}

// FormatContexts formats a context map into "[server=lobby, world=hub]".
func FormatContexts(contexts map[string]string) string {
	if len(contexts) == 0 {
		return ""
	}
	var pairs []string
	for k, v := range contexts {
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, v))
	}
	return "&8[" + strings.Join(pairs, ", ") + "]&r"
}
