package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

const (
	initSizeSmall  = "Small (c7a.large / c7g.large)"
	initSizeMedium = "Medium (c7a.xlarge / c7g.xlarge)"
	initSizeLarge  = "Large (c7a.2xlarge / c7g.2xlarge)"
	initSizeCustom = "Custom"
)

var errInitCanceled = errors.New("init canceled")

var initSizeOptions = []string{
	initSizeSmall,
	initSizeMedium,
	initSizeLarge,
	initSizeCustom,
}

type initAnswers struct {
	Region      string
	SizeChoice  string
	Registry    string
	AMD64AMI    string
	ARM64AMI    string
	CustomAMD64 string
	CustomARM64 string
}

type initFocus int

const (
	initFocusRegion initFocus = iota
	initFocusSize
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
	sizeIndex        int
	focusIndex       int
	errMessage       string
	cancelled        bool
	answers          initAnswers
}

func collectInitAnswers(cmd *cobra.Command) (initAnswers, error) {
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
		regionInput:      newInitTextInput("us-east-1"),
		registryInput:    newInitTextInput(""),
		amd64AMIInput:    newInitTextInput(""),
		arm64AMIInput:    newInitTextInput(""),
		customAMD64Input: newInitTextInput("c7a.large"),
		customARM64Input: newInitTextInput("c7g.large"),
	}
	model.setFocus()
	return model
}

func newInitTextInput(defaultValue string) textinput.Model {
	input := textinput.New()
	input.Prompt = ""
	input.CharLimit = 512
	input.Width = 56
	input.SetValue(defaultValue)
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
		case "left":
			if m.focusedItem() == initFocusSize && m.sizeIndex > 0 {
				m.sizeIndex--
				m.errMessage = ""
				m.clampFocus()
				m.setFocus()
			}
			return m, nil
		case "right":
			if m.focusedItem() == initFocusSize && m.sizeIndex < len(initSizeOptions)-1 {
				m.sizeIndex++
				m.errMessage = ""
				m.clampFocus()
				m.setFocus()
			}
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

	b.WriteString("Forja init\n\n")
	b.WriteString("Configure the AWS resources that forja should provision.\n")
	b.WriteString("Tab or Up/Down moves between fields. Left/Right changes instance size. Enter continues. Esc cancels.\n\n")
	b.WriteString(m.renderInput(initFocusRegion, "AWS region", m.regionInput.View(), false))
	b.WriteString(m.renderSizeChoice())
	b.WriteString(m.renderInput(initFocusRegistry, "Default registry (optional)", m.registryInput.View(), false))
	b.WriteString(m.renderInput(initFocusAMD64AMI, "Published amd64 AMI ID", m.amd64AMIInput.View(), true))
	b.WriteString(m.renderInput(initFocusARM64AMI, "Published arm64 AMI ID", m.arm64AMIInput.View(), true))
	if m.usesCustomInstances() {
		b.WriteString(m.renderInput(initFocusCustomAMD64, "Custom amd64 instance type", m.customAMD64Input.View(), true))
		b.WriteString(m.renderInput(initFocusCustomARM64, "Custom arm64 instance type", m.customARM64Input.View(), true))
	}
	b.WriteString(m.renderButtons())
	if m.errMessage != "" {
		b.WriteString("\nerror: ")
		b.WriteString(m.errMessage)
	}
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
		initFocusSize,
		initFocusRegistry,
		initFocusAMD64AMI,
		initFocusARM64AMI,
	}
	if m.usesCustomInstances() {
		items = append(items, initFocusCustomAMD64, initFocusCustomARM64)
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
	m.setFocus()
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

func (m *initModel) setFocus() {
	m.regionInput.Blur()
	m.registryInput.Blur()
	m.amd64AMIInput.Blur()
	m.arm64AMIInput.Blur()
	m.customAMD64Input.Blur()
	m.customARM64Input.Blur()

	switch m.focusedItem() {
	case initFocusRegion:
		m.regionInput.Focus()
	case initFocusRegistry:
		m.registryInput.Focus()
	case initFocusAMD64AMI:
		m.amd64AMIInput.Focus()
	case initFocusARM64AMI:
		m.arm64AMIInput.Focus()
	case initFocusCustomAMD64:
		m.customAMD64Input.Focus()
	case initFocusCustomARM64:
		m.customARM64Input.Focus()
	}
}

func (m initModel) usesCustomInstances() bool {
	return initSizeOptions[m.sizeIndex] == initSizeCustom
}

func (m initModel) renderInput(focus initFocus, label string, value string, required bool) string {
	prefix := "  "
	if m.focusedItem() == focus {
		prefix = "> "
	}

	suffix := ""
	if required {
		suffix = " *"
	}

	return fmt.Sprintf("%s%s%s\n  %s\n\n", prefix, label, suffix, value)
}

func (m initModel) renderSizeChoice() string {
	prefix := "  "
	if m.focusedItem() == initFocusSize {
		prefix = "> "
	}

	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString("Instance size for builds\n")
	for i, option := range initSizeOptions {
		marker := "( )"
		if i == m.sizeIndex {
			marker = "(x)"
		}
		cursor := "  "
		if m.focusedItem() == initFocusSize && i == m.sizeIndex {
			cursor = "> "
		}
		b.WriteString(fmt.Sprintf("  %s%s %s\n", cursor, marker, option))
	}
	b.WriteString("\n")
	return b.String()
}

func (m initModel) renderButtons() string {
	submit := "[ Submit ]"
	cancel := "[ Cancel ]"
	if m.focusedItem() == initFocusSubmit {
		submit = "[*Submit*]"
	}
	if m.focusedItem() == initFocusCancel {
		cancel = "[*Cancel*]"
	}

	return fmt.Sprintf("  %s  %s\n", submit, cancel)
}

func (m initModel) answersFromState() initAnswers {
	return initAnswers{
		Region:      strings.TrimSpace(m.regionInput.Value()),
		SizeChoice:  initSizeOptions[m.sizeIndex],
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
	if strings.TrimSpace(answers.AMD64AMI) == "" {
		return errors.New("published amd64 AMI ID is required")
	}
	if strings.TrimSpace(answers.ARM64AMI) == "" {
		return errors.New("published arm64 AMI ID is required")
	}
	if answers.SizeChoice == initSizeCustom {
		if strings.TrimSpace(answers.CustomAMD64) == "" {
			return errors.New("custom amd64 instance type is required")
		}
		if strings.TrimSpace(answers.CustomARM64) == "" {
			return errors.New("custom arm64 instance type is required")
		}
	}
	return nil
}

func instanceTypesForSizeChoice(sizeChoice string, customAMD64 string, customARM64 string) (map[string]string, error) {
	switch sizeChoice {
	case initSizeSmall:
		return map[string]string{
			"amd64": "c7a.large",
			"arm64": "c7g.large",
		}, nil
	case initSizeMedium:
		return map[string]string{
			"amd64": "c7a.xlarge",
			"arm64": "c7g.xlarge",
		}, nil
	case initSizeLarge:
		return map[string]string{
			"amd64": "c7a.2xlarge",
			"arm64": "c7g.2xlarge",
		}, nil
	case initSizeCustom:
		if strings.TrimSpace(customAMD64) == "" || strings.TrimSpace(customARM64) == "" {
			return nil, errors.New("custom instance types are required when size is Custom")
		}
		return map[string]string{
			"amd64": strings.TrimSpace(customAMD64),
			"arm64": strings.TrimSpace(customARM64),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported size choice %q", sizeChoice)
	}
}
