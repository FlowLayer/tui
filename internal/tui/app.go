package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/FlowLayer/tui/internal/wsclient"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	defaultWidth      = 80
	defaultHeight     = 24
	leftPanelWidth    = 30
	bodyPanelsGap     = 1
	minimumTotalLines = 3
	actionFooterTTL   = 2 * time.Second
	serviceBusyMarker = "*"

	localLogCacheMultiplier = 10
	localLogCacheMinEntries = 1000
	localLogCacheMaxEntries = 10000

	allLogsName      = "all logs"
	footerCenterText = "flowlayer.tech • since 2026"
)

type panelFocus int

const (
	focusLeft panelFocus = iota
	focusRight
)

type serviceItem struct {
	name   string
	status string
}

type connectDoneMsg struct {
	result ConnectResult
	client *wsclient.Client
}

type wsEventMsg struct {
	event wsclient.Event
	ok    bool
}

type serviceLogsLoadedMsg struct {
	serviceName string
	result      ServiceLogsFetchResult
	requestSeq  int64
}

type replayLogsLoadedMsg struct {
	serviceName string
	result ServiceLogsFetchResult
}

type olderLogsLoadedMsg struct {
	serviceName string
	beforeSeq   int64
	result      ServiceLogsFetchResult
}

type serviceActionDoneMsg struct {
	action      ServiceAction
	result      ServiceActionResult
	serviceName string
}

type footerMessageExpiredMsg struct {
	token int
}

type headerKeyMap struct {
	switchPanel key.Binding
	navigate    key.Binding
	filter      key.Binding
	closeFilter key.Binding
	connection  key.Binding
	quit        key.Binding
}

func newHeaderKeyMap() headerKeyMap {
	return headerKeyMap{
		switchPanel: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "switch panel"),
		),
		navigate: key.NewBinding(
			key.WithKeys("up", "down"),
			key.WithHelp("↑↓", "navigate/scroll"),
		),
		filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter"),
		),
		closeFilter: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "close filter"),
		),
		connection: key.NewBinding(
			key.WithKeys("i"),
			key.WithHelp("i", "conn info"),
		),
		quit: key.NewBinding(
			key.WithKeys("q"),
			key.WithHelp("q", "quit"),
		),
	}
}

func (keys headerKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{keys.switchPanel, keys.navigate, keys.filter, keys.closeFilter, keys.connection, keys.quit}
}

func (keys headerKeyMap) LongHelp() [][]key.Binding {
	return [][]key.Binding{keys.ShortHelp()}
}

var (
	headerIdentityStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorHeaderIdentity).
				Background(colorPanelBackground)

	headerActionsStyle = lipgloss.NewStyle().
				Foreground(colorTextMuted).
				Background(colorPanelBackground).
				Padding(0, 1)

	headerActionStartStyle = headerActionsStyle.Copy().
				Foreground(colorStatusSuccess)

	headerActionStopStyle = headerActionsStyle.Copy().
				Foreground(colorStatusError)

	headerShortcutsStyle = lipgloss.NewStyle().
				Foreground(colorTextMuted).
				Background(colorPanelBackground)

	connectionInfoModalFrameStyle = lipgloss.NewStyle().
					Border(lipgloss.RoundedBorder()).
					BorderForeground(colorSelectedPanelBorder).
					BorderBackground(colorBackground).
					Background(colorPanelBackground).
					Foreground(colorTextDefault).
					Padding(1, 2)

	connectionInfoModalTitleStyle = lipgloss.NewStyle().
					Bold(true).
					Foreground(colorSelectedPanelTitle).
					Background(colorPanelBackground)

	connectionInfoModalLabelStyle = lipgloss.NewStyle().
					Foreground(colorTextMuted).
					Background(colorPanelBackground)

	connectionInfoModalHintStyle = lipgloss.NewStyle().
					Foreground(colorTextMuted).
					Background(colorPanelBackground)

	footerStyle = lipgloss.NewStyle().
			Foreground(colorTextMuted).
			Background(colorPanelBackground)

	panelFrameStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(colorUnselectedPanelBorder).
			BorderBackground(colorBackground)

	activePanelFrameStyle = panelFrameStyle.Copy().
				BorderForeground(colorSelectedPanelBorder)

	panelTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorUnselectedPanelTitle).
			Background(colorPanelBackground)

	activePanelTitleStyle = panelTitleStyle.Copy().
				Foreground(colorSelectedPanelTitle)

	panelBodyStyle = lipgloss.NewStyle().
			Foreground(colorTextDefault).
			Background(colorPanelBackground)

	filterStyle = lipgloss.NewStyle().
			Foreground(colorTextMuted).
			Background(colorPanelBackground)

	activeFilterStyle = filterStyle.Copy().
				Foreground(colorTextDefault)

	selectedServiceStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorTextDefault).
				Background(colorPanelSelectedBackground)

	emptyStateStyle = lipgloss.NewStyle().
			Foreground(colorTextMuted).
			Background(colorPanelBackground)

	logTimestampStyle = lipgloss.NewStyle().
				Foreground(colorTimestamp).
				Background(colorPanelBackground)

	logServiceStyle = lipgloss.NewStyle().
			Foreground(colorServiceName).
			Background(colorPanelBackground)

	logStderrStyle = lipgloss.NewStyle().
			Foreground(colorStatusError).
			Background(colorPanelBackground)

	statusReadyStyle = lipgloss.NewStyle().
				Foreground(colorStatusSuccess).
				Background(colorPanelBackground)

	statusRunningStyle = lipgloss.NewStyle().
				Foreground(colorServiceName).
				Background(colorPanelBackground)

	statusFailedStyle = lipgloss.NewStyle().
				Foreground(colorStatusError).
				Background(colorPanelBackground)

	statusMutedStyle = lipgloss.NewStyle().
				Foreground(colorTextMuted).
				Background(colorPanelBackground)

	scrollIndicatorStyle = lipgloss.NewStyle().
				Foreground(colorTextMuted).
				Background(colorPanelBackground)
)

type model struct {
	addr   string
	token  string
	client *wsclient.Client

	width  int
	height int

	connectionLabel      string
	showConnectionInfo   bool
	focus                panelFocus
	serviceItems         []serviceItem
	serviceSelection     int
	serviceFilter        string
	serviceFilterEdit    bool
	logsFilter           string
	logsFilterEdit       bool
	logsViewport         viewport.Model
	logsFollow           bool
	events               <-chan wsclient.Event
	lastSeq              int64
	seenLogSeqs          map[int64]struct{}
	replayPending        bool
	logsTruncated        bool
	loadingOlderLogs     bool
	noOlderLogsAvailable bool
	effectiveLogLimit    *int
	headerHelp           help.Model
	headerKeys           headerKeyMap
	logEntries           []ServiceLog
	footerMessage        string
	footerTransient      bool
	footerToken          int
	busyServices         map[string]bool
	restartingServices   map[string]bool
	globalActionInFlight bool
}

func newModel(addr, token string, client *wsclient.Client) model {

	headerHelp := help.New()
	headerHelp.ShowAll = false
	headerHelp.Styles.ShortKey = lipgloss.NewStyle().Foreground(colorTextMuted).Background(colorPanelBackground)
	headerHelp.Styles.ShortDesc = lipgloss.NewStyle().Foreground(colorTextMuted).Background(colorPanelBackground)
	headerHelp.Styles.ShortSeparator = lipgloss.NewStyle().Foreground(colorTextMuted).Background(colorPanelBackground)

	model := model{
		addr:               addr,
		token:              token,
		client:             client,
		width:              defaultWidth,
		height:             defaultHeight,
		connectionLabel:    "Connecting",
		headerHelp:         headerHelp,
		headerKeys:         newHeaderKeyMap(),
		focus:              focusLeft,
		serviceItems:       []serviceItem{},
		logsFollow:         true,
		logEntries:         []ServiceLog{},
		seenLogSeqs:        map[int64]struct{}{},
		busyServices:       map[string]bool{},
		restartingServices: map[string]bool{},
	}

	model.logsViewport = viewport.New(1, 1)
	model.logsViewport.Style = lipgloss.NewStyle().Background(colorPanelBackground)
	model.resizeLogsViewport()
	model.setLogsViewportContent()

	return model
}

func (model model) Init() tea.Cmd {
	return model.connectCmd()
}

func (model model) connectCmd() tea.Cmd {
	addr := model.addr
	token := model.token

	return func() tea.Msg {
		result, client := connectWSClient(context.Background(), addr, token)
		return connectDoneMsg{result: result, client: client}
	}
}

func (model model) readEventCmd() tea.Cmd {
	events := model.events

	return func() tea.Msg {
		if events == nil {
			return wsEventMsg{ok: false}
		}

		event, ok := <-events
		return wsEventMsg{event: event, ok: ok}
	}
}

func (model model) fetchServiceLogsCmd(serviceName string) tea.Cmd {
	client := model.client
	name := model.fetchRequestTargetForSelection(serviceName)
	requestSeq := model.lastSeq

	return func() tea.Msg {
		if client == nil {
			return serviceLogsLoadedMsg{serviceName: name, result: ServiceLogsFetchResult{Status: ServiceLogsFetchError}, requestSeq: requestSeq}
		}

		result := fetchLogs(context.Background(), client, name)
		return serviceLogsLoadedMsg{serviceName: name, result: result, requestSeq: requestSeq}
	}
}

func (model model) fetchGlobalLogsCmd() tea.Cmd {
	client := model.client
	serviceName := model.fetchRequestTargetForSelection(allLogsName)
	requestSeq := model.lastSeq

	return func() tea.Msg {
		if client == nil {
			return serviceLogsLoadedMsg{serviceName: allLogsName, result: ServiceLogsFetchResult{Status: ServiceLogsFetchError}, requestSeq: requestSeq}
		}

		result := fetchLogs(context.Background(), client, serviceName)
		return serviceLogsLoadedMsg{serviceName: allLogsName, result: result, requestSeq: requestSeq}
	}
}

func (model model) fetchReplayLogsCmd(afterSeq int64) tea.Cmd {
	client := model.client
	cursor := afterSeq
	serviceName := model.replayFetchRequestTarget()

	return func() tea.Msg {
		if client == nil {
			return replayLogsLoadedMsg{serviceName: serviceName, result: ServiceLogsFetchResult{Status: ServiceLogsFetchError}}
		}

		result := fetchLogsAfter(context.Background(), client, serviceName, cursor)
		return replayLogsLoadedMsg{serviceName: serviceName, result: result}
	}
}

// fetchOlderLogsCmd issues a backward-pagination request via before_seq.
// `serviceName` is the selected list entry (may be the synthetic "all logs"
// label) and `targetService` is the actual wire-protocol service filter.
// The request is bounded by the current effective limit so the server
// returns a usable page rather than the full configured maximum.
func (model model) fetchOlderLogsCmd(serviceName string, beforeSeq int64) tea.Cmd {
	client := model.client
	targetService := model.fetchRequestTargetForSelection(serviceName)
	pageLimit := 0
	if model.effectiveLogLimit != nil && *model.effectiveLogLimit > 0 {
		pageLimit = *model.effectiveLogLimit
	}

	return func() tea.Msg {
		if client == nil {
			return olderLogsLoadedMsg{serviceName: serviceName, beforeSeq: beforeSeq, result: ServiceLogsFetchResult{Status: ServiceLogsFetchError}}
		}
		result := fetchLogsBefore(context.Background(), client, targetService, beforeSeq, pageLimit)
		return olderLogsLoadedMsg{serviceName: serviceName, beforeSeq: beforeSeq, result: result}
	}
}

func (model model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	var shouldQuit bool
	var messageCmd tea.Cmd
	var skipViewportUpdate bool

	selectedBefore := model.selectedServiceName()

	switch message := message.(type) {
	case tea.WindowSizeMsg:
		model.width = maxInt(1, message.Width)
		model.height = maxInt(minimumTotalLines, message.Height)
		model.resizeLogsViewport()
		model.setLogsViewportContent()
	case connectDoneMsg:
		model.connectionLabel = connectionLabelForStatus(message.result.Status)
		if message.result.Status == StatusConnected {
			if message.client != nil {
				model.client = message.client
				model.events = message.client.Events()
				messageCmd = model.readEventCmd()
			}

			if len(message.result.Services) > 0 {
				model.serviceItems = serviceItemsFromServices(message.result.Services)
				model.serviceSelection = clampSelection(model.serviceSelection, len(model.filteredServices()))
				model.refreshBusyServicesFromItems()
				model.resetRestartingServices()
			}

			model.setPersistentFooter("connected")
		} else {
			model.client = nil
			model.events = nil
			model.serviceItems = []serviceItem{}
			model.serviceSelection = 0
			model.logEntries = nil
			model.logsTruncated = false
			model.setEffectiveLogLimit(nil)
			model.lastSeq = 0
			model.resetSeenLogSeqs()
			model.replayPending = false
			model.resetBusyServices()
			model.resetRestartingServices()
			model.setLogsViewportContent()
			model.setPersistentFooter(string(message.result.Status))
		}
	case wsEventMsg:
		if !message.ok {
			model.connectionLabel = "Disconnected"
			model.resetBusyServices()
			model.resetRestartingServices()
			model.client = nil
			model.events = nil
			model.replayPending = false
			model.setEffectiveLogLimit(nil)
			model.setPersistentFooter("connection closed")
			break
		}

		var replayCmd tea.Cmd

		switch strings.TrimSpace(message.event.Name) {
		case "hello":
			if _, ok := decodeHelloEvent(message.event); ok {
				model.connectionLabel = "Connected"
				model.replayPending = true
			}
		case "snapshot":
			if services, ok := decodeSnapshotEvent(message.event); ok {
				model.serviceItems = serviceItemsFromServices(services)
				model.serviceSelection = clampSelection(model.serviceSelection, len(model.filteredServices()))
				model.refreshBusyServicesFromItems()
				model.resetRestartingServices()
				if model.replayPending && model.lastSeq > 0 {
					replayCmd = model.fetchReplayLogsCmd(model.lastSeq)
				}
				model.replayPending = false
			}
		case "service_status":
			if service, ok := decodeServiceStatusEvent(message.event); ok {
				model.serviceItems = upsertServiceItem(model.serviceItems, service)
				model.serviceSelection = clampSelection(model.serviceSelection, len(model.filteredServices()))
				model.updateRestartingStateForService(service)
				model.updateBusyStateForService(service)
			}
		case "log":
			if logEntry, ok := decodeLogEvent(message.event); ok {
				model.updateLastSeq(logEntry.Seq)
				if model.appendLogEntryIfVisible(logEntry) {
					model.enforceVisibleLogBufferLimit()
					model.setLogsViewportContent()
				}
			}
		}

		messageCmd = batchCmds(model.readEventCmd(), replayCmd)
	case serviceLogsLoadedMsg:
		if message.result.Status == ServiceLogsFetchOK {
			loadedLogs := dedupeLogsBySeq(message.result.Logs)
			model.updateLastSeqFromLogs(loadedLogs)
			if message.serviceName == model.selectedServiceName() {
				loadedCount := len(loadedLogs)
				model.setEffectiveLogLimit(message.result.EffectiveLimit)
				liveLogs := filterVisibleLogsAfterSeq(model.logEntries, message.serviceName, message.requestSeq)
				model.logEntries = mergeLoadedAndLiveLogsWithinLimit(loadedLogs, liveLogs, model.effectiveLogLimit)
				model.logsTruncated = message.result.Truncated
				model.rebuildSeenLogSeqs()
				if loadedCount > 0 {
					// Keep freshly loaded history visible instead of immediately
					// snapping back to the live tail.
					model.logsFollow = false
				}
				model.setLogsViewportContent()
				if loadedCount > 0 {
					model.logsViewport.SetYOffset(0)
					messageCmd = batchCmds(messageCmd, model.setTransientFooter(fmt.Sprintf("loaded %d historical entries", loadedCount)))
				}
			}
		} else if message.serviceName == model.selectedServiceName() {
			// Distinguish deterministic protocol errors (the historical data is
			// genuinely gone or the request was malformed) from transient
			// failures (timeouts, network blips). For the latter, keep whatever
			// we already have visible and let live events keep flowing — wiping
			// the view on every transient hiccup is what made "all logs"
			// appear empty under load.
			switch message.result.Status {
			case ServiceLogsFetchBadRequest, ServiceLogsFetchUnknownService:
				model.logEntries = nil
				model.logsTruncated = false
				model.setEffectiveLogLimit(nil)
				model.resetSeenLogSeqs()
				model.setLogsViewportContent()
			default:
				// Transient: keep current entries, surface a hint.
				messageCmd = batchCmds(messageCmd, model.setTransientFooter("logs fetch failed (transient) — live updates continue"))
			}
		}
	case replayLogsLoadedMsg:
		currentSelection := model.selectedServiceName()
		if currentSelection == "" {
			break
		}
		if message.serviceName != model.fetchRequestTargetForSelection(currentSelection) {
			break
		}
		if message.result.Status != ServiceLogsFetchOK {
			break
		}

		model.setEffectiveLogLimit(message.result.EffectiveLimit)

		replayLogs := dedupeLogsBySeq(message.result.Logs)
		model.updateLastSeqFromLogs(replayLogs)

		appended := false
		for _, logEntry := range replayLogs {
			if model.appendLogEntryIfVisible(logEntry) {
				appended = true
			}
		}

		beforeTrimCount := len(model.logEntries)
		model.enforceVisibleLogBufferLimit()
		if appended || len(model.logEntries) != beforeTrimCount {
			model.setLogsViewportContent()
		}
	case olderLogsLoadedMsg:
		// Backward pagination: clear the in-flight flag, then merge older
		// entries into logEntries while preserving the user's scroll position
		// in the viewport. We do not flip `loadingOlderLogs` until we are
		// sure the response targets the currently-selected service so a
		// late-arriving response for a previous selection does not unblock
		// the new one.
		if message.serviceName != model.selectedServiceName() {
			break
		}
		model.loadingOlderLogs = false
		if message.result.Status != ServiceLogsFetchOK {
			// Best-effort: a failure here just means the user can retry by
			// scrolling up again. Keep the existing buffer as-is.
			break
		}

		olderLogs := dedupeLogsBySeq(message.result.Logs)
		// Drop entries we have already (defence against overlap / duplicate
		// frames). After dedupe, anything left is strictly older than what
		// we currently hold.
		newOlder := make([]ServiceLog, 0, len(olderLogs))
		for _, entry := range olderLogs {
			if _, seen := model.seenLogSeqs[entry.Seq]; seen {
				continue
			}
			newOlder = append(newOlder, entry)
		}

		if len(newOlder) == 0 {
			// Server has nothing older than what we already have — stop
			// firing requests for this selection.
			model.noOlderLogsAvailable = true
			break
		}

		// Capture the line count before we mutate the viewport content so we
		// can offset the viewport's YOffset by exactly the number of lines
		// that were prepended, keeping the user's anchor visible.
		previousLineCount := model.logsViewport.TotalLineCount()
		previousYOffset := model.logsViewport.YOffset

		merged := append([]ServiceLog{}, newOlder...)
		merged = append(merged, model.logEntries...)
		model.logEntries = merged
		model.rebuildSeenLogSeqs()
		model.setLogsViewportContent()

		newLineCount := model.logsViewport.TotalLineCount()
		delta := newLineCount - previousLineCount
		if delta > 0 {
			model.logsFollow = false
			model.logsViewport.SetYOffset(previousYOffset + delta)
		}
		messageCmd = batchCmds(messageCmd, model.setTransientFooter(fmt.Sprintf("loaded %d older entries", len(newOlder))))
	case serviceActionDoneMsg:
		model.updateRestartingStateForActionResult(message.action, message.serviceName, message.result)
		model.updateBusyStateForActionResult(message.serviceName, message.result)
		if isAllLogsItem(message.serviceName) {
			model.globalActionInFlight = false
		}
	case footerMessageExpiredMsg:
		if model.footerTransient && message.token == model.footerToken {
			model.clearTransientFooter()
		}
	case tea.KeyMsg:
		if model.showConnectionInfo {
			skipViewportUpdate = true
			if message.String() == "i" || message.Type == tea.KeyEsc {
				model.showConnectionInfo = false
			}
			break
		}

		switch message.String() {
		case "i":
			model.showConnectionInfo = true
			skipViewportUpdate = true
		case "q", "ctrl+c":
			shouldQuit = true
		case "tab":
			if model.focus == focusLeft {
				model.focus = focusRight
				model.serviceFilterEdit = false
			} else {
				model.focus = focusLeft
				model.logsFilterEdit = false
			}
		}

		if model.showConnectionInfo {
			break
		}

		if model.serviceFilterEdit {
			switch message.Type {
			case tea.KeyEsc:
				model.serviceFilterEdit = false
			case tea.KeyBackspace:
				model.serviceFilter = trimLastRune(model.serviceFilter)
				model.serviceSelection = clampSelection(model.serviceSelection, len(model.filteredServices()))
			case tea.KeyRunes:
				if len(message.Runes) > 0 {
					model.serviceFilter += string(message.Runes)
					model.serviceSelection = clampSelection(model.serviceSelection, len(model.filteredServices()))
				}
			}
		} else if model.logsFilterEdit {
			switch message.Type {
			case tea.KeyEsc:
				model.logsFilterEdit = false
			case tea.KeyBackspace:
				model.logsFilter = trimLastRune(model.logsFilter)
				model.setLogsViewportContent()
			case tea.KeyRunes:
				if len(message.Runes) > 0 {
					model.logsFilter += string(message.Runes)
					model.setLogsViewportContent()
				}
			}
		} else if model.focus == focusLeft {
			switch message.String() {
			case "/":
				model.serviceFilterEdit = true
			case "up":
				model.serviceSelection = moveSelection(model.serviceSelection, -1, len(model.filteredServices()))
			case "down":
				model.serviceSelection = moveSelection(model.serviceSelection, 1, len(model.filteredServices()))
			case "s", "S", "x", "X":
				selectedService, selected := model.selectedService()
				if !selected {
					model.setPersistentFooter("no service selected")
					break
				}

				if isAllLogsItem(selectedService.name) {
					if model.globalActionInFlight {
						break
					}
					switch strings.ToLower(strings.TrimSpace(message.String())) {
					case "s":
						model.globalActionInFlight = true
						messageCmd = model.globalActionCmd(ServiceActionStart)
					case "x":
						model.globalActionInFlight = true
						messageCmd = model.globalActionCmd(ServiceActionStop)
					}
					break
				}

				action, ok := actionForServiceKey(message.String(), selectedService.status)
				if !ok {
					break
				}

				if model.isServiceBusy(selectedService.name, selectedService.status) {
					break
				}

				if !canApplyServiceAction(action, selectedService.status) {
					break
				}

				model.markServiceBusy(selectedService.name)
				if action == ServiceActionRestart {
					model.markServiceRestarting(selectedService.name)
				}
				messageCmd = model.serviceActionCmd(action, selectedService.name)
			}
		} else if model.focus == focusRight {
			switch message.String() {
			case "/":
				model.logsFilterEdit = true
			case "up":
				model.logsFollow = false
			}
		}
	}

	selectedAfter := model.selectedServiceName()
	var selectionLogsCmd tea.Cmd
	if selectedAfter != selectedBefore {
		model.logEntries = nil
		model.resetSeenLogSeqs()
		model.logsTruncated = false
		model.loadingOlderLogs = false
		model.noOlderLogsAvailable = false
		model.setEffectiveLogLimit(nil)
		model.setLogsViewportContent()
		if selectedAfter == "" {
		} else if isAllLogsItem(selectedAfter) {
			selectionLogsCmd = model.fetchGlobalLogsCmd()
		} else {
			selectionLogsCmd = model.fetchServiceLogsCmd(selectedAfter)
		}
	}

	model.setLogsViewportKeyHandling(model.focus == focusRight && !model.logsFilterEdit && !model.showConnectionInfo)
	var viewportCmd tea.Cmd
	var olderLogsCmd tea.Cmd
	if !skipViewportUpdate {
		updatedViewport, command := model.logsViewport.Update(message)
		model.logsViewport = updatedViewport
		viewportCmd = command

		if keyMessage, ok := message.(tea.KeyMsg); ok {
			if model.focus == focusRight {
				switch keyMessage.String() {
				case "down":
					if model.logsViewport.AtBottom() {
						model.logsFollow = true
					}
				case "up", "pgup", "k":
					// Backward pagination: when the user scrolls past the
					// top of the buffer, fetch older entries via
					// before_seq. The viewport already absorbs the key
					// (above) so this only fires when AtTop reports true
					// _after_ the scroll attempt — i.e. there is no more
					// content above. We rely on the loadingOlderLogs flag
					// to coalesce repeated key-presses into one in-flight
					// request.
					if olderCmd, triggered := model.maybeRequestOlderLogs(); triggered {
						olderLogsCmd = olderCmd
					}
				}
			}
		}
	}

	if model.logsFollow {
		model.logsViewport.GotoBottom()
	}

	if shouldQuit {
		if model.client != nil {
			_ = model.client.Close()
			model.client = nil
			model.events = nil
		}

		if viewportCmd != nil {
			return model, tea.Batch(viewportCmd, tea.Quit)
		}
		return model, tea.Quit
	}

	return model, batchCmds(viewportCmd, messageCmd, selectionLogsCmd, olderLogsCmd)
}

func (model model) View() string {
	width := maxInt(1, model.width)
	height := maxInt(minimumTotalLines, model.height)
	bodyHeight := maxInt(1, height-2)

	header := model.renderHeader(width)
	body := model.renderBody(width, bodyHeight)
	footer := model.renderFooter(width)

	// Each sub-region is already filled to exact width with its own bg.
	// Join them and apply a final safety fill for the full surface.
	content := header + "\n" + body + "\n" + footer
	surface := fillBg(content, width, height, colorBackground)
	if model.showConnectionInfo {
		surface = overlayPlaced(surface, model.renderConnectionInfoModal(width), width, height)
	}

	return ensureBackgroundConsistency(surface)
}

func (model model) renderConnectionInfoModal(totalWidth int) string {
	const (
		connectionInfoModalMaxWidth             = 64
		connectionInfoModalPreferredOuterMargin = 8  // Keep roughly 4 cells of surrounding context on each side.
		connectionInfoModalMinimumReadableWidth = 34 // Below this, fallback to near full width to preserve readability.
		connectionInfoModalHorizontalChrome     = 6  // Border (1+1) + horizontal padding (2+2).
	)

	modalWidth := totalWidth - connectionInfoModalPreferredOuterMargin
	if modalWidth > connectionInfoModalMaxWidth {
		modalWidth = connectionInfoModalMaxWidth
	}
	if modalWidth < connectionInfoModalMinimumReadableWidth {
		modalWidth = totalWidth - 2
	}
	if modalWidth < 1 {
		modalWidth = 1
	}

	innerWidth := maxInt(1, modalWidth-connectionInfoModalHorizontalChrome)

	addressValue := strings.TrimSpace(model.addr)
	if addressValue == "" {
		addressValue = "(none)"
	}

	tokenValue := strings.TrimSpace(model.token)
	if tokenValue == "" {
		tokenValue = "(none)"
	}

	statusValue := strings.TrimSpace(model.connectionLabel)
	if statusValue == "" {
		statusValue = "(unknown)"
	}

	lines := []string{
		connectionInfoModalTitleStyle.Render(fitLine("Connection Info", innerWidth)),
		"",
		renderConnectionInfoRow("Address", addressValue, innerWidth),
		renderConnectionInfoRow("Token", tokenValue, innerWidth),
		renderConnectionInfoRow("Status", statusValue, innerWidth),
		"",
		connectionInfoModalHintStyle.Render(fitLine("i / esc  close", innerWidth)),
	}

	content := strings.Join(lines, "\n")
	return connectionInfoModalFrameStyle.
		Width(modalWidth).
		Render(content)
}

func renderConnectionInfoRow(label, value string, width int) string {
	const connectionInfoModalLabelWidth = 7 // "Address" is the longest label and defines the fixed column.

	valueText := sanitizeConnectionInfoValue(strings.TrimSpace(value))

	labelCell := connectionInfoModalLabelStyle.
		Width(connectionInfoModalLabelWidth).
		MaxWidth(connectionInfoModalLabelWidth).
		Render(fitLine(label, connectionInfoModalLabelWidth))

	valueWidth := maxInt(1, width-connectionInfoModalLabelWidth-2)
	valueCell := fitLine(valueText, valueWidth)

	return fillBg(labelCell+bgSpace(2, colorPanelBackground)+valueCell, width, 0, colorPanelBackground)
}

// sanitizeConnectionInfoValue strips ANSI CSI escape sequences (ESC [ ... final byte).
// This is intentionally scoped to CSI for current modal usage.
func sanitizeConnectionInfoValue(value string) string {
	if value == "" {
		return ""
	}

	var out strings.Builder

	for index := 0; index < len(value); {
		next, ok := ansiEscapeEnd(value, index)
		if ok {
			index = next
			continue
		}

		r, size := utf8.DecodeRuneInString(value[index:])
		if r == utf8.RuneError && size == 0 {
			break
		}

		out.WriteRune(r)
		index += size
	}

	return out.String()
}

func overlayPlaced(base, overlay string, width, height int) string {
	if width <= 0 || height <= 0 {
		return base
	}

	overlayWidth := lipgloss.Width(overlay)
	overlayHeight := lipgloss.Height(overlay)
	if overlayWidth <= 0 || overlayHeight <= 0 {
		return base
	}

	baseLines := strings.Split(fillBg(base, width, height, colorBackground), "\n")

	if overlayWidth > width {
		overlayWidth = width
	}
	if overlayHeight > height {
		overlayHeight = height
	}

	xOffset := 0
	if width > overlayWidth {
		xOffset = (width - overlayWidth) / 2
	}
	yOffset := 0
	if height > overlayHeight {
		yOffset = (height - overlayHeight) / 2
	}

	overlayLines := strings.Split(fillBg(overlay, overlayWidth, overlayHeight, colorPanelBackground), "\n")

	for row := 0; row < overlayHeight; row++ {
		baseRow := yOffset + row
		if baseRow < 0 || baseRow >= len(baseLines) || row >= len(overlayLines) {
			continue
		}

		center := overlayLines[row]
		centerWidth := lipgloss.Width(center)
		if centerWidth < overlayWidth {
			center = fillBg(center, overlayWidth, 0, colorPanelBackground)
			centerWidth = overlayWidth
		}
		if centerWidth > overlayWidth {
			center = sliceStyledByWidth(center, 0, overlayWidth)
			centerWidth = overlayWidth
		}

		prefix := sliceStyledByWidth(baseLines[baseRow], 0, xOffset)
		suffixStart := xOffset + centerWidth
		if suffixStart > width {
			suffixStart = width
		}
		suffix := sliceStyledByWidth(baseLines[baseRow], suffixStart, width)
		baseLines[baseRow] = fillBg(prefix+center+suffix, width, 0, colorBackground)
	}

	return strings.Join(baseLines, "\n")
}

func sliceStyledByWidth(line string, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end <= start {
		return ""
	}

	visible := 0
	index := 0
	capturing := false

	var prefixEsc strings.Builder
	var out strings.Builder

	for index < len(line) {
		next, ok := ansiEscapeEnd(line, index)
		if ok {
			seq := line[index:next]
			if capturing {
				out.WriteString(seq)
			} else {
				prefixEsc.WriteString(seq)
			}
			index = next
			continue
		}

		r, size := utf8.DecodeRuneInString(line[index:])
		if r == utf8.RuneError && size == 0 {
			break
		}

		runeWidth := lipgloss.Width(string(r))
		nextVisible := visible + runeWidth

		if !capturing && nextVisible > start {
			capturing = true
			out.WriteString(prefixEsc.String())
		}

		if capturing {
			if visible >= end {
				break
			}
			if nextVisible > start && visible < end {
				out.WriteRune(r)
			}
		}

		visible = nextVisible
		index += size

		if visible >= end && capturing {
			break
		}
	}

	if !capturing {
		return ""
	}

	out.WriteString("\x1b[0m")
	return out.String()
}

func ansiEscapeEnd(line string, start int) (int, bool) {
	if start+1 >= len(line) || line[start] != '\x1b' || line[start+1] != '[' {
		return 0, false
	}

	index := start + 2
	for index < len(line) {
		if line[index] >= 0x40 && line[index] <= 0x7e {
			return index + 1, true
		}
		index++
	}

	return len(line), true
}

func (model model) renderHeader(width int) string {
	if width <= 0 {
		return ""
	}

	leftWidth, rightWidth, gap := computeBodyPanelWidths(width)
	leftSegment := model.renderHeaderLeftSegment(leftWidth)
	rightSegment := model.renderHeaderRightSegment(rightWidth)

	line := leftSegment + rightSegment
	if gap > 0 {
		line = leftSegment + bgSpace(gap, colorPanelBackground) + rightSegment
	}

	return fillBg(line, width, 1, colorPanelBackground)
}

func (model model) renderHeaderLeftSegment(width int) string {
	leftText := " FlowLayer TUI " + model.connectionLabel + " "
	return renderHeaderTextSegment(headerIdentityStyle, leftText, width)
}

func (model model) renderHeaderRightSegment(width int) string {
	if width <= 0 {
		return ""
	}

	actions := model.renderHeaderActions()
	actionsWidth := lipgloss.Width(actions)
	if actionsWidth >= width {
		return renderHeaderTextSegment(headerActionsStyle, model.renderHeaderActionsFallbackText(), width)
	}

	remaining := width - actionsWidth
	if remaining <= 1 {
		return fillBg(actions, width, 0, colorPanelBackground)
	}

	shortcuts := model.renderHeaderShortcuts(remaining - 1)
	line := actions + bgSpace(1, colorPanelBackground) + shortcuts

	return fillBg(line, width, 0, colorPanelBackground)
}

func (model model) renderHeaderActions() string {
	sp := bgSpace(1, colorPanelBackground)
	return headerActionStartStyle.Render(model.headerStartActionLabel()) +
		sp +
		headerActionStopStyle.Render("X Stop")
}

func (model model) renderHeaderActionsFallbackText() string {
	return " " + model.headerStartActionLabel() + "  X Stop "
}

func (model model) headerStartActionLabel() string {
	if isAllLogsItem(model.selectedServiceName()) {
		return "S Start All"
	}
	return "S Start/Restart"
}

func (model model) renderHeaderShortcuts(width int) string {
	if width <= 0 {
		return ""
	}

	helpModel := model.headerHelp
	helpModel.Width = width
	helpText := helpModel.ShortHelpView(model.headerKeys.ShortHelp())

	// The help component inserts bare " " between key and description
	// (after an ANSI reset), which leaks the terminal background.
	// Patch these gaps with bg-styled spaces.
	helpText = strings.ReplaceAll(helpText, "\x1b[0m ", "\x1b[0m"+bgSpace(1, colorPanelBackground))

	return headerShortcutsStyle.
		Width(width).
		MaxWidth(width).
		Align(lipgloss.Right).
		Render(helpText)
}

func renderServiceRow(service serviceItem, width int, busy bool) string {
	name := "  " + service.name + "  "
	status := service.status
	busySuffix := serviceBusySuffix(busy)
	styledStatus := serviceStatusStyle(status).Render(status)
	plain := name + status + busySuffix
	pad := 0
	if lipgloss.Width(plain) < width {
		pad = width - lipgloss.Width(plain)
	}
	return panelBodyStyle.Render(name) + styledStatus + panelBodyStyle.Render(busySuffix) + bgSpace(pad, colorPanelBackground)
}

func serviceBusySuffix(busy bool) string {
	if !busy {
		return ""
	}
	return "  " + serviceBusyMarker
}

func serviceStatusStyle(status string) lipgloss.Style {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ready":
		return statusReadyStyle
	case "running":
		return statusRunningStyle
	case "failed":
		return statusFailedStyle
	default:
		return statusMutedStyle
	}
}

func renderHeaderTextSegment(style lipgloss.Style, text string, width int) string {
	if width <= 0 {
		return ""
	}

	return style.
		Width(width).
		MaxWidth(width).
		Render(fitLine(text, width))
}

func (model model) renderFooter(width int) string {
	message := strings.TrimSpace(model.footerMessage)
	if message == "" {
		message = "OK"
	}

	rightContent := model.logsFooterStatus()
	rightWidth := lipgloss.Width(rightContent)
	if rightWidth > width {
		rightContent = strings.TrimRight(fitLine(rightContent, width), " ")
		rightWidth = lipgloss.Width(rightContent)
	}

	leftMaxWidth := width - rightWidth
	if leftMaxWidth < 0 {
		leftMaxWidth = 0
	}
	leftContent := strings.TrimRight(fitLine(message, leftMaxWidth), " ")
	leftWidth := lipgloss.Width(leftContent)

	middleWidth := width - leftWidth - rightWidth
	if middleWidth < 0 {
		middleWidth = 0
	}
	centerContent := strings.TrimRight(fitLine(footerCenterText, middleWidth), " ")
	centerSegment := lipgloss.PlaceHorizontal(middleWidth, lipgloss.Center, centerContent)

	left := footerStyle.Render(leftContent)
	middle := footerStyle.Render(centerSegment)
	right := scrollIndicatorStyle.Render(rightContent)
	line := left + middle + right
	return fillBg(line, width, 1, colorPanelBackground)
}

func (model model) logsFooterStatus() string {
	parts := make([]string, 0, 2)
	if model.logsTruncated {
		parts = append(parts, "history truncated")
	}
	if indicator := model.logsScrollIndicator(); indicator != "" {
		parts = append(parts, indicator)
	}

	return strings.Join(parts, "  |  ")
}

func (model model) logsScrollIndicator() string {
	if len(model.logEntries) == 0 {
		return ""
	}

	total := model.logsViewport.TotalLineCount()
	if total == 0 {
		return ""
	}

	height := model.logsViewport.Height
	yOffset := model.logsViewport.YOffset

	firstLine := yOffset + 1
	lastLine := yOffset + height
	if lastLine > total {
		lastLine = total
	}

	percent := int(model.logsViewport.ScrollPercent() * 100)
	if percent > 100 {
		percent = 100
	}

	return fmt.Sprintf("%d-%d / %d  %d%%", firstLine, lastLine, total, percent)
}

func (model model) renderBody(width, height int) string {
	leftWidth, rightWidth, gap := computeBodyPanelWidths(width)

	leftPanel := model.renderLeftPanel(leftWidth, height, model.focus == focusLeft)
	rightPanel := model.renderLogsPanel(rightWidth, height, model.focus == focusRight)

	// Join panels side by side with a bg-filled gap, then fill the
	// entire body area line-by-line to prevent terminal bg leaks.
	leftLines := strings.Split(leftPanel, "\n")
	rightLines := strings.Split(rightPanel, "\n")
	spacer := bgSpace(gap, colorBackground)

	lines := make([]string, height)
	for i := 0; i < height; i++ {
		l := ""
		if i < len(leftLines) {
			l = leftLines[i]
		}
		r := ""
		if i < len(rightLines) {
			r = rightLines[i]
		}
		row := l + spacer + r
		lines[i] = row
	}

	return fillBg(strings.Join(lines, "\n"), width, height, colorBackground)
}

func (model model) renderLogsPanel(width, height int, active bool) string {
	width = maxInt(3, width)
	height = maxInt(3, height)

	innerWidth := maxInt(1, width-2)
	innerHeight := maxInt(1, height-2)
	contentHeight := maxInt(0, innerHeight-2)

	var titleLine string
	if active {
		titleLine = activePanelTitleStyle.Render(fitLine("Logs", innerWidth))
	} else {
		titleLine = panelTitleStyle.Render(fitLine("Logs", innerWidth))
	}

	filterValue := model.logsFilter
	if filterValue == "" {
		filterValue = "-"
	}
	if model.logsFilterEdit {
		filterValue += "_"
	}
	filterLine := fitLine("Filter: "+filterValue, innerWidth)
	filterRendered := filterStyle.Render(filterLine)
	if model.logsFilterEdit {
		filterRendered = activeFilterStyle.Render(filterLine)
	}

	contentBlock := ""
	if contentHeight > 0 {
		contentBlock = fillBg(model.logsViewport.View(), innerWidth, contentHeight, colorPanelBackground)
	}

	var parts []string
	parts = append(parts, titleLine)
	if innerHeight > 1 {
		parts = append(parts, filterRendered)
	}
	if contentHeight > 0 {
		parts = append(parts, contentBlock)
	}

	innerBlock := fillBg(strings.Join(parts, "\n"), innerWidth, innerHeight, colorPanelBackground)

	if active {
		return activePanelFrameStyle.Render(innerBlock)
	}
	return panelFrameStyle.Render(innerBlock)
}

func (model *model) setLogsViewportContent() {
	entries := model.filteredLogEntries()
	if len(entries) == 0 {
		model.logsViewport.SetContent("No logs")
	} else {
		// logsViewport.Width is the inner content width of the logs panel
		// (panel outer width − 2 for the border on each side).
		innerWidth := model.logsViewport.Width
		showService := isAllLogsItem(model.selectedServiceName())
		// SERVICE width is derived from the actual labels present in this render
		// batch so the column takes only the space the content requires.
		serviceWidth := 0
		if showService {
			serviceWidth = computeServiceWidth(entries)
		}
		lines := make([]string, len(entries))
		for i, entry := range entries {
			lines[i] = renderLogRow(entry, innerWidth, showService, serviceWidth)
		}
		model.logsViewport.SetContent(strings.Join(lines, "\n"))
	}
	if model.logsFollow {
		model.logsViewport.GotoBottom()
	}
}

func (model *model) setLogsViewportKeyHandling(enabled bool) {
	model.logsViewport.KeyMap.Up.SetEnabled(enabled)
	model.logsViewport.KeyMap.Down.SetEnabled(enabled)
	model.logsViewport.KeyMap.PageUp.SetEnabled(enabled)
	model.logsViewport.KeyMap.PageDown.SetEnabled(enabled)
	model.logsViewport.KeyMap.HalfPageUp.SetEnabled(enabled)
	model.logsViewport.KeyMap.HalfPageDown.SetEnabled(enabled)
}

func (model *model) resizeLogsViewport() {
	bodyHeight := maxInt(1, model.height-2)
	_, rightWidth, _ := computeBodyPanelWidths(maxInt(1, model.width))
	rightPanelHeight := maxInt(3, bodyHeight)

	innerWidth := maxInt(1, rightWidth-2)
	innerHeight := maxInt(1, rightPanelHeight-2)
	contentHeight := maxInt(0, innerHeight-2)

	model.logsViewport.Width = innerWidth
	model.logsViewport.Height = contentHeight
}

func (model model) filteredLogEntries() []ServiceLog {
	needle := strings.ToLower(strings.TrimSpace(model.logsFilter))
	if needle == "" {
		return model.logEntries
	}

	filtered := make([]ServiceLog, 0, len(model.logEntries))
	for _, entry := range model.logEntries {
		if strings.Contains(strings.ToLower(entry.Message), needle) ||
			strings.Contains(strings.ToLower(entry.Phase), needle) ||
			strings.Contains(strings.ToLower(entry.Service), needle) ||
			strings.Contains(strings.ToLower(entry.Stream), needle) {
			filtered = append(filtered, entry)
		}
	}

	return filtered
}

func computeBodyPanelWidths(width int) (int, int, int) {
	gap := bodyPanelsGap
	if width <= 2 {
		gap = 0
	}

	leftWidth := leftPanelWidth
	if leftWidth > width-gap-1 {
		leftWidth = maxInt(1, (width-gap)/2)
	}

	rightWidth := width - leftWidth - gap
	if rightWidth < 1 {
		rightWidth = 1
		leftWidth = maxInt(1, width-gap-rightWidth)
	}

	return leftWidth, rightWidth, gap
}

func (model model) renderLeftPanel(width, height int, active bool) string {
	width = maxInt(3, width)
	height = maxInt(3, height)

	innerWidth := maxInt(1, width-2)
	innerHeight := maxInt(1, height-2)

	services := model.filteredServices()
	selectedIndex := clampSelection(model.serviceSelection, len(services))

	renderedLines := make([]string, 0, innerHeight)
	if active {
		renderedLines = append(renderedLines, activePanelTitleStyle.Render(fitLine("Services", innerWidth)))
	} else {
		renderedLines = append(renderedLines, panelTitleStyle.Render(fitLine("Services", innerWidth)))
	}

	filterValue := model.serviceFilter
	if filterValue == "" {
		filterValue = "-"
	}
	if model.serviceFilterEdit {
		filterValue += "_"
	}
	filterLine := fitLine("Filter: "+filterValue, innerWidth)
	if model.serviceFilterEdit {
		renderedLines = append(renderedLines, activeFilterStyle.Render(filterLine))
	} else {
		renderedLines = append(renderedLines, filterStyle.Render(filterLine))
	}

	if len(services) == 0 {
		if len(renderedLines) < innerHeight {
			renderedLines = append(renderedLines, emptyStateStyle.Render(fitLine("No services", innerWidth)))
		}
	} else {
		for index, service := range services {
			if len(renderedLines) >= innerHeight {
				break
			}
			displayedService := model.displayServiceItem(service)
			busy := model.isServiceBusy(service.name, service.status)
			if index == selectedIndex {
				line := fitLine("- "+displayedService.name+"  "+displayedService.status+serviceBusySuffix(busy), innerWidth)
				renderedLines = append(renderedLines, selectedServiceStyle.Width(innerWidth).Render(line))
				continue
			}
			renderedLines = append(renderedLines, renderServiceRow(displayedService, innerWidth, busy))
		}
	}

	for len(renderedLines) < innerHeight {
		renderedLines = append(renderedLines, panelBodyStyle.Render(fitLine("", innerWidth)))
	}

	innerBlock := fillBg(strings.Join(renderedLines, "\n"), innerWidth, innerHeight, colorPanelBackground)

	if active {
		return activePanelFrameStyle.Render(innerBlock)
	}
	return panelFrameStyle.Render(innerBlock)
}

func (model model) filteredServices() []serviceItem {
	if len(model.serviceItems) == 0 {
		return nil
	}

	items := make([]serviceItem, 0, len(model.serviceItems)+1)
	items = append(items, serviceItem{name: allLogsName})
	items = append(items, model.serviceItems...)

	needle := strings.ToLower(strings.TrimSpace(model.serviceFilter))
	if needle == "" {
		return items
	}

	filtered := make([]serviceItem, 0, len(items))
	for _, service := range items {
		if strings.Contains(strings.ToLower(service.name), needle) || strings.Contains(strings.ToLower(service.status), needle) {
			filtered = append(filtered, service)
		}
	}

	return filtered
}

func (model model) selectedService() (serviceItem, bool) {
	services := model.filteredServices()
	if len(services) == 0 {
		return serviceItem{}, false
	}

	index := clampSelection(model.serviceSelection, len(services))
	return services[index], true
}

func (model model) selectedServiceName() string {
	service, ok := model.selectedService()
	if !ok {
		return ""
	}
	return strings.TrimSpace(service.name)
}

func (model model) fetchRequestTargetForSelection(selectionName string) string {
	trimmedSelection := strings.TrimSpace(selectionName)
	if trimmedSelection == "" || isAllLogsItem(trimmedSelection) {
		return ""
	}
	return trimmedSelection
}

func (model model) replayFetchRequestTarget() string {
	return model.fetchRequestTargetForSelection(model.selectedServiceName())
}

func connectionLabelForStatus(status ConnectionStatus) string {
	switch status {
	case StatusConnected:
		return "Connected"
	case StatusTokenRequired:
		return "Token Required"
	case StatusInvalidToken:
		return "Invalid Token"
	case StatusUnreachable:
		return "Unreachable"
	case StatusInvalidAddress:
		return "Invalid Address"
	default:
		return "Disconnected"
	}
}

func serviceItemsFromServices(services []Service) []serviceItem {
	items := make([]serviceItem, 0, len(services))
	for _, service := range services {
		name := strings.TrimSpace(service.Name)
		if name == "" {
			continue
		}
		items = append(items, serviceItem{name: name, status: strings.TrimSpace(service.Status)})
	}
	return items
}

func upsertServiceItem(items []serviceItem, service Service) []serviceItem {
	name := strings.TrimSpace(service.Name)
	if name == "" {
		return items
	}

	status := strings.TrimSpace(service.Status)
	for index := range items {
		if items[index].name == name {
			items[index].status = status
			return items
		}
	}

	return append(items, serviceItem{name: name, status: status})
}

func (model model) displayServiceItem(service serviceItem) serviceItem {
	displayed := service
	displayed.status = model.displayServiceStatus(service)
	return displayed
}

func (model model) displayServiceStatus(service serviceItem) string {
	status := strings.TrimSpace(service.status)
	if model.isServiceRestarting(service.name) && (strings.EqualFold(status, "ready") || strings.EqualFold(status, "running")) {
		return "restarting"
	}
	return status
}

func filterEmptyLogs(logs []ServiceLog) []ServiceLog {
	filtered := make([]ServiceLog, 0, len(logs))
	for _, entry := range logs {
		if !isEmptyLogEntry(entry) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func isEmptyLogEntry(entry ServiceLog) bool {
	return strings.TrimSpace(entry.Message) == ""
}

func dedupeLogsBySeq(logs []ServiceLog) []ServiceLog {
	filtered := make([]ServiceLog, 0, len(logs))
	seen := make(map[int64]struct{}, len(logs))

	for _, entry := range logs {
		if isEmptyLogEntry(entry) {
			continue
		}

		if entry.Seq > 0 {
			if _, exists := seen[entry.Seq]; exists {
				continue
			}
			seen[entry.Seq] = struct{}{}
		}

		filtered = append(filtered, entry)
	}

	return filtered
}

func filterVisibleLogsAfterSeq(logs []ServiceLog, selectedName string, afterSeq int64) []ServiceLog {
	if len(logs) == 0 {
		return nil
	}

	filtered := make([]ServiceLog, 0, len(logs))
	for _, entry := range logs {
		if entry.Seq <= afterSeq {
			continue
		}
		if !shouldDisplayLogForSelection(entry, selectedName) {
			continue
		}
		filtered = append(filtered, entry)
	}

	return filtered
}

func mergeLoadedAndLiveLogs(loadedLogs []ServiceLog, liveLogs []ServiceLog) []ServiceLog {
	combined := make([]ServiceLog, 0, len(loadedLogs)+len(liveLogs))
	combined = append(combined, loadedLogs...)
	combined = append(combined, liveLogs...)
	return dedupeLogsBySeq(combined)
}

func mergeLoadedAndLiveLogsWithinLimit(loadedLogs []ServiceLog, liveLogs []ServiceLog, effectiveLimit *int) []ServiceLog {
	loaded := dedupeLogsBySeq(loadedLogs)
	live := dedupeLogsBySeq(liveLogs)

	if len(loaded) > 0 && len(live) > 0 {
		seenLoadedSeqs := make(map[int64]struct{}, len(loaded))
		for _, entry := range loaded {
			if entry.Seq <= 0 {
				continue
			}
			seenLoadedSeqs[entry.Seq] = struct{}{}
		}

		filteredLive := make([]ServiceLog, 0, len(live))
		for _, entry := range live {
			if entry.Seq > 0 {
				if _, exists := seenLoadedSeqs[entry.Seq]; exists {
					continue
				}
			}
			filteredLive = append(filteredLive, entry)
		}
		live = filteredLive
	}

	merged := mergeLoadedAndLiveLogs(loaded, live)
	if effectiveLimit == nil || *effectiveLimit <= 0 || len(merged) <= *effectiveLimit {
		return merged
	}

	limit := *effectiveLimit
	if len(loaded) == 0 || len(live) == 0 {
		return trimLogEntriesToMaxEntries(merged, limit)
	}

	historyBudget := minInt(len(loaded), maxInt(1, limit/2))
	if len(live) < limit {
		historyBudget = minInt(len(loaded), limit-len(live))
		if limit > 1 && historyBudget == 0 {
			historyBudget = 1
		}
	}

	if limit > 1 && historyBudget >= limit {
		historyBudget = limit - 1
	}

	liveBudget := limit - historyBudget
	if liveBudget > len(live) {
		liveBudget = len(live)
		historyBudget = minInt(len(loaded), limit-liveBudget)
	}

	if limit > 1 && liveBudget == 0 && len(live) > 0 {
		liveBudget = 1
		historyBudget = minInt(len(loaded), limit-liveBudget)
	}
	if limit > 1 && historyBudget == 0 && len(loaded) > 0 {
		historyBudget = 1
		liveBudget = minInt(len(live), limit-historyBudget)
	}

	historyTail := trimLogEntriesToMaxEntries(loaded, historyBudget)
	liveTail := trimLogEntriesToMaxEntries(live, liveBudget)
	return trimLogEntriesToMaxEntries(mergeLoadedAndLiveLogs(historyTail, liveTail), limit)
}

func trimLogEntriesToMaxEntries(logs []ServiceLog, maxEntries int) []ServiceLog {
	if maxEntries <= 0 || len(logs) <= maxEntries {
		return logs
	}

	trimmed := make([]ServiceLog, maxEntries)
	copy(trimmed, logs[len(logs)-maxEntries:])
	return trimmed
}

func trimLogEntriesToEffectiveLimit(logs []ServiceLog, effectiveLimit *int) []ServiceLog {
	if effectiveLimit == nil {
		return logs
	}
	return trimLogEntriesToMaxEntries(logs, *effectiveLimit)
}

func shouldDisplayLogForSelection(entry ServiceLog, selectedName string) bool {
	selection := strings.TrimSpace(selectedName)
	if selection == "" {
		return false
	}
	if isAllLogsItem(selection) {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(entry.Service), selection)
}

func (model *model) resetSeenLogSeqs() {
	model.seenLogSeqs = map[int64]struct{}{}
}

func (model *model) rebuildSeenLogSeqs() {
	model.resetSeenLogSeqs()
	for _, entry := range model.logEntries {
		if entry.Seq <= 0 {
			continue
		}
		model.seenLogSeqs[entry.Seq] = struct{}{}
	}
}

func (model *model) updateLastSeq(seq int64) {
	if seq > model.lastSeq {
		model.lastSeq = seq
	}
}

func (model *model) updateLastSeqFromLogs(logs []ServiceLog) {
	for _, entry := range logs {
		model.updateLastSeq(entry.Seq)
	}
}

// maybeRequestOlderLogs decides whether to issue a backward-pagination
// request for the currently selected service. Returns the command to run
// (nil if nothing should be done) and a flag indicating whether a request
// was scheduled. The caller is expected to have just observed a scroll-up
// key event in the right pane.
//
// We only fire the request when the viewport is parked at the top of its
// content (so the user has nothing more to scroll into), no request is in
// flight, and we have not already learned that the server has nothing
// older. The lowest seq in the current buffer becomes the `before_seq`
// cursor.
func (model *model) maybeRequestOlderLogs() (tea.Cmd, bool) {
	if model.loadingOlderLogs || model.noOlderLogsAvailable {
		return nil, false
	}
	if !model.logsViewport.AtTop() {
		return nil, false
	}
	if len(model.logEntries) == 0 {
		return nil, false
	}
	selected := model.selectedServiceName()
	if selected == "" {
		return nil, false
	}

	beforeSeq := lowestSeq(model.logEntries)
	if beforeSeq <= 0 {
		return nil, false
	}

	model.loadingOlderLogs = true
	cmd := model.fetchOlderLogsCmd(selected, beforeSeq)
	return cmd, true
}

func lowestSeq(entries []ServiceLog) int64 {
	var lowest int64
	for _, entry := range entries {
		if entry.Seq <= 0 {
			continue
		}
		if lowest == 0 || entry.Seq < lowest {
			lowest = entry.Seq
		}
	}
	return lowest
}

func (model *model) appendLogEntryIfVisible(logEntry ServiceLog) bool {
	if isEmptyLogEntry(logEntry) {
		return false
	}

	if !shouldDisplayLogForSelection(logEntry, model.selectedServiceName()) {
		return false
	}

	if logEntry.Seq > 0 {
		if model.seenLogSeqs == nil {
			model.seenLogSeqs = map[int64]struct{}{}
		}
		if _, exists := model.seenLogSeqs[logEntry.Seq]; exists {
			return false
		}
		model.seenLogSeqs[logEntry.Seq] = struct{}{}
	}

	model.logEntries = append(model.logEntries, logEntry)
	return true
}

func (model *model) setEffectiveLogLimit(limit *int) {
	copied := copyIntPointer(limit)
	if copied != nil && *copied <= 0 {
		copied = nil
	}
	model.effectiveLogLimit = copied
}

func computeLocalLogCacheLimit(effectiveLimit *int) int {
	localLimit := localLogCacheMinEntries
	if effectiveLimit != nil && *effectiveLimit > 0 {
		if *effectiveLimit > localLogCacheMaxEntries/localLogCacheMultiplier {
			localLimit = localLogCacheMaxEntries
		} else {
			localLimit = *effectiveLimit * localLogCacheMultiplier
		}
	}

	if localLimit < localLogCacheMinEntries {
		localLimit = localLogCacheMinEntries
	}
	if localLimit > localLogCacheMaxEntries {
		localLimit = localLogCacheMaxEntries
	}

	return localLimit
}

func (model *model) enforceVisibleLogBufferLimit() {
	maxEntries := computeLocalLogCacheLimit(model.effectiveLogLimit)
	if len(model.logEntries) <= maxEntries {
		return
	}

	model.logEntries = trimLogEntriesToMaxEntries(model.logEntries, maxEntries)
	model.rebuildSeenLogSeqs()
}

// logTimeFormat is the stable timestamp layout used for every log row.
// logTimeWidth is derived from that format so the column size stays in sync.
const (
	logTimeFormat = "15:04:05.000"
	logTimeWidth  = len(logTimeFormat) // visual width: 12 chars
	logColumnGap  = 1                  // space between columns
)

// computeServiceWidth returns the visual width required by the service column.
// It is derived from the longest rendered service label in the given entries,
// so the column is exactly as wide as the content needs — no more, no less.
func computeServiceWidth(entries []ServiceLog) int {
	max := 0
	for _, e := range entries {
		n := lipgloss.Width(strings.TrimSpace(e.Service))
		if n > max {
			max = n
		}
	}
	return max
}

// renderLogRow renders a single log entry as a formatted row using a 3-column
// layout: TIME (auto) | SERVICE (auto) | MESSAGE (1fr).
//
//	serviceWidth = 0 in single-service mode (service column hidden)
//	serviceWidth = computeServiceWidth(entries) in all-logs mode
func renderLogRow(entry ServiceLog, innerWidth int, showService bool, serviceWidth int) string {
	ts := formatTimestamp(strings.TrimSpace(entry.Timestamp))
	service := strings.TrimSpace(entry.Service)
	stream := strings.TrimSpace(entry.Stream)
	message := strings.TrimSpace(entry.Message)

	tsCell := logTimestampStyle.
		Width(logTimeWidth).
		MaxWidth(logTimeWidth).
		Render(ts)

	// usedWidth = TIME + gap + SERVICE (when visible), or TIME + gap alone.
	usedWidth := logTimeWidth + logColumnGap
	var svcCell string
	if showService {
		svcCell = logServiceStyle.
			Width(serviceWidth).
			MaxWidth(serviceWidth).
			Render(service)
		usedWidth += serviceWidth + logColumnGap // gap between SERVICE and MESSAGE
	}

	// Build plain-text message for accurate wrap calculation (no ANSI codes).
	var stderrPrefix string
	if stream != "" && !strings.EqualFold(stream, "stdout") {
		stderrPrefix = "[" + stream + "] "
	}
	plainMsg := stderrPrefix + message

	// messageWidth = innerWidth − (TIME + gap + SERVICE + gap)
	msgWidth := maxInt(1, innerWidth-usedWidth)

	// Split on real newlines first, then wrap each sub-line independently.
	rawMsg := strings.ReplaceAll(plainMsg, "\r", "")
	rawLines := strings.Split(rawMsg, "\n")

	var chunks []string
	for _, line := range rawLines {
		lineChunks := wrapWords(line, msgWidth)
		if len(lineChunks) == 0 {
			lineChunks = []string{""}
		}
		chunks = append(chunks, lineChunks...)
	}
	if len(chunks) == 0 {
		chunks = []string{""}
	}

	// Re-apply stderr styling to the marker in the first chunk after wrapping.
	if stderrPrefix != "" && strings.HasPrefix(chunks[0], stderrPrefix) {
		rest := chunks[0][len(stderrPrefix):]
		chunks[0] = logStderrStyle.Render("["+stream+"]") + bgSpace(1, colorPanelBackground) + rest
	}

	// Hanging indent: continuation lines start at the same column as the message.
	indent := bgSpace(usedWidth, colorPanelBackground)
	colGap := bgSpace(logColumnGap, colorPanelBackground)
	rows := make([]string, len(chunks))
	for i, chunk := range chunks {
		if i == 0 {
			if showService {
				rows[0] = tsCell + colGap + svcCell + colGap + chunk
			} else {
				rows[0] = tsCell + colGap + chunk
			}
		} else {
			rows[i] = indent + chunk
		}
	}
	return strings.Join(rows, "\n")
}

// wrapWords splits text into lines of at most width visual columns,
// breaking at word boundaries where possible. Width is measured using
// lipgloss.Width so that double-wide characters are handled correctly.
// The caller is responsible for splitting on real newlines beforehand.
func wrapWords(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	text = strings.ReplaceAll(text, "\r", "")

	if lipgloss.Width(text) <= width {
		return []string{text}
	}

	var chunks []string
	runes := []rune(text)

	for len(runes) > 0 {
		if lipgloss.Width(string(runes)) <= width {
			chunks = append(chunks, string(runes))
			break
		}

		// Find how many runes fit within width (visual measurement).
		cumWidth := 0
		breakIdx := 0
		for i, r := range runes {
			rw := lipgloss.Width(string(r))
			if cumWidth+rw > width {
				break
			}
			cumWidth += rw
			breakIdx = i + 1
		}
		if breakIdx == 0 {
			breakIdx = 1 // at least one rune per chunk
		}

		// Try to soft-break at the last space within the allowed width.
		softBreak := breakIdx
		for softBreak > 0 && runes[softBreak-1] != ' ' {
			softBreak--
		}

		if softBreak > 0 {
			chunks = append(chunks, strings.TrimRight(string(runes[:softBreak]), " "))
			runes = runes[softBreak:]
			// Consume leading spaces at the start of the next chunk.
			for len(runes) > 0 && runes[0] == ' ' {
				runes = runes[1:]
			}
		} else {
			// No space found — hard-break at width.
			chunks = append(chunks, string(runes[:breakIdx]))
			runes = runes[breakIdx:]
		}
	}
	return chunks
}

func formatTimestamp(raw string) string {
	if raw == "" {
		return ""
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
	} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.Format("15:04:05.000")
		}
	}
	return raw
}

func batchCmds(cmds ...tea.Cmd) tea.Cmd {
	collected := make([]tea.Cmd, 0, len(cmds))
	for _, cmd := range cmds {
		if cmd != nil {
			collected = append(collected, cmd)
		}
	}

	if len(collected) == 0 {
		return nil
	}

	return tea.Batch(collected...)
}

func actionForServiceKey(key, status string) (ServiceAction, bool) {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "s":
		action := primaryServiceActionForStatus(status)
		if action == "" {
			return "", false
		}
		return action, true
	case "x":
		return ServiceActionStop, true
	default:
		return "", false
	}
}

func primaryServiceActionForStatus(status string) ServiceAction {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "stopped", "failed":
		return ServiceActionStart
	case "running", "ready":
		return ServiceActionRestart
	default:
		return ""
	}
}

func canApplyServiceAction(action ServiceAction, status string) bool {
	normalized := strings.ToLower(strings.TrimSpace(status))

	switch action {
	case ServiceActionStart:
		return normalized == "stopped" || normalized == "failed"
	case ServiceActionStop:
		return normalized == "running" || normalized == "ready" || normalized == "starting"
	case ServiceActionRestart:
		return normalized == "running" || normalized == "ready" || normalized == "failed"
	default:
		return false
	}
}

func isAllLogsItem(name string) bool {
	return strings.TrimSpace(name) == allLogsName
}

func normalizeServiceKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func isTransitionServiceStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "starting", "stopping":
		return true
	default:
		return false
	}
}

func isBusyReleaseServiceStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ready", "failed", "stopped", "running":
		return true
	default:
		return false
	}
}

func (model *model) resetBusyServices() {
	model.busyServices = map[string]bool{}
}

func (model *model) resetRestartingServices() {
	model.restartingServices = map[string]bool{}
}

func (model *model) markServiceBusy(serviceName string) {
	key := normalizeServiceKey(serviceName)
	if key == "" {
		return
	}
	if model.busyServices == nil {
		model.busyServices = map[string]bool{}
	}
	model.busyServices[key] = true
}

func (model *model) markServiceRestarting(serviceName string) {
	key := normalizeServiceKey(serviceName)
	if key == "" {
		return
	}
	if model.restartingServices == nil {
		model.restartingServices = map[string]bool{}
	}
	model.restartingServices[key] = true
}

func (model *model) clearServiceBusy(serviceName string) {
	key := normalizeServiceKey(serviceName)
	if key == "" || model.busyServices == nil {
		return
	}
	delete(model.busyServices, key)
}

func (model *model) clearServiceRestarting(serviceName string) {
	key := normalizeServiceKey(serviceName)
	if key == "" || model.restartingServices == nil {
		return
	}
	delete(model.restartingServices, key)
}

func (model model) isServiceBusy(serviceName, status string) bool {
	if isTransitionServiceStatus(status) {
		return true
	}
	if model.isServiceRestarting(serviceName) {
		return true
	}
	key := normalizeServiceKey(serviceName)
	if key == "" || model.busyServices == nil {
		return false
	}
	return model.busyServices[key]
}

func (model model) isServiceRestarting(serviceName string) bool {
	key := normalizeServiceKey(serviceName)
	if key == "" || model.restartingServices == nil {
		return false
	}
	return model.restartingServices[key]
}

func (model *model) refreshBusyServicesFromItems() {
	model.resetBusyServices()
	for _, service := range model.serviceItems {
		if isTransitionServiceStatus(service.status) {
			model.markServiceBusy(service.name)
		}
	}
}

func (model *model) updateBusyStateForService(service Service) {
	serviceName := strings.TrimSpace(service.Name)
	if serviceName == "" {
		return
	}

	if isTransitionServiceStatus(service.Status) {
		model.markServiceBusy(serviceName)
		return
	}

	if isBusyReleaseServiceStatus(service.Status) {
		model.clearServiceBusy(serviceName)
	}
}

func shouldClearRestartingForServiceStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "stopping", "starting", "stopped", "failed", "ready":
		return true
	default:
		return false
	}
}

func (model *model) updateRestartingStateForService(service Service) {
	serviceName := strings.TrimSpace(service.Name)
	if serviceName == "" {
		return
	}
	if shouldClearRestartingForServiceStatus(service.Status) {
		model.clearServiceRestarting(serviceName)
	}
}

func (model *model) updateBusyStateForActionResult(serviceName string, result ServiceActionResult) {
	if isAllLogsItem(serviceName) {
		return
	}

	switch result {
	case ServiceActionSuccess:
		// Success means the runtime accepted the action. Runtime events will follow
		// and eventually clear the busy flag. If events already completed the cycle
		// (settled), don't re-mark — but keep any existing busy from key-press
		// since the operation is legitimately in-flight.
		if !model.serviceSettled(serviceName) {
			model.markServiceBusy(serviceName)
		}
	case ServiceActionConflict:
		// Conflict means the runtime rejected the action because another
		// operation is already in progress. If the service is in a terminal
		// state (settled), there will be no event follow-up for this rejected
		// action, so the busy flag from the key press must be cleared to avoid
		// a permanent stuck state.
		if model.serviceSettled(serviceName) {
			model.clearServiceBusy(serviceName)
		}
	default:
		model.clearServiceBusy(serviceName)
	}
}

func (model *model) updateRestartingStateForActionResult(action ServiceAction, serviceName string, result ServiceActionResult) {
	if isAllLogsItem(serviceName) {
		return
	}

	if action != ServiceActionRestart {
		return
	}
	if result != ServiceActionSuccess {
		model.clearServiceRestarting(serviceName)
	}
}

func (model model) serviceSettled(serviceName string) bool {
	key := normalizeServiceKey(serviceName)
	for _, item := range model.serviceItems {
		if normalizeServiceKey(item.name) == key {
			return isBusyReleaseServiceStatus(item.status)
		}
	}
	return false
}

func (model model) serviceActionCmd(action ServiceAction, serviceName string) tea.Cmd {
	client := model.client
	name := strings.TrimSpace(serviceName)

	return func() tea.Msg {
		if client == nil {
			return serviceActionDoneMsg{action: action, result: ServiceActionError, serviceName: name}
		}

		result := sendServiceAction(context.Background(), client, action, name)
		return serviceActionDoneMsg{action: action, result: result, serviceName: name}
	}
}

func (model model) globalActionCmd(action ServiceAction) tea.Cmd {
	client := model.client

	return func() tea.Msg {
		if client == nil {
			return serviceActionDoneMsg{action: action, result: ServiceActionError, serviceName: allLogsName}
		}

		result := sendGlobalAction(context.Background(), client, action)

		return serviceActionDoneMsg{action: action, result: result, serviceName: allLogsName}
	}
}

func footerMessageExpiryCmd(token int) tea.Cmd {
	return tea.Tick(actionFooterTTL, func(time.Time) tea.Msg {
		return footerMessageExpiredMsg{token: token}
	})
}

func (model *model) setPersistentFooter(message string) {
	model.footerMessage = strings.TrimSpace(message)
	model.footerTransient = false
	model.footerToken++
}

func (model *model) setTransientFooter(message string) tea.Cmd {
	model.footerMessage = strings.TrimSpace(message)
	model.footerTransient = true
	model.footerToken++
	return footerMessageExpiryCmd(model.footerToken)
}

func (model *model) clearTransientFooter() {
	model.footerMessage = ""
	model.footerTransient = false
	model.footerToken++
}

// truncateToWidth returns the longest prefix of text that fits within width
// visual columns, measured via lipgloss.Width.
func truncateToWidth(text string, width int) string {
	cumWidth := 0
	for i, r := range text {
		rw := lipgloss.Width(string(r))
		if cumWidth+rw > width {
			return text[:i]
		}
		cumWidth += rw
	}
	return text
}

func fitLine(text string, width int) string {
	if width <= 0 {
		return ""
	}

	replaced := strings.ReplaceAll(text, "\n", " ")
	replaced = strings.ReplaceAll(replaced, "\r", " ")

	vw := lipgloss.Width(replaced)
	if vw > width {
		if width <= 3 {
			return truncateToWidth(replaced, width)
		}
		return truncateToWidth(replaced, width-3) + "..."
	}

	if vw < width {
		return replaced + strings.Repeat(" ", width-vw)
	}

	return replaced
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func trimLastRune(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return ""
	}
	return string(runes[:len(runes)-1])
}

func clampSelection(index, length int) int {
	if length <= 0 {
		return 0
	}
	if index < 0 {
		return 0
	}
	if index >= length {
		return length - 1
	}
	return index
}

func moveSelection(current, delta, length int) int {
	if length <= 0 {
		return 0
	}
	return clampSelection(current+delta, length)
}

// fillBg ensures every line in content is exactly width visible columns by
// appending bg-colored spaces, and pads to height total lines when height > 0.
// This prevents terminal-default background from leaking through ANSI resets
// emitted by inner styled content.
func fillBg(content string, width, height int, bg lipgloss.TerminalColor) string {
	if width <= 0 {
		return content
	}
	pad := lipgloss.NewStyle().Background(bg)
	lines := strings.Split(content, "\n")
	out := make([]string, 0, maxInt(height, len(lines)))
	for _, line := range lines {
		if height > 0 && len(out) >= height {
			break
		}
		w := lipgloss.Width(line)
		if w < width {
			line += pad.Render(strings.Repeat(" ", width-w))
		}
		out = append(out, line)
	}
	if height > 0 {
		blank := pad.Render(strings.Repeat(" ", width))
		for len(out) < height {
			out = append(out, blank)
		}
	}
	return strings.Join(out, "\n")
}

// bgResetSeq is the ANSI SGR sequence that re-establishes the application
// background color.  It is injected after every \x1b[0m reset in the final
// View output so that no cell in the terminal buffer relies on the terminal's
// default background (which can revert to black after a tab switch in VS Code).
var bgResetSeq = func() string {
	hex := strings.TrimPrefix(string(colorBackground), "#")
	if len(hex) != 6 {
		return ""
	}
	r, _ := strconv.ParseUint(hex[0:2], 16, 8)
	g, _ := strconv.ParseUint(hex[2:4], 16, 8)
	b, _ := strconv.ParseUint(hex[4:6], 16, 8)
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
}()

// ensureBackgroundConsistency re-establishes the application background after
// every ANSI SGR reset (\x1b[0m) in the final View output.
//
// Problem: terminals such as VS Code's integrated terminal (xterm.js) revert
// cells to their native default background when the tab loses and regains
// focus.  Any cell whose last SGR state is a bare reset will turn black,
// breaking the intended background color in empty/padding regions.
//
// Fix: after each reset we immediately re-apply the app background via a
// true-color SGR sequence, so no cell ever relies on the terminal's default.
// This is an intentional, permanent corrective measure — not a temporary hack.
func ensureBackgroundConsistency(s string) string {
	if bgResetSeq == "" {
		return s
	}
	return strings.ReplaceAll(s, "\x1b[0m", "\x1b[0m"+bgResetSeq)
}

// bgSpace returns n space characters explicitly styled with the given
// background color so they are not affected by adjacent ANSI resets.
func bgSpace(n int, bg lipgloss.TerminalColor) string {
	if n <= 0 {
		return ""
	}
	return lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", n))
}
