package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/box1bs/monocle/internal/model"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
)

const (
	minX = 30
	minY = 20
	outputSize = 100
)

type logMsg string
type showDocCount int

type viewSize struct {
	width      int
	height     int
	leftWidth  int
	rightWidth int
}

type viewModel struct {
	size        	*viewSize
	UILogWriter 	*outputChannel
	searchLabel 	textinput.Model

	border 			lipgloss.Style
	rightVessel     viewport.Model
	leftVessel      viewport.Model

	getCurrentState func() (int, error)
	searchFunc 		func(string, int) []*model.Document
	logLines    	[]string
	logPlate    	[]string
	closeIndex 		chan struct{}
	counter     	int
	curLogSize 		int
}

func NewLogChannel(size int) *outputChannel {
	return &outputChannel{readCh: make(chan []byte, size)}
}

func InitModel(logChan *outputChannel, borderColor string, currentHandledNum func() (int, error), searchFunc func(string, int) []*model.Document, quitChan chan struct{}) *viewModel {
	ti := textinput.New()
	ti.Placeholder = "Enter request..."
	ti.Focus()
	ti.Width = 20

	vp := viewport.New(minX, minY)
	vpLeft := viewport.New(minX, minY)

	return &viewModel{
		getCurrentState: currentHandledNum,
		searchFunc: searchFunc,
		size: &viewSize{
			width: 0,
			height: 0,
			leftWidth: 0,
			rightWidth: 0,
		},
		border: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(borderColor)),
		UILogWriter: logChan,
		searchLabel: ti,
		rightVessel: vp,
		leftVessel: vpLeft,
		logPlate: make([]string, 0),
		logLines: make([]string, 0),
		closeIndex: quitChan,
	}
}

type outputChannel struct {
	readCh chan []byte
}

func (oc *outputChannel) Write(data []byte) (n int, err error) {
	select {
	case oc.readCh <- data:
	default:
	}
	return len(data), nil
}

func showIndexedNum() tea.Cmd {
	return tea.Tick(time.Second * 30, func(t time.Time) tea.Msg {
		return showDocCount(1)
	})
}

func (vm *viewModel) waitForLog() tea.Cmd {
	return func() tea.Msg {
		return logMsg(<-vm.UILogWriter.readCh)
	}
}

func (vm *viewModel) renderLeftLog() {
	width := vm.leftVessel.Width
	if width < minX * 0.4 {
		return
	}
	
	allLogs := strings.Join(vm.logPlate, "")
	wrappedContent := wordwrap.String(allLogs, width)
	
	vm.leftVessel.SetContent(wrappedContent)
	vm.leftVessel.GotoBottom()
}

func (vm *viewModel) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, vm.waitForLog(), showIndexedNum())
}

func (vm *viewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		vm.size.width = msg.Width
		vm.size.height = msg.Height

		vm.size.leftWidth = int(float64(vm.size.width) * 0.4)
		vm.size.rightWidth = vm.size.width - vm.size.leftWidth

		inputHeight := 4

		vm.leftVessel.Width = max(vm.size.leftWidth - 2, 0)
		vm.leftVessel.Height = max(vm.size.height - 2, 0)
		vm.rightVessel.Width = max(vm.size.rightWidth - 2, 0)
		vm.rightVessel.Height = max(vm.size.height - inputHeight - 2, 0)

		vm.searchLabel.Width = max(vm.size.rightWidth - 15, 10)

	case logMsg:
		vm.logPlate = append(vm.logPlate, string(msg))
		vm.curLogSize += len(string(msg)) / vm.size.leftWidth
		if len(vm.logPlate) > 500 {
			vm.logPlate = vm.logPlate[len(vm.logPlate) - 500:]
		}
		vm.renderLeftLog()

		return vm, vm.waitForLog()

	case showDocCount:
		vm.counter, _ = vm.getCurrentState()
		return vm, showIndexedNum()

	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			text := strings.TrimSpace(vm.searchLabel.Value())
			if text != "" {
				vm.logLines = make([]string, 0)
				out := vm.searchFunc(text, outputSize)
				for _, doc := range out {
					vm.logLines = append(vm.logLines, doc.URL)
				}
				vm.rightVessel.SetContent(strings.Join(vm.logLines, "\n"))
				vm.searchLabel.SetValue("")
				vm.rightVessel.GotoTop()
			}
		case "q":
			return vm, tea.Quit
		case "ctrl+c":
			select {
			case vm.closeIndex <- struct{}{}:
			default:
			}
			return vm, nil
		}
	}

	var cmd tea.Cmd
	vm.searchLabel, cmd = vm.searchLabel.Update(msg)
	cmds = append(cmds, cmd)
	vm.rightVessel, cmd = vm.rightVessel.Update(msg)
	cmds = append(cmds, cmd)
	vm.leftVessel, cmd = vm.leftVessel.Update(msg)
	cmds = append(cmds, cmd)

	return vm, tea.Batch(cmds...)
}

func (vm *viewModel) View() string {
	if vm.size.width < minX || vm.size.height < minY {
		return lipgloss.Place(
			vm.size.width, vm.size.height, 
			lipgloss.Center, lipgloss.Center, 
			lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("Terminal too small"),
		)
	}

	leftView := vm.border.
		Width(vm.size.leftWidth - 2).
		Height(vm.size.height - 2).
		Render(vm.leftVessel.View())

	inputView := vm.searchLabel.View()
	counterView := fmt.Sprintf("[%d]", vm.counter)

	headerContent := lipgloss.JoinHorizontal(lipgloss.Center, 
		inputView,
		counterView,
	)
	topRightBox := vm.border.
		Width(vm.size.rightWidth - 2).
		Height(2).
		Render(headerContent)

	bottomHeight := vm.size.height - 6
	bottomRightBox := vm.border.
		Width(vm.size.rightWidth - 2).
		Height(max(bottomHeight, 0)).
		Render(vm.rightVessel.View())

	rightView := lipgloss.JoinVertical(lipgloss.Left, topRightBox, bottomRightBox)
	return lipgloss.JoinHorizontal(lipgloss.Top, leftView, rightView)
}