package cli

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	releaseinfo "github.com/noqcks/forja/internal/release"
	"github.com/spf13/cobra"
)

var (
	defaultAMD64AMI      = releaseinfo.AWSAMI("us-east-1", "amd64")
	defaultARM64AMI      = releaseinfo.AWSAMI("us-east-1", "arm64")
	defaultAMD64Instance = "c7a.8xlarge"
	defaultARM64Instance = "c7g.8xlarge"

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
	initFocusSubmit
	initFocusCancel
)

type initModel struct {
	regions     []string
	regionIndex int
	focusIndex  int
	errMessage  string
	cancelled   bool
	answers     initAnswers
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
	regions := releaseinfo.AWSRegions()
	return initModel{
		regions: regions,
	}
}

func (m initModel) Init() tea.Cmd {
	return nil
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
			if m.focusedItem() == initFocusRegion && m.regionIndex > 0 {
				m.regionIndex--
				m.errMessage = ""
			}
			return m, nil
		case "right":
			if m.focusedItem() == initFocusRegion && m.regionIndex < len(m.regions)-1 {
				m.regionIndex++
				m.errMessage = ""
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

	return m, nil
}

func (m initModel) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Forja Init"))
	b.WriteString("\n\n")
	b.WriteString(subtitleStyle.Render("The following AWS resources will be created:"))
	b.WriteString("\n\n")
	b.WriteString(subtitleStyle.Render("  • S3 bucket (forja-cache-<account>-<region>)"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render("  • IAM role & instance profile (forja-builder)"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render("  • EC2 security group (forja-builder)"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render("  • EC2 launch templates (forja-builder-amd64, forja-builder-arm64)"))
	b.WriteString("\n\n")

	b.WriteString(m.renderRegionSelector())

	b.WriteString("\n")
	b.WriteString(m.renderButtons())

	if m.errMessage != "" {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render("  Error: " + m.errMessage))
	}

	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("  ↑/↓ navigate • ←/→ change region • enter confirm • esc cancel"))
	b.WriteString("\n")

	return b.String()
}

func (m initModel) renderRegionSelector() string {
	focused := m.focusedItem() == initFocusRegion

	cursor := "  "
	style := labelStyle
	if focused {
		cursor = focusedStyle.Render("> ")
		style = focusedLabelStyle
	}

	var dots strings.Builder
	for i, r := range m.regions {
		if i == m.regionIndex {
			dots.WriteString(selectedDot + " " + style.Render(r))
		} else {
			dots.WriteString(unselectedDot + " " + blurredStyle.Render(r))
		}
		if i < len(m.regions)-1 {
			dots.WriteString("  ")
		}
	}

	return fmt.Sprintf("%s%s\n  %s\n\n", cursor, style.Render("AWS region"), dots.String())
}

func (m initModel) visibleItems() []initFocus {
	items := []initFocus{initFocusRegion}
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
	region := m.regions[m.regionIndex]
	return initAnswers{
		Region:      region,
		Registry:    "",
		AMD64AMI:    resolvePublishedAMI(region, "amd64", ""),
		ARM64AMI:    resolvePublishedAMI(region, "arm64", ""),
		CustomAMD64: defaultAMD64Instance,
		CustomARM64: defaultARM64Instance,
	}
}

func validateInitAnswers(answers initAnswers) error {
	region := strings.TrimSpace(answers.Region)
	if strings.TrimSpace(answers.Region) == "" {
		return errors.New("AWS region is required")
	}
	if strings.TrimSpace(answers.AMD64AMI) == "" && resolvePublishedAMI(region, "amd64", answers.AMD64AMI) == "" {
		return fmt.Errorf("no published amd64 AMI for region %s; rerun with --amd64-ami", region)
	}
	if strings.TrimSpace(answers.ARM64AMI) == "" && resolvePublishedAMI(region, "arm64", answers.ARM64AMI) == "" {
		return fmt.Errorf("no published arm64 AMI for region %s; rerun with --arm64-ami", region)
	}
	if strings.TrimSpace(answers.CustomAMD64) == "" {
		return errors.New("amd64 instance type is required")
	}
	if strings.TrimSpace(answers.CustomARM64) == "" {
		return errors.New("arm64 instance type is required")
	}
	return nil
}
