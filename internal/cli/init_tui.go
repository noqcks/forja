package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	releaseinfo "github.com/noqcks/forja/internal/release"
	"github.com/spf13/cobra"
)

var (
	defaultAMD64AMI = releaseinfo.AWSAMI("us-east-1", "amd64")
	defaultARM64AMI = releaseinfo.AWSAMI("us-east-1", "arm64")

	errInitCanceled = errors.New("init canceled")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205"))

	subtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244"))

	focusedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205"))

	blurredStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	labelStyle = lipgloss.NewStyle().
			Bold(true)

	focusedLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("205"))

	cursorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	selectedDot   = focusedStyle.Render("●")
	unselectedDot = blurredStyle.Render("○")

	focusedButton = focusedStyle.Render("[ Submit ]")
	blurredButton = fmt.Sprintf("[ %s ]", blurredStyle.Render("Submit"))

	focusedCancelButton = focusedStyle.Render("[ Cancel ]")
	blurredCancelButton = fmt.Sprintf("[ %s ]", blurredStyle.Render("Cancel"))
)

type initAnswers struct {
	Region      string
	Registry    string
	AMD64AMI    string
	ARM64AMI    string
	CustomAMD64 string
	CustomARM64 string
}

type initFocus int

const (
	initFocusRegion initFocus = iota
	initFocusRegistry
	initFocusAMD64AMI
	initFocusARM64AMI
	initFocusCustomAMD64
	initFocusCustomARM64
	initFocusSubmit
	initFocusCancel
)

type initModel struct {
	regionInput      textinput.Model
	registryInput    textinput.Model
	amd64AMIInput    textinput.Model
	arm64AMIInput    textinput.Model
	customAMD64Input textinput.Model
	customARM64Input textinput.Model
	focusIndex       int
	errMessage       string
	cancelled        bool
	answers          initAnswers
}

func collectInitAnswersTUI(cmd *cobra.Command) (initAnswers, error) {
	model := newInitModel()
	program := tea.NewProgram(
		model,
		tea.WithContext(cmd.Context()),
		tea.WithInput(cmd.InOrStdin()),
		tea.WithOutput(cmd.OutOrStdout()),
	)

	result, err := program.Run()
	if err != nil {
		return initAnswers{}, err
	}

	finalModel, ok := result.(initModel)
	if !ok {
		return initAnswers{}, fmt.Errorf("unexpected init model type %T", result)
	}
	if finalModel.cancelled {
		return initAnswers{}, errInitCanceled
	}

	return finalModel.answers, nil
}

func newInitModel() initModel {
	model := initModel{
		regionInput:      newInitTextInput("us-east-1", "us-east-1"),
		registryInput:    newInitTextInput("", "ghcr.io/org"),
		amd64AMIInput:    newInitTextInput(defaultAMD64AMI, "ami-0123456789abcdef0"),
		arm64AMIInput:    newInitTextInput(defaultARM64AMI, "ami-0123456789abcdef0"),
		customAMD64Input: newInitTextInput("c7a.8xlarge", "c7a.8xlarge"),
		customARM64Input: newInitTextInput("c7g.8xlarge", "c7g.8xlarge"),
	}
	model.applyFocusStyles()
	return model
}

func newInitTextInput(defaultValue, placeholder string) textinput.Model {
	input := textinput.New()
	input.Prompt = "  "
	input.CharLimit = 512
	input.Width = 40
	input.Placeholder = placeholder
	input.Cursor.Style = cursorStyle
	if defaultValue != "" {
		input.SetValue(defaultValue)
	}
	return input
}

func (m initModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m initModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "shift+tab", "up":
			m.moveFocus(-1)
			return m, nil
		case "tab", "down":
			m.moveFocus(1)
			return m, nil
		case "enter":
			switch m.focusedItem() {
			case initFocusSubmit:
				answers := m.answersFromState()
				if err := validateInitAnswers(answers); err != nil {
					m.errMessage = err.Error()
					return m, nil
				}
				m.answers = answers
				return m, tea.Quit
			case initFocusCancel:
				m.cancelled = true
				return m, tea.Quit
			default:
				m.moveFocus(1)
				return m, nil
			}
		}
	}

	cmd := m.updateFocusedInput(msg)
	return m, cmd
}

func (m initModel) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Forja Init"))
	b.WriteString("\n\n")
	b.WriteString(subtitleStyle.Render("Configure the AWS resources that forja should provision."))
	b.WriteString("\n\n")

	b.WriteString(m.renderField(initFocusRegion, "AWS region", m.regionInput.View()))
	b.WriteString(m.renderField(initFocusRegistry, "Default registry", m.registryInput.View()))
	b.WriteString(m.renderField(initFocusAMD64AMI, "Published amd64 AMI", m.amd64AMIInput.View()))
	b.WriteString(m.renderField(initFocusARM64AMI, "Published arm64 AMI", m.arm64AMIInput.View()))
	b.WriteString(m.renderField(initFocusCustomAMD64, "amd64 instance", m.customAMD64Input.View()))
	b.WriteString(m.renderField(initFocusCustomARM64, "arm64 instance", m.customARM64Input.View()))

	b.WriteString("\n")
	b.WriteString(m.renderButtons())

	if m.errMessage != "" {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render("  Error: " + m.errMessage))
	}

	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("  ↑/↓ navigate • enter confirm • esc cancel"))
	b.WriteString("\n")

	return b.String()
}

func (m *initModel) updateFocusedInput(msg tea.Msg) tea.Cmd {
	switch m.focusedItem() {
	case initFocusRegion:
		var cmd tea.Cmd
		m.regionInput, cmd = m.regionInput.Update(msg)
		m.errMessage = ""
		return cmd
	case initFocusRegistry:
		var cmd tea.Cmd
		m.registryInput, cmd = m.registryInput.Update(msg)
		m.errMessage = ""
		return cmd
	case initFocusAMD64AMI:
		var cmd tea.Cmd
		m.amd64AMIInput, cmd = m.amd64AMIInput.Update(msg)
		m.errMessage = ""
		return cmd
	case initFocusARM64AMI:
		var cmd tea.Cmd
		m.arm64AMIInput, cmd = m.arm64AMIInput.Update(msg)
		m.errMessage = ""
		return cmd
	case initFocusCustomAMD64:
		var cmd tea.Cmd
		m.customAMD64Input, cmd = m.customAMD64Input.Update(msg)
		m.errMessage = ""
		return cmd
	case initFocusCustomARM64:
		var cmd tea.Cmd
		m.customARM64Input, cmd = m.customARM64Input.Update(msg)
		m.errMessage = ""
		return cmd
	default:
		return nil
	}
}

func (m initModel) visibleItems() []initFocus {
	items := []initFocus{
		initFocusRegion,
		initFocusRegistry,
		initFocusAMD64AMI,
		initFocusARM64AMI,
		initFocusCustomAMD64,
		initFocusCustomARM64,
	}
	return append(items, initFocusSubmit, initFocusCancel)
}

func (m initModel) focusedItem() initFocus {
	items := m.visibleItems()
	if m.focusIndex < 0 || m.focusIndex >= len(items) {
		return items[0]
	}
	return items[m.focusIndex]
}

func (m *initModel) moveFocus(delta int) {
	items := m.visibleItems()
	m.focusIndex = (m.focusIndex + delta + len(items)) % len(items)
	m.errMessage = ""
	m.applyFocusStyles()
}

func (m *initModel) clampFocus() {
	items := m.visibleItems()
	if m.focusIndex >= len(items) {
		m.focusIndex = len(items) - 1
	}
	if m.focusIndex < 0 {
		m.focusIndex = 0
	}
}

func (m *initModel) applyFocusStyles() {
	inputs := []*textinput.Model{
		&m.regionInput,
		&m.registryInput,
		&m.amd64AMIInput,
		&m.arm64AMIInput,
		&m.customAMD64Input,
		&m.customARM64Input,
	}
	focusItems := []initFocus{
		initFocusRegion,
		initFocusRegistry,
		initFocusAMD64AMI,
		initFocusARM64AMI,
		initFocusCustomAMD64,
		initFocusCustomARM64,
	}

	for i, input := range inputs {
		if m.focusedItem() == focusItems[i] {
			input.Focus()
			input.PromptStyle = focusedStyle
			input.TextStyle = focusedStyle
		} else {
			input.Blur()
			input.PromptStyle = lipgloss.NewStyle()
			input.TextStyle = lipgloss.NewStyle()
		}
	}
}

func (m initModel) renderField(focus initFocus, label string, inputView string) string {
	focused := m.focusedItem() == focus

	cursor := "  "
	style := labelStyle
	if focused {
		cursor = focusedStyle.Render("> ")
		style = focusedLabelStyle
	}

	return fmt.Sprintf("%s%s\n%s\n\n", cursor, style.Render(label), inputView)
}

func (m initModel) renderButtons() string {
	submit := blurredButton
	if m.focusedItem() == initFocusSubmit {
		submit = focusedButton
	}

	cancel := blurredCancelButton
	if m.focusedItem() == initFocusCancel {
		cancel = focusedCancelButton
	}

	return fmt.Sprintf("  %s  %s\n", submit, cancel)
}

func (m initModel) answersFromState() initAnswers {
	return initAnswers{
		Region:      strings.TrimSpace(m.regionInput.Value()),
		Registry:    strings.TrimSpace(m.registryInput.Value()),
		AMD64AMI:    strings.TrimSpace(m.amd64AMIInput.Value()),
		ARM64AMI:    strings.TrimSpace(m.arm64AMIInput.Value()),
		CustomAMD64: strings.TrimSpace(m.customAMD64Input.Value()),
		CustomARM64: strings.TrimSpace(m.customARM64Input.Value()),
	}
}

func validateInitAnswers(answers initAnswers) error {
	if strings.TrimSpace(answers.Region) == "" {
		return errors.New("AWS region is required")
	}
	if strings.TrimSpace(answers.AMD64AMI) == "" && resolvePublishedAMI(answers.Region, "amd64", answers.AMD64AMI) == "" {
		return fmt.Errorf("published amd64 AMI is required for region %s", strings.TrimSpace(answers.Region))
	}
	if strings.TrimSpace(answers.ARM64AMI) == "" && resolvePublishedAMI(answers.Region, "arm64", answers.ARM64AMI) == "" {
		return fmt.Errorf("published arm64 AMI is required for region %s", strings.TrimSpace(answers.Region))
	}
	if strings.TrimSpace(answers.CustomAMD64) == "" {
		return errors.New("amd64 instance type is required")
	}
	if strings.TrimSpace(answers.CustomARM64) == "" {
		return errors.New("arm64 instance type is required")
	}
	return nil
}
