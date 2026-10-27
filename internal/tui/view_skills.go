package tui

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/lkshrk/omni/internal/actions"
	"github.com/lkshrk/omni/internal/apm"
	"github.com/lkshrk/omni/internal/app"
	"github.com/lkshrk/omni/internal/text"
)

const (
	agentsMaxNoticeLines        = 3
	agentsMaxHarnessNoticeLines = 2
	iconDrifted                 = "≠"
)

const (
	agentsFallbackWorkspacePath = "~/.apm/apm.yml"
)

func agentsDetailPair(label, value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return label + ": " + value
}

// Detail order mirrors the tools tab: description first, then the row's metadata.
// The manifest author when the package declares one, else the owner its source is published under.
func agentsRowAuthor(row app.AgentsPackageRow) string {
	if author := strings.TrimSpace(row.Author); author != "" {
		return author
	}
	return agentsSourceOwner(row)
}

// A local checkout has no publisher, and a URL carries a host before the owner.
func agentsSourceOwner(row app.AgentsPackageRow) string {
	if row.Local() {
		return ""
	}
	source := strings.TrimSpace(row.Source)
	if _, after, ok := strings.Cut(source, "://"); ok {
		if _, path, hosted := strings.Cut(after, "/"); hosted {
			source = path
		} else {
			return ""
		}
	}
	owner, _, ok := strings.Cut(strings.TrimPrefix(source, "/"), "/")
	if !ok || owner == "" || owner == "." || owner == ".." {
		return ""
	}
	return owner
}

func agentsPackageDetails(row app.AgentsPackageRow) []string {
	out := []string{
		agentsDetailPair("source", row.Source),
		agentsDetailPair("author", row.Author),
	}
	if row.DeployedFiles > 0 {
		out = append(out, "files: "+strconv.Itoa(row.DeployedFiles))
	}
	provided := slices.Clone(row.Provides)
	slices.SortFunc(provided, func(a, b app.AgentsProvidedChild) int {
		kindRank := func(kind string) int {
			switch strings.ToLower(kind) {
			case "mcp":
				return 0
			case "lsp":
				return 1
			default:
				return 2
			}
		}
		if rank := kindRank(a.Kind) - kindRank(b.Kind); rank != 0 {
			return rank
		}
		if kind := strings.Compare(strings.ToLower(a.Kind), strings.ToLower(b.Kind)); kind != 0 {
			return kind
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	providedNames := make([]string, 0, len(provided))
	for _, child := range provided {
		providedNames = append(providedNames, strings.ToUpper(child.Kind)+" "+child.Name)
	}
	if len(providedNames) > 0 {
		out = append(out, "provides: "+strings.Join(providedNames, ", "))
	}
	issues := slices.Clone(row.Issues)
	slices.Sort(issues)
	if len(issues) > 0 {
		out = append(out, "issues: "+strings.Join(issues, ", "))
	}
	return out
}

const agentsNativeSectionTitle = "Not managed by APM"

func agentsNativeDetail(row app.AgentsNativeRow) string {
	if row.Ignored {
		return row.Kind + " · ignored"
	}
	return row.Kind
}

// Ignored rows read as orphaned rather than drifted: they are deliberately outside APM, not damaged.
func agentsNativeStatus(row app.AgentsNativeRow) app.AgentsPackageStatus {
	if row.Ignored {
		return app.AgentsPackageOrphaned
	}
	return app.AgentsPackageUnavailable
}

func agentsNativeDetails(row app.AgentsNativeRow) []string {
	state := "not declared in the host template"
	switch {
	case row.Ignored:
		state = "ignored" + agentsNativeReasonSuffix(row.Reason)
	case !row.Adoptable:
		state = "retained" + agentsNativeReasonSuffix(row.Reason)
	}
	return []string{
		agentsDetailPair("client", row.Target),
		agentsDetailPair("kind", row.Kind),
		agentsDetailPair("state", state),
		agentsDetailPair("read from", row.Source),
		agentsDetailPair("install root", row.InstallRoot),
	}
}

func agentsNativeReasonSuffix(reason string) string {
	if strings.TrimSpace(reason) == "" {
		return ""
	}
	return " — " + reason
}

func agentsServiceDetails(row app.AgentsServiceRow) []string {
	return []string{
		agentsDetailPair("transport", row.Detail),
		agentsDetailPair("command", row.Command),
		agentsDetailPair("host", row.URLHost),
		agentsDetailPair("deployed to", strings.Join(row.Harnesses, ",")),
		agentsDetailPair("targets", agentsTargetsText(row.Targets)),
	}
}

func agentsRegistryDetails(entry app.AgentsRegistryEntry) []string {
	return []string{
		agentsDetailPair("marketplace", entry.Marketplace),
		agentsDetailPair("version", entry.Version),
		agentsDetailPair("author", entry.Author),
		agentsDetailPair("spec", entry.Spec()),
	}
}

// Mirrors inlineDetailLines: the wrapped description, the metadata, the row's action error, then its hints.
func (m Model) agentsDetailBlock(description string, details []string, ctx hintContext) []string {
	p := m.palette
	prefix, hintPrefix := listTextPrefix(), listHintPrefix()
	wrapWidth := max(rowAvailableWidth(m.width)-lipgloss.Width(prefix), 20)

	var lines []string
	if description != "" {
		for _, wrapped := range text.WrapText(description, wrapWidth) {
			lines = append(lines, prefix+p.styleHelp.Render(wrapped))
		}
	} else if row, ok := m.agentsSelectedRow(); ctx == hintCtxAgentsRow && ok && row.kind == agentsRowPackage {
		lines = append(lines, prefix+p.styleNoDesc.Render("no description available"))
	}
	for _, detail := range details {
		if strings.TrimSpace(detail) == "" {
			continue
		}
		style := p.styleHelp
		if strings.HasPrefix(detail, "issues:") {
			style = p.styleOutdated
		}
		for _, wrapped := range text.WrapText(detail, wrapWidth) {
			lines = append(lines, prefix+style.Render(wrapped))
		}
	}
	if ctx == hintCtxAgentsRow {
		// A limitation is metadata about the row, so it lines up with the other detail lines, as a tool row's advisories do.
		for _, limitation := range agentsRowLimitations(m) {
			for _, wrapped := range text.WrapText(limitation, wrapWidth) {
				lines = append(lines, prefix+p.styleHintDesc.Render(wrapped))
			}
		}
	}
	var interaction string
	if ctx == hintCtxAgentsNativeRow && m.agentsConfirmIdx == m.agentsCursor {
		interaction = renderConfirmActionHints(m, hintPrefix, m.keys.AgentsRemove, actions.MustTUIConfirmDescription(actions.AgentsRemoveNative))
	} else if ctx == hintCtxAgentsRow && m.agentsConfirmIdx == m.agentsCursor {
		interaction = renderConfirmActionHints(m, hintPrefix, m.keys.AgentsRemove, "confirm uninstall")
	} else if ctx == hintCtxAgentsRow && m.apmRunning && m.agentsRowOpSpec != "" {
		if row, ok := m.agentsSelectedRow(); ok && row.kind == agentsRowPackage && agentsUninstallSpec(row.pkg) == m.agentsRowOpSpec {
			interaction = renderRowOperationStatusLine(m, prefix, m.spinner.View()+" running "+m.apmCommand+"…")
		}
	} else {
		interaction = renderContextHints(m, ctx, hintPrefix)
	}
	if interaction != "" {
		lines = append(lines, interaction)
	}
	return lines
}

// The failed op's message sits between the row and its details, as a tool row's action error does.
func (m Model) agentsRowErrorLines(spec string) []string {
	if spec == "" || spec != m.agentsRowOpSpec || m.apmErr == nil {
		return nil
	}
	prefix := listTextPrefix()
	wrapWidth := max(rowAvailableWidth(m.width)-lipgloss.Width(prefix), 20)
	var lines []string
	for _, wrapped := range text.WrapText(m.apmErr.Error(), wrapWidth) {
		lines = append(lines, prefix+m.palette.styleErr.Render(wrapped))
	}
	return lines
}

func renderAgentsFilterControl(m Model) string {
	return screenEdgeInset() + m.palette.styleNormal.Render("/") + " " + renderEmptyAwareTextInputView(m.palette, m.filter, m.filter.Placeholder, 0)
}

func agentsStatusGlyph(p palette, status app.AgentsPackageStatus) (string, lipgloss.Style) {
	switch status {
	case app.AgentsPackageInstalled:
		return iconInstalled, p.styleInstalled
	case app.AgentsPackageDrifted:
		return iconDrifted, p.styleWrongProv
	case app.AgentsPackageUnavailable:
		return iconWrongProv, p.styleOutdated
	case app.AgentsPackageMissing:
		return iconMissing, p.styleMissing
	default:
		return iconOrphan, p.styleOrphan
	}
}

// nil means defaults; an empty non-nil slice means declared targets exclude this surface.
func agentsTargetsText(targets []string) string {
	if targets == nil {
		return "all"
	}
	if len(targets) == 0 {
		return "none"
	}
	return strings.Join(targets, ",")
}

// A raw apm run carries no structured result, so its own marked output is the only summary available.
func apmMarkedLines(output string) []string {
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		if app.IsAPMNoticeLine(line) {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	return lines
}

// apm prints progress before its verdict, so severity, not order, decides which notices survive the cap.
func agentsNoticeRank(line string) int {
	line = strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(line, "[x]") || strings.HasPrefix(line, "error:"):
		return 0
	case (strings.HasPrefix(line, "[!]") && !strings.Contains(line, "User-scope primitives are fully supported")) ||
		strings.HasPrefix(line, "warning:"):
		return 1
	default:
		return 2
	}
}

func agentsNoticeText(line string) string {
	line = strings.TrimSpace(line)
	body := line
	if len(body) >= 4 && body[0] == '[' && body[2] == ']' {
		body = strings.TrimSpace(body[3:])
	}
	if strings.Contains(body, "files skipped") && strings.Contains(body, "local files exist, not managed by APM") {
		count := strings.Fields(body)
		if len(count) > 0 {
			return count[0] + " existing local files were left unchanged because APM does not own them; press e to review the trace log, and remove them only if APM should manage them"
		}
	}
	if strings.HasPrefix(line, "note:") {
		return strings.TrimSpace(strings.TrimPrefix(line, "note:")) + "; no action is needed unless APM should replace those files"
	}
	switch agentsNoticeRank(line) {
	case 0:
		return "Error: " + body
	case 1:
		return "Warning: " + body
	default:
		return body
	}
}

func agentsNoticeStyle(p palette, line string) lipgloss.Style {
	switch agentsNoticeRank(line) {
	case 0:
		return p.styleErr
	case 1:
		return p.styleOutdated
	default:
		return p.styleHelp
	}
}

func capAgentsNotices(notices []string) []string {
	ordered := make([]string, 0, len(notices))
	seen := make(map[string]bool, len(notices))
	for _, notice := range notices {
		if notice = strings.TrimSpace(notice); notice != "" && !seen[notice] {
			seen[notice] = true
			ordered = append(ordered, notice)
		}
	}
	slices.SortStableFunc(ordered, func(a, b string) int { return agentsNoticeRank(a) - agentsNoticeRank(b) })
	if len(ordered) <= agentsMaxNoticeLines {
		return ordered
	}
	remaining := len(ordered) - agentsMaxNoticeLines
	return append(ordered[:agentsMaxNoticeLines:agentsMaxNoticeLines], strconv.Itoa(remaining)+" more APM messages hidden; press e to view the full trace log")
}

// Harness notices never pass IsAPMNoticeLine, so they need their own capped feed rather than the apm one.
func agentsHarnessNoticeLines(notices []string) []string {
	if len(notices) <= agentsMaxHarnessNoticeLines {
		return notices
	}
	return append(slices.Clone(notices[:agentsMaxHarnessNoticeLines]), "…more harness notices")
}

// Notices are whole sentences, so they wrap rather than losing their tail to the clip every other line takes.
func (m Model) agentsWrappedNotice(text string, style lipgloss.Style) []string {
	pad := screenEdgeInset()
	width := max(m.width-lipgloss.Width(pad), 1)
	wrapped := strings.Split(lipgloss.NewStyle().Width(width).Render(strings.TrimSpace(text)), "\n")
	for i := range wrapped {
		wrapped[i] = strings.TrimRight(wrapped[i], " ")
	}
	out := make([]string, 0, len(wrapped))
	for _, line := range wrapped {
		out = append(out, style.Render(pad+line))
	}
	return out
}

// One body row, the footer separator, the command or status line, the summary,
// and the global hint bar.
const agentsNoticeReservedLines = 5

func (m Model) agentsAPMNoticeLines() []string {
	budget := max(listAvailableHeight(m)-agentsNoticeReservedLines, 1)
	var out []string
	for _, notice := range m.apmNotices {
		wrapped := m.agentsWrappedNotice(agentsNoticeText(notice), agentsNoticeStyle(m.palette, notice))
		if len(out)+len(wrapped) <= budget {
			out = append(out, wrapped...)
			continue
		}
		room := budget - len(out)
		if room > 1 {
			out = append(out, wrapped[:room-1]...)
		}
		hint := "e: full APM log"
		out = append(out, m.agentsWrappedNotice(hint, m.palette.styleHelp)[0])
		return out
	}
	return out
}

// The op block sits outside the scroll window, so a running sync stays visible however long the row list is.
// The footer reads as state first, then what is happening, then the summary,
// so the workspace's condition is not buried under whichever command last ran.
// What is happening. What is true sits under the workspace path, where the dots repo state sits.
func (m Model) agentsFooterLines() []string {
	lines := m.agentsActivityLines()
	if m.agentsRegistryMode {
		lines = append(lines, m.agentsSummaryLines()...)
	}
	return lines
}

// What is true about the workspace right now.
func (m Model) agentsStateLines() []string {
	p := m.palette
	var lines []string
	if cause, remedy := agentsReadinessGuidanceParts(m); cause != "" {
		style := p.styleHelp
		if m.agentsReadinessErr != nil || m.agentsReadiness.State == app.AgentsReadinessInvalid {
			style = p.styleErr
		}
		lines = append(lines, m.agentsWrappedNotice(cause, style)...)
		if remedy != "" {
			lines = append(lines, m.agentsWrappedNotice(remedy, p.styleHelp)...)
		}
	}
	for _, notice := range agentsHarnessNoticeLines(m.agentsNotices) {
		lines = append(lines, m.agentsWrappedNotice(notice, p.styleOutdated)...)
	}
	return lines
}

// What just happened, or is happening: commands, failures and checks.
func (m Model) agentsActivityLines() []string {
	p := m.palette
	pad := screenEdgeInset()
	var lines []string
	switch {
	case m.apmRunning && m.agentsRowOpSpec == "":
		lines = append(lines, p.styleStatus.Render(pad+m.spinner.View()+" running "+m.apmCommand+"…"))
	case m.apmCommand != "" && m.agentsRowOpSpec == "":
		mark := p.styleInstalled.Render(iconInstalled)
		if m.apmErr != nil {
			mark = p.styleMissing.Render(iconMissing)
		}
		lines = append(lines, pad+mark+" "+p.styleHelp.Render(m.apmCommand))
	}
	lines = append(lines, m.agentsAPMNoticeLines()...)
	// A row op's failure belongs to that row's detail block, not to the tab-wide footer.
	if m.apmErr != nil && m.agentsRowOpSpec == "" {
		lines = append(lines, m.agentsWrappedNotice(m.apmErr.Error(), p.styleErr)...)
	}
	lines = append(lines, m.agentsRemovalHintLines()...)
	if m.agentsRowsErr != nil {
		lines = append(lines, m.agentsWrappedNotice(m.agentsRowsErr.Error(), p.styleErr)...)
	}
	switch {
	case m.agentsOutdatedChecking:
		lines = append(lines, p.styleStatus.Render(pad+m.spinner.View()+" checking package updates…"))
	case m.agentsOutdatedErr != nil:
		lines = append(lines, m.agentsWrappedNotice("Update check failed: "+m.agentsOutdatedErr.Error()+"; press R to retry", p.styleErr)...)
	case m.agentsOutdatedUnknown > 0:
		lines = append(lines, p.styleHelp.Render(pad+strconv.Itoa(m.agentsOutdatedUnknown)+" package updates could not be checked  ·  R retry"))
	}
	return lines
}

func (m Model) agentsSummaryLines() []string {
	p := m.palette
	pad := screenEdgeInset()
	switch {
	case m.agentsRegistryMode:
		return []string{p.styleHelp.Render(pad + strconv.Itoa(len(m.agentsVisibleRegistry())) + "/" + strconv.Itoa(len(m.agentsRegistry)) + " plugins  ·  esc back")}
	case m.agentsRowsKnown:
		return []string{p.styleHelp.Render(pad + agentsSummaryText(m))}
	}
	return nil
}

func agentsReadinessGuidance(m Model) string {
	cause, remedy := agentsReadinessGuidanceParts(m)
	switch {
	case cause == "":
		return ""
	case remedy == "":
		return cause
	}
	return cause + " · " + remedy
}

// The footer gives the cause and the remedy their own lines; the status line joins them.
func agentsReadinessGuidanceParts(m Model) (cause, remedy string) {
	if m.agentsReadinessPending {
		return "Checking APM readiness…", ""
	}
	if m.agentsReadinessErr != nil {
		return "APM readiness check failed: " + m.agentsReadinessErr.Error(), "R recheck"
	}
	detail := strings.Join(m.agentsReadiness.Details, "; ")
	switch m.agentsReadiness.State {
	case app.AgentsReadinessEmpty:
		return firstNonEmpty(detail, "No APM workspace and no host template"), "commit a host template, then S sync"
	case app.AgentsReadinessTemplateOnly:
		return firstNonEmpty(detail, "APM template is staged but not installed"), "S sync"
	case app.AgentsReadinessLiveIncomplete:
		return firstNonEmpty(detail, "APM manifest has no lockfile"), "S sync"
	case app.AgentsReadinessInvalid:
		return firstNonEmpty(detail, "APM workspace is invalid"), "inspect APM files · R recheck"
	default:
		return "", ""
	}
}

// contents says what a row is and means the same thing in every section; publisher is the first to give way.
type agentsColWidths struct {
	name, publisher, contents, state, version, targets int
}

var agentsTableColumns = []tableColumn{
	{key: "name", seed: 20, align: rowCellAlignLeft},
	{key: "publisher", align: rowCellAlignRight},
	{key: "contents", align: rowCellAlignRight},
	{key: "state", align: rowCellAlignRight},
	{key: "version", align: rowCellAlignRight},
	{key: "targets", align: rowCellAlignRight},
}

// name gives up its comfortable width first but is crushed last; the
// right-hand columns may collapse away entirely.
var agentsShrinkLadder = []tableShrinkStep{
	shrinkStep("publisher", 0),
	shrinkStep("contents", 12),
	shrinkStep("name", 12),
	shrinkStep("targets", 6),
	shrinkStep("version", 7),
	shrinkStep("contents", 0),
	shrinkStep("targets", 0),
	shrinkStep("version", 0),
	shrinkStep("state", 0),
	shrinkStep("name", 8),
}

var agentsColumnsByKey = columnsByKey(agentsTableColumns)

func agentsColumnByKey(key string) tableColumn {
	return agentsColumnsByKey[key]
}

// state is the management status of a service row.
type agentsRowCells struct {
	name, publisher, contents, version, latest, state, targets string
	status                                                     app.AgentsPackageStatus
}

func agentsPackageCells(row app.AgentsPackageRow) agentsRowCells {
	return agentsRowCells{name: row.Name, publisher: agentsRowAuthor(row), contents: row.Contents(), version: row.Version,
		latest: row.LatestVersion, targets: agentsTargetsText(row.Targets), status: row.Status}
}

func agentsServiceCells(row app.AgentsServiceRow, contents string) agentsRowCells {
	cells := agentsRowCells{name: row.Name, contents: contents, targets: agentsTargetsText(row.Targets), status: row.Status}
	if row.Unmanaged {
		cells.state = "unmanaged"
	}
	return cells
}

func agentsNativeCells(row app.AgentsNativeRow) agentsRowCells {
	return agentsRowCells{name: row.Identity, contents: agentsNativeDetail(row), targets: row.Target, status: agentsNativeStatus(row)}
}

func agentsRefCells(ref agentsRowRef) agentsRowCells {
	switch ref.kind {
	case agentsRowService:
		return agentsServiceCells(ref.service, ref.serviceKind)
	case agentsRowNative:
		return agentsNativeCells(ref.native)
	default:
		return agentsPackageCells(ref.pkg)
	}
}

func (c agentsRowCells) measuredVersion() string {
	if c.latest != "" {
		return compactVersion(c.version) + " → " + compactVersion(c.latest)
	}
	return c.version
}

func agentsColumnWidths(m Model) agentsColWidths {
	var rows []agentsRowCells
	for _, section := range m.agentsRowSections() {
		for _, ref := range section.rows {
			rows = append(rows, agentsRefCells(ref))
		}
	}
	widths := measureTableColumns(agentsTableColumns, len(rows), func(i int, key string) string {
		switch key {
		case "name":
			return rows[i].name
		case "publisher":
			return rows[i].publisher
		case "contents":
			return rows[i].contents
		case "state":
			return rows[i].state
		case "version":
			return rows[i].measuredVersion()
		default:
			return rows[i].targets
		}
	})
	cols := agentsWidthsOf(widths)
	layout := agentsTableLayout()
	over := layout.iconWidth + layout.iconGap + cols.name + layout.columnGap + agentsRightGroupWidth(cols) - rowAvailableWidth(m.width)
	widths.fit(over, agentsShrinkLadder...)
	return agentsWidthsOf(widths)
}

func agentsWidthsOf(widths tableWidths) agentsColWidths {
	return agentsColWidths{name: widths["name"], publisher: widths["publisher"], contents: widths["contents"], state: widths["state"], version: widths["version"], targets: widths["targets"]}
}

func agentsRightGroupWidth(cols agentsColWidths) int {
	width := 0
	for _, w := range []int{cols.publisher, cols.contents, cols.state, cols.version, cols.targets} {
		if w > 0 {
			width += w + listColumnGap
		}
	}
	return max(width-listColumnGap, 0)
}

func (m Model) agentsRowLine(cells agentsRowCells, cols agentsColWidths, selected bool) string {
	p := m.palette
	glyph, glyphStyle := agentsStatusGlyph(p, cells.status)
	if cells.latest != "" {
		glyph, glyphStyle = iconOutdated, p.styleOutdated
	}
	nameStyle := p.styleNormal
	if selected {
		nameStyle = p.styleActiveText
	}
	widths := tableWidths{"name": cols.name, "publisher": cols.publisher, "contents": cols.contents, "state": cols.state, "version": cols.version, "targets": cols.targets}
	right := []rowCell{
		widths.cell(agentsColumnByKey("publisher"), cells.publisher, p.styleHelp),
		widths.cell(agentsColumnByKey("contents"), cells.contents, p.styleHelp),
		widths.cell(agentsColumnByKey("state"), cells.state, p.styleOrphan),
	}
	switch {
	case cells.latest != "" && cols.version > 0:
		current, upgrade := fitUpgradeVersionText(compactVersion(cells.version), compactVersion(cells.latest), cols.version)
		right = append(right, rightCell(p.styleVersionMuted.Render(current)+p.styleOutdated.Render(upgrade), cols.version))
	default:
		right = append(right, widths.cell(agentsColumnByKey("version"), cells.version, p.styleVersion))
	}
	right = append(right, widths.cell(agentsColumnByKey("targets"), cells.targets, p.styleProvider))
	left := []rowCell{
		leftCell(glyphStyle.Render(glyph), listIconWidth),
		widths.cell(agentsColumnByKey("name"), cells.name, nameStyle),
	}
	return listRowPrefix(p, selected) + renderTableRowBody(agentsTableLayout(), left, right, rowAvailableWidth(m.width))
}

func (m Model) viewSkillsBody() string {
	return renderSectionedTab(m, m.agentsSectionedTab())
}

func (m Model) agentsSectionedTab() sectionedTab {
	p := m.palette
	cols := agentsColumnWidths(m)
	cursor := clampIndex(m.agentsCursor, m.agentsRowCount())
	index := 0

	// The cursor row always carries its detail block, exactly as a tool or dot row does.
	section := func(title string, rows int, render func(i int, selected bool) string, block func(i int) ([]string, []string)) sectionedTabSection {
		out := sectionedTabSection{title: title}
		for i := 0; i < rows; i++ {
			selected := index == cursor && !m.cursorHidden
			row := sectionedTabRow{selected: selected, index: index, line: render(i, selected)}
			if selected {
				errorLines, details := block(i)
				row.details = append(errorLines, details...)
			}
			out.rows = append(out.rows, row)
			index++
		}
		return out
	}

	if m.agentsRegistryMode {
		return m.viewAgentsRegistryBody(section)
	}
	var sections []sectionedTabSection
	for _, group := range m.agentsRowSections() {
		rows := group.rows
		sections = append(sections, section(group.title, len(rows), func(i int, selected bool) string {
			return m.agentsRowLine(agentsRefCells(rows[i]), cols, selected)
		}, func(i int) ([]string, []string) {
			switch ref := rows[i]; ref.kind {
			case agentsRowService:
				return nil, m.agentsDetailBlock("", agentsServiceDetails(ref.service), hintCtxAgentsRow)
			case agentsRowNative:
				return nil, m.agentsDetailBlock("", agentsNativeDetails(ref.native), hintCtxAgentsNativeRow)
			default:
				return m.agentsRowErrorLines(agentsUninstallSpec(ref.pkg)), m.agentsDetailBlock(ref.pkg.Description, agentsPackageDetails(ref.pkg), hintCtxAgentsRow)
			}
		}))
	}
	sections = slices.DeleteFunc(sections, func(s sectionedTabSection) bool { return len(s.rows) == 0 })

	if len(sections) == 0 && m.agentsRowsKnown && m.agentsRowsErr == nil {
		empty := rowContentInset() + "Nothing declared in ~/.apm/apm.yml."
		if m.agentsFilterText() != "" {
			empty = rowContentInset() + "No rows match the filter."
		}
		sections = []sectionedTabSection{{empty: []string{p.styleHelp.Render(empty)}}}
	}

	return sectionedTab{
		pinnedTop: m.agentsPinnedTopLines(),
		top:       m.agentsBodyTopLines(),
		footer:    m.agentsFooterLines(),
		sections:  sections,
	}
}

// Mirrors the dots body: the search control, then the workspace path, then the first section.
// The filter control stays pinned: scrolling it out of view while filtering
// hides the thing being typed into.
func (m Model) agentsPinnedTopLines() []string {
	if !m.agentsSearchActive {
		return nil
	}
	return []string{renderAgentsFilterControl(m)}
}

func (m Model) agentsBodyTopLines() []string {
	var lines []string
	path := agentsWorkspacePath(m)
	if path != "" {
		width := max(rowAvailableWidth(m.width)-2, 1)
		lines = append(lines, m.palette.styleHelp.PaddingLeft(2).Render(truncatePath(tildePath(path), width)))
	}
	return append(lines, m.agentsStateLines()...)
}

// installed, missing, and orphaned always show so the three baseline counts never jump position.
var agentsAlwaysCounted = []app.AgentsPackageStatus{
	app.AgentsPackageInstalled,
	app.AgentsPackageMissing,
	app.AgentsPackageOrphaned,
}

func agentsSummaryText(m Model) string {
	packages := m.agentsVisiblePackages()
	mcp, lsp := m.agentsVisibleServices(m.agentsMCPRows), m.agentsVisibleServices(m.agentsLSPRows)
	counts := map[app.AgentsPackageStatus]int{}
	for _, row := range packages {
		counts[row.Status]++
	}
	for _, rows := range [][]app.AgentsServiceRow{mcp, lsp} {
		for _, row := range rows {
			counts[row.Status]++
		}
	}
	var parts []string
	for _, status := range app.AgentsStatusOrder {
		if counts[status] > 0 || slices.Contains(agentsAlwaysCounted, status) {
			parts = append(parts, strconv.Itoa(counts[status])+" "+string(status))
		}
	}
	return strings.Join(parts, "  ")
}

func (m Model) agentsUpdateCount() int {
	updates := 0
	for _, row := range m.agentsVisiblePackages() {
		if row.UpdateAvailable {
			updates++
		}
	}
	return updates
}

func (m Model) agentsRemovalHintLines() []string {
	out := make([]string, 0, len(m.agentsRemovalHint))
	for _, line := range m.agentsRemovalHint {
		if strings.TrimSpace(line) != "" {
			out = append(out, m.agentsWrappedNotice(line, m.palette.styleHelp)...)
		}
	}
	return out
}

func agentsWorkspacePath(m Model) string {
	if m.app == nil {
		return agentsFallbackWorkspacePath
	}
	if dir, err := apm.GlobalWorkspaceDir(); err == nil {
		return filepath.Join(dir, "apm.yml")
	}
	return agentsFallbackWorkspacePath
}

var agentsRegistryColumns = []tableColumn{
	{key: "name", seed: 20, align: rowCellAlignLeft},
	{key: "targets", align: rowCellAlignRight},
}

var agentsRegistryShrinkLadder = []tableShrinkStep{
	shrinkStep("name", 12),
	shrinkStep("targets", 6),
	shrinkStep("targets", 0),
	shrinkStep("name", 8),
}

func (m Model) viewAgentsRegistryBody(section func(string, int, func(int, bool) string, func(int) ([]string, []string)) sectionedTabSection) sectionedTab {
	p := m.palette
	entries := m.agentsVisibleRegistry()
	statusWidth := len(string(app.AgentsPackageInstalled))
	widths := measureTableColumns(agentsRegistryColumns, len(entries), func(i int, key string) string {
		if key == "name" {
			return entries[i].Name
		}
		return entries[i].Marketplace
	})
	cols := agentsColWidths{name: widths["name"], targets: widths["targets"]}
	layout := agentsTableLayout()
	over := layout.iconWidth + layout.iconGap + cols.name + layout.columnGap + agentsRightGroupWidth(cols) + statusWidth + layout.columnGap - rowAvailableWidth(m.width)
	widths.fit(over, agentsRegistryShrinkLadder...)
	cols = agentsColWidths{name: widths["name"], targets: widths["targets"]}

	rows := section("Registry", len(entries), func(i int, selected bool) string {
		entry := entries[i]
		status, glyph, style := "available", iconMissing, p.styleHelp
		if entry.Installed {
			status, glyph, style = string(app.AgentsPackageInstalled), iconInstalled, p.styleInstalled
		}
		nameStyle := p.styleNormal
		if selected {
			nameStyle = p.styleActiveText
		}
		return listRowPrefix(p, selected) + renderTableRowBody(agentsTableLayout(),
			[]rowCell{
				leftCell(style.Render(glyph), listIconWidth),
				leftCell(nameStyle.Render(fitCellText(entry.Name, cols.name)), cols.name),
			},
			[]rowCell{
				rightCell(style.Render(status), statusWidth),
				rightCell(p.styleProvider.Render(fitCellText(entry.Marketplace, cols.targets)), cols.targets),
			},
			rowAvailableWidth(m.width),
		)
	}, func(i int) ([]string, []string) {
		entry := entries[i]
		return nil, m.agentsDetailBlock(entry.Description, agentsRegistryDetails(entry), hintCtxAgentsRegistryRow)
	})

	sections := []sectionedTabSection{rows}
	if len(entries) == 0 {
		empty := rowContentInset() + "No plugins match the filter."
		if len(m.agentsRegistry) == 0 {
			empty = rowContentInset() + app.AgentsRegistryEmptyNotice
		}
		sections = []sectionedTabSection{{empty: []string{p.styleHelp.Render(empty)}}}
	}
	return sectionedTab{
		pinnedTop: m.agentsPinnedTopLines(),
		top:       m.agentsBodyTopLines(),
		footer:    m.agentsFooterLines(),
		sections:  sections,
	}
}
