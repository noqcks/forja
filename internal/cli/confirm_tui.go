package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var errPromptCanceled = errors.New("prompt canceled")

type confirmActionModel struct {
	prompt    string
	selected  int
	cancelled bool
	result    bool
}

func confirmAction(cmd *cobra.Command, prompt string, defaultYes bool) (bool, error) {
	selected := 1
	if defaultYes {
		selected = 0
	}

	model := confirmActionModel{
		prompt:   prompt,
		selected: selected,
	}

	program := tea.NewProgram(
		model,
		tea.WithContext(cmd.Context()),
		tea.WithInput(cmd.InOrStdin()),
		tea.WithOutput(cmd.OutOrStdout()),
	)

	result, err := program.Run()
	if err != nil {
		return false, err
	}

	finalModel, ok := result.(confirmActionModel)
	if !ok {
		return false, fmt.Errorf("unexpected confirm model type %T", result)
	}
	if finalModel.cancelled {
		return false, errPromptCanceled
	}
	return finalModel.result, nil
}

func (m confirmActionModel) Init() tea.Cmd {
	return nil
}

func (m confirmActionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "left", "right", "tab":
			m.selected = 1 - m.selected
			return m, nil
		case "y":
			m.result = true
			return m, tea.Quit
		case "n":
			m.result = false
			return m, tea.Quit
		case "enter":
			m.result = m.selected == 0
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m confirmActionModel) View() string {
	yes := "[ Yes ]"
	no := "[ No ]"
	if m.selected == 0 {
		yes = "[*Yes*]"
	} else {
		no = "[*No*]"
	}

	return fmt.Sprintf(
		"%s\nUse Left/Right, Tab, Y/N, or Enter. Esc cancels.\n\n  %s  %s\n",
		m.prompt,
		yes,
		no,
	)
}

type confirmTypedValueModel struct {
	prompt     string
	expected   string
	input      textinput.Model
	errMessage string
	cancelled  bool
	result     string
}

func confirmTypedValue(cmd *cobra.Command, prompt string, expected string) (string, error) {
	input := textinput.New()
	input.Prompt = ""
	input.CharLimit = 128
	input.Width = 32
	input.Focus()

	model := confirmTypedValueModel{
		prompt:   prompt,
		expected: expected,
		input:    input,
	}

	program := tea.NewProgram(
		model,
		tea.WithContext(cmd.Context()),
		tea.WithInput(cmd.InOrStdin()),
		tea.WithOutput(cmd.OutOrStdout()),
	)

	result, err := program.Run()
	if err != nil {
		return "", err
	}

	finalModel, ok := result.(confirmTypedValueModel)
	if !ok {
		return "", fmt.Errorf("unexpected typed confirm model type %T", result)
	}
	if finalModel.cancelled {
		return "", errPromptCanceled
	}
	return finalModel.result, nil
}

func (m confirmTypedValueModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m confirmTypedValueModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "enter":
			value := strings.TrimSpace(m.input.Value())
			if value != m.expected {
				m.errMessage = fmt.Sprintf("type %q exactly to continue", m.expected)
				return m, nil
			}
			m.result = value
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.errMessage = ""
	return m, cmd
}

func (m confirmTypedValueModel) View() string {
	var b strings.Builder

	b.WriteString(m.prompt)
	b.WriteString("\nEnter submits. Esc cancels.\n\n")
	b.WriteString("  ")
	b.WriteString(m.input.View())
	if m.errMessage != "" {
		b.WriteString("\n\nerror: ")
		b.WriteString(m.errMessage)
	}
	b.WriteString("\n")

	return b.String()
}
