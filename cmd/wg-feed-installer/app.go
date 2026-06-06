package main

import (
	"context"
	"fmt"
	"runtime"
	"slices"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/exeteres/wg-feed/internal/client/config"
	clientfeed "github.com/exeteres/wg-feed/internal/client/feed"
	"github.com/exeteres/wg-feed/internal/installer"
	"github.com/exeteres/wg-feed/internal/model"
)

var (
	keySelect = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	)
	keyExit = key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "exit"),
	)
	keySubmit = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "submit"),
	)
	keyCancel = key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	)
	keyToggle = key.NewBinding(
		key.WithKeys("space"),
		key.WithHelp("space", "toggle"),
	)
)

const (
	tunnelOptionServer = "__server__"
	tunnelOptionAll    = "__all__"
	tunnelOptionNone   = "__none__"
)

type staticKeyMap struct {
	short []key.Binding
	full  [][]key.Binding
}

func (m staticKeyMap) ShortHelp() []key.Binding  { return m.short }
func (m staticKeyMap) FullHelp() [][]key.Binding { return m.full }

type uiScreen int

const (
	screenMain uiScreen = iota
	screenAddLabel
	screenAddURL
	screenAddBackendsSelect
	screenAddTunnelSelect
	screenUpdateSelectSubscription
	screenUpdateMenu
	screenUpdateURL
	screenUpdateBackendsSelect
	screenUpdateTunnelsSelectBackend
	screenUpdateTunnelSelect
	screenDeleteSelectSubscription
	screenDeleteConfirm
	screenDeleteLastConfirmUninstall
	screenUninstallConfirm
)

type listItem string

func (i listItem) FilterValue() string { return string(i) }
func (i listItem) Title() string       { return string(i) }
func (i listItem) Description() string { return "" }

type multiOptionItem struct {
	value    string
	label    string
	selected bool
}

func (i multiOptionItem) FilterValue() string { return i.label }
func (i multiOptionItem) Title() string {
	if i.selected {
		return "[x] " + i.label
	}
	return "[ ] " + i.label
}
func (i multiOptionItem) Description() string {
	return ""
}

type addState struct {
	label      string
	url        string
	doc        model.FeedDocument
	backends   []config.BackendType
	backendIdx int
	plans      []installer.BackendPlan
}

type updateTunnelMode int

const (
	updateTunnelModeAll updateTunnelMode = iota
	updateTunnelModeOne
)

type updateState struct {
	label         string
	url           string
	origURL       string
	doc           model.FeedDocument
	plans         []installer.BackendPlan
	origPlans     []installer.BackendPlan
	pending       []config.BackendType
	pendingAll    []config.BackendType
	pendingKeep   map[config.BackendType]installer.TunnelChoice
	pendingNew    map[config.BackendType]installer.TunnelChoice
	pendingPlans  []installer.BackendPlan
	backendIdx    int
	selectedIndex int
	tunnelMode    updateTunnelMode
}

const (
	updateActionChangeURL      = "Change URL"
	updateActionChangeBackends = "Change set of backends"
	updateActionChangeTunnels  = "Change tunnels"
	updateActionApply          = "Apply update"
	updateActionCancel         = "Cancel"
)

type navSnapshot struct {
	screen    uiScreen
	title     string
	listItems []list.Item
	listIndex int
	input     textinput.Model
}

type installerModel struct {
	ctx  context.Context
	opts installer.ApplyOptions
	cfg  config.Config

	screen uiScreen
	title  string
	list   list.Model
	input  textinput.Model
	help   help.Model
	pbar   progress.Model
	width  int

	status string
	errMsg string
	quit   bool

	mainActions  []string
	add          addState
	update       updateState
	deleteLabel  string
	navStack     []navSnapshot
	mainInfoLine string

	applying          bool
	applyMode         string
	applyCh           chan tea.Msg
	downloadDoneBytes int64
	downloadTotal     int64
}

type downloadProgressMsg struct {
	done  int64
	total int64
}

type applyDoneMsg struct {
	mode string
	err  error
}

func runInstallerApp(opts installer.ApplyOptions) error {
	ctx := context.Background()
	m, err := newInstallerModel(ctx, opts)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func newInstallerModel(ctx context.Context, opts installer.ApplyOptions) (installerModel, error) {
	cfg, err := installer.LoadConfigOrEmpty(opts.ConfigPath)
	if err != nil {
		return installerModel{}, err
	}
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetSpacing(0)
	l := list.New([]list.Item{}, delegate, 80, 16)
	l.SetDelegate(delegate)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(true)
	l.DisableQuitKeybindings()
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{keySelect, keyExit}
	}
	l.AdditionalFullHelpKeys = func() []key.Binding {
		return []key.Binding{keySelect, keyExit}
	}
	ti := textinput.New()
	ti.Prompt = "> "
	h := help.New()
	pb := progress.New(progress.WithDefaultGradient())

	m := installerModel{
		ctx:    ctx,
		opts:   opts,
		cfg:    cfg,
		screen: screenMain,
		title:  "wg-feed installer",
		list:   l,
		input:  ti,
		help:   h,
		pbar:   pb,
	}
	m.refreshMainMenu("", "")
	return m, nil
}

func (m installerModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m installerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.list.SetSize(msg.Width, max(10, msg.Height-7))
		m.help.Width = msg.Width
		m.pbar.Width = max(10, msg.Width-6)
		return m, nil
	case downloadProgressMsg:
		if !m.applying {
			return m, nil
		}
		m.downloadDoneBytes = msg.done
		m.downloadTotal = msg.total
		if msg.total > 0 {
			percent := float64(msg.done) / float64(msg.total)
			if percent < 0 {
				percent = 0
			}
			if percent > 1 {
				percent = 1
			}
			cmd := m.pbar.SetPercent(percent)
			return m, tea.Batch(cmd, waitInstallerMsg(m.applyCh))
		}
		return m, waitInstallerMsg(m.applyCh)
	case applyDoneMsg:
		if !m.applying {
			return m, nil
		}
		m.applying = false
		m.applyCh = nil
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}
		switch msg.mode {
		case "add":
			m.refreshMainMenu("", "Subscription added and daemon restarted")
		case "update":
			m.refreshMainMenu("", "Subscription updated and daemon restarted")
		case "delete":
			m.refreshMainMenu("", "Subscription deleted and daemon restarted")
		case "repair":
			m.refreshMainMenu("", "Daemon and service repaired")
		default:
			m.refreshMainMenu("", "")
		}
		return m, nil
	case tea.KeyMsg:
		if m.applying {
			if msg.Type == tea.KeyCtrlC {
				m.quit = true
				return m, tea.Quit
			}
			return m, nil
		}
		if m.isMultiSelectScreen() && key.Matches(msg, keyToggle) {
			m.toggleCurrentSelection()
			return m, nil
		}
		switch msg.Type {
		case tea.KeyCtrlC:
			m.quit = true
			return m, tea.Quit
		case tea.KeyEsc:
			if m.screen == screenMain {
				return m, nil
			}
			if !m.popNav() {
				m.refreshMainMenu("", "")
			}
			return m, nil
		case tea.KeyEnter:
			if m.isListScreen() {
				return m.handleListEnter()
			}
			if m.isInputScreen() {
				return m.handleInputEnter()
			}
		}
	}

	if m.isListScreen() {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}
	if m.isInputScreen() {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m installerModel) View() string {
	if m.quit {
		return "Bye\n"
	}
	if m.applying {
		var b strings.Builder
		b.WriteString(m.renderSharedTitle())
		b.WriteString("\n\n")
		b.WriteString("Downloading daemon binary...\n\n")
		if m.downloadTotal > 0 {
			b.WriteString(m.pbar.ViewAs(float64(m.downloadDoneBytes) / float64(m.downloadTotal)))
			b.WriteString("\n")
			_, _ = fmt.Fprintf(&b, "%d/%d bytes", m.downloadDoneBytes, m.downloadTotal)
		} else {
			b.WriteString(m.pbar.ViewAs(0))
			b.WriteString("\n")
			_, _ = fmt.Fprintf(&b, "%d bytes", m.downloadDoneBytes)
		}
		b.WriteString("\n\nCtrl+C to exit")
		return b.String()
	}
	var b strings.Builder
	if m.errMsg != "" {
		b.WriteString("Error: ")
		b.WriteString(m.errMsg)
		b.WriteString("\n\n")
	}
	if m.isListScreen() {
		b.WriteString(m.renderSharedTitle())
		if m.screen == screenMain && strings.TrimSpace(m.mainInfoLine) != "" {
			b.WriteString("\n")
			b.WriteString(m.mainInfoLine)
		}
		b.WriteString("\n\n")
		b.WriteString(m.list.View())
		return b.String()
	}
	if m.isInputScreen() {
		b.WriteString(m.renderSharedTitle())
		b.WriteString("\n\n")
		b.WriteString(m.input.View())
		b.WriteString("\n\n")
		b.WriteString(m.help.View(staticKeyMap{
			short: []key.Binding{keySubmit, keyCancel},
			full:  [][]key.Binding{{keySubmit, keyCancel}},
		}))
		b.WriteString("\n")
		return b.String()
	}
	return b.String()
}

func (m installerModel) renderSharedTitle() string {
	return m.list.Styles.Title.Render(m.title)
}

func (m installerModel) isListScreen() bool {
	switch m.screen {
	case screenMain,
		screenAddBackendsSelect,
		screenAddTunnelSelect,
		screenUpdateSelectSubscription,
		screenUpdateMenu,
		screenUpdateBackendsSelect,
		screenUpdateTunnelsSelectBackend,
		screenUpdateTunnelSelect,
		screenDeleteSelectSubscription,
		screenDeleteConfirm,
		screenDeleteLastConfirmUninstall,
		screenUninstallConfirm:
		return true
	default:
		return false
	}
}

func (m installerModel) isInputScreen() bool {
	switch m.screen {
	case screenAddLabel,
		screenAddURL,
		screenUpdateURL:
		return true
	default:
		return false
	}
}

func (m installerModel) isMultiSelectScreen() bool {
	switch m.screen {
	case screenAddBackendsSelect,
		screenAddTunnelSelect,
		screenUpdateBackendsSelect,
		screenUpdateTunnelSelect:
		return true
	default:
		return false
	}
}

func (m installerModel) handleListEnter() (tea.Model, tea.Cmd) {
	idx := m.list.Index()
	switch m.screen {
	case screenMain:
		if idx < 0 || idx >= len(m.mainActions) {
			m.errMsg = "invalid menu selection"
			return m, nil
		}
		m.navStack = nil
		action := m.mainActions[idx]
		switch action {
		case "add":
			m.startAddFlow()
		case "update":
			m.startUpdateSelect()
		case "delete":
			m.startDeleteSelect()
		case "repair":
			return m, m.startApply("repair")
		case "uninstall":
			m.startUninstallConfirm()
		case "quit":
			m.quit = true
			return m, tea.Quit
		}
		return m, nil
	case screenAddBackendsSelect:
		backends, err := m.selectedBackendsFromList()
		if err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		m.add.backends = backends
		m.add.backendIdx = 0
		m.add.plans = nil
		m.pushNav()
		firstBackend := m.add.backends[m.add.backendIdx]
		m.startAddTunnelSelector(firstBackend, defaultTunnelChoiceForBackend(firstBackend, docTunnelIDs(m.add.doc)))
		return m, nil
	case screenAddTunnelSelect:
		if len(m.add.backends) == 0 {
			m.errMsg = "no backends selected"
			m.startAddBackendsSelector()
			return m, nil
		}
		if m.add.backendIdx >= len(m.add.backends) {
			// Selection phase already completed; allow retrying apply after an earlier failure.
			return m, m.startApply("add")
		}
		choice, err := m.selectedTunnelChoiceFromList(docTunnelIDs(m.add.doc))
		if err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		bt := m.add.backends[m.add.backendIdx]
		m.add.plans = append(m.add.plans, installer.BackendPlan{Type: bt, EnabledTunnels: choice})
		m.add.backendIdx++
		if m.add.backendIdx < len(m.add.backends) {
			m.pushNav()
			nextBackend := m.add.backends[m.add.backendIdx]
			m.startAddTunnelSelector(nextBackend, defaultTunnelChoiceForBackend(nextBackend, docTunnelIDs(m.add.doc)))
			return m, nil
		}
		feedCfg, err := installer.BuildFeedConfig(m.add.url, m.add.plans, m.add.doc)
		if err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		m.cfg.Feeds[m.add.label] = feedCfg
		if strings.TrimSpace(m.cfg.StatePath) == "" {
			m.cfg.StatePath = installer.DefaultStatePath
		}
		return m, m.startApply("add")
	case screenUpdateSelectSubscription:
		labels := sortedFeedLabels(m.cfg.Feeds)
		if idx < 0 || idx >= len(labels) {
			m.errMsg = "invalid subscription selection"
			return m, nil
		}
		m.pushNav()
		m.startUpdateMenu(labels[idx])
		return m, nil
	case screenUpdateMenu:
		selected, ok := m.selectedListLabel()
		if !ok {
			m.errMsg = "invalid update action"
			return m, nil
		}
		switch selected {
		case updateActionChangeURL:
			m.pushNav()
			m.screen = screenUpdateURL
			m.setInputTitle("New subscription URL (current: "+redactURLForUI(m.update.url)+")", "")
		case updateActionChangeBackends:
			m.pushNav()
			m.startUpdateBackendsSelector()
		case updateActionChangeTunnels:
			if len(m.update.plans) == 0 {
				m.errMsg = "no backends configured; change set of backends first"
				return m, nil
			}
			if m.windowsOnlyBackendMode() && len(m.update.plans) == 1 {
				m.update.selectedIndex = 0
				m.update.tunnelMode = updateTunnelModeOne
				selected := m.update.plans[0]
				m.pushNav()
				m.startUpdateTunnelSelector(selected.Type, selected.EnabledTunnels)
				return m, nil
			}
			m.pushNav()
			m.screen = screenUpdateTunnelsSelectBackend
			options := make([]string, 0, len(m.update.plans))
			for _, p := range m.update.plans {
				options = append(options, string(p.Type))
			}
			m.setList("Select backend", options, 0)
		case updateActionApply:
			newFeedCfg, err := installer.BuildFeedConfig(m.update.url, m.update.plans, m.update.doc)
			if err != nil {
				m.errMsg = err.Error()
				return m, nil
			}
			m.cfg.Feeds[m.update.label] = newFeedCfg
			return m, m.startApply("update")
		case updateActionCancel:
			m.refreshMainMenu("", "")
		default:
			m.errMsg = "invalid update action"
		}
		return m, nil
	case screenUpdateTunnelsSelectBackend:
		if idx < 0 || idx >= len(m.update.plans) {
			m.errMsg = "invalid backend selection"
			return m, nil
		}
		m.update.selectedIndex = idx
		m.update.tunnelMode = updateTunnelModeOne
		selected := m.update.plans[idx]
		m.pushNav()
		m.startUpdateTunnelSelector(selected.Type, selected.EnabledTunnels)
		return m, nil
	case screenUpdateBackendsSelect:
		backends, err := m.selectedBackendsFromList()
		if err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		existing := map[config.BackendType]installer.TunnelChoice{}
		for _, plan := range m.update.plans {
			existing[plan.Type] = plan.EnabledTunnels
		}
		m.update.pendingAll = append([]config.BackendType(nil), backends...)
		m.update.pending = nil
		m.update.pendingKeep = map[config.BackendType]installer.TunnelChoice{}
		m.update.pendingNew = map[config.BackendType]installer.TunnelChoice{}
		for _, bt := range backends {
			if choice, ok := existing[bt]; ok {
				m.update.pendingKeep[bt] = choice
				continue
			}
			m.update.pending = append(m.update.pending, bt)
		}
		if len(m.update.pending) == 0 {
			m.update.plans = m.buildPendingPlans()
			m.startUpdateActionMenu()
			return m, nil
		}
		m.update.pendingPlans = nil
		m.update.backendIdx = 0
		m.update.tunnelMode = updateTunnelModeAll
		m.pushNav()
		firstBackend := m.update.pending[m.update.backendIdx]
		m.startUpdateTunnelSelector(firstBackend, defaultTunnelChoiceForBackend(firstBackend, docTunnelIDs(m.update.doc)))
		return m, nil
	case screenUpdateTunnelSelect:
		choice, err := m.selectedTunnelChoiceFromList(docTunnelIDs(m.update.doc))
		if err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		if m.update.tunnelMode == updateTunnelModeOne {
			if m.update.selectedIndex < 0 || m.update.selectedIndex >= len(m.update.plans) {
				m.errMsg = "invalid backend selection"
				return m, nil
			}
			m.update.plans[m.update.selectedIndex].EnabledTunnels = choice
			m.pushNav()
			m.startUpdateActionMenu()
			return m, nil
		}
		bt := m.update.pending[m.update.backendIdx]
		m.update.pendingNew[bt] = choice
		m.update.backendIdx++
		if m.update.backendIdx < len(m.update.pending) {
			nextBackend := m.update.pending[m.update.backendIdx]
			m.pushNav()
			m.startUpdateTunnelSelector(nextBackend, defaultTunnelChoiceForBackend(nextBackend, docTunnelIDs(m.update.doc)))
			return m, nil
		}
		m.update.plans = m.buildPendingPlans()
		m.pushNav()
		m.startUpdateActionMenu()
		return m, nil
	case screenDeleteSelectSubscription:
		labels := sortedFeedLabels(m.cfg.Feeds)
		if idx < 0 || idx >= len(labels) {
			m.errMsg = "invalid subscription selection"
			return m, nil
		}
		m.deleteLabel = labels[idx]
		m.pushNav()
		m.screen = screenDeleteConfirm
		m.setList(fmt.Sprintf("Delete subscription %s?", m.deleteLabel), []string{"No", "Yes"}, 0)
		return m, nil
	case screenDeleteConfirm:
		if idx == 0 {
			m.refreshMainMenu("", "")
			return m, nil
		}
		if len(m.cfg.Feeds) == 1 {
			m.pushNav()
			m.screen = screenDeleteLastConfirmUninstall
			m.setList("This is the last subscription. Uninstall wg-feed instead?", []string{"No", "Yes"}, 0)
			return m, nil
		}
		delete(m.cfg.Feeds, m.deleteLabel)
		return m, m.startApply("delete")
	case screenDeleteLastConfirmUninstall:
		if idx == 0 {
			m.refreshMainMenu("", "")
			return m, nil
		}
		if err := installer.UninstallSystem(m.ctx, m.opts); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		m.refreshMainMenu("", "wg-feed uninstalled")
		return m, nil
	case screenUninstallConfirm:
		if idx == 0 {
			m.refreshMainMenu("", "")
			return m, nil
		}
		if err := installer.UninstallSystem(m.ctx, m.opts); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		m.refreshMainMenu("", "wg-feed uninstalled")
		return m, nil
	}
	return m, nil
}

func (m installerModel) handleInputEnter() (tea.Model, tea.Cmd) {
	value := strings.TrimSpace(m.input.Value())
	switch m.screen {
	case screenAddLabel:
		label := installer.NextSubscriptionLabel(value, occupiedLabels(m.cfg.Feeds))
		if strings.TrimSpace(label) == "" {
			m.errMsg = "subscription label is required"
			return m, nil
		}
		if _, exists := m.cfg.Feeds[label]; exists {
			m.errMsg = fmt.Sprintf("subscription %q already exists", label)
			return m, nil
		}
		m.add.label = label
		m.screen = screenAddURL
		m.errMsg = ""
		m.setInputTitle("Subscription URL", "")
		return m, nil
	case screenAddURL:
		urls, err := installer.ParseURLs(value)
		if err != nil || len(urls) != 1 {
			if err == nil {
				err = fmt.Errorf("exactly one subscription URL is required")
			}
			m.errMsg = err.Error()
			return m, nil
		}
		url := urls[0]
		doc, err := installer.FetchFeedDocument(m.ctx, url)
		if err != nil {
			m.errMsg = fmt.Sprintf("fetch failed: %v", err)
			return m, nil
		}
		m.add.url = url
		m.add.doc = doc
		m.add.plans = nil
		m.errMsg = ""
		if m.windowsOnlyBackendMode() {
			m.add.backends = []config.BackendType{config.BackendWindows}
			m.add.backendIdx = 0
			m.pushNav()
			m.startAddTunnelSelector(config.BackendWindows, defaultTunnelChoiceForBackend(config.BackendWindows, docTunnelIDs(m.add.doc)))
			return m, nil
		}
		m.pushNav()
		m.startAddBackendsSelector()
		return m, nil
	case screenUpdateURL:
		urls, err := installer.ParseURLs(value)
		if err != nil || len(urls) != 1 {
			if err == nil {
				err = fmt.Errorf("exactly one subscription URL is required")
			}
			m.errMsg = err.Error()
			return m, nil
		}
		newURL := urls[0]
		newDoc, err := installer.FetchFeedDocument(m.ctx, newURL)
		if err != nil {
			m.errMsg = fmt.Sprintf("fetch failed: %v", err)
			return m, nil
		}
		m.update.url = newURL
		m.update.doc = newDoc
		m.pushNav()
		m.startUpdateActionMenu()
		return m, nil
	}
	return m, nil
}

func (m *installerModel) startAddFlow() {
	m.add = addState{}
	m.screen = screenAddLabel
	m.errMsg = ""
	m.status = ""
	m.setInputTitle("Subscription label", installer.NextSubscriptionLabel("", occupiedLabels(m.cfg.Feeds)))
}

func (m *installerModel) startUpdateSelect() {
	labels := sortedFeedLabels(m.cfg.Feeds)
	if len(labels) == 0 {
		m.refreshMainMenu("no subscriptions available", "")
		return
	}
	m.screen = screenUpdateSelectSubscription
	m.errMsg = ""
	m.status = ""
	m.setList("Select subscription to update", labels, 0)
}

func (m *installerModel) startUpdateMenu(label string) {
	feedCfg := m.cfg.Feeds[label]
	m.update = updateState{label: label}
	m.update.url = currentEndpoint(feedCfg)
	m.update.origURL = m.update.url
	plans := installer.BackendPlansFromFeedConfig(feedCfg)
	m.update.plans = cloneBackendPlans(plans)
	m.update.origPlans = cloneBackendPlans(plans)
	if strings.TrimSpace(m.update.url) == "" {
		m.errMsg = fmt.Sprintf("subscription %q has no endpoint URL", label)
		m.startUpdateActionMenu()
		return
	}
	doc, err := installer.FetchFeedDocument(m.ctx, m.update.url)
	if err != nil {
		m.errMsg = fmt.Sprintf("fetch subscription tunnels from existing URL failed: %v", err)
		m.update.doc = model.FeedDocument{}
		m.startUpdateActionMenu()
		return
	}
	m.update.doc = doc
	m.startUpdateActionMenu()
}

func (m *installerModel) startUpdateActionMenu() {
	m.screen = screenUpdateMenu
	m.status = ""
	actions := []string{updateActionChangeURL}
	if !m.windowsOnlyBackendMode() {
		actions = append(actions, updateActionChangeBackends)
	}
	actions = append(actions, updateActionChangeTunnels)
	defaultIdx := 0
	if m.isUpdateDirty() {
		actions = append(actions, updateActionApply)
		defaultIdx = len(actions) - 1
	}
	actions = append(actions, updateActionCancel)
	preview := m.updatePreviewLines()
	title := fmt.Sprintf("Updating %s (%s)", m.update.label, redactURLForUI(m.update.url))
	if len(preview) > 0 {
		title = title + "\n\n" + strings.Join(preview, "\n")
	}
	m.setList(
		title,
		actions,
		defaultIdx,
	)
}

func (m installerModel) isUpdateDirty() bool {
	if strings.TrimSpace(m.update.url) != strings.TrimSpace(m.update.origURL) {
		return true
	}
	return !backendPlansEqual(m.update.plans, m.update.origPlans)
}

func (m installerModel) updatePreviewLines() []string {
	if !m.isUpdateDirty() {
		return nil
	}
	lines := []string{"Preview:"}
	if strings.TrimSpace(m.update.url) != strings.TrimSpace(m.update.origURL) {
		lines = append(lines, fmt.Sprintf("- URL: %s -> %s", redactURLForUI(m.update.origURL), redactURLForUI(m.update.url)))
	}
	origBackends := joinBackendTypes(planBackendTypes(m.update.origPlans))
	newBackends := joinBackendTypes(planBackendTypes(m.update.plans))
	if origBackends != newBackends {
		lines = append(lines, fmt.Sprintf("- Backends: %s -> %s", origBackends, newBackends))
	}
	if !backendPlansEqual(m.update.plans, m.update.origPlans) {
		lines = append(lines, "- Tunnels/backend settings: changed")
	}
	lines = append(lines, "- On apply: write config and restart daemon")
	return lines
}

func (m installerModel) selectedListLabel() (string, bool) {
	idx := m.list.Index()
	items := m.list.Items()
	if idx < 0 || idx >= len(items) {
		return "", false
	}
	item, ok := items[idx].(listItem)
	if !ok {
		return "", false
	}
	return string(item), true
}

func cloneBackendPlans(in []installer.BackendPlan) []installer.BackendPlan {
	out := make([]installer.BackendPlan, len(in))
	for i := range in {
		out[i] = installer.BackendPlan{
			Type: in[i].Type,
			EnabledTunnels: installer.TunnelChoice{
				Provided: in[i].EnabledTunnels.Provided,
				IDs:      append([]string(nil), in[i].EnabledTunnels.IDs...),
			},
		}
	}
	return out
}

func backendPlansEqual(a, b []installer.BackendPlan) bool {
	if len(a) != len(b) {
		return false
	}
	an := normalizeBackendPlans(a)
	bn := normalizeBackendPlans(b)
	for i := range an {
		if an[i].Type != bn[i].Type {
			return false
		}
		if an[i].EnabledTunnels.Provided != bn[i].EnabledTunnels.Provided {
			return false
		}
		if len(an[i].EnabledTunnels.IDs) != len(bn[i].EnabledTunnels.IDs) {
			return false
		}
		for j := range an[i].EnabledTunnels.IDs {
			if an[i].EnabledTunnels.IDs[j] != bn[i].EnabledTunnels.IDs[j] {
				return false
			}
		}
	}
	return true
}

func normalizeBackendPlans(in []installer.BackendPlan) []installer.BackendPlan {
	out := cloneBackendPlans(in)
	for i := range out {
		out[i].EnabledTunnels.IDs = dedupeStrings(out[i].EnabledTunnels.IDs)
		slices.Sort(out[i].EnabledTunnels.IDs)
	}
	slices.SortFunc(out, func(a, b installer.BackendPlan) int {
		return strings.Compare(string(a.Type), string(b.Type))
	})
	return out
}

func joinBackendTypes(backends []config.BackendType) string {
	parts := make([]string, 0, len(backends))
	for _, bt := range backends {
		parts = append(parts, string(bt))
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, ",")
}

func planBackendTypes(plans []installer.BackendPlan) []config.BackendType {
	out := make([]config.BackendType, 0, len(plans))
	for _, p := range plans {
		out = append(out, p.Type)
	}
	return out
}

func (m *installerModel) startDeleteSelect() {
	labels := sortedFeedLabels(m.cfg.Feeds)
	if len(labels) == 0 {
		m.refreshMainMenu("no subscriptions available", "")
		return
	}
	m.screen = screenDeleteSelectSubscription
	m.errMsg = ""
	m.status = ""
	m.setList("Select subscription to delete", labels, 0)
}

func (m *installerModel) startUninstallConfirm() {
	m.screen = screenUninstallConfirm
	m.errMsg = ""
	m.status = ""
	m.setList("Uninstall wg-feed?", []string{"No", "Yes"}, 0)
}

func (m *installerModel) refreshMainMenu(errMsg string, status string) {
	cfg, err := installer.LoadConfigOrEmpty(m.opts.ConfigPath)
	if err != nil {
		m.errMsg = err.Error()
		m.screen = screenMain
		return
	}
	m.cfg = cfg
	hasSubs := len(m.cfg.Feeds) > 0
	hasTraces := installer.DetectInstallTraces(m.ctx, m.opts)
	actions := []string{"add"}
	if hasSubs {
		actions = append(actions, "update", "delete", "repair")
	}
	if hasTraces {
		actions = append(actions, "uninstall")
	}
	actions = append(actions, "quit")
	m.mainActions = actions
	daemonStatus := installer.DaemonStatusMissing
	if hasTraces {
		if ds, _, derr := installer.CurrentDaemonStatus(m.opts); derr != nil {
			daemonStatus = installer.DaemonStatusOutdated
		} else {
			daemonStatus = ds
		}
	}
	menuItems := make([]string, 0, len(actions))
	for _, action := range actions {
		if action == "repair" && daemonStatus == installer.DaemonStatusOutdated {
			menuItems = append(menuItems, "Update daemon")
			continue
		}
		menuItems = append(menuItems, actionLabel(action))
	}
	version := strings.TrimSpace(m.opts.ReleaseTag)
	if version == "" {
		version = "unknown"
	}
	m.title = fmt.Sprintf("wg-feed installer (%s)", version)
	m.mainInfoLine = ""
	if hasTraces {
		statusText := "Daemon Status: "
		switch daemonStatus {
		case installer.DaemonStatusOK:
			statusText += lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("ok")
		case installer.DaemonStatusMissing:
			statusText = "\n" + statusText
			statusText += lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("missing")
		default:
			statusText += lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("outdated")
		}
		m.mainInfoLine = statusText
	}
	m.screen = screenMain
	m.navStack = nil
	m.setList(m.title, menuItems, 0)
	m.errMsg = errMsg
	m.status = status
}

func (m *installerModel) pushNav() {
	items := append([]list.Item(nil), m.list.Items()...)
	m.navStack = append(m.navStack, navSnapshot{
		screen:    m.screen,
		title:     m.title,
		listItems: items,
		listIndex: m.list.Index(),
		input:     m.input,
	})
}

func (m *installerModel) popNav() bool {
	if len(m.navStack) == 0 {
		return false
	}
	last := m.navStack[len(m.navStack)-1]
	m.navStack = m.navStack[:len(m.navStack)-1]

	m.screen = last.screen
	m.title = last.title
	m.list.SetItems(last.listItems)
	m.applyListHelpForScreen()
	if last.listIndex >= 0 && last.listIndex < len(last.listItems) {
		m.list.Select(last.listIndex)
	} else {
		m.list.Select(0)
	}
	m.input = last.input
	m.errMsg = ""
	return true
}

func (m *installerModel) startApply(mode string) tea.Cmd {
	m.applying = true
	m.applyMode = mode
	m.errMsg = ""
	m.downloadDoneBytes = 0
	m.downloadTotal = 0
	if m.width > 0 {
		m.pbar.Width = max(10, m.width-6)
	}

	ch := make(chan tea.Msg, 64)
	m.applyCh = ch
	cfg := m.cfg
	opts := m.opts
	opts.DownloadProgress = func(done, total int64) {
		select {
		case ch <- downloadProgressMsg{done: done, total: total}:
		default:
		}
	}

	ctx := m.ctx
	go func(mode string) {
		err := installer.ApplyConfigSystem(ctx, cfg, opts)
		ch <- applyDoneMsg{mode: mode, err: err}
		close(ch)
	}(mode)

	return waitInstallerMsg(ch)
}

func waitInstallerMsg(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

func (m *installerModel) setList(title string, options []string, defaultIndex int) {
	items := make([]list.Item, 0, len(options))
	for _, option := range options {
		items = append(items, listItem(option))
	}
	m.title = title
	m.list.SetItems(items)
	m.applyListHelpForScreen()
	if defaultIndex >= 0 && defaultIndex < len(items) {
		m.list.Select(defaultIndex)
	} else {
		m.list.Select(0)
	}
}

func (m *installerModel) setMultiSelect(title string, order []string, labels map[string]string, selected []string, required bool) {
	selectedSet := map[string]struct{}{}
	for _, value := range selected {
		selectedSet[value] = struct{}{}
	}
	items := make([]list.Item, 0, len(order))
	for _, value := range order {
		label := labels[value]
		if label == "" {
			label = value
		}
		_, isSelected := selectedSet[value]
		items = append(items, multiOptionItem{value: value, label: label, selected: isSelected})
	}
	m.title = title
	m.list.SetItems(items)
	m.applyListHelpForScreen()
	m.list.Select(0)
	if required && len(selected) == 0 {
		m.errMsg = "select at least one option"
	}
}

func (m *installerModel) applyListHelpForScreen() {
	if m.isMultiSelectScreen() {
		m.list.AdditionalShortHelpKeys = func() []key.Binding {
			return []key.Binding{keyToggle, keySelect, keyCancel}
		}
		m.list.AdditionalFullHelpKeys = func() []key.Binding {
			return []key.Binding{keyToggle, keySelect, keyCancel}
		}
		return
	}
	if m.screen == screenMain {
		m.list.AdditionalShortHelpKeys = func() []key.Binding {
			return []key.Binding{keySelect, keyExit}
		}
		m.list.AdditionalFullHelpKeys = func() []key.Binding {
			return []key.Binding{keySelect, keyExit}
		}
		return
	}
	m.list.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{keySelect, keyCancel}
	}
	m.list.AdditionalFullHelpKeys = func() []key.Binding {
		return []key.Binding{keySelect, keyCancel}
	}
}

func (m *installerModel) toggleCurrentSelection() {
	idx := m.list.Index()
	items := m.list.Items()
	if idx < 0 || idx >= len(items) {
		return
	}
	current, ok := items[idx].(multiOptionItem)
	if !ok {
		return
	}

	switch current.value {
	case tunnelOptionServer:
		for i, it := range items {
			opt, ok := it.(multiOptionItem)
			if !ok {
				continue
			}
			opt.selected = (opt.value == tunnelOptionServer)
			items[i] = opt
		}
	case tunnelOptionAll:
		for i, it := range items {
			opt, ok := it.(multiOptionItem)
			if !ok {
				continue
			}
			opt.selected = (opt.value == tunnelOptionAll)
			items[i] = opt
		}
	case tunnelOptionNone:
		for i, it := range items {
			opt, ok := it.(multiOptionItem)
			if !ok {
				continue
			}
			opt.selected = (opt.value == tunnelOptionNone)
			items[i] = opt
		}
	default:
		for i, it := range items {
			opt, ok := it.(multiOptionItem)
			if !ok {
				continue
			}
			if opt.value == tunnelOptionServer || opt.value == tunnelOptionAll || opt.value == tunnelOptionNone {
				opt.selected = false
			}
			if i == idx {
				opt.selected = !opt.selected
			}
			items[i] = opt
		}
	}
	m.list.SetItems(items)
	m.list.Select(idx)
}

func (m *installerModel) selectedBackendsFromList() ([]config.BackendType, error) {
	allowed := map[config.BackendType]struct{}{}
	for _, bt := range m.backendTypesForPlatform() {
		allowed[bt] = struct{}{}
	}

	out := make([]config.BackendType, 0, len(m.list.Items()))
	for _, item := range m.list.Items() {
		opt, ok := item.(multiOptionItem)
		if !ok || !opt.selected {
			continue
		}
		bt := config.BackendType(opt.value)
		if _, ok := allowed[bt]; ok {
			out = append(out, bt)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one backend is required")
	}
	return out, nil
}

func (m *installerModel) selectedTunnelChoiceFromList(availableIDs []string) (installer.TunnelChoice, error) {
	selectedSet := map[string]struct{}{}
	for _, item := range m.list.Items() {
		opt, ok := item.(multiOptionItem)
		if !ok || !opt.selected {
			continue
		}
		selectedSet[opt.value] = struct{}{}
	}

	if _, ok := selectedSet[tunnelOptionServer]; ok {
		return installer.TunnelChoice{Provided: false}, nil
	}
	if _, ok := selectedSet[tunnelOptionAll]; ok {
		return installer.TunnelChoice{Provided: true, IDs: dedupeStrings(availableIDs)}, nil
	}
	if _, ok := selectedSet[tunnelOptionNone]; ok {
		return installer.TunnelChoice{Provided: true, IDs: []string{}}, nil
	}

	availableSet := map[string]struct{}{}
	for _, id := range dedupeStrings(availableIDs) {
		availableSet[id] = struct{}{}
	}
	selectedIDs := make([]string, 0, len(availableSet))
	for _, id := range dedupeStrings(availableIDs) {
		if _, ok := selectedSet[id]; ok {
			selectedIDs = append(selectedIDs, id)
		}
	}
	if len(selectedIDs) > 0 {
		return installer.TunnelChoice{Provided: true, IDs: selectedIDs}, nil
	}

	if len(selectedSet) == 0 {
		return installer.TunnelChoice{Provided: false}, nil
	}
	return installer.TunnelChoice{}, fmt.Errorf("invalid tunnel selection")
}

func (m *installerModel) startAddBackendsSelector() {
	if m.windowsOnlyBackendMode() {
		m.screen = screenAddTunnelSelect
		m.add.backends = []config.BackendType{config.BackendWindows}
		m.add.backendIdx = 0
		m.startAddTunnelSelector(config.BackendWindows, defaultTunnelChoiceForBackend(config.BackendWindows, docTunnelIDs(m.add.doc)))
		return
	}

	m.screen = screenAddBackendsSelect
	m.errMsg = ""
	m.status = ""
	order, labels := backendSelectionOptions(m.backendTypesForPlatform())
	m.setMultiSelect("Select backends", order, labels, []string{string(config.BackendWGQuick)}, true)
}

func (m *installerModel) startAddTunnelSelector(bt config.BackendType, preset installer.TunnelChoice) {
	m.screen = screenAddTunnelSelect
	m.errMsg = ""
	m.status = ""
	available := dedupeStrings(docTunnelIDs(m.add.doc))
	order := []string{tunnelOptionServer, tunnelOptionNone, tunnelOptionAll}
	labels := map[string]string{
		tunnelOptionServer: "Defined by server",
		tunnelOptionNone:   "No tunnels",
		tunnelOptionAll:    "All tunnels",
	}
	for _, id := range available {
		order = append(order, id)
		labels[id] = id
	}
	m.setMultiSelect(
		fmt.Sprintf("Tunnel mode for backend %s", bt),
		order,
		labels,
		tunnelChoiceToSelectedValues(preset, available),
		false,
	)
}

func (m *installerModel) startUpdateBackendsSelector() {
	if m.windowsOnlyBackendMode() {
		m.update.pendingAll = []config.BackendType{config.BackendWindows}
		m.update.pending = nil
		m.update.pendingKeep = map[config.BackendType]installer.TunnelChoice{}
		m.update.pendingNew = map[config.BackendType]installer.TunnelChoice{}
		for _, plan := range m.update.plans {
			if plan.Type == config.BackendWindows {
				m.update.pendingKeep[config.BackendWindows] = plan.EnabledTunnels
				break
			}
		}
		m.update.plans = m.buildPendingPlans()
		m.startUpdateActionMenu()
		return
	}

	m.screen = screenUpdateBackendsSelect
	m.errMsg = ""
	m.status = ""
	order, labels := backendSelectionOptions(m.backendTypesForPlatform())
	selected := make([]string, 0, len(m.update.plans))
	for _, plan := range m.update.plans {
		selected = append(selected, string(plan.Type))
	}
	m.setMultiSelect("Select backends", order, labels, selected, true)
}

func (m installerModel) windowsOnlyBackendMode() bool {
	return runtime.GOOS == "windows"
}

func (m installerModel) backendTypesForPlatform() []config.BackendType {
	if m.windowsOnlyBackendMode() {
		return []config.BackendType{config.BackendWindows}
	}
	return []config.BackendType{config.BackendNetworkManager, config.BackendWGQuick, config.BackendNetNS}
}

func backendSelectionOptions(backends []config.BackendType) ([]string, map[string]string) {
	order := make([]string, 0, len(backends))
	labels := map[string]string{}
	for _, bt := range backends {
		value := string(bt)
		order = append(order, value)
		labels[value] = value
	}
	return order, labels
}

func defaultTunnelChoiceForBackend(bt config.BackendType, availableIDs []string) installer.TunnelChoice {
	if bt == config.BackendWindows {
		return installer.TunnelChoice{Provided: true, IDs: dedupeStrings(availableIDs)}
	}
	return installer.TunnelChoice{Provided: false}
}

func (m *installerModel) startUpdateTunnelSelector(bt config.BackendType, preset installer.TunnelChoice) {
	m.screen = screenUpdateTunnelSelect
	m.errMsg = ""
	m.status = ""
	available := dedupeStrings(docTunnelIDs(m.update.doc))
	order := []string{tunnelOptionServer, tunnelOptionNone, tunnelOptionAll}
	labels := map[string]string{
		tunnelOptionServer: "Defined by server",
		tunnelOptionNone:   "No tunnels",
		tunnelOptionAll:    "All tunnels",
	}
	for _, id := range available {
		order = append(order, id)
		labels[id] = id
	}
	m.setMultiSelect(
		fmt.Sprintf("Tunnel mode for backend %s", bt),
		order,
		labels,
		tunnelChoiceToSelectedValues(preset, available),
		false,
	)
}

func (m installerModel) buildPendingPlans() []installer.BackendPlan {
	plans := make([]installer.BackendPlan, 0, len(m.update.pendingAll))
	for _, bt := range m.update.pendingAll {
		if keep, ok := m.update.pendingKeep[bt]; ok {
			plans = append(plans, installer.BackendPlan{Type: bt, EnabledTunnels: keep})
			continue
		}
		if choice, ok := m.update.pendingNew[bt]; ok {
			plans = append(plans, installer.BackendPlan{Type: bt, EnabledTunnels: choice})
			continue
		}
		plans = append(plans, installer.BackendPlan{Type: bt, EnabledTunnels: installer.TunnelChoice{Provided: false}})
	}
	return plans
}

func (m *installerModel) setInputTitle(prompt string, defaultValue string) {
	m.title = prompt
	m.input = textinput.New()
	m.input.Prompt = "> "
	m.input.SetValue(defaultValue)
	m.input.CursorEnd()
	m.input.Focus()
}

func occupiedLabels(feeds map[string]config.FeedConfig) map[string]struct{} {
	out := map[string]struct{}{}
	for label := range feeds {
		out[label] = struct{}{}
	}
	return out
}

func sortedFeedLabels(feeds map[string]config.FeedConfig) []string {
	labels := make([]string, 0, len(feeds))
	for label := range feeds {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	return labels
}

func currentEndpoint(fc config.FeedConfig) string {
	if len(fc.Sync.Endpoints) == 0 {
		return ""
	}
	return strings.TrimSpace(fc.Sync.Endpoints[0])
}

func redactURLForUI(raw string) string {
	return clientfeed.RedactURL(raw)
}

func docTunnelIDs(doc model.FeedDocument) []string {
	ids := make([]string, 0, len(doc.Tunnels))
	for _, t := range doc.Tunnels {
		ids = append(ids, strings.TrimSpace(t.ID))
	}
	return ids
}

func tunnelChoiceToSelectedValues(choice installer.TunnelChoice, availableIDs []string) []string {
	available := dedupeStrings(availableIDs)
	if !choice.Provided {
		return []string{tunnelOptionServer}
	}
	if len(choice.IDs) == 0 {
		return []string{tunnelOptionNone}
	}

	availableSet := map[string]struct{}{}
	for _, id := range available {
		availableSet[id] = struct{}{}
	}

	selected := make([]string, 0, len(choice.IDs))
	for _, id := range choice.IDs {
		if _, ok := availableSet[id]; ok {
			selected = append(selected, id)
		}
	}
	if len(selected) == 0 {
		return []string{tunnelOptionNone}
	}
	if len(availableSet) > 0 && len(selected) == len(availableSet) {
		return []string{tunnelOptionAll}
	}
	return selected
}

func dedupeStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func actionLabel(action string) string {
	switch action {
	case "add":
		return "Add subscription"
	case "update":
		return "Update subscription"
	case "delete":
		return "Delete subscription"
	case "repair":
		return "Repair daemon/service"
	case "uninstall":
		return "Uninstall wg-feed"
	case "quit":
		return "Quit"
	default:
		return action
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
