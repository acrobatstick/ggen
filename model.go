package main

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var (
	srcStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	doneStyle = lipgloss.NewStyle().Margin(1, 2)
	checkMark = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).SetString("✓")
	errorMark = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).SetString("✗")
	infoMark  = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).SetString("i")
)

type model struct {
	spinner   spinner.Model
	progress  progress.Model
	completed int
	mediaLen  int
	done      bool
	width     int
	height    int
}

type errorMsg error

type doneMsg struct{}

func newModel(mediaLen int) model {
	p := progress.New(
		progress.WithDefaultBlend(),
		progress.WithWidth(40),
		progress.WithoutPercentage(),
	)
	s := spinner.New()
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("63"))
	return model{
		spinner:  s,
		progress: p,
		mediaLen: mediaLen,
	}
}

func (m model) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			return m, tea.Quit
		}
	case errorMsg:
		return m, tea.Sequence(
			tea.Printf("%s %s", errorMark, msg.Error()),
			tea.Quit,
		)

	case result:
		m.completed++
		progressCmd := m.progress.SetPercent(float64(m.completed) / float64(m.mediaLen))

		var print tea.Cmd

		src := srcStyle.Render(msg.path)

		switch msg.status {
		case resultSuccess:
			print = tea.Printf("%s  Processed successfully: %s", checkMark, src)
		case resultCached:
			print = tea.Printf("%s  Already cached, skipping: %s", infoMark, src)
		case resultFailed:
			print = tea.Printf("%s Processing failed: %s", errorMark, src)
		}

		return m, tea.Batch(progressCmd, print)

	case doneMsg:
		m.done = true
		return m, tea.Quit

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case progress.FrameMsg:
		var cmd tea.Cmd
		m.progress, cmd = m.progress.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) View() tea.View {
	n := m.mediaLen
	w := lipgloss.Width(fmt.Sprintf("%d", n))

	if m.done {
		return tea.NewView(doneStyle.Render(fmt.Sprintf("Done! found %d media sources.\n", n)))
	}

	pkgCount := fmt.Sprintf(" %*d/%*d", w, m.completed, w, n)

	spin := m.spinner.View() + " "
	prog := m.progress.View()
	cellsAvail := max(0, m.width-lipgloss.Width(spin+prog+pkgCount))

	info := lipgloss.NewStyle().MaxWidth(cellsAvail).Render("Processing preview image")

	cellsRemaining := max(0, m.width-lipgloss.Width(spin+info+prog+pkgCount))
	gap := strings.Repeat(" ", cellsRemaining)

	return tea.NewView(spin + info + gap + prog + pkgCount)
}
